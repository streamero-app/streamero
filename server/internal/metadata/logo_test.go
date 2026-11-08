package metadata

import "testing"

func TestLogoPathPrefersUILanguageThenEnglishThenNeutral(t *testing.T) {
	var d Details
	add := func(path, lang string, vote float64) {
		d.Images.Logos = append(d.Images.Logos, struct {
			FilePath string  `json:"file_path"`
			Language string  `json:"iso_639_1"`
			Vote     float64 `json:"vote_average"`
		}{path, lang, vote})
	}
	add("/neutral.png", "", 9)
	add("/en-low.png", "en", 1)
	add("/en-high.png", "en", 4)
	add("/hr.svg", "hr", 9) // SVG is skipped: clients render PNG
	if got := d.LogoPath("hr-HR"); got != "/en-high.png" {
		t.Fatalf("hr-HR: %q", got)
	}
	add("/hr.png", "hr", 2)
	if got := d.LogoPath("hr-HR"); got != "/hr.png" {
		t.Fatalf("hr-HR with hr logo: %q", got)
	}
	if got := (Details{}).LogoPath("en-US"); got != "" {
		t.Fatalf("no logos: %q", got)
	}
	if imageLanguages("hr-HR") != "hr,en,null" || imageLanguages("en-US") != "en,null" {
		t.Fatal("image languages")
	}
}
