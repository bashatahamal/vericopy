package sshclient_test

import (
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/bashatahamal/vericopy/internal/sshclient"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestStrictHostKeyCallback(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{"example.test"}, publicKey)
	if err := os.WriteFile(knownHosts, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	callback, err := sshclient.NewHostKeyCallback(knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 22}
	if err := callback("example.test:22", remote, publicKey); err != nil {
		t.Fatalf("known key rejected: %v", err)
	}

	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherPublic, _ := ssh.NewPublicKey(&otherKey.PublicKey)
	err = callback("example.test:22", remote, otherPublic)
	if err == nil || verrors.As(err).Code != verrors.CodeHostKeyRejected {
		t.Fatalf("changed key was not rejected correctly: %v", err)
	}
	if err := callback("unknown.test:22", remote, publicKey); err == nil {
		t.Fatal("unknown host was accepted")
	}
}
