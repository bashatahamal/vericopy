//go:build integration

package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/sshclient"
	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

type environment struct {
	host, identity, knownHosts string
	port                       int
}

func integrationEnvironment(t *testing.T) environment {
	t.Helper()
	if os.Getenv("VERICOPY_INTEGRATION") != "1" {
		t.Skip("set VERICOPY_INTEGRATION=1 or run integration/run.sh")
	}
	port, err := strconv.Atoi(os.Getenv("VERICOPY_INTEGRATION_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	return environment{
		host: os.Getenv("VERICOPY_INTEGRATION_HOST"), port: port,
		identity: os.Getenv("VERICOPY_INTEGRATION_IDENTITY"), knownHosts: os.Getenv("VERICOPY_INTEGRATION_KNOWN_HOSTS"),
	}
}

func connect(t *testing.T, environment environment) (*sshclient.Client, *nativesftp.Client) {
	t.Helper()
	sshConnection, err := sshclient.Dial(context.Background(), sshclient.Options{
		User: "transfer", Host: environment.host, Port: environment.port,
		Identity: environment.identity, KnownHosts: environment.knownHosts,
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteFS, err := nativesftp.New(sshConnection)
	if err != nil {
		sshConnection.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { remoteFS.Close(); sshConnection.Close() })
	return sshConnection, remoteFS
}

func TestUploadRecursivePermissionsAndExistingProtection(t *testing.T) {
	environment := integrationEnvironment(t)
	_, remoteFS := connect(t, environment)
	localRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(localRoot, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "My Film.mkv"), []byte(strings.Repeat("media", 4096)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "sub", "動画.txt"), []byte("unicode"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("media-readonly", "", "")
	destination := "/data/tree-" + strconv.Itoa(os.Getpid())
	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), localRoot, destination, transfer.Options{Recursive: true, Resume: true, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 2 || !result.Verified {
		t.Fatalf("unexpected result: %#v", result)
	}
	info, err := remoteFS.Stat(destination + "/My Film.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%04o want 0640", info.Mode().Perm())
	}
	_, err = (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), localRoot, destination, transfer.Options{Recursive: true, Policy: policy})
	if err == nil || verrors.As(err).Code != verrors.CodeDestinationExists {
		t.Fatalf("existing destination was not protected: %v", err)
	}
}

type failingRemote struct {
	*nativesftp.Client
	remaining int
}

func (r *failingRemote) OpenFile(name string, flags int) (nativesftp.File, error) {
	file, err := r.Client.OpenFile(name, flags)
	if err != nil || !strings.Contains(name, ".partial") || strings.HasSuffix(name, ".json") {
		return file, err
	}
	return &failingFile{File: file, remaining: &r.remaining}, nil
}

type failingFile struct {
	nativesftp.File
	remaining *int
}

func (f *failingFile) Write(buffer []byte) (int, error) {
	if *f.remaining <= 0 {
		return 0, errors.New("injected connection interruption")
	}
	if len(buffer) > *f.remaining {
		buffer = buffer[:*f.remaining]
	}
	n, err := f.File.Write(buffer)
	*f.remaining -= n
	return n, err
}

func TestInterruptedUploadResumes(t *testing.T) {
	environment := integrationEnvironment(t)
	_, remoteFS := connect(t, environment)
	source := filepath.Join(t.TempDir(), "large.bin")
	if err := os.WriteFile(source, []byte(strings.Repeat("0123456789abcdef", 256*1024)), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	destination := "/data/resume-" + strconv.Itoa(os.Getpid()) + ".bin"
	interrupted := &failingRemote{Client: remoteFS, remaining: 512 * 1024}
	_, err := (transfer.Engine{Remote: interrupted}).Copy(context.Background(), source, destination, transfer.Options{Resume: true, Policy: policy})
	if err == nil || verrors.As(err).Code != verrors.CodeTransferInterrupted {
		t.Fatalf("expected retained interrupted state, got %v", err)
	}
	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), source, destination, transfer.Options{Resume: true, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.ResumedBytes == 0 {
		t.Fatalf("upload did not resume: %#v", result)
	}
}

func TestUnknownHostRejected(t *testing.T) {
	environment := integrationEnvironment(t)
	emptyKnownHosts := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(emptyKnownHosts, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	connection, err := sshclient.Dial(context.Background(), sshclient.Options{
		User: "transfer", Host: environment.host, Port: environment.port,
		Identity: environment.identity, KnownHosts: emptyKnownHosts,
	})
	if connection != nil {
		connection.Close()
	}
	if err == nil || verrors.As(err).Code != verrors.CodeHostKeyRejected {
		t.Fatalf("unknown host was not rejected: %v", err)
	}
}
