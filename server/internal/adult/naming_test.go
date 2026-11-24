package adult

import (
	"reflect"
	"testing"
)

func TestParseFile(t *testing.T) {
	cases := []struct {
		folder, file string
		want         Parsed
	}{
		{"Studio A", "Studio A - 2024-01-15 - Scene Title [Performer One, Performer Two] WEBDL-1080p.mp4",
			Parsed{Kind: "scene", Studio: "Studio A", Date: "2024-01-15", Year: 2024, Title: "Scene Title", Performers: []string{"Performer One", "Performer Two"}}},
		{"Studio A", "Studio A - 2024-01-15 - Scene Title WEBDL-1080p.mkv",
			Parsed{Kind: "scene", Studio: "Studio A", Date: "2024-01-15", Year: 2024, Title: "Scene Title"}},
		{"Studio A", "Studio A 2024-01-15 Scene Title Performer One, Performer Two.mp4",
			Parsed{Kind: "scene", Studio: "Studio A", Date: "2024-01-15", Year: 2024, Title: "Scene Title", Performers: []string{"Performer One", "Performer Two"}}},
		{"Studio A", "Studio A 2024-01-15 Scene Title.mp4",
			Parsed{Kind: "scene", Studio: "Studio A", Date: "2024-01-15", Year: 2024, Title: "Scene Title"}},
		{"Studio A", "Studio A 2024-01-15 Scene Title - Performer One & Performer Two Bluray-2160p.mp4",
			Parsed{Kind: "scene", Studio: "Studio A", Date: "2024-01-15", Year: 2024, Title: "Scene Title", Performers: []string{"Performer One", "Performer Two"}}},
		{"", "StudioB.24.03.09.Some.Scene.Name.XXX.1080p.HEVC.mp4",
			Parsed{Kind: "scene", Studio: "StudioB", Date: "2024-03-09", Year: 2024, Title: "Some Scene Name"}},
		{"Movie Title (2019)", "Movie Title (2019) WEBDL-1080p.mkv",
			Parsed{Kind: "movie", Year: 2019, Title: "Movie Title"}},
		{"Movie Title (2019)", "movie.mkv",
			Parsed{Kind: "movie", Year: 2019, Title: "Movie Title"}},
		{"Some Studio", "random clip.mp4",
			Parsed{Kind: "scene", Studio: "Some Studio", Title: "random clip"}},
		{"", "2023-13-45 not a date.mp4",
			Parsed{Kind: "scene", Title: "2023-13-45 not a date"}},
	}
	for _, c := range cases {
		got := ParseFile(c.folder, c.file)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseFile(%q, %q)\n got %+v\nwant %+v", c.folder, c.file, got, c.want)
		}
	}
}
