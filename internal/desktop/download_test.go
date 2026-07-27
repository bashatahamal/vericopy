package desktop

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestPrepareDownloadRequiresRemoteSourceAndLocalDestination(t *testing.T) {
	destination := t.TempDir()
	prepared, err := prepare(TransferRequest{
		Direction: transferDirectionDownload, Source: "transfer@example.com:/data/movie.mkv", Destination: destination,
		Authentication: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.remote.User != "transfer" || prepared.remote.Host != "example.com" || prepared.remote.Path != "/data/movie.mkv" {
		t.Fatalf("unexpected remote endpoint: %#v", prepared.remote)
	}
	if prepared.local != destination {
		t.Fatalf("expected local destination %q, got %q", destination, prepared.local)
	}
}

func TestPrepareDownloadRejectsSourceWithoutUser(t *testing.T) {
	_, err := prepare(TransferRequest{
		Direction: transferDirectionDownload, Source: "example.com:/data/movie.mkv", Destination: t.TempDir(),
	})
	if err == nil || verrors.As(err).Code != verrors.CodeInvalidArguments {
		t.Fatalf("expected a missing-user error, got: %v", err)
	}
}

func TestPrepareDownloadRejectsRelativeRemotePath(t *testing.T) {
	_, err := prepare(TransferRequest{
		Direction: transferDirectionDownload, Source: "transfer@example.com:relative/movie.mkv", Destination: t.TempDir(),
	})
	if err == nil || verrors.As(err).Code != verrors.CodeInvalidArguments {
		t.Fatalf("expected a relative-path error, got: %v", err)
	}
}

func TestEnqueueDownloadTransferUsesEngineDownloadDirection(t *testing.T) {
	service := NewServiceWithStatePath(filepath.Join(t.TempDir(), "desktop-state.json"))
	t.Cleanup(service.Close)
	destination := t.TempDir()

	var capturedSource, capturedDestination string
	done := make(chan struct{})
	service.executor = func(ctx context.Context, prepared preparedTransfer, _ string, progress func(transfer.Progress)) (transfer.Result, error) {
		capturedSource, capturedDestination = prepared.remote.Path, prepared.local
		if prepared.request.Direction != transferDirectionDownload {
			t.Errorf("expected direction %q, got %q", transferDirectionDownload, prepared.request.Direction)
		}
		close(done)
		return transfer.Result{Files: 1, Bytes: 6, Verified: true}, nil
	}

	job, err := service.EnqueueTransfer(TransferRequest{
		Direction: transferDirectionDownload, Source: "transfer@example.com:/data/movie.mkv", Destination: destination,
		Authentication: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the download job never reached the executor")
	}
	if capturedSource != "/data/movie.mkv" || capturedDestination != destination {
		t.Fatalf("executor did not receive the expected remote source / local destination: source=%q destination=%q", capturedSource, capturedDestination)
	}

	stored, err := service.GetTransferJobRequest(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Direction != transferDirectionDownload {
		t.Fatalf("expected the persisted request to keep direction=download, got %q", stored.Direction)
	}
	if stored.Source != "transfer@example.com:/data/movie.mkv" || stored.Destination != destination {
		t.Fatalf("unexpected persisted request: %#v", stored)
	}
}
