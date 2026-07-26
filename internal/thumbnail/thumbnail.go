// Package thumbnail renders a plain placeholder image carrying a text label.
// It exists so a copied episode can get a locally distinguishable thumbnail
// without decoding the source video or reaching the network: no ffmpeg, no
// metadata lookup, just a label drawn on a flat background.
package thumbnail

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// width and height match a portrait poster aspect ratio (2:3), because the
// generated image is written as the item's Primary/poster image (see
// transfer.Options.GenerateThumbnails), not a landscape thumb — a landscape
// image in that slot would render stretched or cropped in most media
// server grids. fontPoints is deliberately large: a poster this size is
// usually viewed shrunk to a small grid tile, so text needs to stay
// legible after that scale-down, not just at full size.
const (
	width      = 500
	height     = 750
	margin     = 40
	fontPoints = 64
	dpi        = 72
)

// Generate renders a JPEG-encoded placeholder image with label centered on
// it. label is expected to be short (a title and an episode tag); long
// labels wrap across multiple lines but are not truncated.
func Generate(label string) ([]byte, error) {
	face, err := loadFace()
	if err != nil {
		return nil, err
	}
	defer face.Close()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 0x1b, G: 0x1f, B: 0x24, A: 0xff}}, image.Point{}, draw.Src)
	drawCenteredText(img, label, face, color.White)

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// loadFace parses the embedded Go Bold TTF (bundled with golang.org/x/image;
// no network fetch and no font files to ship) at a large fixed point size.
func loadFace() (font.Face, error) {
	parsed, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    fontPoints,
		DPI:     dpi,
		Hinting: font.HintingFull,
	})
}

func drawCenteredText(img *image.RGBA, label string, face font.Face, textColor color.Color) {
	lines := wrap(label, face, width-2*margin)
	lineHeight := face.Metrics().Height.Ceil() + 8
	totalHeight := lineHeight * len(lines)
	y := (height-totalHeight)/2 + face.Metrics().Ascent.Ceil()
	for _, line := range lines {
		textWidth := font.MeasureString(face, line).Ceil()
		x := (width - textWidth) / 2
		(&font.Drawer{
			Dst:  img,
			Src:  &image.Uniform{C: textColor},
			Face: face,
			Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
		}).DrawString(line)
		y += lineHeight
	}
}

func wrap(label string, face font.Face, maxWidth int) []string {
	words := strings.Fields(label)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, 2)
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if font.MeasureString(face, candidate).Ceil() > maxWidth {
			lines = append(lines, current)
			current = word
			continue
		}
		current = candidate
	}
	return append(lines, current)
}
