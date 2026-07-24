package sshclient_test

import (
	"context"
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

// TestDialNormalizesHostCaseForKnownHosts guards against a regression where a
// destination hostname typed with different letter case than the stored
// known_hosts entry (e.g. "MixedCase.test" vs "mixedcase.test") was rejected
// as an unknown host, even though DNS hostnames are case-insensitive.
func TestDialNormalizesHostCaseForKnownHosts(t *testing.T) {
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		serverConfig := &ssh.ServerConfig{NoClientAuth: true}
		serverConfig.AddHostKey(signer)
		serverConnection, channels, requests, err := ssh.NewServerConn(connection, serverConfig)
		if err != nil {
			connection.Close()
			return
		}
		defer serverConnection.Close()
		go ssh.DiscardRequests(requests)
		for newChannel := range channels {
			_ = newChannel.Reject(ssh.Prohibited, "test server accepts no channels")
		}
	}()

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{"mixedcase.test"}, signer.PublicKey())
	if err := os.WriteFile(knownHosts, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	client, err := sshclient.Dial(context.Background(), sshclient.Options{
		User:           "test",
		Host:           "MixedCase.test",
		KnownHosts:     knownHosts,
		Authentication: sshclient.AuthenticationPassword,
		Password:       "unused",
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, listener.Addr().String())
		},
	})
	if err != nil {
		t.Fatalf("Dial rejected a known host that only differed in case: %v", err)
	}
	defer client.Close()
}
