package localpath_test

import (
	"testing"

	"github.com/bashatahamal/vericopy/internal/localpath"
)

func TestInspectPathDialects(t *testing.T) {
	tests := []struct {
		name, input, target string
		kind                localpath.Kind
		want                string
	}{
		{"drive backslash", `C:\Users\Person\Downloads\HoTD`, "windows", localpath.KindWindowsDrive, `C:\Users\Person\Downloads\HoTD`},
		{"drive slash", `d:/Media/Film`, "windows", localpath.KindWindowsDrive, `d:\Media\Film`},
		{"mingw", `/c/Users/José/My Film.mkv`, "windows", localpath.KindMINGW, `C:\Users\José\My Film.mkv`},
		{"cygwin", `/cygdrive/e/動画/file.mkv`, "windows", localpath.KindCygwin, `E:\動画\file.mkv`},
		{"unc", `\\server\media\Film.mkv`, "windows", localpath.KindUNC, `\\server\media\Film.mkv`},
		{"posix", `/home/user/My Film.mkv`, "linux", localpath.KindPOSIX, `/home/user/My Film.mkv`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := localpath.Inspect(tt.input, tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tt.kind || got.Normalized != tt.want {
				t.Fatalf("got kind=%q path=%q, want kind=%q path=%q", got.Kind, got.Normalized, tt.kind, tt.want)
			}
		})
	}
}

func TestRejectDevicePath(t *testing.T) {
	if _, err := localpath.Inspect(`\\.\PhysicalDrive0`, "windows"); err == nil {
		t.Fatal("expected device path rejection")
	}
}
