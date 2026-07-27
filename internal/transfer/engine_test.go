package transfer_test

import (
	"bytes"
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
	"github.com/bashatahamal/vericopy/internal/checksum"
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
func (r localRemote) ReadDir(name string) ([]fs.FileInfo, error) {
	entries, err := os.ReadDir(r.local(name))
	if err != nil {
		return nil, err
	}
	infos := make([]fs.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

type countingOpenRemote struct {
	localRemote
	opens *int
}

func (r countingOpenRemote) Open(name string) (io.ReadCloser, error) {
	*r.opens++
	return r.localRemote.Open(name)
}

type stubHasher struct {
	digest string
	ok     bool
	err    error
	calls  int
}

func (h *stubHasher) HashSHA256(_ context.Context, _ string) (string, bool, error) {
	h.calls++
	return h.digest, h.ok, h.err
}

func TestCopyUsesRemoteHasherFastPathAndSkipsReadback(t *testing.T) {
	source := filepath.Join(t.TempDir(), "movie.mkv")
	content := strings.Repeat("large-file-content", 2048)
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	opens := 0
	remoteFS := countingOpenRemote{localRemote: localRemote{root: t.TempDir()}, opens: &opens}
	policy, _ := permissions.Resolve("private", "", "")

	// Compute the real digest up front so the stub hasher reports the truth,
	// as a real remote hash command would.
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	localDigest, _, err := checksum.SHA256(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	hasher := &stubHasher{digest: localDigest, ok: true}

	result, err := (transfer.Engine{Remote: remoteFS, Hasher: hasher}).Copy(
		context.Background(), source, "/movies/movie.mkv", transfer.Options{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified {
		t.Fatal("result was not marked verified")
	}
	if hasher.calls == 0 {
		t.Fatal("the remote hasher was never consulted")
	}
	if opens != 0 {
		t.Fatalf("expected the fast path to skip reading the remote file back, but Open was called %d time(s)", opens)
	}
}

func TestCopyFallsBackWhenHasherMismatches(t *testing.T) {
	source := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(source, []byte("actual matching content"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	policy, _ := permissions.Resolve("private", "", "")
	// A hasher that lies (wrong digest) must not cause a false failure or a
	// false success: the authoritative read-back should still run and find
	// the real content actually matches.
	hasher := &stubHasher{digest: "0000000000000000000000000000000000000000000000000000000000000000", ok: true}

	result, err := (transfer.Engine{Remote: remoteFS, Hasher: hasher}).Copy(
		context.Background(), source, "/target", transfer.Options{Policy: policy})
	if err != nil {
		t.Fatalf("expected the read-back fallback to still succeed, got: %v", err)
	}
	if !result.Verified {
		t.Fatal("result was not marked verified")
	}
}

func TestCopyFallsBackWhenHasherUnavailable(t *testing.T) {
	source := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(source, []byte("some content"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	policy, _ := permissions.Resolve("private", "", "")
	hasher := &stubHasher{ok: false}

	result, err := (transfer.Engine{Remote: remoteFS, Hasher: hasher}).Copy(
		context.Background(), source, "/target", transfer.Options{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified {
		t.Fatal("result was not marked verified")
	}
}

func TestCopyAndVerify(t *testing.T) {
	source := filepath.Join(t.TempDir(), "quarterly-report.zip")
	content := strings.Repeat("verified-content", 1024)
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	policy, _ := permissions.Resolve("shared", "", "")
	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), source, "/shared/quarterly-report.zip", transfer.Options{Resume: true, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.Bytes != int64(len(content)) || result.SHA256 == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	got, err := os.ReadFile(remoteFS.local("/shared/quarterly-report.zip"))
	if err != nil || string(got) != content {
		t.Fatalf("remote content mismatch: %v", err)
	}
}

func TestCopyReportsTruthfulFileProgress(t *testing.T) {
	source := filepath.Join(t.TempDir(), "annual-report.pdf")
	content := strings.Repeat("progress-evidence", 4096)
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	policy, _ := permissions.Resolve("private", "", "")
	var updates []transfer.Progress
	_, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), source, "/shared/annual-report.pdf", transfer.Options{
		Policy: policy,
		Progress: func(progress transfer.Progress) {
			updates = append(updates, progress)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) == 0 {
		t.Fatal("expected progress updates")
	}
	var sawUpload, sawVerify, sawFinal bool
	for _, update := range updates {
		if update.TotalBytes != int64(len(content)) {
			t.Fatalf("unexpected progress total: %#v", update)
		}
		switch update.Phase {
		case "uploading":
			sawUpload = update.TransferredBytes <= update.TotalBytes
		case "verifying":
			sawVerify = update.TransferredBytes == update.TotalBytes
		case "verified":
			sawFinal = update.TransferredBytes == update.TotalBytes
		}
	}
	if !sawUpload || !sawVerify || !sawFinal {
		t.Fatalf("missing truthful progress phases: %#v", updates)
	}
}

func TestCopyDirectoryReportsFilePosition(t *testing.T) {
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "a.txt"), []byte("first file content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "b.txt"), []byte("second file content"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	policy, _ := permissions.Resolve("private", "", "")
	var updates []transfer.Progress
	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), sourceRoot, "/shared", transfer.Options{
		Recursive: true, Policy: policy,
		Progress: func(progress transfer.Progress) { updates = append(updates, progress) },
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
		if update.CurrentFile < 1 || update.CurrentFile > 2 {
			t.Fatalf("CurrentFile out of range: %#v", update)
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

func TestNoClobberSkipsExistingFile(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("new content"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	if err := os.WriteFile(remoteFS.local("/target"), []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), source, "/target", transfer.Options{NoClobber: true, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 0 || result.SkippedFiles != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	got, err := os.ReadFile(remoteFS.local("/target"))
	if err != nil || string(got) != "old content" {
		t.Fatalf("existing file was modified: content=%q err=%v", got, err)
	}
}

// TestNoClobberDirectoryMergesAndSkipsExistingFiles is the exact scenario a
// user hits re-copying a folder after an earlier run already delivered some
// of its files: the destination directory already exists, one file inside
// it already matches a source file, and another source file is new. Without
// --overwrite or --no-clobber this used to fail the entire copy immediately
// just because the destination directory existed, before even looking at
// which files inside it were actually new.
func TestNoClobberDirectoryMergesAndSkipsExistingFiles(t *testing.T) {
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "already-there.mkv"), []byte("source version"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "new-episode.mkv"), []byte("new episode"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	if err := os.MkdirAll(remoteFS.local("/movies/TYaL"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remoteFS.local("/movies/TYaL/already-there.mkv"), []byte("remote version"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	result, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), sourceRoot, "/movies/TYaL",
		transfer.Options{Recursive: true, NoClobber: true, Policy: policy})
	if err != nil {
		t.Fatalf("an existing destination directory blocked the merge: %v", err)
	}
	if result.Files != 1 || result.SkippedFiles != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	existing, err := os.ReadFile(remoteFS.local("/movies/TYaL/already-there.mkv"))
	if err != nil || string(existing) != "remote version" {
		t.Fatalf("the already-present file was clobbered: content=%q err=%v", existing, err)
	}
	added, err := os.ReadFile(remoteFS.local("/movies/TYaL/new-episode.mkv"))
	if err != nil || string(added) != "new episode" {
		t.Fatalf("the new file was not copied: content=%q err=%v", added, err)
	}
}

func TestNoClobberDirectoryStillRejectsPlainMergeAttempt(t *testing.T) {
	// Without NoClobber (and without Overwrite), an existing destination
	// directory must still fail fast, exactly as before. NoClobber is an
	// explicit opt-in to merging, not a change to the default.
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFS := localRemote{root: t.TempDir()}
	if err := os.MkdirAll(remoteFS.local("/target"), 0o700); err != nil {
		t.Fatal(err)
	}
	policy, _ := permissions.Resolve("private", "", "")
	_, err := (transfer.Engine{Remote: remoteFS}).Copy(context.Background(), sourceRoot, "/target", transfer.Options{Recursive: true, Policy: policy})
	if err == nil || verrors.As(err).Code != verrors.CodeDestinationExists {
		t.Fatalf("existing destination directory was not protected by default: %v", err)
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
