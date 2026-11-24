package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	pngenc "image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/streamero-app/streamero/server/internal/activity"
	"github.com/streamero-app/streamero/server/internal/compat"
	"github.com/streamero-app/streamero/server/internal/dlna"
	"github.com/streamero-app/streamero/server/internal/jobs"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/notify"
)

// Words that exist only in the adult library: no response to an account without
// adult access may contain any of them.
var adultMarkers = []string{"Velvetine", "Moonlit Secret", "Performer Onyx", "Xanadu Tag"}

// rawCall sends a native API request and returns the status and body.
func rawCall(t *testing.T, c *client, method, path string, body any) (int, string) {
	t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range 8 {
		img.Set(i, i, color.RGBA{200, 10, 10, 255})
	}
	var b bytes.Buffer
	if err := pngenc.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// fakeTPDB answers like ThePornDB's public REST API for one scene.
func fakeTPDB(t *testing.T) *httptest.Server {
	t.Helper()
	pic := pngBytes(t)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/img/") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pic)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tpdb-good-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Unauthenticated."}`))
			return
		}
		if r.URL.Path != "/scenes" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("parse") == "" {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"s-1","title":"Moonlit Secret","date":"2024-01-15","description":"A scene.",
			"url":"https://example.invalid/scene","image":"%[1]s/img/poster.png","background":{"large":"%[1]s/img/back.png"},
			"duration":600,"site":{"id":7,"name":"Velvetine","url":"https://example.invalid","logo":"%[1]s/img/logo.png"},
			"performers":[{"id":"p-1","name":"Performer Onyx","image":"%[1]s/img/p.png","extra":{"gender":"female"}}],
			"tags":[{"name":"Xanadu Tag"}]},
			{"id":"s-2","title":"Other","date":"2020-02-02","deleted_at":"2021-01-01","site":{"name":"Velvetine"}}]}`, srv.URL)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// didlOf browses a DLNA container and returns its children and the whole
// (unescaped) DIDL document.
func didlOf(t *testing.T, base, id string) (didlDoc, string) {
	t.Helper()
	doc, _ := browse(t, base, id, "BrowseDirectChildren", nil)
	_, body := soapCall(t, base, "Browse", "<ObjectID>"+id+"</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><Filter>*</Filter>"+
		"<StartingIndex>0</StartingIndex><RequestedCount>50</RequestedCount><SortCriteria></SortCriteria>", nil)
	return doc, html.UnescapeString(body)
}

func login(t *testing.T, srv *httptest.Server, user, pass string) *client {
	t.Helper()
	c := newClient(t, srv)
	var res authResult
	if s := c.do("POST", "/auth/login", map[string]any{"username": user, "password": pass, "device": map[string]any{"name": "t"}}, &res); s != 200 {
		t.Fatalf("login %s: HTTP %d", user, s)
	}
	c.token = res.Token
	return c
}

