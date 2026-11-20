package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/compat"
	"github.com/streamero-app/streamero/server/internal/netx"
)

// ratedLibrary seeds two libraries with rated and unrated titles (D-053):
//
//	movies: Grown (R), Family (PG), Plain (unrated)
//	shows:  Night Show (TV-MA) / season 1 / Pilot (unrated: inherits TV-MA)
//	        Toons (TV-G) / season 1 / Toon One (unrated), Toon Bad (TV-MA itself)
type ratedLibrary struct {
	movies, shows                          uuid.UUID
	grown, family, plain                   uuid.UUID
	night, nightSeason, pilot              uuid.UUID
	toons, toonsSeason, toonOne, toonBad   uuid.UUID
	grownFile, familyFile, pilotFile, oneF uuid.UUID
}

func seedRatedLibrary(t *testing.T) ratedLibrary {
	t.Helper()
	ctx := context.Background()
	var r ratedLibrary
	for _, p := range []*uuid.UUID{&r.movies, &r.shows, &r.grown, &r.family, &r.plain, &r.night, &r.nightSeason, &r.pilot,
		&r.toons, &r.toonsSeason, &r.toonOne, &r.toonBad, &r.grownFile, &r.familyFile, &r.pilotFile, &r.oneF} {
		*p = uuid.New()
	}
	dir := filepath.Join(mediaRoot, "rated-"+r.movies.String()[:8])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("not really a video, served as bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := testPool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO libraries (id, name, kind, paths) VALUES ($1, 'Rated films', 'movies', ARRAY[$3]), ($2, 'Rated shows', 'shows', ARRAY[$3])`,
		r.movies, r.shows, dir)
	item := func(id, lib uuid.UUID, parent *uuid.UUID, kind, title string, rating any, season, episode any) {
		exec(`INSERT INTO items (id, library_id, parent_id, kind, path, title, sort_title, official_rating, season_number, episode_number)
			VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $9)`, id, lib, parent, kind, filepath.Join(dir, id.String()), title, rating, season, episode)
	}
	item(r.grown, r.movies, nil, "movie", "Zq7 Grown", "R", nil, nil)
	item(r.family, r.movies, nil, "movie", "Zq7 Family", "PG", nil, nil)
	item(r.plain, r.movies, nil, "movie", "Zq7 Plain", nil, nil, nil)
	item(r.night, r.shows, nil, "series", "Zq7 Night Show", "TV-MA", nil, nil)
	item(r.nightSeason, r.shows, &r.night, "season", "Season 1", nil, 1, nil)
	item(r.pilot, r.shows, &r.nightSeason, "episode", "Zq7 Pilot", nil, 1, 1)
	item(r.toons, r.shows, nil, "series", "Zq7 Toons", "TV-G", nil, nil)
	item(r.toonsSeason, r.shows, &r.toons, "season", "Season 1", nil, 1, nil)
	item(r.toonOne, r.shows, &r.toonsSeason, "episode", "Zq7 Toon One", nil, 1, 1)
	item(r.toonBad, r.shows, &r.toonsSeason, "episode", "Zq7 Toon Bad", "TV-MA", 1, 2)
	media := func(id, itemID, lib uuid.UUID, name string) {
		exec(`INSERT INTO media_files (id, item_id, library_id, path, size, mtime, container, video_codec, width, height, duration_ms, audio)
			VALUES ($1, $2, $3, $4, 35, now(), 'mp4', 'h264', 1920, 1080, 6000000, '[{"codec":"aac","channels":2}]')`,
			id, itemID, lib, file(name))
	}
	media(r.grownFile, r.grown, r.movies, "grown.mp4")
	media(r.familyFile, r.family, r.movies, "family.mp4")
	media(r.pilotFile, r.pilot, r.shows, "pilot.mp4")
	media(r.oneF, r.toonOne, r.shows, "one.mp4")
	return r
}

