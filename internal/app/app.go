package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bashatahamal/vericopy/internal/access"
	"github.com/bashatahamal/vericopy/internal/backend/rsync"
	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/localpath"
	"github.com/bashatahamal/vericopy/internal/output"
	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/remote"
	"github.com/bashatahamal/vericopy/internal/remotehash"
	"github.com/bashatahamal/vericopy/internal/sshclient"
	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
	"github.com/bashatahamal/vericopy/internal/version"
	"github.com/bashatahamal/vericopy/internal/wakelock"
)

// Globals contains output controls shared by every command.
type Globals struct {
	JSON    bool
	Quiet   bool
	NoColor bool
	Out     io.Writer
	Err     io.Writer
}

func (g *Globals) printer() output.Printer {
	return output.Printer{Out: g.Out, Err: g.Err, JSON: g.JSON, Quiet: g.Quiet}
}

// NewRoot creates the complete command tree.
func NewRoot(out, errOut io.Writer) (*cobra.Command, *Globals) {
	globals := &Globals{Out: out, Err: errOut}
	root := &cobra.Command{
		Use:           "vericopy",
		Short:         "Secure, resumable file transfer over SSH",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return verrors.Wrap(verrors.CodeInvalidArguments, err.Error(), err)
	})
	root.SetOut(out)
	root.SetErr(errOut)
	root.PersistentFlags().BoolVar(&globals.JSON, "json", false, "write stable JSON without progress animation")
	root.PersistentFlags().BoolVarP(&globals.Quiet, "quiet", "q", false, "suppress non-error human output")
	root.PersistentFlags().BoolVar(&globals.NoColor, "no-color", false, "disable ANSI color output")
	root.AddCommand(
		newVersionCommand(globals),
		newInspectPathCommand(globals),
		newDoctorCommand(globals),
		newCopyCommand(globals),
		newVerifyCommand(globals),
		newCheckAccessCommand(globals),
	)
	return root, globals
}

type connectionFlags struct {
	Identity   string
	Port       int
	KnownHosts string
}

func addConnectionFlags(command *cobra.Command, flags *connectionFlags) {
	command.Flags().StringVar(&flags.Identity, "identity", "", "private-key path (SSH agent is preferred)")
	command.Flags().IntVarP(&flags.Port, "port", "p", 22, "SSH port")
	command.Flags().StringVar(&flags.KnownHosts, "known-hosts", sshclient.DefaultKnownHosts(), "strict known_hosts file")
}

func newVersionCommand(globals *Globals) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show build identity",
		Args:  noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := version.Current()
			human := fmt.Sprintf("vericopy %s\ncommit: %s\nbuilt: %s\ngo: %s %s/%s",
				info.Version, info.Commit, info.BuildDate, info.GoVersion, info.OS, info.Arch)
			return globals.printer().Success(human, info)
		},
	}
}

func newInspectPathCommand(globals *Globals) *cobra.Command {
	var targetOS string
	command := &cobra.Command{
		Use:   "inspect-path PATH",
		Short: "Classify a local path without transferring it",
		Args:  exactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			info, err := localpath.Inspect(args[0], targetOS)
			if err != nil {
				return err
			}
			human := fmt.Sprintf("kind: %s\nnormalized: %s\ntarget OS: %s", info.Kind, info.Normalized, info.TargetOS)
			return globals.printer().Success(human, info)
		},
	}
	command.Flags().StringVar(&targetOS, "target-os", runtime.GOOS, "normalization target: windows, linux, or darwin")
	return command
}

