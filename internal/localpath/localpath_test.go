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
		{"drive slash", `d:/Documents/QuarterlyReport`, "windows", localpath.KindWindowsDrive, `d:\Documents\QuarterlyReport`},
		{"mingw", `/c/Users/José/Documents/QuarterlyReport.zip`, "windows", localpath.KindMINGW, `C:\Users\José\Documents\QuarterlyReport.zip`},
		{"cygwin", `/cygdrive/e/資料/annual-report.pdf`, "windows", localpath.KindCygwin, `E:\資料\annual-report.pdf`},
		{"unc", `\\server\shared\annual-report.pdf`, "windows", localpath.KindUNC, `\\server\shared\annual-report.pdf`},
		{"posix", `/home/user/Documents/annual-report.pdf`, "linux", localpath.KindPOSIX, `/home/user/Documents/annual-report.pdf`},
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
