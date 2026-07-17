package sshclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/bashatahamal/vericopy/internal/verrors"
)

// Options controls strict SSH client construction.
type Options struct {
	User            string
	Host            string
	Port            int
	KnownHosts      string
	Identity        string
	Timeout         time.Duration
	HostKeyCallback ssh.HostKeyCallback
	DialContext     func(context.Context, string, string) (net.Conn, error)
}

// Client owns an authenticated SSH connection.
type Client struct {
	*ssh.Client
}

// NewHostKeyCallback loads a strict known_hosts verifier.
func NewHostKeyCallback(filename string) (ssh.HostKeyCallback, error) {
	if filename == "" {
		return nil, verrors.New(verrors.CodeKnownHostsUnavailable, "no known_hosts file was configured")
	}
	info, err := os.Stat(filename)
	if err != nil {
		return nil, verrors.Wrap(verrors.CodeKnownHostsUnavailable,
			fmt.Sprintf("cannot read known_hosts file %q", filename), err).
			WithHint("Add the server key with ssh-keyscan after verifying its fingerprint through a trusted channel.")
	}
	if info.IsDir() {
		return nil, verrors.New(verrors.CodeKnownHostsUnavailable, "the known_hosts path is a directory")
	}
	callback, err := knownhosts.New(filename)
	if err != nil {
		return nil, verrors.Wrap(verrors.CodeKnownHostsUnavailable, "the known_hosts file could not be parsed", err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := callback(hostname, remote, key); err != nil {
			var keyErr *knownhosts.KeyError
			if errors.As(err, &keyErr) {
				return verrors.Wrap(verrors.CodeHostKeyRejected,
					"the SSH server host key is unknown or does not match known_hosts", err).
					WithHint("Verify the server fingerprint independently. Never bypass this check.")
			}
			return verrors.Wrap(verrors.CodeHostKeyRejected, "SSH host-key verification failed", err)
		}
		return nil
	}, nil
}

// Dial opens a context-aware SSH connection with agent or private-key auth.
func Dial(ctx context.Context, options Options) (*Client, error) {
	if options.Port == 0 {
		options.Port = 22
	}
	if options.Timeout == 0 {
		options.Timeout = 15 * time.Second
	}
	if options.User == "" {
		return nil, verrors.New(verrors.CodeInvalidArguments, "an SSH user is required")
	}
	callback := options.HostKeyCallback
	if callback == nil {
		var err error
		callback, err = NewHostKeyCallback(options.KnownHosts)
		if err != nil {
			return nil, err
		}
	}
	authMethods, cleanup, err := authenticationMethods(options.Identity)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if len(authMethods) == 0 {
		return nil, verrors.New(verrors.CodeAuthenticationFailed,
			"no SSH agent or readable private key was available").WithHint(
			"Start an SSH agent with a loaded key, or pass --identity PATH.")
	}

	configuration := &ssh.ClientConfig{
		User: options.User, Auth: authMethods, HostKeyCallback: callback, Timeout: options.Timeout,
	}
	address := net.JoinHostPort(options.Host, strconv.Itoa(options.Port))
	dialContext := options.DialContext
	if dialContext == nil {
		dialer := &net.Dialer{Timeout: options.Timeout, KeepAlive: 30 * time.Second}
		dialContext = dialer.DialContext
	}
	connection, err := dialContext(ctx, "tcp", address)
	if err != nil {
		return nil, verrors.Wrap(verrors.CodeConnectionFailed,
			fmt.Sprintf("could not connect to %s", address), err)
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, address, configuration)
	if err != nil {
		_ = connection.Close()
		if diagnostic := verrors.As(err); diagnostic.Code != verrors.CodeInternal {
			return nil, diagnostic
		}
		return nil, verrors.Wrap(verrors.CodeAuthenticationFailed,
			"the SSH handshake or authentication failed", err)
	}
	return &Client{Client: ssh.NewClient(clientConnection, channels, requests)}, nil
}

func authenticationMethods(identity string) ([]ssh.AuthMethod, func(), error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	closers := make([]func(), 0, 1)
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		connection, err := net.Dial("unix", socket)
		if err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(connection).Signers))
			closers = append(closers, func() { _ = connection.Close() })
		}
	}

	identities := []string{}
	if identity != "" {
		identities = append(identities, identity)
	} else if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			identities = append(identities, filepath.Join(home, ".ssh", name))
		}
	}
	for _, filename := range identities {
		contents, err := os.ReadFile(filename)
		if err != nil {
			if identity != "" {
				return nil, closeAll(closers), verrors.Wrap(verrors.CodeAuthenticationFailed,
					fmt.Sprintf("could not read identity file %q", filename), err)
			}
			continue
		}
		signer, err := ssh.ParsePrivateKey(contents)
		if err != nil {
			var passphraseMissing *ssh.PassphraseMissingError
			if errors.As(err, &passphraseMissing) {
				return nil, closeAll(closers), verrors.Wrap(verrors.CodeAuthenticationFailed,
					"the private key is encrypted and requires an SSH agent", err).
					WithHint("Load the key with ssh-add so no password is exposed in process arguments.")
			}
			return nil, closeAll(closers), verrors.Wrap(verrors.CodeAuthenticationFailed,
				"the private key could not be parsed", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
		if identity != "" {
			break
		}
	}
	return methods, closeAll(closers), nil
}

func closeAll(closers []func()) func() {
	return func() {
		for _, closeFn := range closers {
			closeFn()
		}
	}
}

// DefaultKnownHosts returns the user's standard known_hosts path.
func DefaultKnownHosts() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

// FormatHost returns the known_hosts address form for a host and port.
func FormatHost(host string, port int) string {
	if port == 0 || port == 22 {
		return host
	}
	return knownhosts.Normalize(net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)))
}
