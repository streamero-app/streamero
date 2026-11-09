package requests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBazarrSearchDownloadAndTargets(t *testing.T) {
	var downloads, patches []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bazarr/api/system/status":
			if r.Header.Get("X-API-KEY") != "bk" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"bazarr_version":"1.5.1"}}`))
		case r.URL.Path == "/bazarr/api/providers/movies" && r.Method == http.MethodGet:
			if r.URL.Query().Get("radarrid") != "7" {
				t.Errorf("radarrid %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"provider":"opensubtitlescom","language":"hr","forced":"False","hearing_impaired":"True","score":92.5,
				"release_info":["Film.2021.1080p.WEB"],"subtitle":"abc123","original_format":"False"}]}`))
		case r.URL.Path == "/bazarr/api/providers/movies" && r.Method == http.MethodPost:
			downloads = append(downloads, r.URL.RawQuery)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch:
			patches = append(patches, r.URL.Path+"?"+r.URL.RawQuery)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/radarr/api/v3/movie":
			_, _ = w.Write([]byte(`[{"id":7,"tmdbId":603}]`))
		case r.URL.Path == "/sonarr/api/v3/series":
			_, _ = w.Write([]byte(`[{"id":3,"tvdbId":99}]`))
		case r.URL.Path == "/sonarr/api/v3/episode":
			_, _ = w.Write([]byte(`[{"id":40,"seasonNumber":1,"episodeNumber":1},{"id":41,"seasonNumber":1,"episodeNumber":2}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s := Settings{BazarrURL: srv.URL + "/bazarr", BazarrKey: "bk", RadarrURL: srv.URL + "/radarr", RadarrKey: "r", SonarrURL: srv.URL + "/sonarr", SonarrKey: "s"}
	c := &Client{}
	ctx := context.Background()
	if v, err := c.BazarrVersion(ctx, s); err != nil || v != "1.5.1" {
		t.Fatalf("version: %s %v", v, err)
	}
	movie, err := c.FindSubtitleTarget(ctx, s, "movie", map[string]string{"tmdb": "603"}, 0, 0)
	if err != nil || movie.ID != 7 {
		t.Fatalf("movie target: %+v %v", movie, err)
	}
	ep, err := c.FindSubtitleTarget(ctx, s, "episode", map[string]string{"tvdb": "99"}, 1, 2)
	if err != nil || ep.ID != 41 || ep.SeriesID != 3 {
		t.Fatalf("episode target: %+v %v", ep, err)
	}
	offers, err := c.SearchSubtitles(ctx, s, movie)
	if err != nil || len(offers) != 1 || !offers[0].HearingImpair || offers[0].Forced || offers[0].Subtitle != "abc123" {
		t.Fatalf("offers: %+v %v", offers, err)
	}
	if err := c.DownloadSubtitle(ctx, s, movie, offers[0]); err != nil || len(downloads) != 1 {
		t.Fatalf("download: %v %v", err, downloads)
	}
	if err := c.SearchMissing(ctx, s, ep); err != nil || len(patches) != 1 || patches[0] != "/bazarr/api/series?action=search-missing&seriesid=3" {
		t.Fatalf("search missing: %v %v", err, patches)
	}
}