type doctorResult struct {
	Ready  bool          `json:"ready"`
	Checks []doctorCheck `json:"checks"`
}

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func newDoctorCommand(globals *Globals) *cobra.Command {
	var knownHosts string
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check local transfer prerequisites",
		Args:  noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			result := doctorResult{Ready: true}
			add := func(name, status, message string) {
				result.Checks = append(result.Checks, doctorCheck{Name: name, Status: status, Message: message})
				if status == "error" {
					result.Ready = false
				}
			}
			if _, err := sshclient.NewHostKeyCallback(knownHosts); err != nil {
				add("known_hosts", "error", verrors.As(err).Message)
			} else {
				add("known_hosts", "ok", knownHosts)
			}
			if socket := os.Getenv("SSH_AUTH_SOCK"); socket == "" {
				add("ssh-agent", "warning", "SSH_AUTH_SOCK is not set; use --identity or start an agent")
			} else {
				add("ssh-agent", "ok", "agent socket is configured")
			}
			if executable, err := exec.LookPath("rsync"); err != nil {
				add("rsync", "optional", "not installed; native SFTP remains available")
			} else {
				add("rsync", "ok", executable)
			}
			add("runtime", "ok", fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH))

			var builder strings.Builder
			if result.Ready {
				builder.WriteString("Vericopy is ready for strict SSH transfers.\n")
			} else {
				builder.WriteString("Vericopy needs attention before connecting.\n")
			}
			for _, check := range result.Checks {
				fmt.Fprintf(&builder, "%-12s %-8s %s\n", check.Name, check.Status, check.Message)
			}
			return globals.printer().Success(strings.TrimSpace(builder.String()), result)
		},
	}
	command.Flags().StringVar(&knownHosts, "known-hosts", sshclient.DefaultKnownHosts(), "known_hosts file to validate")
	return command
}

type copyFlags struct {
	connectionFlags
	Recursive     bool
	Resume        bool
	Overwrite     bool
	NoClobber     bool
	PreserveTime  bool
	Permission    string
	FileMode      string
	DirectoryMode string
	Group         string
	ReadableBy    string
	Backend       string
	Verify        string
	DryRun        bool
}

func newCopyCommand(globals *Globals) *cobra.Command {
	flags := &copyFlags{}
	command := &cobra.Command{
		Use:   "copy SOURCE DESTINATION",
		Short: "Copy and verify a file or directory over SSH",
		Args:  exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runCopy(command.Context(), globals, flags, args[0], args[1])
		},
	}
	addConnectionFlags(command, &flags.connectionFlags)
	command.Flags().BoolVarP(&flags.Recursive, "recursive", "r", false, "copy a directory tree")
	command.Flags().BoolVar(&flags.Resume, "resume", false, "resume only compatible partial state")
	command.Flags().BoolVar(&flags.Overwrite, "overwrite", false, "replace an existing destination")
	command.Flags().BoolVar(&flags.NoClobber, "no-clobber", false, "skip files and directories that already exist at the destination instead of failing")
	command.Flags().BoolVar(&flags.PreserveTime, "preserve-time", false, "preserve source modification times where supported")
	command.Flags().StringVar(&flags.Permission, "permissions", "private", "destination permission policy")
	command.Flags().StringVar(&flags.FileMode, "file-mode", "", "octal file-mode override")
	command.Flags().StringVar(&flags.DirectoryMode, "directory-mode", "", "octal directory-mode override")
	command.Flags().StringVar(&flags.Group, "group", "", "remote group name to apply without escalation")
	command.Flags().StringVar(&flags.ReadableBy, "readable-by", "", "verify final access for a remote service user")
	command.Flags().StringVar(&flags.Backend, "backend", "sftp", "transfer backend: sftp, rsync, or auto")
	command.Flags().StringVar(&flags.Verify, "verify", "sha256", "verification algorithm (sha256)")
	command.Flags().BoolVar(&flags.DryRun, "dry-run", false, "validate and describe work without writing")
	return command
}

