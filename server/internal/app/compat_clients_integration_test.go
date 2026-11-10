package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/compat"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
)

// compatCaller sends requests the way media clients do (MediaBrowser authorization header).
type compatCaller struct {
	t     *testing.T
	base  string
	token string
}

func (c *compatCaller) call(method, path string, body any, out any) int {
	c.t.Helper()
	status, raw, _ := c.raw(method, path, body, nil)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return status
}

func (c *compatCaller) raw(method, path string, body any, hdr map[string]string) (int, []byte, http.Header) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	h := `MediaBrowser Client="Living Room TV", Device="Television", DeviceId="tv-0001", Version="1.2.3"`
	if c.token != "" {
		h += `, Token="` + c.token + `"`
	}
	req.Header.Set("Authorization", h)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, resp.Header
}

type compatItem struct {
	ID           string `json:"Id"`
	Name         string
	Type         string
	SeriesName   string
	SeriesID     string `json:"SeriesId"`
	IndexNumber  int
	ImageTags    map[string]string
	RunTimeTicks int64
	People       []struct {
		ID              string `json:"Id"`
		Name            string
		Type            string
		PrimaryImageTag string
	}
	UserData struct {
		PlaybackPositionTicks int64
		Played                bool
		IsFavorite            bool
		Key                   string
	}
	MediaSources []compatSource
}

type compatSource struct {
	ID                         string `json:"Id"`
	Container                  string
	ETag                       string
	SupportsDirectPlay         bool
	SupportsDirectStream       bool
	IsRemote                   bool
	TranscodingURL             string `json:"TranscodingUrl"`
	TranscodingSubProtocol     string
	DefaultAudioStreamIndex    *int
	DefaultSubtitleStreamIndex *int
	MediaStreams               []struct {
		Type           string
		Index          int
		Codec          string
		Language       string
		IsExternal     bool
		DeliveryMethod string
		DeliveryURL    string `json:"DeliveryUrl"`
		VideoRangeType string
	}
}

