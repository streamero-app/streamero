package metadata

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strconv"
	"testing"
)

func jpegOf(t *testing.T, fill func(x, y int) color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 160, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, fill(x, y))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func rgb(hex string) (r, g, b int) {
	v, _ := strconv.ParseUint(hex[1:], 16, 32)
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)
}

func TestPaletteFindsDominantAndSecondHue(t *testing.T) {
	// Mostly dark night scene, a large orange fire and a smaller teal sky.
	data := jpegOf(t, func(x, y int) color.Color {
		switch {
		case x < 70 && y > 30:
			return color.RGBA{230, 110, 20, 255} // orange
		case y < 20:
			return color.RGBA{20, 150, 160, 255} // teal
		default:
			return color.RGBA{12, 12, 16, 255} // near black
		}
	})
	p, err := Palette(data)
	if err != nil || len(p) != 2 {
		t.Fatalf("palette %v %v", p, err)
	}
	r, g, b := rgb(p[0])
	if r < 180 || g > 150 || b > 80 {
		t.Errorf("primary should be orange: %s", p[0])
	}
	r, g, b = rgb(p[1])
	if b < 120 || g < 120 || r > 100 {
		t.Errorf("secondary should be teal: %s", p[1])
	}
}

func TestPaletteLiftsDarkAndHandlesGreyscale(t *testing.T) {
	dark := jpegOf(t, func(int, int) color.Color { return color.RGBA{60, 20, 70, 255} }) // dim purple
	p, _ := Palette(dark)
	if r, g, b := rgb(p[0]); max(r, g, b) < 140 {
		t.Errorf("dark colour not lifted for a black room: %s", p[0])
	}
	grey := jpegOf(t, func(x, _ int) color.Color { v := uint8(x); return color.RGBA{v, v, v, 255} })
	p, err := Palette(grey)
	if err != nil || len(p) != 2 || p[0] != p[1] {
		t.Errorf("greyscale palette %v %v", p, err)
	}
}
