package metadata

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // artwork is cached as JPEG
	_ "image/png"  // title logos
	"math"
	"os"
	"sort"
)

// Palette extracts two characteristic colours of an image for ambient lighting:
// the most prominent vivid colour and a second one of a clearly different hue.
// Dull, near-black and near-white pixels count less, so the result is the colour
// the eye remembers, not the average grey.
func Palette(data []byte) ([]string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return nil, fmt.Errorf("empty image")
	}
	type bucket struct {
		weight     float64
		r, g, bl   float64
		hueSamples int
	}
	const hueBins = 24
	var bins [hueBins + 1]bucket // last bin: greys
	const grid = 48
	for y := 0; y < grid; y++ {
		for x := 0; x < grid; x++ {
			c := img.At(b.Min.X+x*b.Dx()/grid, b.Min.Y+y*b.Dy()/grid)
			r16, g16, b16, a16 := c.RGBA()
			if a16 < 0x8000 {
				continue // transparent (logos)
			}
			r, g, bl := float64(r16>>8), float64(g16>>8), float64(b16>>8)
			h, s, v := hsv(r, g, bl)
			idx := hueBins
			w := 0.15
			if s > 0.2 && v > 0.15 {
				idx = int(h / 360 * hueBins)
				if idx >= hueBins {
					idx = hueBins - 1
				}
				// Vivid, mid-bright pixels define the mood.
				w = s * (0.5 + 0.5*(1-math.Abs(v-0.6)))
			}
			bk := &bins[idx]
			bk.weight += w
			bk.r += r * w
			bk.g += g * w
			bk.bl += bl * w
			bk.hueSamples++
		}
	}
	type scored struct {
		idx    int
		weight float64
	}
	var order []scored
	for i := 0; i < hueBins; i++ {
		// Merge neighbouring bins so a hue split across a boundary is not penalized.
		w := bins[i].weight + 0.5*(bins[(i+1)%hueBins].weight+bins[(i+hueBins-1)%hueBins].weight)
		if bins[i].weight > 0 {
			order = append(order, scored{i, w})
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i].weight > order[j].weight })
	hex := func(bk bucket) string {
		if bk.weight == 0 {
			return ""
		}
		return liftHex(bk.r/bk.weight, bk.g/bk.weight, bk.bl/bk.weight)
	}
	var out []string
	if len(order) > 0 {
		first := order[0].idx
		out = append(out, hex(bins[first]))
		for _, o := range order[1:] {
			d := (o.idx - first + hueBins) % hueBins
			if d >= 4 && d <= hueBins-4 { // at least 60° apart
				out = append(out, hex(bins[o.idx]))
				break
			}
		}
	}
	if len(out) == 0 && bins[hueBins].weight > 0 {
		out = append(out, hex(bins[hueBins])) // a black-and-white image
	}
	if len(out) == 1 {
		out = append(out, out[0])
	}
	return out, nil
}

// PaletteFile reads an image file and extracts its palette.
func PaletteFile(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: cache path built from a UUID
	if err != nil {
		return nil, err
	}
	return Palette(data)
}

func hsv(r, g, b float64) (h, s, v float64) {
	r, g, b = r/255, g/255, b/255
	mx := math.Max(r, math.Max(g, b))
	mn := math.Min(r, math.Min(g, b))
	v = mx
	d := mx - mn
	if mx > 0 {
		s = d / mx
	}
	switch {
	case d == 0:
		h = 0
	case mx == r:
		h = math.Mod((g-b)/d, 6) * 60
	case mx == g:
		h = ((b-r)/d + 2) * 60
	default:
		h = ((r-g)/d + 4) * 60
	}
	if h < 0 {
		h += 360
	}
	return h, s, v
}

// liftHex keeps the hue but raises dark colours so they read as light on a black room.
func liftHex(r, g, b float64) string {
	h, s, v := hsv(r, g, b)
	if v < 0.55 {
		v = 0.55 + v*0.3
	}
	s = math.Min(1, s*1.1)
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var rr, gg, bb float64
	switch {
	case h < 60:
		rr, gg, bb = c, x, 0
	case h < 120:
		rr, gg, bb = x, c, 0
	case h < 180:
		rr, gg, bb = 0, c, x
	case h < 240:
		rr, gg, bb = 0, x, c
	case h < 300:
		rr, gg, bb = x, 0, c
	default:
		rr, gg, bb = c, 0, x
	}
	to := func(f float64) int { return int(math.Round((f + m) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", to(rr), to(gg), to(bb))
}
