package importer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/db"
)

// isolatedPool creates a fresh database: the import reads every item and user, so it
// must not see (or be disturbed by) other packages' integration tests running in parallel.
func isolatedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("STREAMERO_TEST_DB_URL")
	if url == "" {
		t.Skip("STREAMERO_TEST_DB_URL not set")
	}
	ctx := context.Background()
	admin, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "import_test_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") })
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

type idmap map[string]uuid.UUID

func seed(t *testing.T, pool *pgxpool.Pool) idmap {
	t.Helper()
	ctx := context.Background()
	ids := idmap{}
	for _, k := range []string{"ana", "marko", "lib", "dune", "arrival", "show", "s1", "e1", "e2", "other"} {
		ids[k] = uuid.New()
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO users (id, username, password_hash, is_admin) VALUES ($1, 'ana', 'x', true), ($2, 'Marko', 'x', false)`, ids["ana"], ids["marko"])
	exec(`INSERT INTO libraries (id, name, kind, paths) VALUES ($1, 'All', 'mixed', '{/media}')`, ids["lib"])
	item := func(id, parent uuid.UUID, kind, path, title string, year, season, episode any, providers string) {
		var p any
		if parent != uuid.Nil {
			p = parent
		}
		exec(`INSERT INTO items (id, library_id, parent_id, kind, path, title, sort_title, year, season_number, episode_number, provider_ids)
			VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $9, $10)`, id, ids["lib"], p, kind, path, title, year, season, episode, providers)
	}
	file := func(item uuid.UUID, path string) {
		exec(`INSERT INTO media_files (id, item_id, library_id, path, size, mtime, duration_ms) VALUES ($1, $2, $3, $4, 1, now(), 7200000)`,
			uuid.New(), item, ids["lib"], path)
	}
	item(ids["dune"], uuid.Nil, "movie", "/media/movies/Dune (2021)", "Dune", 2021, nil, nil, `{"tmdb":"438631"}`)
	file(ids["dune"], "/media/movies/Dune (2021)/Dune.mkv")
	item(ids["arrival"], uuid.Nil, "movie", "/media/movies/Arrival (2016)", "Arrival", 2016, nil, nil, `{}`)
	file(ids["arrival"], "/media/movies/Arrival (2016)/Arrival.mkv")
	item(ids["other"], uuid.Nil, "movie", "/media/movies/Other", "Other", 2000, nil, nil, `{}`)
	item(ids["show"], uuid.Nil, "series", "/media/tv/Show", "Show", 2019, nil, nil, `{"tvdb":"777"}`)
	item(ids["s1"], ids["show"], "season", "/media/tv/Show/Season 1", "Season 1", nil, 1, nil, `{}`)
	item(ids["e1"], ids["s1"], "episode", "/media/tv/Show/Season 1/S01E01.mkv", "Pilot", nil, 1, 1, `{}`)
	item(ids["e2"], ids["s1"], "episode", "/media/tv/Show/Season 1/S01E02.mkv", "Two", nil, 1, 2, `{}`)
	return ids
}

func source(t *testing.T) (*fakeSource, time.Time, time.Time) {
	f := newFakeSource(t)
	t1 := time.Date(2026, 10, 1, 20, 0, 0, 1234567, time.UTC) // sub-microsecond on purpose
	t2 := time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC)
	f.users = []map[string]any{{"Id": "u-ana", "Name": "Ana"}, {"Id": "u-marko", "Name": "marko"}, {"Id": "u-ghost", "Name": "Ghost"}}
	f.item("", map[string]any{"Id": "ser", "Name": "Show", "Type": "Series", "ProviderIds": map[string]string{"Tvdb": "777"}})
	f.item("u-ana", map[string]any{"Id": "j-dune", "Name": "Dune", "Type": "Movie", "ProductionYear": 2021,
		"ProviderIds": map[string]string{"Tmdb": "438631", "Imdb": "tt1160419"},
		"UserData":    map[string]any{"Played": true, "PlayCount": 2, "IsFavorite": true, "LastPlayedDate": t1}})
	f.item("u-ana", map[string]any{"Id": "j-arrival", "Name": "Arrival", "Type": "Movie", "Path": "/data/movies/Arrival (2016)/Arrival.mkv",
		"UserData": map[string]any{"PlaybackPositionTicks": int64(18_000_000_000), "PlayCount": 0, "LastPlayedDate": t2}})
	f.item("u-ana", map[string]any{"Id": "j-e1", "Name": "Pilot", "Type": "Episode", "SeriesId": "ser", "SeriesName": "Show",
		"ParentIndexNumber": 1, "IndexNumber": 1, "UserData": map[string]any{"Played": true, "PlayCount": 1, "LastPlayedDate": t2}})
	f.item("u-ana", map[string]any{"Id": "j-nope", "Name": "Nope", "Type": "Movie", "ProductionYear": 2022,
		"UserData": map[string]any{"Played": true, "PlayCount": 1}})
	f.item("u-ana", map[string]any{"Id": "j-showfav", "Name": "Show", "Type": "Series", "ProviderIds": map[string]string{"Tvdb": "777"},
		"UserData": map[string]any{"IsFavorite": true}})
	f.item("u-marko", map[string]any{"Id": "j-dune", "Name": "Dune", "Type": "Movie", "ProviderIds": map[string]string{"Tmdb": "438631"},
		"UserData": map[string]any{"PlaybackPositionTicks": int64(600_000_000), "LastPlayedDate": t1}})
	f.item("u-ghost", map[string]any{"Id": "j-dune", "Name": "Dune", "Type": "Movie", "ProviderIds": map[string]string{"Tmdb": "438631"},
		"UserData": map[string]any{"Played": true}})
	// Ana's own playlist (shared with Marko), and an open one everyone sees.
	f.playlists["u-ana"] = []string{"pl-mine", "pl-open"}
	f.playlists["u-marko"] = []string{"pl-mine", "pl-open"}
	f.playlists["u-ghost"] = []string{"pl-open"}
	f.lists["pl-mine"] = map[string]any{"Name": "Weekend", "OpenAccess": false, "Shares": []map[string]any{{"UserId": "u-marko", "CanEdit": false}},
		"Items": []string{"j-arrival", "j-nope", "j-e1"}}
	f.lists["pl-open"] = map[string]any{"Name": "Family", "OpenAccess": true, "Shares": []map[string]any{}, "Items": []string{"j-dune"}}
	return f, t1.Truncate(time.Microsecond), t2
}

func TestImportEndToEndIntegration(t *testing.T) {
	pool := isolatedPool(t)
	ids := seed(t, pool)
	f, t1, t2 := source(t)
	srv := f.start()
	ctx := context.Background()
	runner := &Runner{Pool: pool}
	ana, marko := ids["ana"], ids["marko"]
	opts := Options{Playlists: true, PathMappings: []PathMapping{{From: "/data", To: "/media"}}, Users: []UserMapping{
		{SourceUserID: "u-ana", SourceUserName: "Ana", UserID: &ana},
		{SourceUserID: "u-marko", SourceUserName: "marko", UserID: &marko},
		{SourceUserID: "u-ghost", SourceUserName: "Ghost"},
	}}
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Dry run: reports, writes nothing.
	client := &Client{BaseURL: srv.URL, Key: f.key}
	rep, err := runner.Execute(ctx, client, opts, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Users) != 2 || rep.Users[0].Matched != 4 || rep.Users[0].Unmatched != 1 || rep.Unmatched != 1 ||
		rep.PlaylistsCreated != 2 || len(rep.SkippedUsers) != 1 || rep.SkippedUsers[0] != "Ghost" {
		t.Fatalf("dry run report: %+v", rep)
	}
	if n := count(`SELECT count(*) FROM user_item_data`) + count(`SELECT count(*) FROM playlists`); n != 0 {
		t.Fatalf("dry run wrote %d rows", n)
	}

	// Real run through the queued-run path (as the worker does).
	run := func() Report {
		t.Helper()
		raw, _ := json.Marshal(opts)
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO import_runs (id, dry_run, source_url, secret, options) VALUES ($1, false, $2, $3, $4)`,
			id, srv.URL, f.key, raw); err != nil {
			t.Fatal(err)
		}
		if err := runner.Run(ctx, id); err != nil {
			t.Fatal(err)
		}
		var status string
		var secret *string
		var rawRep []byte
		var errMsg *string
		if err := pool.QueryRow(ctx, `SELECT status, secret, report, error FROM import_runs WHERE id = $1`, id).Scan(&status, &secret, &rawRep, &errMsg); err != nil {
			t.Fatal(err)
		}
		if status != "done" || secret != nil {
			t.Fatalf("run ended %s (error %v), secret kept: %v", status, errMsg, secret != nil)
		}
		var r Report
		_ = json.Unmarshal(rawRep, &r)
		return r
	}
	rep = run()
	if rep.MatchedByProvider != 4 || rep.MatchedByPath != 1 || rep.Unmatched != 1 || rep.UnmatchedSample[0].Name != "Nope" ||
		rep.PlaylistsCreated != 2 || rep.PlaylistItemsMissing != 1 {
		t.Fatalf("report: %+v", rep)
	}

	type row struct {
		pos        int64
		played     bool
		count      int
		lastPlayed time.Time
		fav        bool
	}
	get := func(owner, item uuid.UUID) row {
		t.Helper()
		var r row
		if err := pool.QueryRow(ctx, `SELECT position_ms, played, play_count, last_played_at, favorite FROM user_item_data
			WHERE owner_id = $1 AND item_id = $2`, owner, item).Scan(&r.pos, &r.played, &r.count, &r.lastPlayed, &r.fav); err != nil {
			t.Fatalf("row %v: %v", item, err)
		}
		return r
	}
	if r := get(ana, ids["dune"]); !r.played || r.count != 2 || !r.fav || r.pos != 0 || !r.lastPlayed.Equal(t1) {
		t.Fatalf("ana dune: %+v", r)
	}
	if r := get(ana, ids["arrival"]); r.played || r.pos != 1_800_000 || !r.lastPlayed.Equal(t2) {
		t.Fatalf("ana arrival: %+v", r)
	}
	if r := get(ana, ids["e1"]); !r.played || r.count != 1 {
		t.Fatalf("ana e1: %+v", r)
	}
	if r := get(ana, ids["show"]); !r.fav || r.played {
		t.Fatalf("ana show: %+v", r)
	}
	if r := get(marko, ids["dune"]); r.pos != 60_000 || r.played {
		t.Fatalf("marko dune: %+v", r)
	}
	if n := count(`SELECT count(*) FROM user_item_data WHERE owner_id NOT IN ($1, $2)`, ana, marko); n != 0 {
		t.Fatalf("skipped user got %d rows", n)
	}
	var mineID uuid.UUID
	var public bool
	if err := pool.QueryRow(ctx, `SELECT id, public FROM playlists WHERE owner_id = $1 AND name = 'Weekend'`, ana).Scan(&mineID, &public); err != nil || public {
		t.Fatalf("weekend playlist: %v public=%v", err, public)
	}
	var order []uuid.UUID
	rows, _ := pool.Query(ctx, `SELECT item_id FROM playlist_items WHERE playlist_id = $1 ORDER BY position`, mineID)
	for rows.Next() {
		var id uuid.UUID
		_ = rows.Scan(&id)
		order = append(order, id)
	}
	rows.Close()
	if len(order) != 2 || order[0] != ids["arrival"] || order[1] != ids["e1"] {
		t.Fatalf("playlist order: %v", order)
	}
	if n := count(`SELECT count(*) FROM playlists WHERE owner_id = $1 AND name = 'Family' AND public`, ana); n != 1 {
		t.Fatalf("open playlist: %d", n)
	}

	// Ana keeps watching Arrival in Streamero; then the import runs again.
	later := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `UPDATE user_item_data SET position_ms = 2400000, last_played_at = $3 WHERE owner_id = $1 AND item_id = $2`,
		ana, ids["arrival"], later); err != nil {
		t.Fatal(err)
	}
	before := count(`SELECT count(*) FROM user_item_data`)
	rep = run()
	if rep.PlaylistsCreated != 0 || rep.PlaylistsUpdated != 2 || rep.KeptNewer != 1 {
		t.Fatalf("second report: %+v", rep)
	}
	if n := count(`SELECT count(*) FROM playlists`); n != 2 {
		t.Fatalf("playlists duplicated: %d", n)
	}
	if n := count(`SELECT count(*) FROM playlist_items`); n != 3 {
		t.Fatalf("playlist items: %d", n)
	}
	if n := count(`SELECT count(*) FROM user_item_data`); n != before {
		t.Fatalf("rows %d -> %d", before, n)
	}
	if r := get(ana, ids["arrival"]); r.pos != 2_400_000 || !r.lastPlayed.Equal(later) {
		t.Fatalf("newer Streamero progress regressed: %+v", r)
	}
	if r := get(ana, ids["dune"]); r.count != 2 || !r.lastPlayed.Equal(t1) {
		t.Fatalf("dune changed on re-import: %+v", r)
	}
}

func TestImportRunFailsCleanlyIntegration(t *testing.T) {
	pool := isolatedPool(t)
	f, _, _ := source(t)
	srv := f.start()
	ctx := context.Background()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO import_runs (id, dry_run, source_url, secret, options) VALUES ($1, true, $2, 'wrong', '{}')`, id, srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := (&Runner{Pool: pool}).Run(ctx, id); err != nil {
		t.Fatal(err)
	}
	var status, msg string
	var secret *string
	if err := pool.QueryRow(ctx, `SELECT status, error, secret FROM import_runs WHERE id = $1`, id).Scan(&status, &msg, &secret); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || secret != nil || msg == "" {
		t.Fatalf("status %s, error %q, secret kept %v", status, msg, secret != nil)
	}
	// A finished run is not executed again.
	if err := (&Runner{Pool: pool}).Run(ctx, id); err != nil {
		t.Fatal(err)
	}
}
