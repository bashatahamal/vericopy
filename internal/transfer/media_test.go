package transfer_test

import (
	"bytes"
	"context"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/transfer"
)

func TestCopyDirectoryFixesCollidingYearAndGeneratesThumbnails(t *testing.T) {
	sourceDir := t.TempDir()
	names := []string{
		"The.East.Palace.2026.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.mkv",
		"The.East.Palace.2026.S01E02.1080p.NF.WEB-DL.x265.6CH-Pahe.in.mkv",
		"Yumis.Cells.S03E01.1080p.WEB-DL.x265.2CH-Pahe.in.mkv",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte("video bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	remoteFS := localRemote{root: t.TempDir()}
	policy, _ := permissions.Resolve("private", "", "")

	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), sourceDir, "/tv/east-palace", transfer.Options{
		Recursive: true, Policy: policy, FixMediaNames: true, GenerateThumbnails: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 3 {
		t.Fatalf("expected 3 files transferred, got %d", result.Files)
	}

	renamedFirst := filepath.Join(remoteFS.root, "tv", "east-palace", "The.East.Palace.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.mkv")
	if _, err := os.Stat(renamedFirst); err != nil {
		t.Fatalf("expected the colliding year to be removed, but %v", err)
	}
	if _, err := os.Stat(filepath.Join(remoteFS.root, "tv", "east-palace", "The.East.Palace.2026.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.mkv")); err == nil {
		t.Fatal("expected the original colliding filename not to exist")
	}

	unrenamed := filepath.Join(remoteFS.root, "tv", "east-palace", "Yumis.Cells.S03E01.1080p.WEB-DL.x265.2CH-Pahe.in.mkv")
	if _, err := os.Stat(unrenamed); err != nil {
		t.Fatalf("expected the already-unambiguous filename to be left alone, but %v", err)
	}

	posterPath := filepath.Join(remoteFS.root, "tv", "east-palace", "The.East.Palace.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.jpg")
	data, err := os.ReadFile(posterPath)
	if err != nil {
		t.Fatalf("expected a placeholder poster matching the episode's base name, but %v", err)
	}
	if _, err := jpeg.DecodeConfig(bytes.NewReader(data)); err != nil {
		t.Fatalf("thumbnail sidecar is not a valid JPEG: %v", err)
	}
}

func TestCopyLeavesFilenamesAloneWhenFixMediaNamesDisabled(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "The.East.Palace.2026.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.mkv")
	if err := os.WriteFile(sourceFile, []byte("video bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	policy, _ := permissions.Resolve("private", "", "")

	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), sourceFile, "/tv/episode.mkv", transfer.Options{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if result.Destination != "/tv/episode.mkv" {
		t.Fatalf("expected the explicit destination name to be respected, got %q", result.Destination)
	}
	if _, err := os.Stat(filepath.Join(remoteFS.root, "tv", "episode.jpg")); err == nil {
		t.Fatal("expected no thumbnail when GenerateThumbnails is disabled")
	}
}
