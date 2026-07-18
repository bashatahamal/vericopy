package desktop

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestStateStoreSessionsRoundTripAndUpsertByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "desktop-state.json")
	store := newStateStore(statePath)
	want := SessionProfile{
		Name: "Monthly reports", Source: `C:\Users\me\Documents\report.zip`,
		Destination: "transfer@files.example:/srv/shared/report.zip", Port: 2222,
		Permissions: "shared", Identity: `C:\Users\me\.ssh\id_ed25519`,
		KnownHosts: `C:\Users\me\.ssh\known_hosts`, Group: "reporters", ReadableBy: "document-indexer",
		Recursive: true, Resume: true, Overwrite: true, PreserveTime: true,
	}
	saved, err := store.SaveSession(want)
	if err != nil {
		t.Fatal(err)
	}
	if saved.UpdatedAt.IsZero() {
		t.Fatal("session update time was not assigned")
	}
	want.UpdatedAt = saved.UpdatedAt
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved session differs:\n got: %#v\nwant: %#v", saved, want)
	}

	// A fresh store simulates a restarted app or cleared WebView storage. The
	// session must survive because it belongs to the Go state file.
	reloaded := newStateStore(statePath)
	sessions, err := reloaded.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || !reflect.DeepEqual(sessions[0], saved) {
		t.Fatalf("session did not survive a fresh store: %#v", sessions)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `C:\\Users\\me\\Documents\\report.zip`) ||
		!strings.Contains(string(data), `C:\\Users\\me\\.ssh\\id_ed25519`) {
		t.Fatalf("session paths were not persisted: %s", data)
	}

	replacement := want
	replacement.Port = 22
	replacement.Source = "/home/me/revised-report.zip"
	replacement.UpdatedAt = time.Time{}
	replaced, err := reloaded.SaveSession(replacement)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err = reloaded.ListSessions()
	if err != nil || len(sessions) != 1 || sessions[0].Source != replacement.Source || sessions[0].Port != 22 {
		t.Fatalf("session upsert failed: sessions=%#v err=%v", sessions, err)
	}
	if !replaced.UpdatedAt.After(saved.UpdatedAt) && !replaced.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatalf("session update time regressed: old=%v new=%v", saved.UpdatedAt, replaced.UpdatedAt)
	}

	removed, err := reloaded.DeleteSession(want.Name)
	if err != nil || !removed {
		t.Fatalf("session was not removed: removed=%t err=%v", removed, err)
	}
	removed, err = reloaded.DeleteSession(want.Name)
	if err != nil || removed {
		t.Fatalf("missing session delete was not idempotent: removed=%t err=%v", removed, err)
	}
}

func TestStateStoreRejectsInvalidSessions(t *testing.T) {
	valid := SessionProfile{
		Name: "Reports", Destination: "transfer@files.example:/srv/shared/reports",
		Port: 22, Permissions: "private", Resume: true,
	}
	tests := []struct {
		name   string
		mutate func(*SessionProfile)
		code   verrors.Code
	}{
		{name: "empty name", mutate: func(session *SessionProfile) { session.Name = " " }},
		{name: "long name", mutate: func(session *SessionProfile) { session.Name = strings.Repeat("a", 81) }},
		{name: "control in name", mutate: func(session *SessionProfile) { session.Name = "bad\nname" }},
		{name: "invalid port", mutate: func(session *SessionProfile) { session.Port = 70000 }},
		{name: "missing SSH user", mutate: func(session *SessionProfile) { session.Destination = "files.example:/srv/reports" }},
		{name: "relative destination", mutate: func(session *SessionProfile) { session.Destination = "transfer@files.example:reports" }},
		{name: "unknown policy", mutate: func(session *SessionProfile) { session.Permissions = "invented" }, code: verrors.CodeInvalidPermission},
		{name: "key contents instead of path", mutate: func(session *SessionProfile) { session.Identity = "-----BEGIN PRIVATE KEY-----\nsecret" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := valid
			test.mutate(&session)
			store := newStateStore(filepath.Join(t.TempDir(), "desktop-state.json"))
			wantCode := test.code
			if wantCode == "" {
				wantCode = verrors.CodeInvalidArguments
			}
			if _, err := store.SaveSession(session); err == nil || verrors.As(err).Code != wantCode {
				t.Fatalf("invalid session was accepted: %#v err=%v", session, err)
			}
		})
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
