package compat

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/stream"
)

func TestIDsRoundTrip(t *testing.T) {
	u := uuid.Must(uuid.NewV7())
	id := ID(u)
	if len(id) != 32 || strings.Contains(id, "-") || strings.ToLower(id) != id {
		t.Fatalf("ID format: %q", id)
	}
	if back, err := ParseID(id); err != nil || back != u {
		t.Fatalf("round trip: %v %v", back, err)
	}
	if back, err := ParseID(strings.ToUpper(u.String())); err != nil || back != u {
		t.Fatalf("dashed upper case: %v %v", back, err)
	}
	if tag, _ := syntheticKind(id); tag != "" {
		t.Fatalf("a UUIDv7 must never look synthetic: %s", id)
	}
	if tag, _ := syntheticKind(ID(uuid.New())); tag != "" {
		t.Fatal("a UUIDv4 must never look synthetic")
	}
}

func TestSyntheticIDs(t *testing.T) {
	p := PersonID(31)
	if len(p) != 32 {
		t.Fatalf("person id %q", p)
	}
	if n, ok := ParsePersonID(p); !ok || n != 31 {
		t.Fatalf("person round trip: %d %v", n, ok)
	}
	if n, ok := ParsePersonID(strings.ToUpper(p)); !ok || n != 31 {
		t.Fatal("person id is case-insensitive")
	}
	if _, ok := ParsePersonID(GenreID("Drama")); ok {
		t.Fatal("a genre id is not a person id")
	}
	if GenreID("Drama") != GenreID("drama") || GenreID("Drama") == GenreID("Comedy") {
		t.Fatal("genre ids are case-insensitive and distinct")
	}
	if tag, payload := syntheticKind(PersonNameID("Jane Doe")); tag != tagPersonName || payload != nameHash("jane doe") {
		t.Fatalf("person name id: %s %s", tag, payload)
	}
	// Synthetic IDs still parse as UUIDs (clients treat every ID as a GUID).
	for _, s := range []string{p, GenreID("x"), PersonNameID("y")} {
		if _, err := ParseID(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func TestClientInfo(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Authorization", `MediaBrowser Client="Android TV", Device="Shield", DeviceId="abc", Version="0.19.0", Token="tok"`)
	c := clientInfo(r)
	if c.token != "tok" || c.name != "Android TV" || c.device != "Shield" || c.deviceID != "abc" || c.version != "0.19.0" {
		t.Fatalf("parsed %+v", c)
	}
	r = httptest.NewRequest("GET", "/x?api_key=k1", nil)
	if c := clientInfo(r); c.token != "k1" {
		t.Fatalf("api_key: %+v", c)
	}
	r = httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("X-Emby-Authorization", `Emby UserId="u", Client="Kodi", Device="pi", DeviceId="d", Version="1"`)
	r.Header.Set("X-Emby-Token", "k2")
	if c := clientInfo(r); c.token != "k2" || c.name != "Kodi" {
		t.Fatalf("emby headers: %+v", c)
	}
}

func file(container, video, hdr string, audio ...string) library.MediaFile {
	h := 2160
	f := library.MediaFile{ID: uuid.New(), Container: container, VideoCodec: video, HDR: hdr, Height: &h}
	for _, a := range audio {
		f.Audio = append(f.Audio, library.AudioStream{Codec: a})
	}
	return f
}

func TestDeviceForProfiles(t *testing.T) {
	f := file("mkv", "hevc", "HDR10", "eac3", "aac")
	// No profile: the client plays the file as it is.
	if plan := stream.Decide(f, 0, deviceFor(nil, f, true)); plan.Method != "direct" {
		t.Fatalf("no profile: %+v", plan)
	}
	prof := &deviceProfile{}
	prof.DirectPlayProfiles = append(prof.DirectPlayProfiles, struct{ Container, AudioCodec, VideoCodec, Type string }{
		Container: "mp4,mkv", VideoCodec: "h264,hevc", AudioCodec: "aac,eac3", Type: "Video"})
	if plan := stream.Decide(f, 0, deviceFor(prof, f, true)); plan.Method != "direct" {
		t.Fatalf("matching profile: %+v", plan)
	}
	// HDR10 excluded by a VideoRangeType condition: tone-mapped transcode.
	// HDR10 excluded for HEVC (as the Android TV app sends it): tone-mapped transcode.
	hevc := "hevc"
	prof.CodecProfiles = append(prof.CodecProfiles, codecProfile{Type: "Video", Codec: &hevc,
		Conditions: []profileCondition{{Condition: "NotEquals", Property: "VideoRangeType", Value: "DOVI|DOVIWithHDR10|HDR10Plus|HDR10"}}})
	if plan := stream.Decide(f, 0, deviceFor(prof, f, true)); plan.Method != "transcode" || !plan.ToneMap {
		t.Fatalf("hdr not allowed: %+v", plan)
	}
	// Profile and level conditions for H.264 (values as ffprobe reports them).
	h264 := "h264"
	prof.CodecProfiles = append(prof.CodecProfiles,
		codecProfile{Type: "Video", Codec: &h264, Conditions: []profileCondition{
			{Condition: "EqualsAny", Property: "VideoProfile", Value: "high|main|baseline|constrained baseline"}}},
		codecProfile{Type: "Video", Codec: &h264, Conditions: []profileCondition{
			{Condition: "LessThanEqual", Property: "VideoLevel", Value: "52"}}})
	hi := file("mkv", "h264", "", "aac")
	hi.VideoProfile, hi.VideoLevel = "High", 41
	if plan := stream.Decide(hi, 0, deviceFor(prof, hi, true)); plan.Method != "direct" {
		t.Fatalf("high profile: %+v", plan)
	}
	hi444 := hi
	hi444.VideoProfile = "High 4:4:4 Predictive"
	if plan := stream.Decide(hi444, 0, deviceFor(prof, hi444, true)); plan.Method != "transcode" {
		t.Fatalf("4:4:4 profile: %+v", plan)
	}
	tooHigh := hi
	tooHigh.VideoLevel = 62
	if plan := stream.Decide(tooHigh, 0, deviceFor(prof, tooHigh, true)); plan.Method != "transcode" {
		t.Fatalf("level 6.2: %+v", plan)
	}
	// Unknown profile (scanned before it was recorded): direct play is tried.
	unknown := file("mkv", "h264", "", "aac")
	if plan := stream.Decide(unknown, 0, deviceFor(prof, unknown, true)); plan.Method != "direct" {
		t.Fatalf("unknown profile: %+v", plan)
	}
	// After a failed direct play (EnableDirectPlay=false): copy only a known-good video.
	if plan := stream.Decide(hi, 0, deviceFor(prof, hi, false)); plan.Method != "remux" {
		t.Fatalf("retry, known profile: %+v", plan)
	}
	if plan := stream.Decide(unknown, 0, deviceFor(prof, unknown, false)); plan.Method != "transcode" {
		t.Fatalf("retry, unknown profile: %+v", plan)
	}
	// Only a missing audio codec: remux with audio transcoding, video copied.
	g := file("mkv", "h264", "", "truehd")
	if plan := stream.Decide(g, 0, deviceFor(prof, g, true)); plan.Method != "remux" || !plan.TranscodeAudio || plan.TranscodeVideo {
		t.Fatalf("audio only: %+v", plan)
	}
	// Empty codec lists mean "any codec".
	anyCodec := &deviceProfile{}
	anyCodec.DirectPlayProfiles = append(anyCodec.DirectPlayProfiles, struct{ Container, AudioCodec, VideoCodec, Type string }{Container: "mkv"})
	if plan := stream.Decide(g, 0, deviceFor(anyCodec, g, true)); plan.Method != "direct" {
		t.Fatalf("any codec: %+v", plan)
	}
	// Direct play and direct stream disabled: always rebuilt as HLS.
	if plan := stream.Decide(g, 0, deviceFor(nil, g, false)); plan.Method == "direct" {
		t.Fatalf("forced transcode: %+v", plan)
	}
	// The bitrate limit applies.
	prof2 := &deviceProfile{MaxStreamingBitrate: 1_000_000}
	dur := int64(10_000)
	big := file("mkv", "h264", "", "aac")
	big.DurationMS, big.Size = &dur, 50_000_000
	if d := deviceFor(prof2, big, true); d.MaxBitrate != 1_000_000 || stream.Decide(big, 0, d).Method != "transcode" {
		t.Fatalf("bitrate: %+v", d)
	}
}

func TestMediaStreamIndexes(t *testing.T) {
	f := file("mkv", "h264", "Dolby Vision", "aac", "ac3")
	f.Subtitles = []library.SubtitleStream{{Codec: "subrip", Language: "hrv", External: true}, {Codec: "hdmv_pgs_subtitle"}}
	streams := mediaStreams(f, 1)
	if len(streams) != 5 {
		t.Fatalf("streams: %v", streams)
	}
	want := []struct {
		typ   string
		index int
	}{{"Video", 0}, {"Audio", 1}, {"Audio", 2}, {"Subtitle", 3}, {"Subtitle", 4}}
	for i, w := range want {
		m := streams[i].(map[string]any)
		if m["Type"] != w.typ || m["Index"] != w.index {
			t.Fatalf("stream %d: %v", i, m)
		}
	}
	if streams[0].(map[string]any)["VideoRangeType"] != "DOVI" || streams[2].(map[string]any)["IsDefault"] != true {
		t.Fatalf("flags: %v", streams)
	}
	if streams[3].(map[string]any)["DeliveryMethod"] != "External" || streams[4].(map[string]any)["IsTextSubtitleStream"] != false {
		t.Fatalf("subtitles: %v", streams[3:])
	}
	if subtitleBase(f) != 3 || audioBase(f) != 1 {
		t.Fatal("bases")
	}
	music := library.MediaFile{Audio: []library.AudioStream{{Codec: "flac"}}}
	if audioBase(music) != 0 || mediaStreams(music, -1)[0].(map[string]any)["Type"] != "Audio" {
		t.Fatal("music has no video stream")
	}
	// Every source carries the fields typed clients require.
	src := mediaSource(f)
	for _, k := range []string{"Protocol", "Type", "IsRemote", "ReadAtNativeFramerate", "IgnoreDts", "IgnoreIndex", "GenPtsInput",
		"SupportsTranscoding", "SupportsDirectStream", "SupportsDirectPlay", "IsInfiniteStream", "UseMostCompatibleTranscodingProfile",
		"RequiresOpening", "RequiresClosing", "RequiresLooping", "SupportsProbing", "TranscodingSubProtocol", "HasSegments"} {
		if _, ok := src[k]; !ok {
			t.Errorf("media source lacks %s", k)
		}
	}
}

func TestOrderBy(t *testing.T) {
	if got := orderBy("SortName", "Descending", "x"); got != "i.sort_title DESC NULLS LAST, i.sort_title, i.id" {
		t.Fatalf("got %q", got)
	}
	got := orderBy("PremiereDate,SortName", "Descending,Ascending", "x")
	if !strings.HasPrefix(got, "i.premiere_date DESC NULLS LAST, i.sort_title ASC NULLS LAST") {
		t.Fatalf("got %q", got)
	}
	// Unknown keys (and anything that looks like SQL) never reach the query.
	if got := orderBy("Name; DROP TABLE items", "Ascending", "fallback"); got != "fallback" {
		t.Fatalf("got %q", got)
	}
	if got := orderBy("Random", "", "x"); !strings.HasPrefix(got, "random()") {
		t.Fatalf("got %q", got)
	}
}

func TestKindsFor(t *testing.T) {
	if k := kindsFor("episode"); !slices.Equal(k, []string{"episode"}) {
		t.Fatalf("%v", k)
	}
	if k := kindsFor("BoxSet"); len(k) != 0 {
		t.Fatalf("%v", k)
	}
}
