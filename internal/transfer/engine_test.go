package transfer_test

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

type localRemote struct{ root string }

func (r localRemote) local(name string) string {
	clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(name)), "/")
	return filepath.Join(r.root, filepath.FromSlash(clean))
}
func (r localRemote) Open(name string) (io.ReadCloser, error) { return os.Open(r.local(name)) }
func (r localRemote) OpenFile(name string, flags int) (nativesftp.File, error) {
	return os.OpenFile(r.local(name), flags, 0o600)
}
func (r localRemote) Stat(name string) (fs.FileInfo, error)  { return os.Stat(r.local(name)) }
func (r localRemote) Lstat(name string) (fs.FileInfo, error) { return os.Lstat(r.local(name)) }
func (r localRemote) Mkdir(name string) error                { return os.Mkdir(r.local(name), 0o700) }
func (r localRemote) MkdirAll(name string) error             { return os.MkdirAll(r.local(name), 0o700) }
func (r localRemote) Chmod(name string, mode fs.FileMode) error {
	return os.Chmod(r.local(name), mode)
}
func (r localRemote) Chown(_ string, _, _ int) error { return nil }
func (r localRemote) Chtimes(name string, atime, mtime time.Time) error {
	return os.Chtimes(r.local(name), atime, mtime)
}
func (r localRemote) Rename(oldName, newName string) error {
	return os.Rename(r.local(oldName), r.local(newName))
}
func (r localRemote) Remove(name string) error { return os.Remove(r.local(name)) }

func TestCopyAndVerify(t *testing.T) {
	source := filepath.Join(t.TempDir(), "My Film.mkv")
	content := strings.Repeat("verified-content", 1024)
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	policy, _ := permissions.Resolve("media-readonly", "", "")
	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), source, "/media/My Film.mkv", transfer.Options{Resume: true, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.Bytes != int64(len(content)) || result.SHA256 == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	got, err := os.ReadFile(remoteFS.local("/media/My Film.mkv"))
	if err != nil || string(got) != content {
		t.Fatalf("remote content mismatch: %v", err)
	}
}

func TestExistingDestinationProtected(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	if err := os.MkdirAll(filepath.Dir(remoteFS.local("/target")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remoteFS.local("/target"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	_, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), source, "/target", transfer.Options{Policy: policy})
	if err == nil || verrors.As(err).Code != verrors.CodeDestinationExists {
		t.Fatalf("existing destination was not protected: %v", err)
	}
}

func TestOverwriteReplacesDestination(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("new content"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	if err := os.WriteFile(remoteFS.local("/target"), []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), source, "/target", transfer.Options{Overwrite: true, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(remoteFS.local("/target"))
	if err != nil || string(got) != "new content" || !result.Verified {
		t.Fatalf("overwrite failed: result=%#v content=%q err=%v", result, got, err)
	}
}

func TestOverwriteAndNoClobberConflict(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	_, err := (transfer.Engine{Remote: localRemote{root: t.TempDir()}}).Copy(context.Background(), source, "/target", transfer.Options{Overwrite: true, NoClobber: true, Policy: policy})
	if err == nil || verrors.As(err).Code != verrors.CodeInvalidArguments {
		t.Fatalf("conflicting policy was not rejected: %v", err)
	}
}

type corruptReaderRemote struct{ localRemote }

func (r corruptReaderRemote) Open(name string) (io.ReadCloser, error) {
	if strings.Contains(name, ".partial") && !strings.HasSuffix(name, ".json") {
		return io.NopCloser(strings.NewReader("corrupted remote bytes")), nil
	}
	return r.localRemote.Open(name)
}

func TestChecksumMismatchRetainsPartial(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("verified source bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := corruptReaderRemote{localRemote{root: t.TempDir()}}
	policy, _ := permissions.Resolve("private", "", "")
	_, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), source, "/target", transfer.Options{Policy: policy})
	if err == nil || verrors.As(err).Code != verrors.CodeChecksumMismatch {
		t.Fatalf("corruption was not detected: %v", err)
	}
	if _, statErr := os.Stat(remoteFS.local("/target")); !os.IsNotExist(statErr) {
		t.Fatalf("corrupt partial was finalized: %v", statErr)
	}
}

func TestSymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated Windows privileges")
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	link := filepath.Join(directory, "link")
	if err := os.WriteFile(target, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	_, err := (transfer.Engine{Remote: localRemote{root: t.TempDir()}}).Copy(context.Background(), link, "/target", transfer.Options{Policy: policy})
	if err == nil || verrors.As(err).Code != verrors.CodeUnsupportedFileType {
		t.Fatalf("symlink was not rejected: %v", err)
	}
}

func TestRemoteSymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated Windows privileges")
	}
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, remoteFS.local("/linked")); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	_, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), source, "/linked/target", transfer.Options{Policy: policy})
	if err == nil || verrors.As(err).Code != verrors.CodeUnsupportedFileType {
		t.Fatalf("remote symlink was not rejected: %v", err)
	}
}
