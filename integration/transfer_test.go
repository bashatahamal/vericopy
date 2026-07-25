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

	"github.com/bashatahamal/vericopy/internal/access"
	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/desktop"
	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/remotehash"
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
	if err := os.WriteFile(filepath.Join(localRoot, "quarterly-report.zip"), []byte(strings.Repeat("report-data", 4096)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "sub", "動画.txt"), []byte("unicode"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("shared", "", "")
	destination := "/data/tree-" + strconv.Itoa(os.Getpid())
	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), localRoot, destination, transfer.Options{Recursive: true, Resume: true, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 2 || !result.Verified {
		t.Fatalf("unexpected result: %#v", result)
	}
	info, err := remoteFS.Stat(destination + "/quarterly-report.zip")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o660 {
		t.Fatalf("mode=%04o want 0660", info.Mode().Perm())
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

func TestCopyUsesRemoteHasherAgainstRealServer(t *testing.T) {
	environment := integrationEnvironment(t)
	sshConnection, remoteFS := connect(t, environment)
	source := filepath.Join(t.TempDir(), "hashed.bin")
	if err := os.WriteFile(source, []byte(strings.Repeat("verify-me-fast", 8192)), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	destination := "/data/hashed-" + strconv.Itoa(os.Getpid()) + ".bin"
	hasher := remotehash.Hasher{Runner: access.SSHRunner{Client: sshConnection.Client}}
	result, err := (transfer.Engine{Remote: remoteFS, Hasher: hasher}).Copy(context.Background(), source, destination, transfer.Options{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.SHA256 == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	// Independently ask the real container to hash the file it just
	// received. This proves sha256sum is actually reachable on the
	// container and that the digest it returns for this specific
	// destination path round-trips correctly through real quoting and
	// parsing over a live SSH exec channel, not just against a stub.
	digest, ok, hashErr := hasher.HashSHA256(context.Background(), destination)
	if hashErr != nil || !ok {
		t.Fatalf("remote hasher unavailable against the real container: ok=%v err=%v", ok, hashErr)
	}
	if digest != result.SHA256 {
		t.Fatalf("remote digest %s does not match transfer result %s", digest, result.SHA256)
	}
}

func TestRemoteHasherHandlesHostileFilenames(t *testing.T) {
	environment := integrationEnvironment(t)
	sshConnection, remoteFS := connect(t, environment)
	source := filepath.Join(t.TempDir(), "tricky.bin")
	if err := os.WriteFile(source, []byte("tricky path content"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	// A filename built to break naive shell interpolation: single quotes,
	// a subshell, backticks, and a semicolon. If quoting is wrong this
	// either corrupts the hash result or, in the worst case, executes
	// something on the container instead of just hashing a file.
	destination := "/data/tricky-" + strconv.Itoa(os.Getpid()) + " it's a $(id) `id` ; echo hi.bin"
	hasher := remotehash.Hasher{Runner: access.SSHRunner{Client: sshConnection.Client}}
	result, err := (transfer.Engine{Remote: remoteFS, Hasher: hasher}).Copy(context.Background(), source, destination, transfer.Options{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified {
		t.Fatalf("unexpected result: %#v", result)
	}
	info, err := remoteFS.Stat(destination)
	if err != nil {
		t.Fatalf("the destination was not created under its literal hostile name: %v", err)
	}
	if info.Size() != int64(len("tricky path content")) {
		t.Fatalf("unexpected remote file size: %d", info.Size())
	}
}

func TestPreviewDestinationAgainstRealServer(t *testing.T) {
	environment := integrationEnvironment(t)
	_, remoteFS := connect(t, environment)
	destination := "/data/preview-" + strconv.Itoa(os.Getpid())
	if err := remoteFS.MkdirAll(destination); err != nil {
		t.Fatal(err)
	}
	sourceFile := filepath.Join(t.TempDir(), "existing.bin")
	if err := os.WriteFile(sourceFile, []byte("existing content"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	if _, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), sourceFile, destination+"/existing.bin", transfer.Options{Policy: policy}); err != nil {
		t.Fatal(err)
	}

	service := desktop.NewService()
	t.Cleanup(service.Close)
	preview, err := service.PreviewDestination(desktop.TransferRequest{
		Source:      sourceFile,
		Destination: "transfer@" + environment.host + ":" + destination,
		Port:        environment.port, Identity: environment.identity, KnownHosts: environment.knownHosts,
		Authentication: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Exists || !preview.IsDirectory || preview.WillCreate {
		t.Fatalf("unexpected preview for an existing directory: %#v", preview)
	}
	found := false
	for _, entry := range preview.Entries {
		if entry.Name == "existing.bin" && !entry.IsDir && entry.Size == int64(len("existing content")) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected to see the file already copied there: %#v", preview.Entries)
	}

	missingPreview, err := service.PreviewDestination(desktop.TransferRequest{
		Source:      sourceFile,
		Destination: "transfer@" + environment.host + ":" + destination + "/does-not-exist-yet",
		Port:        environment.port, Identity: environment.identity, KnownHosts: environment.knownHosts,
		Authentication: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if missingPreview.Exists || !missingPreview.WillCreate || missingPreview.Path != destination {
		t.Fatalf("unexpected preview for a not-yet-created destination: %#v", missingPreview)
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
