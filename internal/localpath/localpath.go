package localpath

import (
	"fmt"
	"path"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	"github.com/bashatahamal/vericopy/internal/verrors"
)

// Kind identifies the path dialect supplied by the user.
type Kind string

const (
	KindWindowsDrive Kind = "windows-drive"
	KindUNC          Kind = "windows-unc"
	KindMINGW        Kind = "mingw"
	KindCygwin       Kind = "cygwin"
	KindPOSIX        Kind = "posix"
	KindRelative     Kind = "relative"
)

var (
	windowsDrivePattern = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	mingwPattern        = regexp.MustCompile(`^/[A-Za-z](?:/|$)`)
	cygwinPattern       = regexp.MustCompile(`^/cygdrive/[A-Za-z](?:/|$)`)
)

// Info is a non-mutating inspection of a path string.
type Info struct {
	Original   string `json:"original"`
	Kind       Kind   `json:"kind"`
	Normalized string `json:"normalized"`
	TargetOS   string `json:"target_os"`
	Absolute   bool   `json:"absolute"`
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

// IsWindowsLike protects drive-letter and UNC paths from remote-spec parsing.
func IsWindowsLike(value string) bool {
	return windowsDrivePattern.MatchString(value) || strings.HasPrefix(value, `\\`) ||
		strings.HasPrefix(value, `//`) || mingwPattern.MatchString(value) || cygwinPattern.MatchString(value)
}

// Inspect classifies and normalizes a path for the requested operating system.
func Inspect(value, targetOS string) (Info, error) {
	if value == "" {
		return Info{}, verrors.New(verrors.CodeInvalidLocalPath, "the source path is empty")
	}
	if containsControl(value) {
		return Info{}, verrors.New(verrors.CodeInvalidLocalPath, "the source path contains control characters")
	}
	if strings.HasPrefix(value, `\\.\`) || strings.HasPrefix(value, `\\?\`) {
		return Info{}, verrors.New(verrors.CodeInvalidLocalPath, "Windows device and extended device paths are not supported")
	}
	if targetOS == "" {
		targetOS = runtime.GOOS
	}

	kind := KindRelative
	absolute := false
	switch {
	case windowsDrivePattern.MatchString(value):
		kind, absolute = KindWindowsDrive, true
	case strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, `//`):
		kind, absolute = KindUNC, true
	case cygwinPattern.MatchString(value):
		kind, absolute = KindCygwin, true
	case mingwPattern.MatchString(value):
		kind, absolute = KindMINGW, true
	case strings.HasPrefix(value, "/"):
		kind, absolute = KindPOSIX, true
	}

	normalized, err := normalize(value, kind, targetOS)
	if err != nil {
		return Info{}, err
	}
	return Info{Original: value, Kind: kind, Normalized: normalized, TargetOS: targetOS, Absolute: absolute}, nil
}

func normalize(value string, kind Kind, targetOS string) (string, error) {
	if targetOS == "windows" {
		switch kind {
		case KindMINGW:
			drive := strings.ToUpper(value[1:2])
			rest := strings.TrimPrefix(value[2:], "/")
			return drive + `:\` + strings.ReplaceAll(rest, "/", `\`), nil
		case KindCygwin:
			drive := strings.ToUpper(value[len("/cygdrive/") : len("/cygdrive/")+1])
			rest := strings.TrimPrefix(value[len("/cygdrive/")+1:], "/")
			return drive + `:\` + strings.ReplaceAll(rest, "/", `\`), nil
		case KindWindowsDrive, KindUNC:
			return strings.ReplaceAll(value, "/", `\`), nil
		case KindPOSIX:
			return "", verrors.New(verrors.CodeInvalidLocalPath, "a POSIX path cannot be mapped to Windows without a drive")
		default:
			return strings.ReplaceAll(value, "/", `\`), nil
		}
	}

	switch kind {
	case KindWindowsDrive, KindUNC, KindMINGW, KindCygwin:
		return value, nil
	default:
		// Inspect can validate a path dialect other than the operating system
		// running this binary. Use slash-based cleaning for POSIX paths so a
		// Windows runner does not rewrite /home/... as \\home\\....
		return path.Clean(value), nil
	}
}

// ResolveForRuntime returns a path that the current process can open.
func ResolveForRuntime(value string) (string, error) {
	info, err := Inspect(value, runtime.GOOS)
	if err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" && (info.Kind == KindWindowsDrive || info.Kind == KindUNC || info.Kind == KindMINGW || info.Kind == KindCygwin) {
		return "", verrors.New(verrors.CodeInvalidLocalPath,
			fmt.Sprintf("%s paths cannot be opened by this %s binary", info.Kind, runtime.GOOS)).WithHint(
			"Use a path native to this operating system, or run the Windows build.")
	}
	return info.Normalized, nil
}
