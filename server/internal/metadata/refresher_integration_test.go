package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/db"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
)

// fakeTMDb serves just enough of the TMDb API for the refresher.
func fakeTMDb(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("api_key") != "goodkey" && r.Header.Get("Authorization") != "Bearer "+strings.Repeat("t", 50) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/3/configuration", auth(func(w http.ResponseWriter, _ *http.Request) { write(w, map[string]any{}) }))
	mux.HandleFunc("/3/search/movie", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "Big Test Movie" {
			write(w, map[string]any{"results": []map[string]any{{"id": 111, "title": "Big Test Movie"}}})
			return
		}
		write(w, map[string]any{"results": []any{}})
	}))
	mux.HandleFunc("/3/movie/111", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("language") == "hr-HR" {
			write(w, map[string]any{"id": 111, "title": "Veliki testni film", "original_title": "Big Test Movie", "overview": "",
				"release_date": "2021-05-01", "vote_average": 7.4, "poster_path": "/p.jpg", "backdrop_path": "/b.jpg", "imdb_id": "tt0000111",
				"genres":                []map[string]any{{"name": "Drama"}},
				"credits":               map[string]any{"cast": []map[string]any{{"id": 501, "name": "Ana Glumica", "character": "Ivana", "profile_path": "/ana.jpg"}}, "crew": []map[string]any{{"name": "Redatelj Ivo", "job": "Director"}, {"name": "Grip", "job": "Grip"}}},
				"belongs_to_collection": map[string]any{"id": 9001, "name": "Testni filmovi – kolekcija", "poster_path": "/cp.jpg", "backdrop_path": "/cb.jpg"},
				"images":                map[string]any{"logos": []map[string]any{{"file_path": "/logo-en.png", "iso_639_1": "en", "vote_average": 5}, {"file_path": "/logo-hr.png", "iso_639_1": "hr", "vote_average": 1}}},
				"release_dates":         map[string]any{"results": []map[string]any{{"iso_3166_1": "HR", "release_dates": []map[string]any{{"certification": "12"}}}}}})
			return
		}
		write(w, map[string]any{"id": 111, "title": "Big Test Movie", "overview": "An English overview.", "tagline": "Big."})
	}))
	mux.HandleFunc("/3/movie/222", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"id": 222, "title": "Dina", "overview": "Pustinja.", "release_date": "2021-09-15"})
	}))
	mux.HandleFunc("/3/search/tv", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"results": []map[string]any{{"id": 333, "name": "Test Show"}}})
	}))
	mux.HandleFunc("/3/tv/333", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"id": 333, "name": "Testna serija", "overview": "Serija.", "first_air_date": "2020-01-01",
			"poster_path": "/sp.jpg", "external_ids": map[string]any{"imdb_id": "tt333", "tvdb_id": 9},
			"content_ratings": map[string]any{"results": []map[string]any{{"iso_3166_1": "US", "rating": "TV-14"}}},
			"seasons":         []map[string]any{{"season_number": 1, "poster_path": "/s1.jpg"}}})
	}))
	mux.HandleFunc("/3/tv/333/season/1", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"episodes": []map[string]any{{"episode_number": 1, "name": "Pilot", "overview": "Početak.", "air_date": "2020-01-01", "still_path": "/e1.jpg"}}})
	}))
	mux.HandleFunc("/img/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("\xff\xd8\xffjpeg")) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("STREAMERO_TEST_DB_URL")
	if url == "" {
		t.Skip("STREAMERO_TEST_DB_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func touch(t *testing.T, root string, files ...string) {
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRefreshLibraryIntegration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tm := fakeTMDb(t)
	root := t.TempDir()
	media := filepath.Join(root, "media")
	touch(t, media,
		"Big Test Movie (2021)/Big Test Movie (2021).mkv",
		"Dune (2021) [tmdbid-222]/Dune (2021).mkv",
		"Unknown Thing (1999)/Unknown Thing (1999).mkv",
		"Test Show (2020)/Season 01/Test Show S01E01.mkv",
	)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &library.Service{Pool: pool, Roots: library.Roots{root}, Enqueue: func(context.Context, uuid.UUID) error { return nil }}
	scanner := &library.Scanner{Pool: pool, Roots: library.Roots{root}, Log: log,
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv"}, nil
		}}
	lib, err := svc.Create(ctx, "Mixed", "mixed", []string{media})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(context.Background(), lib.ID) })
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}

	// A settings store isolated from other tests: unique key namespace is not possible,
	// so save and restore the real settings around the test.
	st := settings.New(pool)
	old, _ := LoadSettings(ctx, st)
	t.Cleanup(func() { _ = SaveSettings(context.Background(), st, old) })
	if err := SaveSettings(ctx, st, Settings{TMDbKey: "goodkey", Language: "hr-HR", Country: "HR"}); err != nil {
		t.Fatal(err)
	}
	images := filepath.Join(root, "images")
	r := &Refresher{Pool: pool, Settings: st, ImageDir: images, Log: log,
		NewClient: func(key string) *TMDb { return &TMDb{Key: key, BaseURL: tm.URL + "/3", ImageBase: tm.URL + "/img"} }}
	n, err := r.RefreshLibrary(ctx, lib.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("refreshed %d items, want 3 (one has no match)", n)
	}

	items, _, err := svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 10, 0, library.ItemFilter{})
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]library.Item{}
	for _, it := range items {
		byTitle[it.Title] = it
	}
	movie, ok := byTitle["Veliki testni film"]
	if !ok || !movie.HasPoster || movie.Rating == nil || *movie.Rating < 7 {
		t.Fatalf("movie: %+v (titles %v)", movie, byTitle)
	}
	m, _ := svc.GetMetadata(ctx, movie.ID)
	if m.Overview != "An English overview." || m.OfficialRating != "12" || len(m.Genres) != 1 || !m.HasBackdrop ||
		len(m.Cast) != 1 || m.Cast[0].Role != "Ivana" || len(m.Crew) != 1 || m.Crew[0].Role != "Director" || m.OriginalTitle != "Big Test Movie" {
		t.Fatalf("movie metadata: %+v", m)
	}
	if _, err := os.Stat(ImagePath(images, movie.ID, "poster")); err != nil {
		t.Fatalf("poster not cached: %v", err)
	}
	// Title logo (UI language preferred) and the actor's photo.
	if !movie.HasLogo || !m.HasLogo {
		t.Fatalf("logo flag: %+v / %+v", movie, m)
	}
	if _, err := os.Stat(ImagePath(images, movie.ID, "logo")); err != nil {
		t.Fatalf("logo not cached: %v", err)
	}
	if m.Cast[0].ImageID == nil || *m.Cast[0].ImageID != 501 {
		t.Fatalf("cast photo: %+v", m.Cast[0])
	}
	if _, err := os.Stat(PersonImagePath(images, 501)); err != nil {
		t.Fatalf("person photo not cached: %v", err)
	}
	// TMDb box set: collection created once, movie linked, artwork cached.
	cols, err := svc.ItemCollections(ctx, movie.ID)
	if err != nil || len(cols) != 1 || cols[0].Name != "Testni filmovi – kolekcija" || !cols[0].HasPoster || cols[0].TMDbID == nil {
		t.Fatalf("collection: %+v %v", cols, err)
	}
	t.Cleanup(func() { _ = svc.DeleteCollection(context.Background(), cols[0].ID) })
	if _, ok := byTitle["Dina"]; !ok {
		t.Fatalf("tmdb id from folder name not used: %v", byTitle)
	}
	unknown := byTitle["Unknown Thing"]
	if m, _ := svc.GetMetadata(ctx, unknown.ID); !strings.Contains(m.Error, "no TMDb match") {
		t.Fatalf("unmatched item error: %q", m.Error)
	}
	show, ok := byTitle["Testna serija"]
	if !ok {
		t.Fatalf("series not matched: %v", byTitle)
	}
	_, _, seasons, _ := svc.GetItem(ctx, show.ID, uuid.Nil)
	_, _, eps, _ := svc.GetItem(ctx, seasons[0].ID, uuid.Nil)
	if !seasons[0].HasPoster || len(eps) != 1 || eps[0].Title != "Pilot" || !eps[0].HasPoster {
		t.Fatalf("episodes: %+v / %+v", seasons, eps)
	}
	if m, _ := svc.GetMetadata(ctx, show.ID); m.OfficialRating != "TV-14" {
		t.Fatalf("series rating fallback: %+v", m)
	}

	// A re-scan keeps the metadata title; a second non-forced refresh does nothing.
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	items, _, _ = svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 10, 0, library.ItemFilter{})
	found := false
	for _, it := range items {
		found = found || it.Title == "Veliki testni film"
	}
	if !found {
		t.Fatal("metadata title lost after re-scan")
	}
	if n, _ := r.RefreshLibrary(ctx, lib.ID, false); n != 0 {
		t.Fatalf("second refresh touched %d items", n)
	}

	// Removing the library leaves orphaned artwork that cleanup removes.
	if err := svc.Delete(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.CleanupImages(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(images, movie.ID.String())); !os.IsNotExist(err) {
		t.Fatalf("artwork of deleted item kept: %v", err)
	}
}

func TestVerifyKey(t *testing.T) {
	tm := fakeTMDb(t)
	ctx := context.Background()
	if err := (&TMDb{Key: "goodkey", BaseURL: tm.URL + "/3"}).Verify(ctx); err != nil {
		t.Fatalf("v3 key: %v", err)
	}
	if err := (&TMDb{Key: strings.Repeat("t", 50), BaseURL: tm.URL + "/3"}).Verify(ctx); err != nil {
		t.Fatalf("v4 token: %v", err)
	}
	if err := (&TMDb{Key: "bad", BaseURL: tm.URL + "/3"}).Verify(ctx); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("bad key: %v", err)
	}
}
