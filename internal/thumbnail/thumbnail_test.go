package thumbnail_test

import (
	"bytes"
	"image/jpeg"
	"testing"

	"github.com/bashatahamal/vericopy/internal/thumbnail"
)

func TestGenerateProducesDecodableJPEG(t *testing.T) {
	data, err := thumbnail.Generate("The East Palace S01E01")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty image data")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("generated data is not a valid JPEG: %v", err)
	}
	if config.Width == 0 || config.Height == 0 {
		t.Fatalf("expected non-zero dimensions, got %dx%d", config.Width, config.Height)
	}
}

func TestGenerateHandlesEmptyAndLongLabels(t *testing.T) {
	for _, label := range []string{"", "A Very Long Series Title That Should Wrap Across Several Lines S01E01"} {
		if _, err := thumbnail.Generate(label); err != nil {
			t.Fatalf("Generate(%q) failed: %v", label, err)
		}
	}
}
