package app

import (
	"context"
	"encoding/xml"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/activity"
	"github.com/streamero-app/streamero/server/internal/dlna"
	"github.com/streamero-app/streamero/server/internal/library"
)

// soapCall posts a ContentDirectory action like a TV does.
func soapCall(t *testing.T, base, action, args string, hdr map[string]string) (int, string) {
	t.Helper()
	body := `<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action +
		` xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">` + args + `</u:` + action + `></s:Body></s:Envelope>`
	req, _ := http.NewRequest("POST", base+"/dlna/ctl/cds", strings.NewReader(body))
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ContentDirectory:1#`+action+`"`)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

type didlDoc struct {
	Containers []struct {
		ID    string `xml:"id,attr"`
		Title string `xml:"http://purl.org/dc/elements/1.1/ title"`
	} `xml:"container"`
	Items []struct {
		ID    string `xml:"id,attr"`
		Title string `xml:"http://purl.org/dc/elements/1.1/ title"`
		Res   []struct {
			ProtocolInfo string `xml:"protocolInfo,attr"`
			URL          string `xml:",chardata"`
		} `xml:"res"`
	} `xml:"item"`
}

var resultRE = regexp.MustCompile(`(?s)<Result>(.*)</Result>.*<TotalMatches>(\d+)</TotalMatches>`)

func browse(t *testing.T, base, id, flag string, hdr map[string]string) (didlDoc, string) {
	t.Helper()
	code, body := soapCall(t, base, "Browse", "<ObjectID>"+id+"</ObjectID><BrowseFlag>"+flag+
		"</BrowseFlag><Filter>*</Filter><StartingIndex>0</StartingIndex><RequestedCount>50</RequestedCount><SortCriteria></SortCriteria>", hdr)
	if code != 200 {
		t.Fatalf("browse %s: HTTP %d %s", id, code, body)
	}
	m := resultRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("browse %s: no result: %s", id, body)
	}
	var d didlDoc
	if err := xml.Unmarshal([]byte(html.UnescapeString(m[1])), &d); err != nil {
		t.Fatalf("browse %s: DIDL: %v", id, err)
	}
	return d, m[2]
}

func TestDLNAIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	admin := setupAdmin(t, adminC)
	ctx := context.Background()

	movies := mediaRoot + "/movies"
	payload := []byte("0123456789abcdefghij")
	if err := os.MkdirAll(movies+"/Film (2020)", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(movies+"/Film (2020)/Film (2020).mkv", payload, 0o644); err != nil {
		t.Fatal(err)
	}
	var lib struct {
		ID string `json:"id"`
	}
	libName := "Filmovi " + uuid.NewString()[:8]
	adminC.do("POST", "/libraries", map[string]any{"name": libName, "kind": "movies", "paths": []string{movies}}, &lib)
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE id = $1`, lib.ID) })
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv", VideoCodec: "h264", Width: 1920, Height: 1080, DurationMS: 1_000_000,
				Audio: []library.AudioStream{{Codec: "aac", Channels: 2}}}, nil
		}}
	if _, err := scanner.Scan(ctx, uuid.MustParse(lib.ID)); err != nil {
		t.Fatal(err)
	}

	// Settings: turning on needs a user.
	var e apiError
	if s := adminC.do("PUT", "/settings/dlna", map[string]any{"enabled": true, "friendlyName": "", "language": "hr"}, &e); s != 400 {
		t.Fatalf("enable without a user: HTTP %d", s)
	}
	var ov struct {
		Settings struct {
			Enabled bool   `json:"enabled"`
			UserID  string `json:"userId"`
		} `json:"settings"`
		Status struct {
			Available bool `json:"available"`
		} `json:"status"`
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"users"`
	}
	if s := adminC.do("GET", "/settings/dlna", nil, &ov); s != 200 || ov.Settings.Enabled || len(ov.Users) != 1 || ov.Users[0].Username != "Admin" {
		t.Fatalf("defaults: HTTP %d %+v", s, ov)
	}

	d := &dlna.Server{Pool: testPool, Auth: testState.Auth(), Library: testState.Library(), Stream: testState.Stream(),
		Settings: testState.Settings, ServerID: testState.ServerID(), ServerName: func(context.Context) string { return "Kino" },
		FFmpeg: "ffmpeg-not-installed", Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil)), NowPlaying: &activity.Registry{}}
	tv := httptest.NewServer(d.Handler())
	t.Cleanup(tv.Close)
	if resp, err := http.Get(tv.URL + dlna.PathDescription); err != nil || resp.StatusCode != 404 {
		t.Fatalf("description while off: %v %v", resp, err)
	}

	if s := adminC.do("PUT", "/settings/dlna", map[string]any{"enabled": true, "friendlyName": "Dnevna soba", "language": "hr",
		"userId": admin.User.ID, "profileId": uuid.NewString()}, &e); s != 400 {
		t.Fatalf("profile of nobody: HTTP %d", s)
	}
	if s := adminC.do("PUT", "/settings/dlna", map[string]any{"enabled": true, "friendlyName": "Dnevna soba", "language": "hr",
		"userId": admin.User.ID}, &ov); s != 200 || !ov.Settings.Enabled || ov.Settings.UserID != admin.User.ID {
		t.Fatalf("enable: HTTP %d %+v", s, ov)
	}
	d.Reload()

	// Device description and service descriptions.
	resp, err := http.Get(tv.URL + dlna.PathDescription)
	if err != nil {
		t.Fatal(err)
	}
	desc, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(desc), "<friendlyName>Dnevna soba</friendlyName>") ||
		!strings.Contains(string(desc), "urn:schemas-upnp-org:service:ContentDirectory:1") {
		t.Fatalf("description: %d %s", resp.StatusCode, desc)
	}
	for _, p := range []string{"/dlna/cds.xml", "/dlna/cms.xml"} {
		if r, err := http.Get(tv.URL + p); err != nil || r.StatusCode != 200 {
			t.Fatalf("%s: %v %v", p, r, err)
		}
	}

	// Only the local network: a public client address is refused.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", dlna.PathDescription, nil)
	req.RemoteAddr = "8.8.8.8:5555"
	d.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("public client: %d", rec.Code)
	}

	lg := map[string]string{"User-Agent": "Linux/3.10 UPnP/1.0 LGE WebOS TV LGE_DLNA_SDK/1.6.0 DLNADOC/1.50"}
	// Other tests leave their libraries behind: look for ours by name.
	root, _ := browse(t, tv.URL, "0", "BrowseDirectChildren", lg)
	libID := ""
	for _, c := range root.Containers {
		if c.Title == libName {
			libID = c.ID
		}
		if c.ID == "cw" {
			t.Fatalf("continue watching before anything was watched: %+v", root)
		}
	}
	if libID != "l:"+lib.ID {
		t.Fatalf("root: %+v", root)
	}
	subs, _ := browse(t, tv.URL, libID, "BrowseDirectChildren", lg)
	if len(subs.Containers) != 3 || subs.Containers[1].Title != "Nedavno dodano" {
		t.Fatalf("library folders (Croatian): %+v", subs)
	}
	az, total := browse(t, tv.URL, subs.Containers[0].ID, "BrowseDirectChildren", lg)
	if total != "1" || len(az.Items) != 1 || len(az.Items[0].Res) != 1 {
		t.Fatalf("A–Z: %s %+v", total, az)
	}
	movie := az.Items[0]
	if !strings.Contains(movie.Res[0].ProtocolInfo, "video/x-matroska:DLNA.ORG_OP=01") || !strings.Contains(movie.Res[0].URL, "/dlna/f/") {
		t.Fatalf("LG plays H.264 MKV directly: %+v", movie.Res[0])
	}
	if one, total := browse(t, tv.URL, movie.ID, "BrowseMetadata", lg); total != "1" || len(one.Items) != 1 || one.Items[0].Title != "Film" {
		t.Fatalf("metadata: %+v", one)
	}
	// A generic renderer gets an MPEG-TS stream instead.
	gen, _ := browse(t, tv.URL, subs.Containers[0].ID, "BrowseDirectChildren", map[string]string{"User-Agent": "SomeRenderer/1.0"})
	if !strings.Contains(gen.Items[0].Res[0].ProtocolInfo, "video/mpeg:DLNA.ORG_OP=10;DLNA.ORG_CI=1") {
		t.Fatalf("generic: %+v", gen.Items[0].Res[0])
	}

	// Stream URL: no login, byte ranges, DLNA headers.
	get := func(method, u string, hdr map[string]string) (*http.Response, string) {
		req, _ := http.NewRequest(method, u, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	r, body := get("GET", movie.Res[0].URL, map[string]string{"Range": "bytes=10-13", "getcontentFeatures.dlna.org": "1"})
	if r.StatusCode != 206 || body != "abcd" || r.Header.Get("transferMode.dlna.org") != "Streaming" ||
		!strings.Contains(r.Header.Get("contentFeatures.dlna.org"), "DLNA.ORG_OP=01") {
		t.Fatalf("range: %d %q %v", r.StatusCode, body, r.Header)
	}
	if r, _ := get("GET", strings.Replace(movie.Res[0].URL, "sig=", "sig=x", 1), nil); r.StatusCode != 403 {
		t.Fatalf("tampered signature: %d", r.StatusCode)
	}
	if r, _ := get("GET", movie.Res[0].URL, map[string]string{"TimeSeekRange.dlna.org": "npt=10-"}); r.StatusCode != 406 {
		t.Fatalf("time seek on a file: %d", r.StatusCode)
	}
	// The converted stream answers HEAD with its time-seek headers (no ffmpeg needed).
	r, _ = get("HEAD", gen.Items[0].Res[0].URL, map[string]string{"TimeSeekRange.dlna.org": "npt=100.0-"})
	if r.StatusCode != 200 || r.Header.Get("TimeSeekRange.dlna.org") != "npt=100.000-1000.000/1000.000" || r.Header.Get("Content-Type") != "video/mpeg" {
		t.Fatalf("HEAD transcode: %d %v", r.StatusCode, r.Header)
	}
	if r, _ := get("HEAD", gen.Items[0].Res[0].URL, map[string]string{"TimeSeekRange.dlna.org": "npt=5000-"}); r.StatusCode != 416 {
		t.Fatalf("seek past the end: %d", r.StatusCode)
	}

	// Samsung's bookmark becomes the user's resume position; "Continue watching" resumes there.
	samsung := map[string]string{"User-Agent": "DLNADOC/1.50 SEC_HHP_[TV] Samsung Q60 Series (55)/1.0 UPnP/1.0"}
	if code, b := soapCall(t, tv.URL, "X_SetBookmark", "<CategoryType>1</CategoryType><RID>0</RID><ObjectID>"+movie.ID+
		"</ObjectID><PosSecond>400</PosSecond>", samsung); code != 200 {
		t.Fatalf("bookmark: %d %s", code, b)
	}
	var cont []struct {
		PositionMs int64 `json:"positionMs"`
	}
	if adminC.do("GET", "/home/continue", nil, &cont); len(cont) != 1 || cont[0].PositionMs != 400_000 {
		t.Fatalf("continue watching after bookmark: %+v", cont)
	}
	root, _ = browse(t, tv.URL, "0", "BrowseDirectChildren", samsung)
	if n := len(root.Containers); n < 2 || root.Containers[n-1].Title != "Nastavi gledati" {
		t.Fatalf("root with continue watching: %+v", root)
	}
	cw, _ := browse(t, tv.URL, "cw", "BrowseDirectChildren", samsung)
	if len(cw.Items) != 1 || !strings.Contains(cw.Items[0].Title, "6:40") || !strings.Contains(cw.Items[0].Res[0].URL, "/dlna/t/") ||
		!strings.Contains(cw.Items[0].Res[0].URL, "s=400000") {
		t.Fatalf("resume entry: %+v", cw)
	}

	// Search by title.
	if code, b := soapCall(t, tv.URL, "Search", `<ContainerID>0</ContainerID><SearchCriteria>(upnp:class derivedfrom "object.item.videoItem") and dc:title contains "fil"</SearchCriteria>`+
		`<Filter>*</Filter><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount><SortCriteria></SortCriteria>`, lg); code != 200 ||
		!strings.Contains(b, "dc:title&gt;Film&lt;") {
		t.Fatalf("search: %d %s", code, b)
	}

	// Parental limit: a kids profile with a 12+ limit does not see an R-rated film.
	if _, err := testPool.Exec(ctx, `UPDATE items SET official_rating = 'R' WHERE kind = 'movie'`); err != nil {
		t.Fatal(err)
	}
	var prof struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/profiles", map[string]any{"name": "Mali", "color": "#3dd68c", "isKid": true, "maxRating": 12}, &prof)
	if s := adminC.do("PUT", "/settings/dlna", map[string]any{"enabled": true, "friendlyName": "", "language": "en",
		"userId": admin.User.ID, "profileId": prof.ID}, &ov); s != 200 {
		t.Fatalf("profile: HTTP %d", s)
	}
	d.Reload()
	if az, total := browse(t, tv.URL, subs.Containers[0].ID, "BrowseDirectChildren", lg); total != "0" || len(az.Items) != 0 {
		t.Fatalf("R-rated film visible to the kids profile: %+v", az)
	}
	if code, b := soapCall(t, tv.URL, "Browse", "<ObjectID>"+movie.ID+"</ObjectID><BrowseFlag>BrowseMetadata</BrowseFlag>"+
		"<Filter>*</Filter><StartingIndex>0</StartingIndex><RequestedCount>0</RequestedCount><SortCriteria></SortCriteria>", lg); code != 500 ||
		!strings.Contains(b, "<errorCode>701</errorCode>") {
		t.Fatalf("hidden item by id: %d %s", code, b)
	}

	// Turned off: silent again.
	adminC.do("PUT", "/settings/dlna", map[string]any{"enabled": false, "friendlyName": "", "language": "en"}, nil)
	d.Reload()
	if r, _ := get("GET", movie.Res[0].URL, nil); r.StatusCode != 404 {
		t.Fatalf("stream while off: %d", r.StatusCode)
	}
}
