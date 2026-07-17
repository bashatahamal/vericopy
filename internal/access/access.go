package access

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"

	pkgSFTP "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/bashatahamal/vericopy/internal/verrors"
)

var accountNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,63}$`)

// ValidateAccountName applies the conservative remote-command allowlist.
func ValidateAccountName(value string) error {
	if !accountNamePattern.MatchString(value) {
		return verrors.New(verrors.CodeInvalidArguments,
			"user and group names may contain only letters, digits, underscore, dot, and hyphen")
	}
	return nil
}

// CommandRunner executes a fixed remote command.
type CommandRunner interface {
	Run(command string) ([]byte, error)
}

// SSHRunner creates one SSH session per command.
type SSHRunner struct{ Client *ssh.Client }

func (r SSHRunner) Run(command string) ([]byte, error) {
	session, err := r.Client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return session.Output(command)
}

// Identity contains numeric ownership information used by POSIX access checks.
type Identity struct {
	Name   string   `json:"name"`
	UID    uint32   `json:"uid"`
	Groups []uint32 `json:"groups"`
}

// Resolver resolves remote account and group IDs using validated fixed commands.
type Resolver struct{ Runner CommandRunner }

func (r Resolver) User(ctx context.Context, name string) (Identity, error) {
	if err := ValidateAccountName(name); err != nil {
		return Identity{}, err
	}
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	uidOutput, err := r.Runner.Run("id -u " + name)
	if err != nil {
		return Identity{}, verrors.Wrap(verrors.CodeDestinationNotReadable,
			fmt.Sprintf("remote user %q does not exist or cannot be inspected", name), err)
	}
	groupsOutput, err := r.Runner.Run("id -G " + name)
	if err != nil {
		return Identity{}, verrors.Wrap(verrors.CodeDestinationNotReadable,
			fmt.Sprintf("remote groups for %q cannot be inspected", name), err)
	}
	uid, err := parseUint32(strings.TrimSpace(string(uidOutput)))
	if err != nil {
		return Identity{}, err
	}
	groups := make([]uint32, 0)
	for _, item := range strings.Fields(string(groupsOutput)) {
		group, parseErr := parseUint32(item)
		if parseErr != nil {
			return Identity{}, parseErr
		}
		groups = append(groups, group)
	}
	return Identity{Name: name, UID: uid, Groups: groups}, nil
}

func (r Resolver) Group(ctx context.Context, name string) (int, error) {
	if err := ValidateAccountName(name); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	output, err := r.Runner.Run("getent group " + name)
	if err != nil {
		return 0, verrors.Wrap(verrors.CodeGroupUnavailable,
			fmt.Sprintf("remote group %q does not exist or cannot be resolved", name), err)
	}
	fields := strings.Split(strings.TrimSpace(string(output)), ":")
	if len(fields) < 3 {
		return 0, verrors.New(verrors.CodeGroupUnavailable, "the remote group record is malformed")
	}
	gid, err := strconv.Atoi(fields[2])
	if err != nil || gid < 0 {
		return 0, verrors.New(verrors.CodeGroupUnavailable, "the remote group ID is invalid")
	}
	return gid, nil
}

func parseUint32(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, verrors.Wrap(verrors.CodeDestinationNotReadable, "the remote account ID is invalid", err)
	}
	return uint32(parsed), nil
}

// RemoteFS exposes safe metadata inspection without remote path shell commands.
type RemoteFS interface {
	Lstat(path string) (fs.FileInfo, error)
}

// Report describes every checked path component.
type Report struct {
	Destination string      `json:"destination"`
	User        string      `json:"user"`
	Readable    bool        `json:"readable"`
	Checks      []PathCheck `json:"checks"`
}

type PathCheck struct {
	Path     string `json:"path"`
	Mode     string `json:"mode"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
	Required string `json:"required"`
	Allowed  bool   `json:"allowed"`
}

// Check verifies POSIX traversal and target-read permissions without mutation.
func Check(ctx context.Context, remote RemoteFS, destination string, identity Identity) (Report, error) {
	cleaned := path.Clean(destination)
	if !path.IsAbs(cleaned) {
		return Report{}, verrors.New(verrors.CodeInvalidRemoteDestination,
			"service-user access checks require an absolute remote path")
	}
	report := Report{Destination: cleaned, User: identity.Name, Readable: true}
	components := pathComponents(cleaned)
	for index, component := range components {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		info, err := remote.Lstat(component)
		if err != nil {
			return Report{}, verrors.Wrap(verrors.CodeDestinationNotReadable,
				fmt.Sprintf("cannot inspect %q", component), err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return Report{}, verrors.New(verrors.CodeDestinationNotReadable,
				fmt.Sprintf("access check stopped at symbolic link %q", component))
		}
		stat, ok := info.Sys().(*pkgSFTP.FileStat)
		if !ok || stat == nil {
			return Report{}, verrors.New(verrors.CodeDestinationNotReadable, "the SFTP server did not provide POSIX ownership metadata")
		}
		last := index == len(components)-1
		required := uint32(1)
		requiredName := "traverse"
		if last {
			required, requiredName = 4, "read"
			if info.IsDir() {
				required, requiredName = 5, "read+traverse"
			}
		}
		allowed := identity.UID == 0 || allowedBits(info.Mode().Perm(), stat.UID, stat.GID, identity)&required == required
		report.Checks = append(report.Checks, PathCheck{
			Path: component, Mode: fmt.Sprintf("%04o", info.Mode().Perm()), UID: stat.UID, GID: stat.GID,
			Required: requiredName, Allowed: allowed,
		})
		if !allowed {
			report.Readable = false
		}
	}
	if !report.Readable {
		blocked := make([]string, 0)
		for _, check := range report.Checks {
			if !check.Allowed {
				blocked = append(blocked, check.Path)
			}
		}
		return report, verrors.New(verrors.CodeDestinationNotReadable,
			fmt.Sprintf("user %q cannot traverse or read the destination", identity.Name)).
			WithDetails(map[string]any{"blocked_paths": blocked, "destination": cleaned, "user": identity.Name}).
			WithHint("Adjust a specific directory or group policy. Do not use chmod -R 777.")
	}
	return report, nil
}

func allowedBits(mode fs.FileMode, owner, group uint32, identity Identity) uint32 {
	permissions := uint32(mode.Perm())
	if identity.UID == owner {
		return (permissions >> 6) & 7
	}
	for _, gid := range identity.Groups {
		if gid == group {
			return (permissions >> 3) & 7
		}
	}
	return permissions & 7
}

func pathComponents(destination string) []string {
	if destination == "/" {
		return []string{"/"}
	}
	parts := strings.Split(strings.TrimPrefix(destination, "/"), "/")
	components := []string{"/"}
	current := ""
	for _, part := range parts {
		current = path.Join(current, "/", part)
		components = append(components, current)
	}
	return components
}
