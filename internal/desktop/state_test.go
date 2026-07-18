package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bashatahamal/vericopy/internal/remote"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestStateStoreSavesOnlyValidatedProfiles(t *testing.T) {
	store := newStateStore(filepath.Join(t.TempDir(), "desktop-state.json"))
	profile, err := store.SaveProfile(ConnectionProfile{
		Name: "Reports server", Destination: "transfer@files.example:/srv/shared/reports", Port: 2222,
		KnownHosts: "/tmp/known_hosts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID == "" || profile.UpdatedAt.IsZero() {
		t.Fatalf("profile metadata was not assigned: %#v", profile)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("desktop state is not user-only: mode=%v", info.Mode())
		}
	}
	profiles, err := store.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Destination != "transfer@files.example:/srv/shared/reports" || profiles[0].Port != 2222 {
		t.Fatalf("unexpected persisted profiles: %#v", profiles)
	}
	if _, err := store.SaveProfile(ConnectionProfile{Name: "Bad", Destination: "files.example:relative-path"}); err == nil || verrors.As(err).Code != verrors.CodeInvalidArguments {
		t.Fatalf("relative destination was accepted: %v", err)
	}
	removed, err := store.DeleteProfile(profile.ID)
	if err != nil || !removed {
		t.Fatalf("profile was not removed: removed=%t err=%v", removed, err)
	}
}

func TestHistoryIsRedactedAndUserControlled(t *testing.T) {
	store := newStateStore(filepath.Join(t.TempDir(), "desktop-state.json"))
	destination, err := remote.Parse("transfer@files.example:/srv/private/annual-report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	entry := TransferHistoryEntry{
		StartedAt: time.Now().UTC().Add(-time.Minute), CompletedAt: time.Now().UTC(), Status: "verified",
		SourceName: "annual-report.pdf", Destination: redactedDestination(destination), Bytes: 42, Verified: true,
	}
	if err := store.recordHistory(entry); err != nil {
		t.Fatal(err)
	}
	history, err := store.ListTransferHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || strings.Contains(history[0].Destination, "/srv/private") || history[0].Destination != "transfer@files.example:…/annual-report.pdf" {
		t.Fatalf("history was not redacted: %#v", history)
	}
	if err := store.ClearTransferHistory(); err != nil {
		t.Fatal(err)
	}
	history, err = store.ListTransferHistory()
	if err != nil || len(history) != 0 {
		t.Fatalf("history was not cleared: %#v err=%v", history, err)
	}
}
