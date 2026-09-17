package m365copilot

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// colorIcon renders a placeholder 192x192 color icon: a solid square of
// accentColor. Microsoft 365 Copilot doesn't currently render anything but
// this color for an agent's icon, but the app package fails validation
// without one.
func colorIcon(accentHex string) ([]byte, error) {
	c, err := parseHexColor(accentHex)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, 192, 192))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	return encodePNG(img)
}

// outlineIcon renders a placeholder 32x32 outline icon: a white square on a
// transparent background, satisfying the app package's "white with a
// transparent background" requirement.
func outlineIcon() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	// Fully transparent canvas, then an inset white square as the "symbol"
	// -- real icons should replace this with actual glyph art.
	inset := 6
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	draw.Draw(img, image.Rect(inset, inset, 32-inset, 32-inset), &image.Uniform{C: white}, image.Point{}, draw.Src)
	return encodePNG(img)
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encoding PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// parseHexColor parses a "#RRGGBB" string into an opaque color.RGBA.
func parseHexColor(hex string) (color.RGBA, error) {
	s := hex
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	if len(s) != 6 {
		return color.RGBA{}, fmt.Errorf("invalid hex color %q (want #RRGGBB)", hex)
	}
	var r, g, b int
	if _, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{}, fmt.Errorf("invalid hex color %q: %w", hex, err)
	}
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xFF}, nil
}
