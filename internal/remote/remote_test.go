package remote_test

import (
	"testing"

	"github.com/bashatahamal/vericopy/internal/remote"
)

func TestParseDestination(t *testing.T) {
	tests := []struct {
		input, user, host, path string
	}{
		{`user@example.com:/shared/quarterly-report.zip`, "user", "example.com", "/shared/quarterly-report.zip"},
		{`server:relative/file`, "", "server", "relative/file"},
		{`user@[2001:db8::1]:/srv/動画`, "user", "2001:db8::1", "/srv/動画"},
	}
	for _, tt := range tests {
		got, err := remote.Parse(tt.input)
		if err != nil {
			t.Fatalf("parse %q: %v", tt.input, err)
		}
		if got.User != tt.user || got.Host != tt.host || got.Path != tt.path {
			t.Fatalf("parse %q = %#v", tt.input, got)
		}
	}
}

func TestWindowsDriveIsNotRemote(t *testing.T) {
	if _, err := remote.Parse(`C:\Users\person\Documents\annual-report.pdf`); err == nil {
		t.Fatal("expected Windows path rejection")
	}
}

func TestRejectTraversal(t *testing.T) {
	if _, err := remote.Parse(`host:/srv/../etc/passwd`); err == nil {
		t.Fatal("expected traversal rejection")
	}
}
