package rsync_test

import (
	"strings"
	"testing"

	"github.com/bashatahamal/vericopy/internal/backend/rsync"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		path, version string
		want          rsync.Dialect
	}{
		{`C:\cygwin64\bin\rsync.exe`, "rsync version 3.2.7 protocol version 31", rsync.DialectCygwin},
		{`C:\Program Files\Git\usr\bin\rsync.exe`, "rsync (MSYS2)", rsync.DialectMINGW},
		{"/usr/bin/rsync", "rsync version 3.2.7", rsync.DialectUnix},
	}
	for _, tt := range tests {
		if got := rsync.Classify(tt.path, tt.version); got != tt.want {
			t.Fatalf("Classify(%q)=%q want %q", tt.path, got, tt.want)
		}
	}
}

func TestBuildArgsRemainSeparate(t *testing.T) {
	args, err := rsync.BuildArgs(`C:\Documents\quarterly-report.zip`, `user@host:/srv/shared/quarterly-report.zip`, rsync.DialectWindows, rsync.Options{Port: 2222, Identity: `C:\Keys\Person's Key`})
	if err != nil {
		t.Fatal(err)
	}
	if args[len(args)-2] != `C:\Documents\quarterly-report.zip` || args[len(args)-1] != `user@host:/srv/shared/quarterly-report.zip` {
		t.Fatalf("paths were not preserved as arguments: %#v", args)
	}
	for _, arg := range args {
		if strings.Contains(arg, `"C:\Documents`) {
			t.Fatalf("source was shell-quoted: %q", arg)
		}
	}
	transport := args[len(args)-4]
	if strings.Contains(transport, `Person's Key`) || !strings.Contains(transport, `Person'"'"'s Key`) {
		t.Fatalf("identity was not safely quoted in rsync transport: %q", transport)
	}
}

func TestDialectMismatch(t *testing.T) {
	_, err := rsync.BuildArgs(`/c/Users/person/Documents/annual-report.pdf`, `host:/srv/shared/annual-report.pdf`, rsync.DialectCygwin, rsync.Options{})
	if err == nil {
		t.Fatal("expected mismatch")
	}
	if verrors.As(err).Code != verrors.CodeSourcePathDialectMismatch {
		t.Fatalf("unexpected code: %v", err)
	}
}
