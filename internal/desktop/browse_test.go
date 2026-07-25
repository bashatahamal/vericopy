package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bashatahamal/vericopy/internal/desktop"
)

func TestListLocalDirectoryOrdersDirectoriesFirst(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "b-file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a-folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	listing, err := desktop.NewService().ListLocalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("unexpected entries: %#v", listing.Entries)
	}
	if !listing.Entries[0].IsDir || listing.Entries[0].Name != "a-folder" {
		t.Fatalf("expected the directory to sort first: %#v", listing.Entries)
	}
	if listing.Entries[1].Name != "b-file.txt" || listing.Entries[1].Size != int64(len("content")) {
		t.Fatalf("unexpected file entry: %#v", listing.Entries[1])
	}
	if listing.Parent == "" || listing.Parent == root {
		t.Fatalf("expected a parent different from root, got %q", listing.Parent)
	}
}

func TestListLocalDirectoryRejectsAFile(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "not-a-directory.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.NewService().ListLocalDirectory(filePath); err == nil {
		t.Fatal("expected an error when listing a file as if it were a directory")
	}
}

func TestDeleteLocalPathsRemovesFilesAndFoldersRecursively(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "delete-me.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	folderPath := filepath.Join(root, "delete-me-too")
	if err := os.MkdirAll(filepath.Join(folderPath, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folderPath, "nested", "inner.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := desktop.NewService().DeleteLocalPaths([]string{filePath, folderPath})
	if result.Deleted != 2 || len(result.Failures) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("file was not deleted: %v", err)
	}
	if _, err := os.Stat(folderPath); !os.IsNotExist(err) {
		t.Fatalf("folder was not deleted: %v", err)
	}
}

func TestDeleteLocalPathsProcessesEveryPathIndependently(t *testing.T) {
	// os.RemoveAll succeeds on an already-missing path (the same rm -rf
	// semantics DeleteLocalPaths inherits), so this exercises that a
	// path listed twice, or a path that vanished a moment earlier, does not
	// stop the rest of a multi-select delete from completing.
	root := t.TempDir()
	existingFile := filepath.Join(root, "exists.txt")
	if err := os.WriteFile(existingFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	alreadyGone := filepath.Join(root, "already-gone.txt")
	result := desktop.NewService().DeleteLocalPaths([]string{alreadyGone, existingFile})
	if result.Deleted != 2 || len(result.Failures) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := os.Stat(existingFile); !os.IsNotExist(err) {
		t.Fatalf("the existing file should have been deleted: %v", err)
	}
}
