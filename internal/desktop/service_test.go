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