func runCopy(ctx context.Context, globals *Globals, flags *copyFlags, sourceArg, destinationArg string) error {
	if flags.Verify != "sha256" {
		return verrors.New(verrors.CodeInvalidArguments, "only --verify sha256 is supported")
	}
	if flags.Backend != "sftp" && flags.Backend != "auto" && flags.Backend != "rsync" {
		return verrors.New(verrors.CodeInvalidArguments, "--backend must be sftp, rsync, or auto")
	}
	if flags.Port < 1 || flags.Port > 65535 {
		return verrors.New(verrors.CodeInvalidArguments, "SSH port must be between 1 and 65535")
	}
	if flags.Overwrite && flags.NoClobber {
		return verrors.New(verrors.CodeInvalidArguments, "--overwrite and --no-clobber cannot be used together")
	}
	policy, err := permissions.Resolve(flags.Permission, flags.FileMode, flags.DirectoryMode)
	if err != nil {
		return err
	}
	destination, err := remote.Parse(destinationArg)
	if err != nil {
		return err
	}
	if destination.User == "" {
		destination.User, err = currentUsername()
		if err != nil {
			return err
		}
	}
	if flags.Backend == "rsync" {
		return runRsync(ctx, globals, flags, policy, sourceArg, destinationArg, destination)
	}
	source, err := localpath.ResolveForRuntime(sourceArg)
	if err != nil {
		return err
	}
	sshConnection, remoteFS, err := connect(ctx, destination, flags.connectionFlags)
	if err != nil {
		return err
	}
	defer sshConnection.Close()
	defer remoteFS.Close()

	var gid *int
	resolver := access.Resolver{Runner: access.SSHRunner{Client: sshConnection.Client}}
	if flags.Group != "" {
		resolved, resolveErr := resolver.Group(ctx, flags.Group)
		if resolveErr != nil {
			return resolveErr
		}
		gid = &resolved
	}
	engine := transfer.Engine{Remote: remoteFS, Hasher: remotehash.Hasher{Runner: access.SSHRunner{Client: sshConnection.Client}}}
	defer wakelock.Acquire("Vericopy transfer in progress")()
	result, err := engine.Copy(ctx, source, destination.Path, transfer.Options{
		Recursive: flags.Recursive, Resume: flags.Resume, Overwrite: flags.Overwrite,
		NoClobber: flags.NoClobber, PreserveTime: flags.PreserveTime, DryRun: flags.DryRun,
		Policy: policy, GID: gid,
	})
	if err != nil {
		return err
	}
	if flags.ReadableBy != "" && !flags.DryRun {
		identity, resolveErr := resolver.User(ctx, flags.ReadableBy)
		if resolveErr != nil {
			return resolveErr
		}
		if _, checkErr := access.Check(ctx, remoteFS, result.Destination, identity); checkErr != nil {
			return checkErr
		}
	}
	human := fmt.Sprintf("Transferred %d file(s), %d bytes to %s\nSHA-256: %s", result.Files, result.Bytes, result.Destination, result.SHA256)
	if result.SkippedFiles > 0 {
		human += fmt.Sprintf("\nSkipped %d file(s) that already existed", result.SkippedFiles)
	}
	if result.DryRun {
		human = fmt.Sprintf("Dry run: %d file(s), %d bytes would be transferred to %s", result.Files, result.Bytes, result.Destination)
	}
	return globals.printer().Success(human, result)
}