type compatList struct {
	Items            []compatItem
	TotalRecordCount int
	StartIndex       int
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TestCompatClientSessionIntegration walks a media client's session against the
// compat API: sign-in, libraries, browsing, artwork, playback info, streaming,
// subtitles, progress reports, user data, search, and Quick Connect (also for a
// two-factor account). With ffmpeg installed it also checks HLS and subtitle conversion.
func TestCompatClientSessionIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	ffmpeg, _ := exec.LookPath("ffmpeg")
	if ffmpeg != "" {
		testState.Stream().FFmpeg, testState.Stream().HLS.FFmpeg = ffmpeg, ffmpeg
	}
	imageDir := t.TempDir()
	testState.Library().ImageDir = imageDir

	root := mediaRoot + "/clients"
	movieFile := root + "/Clip (2024)/Clip (2024).mkv"
	for _, f := range []string{movieFile, root + "/Show (2020)/Season 01/Show S01E01.mkv", root + "/Show (2020)/Season 01/Show S01E02.mkv"} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("not-a-real-video-"+filepath.Base(f)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if ffmpeg != "" { // a real clip so HLS and subtitles can be produced
		out, err := exec.Command(ffmpeg, "-y", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=25",
			"-f", "lavfi", "-i", "sine=frequency=440", "-t", "3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
			movieFile).CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg: %v %s", err, out)
		}
	}
	srt := "1\n00:00:00,000 --> 00:00:02,000\nHello from Streamero\n"
	if err := os.WriteFile(root+"/Clip (2024)/Clip (2024).en.srt", []byte(srt), 0o644); err != nil {
		t.Fatal(err)
	}
	var lib struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "Clients", "kind": "mixed", "paths": []string{root}}, &lib)
	libID := uuid.MustParse(lib.ID)
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE id = $1`, libID) })
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv", VideoCodec: "h264", Width: 1920, Height: 1080, DurationMS: 600_000,
				Audio: []library.AudioStream{{Codec: "aac", Language: "eng", Channels: 2}, {Codec: "ac3", Language: "hrv", Channels: 6}}}, nil
		}}
	if _, err := scanner.Scan(context.Background(), libID); err != nil {
		t.Fatal(err)
	}
	adminC.do("POST", "/users", map[string]any{"username": "tv", "password": "tv-pass-123"}, nil)

	c := &compatCaller{t: t, base: srv.URL + "/compat"}

	// Discovery.
	var pub map[string]any
	if s := c.call("GET", "/System/Info/Public", nil, &pub); s != 200 || pub["ProductName"] != compat.ProductName ||
		!hex32.MatchString(pub["Id"].(string)) || pub["ServerName"] != "Kino" {
		t.Fatalf("public info: %d %v", s, pub)
	}
	var public []any
	if s := c.call("GET", "/Users/Public", nil, &public); s != 200 || len(public) != 0 {
		t.Fatalf("public users must stay empty: %d %v", s, public)
	}

	// Sign in by name.
	var login struct {
		AccessToken string
		User        struct {
			ID     string `json:"Id"`
			Policy map[string]any
		}
		SessionInfo map[string]any
	}
	if s := c.call("POST", "/Users/AuthenticateByName", map[string]any{"Username": "TV", "Pw": "tv-pass-123"}, &login); s != 200 ||
		login.AccessToken == "" || !hex32.MatchString(login.User.ID) || login.User.Policy["AuthenticationProviderId"] == nil ||
		login.SessionInfo["DeviceId"] != "tv-0001" {
		t.Fatalf("login: %d %+v", s, login)
	}
	c.token = login.AccessToken
	uid := login.User.ID

	// Libraries.
	var views compatList
	if s := c.call("GET", "/UserViews?userId="+uid, nil, &views); s != 200 || len(views.Items) == 0 {
		t.Fatalf("views: %d %+v", s, views)
	}
	if views.Items[0].ID != compat.ID(libID) {
		t.Fatalf("view id %q, want %q", views.Items[0].ID, compat.ID(libID))
	}
	if back, err := compat.ParseID(views.Items[0].ID); err != nil || back != libID {
		t.Fatalf("id round trip: %v %v", back, err)
	}

	// Paging and sorting.
	var page1, page2 compatList
	c.call("GET", "/Items?ParentId="+compat.ID(libID)+"&Recursive=true&IncludeItemTypes=Movie,Series&SortBy=SortName&SortOrder=Ascending&StartIndex=0&Limit=1&Fields=PrimaryImageAspectRatio,Overview", nil, &page1)
	c.call("GET", "/Users/"+uid+"/Items?ParentId="+compat.ID(libID)+"&Recursive=true&IncludeItemTypes=Movie,Series&SortBy=SortName&SortOrder=Ascending&StartIndex=1&Limit=1", nil, &page2)
	if page1.TotalRecordCount != 2 || len(page1.Items) != 1 || page1.Items[0].Name != "Clip" || page2.Items[0].Name != "Show" || page2.StartIndex != 1 {
		t.Fatalf("paging: %+v / %+v", page1, page2)
	}
	var desc compatList
	c.call("GET", "/Items?ParentId="+compat.ID(libID)+"&Recursive=true&IncludeItemTypes=Movie,Series&SortBy=SortName&SortOrder=Descending", nil, &desc)
	if len(desc.Items) != 2 || desc.Items[0].Name != "Show" {
		t.Fatalf("descending: %+v", desc)
	}
	movie, series := page1.Items[0], page2.Items[0]
	movieID := uuid.MustParse(movie.ID)

	// Series → seasons → episodes.
	var seasons, episodes compatList
	c.call("GET", "/Shows/"+series.ID+"/Seasons?userId="+uid, nil, &seasons)
	if len(seasons.Items) != 1 || seasons.Items[0].IndexNumber != 1 || seasons.Items[0].SeriesID != series.ID {
		t.Fatalf("seasons: %+v", seasons)
	}
	c.call("GET", "/Shows/"+series.ID+"/Episodes?SeasonId="+seasons.Items[0].ID+"&Fields=Overview", nil, &episodes)
	if len(episodes.Items) != 2 || episodes.Items[1].IndexNumber != 2 || episodes.Items[0].SeriesName != "Show" {
		t.Fatalf("episodes: %+v", episodes)
	}
	ep1, ep2 := episodes.Items[0], episodes.Items[1]

	// Artwork and people.
	if err := os.MkdirAll(filepath.Dir(metadata.ImagePath(imageDir, movieID, "poster")), 0o755); err != nil {
		t.Fatal(err)
	}
	jpeg := []byte("\xff\xd8\xff\xe0fake-jpeg")
	_ = os.WriteFile(metadata.ImagePath(imageDir, movieID, "poster"), jpeg, 0o644)
	_ = os.MkdirAll(filepath.Dir(metadata.PersonImagePath(imageDir, 31)), 0o755)
	_ = os.WriteFile(metadata.PersonImagePath(imageDir, 31), jpeg, 0o644)
	if _, err := testPool.Exec(context.Background(), `UPDATE items SET has_poster = true, genres = '{Drama}' WHERE id = $1;`, movieID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `INSERT INTO item_people (item_id, position, kind, name, role, tmdb_person_id, has_image)
		VALUES ($1, 0, 'cast', 'Tom Hanks', 'Woody', 31, true), ($1, 1, 'crew', 'Jane Doe', 'Director', NULL, false)`, movieID); err != nil {
		t.Fatal(err)
	}
	var detail compatItem
	if s := c.call("GET", "/Users/"+uid+"/Items/"+movie.ID, nil, &detail); s != 200 || detail.ImageTags["Primary"] == "" ||
		len(detail.People) != 2 || detail.People[0].ID != compat.PersonID(31) || detail.People[0].PrimaryImageTag == "" ||
		detail.People[1].Type != "Director" || !hex32.MatchString(detail.People[1].ID) {
		t.Fatalf("detail: %d %+v", s, detail)
	}
	anon := &compatCaller{t: t, base: c.base}
	if s, body, h := anon.raw("GET", "/Items/"+movie.ID+"/Images/Primary?tag="+detail.ImageTags["Primary"]+"&maxWidth=300", nil, nil); s != 200 ||
		!bytes.Equal(body, jpeg) || h.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("poster: %d %q", s, h.Get("Content-Type"))
	}
	if s, _, _ := anon.raw("GET", "/Items/"+compat.PersonID(31)+"/Images/Primary", nil, nil); s != 200 {
		t.Fatalf("person image: %d", s)
	}
	if s, _, _ := anon.raw("GET", "/Items/"+movie.ID+"/Images/Backdrop/0", nil, nil); s != 404 {
		t.Fatalf("missing backdrop: %d", s)
	}
	var person map[string]any
	if s := c.call("GET", "/Items/"+compat.PersonID(31), nil, &person); s != 200 || person["Name"] != "Tom Hanks" || person["Type"] != "Person" {
		t.Fatalf("person item: %d %v", s, person)
	}
	var byPerson, byGenre compatList
	c.call("GET", "/Items?ParentId="+compat.ID(libID)+"&Recursive=true&PersonIds="+compat.PersonID(31), nil, &byPerson)
	c.call("GET", "/Items?ParentId="+compat.ID(libID)+"&Recursive=true&GenreIds="+compat.GenreID("drama"), nil, &byGenre)
	if byPerson.TotalRecordCount != 1 || byGenre.TotalRecordCount != 1 || byGenre.Items[0].ID != movie.ID {
		t.Fatalf("person/genre filters: %+v %+v", byPerson, byGenre)
	}

	// Playback info: a device that plays mkv/h264/aac gets direct play.
	profile := map[string]any{"MaxStreamingBitrate": 120_000_000, "DirectPlayProfiles": []any{
		map[string]any{"Type": "Video", "Container": "mkv,mp4", "VideoCodec": "h264,hevc", "AudioCodec": "aac,ac3"}}}
	var pb struct {
		MediaSources  []compatSource
		PlaySessionID string `json:"PlaySessionId"`
		ErrorCode     string
	}
	if s := c.call("POST", "/Items/"+movie.ID+"/PlaybackInfo?UserId="+uid+"&StartTimeTicks=0&AutoOpenLiveStream=true",
		map[string]any{"DeviceProfile": profile}, &pb); s != 200 || len(pb.MediaSources) != 1 || pb.PlaySessionID == "" {
		t.Fatalf("playback info: %d %+v", s, pb)
	}
	src := pb.MediaSources[0]
	if !src.SupportsDirectPlay || src.TranscodingURL != "" || src.IsRemote || src.DefaultAudioStreamIndex == nil || *src.DefaultAudioStreamIndex != 1 {
		t.Fatalf("direct source: %+v", src)
	}
	var subURL string
	subIndex := -1
	for _, ms := range src.MediaStreams {
		if ms.Type == "Subtitle" {
			subURL, subIndex = ms.DeliveryURL, ms.Index
			if !ms.IsExternal || ms.DeliveryMethod != "External" || !strings.HasPrefix(ms.DeliveryURL, "/stream/sub/") {
				t.Fatalf("subtitle stream: %+v", ms)
			}
		}
	}
	if subIndex != 3 { // video 0, audio 1-2, subtitle 3
		t.Fatalf("subtitle index %d in %+v", subIndex, src.MediaStreams)
	}

	// Direct play bytes need the token (as api_key), and honour ranges.
	want, _ := os.ReadFile(movieFile)
	if s, _, _ := anon.raw("GET", "/Videos/"+movie.ID+"/stream?static=true&mediaSourceId="+src.ID, nil, nil); s != 401 {
		t.Fatalf("anonymous stream: %d", s)
	}
	s, body, _ := anon.raw("GET", "/Videos/"+movie.ID+"/stream.mkv?Static=true&MediaSourceId="+src.ID+"&api_key="+c.token, nil,
		map[string]string{"Range": "bytes=0-3"})
	if s != 206 || !bytes.Equal(body, want[:4]) {
		t.Fatalf("direct stream: %d %q", s, body)
	}
	if s, _, _ := c.raw("GET", "/Videos/"+movie.ID+"/stream?static=true&mediaSourceId="+uuid.NewString(), nil, nil); s != 404 {
		t.Fatalf("foreign media source: %d", s)
	}
	// Players that send no credentials (observed: the Android TV app) pass the
	// source's signed tag instead; the dashed item id is what that app sends.
	s, body, _ = anon.raw("GET", "/Videos/"+movieID.String()+"/stream?static=true&container=mkv&mediaSourceId="+src.ID+"&tag="+src.ETag,
		nil, map[string]string{"Range": "bytes=2-5"})
	if s != 206 || !bytes.Equal(body, want[2:6]) {
		t.Fatalf("tagged stream: %d %q", s, body)
	}
	if s, _, _ := anon.raw("GET", "/Videos/"+movie.ID+"/stream?static=true&mediaSourceId="+src.ID+"&tag="+src.ETag+"x", nil, nil); s != 401 {
		t.Fatalf("tampered tag: %d", s)
	}
	if s, _, _ := anon.raw("GET", "/Videos/"+movie.ID+"/master.m3u8?mediaSourceId="+src.ID+"&tag="+src.ETag, nil, nil); s != 401 {
		t.Fatalf("a tag must not start a transcode: %d", s)
	}

	// Subtitles: the signed URL from PlaybackInfo and the client-built route.
	if ffmpeg != "" {
		s, body, _ = anon.raw("GET", subURL, nil, nil)
		if s != 200 || !strings.Contains(string(body), "WEBVTT") || !strings.Contains(string(body), "Hello from Streamero") {
			t.Fatalf("signed subtitle: %d %s", s, body)
		}
		s, body, _ = c.raw("GET", "/Videos/"+movie.ID+"/"+src.ID+"/Subtitles/3/0/Stream.srt", nil, nil)
		if s != 200 || !strings.Contains(string(body), "Hello from Streamero") {
			t.Fatalf("subtitle route: %d %s", s, body)
		}
	}
	if s, _, _ := anon.raw("GET", strings.Replace(subURL, "sig=", "sig=x", 1), nil, nil); s != 403 {
		t.Fatalf("tampered subtitle link: %d", s)
	}

	// A device without h264 support gets the HLS transcode (or an error code without ffmpeg).
	pb.MediaSources, pb.ErrorCode = nil, ""
	c.call("POST", "/Items/"+movie.ID+"/PlaybackInfo", map[string]any{"DeviceProfile": map[string]any{
		"DirectPlayProfiles": []any{map[string]any{"Type": "Video", "Container": "mp4", "VideoCodec": "vp9", "AudioCodec": "opus"}}}}, &pb)
	if ffmpeg != "" {
		if len(pb.MediaSources) != 1 || pb.MediaSources[0].SupportsDirectPlay || pb.MediaSources[0].TranscodingSubProtocol != "hls" ||
			!strings.HasPrefix(pb.MediaSources[0].TranscodingURL, "/stream/hls/") {
			t.Fatalf("transcode source: %+v", pb)
		}
		s, body, _ := anon.raw("GET", pb.MediaSources[0].TranscodingURL, nil, nil)
		if s != 200 || !strings.Contains(string(body), "#EXTM3U") {
			t.Fatalf("hls playlist: %d %s", s, body)
		}
	} else if pb.ErrorCode != "NoCompatibleStream" {
		t.Fatalf("transcode without ffmpeg: %+v", pb)
	}
	// After a failed direct play the Android TV app asks again with EnableDirectPlay=false
	// (observed); it then needs a stream URL, never direct play.
	pb.MediaSources, pb.ErrorCode = nil, ""
	c.call("POST", "/Items/"+movie.ID+"/PlaybackInfo", map[string]any{"DeviceProfile": profile, "MediaSourceId": src.ID,
		"EnableDirectPlay": false, "EnableDirectStream": true}, &pb)
	if ffmpeg != "" && (len(pb.MediaSources) != 1 || pb.MediaSources[0].SupportsDirectPlay || pb.MediaSources[0].TranscodingURL == "") {
		t.Fatalf("retry without direct play: %+v", pb)
	}
	if ffmpeg == "" && pb.ErrorCode == "" {
		t.Fatalf("retry without ffmpeg: %+v", pb)
	}

	// Progress reports go into Streamero's resume state and the dashboard.
	if s := c.call("POST", "/Sessions/Playing", map[string]any{"ItemId": movie.ID, "MediaSourceId": src.ID,
		"PlaySessionId": pb.PlaySessionID, "PositionTicks": 0, "PlayMethod": "DirectPlay"}, nil); s != 204 {
		t.Fatalf("playing: %d", s)
	}
	nowPlaying := false
	for _, a := range testNowPlaying.Active() {
		if a.ItemID == movieID && a.Device == "Television" && a.Client == "Living Room TV" {
			nowPlaying = true
		}
	}
	if !nowPlaying {
		t.Fatalf("now playing: %+v", testNowPlaying.Active())
	}
	c.call("POST", "/Sessions/Playing/Progress", map[string]any{"ItemId": movie.ID, "MediaSourceId": src.ID, "PositionTicks": 1_200_000_000}, nil)
	if s := c.call("POST", "/Sessions/Playing/Stopped", map[string]any{"ItemId": movie.ID, "MediaSourceId": src.ID,
		"PositionTicks": 1_800_000_000}, nil); s != 204 {
		t.Fatalf("stopped: %d", s)
	}
	var resume compatList
	c.call("GET", "/UserItems/Resume?userId="+uid+"&MediaTypes=Video&Limit=12", nil, &resume)
	if len(resume.Items) != 1 || resume.Items[0].ID != movie.ID || resume.Items[0].UserData.PlaybackPositionTicks != 1_800_000_000 {
		t.Fatalf("resume: %+v", resume)
	}
	var legacyResume compatList
	c.call("GET", "/Users/"+uid+"/Items/Resume", nil, &legacyResume)
	if len(legacyResume.Items) != 1 {
		t.Fatalf("legacy resume: %+v", legacyResume)
	}
	// Legacy progress route (older clients).
	if s := c.call("POST", "/Users/"+uid+"/PlayingItems/"+ep1.ID+"/Progress?PositionTicks=600000000", nil, nil); s != 204 {
		t.Fatalf("legacy progress: %d", s)
	}

	// User data writes and reads.
	var ud map[string]any
	if s := c.call("POST", "/UserFavoriteItems/"+movie.ID, nil, &ud); s != 200 || ud["IsFavorite"] != true || ud["Key"] == nil {
		t.Fatalf("favorite: %d %v", s, ud)
	}
	var favs compatList
	c.call("GET", "/Items?ParentId="+compat.ID(libID)+"&Recursive=true&Filters=IsFavorite", nil, &favs)
	if favs.TotalRecordCount != 1 || favs.Items[0].ID != movie.ID {
		t.Fatalf("favorites: %+v", favs)
	}
	if s := c.call("DELETE", "/Users/"+uid+"/FavoriteItems/"+movie.ID, nil, &ud); s != 200 || ud["IsFavorite"] != false {
		t.Fatalf("unfavorite: %d %v", s, ud)
	}
	if s := c.call("POST", "/UserPlayedItems/"+ep1.ID, nil, &ud); s != 200 || ud["Played"] != true {
		t.Fatalf("played: %d %v", s, ud)
	}
	var next compatList
	c.call("GET", "/Shows/NextUp?userId="+uid+"&Limit=5", nil, &next)
	if len(next.Items) != 1 || next.Items[0].ID != ep2.ID {
		t.Fatalf("next up: %+v", next)
	}
	c.call("GET", "/Shows/NextUp?SeriesId="+series.ID, nil, &next)
	if len(next.Items) != 1 || next.Items[0].ID != ep2.ID {
		t.Fatalf("next up for series: %+v", next)
	}
	if s := c.call("DELETE", "/Users/"+uid+"/PlayedItems/"+ep1.ID, nil, &ud); s != 200 || ud["Played"] != false {
		t.Fatalf("unplayed: %d %v", s, ud)
	}
	if s := c.call("POST", "/UserItems/"+ep2.ID+"/UserData", map[string]any{"PlaybackPositionTicks": 50_000_000}, &ud); s != 200 ||
		ud["PlaybackPositionTicks"] != float64(50_000_000) {
		t.Fatalf("user data post: %d %v", s, ud)
	}
	var seriesNow compatItem
	c.call("GET", "/Items/"+series.ID, nil, &seriesNow)
	if seriesNow.Type != "Series" || seriesNow.UserData.Played {
		t.Fatalf("series: %+v", seriesNow)
	}

	// Search and latest.
	var hints struct {
		SearchHints      []map[string]any
		TotalRecordCount int
	}
	c.call("GET", "/Search/Hints?searchTerm=clip&Limit=10", nil, &hints)
	if hints.TotalRecordCount == 0 || hints.SearchHints[0]["Id"] != movie.ID || hints.SearchHints[0]["Type"] != "Movie" {
		t.Fatalf("hints: %+v", hints)
	}
	var found compatList
	c.call("GET", "/Items?ParentId="+compat.ID(libID)+"&searchTerm=show&Recursive=true&IncludeItemTypes=Series", nil, &found)
	if found.TotalRecordCount != 1 || found.Items[0].ID != series.ID {
		t.Fatalf("search items: %+v", found)
	}
	var latest []compatItem
	c.call("GET", "/Items/Latest?ParentId="+compat.ID(libID)+"&Limit=16", nil, &latest)
	if len(latest) != 2 {
		t.Fatalf("latest: %+v", latest)
	}

	// Quick Connect for a two-factor account: name sign-in is refused with a pointer
	// to Quick Connect; the user approves the TV's code from the web session.
	adminC.do("POST", "/users", map[string]any{"username": "mia2", "password": "mia-pass-123"}, nil)
	mia := newClient(t, srv)
	var miaLogin authResult
	mia.do("POST", "/auth/login", map[string]any{"username": "mia2", "password": "mia-pass-123", "device": map[string]any{"name": "web"}}, &miaLogin)
	mia.token = miaLogin.Token
	var setup struct {
		Secret string `json:"secret"`
	}
	mia.do("POST", "/auth/totp/setup", nil, &setup)
	if s := mia.do("POST", "/auth/totp/enable", map[string]any{"code": totpNow(t, setup.Secret, 0)}, nil); s != 200 {
		t.Fatalf("enable 2fa: %d", s)
	}
	tv := &compatCaller{t: t, base: c.base}
	s, body, _ = tv.raw("POST", "/Users/AuthenticateByName", map[string]any{"Username": "mia2", "Pw": "mia-pass-123"}, nil)
	if s != 401 || !strings.Contains(string(body), "Quick Connect") {
		t.Fatalf("2fa name sign-in: %d %s", s, body)
	}
	var enabled bool
	if tv.call("GET", "/QuickConnect/Enabled", nil, &enabled); !enabled {
		t.Fatal("quick connect disabled")
	}
	var qc struct {
		Secret, Code, DeviceID, DeviceName, AppName string
		Authenticated                               bool
	}
	if s := tv.call("POST", "/QuickConnect/Initiate", nil, &qc); s != 200 || len(qc.Code) != 6 || qc.Secret == "" ||
		qc.DeviceName != "Television" || qc.AppName != "Living Room TV" {
		t.Fatalf("initiate: %d %+v", s, qc)
	}
	secret := qc.Secret
	if tv.call("GET", "/QuickConnect/Connect?Secret="+secret, nil, &qc); qc.Authenticated {
		t.Fatal("approved before approval")
	}
	if s := tv.call("POST", "/Users/AuthenticateWithQuickConnect", map[string]any{"Secret": secret}, nil); s != 401 {
		t.Fatalf("pending quick connect: %d", s)
	}
	if s := mia.do("POST", "/quickconnect/authorize", map[string]any{"code": qc.Code}, nil); s != 200 {
		t.Fatalf("approve from web: %d", s)
	}
	if tv.call("GET", "/QuickConnect/Connect?Secret="+secret, nil, &qc); !qc.Authenticated {
		t.Fatal("not approved after approval")
	}
	var qcLogin struct {
		AccessToken string
		User        struct{ Name string }
	}
	if s := tv.call("POST", "/Users/AuthenticateWithQuickConnect", map[string]any{"Secret": secret}, &qcLogin); s != 200 ||
		qcLogin.AccessToken == "" || qcLogin.User.Name != "mia2" {
		t.Fatalf("quick connect sign-in: %d %+v", s, qcLogin)
	}
	tv.token = qcLogin.AccessToken
	var me map[string]any
	if s := tv.call("GET", "/Users/Me", nil, &me); s != 200 || me["Name"] != "mia2" {
		t.Fatalf("me after quick connect: %d %v", s, me)
	}
	if s := (&compatCaller{t: t, base: c.base}).call("POST", "/Users/AuthenticateWithQuickConnect", map[string]any{"Secret": secret}, nil); s != 401 {
		t.Fatalf("secret reused: %d", s)
	}
	// A signed-in client can approve another device's code too.
	other := &compatCaller{t: t, base: c.base}
	other.call("POST", "/QuickConnect/Initiate", nil, &qc)
	var ok bool
	if s := c.call("POST", "/QuickConnect/Authorize?Code="+qc.Code+"&UserId="+uid, nil, &ok); s != 200 || !ok {
		t.Fatalf("authorize from client: %d", s)
	}
	if s := c.call("POST", "/QuickConnect/Authorize?Code=000000", nil, nil); s != 404 {
		t.Fatalf("unknown code: %d", s)
	}
}
