package transfer_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestDownloadAndVerify(t *testing.T) {
	remoteRoot := t.TempDir()
	content := strings.Repeat("verified-content", 1024)
	if err := os.WriteFile(filepath.Join(remoteRoot, "quarterly-report.zip"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: remoteRoot}
	destination := filepath.Join(t.TempDir(), "quarterly-report.zip")
	result, err := (transfer.Engine{Remote: remoteFS}).Download(context.Background(), "/quarterly-report.zip", destination, transfer.Options{Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.Bytes != int64(len(content)) || result.SHA256 == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != content {
		t.Fatalf("local content mismatch: %v", err)
	}
}

func TestDownloadDirectoryReportsFilePosition(t *testing.T) {
	remoteRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "a.txt"), []byte("first file content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "b.txt"), []byte("second file content"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: remoteRoot}
	destination := filepath.Join(t.TempDir(), "shared")
	var updates []transfer.Progress
	result, err := (transfer.Engine{Remote: remoteFS}).Download(context.Background(), "/", destination, transfer.Options{
		Recursive: true,
		Progress:  func(progress transfer.Progress) { updates = append(updates, progress) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 2 {
		t.Fatalf("unexpected file count: %#v", result)
	}
	seenFileOne, seenFileTwo := false, false
	for _, update := range updates {
		if update.TotalFiles != 2 {
			t.Fatalf("expected TotalFiles=2 on every update, got %#v", update)
		}
		if update.CurrentFile == 1 {
			seenFileOne = true
		}
		if update.CurrentFile == 2 {
			seenFileTwo = true
		}
	}
	if !seenFileOne || !seenFileTwo {
		t.Fatalf("did not see progress for both files: %#v", updates)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(destination, name)); err != nil {
			t.Fatalf("expected %s to be downloaded: %v", name, err)
		}
	}
}

func TestDownloadExistingDestinationProtected(t *testing.T) {
	remoteRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "target"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (transfer.Engine{Remote: localRemote{root: remoteRoot}}).Download(context.Background(), "/target", destination, transfer.Options{})
	if err == nil || verrors.As(err).Code != verrors.CodeDestinationExists {
		t.Fatalf("existing destination was not protected: %v", err)
	}
}

func TestDownloadOverwriteReplacesDestination(t *testing.T) {
	remoteRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "target"), []byte("new content"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(destination, []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (transfer.Engine{Remote: localRemote{root: remoteRoot}}).Download(context.Background(), "/target", destination, transfer.Options{Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != "new content" || !result.Verified {
		t.Fatalf("overwrite failed: result=%#v content=%q err=%v", result, got, err)
	}
}

func TestDownloadNoClobberSkipsExistingFile(t *testing.T) {
	remoteRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "target"), []byte("new content"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(destination, []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (transfer.Engine{Remote: localRemote{root: remoteRoot}}).Download(context.Background(), "/target", destination, transfer.Options{NoClobber: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 0 || result.SkippedFiles != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != "old content" {
		t.Fatalf("existing file was modified: content=%q err=%v", got, err)
	}
}

func TestDownloadRemoteSymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated Windows privileges")
	}
	remoteRoot := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(remoteRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "target")
	_, err := (transfer.Engine{Remote: localRemote{root: remoteRoot}}).Download(context.Background(), "/linked", destination, transfer.Options{})
	if err == nil || verrors.As(err).Code != verrors.CodeUnsupportedFileType {
		t.Fatalf("remote symlink was not rejected: %v", err)
	}
}

type verifyCorruptingRemote struct {
	localRemote
	opens *int
}

func (r verifyCorruptingRemote) Open(name string) (io.ReadCloser, error) {
	*r.opens++
	if *r.opens >= 3 {
		return io.NopCloser(strings.NewReader("corrupted remote bytes")), nil
	}
	return r.localRemote.Open(name)
}

func TestDownloadChecksumMismatchRetainsPartial(t *testing.T) {
	remoteRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "target"), []byte("verified remote bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	opens := 0
	remoteFS := verifyCorruptingRemote{localRemote: localRemote{root: remoteRoot}, opens: &opens}
	destination := filepath.Join(t.TempDir(), "target")
	_, err := (transfer.Engine{Remote: remoteFS}).Download(context.Background(), "/target", destination, transfer.Options{})
	if err == nil || verrors.As(err).Code != verrors.CodeChecksumMismatch {
		t.Fatalf("corruption was not detected: %v", err)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("corrupt partial was finalized: %v", statErr)
	}
}

type limitedReadCloser struct {
	reader io.ReadCloser
	limit  int
	read   int
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	if l.read >= l.limit {
		return 0, errors.New("injected connection interruption")
	}
	if len(p) > l.limit-l.read {
		p = p[:l.limit-l.read]
	}
	n, err := l.reader.Read(p)
	l.read += n
	return n, err
}

func (l *limitedReadCloser) Close() error { return l.reader.Close() }

type interruptingRemote struct {
	localRemote
	opens *int
	limit int
}

func (r interruptingRemote) Open(name string) (io.ReadCloser, error) {
	*r.opens++
	reader, err := r.localRemote.Open(name)
	if err != nil {
		return nil, err
	}
	if *r.opens == 2 {
		return &limitedReadCloser{reader: reader, limit: r.limit}, nil
	}
	return reader, nil
}

func TestDownloadResumesAfterInterruption(t *testing.T) {
	remoteRoot := t.TempDir()
	content := strings.Repeat("resume-content-chunk-", 4096)
	if err := os.WriteFile(filepath.Join(remoteRoot, "target"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	opens := 0
	interrupted := interruptingRemote{localRemote: localRemote{root: remoteRoot}, opens: &opens, limit: len(content) / 2}
	destination := filepath.Join(t.TempDir(), "target")

	_, err := (transfer.Engine{Remote: interrupted}).Download(context.Background(), "/target", destination, transfer.Options{Resume: true})
	if err == nil || verrors.As(err).Code != verrors.CodeTransferInterrupted {
		t.Fatalf("expected an interrupted transfer, got: %v", err)
	}

	result, err := (transfer.Engine{Remote: interrupted}).Download(context.Background(), "/target", destination, transfer.Options{Resume: true})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if result.ResumedBytes == 0 {
		t.Fatalf("expected resumed bytes greater than zero, got %#v", result)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != content {
		t.Fatalf("resumed content mismatch: %v", err)
	}
}
