package output_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/bashatahamal/vericopy/internal/output"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestJSONOutput(t *testing.T) {
	var stdout bytes.Buffer
	printer := output.Printer{Out: &stdout, Err: &stdout, JSON: true}
	if err := printer.Success("ignored", map[string]string{"status": "ready"}); err != nil {
		t.Fatal(err)
	}
	var envelope output.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK {
		t.Fatal("expected success envelope")
	}

	stdout.Reset()
	if err := printer.Failure(verrors.New(verrors.CodeInvalidArguments, "bad input")); err != nil {
		t.Fatal(err)
	}
	var failure map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure["ok"] != false {
		t.Fatalf("unexpected failure envelope: %#v", failure)
	}
}
