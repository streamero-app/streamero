package importer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func ip(n int) *int { return &n }

func TestTicksToMS(t *testing.T) {
	for ticks, want := range map[int64]int64{0: 0, -5: 0, 9_999: 0, 10_000: 1, 12_345_678_901: 1_234_567, 72_000_000_000: 7_200_000} {
		if got := TicksToMS(ticks); got != want {
			t.Errorf("TicksToMS(%d) = %d, want %d", ticks, got, want)
		}
	}
}

func TestClientReadsUsersAndPagesUserItems(t *testing.T) {
	f := newFakeSource(t)
	f.users = []map[string]any{{"Id": "u1", "Name": "Ana", "Policy": map[string]any{"IsAdministrator": true}}}
	for i := 0; i < 7; i++ {
		f.item("u1", map[string]any{"Id": "m" + string(rune('a'+i)), "Name": "Movie", "Type": "Movie",
			"UserData": map[string]any{"Played": true, "PlayCount": 1}})
	}
	// Played and favourite: must be returned once.
	f.item("u1", map[string]any{"Id": "both", "Name": "Both", "Type": "Movie",
		"UserData": map[string]any{"Played": true, "IsFavorite": true}})
	f.item("u1", map[string]any{"Id": "res", "Name": "Half", "Type": "Episode",
		"UserData": map[string]any{"PlaybackPositionTicks": 6_000_000_000}})
	srv := f.start()
	c := &Client{BaseURL: srv.URL, Key: f.key, PageSize: 3}
	ctx := context.Background()
	info, err := c.Info(ctx)
	if err != nil || info.ID != "srv-1" || info.Version != "10.10.7" {
		t.Fatalf("info: %+v %v", info, err)
	}
	users, err := c.Users(ctx)
	if err != nil || len(users) != 1 || !users[0].Policy.IsAdministrator {
		t.Fatalf("users: %+v %v", users, err)
	}
	items, err := c.UserItems(ctx, "u1")
	if err != nil || len(items) != 9 {
		t.Fatalf("items: %d %v", len(items), err)
	}
	if bad := (&Client{BaseURL: srv.URL, Key: "wrong"}); true {
		_, err := bad.Info(ctx)
		var up *ErrUpstream
		if !errors.As(err, &up) || up.Status != 401 {
			t.Fatalf("wrong key: %v", err)
		}
	}
}