// kidClient signs in to the administrator's account and selects a kids profile
// with the given limit.
func kidClient(t *testing.T, srv string, adminC *client, maxRating int) (*client, authResult) {
	t.Helper()
	var kid struct {
		ID string `json:"id"`
	}
	if s := adminC.do("POST", "/profiles", map[string]any{"name": "Kid", "color": "#3dd68c", "isKid": true, "maxRating": maxRating}, &kid); s != 201 {
		t.Fatalf("create kid: HTTP %d", s)
	}
	c := &client{t: t, base: srv + "/api/v1", http: &http.Client{}}
	var login authResult
	if s := c.do("POST", "/auth/login", map[string]any{"username": "Admin", "password": "admin-pass-1",
		"device": map[string]any{"name": "tablet"}}, &login); s != 200 {
		t.Fatalf("login: HTTP %d", s)
	}
	c.token = login.Token
	if s := c.do("POST", "/profiles/"+kid.ID+"/select", nil, nil); s != 204 {
		t.Fatalf("select kid: HTTP %d", s)
	}
	return c, login
}

type summary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func titlesOf(items []summary) string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return strings.Join(out, "|")
}

var webDevice = map[string]any{"device": map[string]any{"containers": []string{"mp4"}, "videoCodecs": []string{"h264"},
	"audioCodecs": []string{"aac"}}}

