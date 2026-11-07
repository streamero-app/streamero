package library

import (
	"fmt"
	"testing"
)

// Golden cases for movie/series names. Grows with every naming bug report (PLAN: 300+).
func TestParseNameGolden(t *testing.T) {
	type want struct {
		title string
		year  int
		label string
		ids   string
	}
	cases := map[string]want{
		"The Matrix (1999)":                          {"The Matrix", 1999, "", ""},
		"Big Test Movie (2021) - 2160p":              {"Big Test Movie", 2021, "2160p", ""},
		"Blade Runner (1982) - Final Cut":            {"Blade Runner", 1982, "Final Cut", ""},
		"Dune (2021) [tmdbid-438631]":                {"Dune", 2021, "", "tmdb=438631"},
		"Dune (2021) {tmdb-438631}":                  {"Dune", 2021, "", "tmdb=438631"},
		"Inception (2010) [imdbid-tt1375666]":        {"Inception", 2010, "", "imdb=tt1375666"},
		"Inception (2010) tt1375666":                 {"Inception", 2010, "", "imdb=tt1375666"},
		"Some Show (2019) [tvdbid-12345]":            {"Some Show", 2019, "", "tvdb=12345"},
		"The.Dark.Knight.2008.1080p.BluRay.x264-GRP": {"The Dark Knight", 2008, "", ""},
		"Parasite.2019.KOREAN.2160p.UHD.BluRay.x265": {"Parasite", 2019, "", ""},
		"Oppenheimer 2023 2160p WEB-DL DDP5.1 Atmos": {"Oppenheimer", 2023, "", ""},
		"1917 (2019)":                                      {"1917", 2019, "", ""},
		"2001 A Space Odyssey 1968":                        {"2001 A Space Odyssey", 1968, "", ""},
		"Blade_Runner_2049_2017_1080p":                     {"Blade Runner 2049", 2017, "", ""},
		"Amélie (2001)":                                    {"Amélie", 2001, "", ""},
		"Kum (2023)":                                       {"Kum", 2023, "", ""},
		"Movie Without Year":                               {"Movie Without Year", 0, "", ""},
		"Movie - Director's Cut":                           {"Movie", 0, "Director's Cut", ""},
		"Spider-Man Into the Spider-Verse (2018)":          {"Spider-Man Into the Spider-Verse", 2018, "", ""},
		"Mission Impossible - Fallout (2018)":              {"Mission Impossible - Fallout", 2018, "", ""},
		"HEVC Ten Bit (2022) - 2160p":                      {"HEVC Ten Bit", 2022, "2160p", ""},
		"Alien.1979.Directors.Cut.REMASTERED.1080p.BluRay": {"Alien", 1979, "", ""},
		"The Office (US) (2005)":                           {"The Office (US)", 2005, "", ""},
		"Test Show (2020)":                                 {"Test Show", 2020, "", ""},
		"[Group] Anime Movie (2020) [1080p]":               {"[Group] Anime Movie", 2020, "", ""},
		"WALL-E.2008.720p.HDTV":                            {"WALL-E", 2008, "", ""},
		"Up 2009":                                          {"Up", 2009, "", ""},
		"300.2006.1080p":                                   {"300", 2006, "", ""},
	}
	for in, w := range cases {
		p := ParseName(in)
		ids := ""
		for _, k := range []string{"imdb", "tmdb", "tvdb"} {
			if v, ok := p.ProviderIDs[k]; ok {
				ids += fmt.Sprintf("%s=%s", k, v)
			}
		}
		if p.Title != w.title || p.Year != w.year || p.Label != w.label || ids != w.ids {
			t.Errorf("%q\n got  {%q %d %q %q}\n want {%q %d %q %q}", in, p.Title, p.Year, p.Label, ids, w.title, w.year, w.label, w.ids)
		}
	}
}

func TestParseEpisodeGolden(t *testing.T) {
	type want struct {
		ok             bool
		season, ep, to int
		daily, abs     bool
	}
	cases := map[string]want{
		"Test Show S01E01":                      {true, 1, 1, 0, false, false},
		"Test Show S01E05-E06":                  {true, 1, 5, 6, false, false},
		"Test Show S01E05E06":                   {true, 1, 5, 6, false, false},
		"Test.Show.s02e10.720p.WEB":             {true, 2, 10, 0, false, false},
		"Show - S2023E14 - Title":               {true, 2023, 14, 0, false, false},
		"Test Show S00E01":                      {true, 0, 1, 0, false, false},
		"show.1x02.hdtv":                        {true, 1, 2, 0, false, false},
		"show 3x10-3x11":                        {true, 3, 10, 11, false, false},
		"Daily Show 2024-03-15":                 {true, 2024, 315, 0, true, false},
		"Daily.Show.2024.03.15.Guest":           {true, 2024, 315, 0, true, false},
		"[Group] Anime Absolute - 101 [1080p]":  {true, -1, 101, 0, false, true},
		"[SubsPlease] One Piece - 1085 (1080p)": {true, -1, 1085, 0, false, true},
		"Anime - Ep 12":                         {true, -1, 12, 0, false, true},
		"Random Video":                          {false, 0, 0, 0, false, false},
		"Movie (2021)":                          {false, 0, 0, 0, false, false},
	}
	for in, w := range cases {
		e, ok := ParseEpisode(in)
		if ok != w.ok || (ok && (e.Season != w.season || e.Episode != w.ep || e.EpisodeEnd != w.to || e.Daily != w.daily || e.Absolute != w.abs)) {
			t.Errorf("%q: got ok=%v %+v, want %+v", in, ok, e, w)
		}
	}
}

func TestSeasonDirsExtrasAndIgnores(t *testing.T) {
	for name, want := range map[string]int{"Season 01": 1, "season 2": 2, "Season.10": 10, "S03": 3, "Specials": 0, "Sezona 4": 4, "Staffel 5": 5} {
		if got, ok := SeasonFromDir(name); !ok || got != want {
			t.Errorf("SeasonFromDir(%q) = %d, %v; want %d", name, got, ok, want)
		}
	}
	if _, ok := SeasonFromDir("Extras"); ok {
		t.Error("Extras parsed as a season")
	}
	for _, n := range []string{"Movie-trailer.mkv", "Movie.sample.mkv", "sample.mkv", "Movie-behindthescenes.mp4", "Movie - featurette2.mkv"} {
		if !IsExtraFile(n) {
			t.Errorf("%q not detected as extra", n)
		}
	}
	for _, n := range []string{"The Sample Movie (2020).mkv", "Trailer Park Boys S01E01.mkv"} {
		if IsExtraFile(n) {
			t.Errorf("%q wrongly detected as extra", n)
		}
	}
	for _, n := range []string{".DS_Store", "@eaDir", "#recycle", "$RECYCLE.BIN", "lost+found"} {
		if !IgnoredName(n) {
			t.Errorf("%q not ignored", n)
		}
	}
	if SortTitle("The Matrix") != "matrix" || SortTitle("A") != "a" || SortTitle("Up") != "up" {
		t.Error("sort title")
	}
}
