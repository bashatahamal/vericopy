package desktop

// path (not path/filepath) is deliberate: the default branch below always
// handles a POSIX-style path from a Linux or BSD desktop, regardless of
// which OS this binary itself was built for, so it must not defer to the
// build host's own separator convention.
import "path"

// RevealCommand returns the OS command and arguments that open a file
// manager with path selected, for the given GOOS. Windows Explorer and
// macOS Finder both support selecting a specific file; there is no portable
// equivalent on Linux desktops, so everything else falls back to opening
// the file's containing folder.
func RevealCommand(goos, filePath string) (name string, args []string) {
	switch goos {
	case "windows":
		return "explorer", []string{"/select," + filePath}
	case "darwin":
		return "open", []string{"-R", filePath}
	default:
		return "xdg-open", []string{path.Dir(filePath)}
	}
}
