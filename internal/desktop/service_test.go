package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bashatahamal/vericopy/internal/desktop"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestReviewTransferUsesExplicitDesktopContract(t *testing.T) {
	source := filepath.Join(t.TempDir(), "annual-report.pdf")
	if err := os.WriteFile(source, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	review, err := desktop.NewService().ReviewTransfer(desktop.TransferRequest{
		Source: source, Destination: "transfer@example.com:/srv/shared/annual-report.pdf", Permissions: "service-readonly", Resume: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if review.Destination.User != "transfer" || review.Destination.Port != 22 || review.Source.Size != 6 {
		t.Fatalf("unexpected review: %#v", review)
	}
	if review.Permissions != "service-readonly" || !review.Resume {
		t.Fatalf("policy was not retained: %#v", review)
	}
}

func TestReviewTransferRequiresExplicitRemoteUser(t *testing.T) {
	source := filepath.Join(t.TempDir(), "annual-report.pdf")
	if err := os.WriteFile(source, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := desktop.NewService().ReviewTransfer(desktop.TransferRequest{
		Source: source, Destination: "example.com:/srv/shared/annual-report.pdf",
	})
	if err == nil || verrors.As(err).Code != verrors.CodeInvalidArguments {
		t.Fatalf("missing explicit user was accepted: %v", err)
	}
}

func TestReviewTransferRequiresAbsoluteRemotePath(t *testing.T) {
	source := filepath.Join(t.TempDir(), "annual-report.pdf")
	if err := os.WriteFile(source, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := desktop.NewService().ReviewTransfer(desktop.TransferRequest{
		Source: source, Destination: "transfer@example.com:relative-report.pdf",
	})
	if err == nil || verrors.As(err).Code != verrors.CodeInvalidArguments {
		t.Fatalf("relative remote path was accepted: %v", err)
	}
}

func TestReviewTransferRequiresRecursiveForDirectory(t *testing.T) {
	source := t.TempDir()
	_, err := desktop.NewService().ReviewTransfer(desktop.TransferRequest{
		Source: source, Destination: "transfer@example.com:/srv/shared/reports",
	})
	if err == nil || verrors.As(err).Code != verrors.CodeInvalidArguments {
		t.Fatalf("directory without recursive option was accepted: %v", err)
	}
}

func TestServicePersistsSessionsOutsideWebViewState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "desktop-state.json")
	service := desktop.NewServiceWithStatePath(statePath)
	saved, err := service.SaveSession(desktop.SessionProfile{
		Name: "Reports", Source: `C:\Users\me\Documents\report.zip`,
		Destination: "transfer@example.com:/srv/shared/report.zip", Port: 22,
		Permissions: "private", Identity: `C:\Users\me\.ssh\id_ed25519`, Resume: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	reloaded := desktop.NewServiceWithStatePath(statePath)
	sessions, err := reloaded.ListSessions()
	if err != nil || len(sessions) != 1 || sessions[0].Name != saved.Name || sessions[0].Identity != saved.Identity {
		t.Fatalf("service session round-trip failed: sessions=%#v err=%v", sessions, err)
	}
	removed, err := reloaded.DeleteSession(saved.Name)
	if err != nil || !removed {
		t.Fatalf("service session delete failed: removed=%t err=%v", removed, err)
	}
}