// TestAdultLibraryNoLeaksIntegration (D-053) builds an adult library inside a
// mixed library's folder and checks every surface an account without adult
// access can reach: none returns adult content. A granted account sees it.
func TestAdultLibraryNoLeaksIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	admin := setupAdmin(t, adminC)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	probe := func(context.Context, string) (library.ProbeResult, error) {
		return library.ProbeResult{Container: "mp4", VideoCodec: "h264", Width: 1920, Height: 1080, DurationMS: 600_000,
			Audio: []library.AudioStream{{Codec: "aac", Channels: 2}}}, nil
	}
	testAdult.Probe = probe
	tpdb := fakeTPDB(t)
	testAdult.TPDBURL = tpdb.URL

	// One folder holds a normal film and, under adult/, a Locarr scene.
	all := filepath.Join(mediaRoot, "all")
	adultDir := filepath.Join(all, "adult")
	for p, data := range map[string]string{
		filepath.Join(all, "Film (2020)", "Film (2020).mkv"):                                                             "film-bytes",
		filepath.Join(adultDir, "Velvetine", "Velvetine - 2024-01-15 - Moonlit Secret [Performer Onyx] WEBDL-1080p.mp4"): "adult-video-bytes",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: log, Probe: probe, Adult: testAdult.Scan}
	var mixed struct{ ID string }
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "Sve", "kind": "mixed", "paths": []string{all}}, &mixed); s != 201 {
		t.Fatalf("create mixed: HTTP %d", s)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE kind IN ('mixed', 'adult') AND name IN ('Sve', 'Privatno')`)
	})
	// Before the adult library exists the mixed library picks up the scene.
	if _, err := scanner.Scan(ctx, uuid.MustParse(mixed.ID)); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = testPool.QueryRow(ctx, `SELECT count(*) FROM media_files WHERE path LIKE '%Velvetine%'`).Scan(&n)
	if n != 1 {
		t.Fatalf("setup: the mixed library should have scanned the scene first (%d)", n)
	}

	// Creating the adult library: admin only, with explicit confirmation.
	var e apiError
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "Privatno", "kind": "adult", "paths": []string{adultDir}}, &e); s != 400 || e.Code != "confirmation_required" {
		t.Fatalf("adult library without confirmation: HTTP %d %+v", s, e)
	}
	var lib struct {
		ID, Kind string
	}
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "Privatno", "kind": "adult", "paths": []string{adultDir}, "confirmAdult": true}, &lib); s != 201 || lib.Kind != "adult" {
		t.Fatalf("create adult library: HTTP %d %+v", s, lib)
	}
	libID := uuid.MustParse(lib.ID)
	// What the mixed library had found inside the adult folder is gone at once.
	_ = testPool.QueryRow(ctx, `SELECT count(*) FROM media_files WHERE path LIKE '%Velvetine%'`).Scan(&n)
	if n != 0 {
		t.Fatalf("items under the adult folder were not purged from the mixed library (%d)", n)
	}
	// Kind changes and overlapping folders are refused.
	if s := adminC.do("PUT", "/libraries/"+lib.ID, map[string]any{"name": "x", "kind": "movies", "paths": []string{adultDir}}, &e); s != 400 || e.Code != "kind_change_not_allowed" {
		t.Fatalf("adult → movies: HTTP %d %+v", s, e)
	}
	if s := adminC.do("PUT", "/libraries/"+mixed.ID, map[string]any{"name": "x", "kind": "adult", "paths": []string{all}, "confirmAdult": true}, &e); s != 400 || e.Code != "kind_change_not_allowed" {
		t.Fatalf("mixed → adult: HTTP %d %+v", s, e)
	}
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "Unutra", "kind": "movies", "paths": []string{filepath.Join(adultDir, "Velvetine")}}, &e); s != 400 || e.Code != "adult_overlap" {
		t.Fatalf("library inside the adult folder: HTTP %d %+v", s, e)
	}
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "Oko", "kind": "adult", "paths": []string{all}, "confirmAdult": true}, &e); s != 400 || e.Code != "adult_overlap" {
		t.Fatalf("adult library around another library: HTTP %d %+v", s, e)
	}

	// Webhook subscribed to everything: adult scans and playback must not reach it.
	var hookMu sync.Mutex
	var hookBodies []string
	hook := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		hookMu.Lock()
		hookBodies = append(hookBodies, string(b))
		hookMu.Unlock()
	}))
	defer hook.Close()
	if s := adminC.do("PUT", "/settings/notifications", map[string]any{"targets": []any{map[string]any{"id": uuid.NewString(),
		"name": "Hook", "kind": "generic", "url": hook.URL, "enabled": true,
		"events": []string{notify.ItemAdded, notify.PlaybackStarted, notify.ScanFailed}}}}, nil); s != 200 {
		t.Fatalf("save webhook: HTTP %d", s)
	}
	dispatcher := &notify.Dispatcher{Settings: testState.Settings, Log: log}
	worker := &jobs.LibraryScanWorker{Scanner: scanner, Log: log, Notify: dispatcher}
	runScan := func(id uuid.UUID) {
		t.Helper()
		job := &river.Job[jobs.LibraryScanArgs]{JobRow: &rivertype.JobRow{ID: time.Now().UnixNano(), Kind: "library.scan"},
			Args: jobs.LibraryScanArgs{LibraryID: id}}
		if err := worker.Work(ctx, job); err != nil {
			t.Fatalf("scan job: %v", err)
		}
	}
	runScan(libID)
	// A new film makes the mixed library's scan announce something (control).
	if err := os.MkdirAll(filepath.Join(all, "Druga (2021)"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(all, "Druga (2021)", "Druga (2021).mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runScan(uuid.MustParse(mixed.ID)) // the rescan skips the adult folder

	// Metadata: a wrong key is refused, the right one is stored encrypted and never returned.
	if s := adminC.do("PUT", "/adult/settings", map[string]any{"tpdbApiKey": "wrong"}, &e); s != 400 || e.Code != "invalid_key" {
		t.Fatalf("bad tpdb key: HTTP %d %+v", s, e)
	}
	if s, body := rawCall(t, adminC, "PUT", "/adult/settings", map[string]any{"tpdbApiKey": "tpdb-good-key"}); s != 200 ||
		strings.Contains(body, "tpdb-good-key") || !strings.Contains(body, `"tpdbConfigured":true`) {
		t.Fatalf("save tpdb key: HTTP %d %s", s, body)
	}
	var stored string
	_ = testPool.QueryRow(ctx, `SELECT value::text FROM settings WHERE key = 'adult.metadata'`).Scan(&stored)
	if stored == "" || strings.Contains(stored, "tpdb-good-key") {
		t.Fatalf("the key is not stored encrypted: %q", stored)
	}
	if matched, err := testAdult.Enrich(ctx, libID, false); err != nil || matched != 1 {
		t.Fatalf("enrich: %d %v", matched, err)
	}
	var sceneID, fileID uuid.UUID
	if err := testPool.QueryRow(ctx, `SELECT a.id, f.id FROM adult_scenes a JOIN adult_files f ON f.scene_id = a.id WHERE a.library_id = $1`,
		libID).Scan(&sceneID, &fileID); err != nil {
		t.Fatal(err)
	}
	var studioID, performerID uuid.UUID
	_ = testPool.QueryRow(ctx, `SELECT id FROM adult_studios WHERE name = 'Velvetine'`).Scan(&studioID)
	_ = testPool.QueryRow(ctx, `SELECT id FROM adult_performers WHERE name = 'Performer Onyx'`).Scan(&performerID)

	// Accounts: a member (default permissions, all libraries), a member through an
	// invite with every permission, and the grantee.
	allPerms := map[string]any{"allLibraries": true,
		"download": true, "videoTranscoding": true, "audioTranscoding": true, "remux": true, "liveTv": true, "manageLiveTv": true,
		"syncPlay": true, "deleteMedia": true, "manageCollections": true, "editSubtitles": true, "remoteControl": true, "maxSessions": 0}
	for _, u := range []string{"clan", "odrasli"} {
		if s := adminC.do("POST", "/users", map[string]any{"username": u, "password": u + "-pass-1", "permissions": allPerms}, nil); s != 201 {
			t.Fatalf("create %s: HTTP %d", u, s)
		}
	}
	var inv struct{ Token string }
	adminC.do("POST", "/invites", map[string]any{"expiresInHours": 1, "maxUses": 1, "permissions": allPerms}, &inv)
	invited := newClient(t, srv)
	var invRes authResult
	if s := invited.do("POST", "/invite/"+inv.Token, map[string]any{"username": "pozvan", "password": "pozvan-pass-1"}, &invRes); s != 200 {
		t.Fatalf("accept invite: HTTP %d", s)
	}
	invited.token = invRes.Token
	member := login(t, srv, "clan", "clan-pass-1")
	grantee := login(t, srv, "odrasli", "odrasli-pass-1")
	var granteeMe struct{ ID string }
	grantee.do("GET", "/auth/me", nil, &granteeMe)

	// Nobody has access yet: not even the administrator.
	for name, c := range map[string]*client{"admin": adminC, "member": member, "invited": invited, "grantee": grantee} {
		var me map[string]any
		c.do("GET", "/auth/me", nil, &me)
		if me["adultAccess"] == true {
			t.Errorf("%s has adult access by default", name)
		}
	}
	// Giving access needs confirmation.
	if s := adminC.do("PUT", "/users/"+granteeMe.ID+"/adult-access", map[string]any{"enabled": true}, &e); s != 400 || e.Code != "confirmation_required" {
		t.Fatalf("grant without confirmation: HTTP %d %+v", s, e)
	}
	if s := member.do("PUT", "/users/"+granteeMe.ID+"/adult-access", map[string]any{"enabled": true, "confirm": true}, nil); s != 403 {
		t.Fatalf("member grants access: HTTP %d", s)
	}
	if s := adminC.do("PUT", "/users/"+granteeMe.ID+"/adult-access", map[string]any{"enabled": true, "confirm": true}, nil); s != 200 {
		t.Fatalf("grant: HTTP %d", s)
	}

	// Positive control: the grantee sees the scene, its studio, performer and artwork.
	var page struct {
		Items []struct {
			ID         string
			Title      string
			HasPoster  bool
			Performers []struct{ Name string }
			Studio     struct{ Name string }
		}
		Total int
	}
	if s := grantee.do("GET", "/adult/scenes", nil, &page); s != 200 || page.Total != 1 || page.Items[0].Title != "Moonlit Secret" ||
		page.Items[0].Studio.Name != "Velvetine" || len(page.Items[0].Performers) != 1 || !page.Items[0].HasPoster {
		t.Fatalf("grantee scenes: HTTP %d %+v", s, page)
	}
	for _, p := range []string{"/adult/status", "/adult/scenes/" + sceneID.String(), "/adult/studios", "/adult/performers",
		"/adult/studios/" + studioID.String(), "/adult/performers/" + performerID.String(), "/adult/scenes?q=onyx",
		"/adult/scenes?performerId=" + performerID.String(), "/adult/scenes?tag=Xanadu%20Tag"} {
		if s, body := rawCall(t, grantee, "GET", p, nil); s != 200 || !strings.Contains(body, "Velvetine") && !strings.Contains(body, "Onyx") &&
			!strings.Contains(body, "Privatno") {
			t.Errorf("grantee %s: HTTP %d %s", p, s, body)
		}
	}
	req, _ := http.NewRequest("GET", grantee.base+"/adult/images/scene/"+sceneID.String()+"/poster", nil)
	req.Header.Set("Authorization", "Bearer "+grantee.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "private, no-store" || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("grantee image: %v %+v", err, resp)
	}
	_ = resp.Body.Close()
	var pb struct {
		Method, URL string
	}
	device := map[string]any{"device": map[string]any{"containers": []string{"mp4", "mkv"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}}}
	if s := grantee.do("POST", "/adult/scenes/"+sceneID.String()+"/playback", device, &pb); s != 200 || pb.Method != "direct" ||
		!strings.HasPrefix(pb.URL, "/stream/adult/"+fileID.String()+"/") {
		t.Fatalf("grantee playback: HTTP %d %+v", s, pb)
	}
	if r, err := http.Get(srv.URL + pb.URL); err != nil || r.StatusCode != 200 {
		t.Fatalf("signed adult stream: %v %v", r, err)
	} else {
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if string(b) != "adult-video-bytes" || r.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatalf("adult stream body %q %v", b, r.Header)
		}
	}
	if r, _ := http.Get(srv.URL + strings.Replace(pb.URL, "sig=", "sig=x", 1)); r == nil || r.StatusCode != 403 {
		t.Fatalf("tampered adult stream link: %v", r)
	}
	// /stream/adult/ serves adult files only, even with a valid signature for another file.
	var filmFile uuid.UUID
	_ = testPool.QueryRow(ctx, `SELECT id FROM media_files WHERE path LIKE '%Film (2020).mkv'`).Scan(&filmFile)
	if filmFile == uuid.Nil {
		t.Fatal("the film was not scanned")
	}
	scope := "/stream/adult/" + filmFile.String() + "/"
	if r, _ := http.Get(srv.URL + scope + "video.mkv?" + testState.Stream().Signer.Query(scope, time.Minute)); r == nil || r.StatusCode != 404 {
		t.Fatalf("a normal file through /stream/adult/: %v", r)
	}
	// …and /stream/file/ never serves an adult file.
	scope = "/stream/file/" + fileID.String() + "/"
	if r, _ := http.Get(srv.URL + scope + "video.mp4?" + testState.Stream().Signer.Query(scope, time.Minute)); r == nil || r.StatusCode != 404 {
		t.Fatalf("an adult file through /stream/file/: %v", r)
	}
	if s := grantee.do("PUT", "/adult/scenes/"+sceneID.String()+"/progress", map[string]any{"positionMs": 120000, "durationMs": 600000}, nil); s != 204 {
		t.Fatalf("progress: HTTP %d", s)
	}
	if grantee.do("GET", "/adult/scenes?status=inProgress", nil, &page); page.Total != 1 {
		t.Fatalf("adult continue watching: %+v", page)
	}
	// The grantee's normal continue-watching row stays free of it.
	if s, body := rawCall(t, grantee, "GET", "/home/continue", nil); s != 200 || strings.Contains(body, sceneID.String()) {
		t.Fatalf("grantee home continue: %d %s", s, body)
	}
	if testNowPlaying != nil {
		for _, a := range testNowPlaying.Active() {
			if a.ItemID == sceneID {
				t.Fatal("adult playback reached the activity dashboard")
			}
		}
	}

	// Kids profile on the grantee's account and the grantee's API key: no access.
	var kid struct{ ID string }
	grantee.do("POST", "/profiles", map[string]any{"name": "Dijete", "color": "#3dd68c", "isKid": true, "maxRating": 7}, &kid)
	kidC := login(t, srv, "odrasli", "odrasli-pass-1")
	if s := kidC.do("POST", "/profiles/"+kid.ID+"/select", nil, nil); s != 204 {
		t.Fatalf("select kids profile: HTTP %d", s)
	}

	// --- Every surface, for every account without adult access -------------------
	adminKey := createKey(t, adminC, "dash", false)
	keyC := &client{t: t, base: adminC.base, http: &http.Client{}, token: adminKey}
	// The administrator gets adult access too, then calls with an API key: still nothing.
	if s := adminC.do("PUT", "/users/"+admin.User.ID+"/adult-access", map[string]any{"enabled": true, "confirm": true}, nil); s != 200 {
		t.Fatalf("grant admin: HTTP %d", s)
	}
	if s := keyC.do("GET", "/adult/scenes", nil, nil); s != 404 {
		t.Fatalf("API key of a granted admin reached adult content: HTTP %d", s)
	}
	if s := adminC.do("PUT", "/users/"+admin.User.ID+"/adult-access", map[string]any{"enabled": false}, nil); s != 200 {
		t.Fatalf("revoke admin: HTTP %d", s)
	}

	sid, fid, stid, pid := sceneID.String(), fileID.String(), studioID.String(), performerID.String()
	type call struct {
		method, path string
		body         any
	}
	native := []call{
		{"GET", "/libraries/" + lib.ID + "/items", nil},
		{"GET", "/libraries/" + mixed.ID + "/items?limit=200", nil},
		{"GET", "/home/latest", nil}, {"GET", "/home/favorites", nil}, {"GET", "/home/continue", nil},
		{"GET", "/home/next-up", nil}, {"GET", "/home/continue-reading", nil},
		{"GET", "/search?q=Moonlit", nil}, {"GET", "/search?q=Velvetine", nil}, {"GET", "/search?q=Onyx", nil},
		{"GET", "/items/" + sid, nil}, {"GET", "/items/" + sid + "/images/poster", nil},
		{"POST", "/items/" + sid + "/playback", device}, {"PUT", "/items/" + sid + "/progress", map[string]any{"positionMs": 1}},
		{"GET", "/collections", nil}, {"POST", "/collections", map[string]any{"name": "c", "itemIds": []string{sid}}},
		{"POST", "/playlists", map[string]any{"name": "p", "itemIds": []string{sid}}}, {"GET", "/playlists", nil},
		{"POST", "/downloads", map[string]any{"itemId": sid, "quality": "original"}}, {"GET", "/downloads", nil},
		{"POST", "/syncplay/groups", map[string]any{"name": "g", "itemIds": []string{sid}}},
		{"GET", "/system/stats", nil}, {"GET", "/admin/activity", nil},
		{"GET", "/fs/directories?path=" + adultDir, nil},
		{"GET", "/adult/status", nil}, {"GET", "/adult/scenes", nil}, {"GET", "/adult/scenes/" + sid, nil},
		{"POST", "/adult/scenes/" + sid + "/playback", device}, {"PUT", "/adult/scenes/" + sid + "/progress", map[string]any{"positionMs": 1}},
		{"GET", "/adult/studios", nil}, {"GET", "/adult/studios/" + stid, nil}, {"GET", "/adult/performers", nil},
		{"GET", "/adult/performers/" + pid, nil}, {"GET", "/adult/images/scene/" + sid + "/poster", nil},
		{"GET", "/adult/images/performer/" + pid + "/image", nil}, {"POST", "/adult/unlock", map[string]any{"pin": "1234"}},
		{"PUT", "/adult/preferences", map[string]any{"blur": false}}, {"POST", "/adult/scenes/" + sid + "/refresh", nil},
	}
	leaks := func(who string, c *client, isAdmin bool) {
		t.Helper()
		for _, cl := range native {
			s, body := rawCall(t, c, cl.method, cl.path, cl.body)
			if strings.HasPrefix(cl.path, "/adult/") && s != 404 {
				t.Errorf("%s %s %s: HTTP %d, want 404", who, cl.method, cl.path, s)
			}
			for _, m := range append(adultMarkers, sid, fid, stid, pid) {
				if strings.Contains(body, m) {
					t.Errorf("%s %s %s leaks %q: %s", who, cl.method, cl.path, m, body)
				}
			}
			if !isAdmin && strings.Contains(body, "Privatno") {
				t.Errorf("%s %s %s shows the adult library: %s", who, cl.method, cl.path, body)
			}
		}
		// The library list: administrators see the library (to manage it), nobody else.
		_, body := rawCall(t, c, "GET", "/libraries", nil)
		if isAdmin != strings.Contains(body, "Privatno") {
			t.Errorf("%s /libraries adult library visible=%v: %s", who, !isAdmin, body)
		}
		var me map[string]any
		c.do("GET", "/auth/me", nil, &me)
		if me["adultAccess"] == true {
			t.Errorf("%s: /auth/me says adult access", who)
		}
	}
	// Controls: the same calls do return normal content.
	for _, p := range []string{"/search?q=Film", "/home/latest", "/libraries/" + mixed.ID + "/items?limit=200"} {
		if s, body := rawCall(t, member, "GET", p, nil); s != 200 || !strings.Contains(body, "Film") && !strings.Contains(body, "Druga") {
			t.Errorf("control %s: %d %s", p, s, body)
		}
	}
	leaks("admin", adminC, true)
	leaks("member", member, false)
	leaks("invited", invited, false)
	leaks("kids profile", kidC, false)
	leaks("api key", keyC, true)

	// /api/stats (dashboards): no adult counts, no adult library.
	st, _, body := statsCall(t, "GET", srv.URL+"/api/stats", map[string]string{"X-Api-Key": adminKey}, "")
	if st != 200 {
		t.Fatalf("/api/stats: %d", st)
	}
	var stats map[string]any
	_ = json.Unmarshal([]byte(body), &stats)
	if stats["libraries"] != float64(1) || stats["movies"] != float64(2) || stats["videos"] != float64(2) {
		t.Errorf("/api/stats counts adult content: %s", body)
	}

	// Compat API used by third-party apps, for the administrator.
	cc := &compatCaller{t: t, base: srv.URL + "/compat", token: adminC.token}
	uid := compat.ID(uuid.MustParse(admin.User.ID))
	for _, p := range []string{"/UserViews?userId=" + uid, "/Users/" + uid + "/Views", "/Items?Recursive=true&searchTerm=Moonlit",
		"/Items?Recursive=true&IncludeItemTypes=Movie,Video,Episode&Limit=200", "/Users/" + uid + "/Items/Latest",
		"/Items/" + compat.ID(sceneID), "/Users/" + uid + "/Items/" + compat.ID(sceneID), "/Search/Hints?searchTerm=Velvetine",
		"/Shows/NextUp?userId=" + uid, "/UserItems/Resume?userId=" + uid, "/Items/" + compat.ID(sceneID) + "/PlaybackInfo",
		"/Videos/" + compat.ID(sceneID) + "/stream?static=true", "/Items/" + compat.ID(sceneID) + "/Images/Primary",
		"/Persons?searchTerm=Onyx", "/Studios?searchTerm=Velvetine", "/Items/Filters", "/Genres"} {
		s, b, _ := cc.raw("GET", p, nil, nil)
		if p == "/Items?Recursive=true&IncludeItemTypes=Movie,Video,Episode&Limit=200" && (s != 200 || !strings.Contains(string(b), "Film")) {
			t.Errorf("control: compat lists no film: %d %s", s, b)
		}
		for _, m := range append(adultMarkers, "Privatno", compat.ID(sceneID), compat.ID(libID)) {
			if strings.Contains(string(b), m) {
				t.Errorf("compat %s leaks %q: %s", p, m, b)
			}
		}
	}

	// DLNA (televisions), acting as the administrator.
	if s := adminC.do("PUT", "/settings/dlna", map[string]any{"enabled": true, "friendlyName": "TV", "language": "hr",
		"userId": admin.User.ID}, nil); s != 200 {
		t.Fatalf("dlna settings: %d", s)
	}
	d := &dlna.Server{Pool: testPool, Auth: testState.Auth(), Library: testState.Library(), Stream: testState.Stream(),
		Settings: testState.Settings, ServerID: testState.ServerID(), ServerName: func(context.Context) string { return "Kino" },
		FFmpeg: "ffmpeg-not-installed", Version: "test", Log: log, NowPlaying: &activity.Registry{}}
	tv := httptest.NewServer(d.Handler())
	defer tv.Close()
	d.Reload()
	seen := map[string]bool{}
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		if depth > 4 || seen[id] {
			return
		}
		seen[id] = true
		doc, raw := didlOf(t, tv.URL, id)
		for _, m := range append(adultMarkers, "Privatno", sid, lib.ID) {
			if strings.Contains(raw, m) {
				t.Errorf("DLNA %s leaks %q", id, m)
			}
		}
		for _, c := range doc.Containers {
			walk(c.ID, depth+1)
		}
	}
	sawFilm := false
	walk("0", 0)
	for id := range seen {
		if _, raw := didlOf(t, tv.URL, id); strings.Contains(raw, "Film") {
			sawFilm = true
		}
	}
	if !sawFilm {
		_, root := didlOf(t, tv.URL, "0")
		t.Errorf("control: DLNA does not show the film either: %v %s", seen, root)
	}
	if len(seen) < 2 {
		t.Fatalf("DLNA walk saw nothing: %v", seen)
	}

	// Webhooks: the normal film was announced, nothing adult was sent.
	time.Sleep(300 * time.Millisecond)
	hookMu.Lock()
	sent := strings.Join(hookBodies, "\n")
	hookMu.Unlock()
	for _, m := range append(adultMarkers, "Privatno", sid, lib.ID) {
		if strings.Contains(sent, m) {
			t.Errorf("webhook leaks %q: %s", m, sent)
		}
	}
	if !strings.Contains(sent, "Druga") {
		t.Errorf("control: the film's notification did not arrive: %q", sent)
	}

	// Server logs (admin log viewer) carry no adult names.
	if s, body := rawCall(t, adminC, "GET", "/admin/logs?limit=500", nil); s == 200 {
		for _, m := range adultMarkers {
			if strings.Contains(body, m) {
				t.Errorf("logs leak %q", m)
			}
		}
	}

	// PIN: a new session of the grantee is locked until the PIN is entered.
	if s := grantee.do("PUT", "/adult/preferences", map[string]any{"pin": "12ab"}, &e); s != 400 || e.Code != "invalid_pin_format" {
		t.Fatalf("bad PIN format: %d %+v", s, e)
	}
	if s := grantee.do("PUT", "/adult/preferences", map[string]any{"pin": "4321", "blur": false}, nil); s != 200 {
		t.Fatalf("set PIN: %d", s)
	}
	second := login(t, srv, "odrasli", "odrasli-pass-1")
	var status struct {
		PinSet, Unlocked, Blur bool
	}
	if second.do("GET", "/adult/status", nil, &status); !status.PinSet || status.Unlocked || status.Blur {
		t.Fatalf("status in a new session: %+v", status)
	}
	if s := second.do("GET", "/adult/scenes", nil, &e); s != 403 || e.Code != "adult_locked" {
		t.Fatalf("locked section: %d %+v", s, e)
	}
	if s := second.do("GET", "/adult/images/scene/"+sid+"/poster", nil, nil); s != 403 {
		t.Fatalf("image of a locked section: %d", s)
	}
	if s := second.do("POST", "/adult/unlock", map[string]any{"pin": "0000"}, &e); s != 403 || e.Code != "invalid_pin" {
		t.Fatalf("wrong PIN: %d %+v", s, e)
	}
	if s := second.do("POST", "/adult/unlock", map[string]any{"pin": "4321"}, nil); s != 204 {
		t.Fatalf("unlock: %d", s)
	}
	if s := second.do("GET", "/adult/scenes", nil, nil); s != 200 {
		t.Fatalf("after unlock: %d", s)
	}
	second.do("POST", "/adult/lock", nil, nil)
	if s := second.do("GET", "/adult/scenes", nil, nil); s != 403 {
		t.Fatalf("after lock: %d", s)
	}

	// Taking access away works at once, also for open sessions.
	if s := adminC.do("PUT", "/users/"+granteeMe.ID+"/adult-access", map[string]any{"enabled": false}, nil); s != 200 {
		t.Fatalf("revoke: %d", s)
	}
	if s := grantee.do("GET", "/adult/scenes", nil, nil); s != 404 {
		t.Fatalf("after revoke: %d", s)
	}

	// Deleting the library removes the adult rows and leaves the files alone.
	if s := adminC.do("DELETE", "/libraries/"+lib.ID, nil, nil); s != 204 {
		t.Fatalf("delete adult library: %d", s)
	}
	_ = testPool.QueryRow(ctx, `SELECT count(*) FROM adult_scenes`).Scan(&n)
	if n != 0 {
		t.Fatalf("adult scenes left: %d", n)
	}
	if _, err := os.Stat(filepath.Join(adultDir, "Velvetine")); err != nil {
		t.Fatalf("adult files were touched: %v", err)
	}
}
