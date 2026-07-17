package app_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bashatahamal/vericopy/internal/app"
)

func TestVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root, _ := app.NewRoot(&stdout, &stderr)
	root.SetArgs([]string{"version", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if envelope["ok"] != true {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestInspectWindowsPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root, _ := app.NewRoot(&stdout, &stderr)
	root.SetArgs([]string{"inspect-path", `C:\\Users\\Person\\My Film.mkv`, "--target-os", "windows"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "windows-drive") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestArgumentErrorsUseStableDiagnostic(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root, _ := app.NewRoot(&stdout, &stderr)
	root.SetArgs([]string{"inspect-path"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "INVALID_ARGUMENTS") {
		t.Fatalf("unexpected argument error: %v", err)
	}
}
