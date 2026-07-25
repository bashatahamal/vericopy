package credentialstore_test

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/bashatahamal/vericopy/internal/credentialstore"
)

func TestMain(m *testing.M) {
	// A mock backend keeps this test from touching the real OS credential
	// store (and from failing in a headless CI environment with no keychain
	// service available at all).
	keyring.MockInit()
	m.Run()
}

func TestSaveLoadDeleteRoundTrip(t *testing.T) {
	if err := credentialstore.Save("home-server", "correct-horse-battery-staple"); err != nil {
		t.Fatal(err)
	}
	password, err := credentialstore.Load("home-server")
	if err != nil || password != "correct-horse-battery-staple" {
		t.Fatalf("Load() = %q, %v", password, err)
	}
	if err := credentialstore.Delete("home-server"); err != nil {
		t.Fatal(err)
	}
	if _, err := credentialstore.Load("home-server"); !errors.Is(err, credentialstore.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestLoadMissingSessionReturnsErrNotFound(t *testing.T) {
	if _, err := credentialstore.Load("never-saved"); !errors.Is(err, credentialstore.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSaveRejectsEmptySessionName(t *testing.T) {
	if err := credentialstore.Save("", "password"); err == nil {
		t.Fatal("expected an error for an empty session name")
	}
}

func TestDeleteMissingSessionIsNotAnError(t *testing.T) {
	if err := credentialstore.Delete("never-saved-either"); err != nil {
		t.Fatalf("deleting a never-saved entry should not error, got %v", err)
	}
}
