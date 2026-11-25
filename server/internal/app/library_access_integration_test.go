package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/notify"
)

// Words that exist only in the hidden library.
var hiddenMarkers = []string{"Skriveni", "Tajna Serija", "Treca Tajna"}

type accessUser struct {
	ID           string   `json:"id"`
	Username     string   `json:"username"`
	DisplayName  string   `json:"displayName"`
	LibraryIDs   []string `json:"libraryIds"`
	NewLibraries bool     `json:"newLibraries"`
	Permissions  map[string]any
}

// TestLibraryAccessIntegration (D-054): an account limited to one library reaches
// nothing of another library through any surface (native API, compat API, DLNA,
// downloads, search, home rows, images, streams, collections, playlists,
// watch-together, notifications), while the allowed library keeps working.
func TestLibraryAccessIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	admin := setupAdmin(t, adminC)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	imageDir := t.TempDir()
	testState.Library().ImageDir = imageDir

	write := func(rel, data string) {
		t.Helper()
		p := filepath.Join(mediaRoot, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("vis/Vidljivi Film (2020)/Vidljivi Film (2020).mkv", "visible-bytes")
	write("hid/Skriveni Film (2021)/Skriveni Film (2021).mkv", "hidden-bytes")
	write("hid/Tajna Serija/Season 01/Tajna Serija S01E01.mkv", "e1")
	write("hid/Tajna Serija/Season 01/Tajna Serija S01E02.mkv", "e2")
	probe := func(context.Context, string) (library.ProbeResult, error) {
		return library.ProbeResult{Container: "mkv", VideoCodec: "h264", Width: 1920, Height: 1080, DurationMS: 600_000,
			Audio: []library.AudioStream{{Codec: "aac", Channels: 2}}}, nil
	}
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: log, Probe: probe}
	var visLib, hidLib struct{ ID string }
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "Vidljivo", "kind": "movies", "paths": []string{filepath.Join(mediaRoot, "vis")}}, &visLib); s != 201 {
		t.Fatalf("create visible library: %d", s)
	}
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "Zakljucano", "kind": "mixed", "paths": []string{filepath.Join(mediaRoot, "hid")}}, &hidLib); s != 201 {
		t.Fatalf("create hidden library: %d", s)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE name IN ('Vidljivo', 'Zakljucano', 'Nova', 'Privatno2')`)
	})
	for _, id := range []string{visLib.ID, hidLib.ID} {
		if _, err := scanner.Scan(ctx, uuid.MustParse(id)); err != nil {
			t.Fatal(err)
		}
	}
	itemID := func(title string) string {
		t.Helper()
		var id uuid.UUID
		if err := testPool.QueryRow(ctx, `SELECT id FROM items WHERE title = $1 OR metadata_title = $1 LIMIT 1`, title).Scan(&id); err != nil {
			t.Fatalf("item %q: %v", title, err)
		}
		return id.String()
	}
	visFilm, hidFilm, hidSeries := itemID("Vidljivi Film"), itemID("Skriveni Film"), itemID("Tajna Serija")
	var ep1, ep2, hidFile, visFile string
	_ = testPool.QueryRow(ctx, `SELECT e.id FROM items e WHERE e.kind = 'episode' AND e.episode_number = 1
		AND e.library_id = $1`, hidLib.ID).Scan(&ep1)
	_ = testPool.QueryRow(ctx, `SELECT e.id FROM items e WHERE e.kind = 'episode' AND e.episode_number = 2
		AND e.library_id = $1`, hidLib.ID).Scan(&ep2)
	_ = testPool.QueryRow(ctx, `SELECT id FROM media_files WHERE item_id = $1`, hidFilm).Scan(&hidFile)
	_ = testPool.QueryRow(ctx, `SELECT id FROM media_files WHERE item_id = $1`, visFilm).Scan(&visFile)
	if ep1 == "" || ep2 == "" || hidFile == "" || visFile == "" {
		t.Fatalf("setup: episodes %q %q files %q %q", ep1, ep2, hidFile, visFile)
	}
	// Artwork for both films.
	for _, id := range []string{visFilm, hidFilm} {
		p := metadata.ImagePath(imageDir, uuid.MustParse(id), "poster")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, pngBytes(t), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hiddenIDs := []string{hidLib.ID, hidFilm, hidSeries, ep1, ep2, hidFile}

	// The viewer starts with every library and leaves traces in the hidden one.
	var viewerU accessUser
	if s := adminC.do("POST", "/users", map[string]any{"username": "gledatelj", "password": "gledatelj-pass-1"}, &viewerU); s != 201 ||
		len(viewerU.LibraryIDs) != 0 || viewerU.Permissions["allLibraries"] != true {
		t.Fatalf("create viewer: %d %+v", s, viewerU)
	}
	viewerC := login(t, srv, "gledatelj", "gledatelj-pass-1")
	device := map[string]any{"device": map[string]any{"containers": []string{"mkv"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}}}
	for _, c := range []struct {
		method, path string
		body         any
		want         int
	}{
		{"PUT", "/items/" + hidFilm + "/progress", map[string]any{"positionMs": 200_000, "durationMs": 600_000}, 204},
		{"PUT", "/items/" + ep1 + "/user-data", map[string]any{"played": true}, 204},
		{"PUT", "/items/" + hidFilm + "/user-data", map[string]any{"favorite": true}, 204},
		{"POST", "/playlists", map[string]any{"name": "Moja lista", "itemIds": []string{hidFilm, visFilm}}, 201},
		{"POST", "/downloads", map[string]any{"itemId": hidFilm, "quality": "original"}, 201},
	} {
		if s, body := rawCall(t, viewerC, c.method, c.path, c.body); s != c.want {
			t.Fatalf("before the limit %s %s: %d %s", c.method, c.path, s, body)
		}
	}
	if _, body := rawCall(t, viewerC, "GET", "/home/continue", nil); !strings.Contains(body, hidFilm) {
		t.Fatalf("setup: continue watching lacks the hidden film: %s", body)
	}
	if _, body := rawCall(t, viewerC, "GET", "/home/next-up", nil); !strings.Contains(body, ep2) {
		t.Fatalf("setup: next up lacks episode 2: %s", body)
	}
	// Collections and a public playlist of the administrator.
	var onlyHidden, mixedCol struct {
		ID        string
		ItemCount int
	}
	adminC.do("POST", "/collections", map[string]any{"name": "Samo Skriveni", "itemIds": []string{hidFilm}}, &onlyHidden)
	adminC.do("POST", "/collections", map[string]any{"name": "Mjesovito", "itemIds": []string{hidFilm, visFilm}}, &mixedCol)
	var pubPl struct{ ID string }
	adminC.do("POST", "/playlists", map[string]any{"name": "Javna", "public": true, "itemIds": []string{hidFilm, visFilm}}, &pubPl)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM collections WHERE name IN ('Samo Skriveni', 'Mjesovito')`)
	})

	// Limit the viewer to the visible library.
	if s := adminC.do("PATCH", "/users/"+viewerU.ID, map[string]any{"permissions": withPerm(viewerU.Permissions, "allLibraries", false),
		"libraryIds": []string{visLib.ID}}, &viewerU); s != 200 || !slices.Equal(viewerU.LibraryIDs, []string{visLib.ID}) {
		t.Fatalf("limit viewer: %d %+v", s, viewerU)
	}
	// A kids profile on the limited account can never see more.
	var kid struct{ ID string }
	viewerC.do("POST", "/profiles", map[string]any{"name": "Mali", "color": "#3dd68c", "isKid": true}, &kid)
	kidC := login(t, srv, "gledatelj", "gledatelj-pass-1")
	if s := kidC.do("POST", "/profiles/"+kid.ID+"/select", nil, nil); s != 204 {
		t.Fatalf("select kids profile: %d", s)
	}

	// --- native API -----------------------------------------------------------------
	type call struct {
		method, path string
		body         any
		status       int // 0 = any
	}
	native := []call{
		{"GET", "/libraries", nil, 200},
		{"GET", "/libraries/" + hidLib.ID + "/items", nil, 404},
		{"GET", "/libraries/" + hidLib.ID + "/genres", nil, 404},
		{"GET", "/libraries/" + hidLib.ID + "/authors", nil, 404},
		{"GET", "/home/latest", nil, 200}, {"GET", "/home/favorites", nil, 200}, {"GET", "/home/continue", nil, 200},
		{"GET", "/home/next-up", nil, 200}, {"GET", "/home/continue-reading", nil, 200},
		{"GET", "/search?q=Skriveni", nil, 200}, {"GET", "/search?q=Tajna", nil, 200},
		{"GET", "/items/" + hidFilm, nil, 404}, {"GET", "/items/" + hidSeries, nil, 404}, {"GET", "/items/" + ep1 + "/next", nil, 404},
		{"GET", "/items/" + hidFilm + "/images/poster", nil, 404},
		{"POST", "/items/" + hidFilm + "/playback", device, 404},
		{"PUT", "/items/" + hidFilm + "/progress", map[string]any{"positionMs": 1}, 404},
		{"PUT", "/items/" + hidFilm + "/user-data", map[string]any{"favorite": false}, 404},
		{"GET", "/trickplay/" + hidFile + "/0", nil, 404},
		{"GET", "/collections", nil, 200}, {"GET", "/collections/" + onlyHidden.ID, nil, 404},
		{"GET", "/collections/" + onlyHidden.ID + "/images/poster", nil, 404},
		{"GET", "/collections/" + mixedCol.ID, nil, 200},
		{"GET", "/playlists", nil, 200}, {"GET", "/playlists/" + pubPl.ID, nil, 200},
		{"POST", "/playlists", map[string]any{"name": "Nova lista", "itemIds": []string{hidFilm}}, 201},
		{"POST", "/downloads", map[string]any{"itemId": hidFilm, "quality": "original"}, 404},
		{"POST", "/downloads", map[string]any{"itemId": hidSeries, "quality": "original"}, 404},
		{"GET", "/downloads", nil, 200},
		{"POST", "/downloads/sync", map[string]any{"itemIds": []string{hidFilm}, "progress": []any{}}, 200},
		{"POST", "/items/" + hidFilm + "/original", map[string]any{}, 404},
		{"POST", "/syncplay/groups", map[string]any{"name": "g", "itemIds": []string{hidFilm}}, 403},
		{"GET", "/discover", nil, 0},
	}
	leaks := func(who string, c *client) {
		t.Helper()
		for _, cl := range native {
			s, body := rawCall(t, c, cl.method, cl.path, cl.body)
			if cl.status != 0 && s != cl.status {
				t.Errorf("%s %s %s: HTTP %d, want %d: %s", who, cl.method, cl.path, s, cl.status, body)
			}
			if strings.HasPrefix(cl.path, "/downloads/sync") {
				// The hidden download comes back revoked, by id: that is the point.
				var res struct{ Revoked []string }
				_ = json.Unmarshal([]byte(body), &res)
				if !slices.Contains(res.Revoked, hidFilm) {
					t.Errorf("%s: offline copy of the hidden film not revoked: %s", who, body)
				}
				continue
			}
			for _, m := range append(append([]string{}, hiddenMarkers...), hiddenIDs...) {
				if strings.Contains(body, m) {
					t.Errorf("%s %s %s leaks %q: %s", who, cl.method, cl.path, m, body)
				}
			}
			if strings.Contains(body, "Zakljucano") || strings.Contains(body, "Samo Skriveni") {
				t.Errorf("%s %s %s shows the hidden library or its collection: %s", who, cl.method, cl.path, body)
			}
		}
	}
	leaks("limited viewer", viewerC)
	leaks("kids profile", kidC)

	// Controls: the allowed library works through the same calls.
	controls := []struct{ path, want string }{
		{"/libraries", "Vidljivo"}, {"/libraries/" + visLib.ID + "/items", "Vidljivi"}, {"/search?q=Vidljivi", "Vidljivi"},
		{"/home/latest", "Vidljivi"}, {"/items/" + visFilm, "Vidljivi"}, {"/collections", "Mjesovito"},
	}
	for _, c := range controls {
		for who, cl := range map[string]*client{"viewer": viewerC, "kid": kidC} {
			if s, body := rawCall(t, cl, "GET", c.path, nil); s != 200 || !strings.Contains(body, c.want) {
				t.Errorf("control %s %s: %d %s", who, c.path, s, body)
			}
		}
	}
	if s, _ := rawCall(t, viewerC, "GET", "/items/"+visFilm+"/images/poster", nil); s != 200 {
		t.Errorf("control: visible poster: %d", s)
	}
	var pb struct{ Method, URL string }
	if s := viewerC.do("POST", "/items/"+visFilm+"/playback", device, &pb); s != 200 || pb.URL == "" {
		t.Errorf("control: visible playback: %d %+v", s, pb)
	} else if r, err := http.Get(srv.URL + pb.URL); err != nil || r.StatusCode != 200 {
		t.Errorf("control: visible stream: %v %v", r, err)
	}
	var mixed struct {
		Collection struct{ ItemCount int }
		Items      []struct{ ID string }
	}
	viewerC.do("GET", "/collections/"+mixedCol.ID, nil, &mixed)
	if mixed.Collection.ItemCount != 1 || len(mixed.Items) != 1 || mixed.Items[0].ID != visFilm {
		t.Errorf("mixed collection for the viewer: %+v", mixed)
	}
	var pls []struct {
		Name      string
		ItemCount int
	}
	viewerC.do("GET", "/playlists", nil, &pls)
	for _, p := range pls {
		if p.Name == "Javna" && p.ItemCount != 1 || p.Name == "Nova lista" && p.ItemCount != 0 {
			t.Errorf("playlist counts hidden titles: %+v", pls)
		}
	}
	// The administrator still sees everything, the hidden collection keeps its title.
	if s, body := rawCall(t, adminC, "GET", "/collections/"+onlyHidden.ID, nil); s != 200 || !strings.Contains(body, hidFilm) {
		t.Errorf("admin hidden collection: %d %s", s, body)
	}
	if s, body := rawCall(t, adminC, "GET", "/libraries", nil); s != 200 || !strings.Contains(body, "Zakljucano") {
		t.Errorf("admin libraries: %d %s", s, body)
	}

	// --- compat API ------------------------------------------------------------------
	cc := &compatCaller{t: t, base: srv.URL + "/compat", token: viewerC.token}
	uid := compat.ID(uuid.MustParse(viewerU.ID))
	var me struct {
		Policy struct {
			EnableAllFolders bool
			EnabledFolders   []string
		}
	}
	if s := cc.call("GET", "/Users/Me", nil, &me); s != 200 || me.Policy.EnableAllFolders ||
		!slices.Equal(me.Policy.EnabledFolders, []string{compat.ID(uuid.MustParse(visLib.ID))}) {
		t.Errorf("compat policy: %d %+v", s, me)
	}
	cid := func(s string) string { return compat.ID(uuid.MustParse(s)) }
	compatPaths := []string{"/UserViews?userId=" + uid, "/Users/" + uid + "/Views",
		"/Items?Recursive=true&IncludeItemTypes=Movie,Series,Episode&Limit=200", "/Items?Recursive=true&searchTerm=Skriveni",
		"/Items?ParentId=" + cid(hidLib.ID), "/Users/" + uid + "/Items/Latest", "/Users/" + uid + "/Items/Latest?ParentId=" + cid(hidLib.ID),
		"/Items/" + cid(hidFilm), "/Items/" + cid(hidLib.ID), "/Users/" + uid + "/Items/" + cid(hidFilm),
		"/Search/Hints?searchTerm=Tajna", "/Shows/NextUp?userId=" + uid, "/Shows/NextUp?SeriesId=" + cid(hidSeries),
		"/Shows/" + cid(hidSeries) + "/Seasons", "/Shows/" + cid(hidSeries) + "/Episodes", "/UserItems/Resume?userId=" + uid,
		"/Items/" + cid(hidFilm) + "/PlaybackInfo", "/Videos/" + cid(hidFilm) + "/stream?static=true",
		"/Items/" + cid(hidFilm) + "/Images/Primary", "/Items/" + cid(hidFilm) + "/Similar", "/Items/" + cid(ep1) + "/Ancestors",
		"/UserItems/" + cid(hidFilm) + "/UserData", "/MediaSegments/" + cid(hidFilm), "/Genres", "/Items/Filters"}
	for _, p := range compatPaths {
		s, b, _ := cc.raw("GET", p, nil, nil)
		for _, m := range append(append([]string{"Zakljucano"}, hiddenMarkers...), cid(hidFilm), cid(hidLib.ID), cid(hidSeries), cid(ep1), cid(ep2)) {
			if strings.Contains(string(b), m) && (!strings.HasPrefix(p, "/MediaSegments") || s != 404) {
				t.Errorf("compat %s leaks %q: %d %s", p, m, s, b)
			}
		}
		if strings.Contains(p, "/stream") || strings.Contains(p, "/Images/") {
			if s != 404 {
				t.Errorf("compat %s: %d, want 404", p, s)
			}
		}
	}
	if s, _, _ := cc.raw("POST", "/Items/"+cid(hidFilm)+"/PlaybackInfo", map[string]any{}, nil); s != 404 {
		t.Errorf("compat POST PlaybackInfo of a hidden film: %d", s)
	}
	if s, _, _ := cc.raw("POST", "/UserFavoriteItems/"+cid(hidFilm), nil, nil); s != 404 {
		t.Errorf("compat favorite of a hidden film: %d", s)
	}
	if s, b, _ := cc.raw("GET", "/Items?Recursive=true&IncludeItemTypes=Movie&Limit=200", nil, nil); s != 200 || !strings.Contains(string(b), "Vidljivi") {
		t.Errorf("control: compat lists no visible film: %d %s", s, b)
	}
	if s, b, _ := cc.raw("GET", "/UserViews?userId="+uid, nil, nil); s != 200 || !strings.Contains(string(b), "Vidljivo") {
		t.Errorf("control: compat views: %d %s", s, b)
	}
	if s, _, _ := cc.raw("GET", "/Items/"+cid(visFilm)+"/Images/Primary", nil, nil); s != 200 {
		t.Errorf("control: compat visible image: %d", s)
	}

	// --- DLNA, acting as the limited viewer --------------------------------------------
	if s := adminC.do("PUT", "/settings/dlna", map[string]any{"enabled": true, "friendlyName": "TV", "language": "hr",
		"userId": viewerU.ID}, nil); s != 200 {
		t.Fatalf("dlna settings: %d", s)
	}
	d := &dlna.Server{Pool: testPool, Auth: testState.Auth(), Library: testState.Library(), Stream: testState.Stream(),
		Settings: testState.Settings, ServerID: testState.ServerID(), ServerName: func(context.Context) string { return "Kino" },
		FFmpeg: "ffmpeg-not-installed", Version: "test", Log: log, NowPlaying: &activity.Registry{}}
	tv := httptest.NewServer(d.Handler())
	defer tv.Close()
	d.Reload()
	seen := map[string]bool{}
	sawFilm := false
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		if depth > 5 || seen[id] {
			return
		}
		seen[id] = true
		doc, raw := didlOf(t, tv.URL, id)
		if strings.Contains(raw, "Vidljivi") {
			sawFilm = true
		}
		for _, m := range append(append([]string{"Zakljucano"}, hiddenMarkers...), hiddenIDs...) {
			if strings.Contains(raw, m) {
				t.Errorf("DLNA %s leaks %q", id, m)
			}
		}
		for _, c := range doc.Containers {
			walk(c.ID, depth+1)
		}
	}
	walk("0", 0)
	for _, id := range []string{"l:" + hidLib.ID, "i:" + hidFilm} {
		code, raw := soapCall(t, tv.URL, "Browse", "<ObjectID>"+id+"</ObjectID><BrowseFlag>BrowseMetadata</BrowseFlag><Filter>*</Filter>"+
			"<StartingIndex>0</StartingIndex><RequestedCount>50</RequestedCount><SortCriteria></SortCriteria>", nil)
		if code == 200 || !strings.Contains(raw, "701") || strings.Contains(raw, "Skriveni") {
			t.Errorf("DLNA direct %s: %d %s", id, code, raw)
		}
	}
	if code, body := soapCall(t, tv.URL, "Search", "<ContainerID>0</ContainerID><SearchCriteria>dc:title contains \"Skriveni\"</SearchCriteria>"+
		"<Filter>*</Filter><StartingIndex>0</StartingIndex><RequestedCount>50</RequestedCount><SortCriteria></SortCriteria>", nil); code == 200 &&
		strings.Contains(body, "Skriveni") {
		t.Errorf("DLNA search leaks: %s", body)
	}
	if !sawFilm {
		t.Errorf("control: DLNA does not show the visible film (walked %v)", seen)
	}

	// --- notifications ------------------------------------------------------------------
	var hookMu sync.Mutex
	got := map[string][]string{}
	hook := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		hookMu.Lock()
		got[r.URL.Path] = append(got[r.URL.Path], string(b))
		hookMu.Unlock()
	}))
	defer hook.Close()
	if s := adminC.do("PUT", "/settings/notifications", map[string]any{"targets": []any{
		map[string]any{"id": uuid.NewString(), "name": "Djeca", "kind": "generic", "url": hook.URL + "/kids", "enabled": true,
			"events": []string{notify.ItemAdded}, "libraryIds": []string{visLib.ID}},
		map[string]any{"id": uuid.NewString(), "name": "Sve", "kind": "generic", "url": hook.URL + "/all", "enabled": true,
			"events": []string{notify.ItemAdded}},
	}}, nil); s != 200 {
		t.Fatalf("save webhooks: %d", s)
	}
	var stored struct {
		Targets []struct {
			Name       string
			LibraryIDs []string `json:"libraryIds"`
		}
	}
	adminC.do("GET", "/settings/notifications", nil, &stored)
	if len(stored.Targets) != 2 || !slices.Equal(stored.Targets[0].LibraryIDs, []string{visLib.ID}) {
		t.Fatalf("stored targets: %+v", stored)
	}
	worker := &jobs.LibraryScanWorker{Scanner: scanner, Log: log, Notify: &notify.Dispatcher{Settings: testState.Settings, Log: log}}
	runScan := func(id string) {
		t.Helper()
		job := &river.Job[jobs.LibraryScanArgs]{JobRow: &rivertype.JobRow{ID: time.Now().UnixNano(), Kind: "library.scan"},
			Args: jobs.LibraryScanArgs{LibraryID: uuid.MustParse(id)}}
		if err := worker.Work(ctx, job); err != nil {
			t.Fatalf("scan job: %v", err)
		}
	}
	write("hid/Treca Tajna (2022)/Treca Tajna (2022).mkv", "x")
	write("vis/Drugi Vidljivi (2023)/Drugi Vidljivi (2023).mkv", "x")
	runScan(hidLib.ID)
	runScan(visLib.ID)
	deadline := time.Now().Add(5 * time.Second)
	for {
		hookMu.Lock()
		n := len(got["/kids"]) + len(got["/all"])
		hookMu.Unlock()
		if n >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	hookMu.Lock()
	kids, all := strings.Join(got["/kids"], "\n"), strings.Join(got["/all"], "\n")
	hookMu.Unlock()
	if strings.Contains(kids, "Treca") || !strings.Contains(kids, "Drugi Vidljivi") {
		t.Errorf("limited webhook: %q", kids)
	}
	if !strings.Contains(all, "Treca") || !strings.Contains(all, "Drugi Vidljivi") {
		t.Errorf("unlimited webhook: %q", all)
	}

	// --- adult libraries are never part of the list (D-053) ------------------------------
	write("priv/Studio/Studio - 2024-01-15 - Scena [Netko] WEBDL-1080p.mp4", "x")
	var adultLib struct{ ID string }
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "Privatno2", "kind": "adult", "paths": []string{filepath.Join(mediaRoot, "priv")},
		"confirmAdult": true}, &adultLib); s != 201 {
		t.Fatalf("create adult library: %d", s)
	}
	if s := adminC.do("PATCH", "/users/"+viewerU.ID, map[string]any{"libraryIds": []string{visLib.ID, adultLib.ID}}, &viewerU); s != 200 ||
		!slices.Equal(viewerU.LibraryIDs, []string{visLib.ID}) {
		t.Errorf("adult library in the list: %d %+v", s, viewerU)
	}
	if s := viewerC.do("GET", "/adult/status", nil, nil); s != 404 {
		t.Errorf("listed viewer reaches the adult section: %d", s)
	}
	if _, body := rawCall(t, viewerC, "GET", "/libraries", nil); strings.Contains(body, "Privatno2") {
		t.Errorf("adult library listed for the viewer: %s", body)
	}
	if ids, _ := testState.Library().VisibleLibraryIDs(ctx, uuid.MustParse(admin.User.ID)); slices.Contains(ids, uuid.MustParse(adultLib.ID)) {
		t.Errorf("all libraries includes the adult library: %v", ids)
	}

	// --- libraries created later -------------------------------------------------------
	var second, everyone accessUser
	adminC.do("POST", "/users", map[string]any{"username": "drugi", "password": "drugi-pass-1",
		"permissions": withPerm(viewerU.Permissions, "allLibraries", false), "libraryIds": []string{visLib.ID}}, &second)
	adminC.do("POST", "/users", map[string]any{"username": "svi", "password": "svi-pass-1"}, &everyone)
	if !slices.Equal(second.LibraryIDs, []string{visLib.ID}) || second.NewLibraries {
		t.Fatalf("create with a list: %+v", second)
	}
	if s := adminC.do("PATCH", "/users/"+viewerU.ID, map[string]any{"newLibraries": true}, &viewerU); s != 200 ||
		!viewerU.NewLibraries || !slices.Equal(viewerU.LibraryIDs, []string{visLib.ID}) {
		t.Fatalf("newLibraries alone keeps the list: %d %+v", s, viewerU)
	}
	_ = os.MkdirAll(filepath.Join(mediaRoot, "nova"), 0o755)
	var nova struct{ ID string }
	adminC.do("POST", "/libraries", map[string]any{"name": "Nova", "kind": "movies", "paths": []string{filepath.Join(mediaRoot, "nova")}}, &nova)
	sees := func(c *client, name string) bool {
		_, body := rawCall(t, c, "GET", "/libraries", nil)
		return strings.Contains(body, name)
	}
	if !sees(viewerC, "Nova") || sees(login(t, srv, "drugi", "drugi-pass-1"), "Nova") || !sees(login(t, srv, "svi", "svi-pass-1"), "Nova") {
		t.Errorf("new library: viewer %v, drugi %v, svi %v", sees(viewerC, "Nova"), sees(login(t, srv, "drugi", "drugi-pass-1"), "Nova"),
			sees(login(t, srv, "svi", "svi-pass-1"), "Nova"))
	}
	// Deleting a library removes it from every list.
	if s := adminC.do("DELETE", "/libraries/"+nova.ID, nil, nil); s != 204 {
		t.Fatalf("delete library: %d", s)
	}
	var n int
	_ = testPool.QueryRow(ctx, `SELECT count(*) FROM user_library_access WHERE library_id = $1`, nova.ID).Scan(&n)
	if n != 0 {
		t.Errorf("access rows of a deleted library: %d", n)
	}

	// --- invites carry the list ----------------------------------------------------------
	var inv struct {
		Token  string
		Invite struct {
			LibraryIDs []string `json:"libraryIds"`
		}
	}
	if s := adminC.do("POST", "/invites", map[string]any{"expiresInHours": 1, "maxUses": 1,
		"permissions": withPerm(viewerU.Permissions, "allLibraries", false), "libraryIds": []string{visLib.ID, adultLib.ID}}, &inv); s != 201 ||
		!slices.Equal(inv.Invite.LibraryIDs, []string{visLib.ID}) {
		t.Fatalf("invite with a list: %d %+v", s, inv)
	}
	invited := newClient(t, srv)
	var invRes authResult
	if s := invited.do("POST", "/invite/"+inv.Token, map[string]any{"username": "pozvani", "password": "pozvani-pass-1"}, &invRes); s != 200 {
		t.Fatalf("accept invite: %d", s)
	}
	invited.token = invRes.Token
	if sees(invited, "Zakljucano") || !sees(invited, "Vidljivo") {
		t.Errorf("invited account libraries wrong")
	}
	if s, body := rawCall(t, invited, "GET", "/items/"+hidFilm, nil); s != 404 {
		t.Errorf("invited account opens the hidden film: %d %s", s, body)
	}

	// --- the user editor: rename, display name, sign out everywhere ------------------------
	var e apiError
	if s := adminC.do("PATCH", "/users/"+viewerU.ID, map[string]any{"username": "drugi"}, &e); s != 409 || e.Code != "username_taken" {
		t.Errorf("rename to a taken name: %d %+v", s, e)
	}
	if s := adminC.do("PATCH", "/users/"+viewerU.ID, map[string]any{"username": "Gledateljica", "displayName": " Ana "}, &viewerU); s != 200 ||
		viewerU.Username != "Gledateljica" || viewerU.DisplayName != "Ana" {
		t.Errorf("rename: %d %+v", s, viewerU)
	}
	if s := viewerC.do("GET", "/auth/me", nil, nil); s != 200 {
		t.Errorf("rename signed the user out: %d", s)
	}
	if s := adminC.do("PATCH", "/users/"+viewerU.ID, map[string]any{"signOutEverywhere": true}, nil); s != 200 {
		t.Fatalf("sign out everywhere: %d", s)
	}
	if s := viewerC.do("GET", "/auth/me", nil, nil); s != 401 {
		t.Errorf("session survived sign out everywhere: %d", s)
	}
}

func withPerm(p map[string]any, key string, v any) map[string]any {
	out := map[string]any{}
	for k, x := range p {
		out[k] = x
	}
	out[key] = v
	return out
}
