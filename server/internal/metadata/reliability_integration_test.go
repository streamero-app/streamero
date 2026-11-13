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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
)

func fastRetries(t *testing.T) {
	t.Helper()
	old := retryBase
	retryBase = time.Millisecond
	t.Cleanup(func() { retryBase = old })
}

// useKey stores a TMDb key for the test and restores the real settings afterwards.
func useKey(t *testing.T, st *settings.Store) {
	t.Helper()
	ctx := context.Background()
	old, _ := LoadSettings(ctx, st)
	t.Cleanup(func() { _ = SaveSettings(context.Background(), st, old) })
	if err := SaveSettings(ctx, st, Settings{TMDbKey: "goodkey", Language: "en-US", Country: "US"}); err != nil {
		t.Fatal(err)
	}
}

// New episodes and seasons of a series that already has metadata get theirs without
// fetching the whole series again; a re-scan keeps the TMDb IDs.
func TestNewSeasonGetsMetadataIntegration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tm := fakeTMDb(t)
	root := t.TempDir()
	media := filepath.Join(root, "media")
	touch(t, media, "Test Show (2020)/Season 01/Test Show S01E01.mkv")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &library.Service{Pool: pool, Roots: library.Roots{root}, Enqueue: func(context.Context, uuid.UUID) error { return nil }}
	scanner := &library.Scanner{Pool: pool, Roots: library.Roots{root}, Log: log,
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv"}, nil
		}}
	lib, err := svc.Create(ctx, "Shows", "shows", []string{media})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(context.Background(), lib.ID) })
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	st := settings.New(pool)
	useKey(t, st)
	r := &Refresher{Pool: pool, Settings: st, ImageDir: filepath.Join(root, "images"), Log: log,
		NewClient: func(key string) *TMDb { return &TMDb{Key: key, BaseURL: tm.URL + "/3", ImageBase: tm.URL + "/img"} }}
	if _, err := r.RefreshLibrary(ctx, lib.ID, false); err != nil {
		t.Fatal(err)
	}
	var seriesID uuid.UUID
	var ids map[string]string
	if err := pool.QueryRow(ctx, `SELECT id, provider_ids FROM items WHERE library_id = $1 AND kind = 'series'`, lib.ID).Scan(&seriesID, &ids); err != nil {
		t.Fatal(err)
	}
	if ids["tmdb"] != "333" || ids["tvdb"] != "9" {
		t.Fatalf("series ids: %v", ids)
	}

	// A new season and a new episode TMDb does not list yet arrive.
	touch(t, media, "Test Show (2020)/Season 02/Test Show S02E01.mkv", "Test Show (2020)/Season 01/Test Show S01E02.mkv")
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT provider_ids FROM items WHERE id = $1`, seriesID).Scan(&ids); err != nil || ids["tmdb"] != "333" {
		t.Fatalf("re-scan dropped the series ids: %v %v", ids, err)
	}
	before := seriesCalls.Load()
	n, err := r.RefreshLibrary(ctx, lib.ID, false)
	if err != nil || n != 2 {
		t.Fatalf("refresh of new seasons: %d %v", n, err)
	}
	if seriesCalls.Load() != before {
		t.Fatal("the whole series was fetched again for a new season")
	}
	var title string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(e.metadata_title, '') FROM items e WHERE e.library_id = $1 AND e.kind = 'episode'
		AND e.season_number = 2 AND e.episode_number = 1`, lib.ID).Scan(&title); err != nil || title != "Second Season Opener" {
		t.Fatalf("new season episode: %q %v", title, err)
	}
	var seasonPoster bool
	_ = pool.QueryRow(ctx, `SELECT has_poster FROM items WHERE library_id = $1 AND kind = 'season' AND season_number = 2`, lib.ID).Scan(&seasonPoster)
	if !seasonPoster {
		t.Fatal("new season has no poster")
	}
	// The episode TMDb does not list is not asked for again on every run.
	if n, err := r.RefreshLibrary(ctx, lib.ID, false); err != nil || n != 0 {
		t.Fatalf("second refresh: %d %v", n, err)
	}
}

