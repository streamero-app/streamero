package library

import "testing"

func TestLanguageMatching(t *testing.T) {
	if !SameLanguage("hr", "hrv") || !SameLanguage("ENG", "en") || !SameLanguage("ger", "deu") || SameLanguage("en", "") || SameLanguage("hr", "sr") {
		t.Fatal("language matching")
	}
	tracks := []AudioStream{{Language: "eng"}, {Language: "hrv"}, {Language: "deu"}}
	if PickAudio(tracks, "hr") != 1 || PickAudio(tracks, "") != 0 || PickAudio(tracks, "fr") != 0 {
		t.Fatal("audio pick")
	}
}

func TestPickSubtitle(t *testing.T) {
	subs := []SubtitleChoice{{Language: "eng", Forced: true}, {Language: "eng"}, {Language: "hr"}}
	hr := Prefs{SubtitleLanguage: "hr"}
	for _, tc := range []struct {
		mode, audio string
		prefs       Prefs
		want        int
	}{
		{"default", "eng", hr, 0},       // forced in the audio language wins
		{"default", "jpn", hr, 2},       // foreign audio → preferred language
		{"default", "hrv", Prefs{}, -1}, // nothing applies
		{"smart", "eng", hr, 2},         // audio differs from preference
		{"smart", "hrv", hr, -1},        // audio already in the preferred language
		{"always", "hrv", hr, 2},        // always the preferred language
		{"forced", "eng", hr, 0},        // forced only
		{"forced", "jpn", hr, -1},       // no forced track for this audio
		{"none", "jpn", hr, -1},         // off
		{"always", "eng", Prefs{}, -1},  // no language chosen
	} {
		p := tc.prefs
		p.SubtitleMode = tc.mode
		if got := PickSubtitle(subs, tc.audio, p); got != tc.want {
			t.Errorf("%s audio=%s: got %d, want %d", tc.mode, tc.audio, got, tc.want)
		}
	}
	if PickSubtitle([]SubtitleChoice{{Forced: true}}, "eng", Prefs{}) != 0 {
		t.Error("forced track without language belongs to the main audio")
	}
}