func TestCleanURL(t *testing.T) {
	if got, err := CleanURL(" http://media:8096/ "); err != nil || got != "http://media:8096" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"", "media:8096", "ftp://x", "http://a:b@x"} {
		if _, err := CleanURL(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// local builds a small Streamero library: a movie, a series with one season, and a track.
func local() (items []LocalItem, ids map[string]uuid.UUID) {
	ids = map[string]uuid.UUID{}
	for _, k := range []string{"dune", "dune4k", "arrival", "show", "s1", "e1", "e2", "track", "nameonly"} {
		ids[k] = uuid.New()
	}
	items = []LocalItem{
		{ID: ids["dune"], Kind: "movie", Path: "/media/movies/Dune (2021)", Title: "Dune", Year: 2021, Season: -1, Episode: -1,
			ProviderIDs: map[string]string{"tmdb": "438631"}, FilePaths: []string{"/media/movies/Dune (2021)/Dune.mkv"}},
		{ID: ids["dune4k"], Kind: "movie", Path: "/media/4k/Dune (2021)", Title: "Dune", Year: 2021, Season: -1, Episode: -1,
			ProviderIDs: map[string]string{"tmdb": "438631"}},
		{ID: ids["arrival"], Kind: "movie", Path: "/media/movies/Arrival (2016)", Title: "Arrival", Year: 2016, Season: -1, Episode: -1,
			ProviderIDs: map[string]string{}, FilePaths: []string{"/media/movies/Arrival (2016)/Arrival.mkv"}},
		{ID: ids["nameonly"], Kind: "movie", Path: "/media/movies/Amélie", Title: "Amélie", AltTitle: "Amelie", Year: 2001, Season: -1, Episode: -1},
		{ID: ids["show"], Kind: "series", Path: "/media/tv/Show", Title: "Show", Year: 2019, Season: -1, Episode: -1,
			ProviderIDs: map[string]string{"tvdb": "777"}},
		{ID: ids["s1"], ParentID: ids["show"], Kind: "season", Path: "/media/tv/Show/Season 1", Title: "Season 1", Season: 1, Episode: -1},
		{ID: ids["e1"], ParentID: ids["s1"], Kind: "episode", Path: "/media/tv/Show/Season 1/Show S01E01.mkv", Title: "Pilot",
			Season: 1, Episode: 1, FilePaths: []string{"/media/tv/Show/Season 1/Show S01E01.mkv"}},
		{ID: ids["e2"], ParentID: ids["s1"], Kind: "episode", Path: "/media/tv/Show/Season 1/Show S01E02-E03.mkv", Title: "Two",
			Season: 1, Episode: 2, EpisodeEnd: 3},
		{ID: ids["track"], Kind: "track", Path: "/media/music/A/B/01.flac", Title: "Song", Season: -1, Episode: -1,
			FilePaths: []string{"/media/music/A/B/01.flac"}},
	}
	return items, ids
}

func TestMatchProviderPathAndTitle(t *testing.T) {
	items, ids := local()
	ix := NewIndex(items, []PathMapping{{From: "/data", To: "/media"}, {From: `D:\Films`, To: "/media/movies"}})
	series := Item{ID: "s", Type: "Series", Name: "Show", ProviderIDs: map[string]string{"Tvdb": "777"}}
	cases := []struct {
		name   string
		it     Item
		series *Item
		want   []uuid.UUID
		how    string
	}{
		{"provider, both copies", Item{Type: "Movie", Name: "Dune", ProviderIDs: map[string]string{"Tmdb": "438631"}}, nil,
			[]uuid.UUID{ids["dune"], ids["dune4k"]}, ByProvider},
		{"mapped path", Item{Type: "Movie", Name: "Arrival (x)", Path: "/data/movies/Arrival (2016)/Arrival.mkv"}, nil,
			[]uuid.UUID{ids["arrival"]}, ByPath},
		{"windows path", Item{Type: "Movie", Name: "?", Path: `D:\Films\Arrival (2016)\Arrival.mkv`}, nil,
			[]uuid.UUID{ids["arrival"]}, ByPath},
		{"title and year", Item{Type: "Movie", Name: "amelie", ProductionYear: 2001}, nil, []uuid.UUID{ids["nameonly"]}, ByTitle},
		{"wrong year", Item{Type: "Movie", Name: "Arrival", ProductionYear: 1999}, nil, nil, ""},
		{"episode via series provider", Item{Type: "Episode", Name: "Pilot", ParentIndexNumber: ip(1), IndexNumber: ip(1)}, &series,
			[]uuid.UUID{ids["e1"]}, ByProvider},
		{"multi-episode file", Item{Type: "Episode", ParentIndexNumber: ip(1), IndexNumber: ip(3)}, &series, []uuid.UUID{ids["e2"]}, ByProvider},
		{"season", Item{Type: "Season", IndexNumber: ip(1)}, &series, []uuid.UUID{ids["s1"]}, ByProvider},
		{"episode via path", Item{Type: "Episode", Path: "/data/tv/Show/Season 1/Show S01E01.mkv"}, nil, []uuid.UUID{ids["e1"]}, ByPath},
		{"episode via series title", Item{Type: "Episode", ParentIndexNumber: ip(1), IndexNumber: ip(1)},
			&Item{Type: "Series", Name: "SHOW", ProductionYear: 2019}, []uuid.UUID{ids["e1"]}, ByTitle},
		{"missing episode", Item{Type: "Episode", ParentIndexNumber: ip(2), IndexNumber: ip(1)}, &series, nil, ""},
		{"track by path", Item{Type: "Audio", Path: "/data/music/A/B/01.flac"}, nil, []uuid.UUID{ids["track"]}, ByPath},
		{"unknown type", Item{Type: "Book", Name: "Dune"}, nil, nil, ""},
	}
	for _, c := range cases {
		got, how := ix.Match(c.it, c.series)
		if how != c.how || len(got) != len(c.want) {
			t.Errorf("%s: got %v %q, want %v %q", c.name, got, how, c.want, c.how)
			continue
		}
		want := map[uuid.UUID]bool{}
		for _, id := range c.want {
			want[id] = true
		}
		for _, id := range got {
			if !want[id] {
				t.Errorf("%s: unexpected %v", c.name, id)
			}
		}
	}
}

func TestMergeKeepsNewestAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	newer := now.Add(-time.Hour)
	// Ticks with 100 ns precision are truncated to what Postgres stores.
	d := newer.Add(123 * time.Nanosecond)
	in := FromUserData(UserData{PlaybackPositionTicks: 36_000_000_000, PlayCount: 2, IsFavorite: true, LastPlayedDate: &Time{d}})
	if in.PositionMS != 3_600_000 || in.LastPlayed == nil || !in.LastPlayed.Equal(newer) {
		t.Fatalf("incoming: %+v", in)
	}

	// Nothing in Streamero yet: take everything.
	first, changed, kept := Merge(nil, in, now)
	if !changed || kept || first.PositionMS != 3_600_000 || first.PlayCount != 2 || !first.Favorite || !first.LastPlayedAt.Equal(newer) {
		t.Fatalf("first: %+v", first)
	}
	// Importing again changes nothing.
	if again, changed, kept := Merge(&first, in, now); changed || kept || again != first {
		t.Fatalf("second import changed state: %+v %v %v", again, changed, kept)
	}

	// Streamero watched it later: keep its progress, but union the favourite and keep the larger count.
	mine := State{Played: true, PlayCount: 1, LastPlayedAt: now}
	got, changed, kept := Merge(&mine, in, now)
	if !changed || !kept || !got.Played || got.PositionMS != 0 || got.PlayCount != 2 || !got.Favorite || !got.LastPlayedAt.Equal(now) {
		t.Fatalf("newer local: %+v %v %v", got, changed, kept)
	}

	// Streamero is older: the import wins.
	stale := State{PositionMS: 10, PlayCount: 0, LastPlayedAt: old, Favorite: false}
	got, _, kept = Merge(&stale, in, now)
	if kept || got.PositionMS != 3_600_000 || !got.LastPlayedAt.Equal(newer) {
		t.Fatalf("older local: %+v", got)
	}

	// A favourite-only row (no progress) takes the played state even without a source date.
	fav := State{Favorite: true, LastPlayedAt: now}
	got, _, _ = Merge(&fav, FromUserData(UserData{Played: true}), now)
	if !got.Played || got.PlayCount != 1 || !got.Favorite {
		t.Fatalf("favourite row: %+v", got)
	}

	// Played on the source resets the position, like Streamero does.
	if p := FromUserData(UserData{Played: true, PlaybackPositionTicks: 99_000}); p.PositionMS != 0 || p.PlayCount != 1 {
		t.Fatalf("played: %+v", p)
	}
}

func TestCombinePrefersLatest(t *testing.T) {
	a, b := time.Unix(100, 0), time.Unix(200, 0)
	got := Combine(Incoming{PositionMS: 5, PlayCount: 3, LastPlayed: &a, Favorite: true}, Incoming{Played: true, PlayCount: 1, LastPlayed: &b})
	if !got.Played || got.PositionMS != 0 || got.PlayCount != 3 || !got.Favorite || !got.LastPlayed.Equal(b) {
		t.Fatalf("%+v", got)
	}
}

func TestSuggestUsersIgnoresCase(t *testing.T) {
	ana, marko := uuid.New(), uuid.New()
	src := []User{{ID: "1", Name: "Ana"}, {ID: "2", Name: " MARKO "}, {ID: "3", Name: "Ghost"}}
	got := SuggestUsers(src, []LocalUser{{ID: ana, Username: "ana"}, {ID: marko, Username: "Marko"}})
	if len(got) != 2 || got["1"] != ana || got["2"] != marko {
		t.Fatalf("%v", got)
	}
	if _, ok := got["3"]; ok {
		t.Fatal("a source user without a Streamero account must not be mapped")
	}
}

func TestSourceDatesAreLenient(t *testing.T) {
	var d struct {
		A, B, C, D *Time
	}
	raw := `{"A":"2024-03-01T20:15:30.1234567Z","B":"2024-03-01T20:15:30.1234567","C":"garbage","D":null}`
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2024, 3, 1, 20, 15, 30, 123456700, time.UTC)
	if !d.A.Equal(want) || !d.B.Equal(want) {
		t.Fatalf("A=%v B=%v", d.A, d.B)
	}
	if d.C == nil || !d.C.IsZero() || d.D != nil {
		t.Fatalf("C=%v D=%v", d.C, d.D)
	}
	if in := FromUserData(UserData{Played: true, LastPlayedDate: d.C}); in.LastPlayed != nil {
		t.Fatalf("unreadable date must count as unknown: %v", in.LastPlayed)
	}
}
