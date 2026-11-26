package adult

import (
	"image"
	"image/color"
	"testing"
)

func TestParseFolder(t *testing.T) {
	for _, tc := range []struct {
		parent, name string
		want         Parsed
	}{
		{"Velvetine", "Velvetine - 2024-01-15 - Moonlit Secret [Ana Bee, Cora Dee]",
			Parsed{Kind: "scene", Studio: "Velvetine", Date: "2024-01-15", Year: 2024, Title: "Moonlit Secret", Performers: []string{"Ana Bee", "Cora Dee"}}},
		{"Velvetine", "Velvetine 2024-01-15 Moonlit Secret",
			Parsed{Kind: "scene", Studio: "Velvetine", Date: "2024-01-15", Year: 2024, Title: "Moonlit Secret"}},
		{"", "Velvetine.24.01.15.Moonlit.Secret.Vol.2",
			Parsed{Kind: "scene", Studio: "Velvetine", Date: "2024-01-15", Year: 2024, Title: "Moonlit Secret Vol 2"}},
		// A dot in a folder name is not an extension.
		{"Velvetine", "Behind the scenes vol. 2",
			Parsed{Kind: "scene", Studio: "Velvetine", Title: "Behind the scenes vol. 2"}},
		{"", "Holiday", Parsed{Kind: "scene", Title: "Holiday"}},
	} {
		got := ParseFolder(tc.parent, tc.name)
		if got.Studio != tc.want.Studio || got.Date != tc.want.Date || got.Title != tc.want.Title || got.Year != tc.want.Year ||
			len(got.Performers) != len(tc.want.Performers) {
			t.Errorf("ParseFolder(%q, %q) = %+v, want %+v", tc.parent, tc.name, got, tc.want)
		}
		for i := range tc.want.Performers {
			if got.Performers[i] != tc.want.Performers[i] {
				t.Errorf("ParseFolder(%q) performers = %v", tc.name, got.Performers)
			}
		}
	}
}

func TestGalleryFileKinds(t *testing.T) {
	for name, want := range map[string]bool{"a.JPG": true, "b.jpeg": true, "c.png": true, "d.webp": true, "e.gif": true,
		"f.avif": true, "g.heic": true, "h.HEIF": true, "i.mp4": false, "j.txt": false, "k.zip": false} {
		if IsGalleryImage(name) != want {
			t.Errorf("IsGalleryImage(%q) = %v", name, !want)
		}
	}
	for name, want := range map[string]bool{"a.zip": true, "b.CBZ": true, "c.cbr": false, "d.7z": false, "e.jpg": false} {
		if IsGalleryArchive(name) != want {
			t.Errorf("IsGalleryArchive(%q) = %v", name, !want)
		}
	}
}

func TestDownscaleAndOrient(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})
	small := downscale(src, 50)
	if b := small.Bounds(); b.Dx() != 50 || b.Dy() != 25 {
		t.Fatalf("downscale: %v", b)
	}
	if b := downscale(src, 500).Bounds(); b.Dx() != 200 {
		t.Fatalf("no upscaling: %v", b)
	}
	// Orientation 6 (rotate 90° clockwise): 200×100 becomes 100×200 and the
	// top-left pixel moves to the top-right.
	rot := orient(src, 6)
	if b := rot.Bounds(); b.Dx() != 100 || b.Dy() != 200 {
		t.Fatalf("orient 6 size: %v", b)
	}
	if r, _, _, _ := rot.At(99, 0).RGBA(); r>>8 != 255 {
		t.Fatalf("orient 6 moved the corner wrongly")
	}
	if orient(src, 1) != image.Image(src) {
		t.Fatal("orientation 1 changes nothing")
	}
}

func TestWithoutTags(t *testing.T) {
	got := withoutTags([]string{"Outdoor", "Kitchen", "Studio"}, []string{"kitchen"})
	if len(got) != 2 || got[0] != "Outdoor" || got[1] != "Studio" {
		t.Fatalf("withoutTags: %v", got)
	}
	if c := cleanTags([]string{" a ", "A", "", "b"}); len(c) != 2 || c[0] != "a" || c[1] != "b" {
		t.Fatalf("cleanTags: %v", c)
	}
}