// TestParentalLimitEverywhereIntegration: a kids profile's age limit hides a
// title, and everything below a rated series, on every surface of the native
// and the compat API, including playback (D-053).
func TestParentalLimitEverywhereIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	r := seedRatedLibrary(t)
	kid, kidLogin := kidClient(t, srv.URL, adminC, 10)

	hidden := []string{"Zq7 Grown", "Zq7 Night Show", "Zq7 Pilot", "Zq7 Toon Bad"}
	noneHidden := func(where, titles string) {
		t.Helper()
		for _, h := range hidden {
			if strings.Contains(titles, h) {
				t.Errorf("%s shows %q to the kids profile: %s", where, h, titles)
			}
		}
	}

	// Lists.
	var page struct {
		Items []summary `json:"items"`
		Total int       `json:"total"`
	}
	kid.do("GET", "/libraries/"+r.movies.String()+"/items", nil, &page)
	if got := titlesOf(page.Items); !strings.Contains(got, "Zq7 Family") || !strings.Contains(got, "Zq7 Plain") || page.Total != 2 {
		t.Errorf("movies for the kid: %s (total %d)", got, page.Total)
	}
	noneHidden("library list", titlesOf(page.Items))
	kid.do("GET", "/libraries/"+r.shows.String()+"/items", nil, &page)
	noneHidden("shows list", titlesOf(page.Items))
	if s := kid.do("GET", "/libraries/"+r.shows.String()+"/items?parentId="+r.nightSeason.String(), nil, nil); s != 404 {
		t.Errorf("children of a hidden season: HTTP %d", s)
	}
	var found []summary
	kid.do("GET", "/search?q=zq7", nil, &found)
	if got := titlesOf(found); !strings.Contains(got, "Zq7 Toon One") || !strings.Contains(got, "Zq7 Family") {
		t.Errorf("search lost visible titles: %s", got)
	}
	noneHidden("search", titlesOf(found))
	kid.do("GET", "/home/latest", nil, &found)
	noneHidden("recently added", titlesOf(found))

	// Continue watching and favourites carry the profile's own state.
	owner := kidProfileID(t, kidLogin.Token)
	for _, id := range []uuid.UUID{r.grown, r.pilot, r.toonOne} {
		if _, err := testPool.Exec(context.Background(), `INSERT INTO user_item_data (owner_id, item_id, position_ms, favorite, last_played_at)
			VALUES ($1, $2, 60000, true, now())`, owner, id); err != nil {
			t.Fatal(err)
		}
	}
	kid.do("GET", "/home/continue", nil, &found)
	if got := titlesOf(found); got != "Zq7 Toon One" {
		t.Errorf("continue watching: %s", got)
	}
	kid.do("GET", "/home/favorites", nil, &found)
	noneHidden("favourites", titlesOf(found))

	// Detail, children, artwork, playback, progress.
	for _, id := range []uuid.UUID{r.grown, r.night, r.nightSeason, r.pilot, r.toonBad} {
		if s := kid.do("GET", "/items/"+id.String(), nil, nil); s != 404 {
			t.Errorf("GET /items/%s: HTTP %d, want 404", id, s)
		}
		if s := kid.do("POST", "/items/"+id.String()+"/playback", webDevice, nil); s != 404 {
			t.Errorf("playback of %s: HTTP %d, want 404", id, s)
		}
		if s := kid.do("GET", "/items/"+id.String()+"/images/poster", nil, nil); s != 404 {
			t.Errorf("poster of %s: HTTP %d", id, s)
		}
		if s := kid.do("PUT", "/items/"+id.String()+"/progress", map[string]any{"positionMs": 1000}, nil); s != 404 {
			t.Errorf("progress on %s: HTTP %d", id, s)
		}
	}
	var season struct {
		Children []summary `json:"children"`
	}
	if s := kid.do("GET", "/items/"+r.toonsSeason.String(), nil, &season); s != 200 || titlesOf(season.Children) != "Zq7 Toon One" {
		t.Errorf("season of a visible series: HTTP %d %s", s, titlesOf(season.Children))
	}
	var info struct {
		URL string `json:"url"`
	}
	if s := kid.do("POST", "/items/"+r.toonOne.String()+"/playback", webDevice, &info); s != 200 || info.URL == "" {
		t.Errorf("playback of a visible episode: HTTP %d", s)
	}
	if s := kid.do("GET", "/trickplay/"+r.grownFile.String()+"/0", nil, nil); s != 404 {
		t.Errorf("trickplay of a hidden file: HTTP %d", s)
	}

	// Playlists keep what was added, but show only what the profile may see.
	var pl struct {
		ID string `json:"id"`
	}
	if s := kid.do("POST", "/playlists", map[string]any{"name": "Mine", "itemIds": []string{r.grown.String(), r.family.String()}}, &pl); s != 201 {
		t.Fatalf("playlist: HTTP %d", s)
	}
	var detail struct {
		Items []summary `json:"items"`
	}
	kid.do("GET", "/playlists/"+pl.ID, nil, &detail)
	if got := titlesOf(detail.Items); got != "Zq7 Family" {
		t.Errorf("playlist for the kid: %s", got)
	}

	// The account's own (adult) profile still sees everything.
	if s := adminC.do("GET", "/items/"+r.grown.String(), nil, nil); s != 200 {
		t.Errorf("admin main profile: HTTP %d", s)
	}

	// Watch-together: a hidden title cannot be queued.
	p, err := testState.Auth().Authenticate(context.Background(), kidLogin.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.SyncLookup(context.Background(), p, r.pilot); err == nil {
		t.Error("SyncPlay lookup of an episode of a hidden series succeeded")
	}
	if _, err := testHandler.SyncLookup(context.Background(), p, r.family); err != nil {
		t.Errorf("SyncPlay lookup of a visible movie: %v", err)
	}

	// Compat API, same rules.
	tc := &compatCaller{t: t, base: srv.URL + "/compat", token: kidLogin.Token}
	var res struct {
		Items []struct {
			Name string `json:"Name"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	ids := strings.Join([]string{compat.ID(r.grown), compat.ID(r.family), compat.ID(r.pilot), compat.ID(r.toonOne), compat.ID(r.toonBad)}, ",")
	tc.call("GET", "/Items?Recursive=true&Ids="+ids, nil, &res)
	var names []string
	for _, it := range res.Items {
		names = append(names, it.Name)
	}
	if got := strings.Join(names, "|"); res.TotalRecordCount != 2 || !strings.Contains(got, "Zq7 Family") || !strings.Contains(got, "Zq7 Toon One") {
		t.Errorf("compat items: %s (total %d)", got, res.TotalRecordCount)
	}
	for _, path := range []string{"/Items/" + compat.ID(r.grown), "/Shows/" + compat.ID(r.night) + "/Episodes",
		"/Shows/" + compat.ID(r.night) + "/Seasons"} {
		if s := tc.call("GET", path, nil, nil); s != 404 {
			t.Errorf("compat %s: HTTP %d, want 404", path, s)
		}
	}
	var anc []any
	if s := tc.call("GET", "/Items/"+compat.ID(r.pilot)+"/Ancestors", nil, &anc); s != 200 || len(anc) != 0 {
		t.Errorf("compat ancestors of a hidden episode: HTTP %d %v", s, anc)
	}
	if s := tc.call("POST", "/Items/"+compat.ID(r.grown)+"/PlaybackInfo", map[string]any{}, nil); s != 404 {
		t.Errorf("compat PlaybackInfo of a hidden movie: HTTP %d", s)
	}
	if s, _, _ := tc.raw("GET", "/Videos/"+compat.ID(r.grown)+"/stream", nil, nil); s != 404 {
		t.Errorf("compat stream of a hidden movie: HTTP %d", s)
	}
	tc.call("GET", "/Shows/"+compat.ID(r.toons)+"/Episodes", nil, &res)
	if len(res.Items) != 1 || res.Items[0].Name != "Zq7 Toon One" {
		t.Errorf("compat episodes of a visible series: %+v", res.Items)
	}
	var hints struct {
		SearchHints []struct {
			Name string `json:"Name"`
		} `json:"SearchHints"`
	}
	tc.call("GET", "/Search/Hints?SearchTerm=zq7", nil, &hints)
	for _, h := range hints.SearchHints {
		noneHidden("compat search", h.Name)
	}
}

func kidProfileID(t *testing.T, token string) uuid.UUID {
	t.Helper()
	p, err := testState.Auth().Authenticate(context.Background(), token)
	if err != nil || p.Profile == nil {
		t.Fatalf("kid principal: %+v %v", p, err)
	}
	return p.Profile.ID
}

func getStatus(t *testing.T, u string) int {
	t.Helper()
	resp, err := http.Get(u) //nolint:gosec // test server
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

// TestStreamLinksFollowSessionIntegration: a signed media link handed to a
// session stops working when the session is signed out, loses library access or
// switches profile; the compat media source tag likewise (D-053).
func TestStreamLinksFollowSessionIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	testState.Stream().Signer.Grant = testState.Auth().CheckGrant
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	r := seedRatedLibrary(t)

	var user struct {
		ID string `json:"id"`
	}
	if s := adminC.do("POST", "/users", map[string]any{"username": "viewer", "password": "viewer-pass-1"}, &user); s != 201 {
		t.Fatalf("create user: HTTP %d", s)
	}
	login := func() (*client, authResult) {
		c := &client{t: t, base: srv.URL + "/api/v1", http: &http.Client{}}
		var res authResult
		if s := c.do("POST", "/auth/login", map[string]any{"username": "viewer", "password": "viewer-pass-1",
			"device": map[string]any{"name": "tv"}}, &res); s != 200 {
			t.Fatalf("login: HTTP %d", s)
		}
		c.token = res.Token
		return c, res
	}
	play := func(c *client) string {
		t.Helper()
		var info struct {
			URL    string `json:"url"`
			Method string `json:"method"`
		}
		if s := c.do("POST", "/items/"+r.family.String()+"/playback", webDevice, &info); s != 200 || info.Method != "direct" {
			t.Fatalf("playback: HTTP %d %+v", s, info)
		}
		if !strings.Contains(info.URL, "g=s_") {
			t.Fatalf("link not bound to the session: %s", info.URL)
		}
		if s := getStatus(t, srv.URL+info.URL); s != 200 {
			t.Fatalf("fresh link: HTTP %d", s)
		}
		return info.URL
	}

	// Revoked device.
	c, res := login()
	link := play(c)
	if s := adminC.do("DELETE", "/sessions/"+res.SessionID, nil, nil); s != 204 {
		t.Fatalf("revoke: HTTP %d", s)
	}
	if s := getStatus(t, srv.URL+link); s != 403 {
		t.Errorf("link after the session was revoked: HTTP %d, want 403", s)
	}
	// Signed out.
	c, _ = login()
	link = play(c)
	c.do("POST", "/auth/logout", nil, nil)
	if s := getStatus(t, srv.URL+link); s != 403 {
		t.Errorf("link after sign-out: HTTP %d", s)
	}
	// Library access removed.
	c, _ = login()
	link = play(c)
	perms := map[string]any{"allLibraries": false, "download": true, "videoTranscoding": true, "audioTranscoding": true, "remux": true,
		"liveTv": true, "manageLiveTv": false, "syncPlay": true, "deleteMedia": false, "manageCollections": false,
		"editSubtitles": false, "remoteControl": true, "maxSessions": 0}
	if s := adminC.do("PATCH", "/users/"+user.ID, map[string]any{"permissions": perms}, nil); s != 200 {
		t.Fatalf("update user: HTTP %d", s)
	}
	if s := getStatus(t, srv.URL+link); s != 403 {
		t.Errorf("link after library access was removed: HTTP %d", s)
	}
	// Profile switched (to a kids profile) on the same device.
	link = play(adminC)
	kid, _ := kidClient(t, srv.URL, adminC, 18)
	_ = kid
	var kidProfile []struct {
		ID string `json:"id"`
	}
	adminC.do("GET", "/profiles", nil, &kidProfile)
	if s := adminC.do("POST", "/profiles/"+kidProfile[0].ID+"/select", nil, nil); s != 204 {
		t.Fatalf("select profile: HTTP %d", s)
	}
	if s := getStatus(t, srv.URL+link); s != 403 {
		t.Errorf("link after switching profile: HTTP %d", s)
	}

	// Compat: the media source tag is bound to the session too, and download
	// needs the download permission.
	perms["allLibraries"], perms["download"] = true, false
	adminC2 := newClient(t, srv)
	var adminLogin authResult
	adminC2.do("POST", "/auth/login", map[string]any{"username": "Admin", "password": "admin-pass-1", "device": map[string]any{"name": "x"}}, &adminLogin)
	adminC2.token = adminLogin.Token
	if s := adminC2.do("PATCH", "/users/"+user.ID, map[string]any{"permissions": perms}, nil); s != 200 {
		t.Fatalf("update user: HTTP %d", s)
	}
	var auth struct {
		AccessToken string `json:"AccessToken"`
	}
	cc := &compatCaller{t: t, base: srv.URL + "/compat"}
	if s := cc.call("POST", "/Users/AuthenticateByName", map[string]any{"Username": "viewer", "Pw": "viewer-pass-1"}, &auth); s != 200 {
		t.Fatalf("compat sign-in: HTTP %d", s)
	}
	cc.token = auth.AccessToken
	var pi struct {
		MediaSources []struct {
			ID   string `json:"Id"`
			ETag string `json:"ETag"`
		} `json:"MediaSources"`
	}
	if s := cc.call("POST", "/Items/"+compat.ID(r.family)+"/PlaybackInfo", map[string]any{}, &pi); s != 200 || len(pi.MediaSources) != 1 {
		t.Fatalf("PlaybackInfo: HTTP %d %+v", s, pi)
	}
	tagURL := srv.URL + "/compat/Videos/" + compat.ID(r.family) + "/stream?MediaSourceId=" + pi.MediaSources[0].ID + "&Tag=" + pi.MediaSources[0].ETag
	if s := getStatus(t, tagURL); s != 200 {
		t.Fatalf("tag link: HTTP %d", s)
	}
	if s, _, _ := cc.raw("GET", "/Videos/"+compat.ID(r.family)+"/download", nil, nil); s != 403 {
		t.Errorf("download without the permission: HTTP %d, want 403", s)
	}
	if s := getStatus(t, srv.URL+"/compat/Videos/"+compat.ID(r.family)+"/download?MediaSourceId="+pi.MediaSources[0].ID+
		"&Tag="+pi.MediaSources[0].ETag); s != 403 {
		t.Errorf("download with a tag only: HTTP %d, want 403", s)
	}
	cc.call("POST", "/Sessions/Logout", nil, nil)
	if s := getStatus(t, tagURL); s != 401 {
		t.Errorf("tag link after sign-out: HTTP %d, want 401", s)
	}
}

// TestLockoutSparesKnownClientsIntegration: wrong passwords from elsewhere lock
// the account, but not for an address that signed in to it before (D-053).
func TestLockoutSparesKnownClientsIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	if s := adminC.do("POST", "/users", map[string]any{"username": "lena", "password": "lena-pass-1"}, nil); s != 201 {
		t.Fatalf("create user: HTTP %d", s)
	}
	svc := testState.Auth()
	from := func(ip string) context.Context {
		return netx.WithClientIP(context.Background(), netip.MustParseAddr(ip))
	}
	home, attacker, newPlace := from("192.0.2.10"), from("198.51.100.66"), from("203.0.113.5")
	dev := auth.Device{Name: "x"}
	if _, _, err := svc.Login(home, "lena", "lena-pass-1", "", dev); err != nil {
		t.Fatalf("first sign-in: %v", err)
	}
	var locked *auth.LockedError
	for range auth.MaxFailedLogins {
		_, _, err := svc.Login(attacker, "lena", "guess", "", dev)
		if err != nil && errors.As(err, &locked) {
			break
		}
	}
	if locked == nil {
		t.Fatal("the account was not locked by repeated wrong passwords")
	}
	if _, _, err := svc.Login(attacker, "lena", "lena-pass-1", "", dev); !errors.As(err, &locked) {
		t.Errorf("right password from the guessing address while locked: %v", err)
	}
	if _, _, err := svc.Login(newPlace, "lena", "lena-pass-1", "", dev); !errors.As(err, &locked) {
		t.Errorf("right password from an unknown address while locked: %v", err)
	}
	if _, _, err := svc.Login(home, "lena", "lena-pass-1", "", dev); err != nil {
		t.Errorf("the owner's known address is locked out: %v", err)
	}
	// Signing in from home does not reopen the account to the guesser.
	if _, _, err := svc.Login(attacker, "lena", "lena-pass-1", "", dev); !errors.As(err, &locked) {
		t.Errorf("the lock was lifted for everyone by the owner's sign-in: %v", err)
	}
	// A known address has its own limit.
	locked = nil
	for range auth.MaxFailedLogins {
		if _, _, err := svc.Login(home, "lena", "wrong", "", dev); errors.As(err, &locked) {
			break
		}
	}
	if locked == nil {
		t.Fatal("wrong passwords from a known address are not limited")
	}
	if _, _, err := svc.Login(home, "lena", "lena-pass-1", "", dev); !errors.As(err, &locked) {
		t.Errorf("known address after its own lock: %v", err)
	}
}

// TestDownloadQuotaIntegration: one account cannot queue more than
// MaxQueuedPerUser transcodes (D-053).
func TestDownloadQuotaIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	if s := adminC.do("POST", "/users", map[string]any{"username": "hoarder", "password": "hoarder-pass-1"}, nil); s != 201 {
		t.Fatalf("create user: HTTP %d", s)
	}
	c := &client{t: t, base: srv.URL + "/api/v1", http: &http.Client{}}
	var res authResult
	c.do("POST", "/auth/login", map[string]any{"username": "hoarder", "password": "hoarder-pass-1", "device": map[string]any{"name": "p"}}, &res)
	c.token = res.Token
	ids := make([]uuid.UUID, 0, 12)
	for range 6 {
		r := seedRatedLibrary(t)
		ids = append(ids, r.family, r.toonOne)
	}
	var created, refused int
	for _, id := range ids {
		switch s := c.do("POST", "/downloads", map[string]any{"itemId": id.String(), "quality": "720p"}, nil); s {
		case 200, 201:
			created++
		case 429:
			refused++
		default:
			t.Fatalf("download: HTTP %d", s)
		}
	}
	if created != 10 || refused != 2 {
		t.Errorf("queued %d, refused %d; want 10 and 2", created, refused)
	}
	var lastErr apiError
	var queued int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM downloads d JOIN download_transcodes t ON t.id = d.transcode_id
		JOIN users u ON u.id = d.user_id WHERE u.username = 'hoarder' AND t.state = 'queued'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued > 10 {
		t.Errorf("%d transcodes queued for one account", queued)
	}
	if s := c.do("POST", "/downloads", map[string]any{"itemId": ids[len(ids)-1].String(), "quality": "1080p"}, &lastErr); s != 429 || lastErr.Code != "download_quota" {
		t.Errorf("over the quota: HTTP %d %+v, want 429 download_quota", s, lastErr)
	}
	// Administrators are not limited.
	if s := adminC.do("POST", "/downloads", map[string]any{"itemId": ids[0].String(), "quality": "1080p"}, nil); s != 201 && s != 200 {
		t.Errorf("admin download: HTTP %d", s)
	}
}