// While TMDb is down, titles stay pending (no "no metadata" mark) and the error says
// the job should be retried.
func TestRefreshKeepsTitlesPendingWhileTMDbIsDownIntegration(t *testing.T) {
	fastRetries(t)
	pool := testPool(t)
	ctx := context.Background()
	var calls atomic.Int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)
	root := t.TempDir()
	media := filepath.Join(root, "media")
	touch(t, media, "Some Movie (2020)/Some Movie (2020).mkv")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &library.Service{Pool: pool, Roots: library.Roots{root}, Enqueue: func(context.Context, uuid.UUID) error { return nil }}
	scanner := &library.Scanner{Pool: pool, Roots: library.Roots{root}, Log: log,
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv"}, nil
		}}
	lib, err := svc.Create(ctx, "Down", "movies", []string{media})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(context.Background(), lib.ID) })
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	st := settings.New(pool)
	useKey(t, st)
	r := &Refresher{Pool: pool, Settings: st, Log: log,
		NewClient: func(key string) *TMDb { return &TMDb{Key: key, BaseURL: down.URL + "/3"} }}
	_, err = r.RefreshLibrary(ctx, lib.ID, false)
	if !errors.Is(err, ErrTemporary) {
		t.Fatalf("refresh while TMDb is down: %v", err)
	}
	if int(calls.Load()) != retryAttempts {
		t.Fatalf("requests: %d, want %d", calls.Load(), retryAttempts)
	}
	var refreshed *time.Time
	var msg *string
	if err := pool.QueryRow(ctx, `SELECT metadata_refreshed_at, metadata_error FROM items WHERE library_id = $1`, lib.ID).Scan(&refreshed, &msg); err != nil {
		t.Fatal(err)
	}
	if refreshed != nil || msg != nil {
		t.Fatalf("title marked as without metadata during an outage: %v %v", refreshed, msg)
	}
}

func TestTMDbRetriesRateLimits(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	t.Cleanup(srv.Close)
	if err := (&TMDb{Key: "k", BaseURL: srv.URL}).Verify(context.Background()); err != nil || calls.Load() != 3 {
		t.Fatalf("after two 429s: %v (%d calls)", err, calls.Load())
	}
	if retryAfter("7") != 7*time.Second || retryAfter("Wed, 21 Oct 2015 07:28:00 GMT") != 0 {
		t.Fatal("Retry-After parsing")
	}
}

func TestSearchFallbackNeedsCloseYearOrSameTitle(t *testing.T) {
	results := map[string][]map[string]any{
		"Remake":   {{"id": 1, "title": "Remake Of Something", "release_date": "1985-01-01"}},
		"Off By":   {{"id": 2, "title": "Off By One Film", "release_date": "2021-03-01"}},
		"Amelie":   {{"id": 3, "title": "Amélie", "release_date": "1990-01-01"}},
		"No Year":  {{"id": 4, "title": "Anything", "release_date": "1950-01-01"}},
		"Original": {{"id": 5, "title": "Localized", "original_title": "Original", "release_date": "1970-01-01"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("year") != "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}}) // the year filter finds nothing
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results[q.Get("query")]})
	}))
	t.Cleanup(srv.Close)
	c := &TMDb{Key: "k", BaseURL: srv.URL}
	ctx := context.Background()
	for _, tc := range []struct {
		title string
		year  int
		want  int
	}{
		{"Remake", 2020, 0},   // 35 years off and another title: not a match
		{"Off By", 2020, 2},   // one year off
		{"Amelie", 2001, 3},   // same title without accents
		{"No Year", 0, 4},     // no year to compare: the best hit
		{"Original", 2001, 5}, // original title matches
	} {
		id, err := c.Search(ctx, "movie", tc.title, tc.year, "en-US")
		if tc.want == 0 {
			if !errors.Is(err, errNotFound) {
				t.Errorf("%s: %d %v, want not found", tc.title, id, err)
			}
			continue
		}
		if err != nil || id != tc.want {
			t.Errorf("%s: %d %v, want %d", tc.title, id, err, tc.want)
		}
	}
}

// Cached photos of people nobody credits and artwork of deleted collections are pruned.
func TestCleanupImagesPrunesPeopleAndCollectionsIntegration(t *testing.T) {
	pool := testPool(t)
	dir := t.TempDir()
	write := func(p string, age time.Duration) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("jpg"), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	oldPerson := PersonImagePath(dir, 987654321)
	newPerson := PersonImagePath(dir, 987654322)
	write(oldPerson, 2*time.Hour)
	write(newPerson, time.Minute) // a refresh may be about to store its credit
	goneCollection := CollectionImagePath(dir, uuid.New(), "poster")
	write(goneCollection, 2*time.Hour)
	r := &Refresher{Pool: pool, ImageDir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := r.CleanupImages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPerson); !os.IsNotExist(err) {
		t.Fatalf("unreferenced person photo kept: %v", err)
	}
	if _, err := os.Stat(newPerson); err != nil {
		t.Fatalf("fresh person photo removed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(goneCollection)); !os.IsNotExist(err) {
		t.Fatalf("artwork of a deleted collection kept: %v", err)
	}
}
