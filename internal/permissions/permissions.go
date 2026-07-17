package permissions

import (
	"fmt"
	"io/fs"
	"strconv"

	"github.com/bashatahamal/vericopy/internal/verrors"
)

// Policy is an explicit destination permission contract.
type Policy struct {
	Name      string      `json:"name"`
	Directory fs.FileMode `json:"directory_mode"`
	File      fs.FileMode `json:"file_mode"`
	Preserve  bool        `json:"preserve"`
}

var presets = map[string]Policy{
	"private":         {Name: "private", Directory: 0o700, File: 0o600},
	"shared":          {Name: "shared", Directory: fs.ModeSetgid | 0o770, File: 0o660},
	"media-readonly":  {Name: "media-readonly", Directory: fs.ModeSetgid | 0o750, File: 0o640},
	"public-readonly": {Name: "public-readonly", Directory: 0o755, File: 0o644},
	"preserve":        {Name: "preserve", Preserve: true},
}

// Resolve returns a validated preset with optional octal overrides.
func Resolve(name, fileMode, directoryMode string) (Policy, error) {
	if name == "" {
		name = "private"
	}
	policy, ok := presets[name]
	if !ok {
		return Policy{}, verrors.New(verrors.CodeInvalidPermission,
			fmt.Sprintf("unknown permission policy %q", name)).WithHint(
			"Choose private, shared, media-readonly, public-readonly, or preserve.")
	}
	var err error
	if fileMode != "" {
		policy.File, err = parseMode(fileMode, false)
		if err != nil {
			return Policy{}, err
		}
		policy.Preserve = false
	}
	if directoryMode != "" {
		policy.Directory, err = parseMode(directoryMode, true)
		if err != nil {
			return Policy{}, err
		}
		policy.Preserve = false
	}
	return policy, nil
}

func parseMode(value string, directory bool) (fs.FileMode, error) {
	if len(value) < 3 || len(value) > 4 {
		return 0, invalidMode(value)
	}
	parsed, err := strconv.ParseUint(value, 8, 12)
	if err != nil || parsed > 0o7777 {
		return 0, invalidMode(value)
	}
	if !directory && parsed > 0o777 {
		return 0, verrors.New(verrors.CodeInvalidPermission,
			"special permission bits are not allowed on transferred files")
	}
	mode := fs.FileMode(parsed & 0o777)
	if parsed&0o4000 != 0 {
		mode |= fs.ModeSetuid
	}
	if parsed&0o2000 != 0 {
		mode |= fs.ModeSetgid
	}
	if parsed&0o1000 != 0 {
		mode |= fs.ModeSticky
	}
	return mode, nil
}

func invalidMode(value string) error {
	return verrors.New(verrors.CodeInvalidPermission,
		fmt.Sprintf("%q is not a valid octal permission mode", value)).WithHint(
		"Use three or four octal digits such as 640, 0750, or 2770.")
}

// Octal renders POSIX special bits and permission bits.
func Octal(mode fs.FileMode) string {
	value := uint32(mode.Perm())
	if mode&fs.ModeSetuid != 0 {
		value |= 0o4000
	}
	if mode&fs.ModeSetgid != 0 {
		value |= 0o2000
	}
	if mode&fs.ModeSticky != 0 {
		value |= 0o1000
	}
	return fmt.Sprintf("%04o", value)
}
