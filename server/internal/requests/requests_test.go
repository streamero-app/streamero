package requests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func fakeSeerr(t *testing.T) (*httptest.Server, *[]map[string]any) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "k" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/api/v1/search":
			if r.URL.Query().Get("query") != "big buck" {
				t.Errorf("query: %q (raw %q)", r.URL.Query().Get("query"), r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"page":1,"totalPages":1,"results":[
				{"id":10378,"mediaType":"movie","title":"Big Buck Bunny","releaseDate":"2008-05-20","posterPath":"/p.jpg","mediaInfo":{"status":5}},
				{"id":1,"mediaType":"person","name":"Someone"},
				{"id":42,"mediaType":"tv","name":"Caminandes","firstAirDate":"2013-01-01"}]}`))
		case "/api/v1/discover/trending":
			_, _ = w.Write([]byte(`{"page":1,"totalPages":3,"results":[{"id":7,"mediaType":"movie","title":"New","mediaInfo":{"status":2}}]}`))
		case "/api/v1/request":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			requests = append(requests, b)
			_, _ = w.Write([]byte(`{"status":2,"media":{"status":3}}`))
		case "/api/v1/status":
			_, _ = w.Write([]byte(`{"version":"2.7.3"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func TestSeerrSearchTrendingAndRequest(t *testing.T) {
	srv, reqs := fakeSeerr(t)
	c := &Client{}
	s := Settings{SeerrURL: srv.URL, SeerrKey: "k"}
	ctx := context.Background()
	p, err := c.Search(ctx, s, "big buck", "en", 1)
	if err != nil || len(p.Titles) != 2 {
		t.Fatalf("search: %+v %v", p, err)
	}
	if b := p.Titles[0]; b.TMDbID != 10378 || b.Year != 2008 || b.Status != StatusAvailable || b.PosterPath != "/p.jpg" {
		t.Fatalf("movie: %+v", b)
	}
	if tv := p.Titles[1]; tv.MediaType != "tv" || tv.Title != "Caminandes" || tv.Status != StatusNone {
		t.Fatalf("series: %+v", tv)
	}
	tr, err := c.Trending(ctx, s, "", 1)
	if err != nil || tr.TotalPages != 3 || tr.Titles[0].Status != StatusPending {
		t.Fatalf("trending: %+v %v", tr, err)
	}
	st, err := c.Request(ctx, s, "tv", 42)
	if err != nil || st != StatusProcessing || (*reqs)[0]["seasons"] != "all" || (*reqs)[0]["mediaId"] != float64(42) {
		t.Fatalf("request: %s %v %v", st, err, *reqs)
	}
	if v, err := c.Ping(ctx, "Seerr", s.SeerrURL, "k"); err != nil || v != "2.7.3" {
		t.Fatalf("ping: %s %v", v, err)
	}
	if _, err := c.Ping(ctx, "Seerr", s.SeerrURL, "wrong"); err == nil {
		t.Fatal("a wrong key must fail")
	}
	if _, err := c.Search(ctx, Settings{}, "x", "", 1); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unconfigured: %v", err)
	}
}

func TestCalendarMergesRadarrAndSonarr(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/radarr/api/v3/calendar":
			_, _ = w.Write([]byte(`[{"title":"Film","inCinemas":"2026-10-01T00:00:00Z","digitalRelease":"2026-11-05T00:00:00Z",
				"images":[{"coverType":"poster","remoteUrl":"https://img/p.jpg"}]}]`))
		case "/sonarr/api/v3/calendar":
			_, _ = w.Write([]byte(`[{"title":"Pilot","seasonNumber":1,"episodeNumber":1,"airDateUtc":"2026-11-03T20:00:00Z","series":{"title":"Show"}}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	got, err := (&Client{}).Calendar(context.Background(), Settings{RadarrURL: srv.URL + "/radarr", RadarrKey: "a", SonarrURL: srv.URL + "/sonarr", SonarrKey: "b"},
		from, from.AddDate(0, 0, 30))
	if err != nil || len(got) != 2 {
		t.Fatalf("calendar: %+v %v", got, err)
	}
	if got[0].Date.Day() != 5 || got[0].PosterURL != "https://img/p.jpg" || got[1].SeriesTitle != "Show" || got[1].Episode != 1 {
		t.Fatalf("entries: %+v", got)
	}
}

func TestCleanURL(t *testing.T) {
	for in, want := range map[string]string{"http://seerr:5055/": "http://seerr:5055", "": "", "https://x.example/base": "https://x.example/base"} {
		if got, err := CleanURL(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"ftp://x", "seerr:5055", "http://user:pw@x"} {
		if _, err := CleanURL(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestLinkFindsTitleInRadarrAndSonarr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/movie":
			_, _ = w.Write([]byte(`[{"titleSlug":"the-matrix-603","tmdbId":603}]`))
		case "/api/v3/series":
			_, _ = w.Write([]byte(`[{"titleSlug":"other","tvdbId":1},{"titleSlug":"caminandes","tvdbId":7,"tmdbId":42}]`))
		}
	}))
	defer srv.Close()
	s := Settings{RadarrURL: srv.URL, RadarrKey: "a", SonarrURL: srv.URL, SonarrKey: "b"}
	c := &Client{}
	if app, u, err := c.Link(context.Background(), s, "movie", map[string]string{"tmdb": "603"}); err != nil || app != "radarr" || u != srv.URL+"/movie/the-matrix-603" {
		t.Fatalf("radarr: %s %s %v", app, u, err)
	}
	if app, u, err := c.Link(context.Background(), s, "series", map[string]string{"tmdb": "42"}); err != nil || app != "sonarr" || u != srv.URL+"/series/caminandes" {
		t.Fatalf("sonarr: %s %s %v", app, u, err)
	}
	if _, _, err := c.Link(context.Background(), s, "movie", map[string]string{"tmdb": "1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}
