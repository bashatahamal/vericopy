package sshclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// TestPreferredHostKeyAlgorithmsMatchesRecordedTypes guards against a
// regression where a server offering multiple host key types (e.g. ed25519
// and rsa) could negotiate a type that was never recorded for a specific
// hostname's known_hosts entry, causing a correct, unchanged server key to
// be rejected as unknown.
func TestPreferredHostKeyAlgorithmsMatchesRecordedTypes(t *testing.T) {
	ed25519Key := newTestED25519Key(t)
	rsaKey := newTestRSAKey(t)

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	lines := knownhosts.Line([]string{"host-a.test"}, ed25519Key) + "\n" +
		knownhosts.Line([]string{"host-b.test"}, rsaKey) + "\n" +
		knownhosts.Line([]string{"host-b.test"}, ed25519Key) + "\n"
	if err := os.WriteFile(knownHosts, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := preferredHostKeyAlgorithms(knownHosts, "host-a.test:22"); len(got) != 1 || got[0] != ed25519Key.Type() {
		t.Fatalf("host-a.test:22 = %v, want a single %q", got, ed25519Key.Type())
	}

	got := preferredHostKeyAlgorithms(knownHosts, "host-b.test:22")
	if len(got) != 2 {
		t.Fatalf("host-b.test:22 = %v, want both recorded algorithms", got)
	}

	if got := preferredHostKeyAlgorithms(knownHosts, "unknown.test:22"); got != nil {
		t.Fatalf("unknown.test:22 = %v, want nil for a host with no entry", got)
	}

	if got := preferredHostKeyAlgorithms(filepath.Join(t.TempDir(), "missing"), "host-a.test:22"); got != nil {
		t.Fatalf("missing known_hosts file = %v, want nil", got)
	}
}

func newTestRSAKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey
}

func newTestED25519Key(t *testing.T) ssh.PublicKey {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return sshPublicKey
}
