package rsync

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/bashatahamal/vericopy/internal/localpath"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

// Dialect describes the path model expected by an rsync executable.
type Dialect string

const (
	DialectUnix    Dialect = "unix"
	DialectCygwin  Dialect = "cygwin"
	DialectMINGW   Dialect = "mingw"
	DialectWindows Dialect = "windows-native"
	DialectUnknown Dialect = "unknown"
)

// Classify uses executable location and version output without executing a shell.
func Classify(executable, versionOutput string) Dialect {
	lowerPath := strings.ToLower(strings.ReplaceAll(filepath.ToSlash(executable), `\`, "/"))
	lowerVersion := strings.ToLower(versionOutput)
	switch {
	case strings.Contains(lowerPath, "/cygwin") || strings.Contains(lowerVersion, "cygwin"):
		return DialectCygwin
	case strings.Contains(lowerPath, "/msys") || strings.Contains(lowerPath, "/mingw") || strings.Contains(lowerVersion, "msys"):
		return DialectMINGW
	case runtime.GOOS != "windows" || strings.HasPrefix(executable, "/"):
		return DialectUnix
	case filepath.IsAbs(executable):
		return DialectWindows
	default:
		return DialectUnknown
	}
}

// Locate resolves and classifies rsync using a direct process invocation.
func Locate(ctx context.Context) (string, Dialect, error) {
	executable, err := exec.LookPath("rsync")
	if err != nil {
		return "", DialectUnknown, verrors.Wrap(verrors.CodeBackendUnavailable,
			"rsync was selected but no executable was found", err)
	}
	output, _ := exec.CommandContext(ctx, executable, "--version").CombinedOutput()
	return executable, Classify(executable, string(output)), nil
}

// Options controls safe argument construction.
type Options struct {
	Recursive    bool
	Resume       bool
	Overwrite    bool
	DryRun       bool
	PreserveTime bool
	Port         int
	Identity     string
	KnownHosts   string
}

// BuildArgs builds an argument vector. It never produces a shell command.
func BuildArgs(source, destination string, dialect Dialect, options Options) ([]string, error) {
	info, err := localpath.Inspect(source, "windows")
	if err != nil {
		return nil, err
	}
	if dialect == DialectCygwin && info.Kind == localpath.KindMINGW {
		return nil, verrors.New(verrors.CodeSourcePathDialectMismatch,
			"the selected rsync executable uses Cygwin paths, but the source uses Git Bash/MINGW syntax").
			WithHint("Convert the path with that Cygwin installation's cygpath command, then run a dry-run preflight.").
			WithDetails(map[string]any{"provided": source, "expected_dialect": "cygwin"})
	}
	if dialect == DialectMINGW && info.Kind == localpath.KindCygwin {
		return nil, verrors.New(verrors.CodeSourcePathDialectMismatch,
			"the selected rsync executable uses MINGW paths, but the source uses Cygwin syntax").
			WithHint("Use a /c/... path for this executable, then run a dry-run preflight.")
	}

	args := []string{"--protect-args", "--partial", "--human-readable"}
	if options.Recursive {
		args = append(args, "--recursive")
	}
	if options.Resume {
		args = append(args, "--append-verify")
	}
	if !options.Overwrite {
		args = append(args, "--ignore-existing")
	}
	if options.DryRun {
		args = append(args, "--dry-run")
	}
	if options.PreserveTime {
		args = append(args, "--times")
	}
	sshArgs := []string{"ssh", "-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes"}
	if options.Port > 0 {
		sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", options.Port))
	}
	if options.Identity != "" {
		sshArgs = append(sshArgs, "-i", options.Identity)
	}
	if options.KnownHosts != "" {
		sshArgs = append(sshArgs, "-o", "UserKnownHostsFile="+options.KnownHosts)
	}
	quotedSSHArgs := make([]string, len(sshArgs))
	for index, argument := range sshArgs {
		quotedSSHArgs[index] = quoteCommandArgument(argument)
	}
	args = append(args, "-e", strings.Join(quotedSSHArgs, " "))
	return append(args, "--", source, destination), nil
}

var safeCommandArgument = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// quoteCommandArgument protects the command string required by rsync's -e
// interface. Source and destination paths remain separate process arguments.
func quoteCommandArgument(value string) string {
	if safeCommandArgument.MatchString(value) {
		return value
	}
	return `'` + strings.ReplaceAll(value, `'`, `'"'"'`) + `'`
}

// Command returns an executable command with separate arguments.
func Command(ctx context.Context, executable string, args []string) *exec.Cmd {
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = os.Environ()
	return command
}