func runRsync(ctx context.Context, globals *Globals, flags *copyFlags, policy permissions.Policy, sourceArg, destinationArg string, destination remote.Destination) error {
	if flags.Recursive {
		return verrors.New(verrors.CodeBackendUnavailable,
			"recursive transfers are not supported by the optional rsync adapter in this release").
			WithHint("Use the native SFTP backend for recursive verified transfers.")
	}
	source, err := localpath.ResolveForRuntime(sourceArg)
	if err != nil {
		return err
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return verrors.Wrap(verrors.CodeInvalidLocalPath, "the rsync source could not be inspected", err)
	}
	if sourceInfo.Mode()&fs.ModeSymlink != 0 || !sourceInfo.Mode().IsRegular() {
		return verrors.New(verrors.CodeUnsupportedFileType, "the rsync adapter accepts regular files only in this release")
	}
	sshConnection, remoteFS, err := connect(ctx, destination, flags.connectionFlags)
	if err != nil {
		return err
	}
	defer sshConnection.Close()
	defer remoteFS.Close()
	defer wakelock.Acquire("Vericopy transfer in progress")()
	finalDestination := destination.Path
	if existing, statErr := remoteFS.Lstat(finalDestination); statErr == nil {
		if existing.Mode()&fs.ModeSymlink != 0 {
			return verrors.New(verrors.CodeUnsupportedFileType, "the rsync destination is a symbolic link")
		}
		if existing.IsDir() {
			finalDestination = path.Join(finalDestination, filepath.Base(source))
			existing, statErr = remoteFS.Lstat(finalDestination)
			if statErr == nil && existing.Mode()&fs.ModeSymlink != 0 {
				return verrors.New(verrors.CodeUnsupportedFileType, "the rsync destination is a symbolic link")
			}
		}
		if statErr == nil && !flags.Overwrite {
			return verrors.New(verrors.CodeDestinationExists,
				fmt.Sprintf("destination %q already exists", finalDestination))
		}
		if statErr != nil && !remoteNotExist(statErr) {
			return verrors.Wrap(verrors.CodeDestinationNotWritable, "the rsync destination could not be inspected", statErr)
		}
	} else if !remoteNotExist(statErr) {
		return verrors.Wrap(verrors.CodeDestinationNotWritable, "the rsync destination could not be inspected", statErr)
	}

	executable, dialect, err := rsync.Locate(ctx)
	if err != nil {
		return err
	}
	args, err := rsync.BuildArgs(sourceArg, destinationArg, dialect, rsync.Options{
		Recursive: flags.Recursive, Resume: flags.Resume, Overwrite: flags.Overwrite,
		DryRun: flags.DryRun, PreserveTime: flags.PreserveTime, Port: flags.Port,
		Identity: flags.Identity, KnownHosts: flags.KnownHosts,
	})
	if err != nil {
		return err
	}
	if globals.JSON {
		command := rsync.Command(ctx, executable, args)
		combined, runErr := command.CombinedOutput()
		if runErr != nil {
			return verrors.Wrap(verrors.CodeTransferFailed, "rsync transfer failed: "+strings.TrimSpace(string(combined)), runErr)
		}
	} else {
		command := rsync.Command(ctx, executable, args)
		command.Stdout, command.Stderr = globals.Out, globals.Err
		if err := command.Run(); err != nil {
			return verrors.Wrap(verrors.CodeTransferFailed, "rsync transfer failed", err)
		}
	}
	if flags.DryRun {
		result := transfer.Result{Source: source, Destination: finalDestination, Files: 1, Bytes: sourceInfo.Size(), DryRun: true}
		return globals.printer().Success(
			fmt.Sprintf("Dry run: rsync would transfer %d bytes to %s", sourceInfo.Size(), finalDestination), result)
	}
	result, err := (transfer.Engine{Remote: remoteFS, Hasher: remotehash.Hasher{Runner: access.SSHRunner{Client: sshConnection.Client}}}).Verify(ctx, source, finalDestination)
	if err != nil {
		return err
	}
	resolver := access.Resolver{Runner: access.SSHRunner{Client: sshConnection.Client}}
	if flags.Group != "" {
		gid, resolveErr := resolver.Group(ctx, flags.Group)
		if resolveErr != nil {
			return resolveErr
		}
		if err := remoteFS.Chown(finalDestination, -1, gid); err != nil {
			return verrors.Wrap(verrors.CodeGroupUnavailable, "could not apply the requested group", err)
		}
	}
	mode := policy.File
	if policy.Preserve {
		mode = sourceInfo.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
	}
	if err := remoteFS.Chmod(finalDestination, mode); err != nil {
		return verrors.Wrap(verrors.CodeInvalidPermission, "could not apply the file permission policy", err)
	}
	if flags.PreserveTime {
		if err := remoteFS.Chtimes(finalDestination, sourceInfo.ModTime(), sourceInfo.ModTime()); err != nil {
			return verrors.Wrap(verrors.CodeInvalidPermission, "could not preserve the source modification time", err)
		}
	}
	if flags.ReadableBy != "" {
		identity, resolveErr := resolver.User(ctx, flags.ReadableBy)
		if resolveErr != nil {
			return resolveErr
		}
		if _, checkErr := access.Check(ctx, remoteFS, finalDestination, identity); checkErr != nil {
			return checkErr
		}
	}
	return globals.printer().Success(
		fmt.Sprintf("Transferred and verified %d bytes to %s\nSHA-256: %s", result.Bytes, finalDestination, result.SHA256), result)
}

func remoteNotExist(err error) bool {
	return err != nil && (errors.Is(err, fs.ErrNotExist) || strings.Contains(strings.ToLower(err.Error()), "no such file"))
}

