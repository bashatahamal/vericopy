package transfer_test

import (
	"strings"
	"testing"

	"github.com/bashatahamal/vericopy/internal/transfer"
)

func TestPartialNaming(t *testing.T) {
	metadata := transfer.PartialMetadata{Schema: 1, SourceSize: 42, PrefixSHA256: strings.Repeat("a", 64)}
	partial, sidecar := transfer.PartialPaths("/srv/media/My Film.mkv", metadata)
	if partial == "/srv/media/My Film.mkv" || !strings.HasPrefix(partial, "/srv/media/.My Film.mkv.vericopy-") {
		t.Fatalf("unsafe partial path: %q", partial)
	}
	if sidecar != partial+".json" {
		t.Fatalf("unexpected sidecar: %q", sidecar)
	}
}

func TestResumeCompatibility(t *testing.T) {
	expected := transfer.PartialMetadata{Schema: 1, SourceSize: 100, SourceMTime: 20, PrefixBytes: 100, PrefixSHA256: "abc"}
	if !transfer.ResumeCompatible(expected, expected, 50) {
		t.Fatal("compatible state rejected")
	}
	changed := expected
	changed.PrefixSHA256 = "def"
	if transfer.ResumeCompatible(expected, changed, 50) {
		t.Fatal("changed prefix accepted")
	}
	if transfer.ResumeCompatible(expected, expected, 101) {
		t.Fatal("oversized partial accepted")
	}
}