func newVerifyCommand(globals *Globals) *cobra.Command {
	flags := connectionFlags{}
	command := &cobra.Command{
		Use:   "verify SOURCE DESTINATION",
		Short: "Compare local and remote size and SHA-256",
		Args:  exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			source, err := localpath.ResolveForRuntime(args[0])
			if err != nil {
				return err
			}
			destination, err := remote.Parse(args[1])
			if err != nil {
				return err
			}
			if destination.User == "" {
				destination.User, err = currentUsername()
				if err != nil {
					return err
				}
			}
			sshConnection, remoteFS, err := connect(command.Context(), destination, flags)
			if err != nil {
				return err
			}
			defer sshConnection.Close()
			defer remoteFS.Close()
			engine := transfer.Engine{Remote: remoteFS, Hasher: remotehash.Hasher{Runner: access.SSHRunner{Client: sshConnection.Client}}}
			result, err := engine.Verify(command.Context(), source, destination.Path)
			if err != nil {
				return err
			}
			return globals.printer().Success(fmt.Sprintf("Verified %d bytes\nSHA-256: %s", result.Bytes, result.SHA256), result)
		},
	}
	addConnectionFlags(command, &flags)
	return command
}

func newCheckAccessCommand(globals *Globals) *cobra.Command {
	flags := connectionFlags{}
	var asUser string
	command := &cobra.Command{
		Use:   "check-access DESTINATION --as-user USER",
		Short: "Check remote traversal and read access without changing permissions",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if asUser == "" {
				return verrors.New(verrors.CodeInvalidArguments, "--as-user is required")
			}
			destination, err := remote.Parse(args[0])
			if err != nil {
				return err
			}
			if destination.User == "" {
				destination.User, err = currentUsername()
				if err != nil {
					return err
				}
			}
			sshConnection, remoteFS, err := connect(command.Context(), destination, flags)
			if err != nil {
				return err
			}
			defer sshConnection.Close()
			defer remoteFS.Close()
			resolver := access.Resolver{Runner: access.SSHRunner{Client: sshConnection.Client}}
			identity, err := resolver.User(command.Context(), asUser)
			if err != nil {
				return err
			}
			report, err := access.Check(command.Context(), remoteFS, destination.Path, identity)
			if err != nil {
				return err
			}
			return globals.printer().Success(fmt.Sprintf("User %q can traverse and read %s", asUser, destination.Path), report)
		},
	}
	addConnectionFlags(command, &flags)
	command.Flags().StringVar(&asUser, "as-user", "", "remote service account to inspect")
	return command
}

func connect(ctx context.Context, destination remote.Destination, flags connectionFlags) (*sshclient.Client, *nativesftp.Client, error) {
	sshConnection, err := sshclient.Dial(ctx, sshclient.Options{
		User: destination.User, Host: destination.Host, Port: flags.Port,
		KnownHosts: flags.KnownHosts, Identity: flags.Identity, Timeout: 15 * time.Second,
	})
	if err != nil {
		return nil, nil, err
	}
	remoteFS, err := nativesftp.New(sshConnection)
	if err != nil {
		_ = sshConnection.Close()
		return nil, nil, verrors.Wrap(verrors.CodeConnectionFailed, "the server did not accept the SFTP subsystem", err)
	}
	return sshConnection, remoteFS, nil
}

func currentUsername() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", verrors.Wrap(verrors.CodeInvalidArguments, "the current SSH user could not be inferred", err)
	}
	name := current.Username
	if strings.Contains(name, `\`) {
		parts := strings.Split(name, `\`)
		name = parts[len(parts)-1]
	}
	if err := access.ValidateAccountName(name); err != nil {
		return "", verrors.New(verrors.CodeInvalidArguments,
			"the current user name cannot be used as an SSH user; include user@ in the destination")
	}
	return name, nil
}

func exactArgs(count int) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(count)(command, args); err != nil {
			return verrors.Wrap(verrors.CodeInvalidArguments, err.Error(), err)
		}
		return nil
	}
}

func noArgs(command *cobra.Command, args []string) error {
	if err := cobra.NoArgs(command, args); err != nil {
		return verrors.Wrap(verrors.CodeInvalidArguments, err.Error(), err)
	}
	return nil
}
