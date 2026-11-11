package dlna

import (
	"context"
	"encoding/xml"
	"image"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/streamero-app/streamero/server/internal/library"
)

const udn = "uuid:11111111-2222-5333-8444-555555555555"

func testSSDP() *SSDP {
	return &SSDP{UDN: udn, Server: "Linux/1.0 UPnP/1.0 Streamero/test", Active: func() bool { return true },
		Location: func(ip netip.Addr) string { return "http://" + ip.String() + ":8788/dlna/description.xml" }}
}

func msearch(st, mx string) []byte {
	m := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nST: " + st + "\r\n"
	if mx != "" {
		m += "MX: " + mx + "\r\n"
	}
	return []byte(m + "\r\n")
}

func TestParseSearch(t *testing.T) {
	s, ok := ParseSearch(msearch("ssdp:all", "10"))
	if !ok || s.ST != "ssdp:all" || s.MX != maxMX {
		t.Fatalf("ssdp:all: %+v %v", s, ok)
	}
	if s, ok := ParseSearch(msearch("upnp:rootdevice", "")); !ok || s.MX != 0 {
		t.Fatalf("unicast search without MX: %+v %v", s, ok)
	}
	for name, msg := range map[string]string{
		"notify":      "NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nNT: upnp:rootdevice\r\nNTS: ssdp:alive\r\n\r\n",
		"missing MAN": "M-SEARCH * HTTP/1.1\r\nST: ssdp:all\r\n\r\n",
		"missing ST":  "M-SEARCH * HTTP/1.1\r\nMAN: \"ssdp:discover\"\r\n\r\n",
		"garbage":     "\x00\x01hello",
	} {
		if _, ok := ParseSearch([]byte(msg)); ok {
			t.Errorf("%s parsed as a search", name)
		}
	}
}

func TestSSDPResponses(t *testing.T) {
	s := testSSDP()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	all := s.Responses(Search{ST: "ssdp:all"}, "http://192.168.1.5:8788/dlna/description.xml", now)
	if len(all) != 5 {
		t.Fatalf("ssdp:all: %d replies", len(all))
	}
	joined := string(all[0])
	for _, want := range []string{"HTTP/1.1 200 OK\r\n", "CACHE-CONTROL: max-age=1800\r\n", "EXT:\r\n",
		"LOCATION: http://192.168.1.5:8788/dlna/description.xml\r\n", "ST: upnp:rootdevice\r\n",
		"USN: " + udn + "::upnp:rootdevice\r\n", "DATE: Wed, 11 Nov 2026 12:00:00 GMT\r\n"} {
		if !strings.Contains(joined, want) {
			t.Errorf("reply lacks %q:\n%s", want, joined)
		}
	}
	one := s.Responses(Search{ST: "urn:schemas-upnp-org:device:MediaServer:1"}, "http://x/", now)
	if len(one) != 1 || !strings.Contains(string(one[0]), "USN: "+udn+"::urn:schemas-upnp-org:device:MediaServer:1\r\n") {
		t.Fatalf("device type search: %q", one)
	}
	if r := s.Responses(Search{ST: udn}, "http://x/", now); len(r) != 1 || !strings.Contains(string(r[0]), "USN: "+udn+"\r\n") {
		t.Fatalf("uuid search: %q", r)
	}
	for _, st := range []string{"urn:schemas-upnp-org:device:MediaRenderer:1", "urn:schemas-upnp-org:service:AVTransport:1", "uuid:other"} {
		if r := s.Responses(Search{ST: st}, "http://x/", now); len(r) != 0 {
			t.Errorf("%s answered: %q", st, r)
		}
	}
	// A notify carries the same identifiers.
	n := string(s.notify(typeCDS, "ssdp:alive", "http://l/"))
	if !strings.HasPrefix(n, "NOTIFY * HTTP/1.1\r\n") || !strings.Contains(n, "NTS: ssdp:alive") || !strings.Contains(n, "USN: "+udn+"::"+typeCDS) {
		t.Fatalf("notify: %q", n)
	}
	if b := string(s.notify(typeCDS, "ssdp:byebye", "")); strings.Contains(b, "LOCATION") {
		t.Fatalf("byebye carries a location: %q", b)
	}
}

func TestSSDPLimiter(t *testing.T) {
	var l limiter
	now := time.Unix(1000, 0)
	a, b := netip.MustParseAddr("192.168.1.10"), netip.MustParseAddr("192.168.1.11")
	n := 0
	for range 20 {
		if l.allow(a, now) {
			n++
		}
	}
	if n != int(burst) {
		t.Fatalf("burst: %d allowed", n)
	}
	if !l.allow(b, now) {
		t.Fatal("another sender is limited by the first one")
	}
	if !l.allow(a, now.Add(600*time.Millisecond)) {
		t.Fatal("bucket did not refill")
	}
	// Overall cap per second.
	var g limiter
	allowed := 0
	for i := range 200 {
		ip := netip.AddrFrom4([4]byte{10, 0, byte(i / 250), byte(i%250 + 1)})
		if g.allow(ip, now) {
			allowed++
		}
	}
	if allowed != maxOverallS {
		t.Fatalf("overall cap: %d", allowed)
	}
}

func TestIsLocal(t *testing.T) {
	for _, ip := range []string{"192.168.1.2", "10.1.2.3", "172.16.0.1", "127.0.0.1", "169.254.1.1", "::1", "fe80::1", "fd00::1"} {
		if !isLocal(netip.MustParseAddr(ip)) {
			t.Errorf("%s not local", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "100.64.0.1", "2001:4860::8888"} {
		if isLocal(netip.MustParseAddr(ip)) {
			t.Errorf("%s local", ip)
		}
	}
}

// TestSSDPServeUnicast runs the responder on loopback (no multicast group).
func TestSSDPServeUnicast(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := testSSDP()
	var off atomic.Bool
	s.Active = func() bool { return !off.Load() }
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, pc, false) }()

	c, err := net.DialUDP("udp4", nil, pc.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Write(msearch("urn:schemas-upnp-org:service:ContentDirectory:1", "")); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 2048)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	reply := string(buf[:n])
	if !strings.Contains(reply, "ST: urn:schemas-upnp-org:service:ContentDirectory:1") ||
		!strings.Contains(reply, "LOCATION: http://127.0.0.1:8788/dlna/description.xml") {
		t.Fatalf("reply: %q", reply)
	}
	// Turned off: silence.
	off.Store(true)
	_, _ = c.Write(msearch("ssdp:all", ""))
	_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := c.Read(buf); err == nil {
		t.Fatal("answered while inactive")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDetectProfile(t *testing.T) {
	cases := []struct {
		hdr  map[string]string
		want string
	}{
		{map[string]string{"User-Agent": "Linux/3.10.19-32.afro.4 UPnP/1.0 LGE WebOS TV LGE_DLNA_SDK/1.6.0 DLNADOC/1.50"}, "lg"},
		{map[string]string{"User-Agent": "DLNADOC/1.50 SEC_HHP_[TV] Samsung Q60 Series (55)/1.0 UPnP/1.0"}, "samsung"},
		{map[string]string{"User-Agent": "UPnP/1.0", "X-AV-Client-Info": `av=5.0; cn="Samsung Electronics"; mn="UE55"; mv="1.0"`}, "samsung"},
		{map[string]string{"X-AV-Client-Info": `av=5.0; cn="Sony Corporation"; mn="BRAVIA KD-55X85J"; mv="3.0"`}, "sony"},
		{map[string]string{"User-Agent": "VLC/3.0.20 LibVLC/3.0.20"}, "generic"},
		{map[string]string{}, "generic"},
	}
	for _, c := range cases {
		h := http.Header{}
		for k, v := range c.hdr {
			h.Set(k, v)
		}
		if got := Detect(h).Name; got != c.want {
			t.Errorf("%v: got %s, want %s", c.hdr, got, c.want)
		}
	}
	if ProfileByName("nope").Name != "generic" || ProfileByName("lg").Name != "lg" {
		t.Fatal("ProfileByName")
	}
	if SamsungTizen.MIMEType("mkv", false) != "video/x-mkv" || LGWebOS.MIMEType("mkv", false) != "video/x-matroska" ||
		Generic.MIMEType("mp4", true) != "audio/mp4" || Generic.MIMEType("mpegts", false) != "video/mpeg" {
		t.Fatal("MIME types")
	}
}

func ptr[T any](v T) *T { return &v }

func TestPlanFor(t *testing.T) {
	h264mkv := library.MediaFile{Container: "mkv", VideoCodec: "h264", Height: ptr(1080), DurationMS: ptr(int64(60_000)), Size: 1 << 20,
		Audio: []library.AudioStream{{Codec: "aac", Channels: 2}}}
	if p := PlanFor(h264mkv, 0, LGWebOS, 0); p.Method != "direct" {
		t.Fatalf("LG plays H.264 MKV directly: %+v", p)
	}
	if p := PlanFor(h264mkv, 0, Generic, 0); p.Method != "remux" || p.TranscodeVideo || p.TranscodeAudio {
		t.Fatalf("generic: remux to TS: %+v", p)
	}
	if p := PlanFor(h264mkv, 0, LGWebOS, 30_000); p.Method != "remux" {
		t.Fatalf("a start position needs a stream: %+v", p)
	}
	dts := h264mkv
	dts.Audio = []library.AudioStream{{Codec: "dts", Channels: 6}}
	if p := PlanFor(dts, 0, SamsungTizen, 0); !p.TranscodeAudio || p.TranscodeVideo {
		t.Fatalf("DTS on Samsung: audio only: %+v", p)
	}
	uhd := library.MediaFile{Container: "mkv", VideoCodec: "hevc", Height: ptr(2160), HDR: "hdr10",
		Audio: []library.AudioStream{{Codec: "eac3"}}}
	if p := PlanFor(uhd, 0, SonyBravia, 0); !p.TranscodeVideo || !p.ToneMap {
		t.Fatalf("HDR on the Sony profile is tone mapped: %+v", p)
	}
	if p := PlanFor(uhd, 0, Generic, 0); !p.TranscodeVideo || p.Height != 1080 || !p.TranscodeAudio {
		t.Fatalf("generic: 1080p H.264/AAC: %+v", p)
	}
	vp9 := library.MediaFile{Container: "webm", VideoCodec: "vp9", Audio: []library.AudioStream{{Codec: "opus"}}}
	if p := PlanFor(vp9, 0, SonyBravia, 0); !p.TranscodeVideo || !p.TranscodeAudio {
		t.Fatalf("VP9/Opus cannot go into TS as-is: %+v", p)
	}
	flac := library.MediaFile{Container: "flac", Audio: []library.AudioStream{{Codec: "flac"}}}
	if p := PlanFor(flac, 0, Generic, 0); p.Method != "direct" {
		t.Fatalf("FLAC direct: %+v", p)
	}
	ogg := library.MediaFile{Container: "ogg", Audio: []library.AudioStream{{Codec: "vorbis"}}}
	if p := PlanFor(ogg, 0, Generic, 0); !p.AudioOnly || !p.TranscodeAudio || p.Method != "transcode" {
		t.Fatalf("Ogg Vorbis → MP3: %+v", p)
	}
}

func TestTimeSeek(t *testing.T) {
	cases := map[string]TimeSeek{
		"npt=10.5-":          {Start: 10500 * time.Millisecond},
		"npt=0:01:00-":       {Start: time.Minute},
		"NPT=1:02:03.25-":    {Start: time.Hour + 2*time.Minute + 3250*time.Millisecond},
		"npt=00:10-00:20":    {Start: 10 * time.Second, End: 20 * time.Second},
		" npt=0-100.000 ":    {End: 100 * time.Second},
		"npt=5.000-10.000/*": {},
	}
	for in, want := range cases {
		got, ok := ParseTimeSeek(in)
		if in == "npt=5.000-10.000/*" {
			if ok {
				t.Errorf("%q accepted", in)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%q: %+v %v, want %+v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"bytes=0-", "npt=-5", "npt=abc-", "npt=0:75-", "npt=20-10", "npt=10"} {
		if _, ok := ParseTimeSeek(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	if got := timeSeekReply(10_500, 3_600_000); got != "npt=10.500-3600.000/3600.000" {
		t.Fatalf("reply: %s", got)
	}
	if got := Duration(3_723_004); got != "1:02:03.004" {
		t.Fatalf("duration: %s", got)
	}
}

func TestFeatures(t *testing.T) {
	if got := Features("", true, false); got != "DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000" {
		t.Fatalf("direct: %s", got)
	}
	if got := Features("MP3", false, true); got != "DLNA.ORG_PN=MP3;DLNA.ORG_OP=10;DLNA.ORG_CI=1;DLNA.ORG_FLAGS=01700000000000000000000000000000" {
		t.Fatalf("converted: %s", got)
	}
	if got := imageFeatures("JPEG_TN"); got != "DLNA.ORG_PN=JPEG_TN;DLNA.ORG_OP=00;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=00f00000000000000000000000000000" {
		t.Fatalf("image: %s", got)
	}
}

func TestDIDL(t *testing.T) {
	doc := DIDL([]Object{
		{ID: "l:x", ParentID: "0", Title: "Films & <Docs>", Class: classFolder, Container: true, ChildCount: 3},
		{ID: "i:y", ParentID: "l:x:az", Title: "Movie", Class: classMovie, Date: "2020-01-01", ArtURL: "http://h/a?x=1&y=2",
			Res: []Res{{URL: "http://h/f?exp=1&sig=a", ProtocolInfo: ProtocolInfo("video/x-mkv", Features("", true, false)),
				Size: 1234, DurationMS: 5_400_000, Resolution: "1920x1080", Channels: 6}},
			Subtitle: "http://h/s.srt?exp=1&sig=b", SubtitleRes: true, CaptionInfoEx: true},
		{ID: "i:z", ParentID: "i:a", Title: "Song", Class: classTrack, Artist: "Artist", Album: "Album", TrackNumber: 4},
	})
	var parsed struct {
		XMLName    xml.Name `xml:"urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/ DIDL-Lite"`
		Containers []struct {
			ID         string `xml:"id,attr"`
			ChildCount string `xml:"childCount,attr"`
			Title      string `xml:"http://purl.org/dc/elements/1.1/ title"`
		} `xml:"container"`
		Items []struct {
			ID    string `xml:"id,attr"`
			Class string `xml:"urn:schemas-upnp-org:metadata-1-0/upnp/ class"`
			Art   struct {
				Profile string `xml:"urn:schemas-dlna-org:metadata-1-0/ profileID,attr"`
				URL     string `xml:",chardata"`
			} `xml:"urn:schemas-upnp-org:metadata-1-0/upnp/ albumArtURI"`
			Res []struct {
				ProtocolInfo string `xml:"protocolInfo,attr"`
				Duration     string `xml:"duration,attr"`
				Size         string `xml:"size,attr"`
				URL          string `xml:",chardata"`
			} `xml:"res"`
			Caption struct {
				Type string `xml:"http://www.sec.co.kr/ type,attr"`
				URL  string `xml:",chardata"`
			} `xml:"http://www.sec.co.kr/ CaptionInfoEx"`
			Track string `xml:"urn:schemas-upnp-org:metadata-1-0/upnp/ originalTrackNumber"`
		} `xml:"item"`
	}
	if err := xml.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, doc)
	}
	if len(parsed.Containers) != 1 || parsed.Containers[0].Title != "Films & <Docs>" || parsed.Containers[0].ChildCount != "3" {
		t.Fatalf("container: %+v", parsed.Containers)
	}
	m := parsed.Items[0]
	if m.Class != classMovie || len(m.Res) != 2 || m.Res[0].URL != "http://h/f?exp=1&sig=a" || m.Res[0].Duration != "1:30:00.000" ||
		m.Res[0].Size != "1234" || !strings.HasPrefix(m.Res[0].ProtocolInfo, "http-get:*:video/x-mkv:DLNA.ORG_OP=01") {
		t.Fatalf("movie: %+v", m)
	}
	if m.Res[1].ProtocolInfo != "http-get:*:text/srt:*" || m.Caption.Type != "srt" || m.Caption.URL != "http://h/s.srt?exp=1&sig=b" {
		t.Fatalf("subtitles: %+v", m)
	}
	if m.Art.Profile != "JPEG_TN" || m.Art.URL != "http://h/a?x=1&y=2" {
		t.Fatalf("art: %+v", m.Art)
	}
	if parsed.Items[1].Track != "4" {
		t.Fatalf("track: %+v", parsed.Items[1])
	}
}

func TestParseIDs(t *testing.T) {
	id := "0198c3a0-0000-7000-8000-000000000001"
	for in, want := range map[string]node{
		"0":                         {kind: "root"},
		"cw":                        {kind: "cw"},
		"l:" + id:                   {kind: "lib"},
		"l:" + id + ":az":           {kind: "sub", sub: "az"},
		"l:" + id + ":g:4472616d61": {kind: "genre", genre: "Drama"},
		"i:" + id:                   {kind: "item"},
		"cw:" + id:                  {kind: "cwitem"},
	} {
		got, ok := parseID(in)
		if !ok || got.kind != want.kind || got.sub != want.sub || got.genre != want.genre {
			t.Errorf("%s: %+v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "1", "l:nope", "l:" + id + ":zz", "l:" + id + ":g:zz", "i:", "../etc"} {
		if _, ok := parseID(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestParseAction(t *testing.T) {
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><ObjectID>0</ObjectID>` +
		`<BrowseFlag>BrowseDirectChildren</BrowseFlag><Filter>*</Filter><StartingIndex>5</StartingIndex>` +
		`<RequestedCount>10</RequestedCount><SortCriteria>+dc:title</SortCriteria></u:Browse></s:Body></s:Envelope>`
	r := httptest.NewRequest("POST", pathCDSControl, strings.NewReader(body))
	r.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ContentDirectory:1#Browse"`)
	a, err := ParseAction(r)
	if err != nil || a.Name != "Browse" || a.Service != typeCDS || a.Args["StartingIndex"] != "5" || a.Args["SortCriteria"] != "+dc:title" {
		t.Fatalf("%+v %v", a, err)
	}
	r = httptest.NewRequest("POST", pathCDSControl, strings.NewReader(body))
	r.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ContentDirectory:1#Search"`)
	if _, err := ParseAction(r); err == nil {
		t.Fatal("header and body disagree")
	}
	r = httptest.NewRequest("POST", pathCDSControl, strings.NewReader("<not soap"))
	if _, err := ParseAction(r); err == nil {
		t.Fatal("garbage accepted")
	}
	if sortFor("-dc:date,+dc:title") != "year" || sortFor("+upnp:originalTrackNumber") != "" {
		t.Fatal("sortFor")
	}
}

func TestDescriptions(t *testing.T) {
	for name, doc := range map[string]string{
		"device": DeviceDescription(DeviceInfo{UDN: udn, FriendlyName: "Kino & co", Version: "1.0", Serial: "x"}),
		"cds":    CDSDescription, "cms": CMSDescription,
	} {
		if err := xml.Unmarshal([]byte(doc), new(struct{})); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	d := DeviceDescription(DeviceInfo{UDN: udn, FriendlyName: "Kino"})
	for _, want := range []string{typeMediaServer, typeCDS, typeCMS, "<UDN>" + udn + "</UDN>", "DMS-1.50", pathCDSControl} {
		if !strings.Contains(d, want) {
			t.Errorf("description lacks %s", want)
		}
	}
}

func TestFolderNames(t *testing.T) {
	if folderName("hr", "continue") != "Nastavi gledati" || folderName("en", "new") != "Recently added" || folderName("xx", "az") != "A–Z" {
		t.Fatal("folder names")
	}
	for key := range folderNames["en"] {
		if folderNames["hr"][key] == "" {
			t.Errorf("no Croatian name for %s", key)
		}
	}
}

func TestFitThumbnail(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 500, 750))
	got := Fit(img, 160).Bounds()
	if got.Dx() != 106 || got.Dy() != 160 {
		t.Fatalf("fit: %v", got)
	}
	small := image.NewRGBA(image.Rect(0, 0, 100, 50))
	if Fit(small, 160) != image.Image(small) {
		t.Fatal("small image scaled")
	}
}

func TestCallbackAndHost(t *testing.T) {
	if callbackURL("<http://192.168.1.20:7676/evt><http://other/>") != "http://192.168.1.20:7676/evt" || callbackURL("nope") != "" {
		t.Fatal("callbackURL")
	}
	if !sameHost("http://192.168.1.20:7676/evt", "192.168.1.20:5000") || sameHost("http://192.168.1.21/", "192.168.1.20:5000") ||
		sameHost("http://example.com/", "192.168.1.20:5000") {
		t.Fatal("sameHost")
	}
}

func TestPickSubtitle(t *testing.T) {
	f := library.MediaFile{Audio: []library.AudioStream{{Codec: "aac", Language: "eng"}},
		Subtitles: []library.SubtitleStream{{Codec: "hdmv_pgs_subtitle", Language: "hrv"}, {Codec: "subrip", Language: "eng", Forced: true},
			{Codec: "srt", External: true, Language: "hr"}}}
	if i := pickSubtitle(f, 0, library.Prefs{SubtitleMode: "always", SubtitleLanguage: "hr"}); i != 2 {
		t.Fatalf("preferred language: %d", i)
	}
	if i := pickSubtitle(f, 0, library.Prefs{SubtitleMode: "none"}); i != -1 {
		t.Fatalf("subtitles off: %d", i)
	}
	if i := pickSubtitle(library.MediaFile{Subtitles: []library.SubtitleStream{{Codec: "hdmv_pgs_subtitle"}}}, -1, library.Prefs{}); i != -1 {
		t.Fatalf("bitmap only: %d", i)
	}
}

func TestProgressTracker(t *testing.T) {
	now := time.Unix(10_000, 0)
	tr := &tracker{now: func() time.Time { return now }}
	p := &play{t: tr, key: "a", last: now}
	tr.plays = map[string]*play{"a": p}
	p.at(42_000)
	if len(tr.idle()) != 0 {
		t.Fatal("idle too early")
	}
	now = now.Add(idleAfter)
	got := tr.idle()
	if len(got) != 1 || got[0].pos != 42_000 || len(tr.plays) != 0 {
		t.Fatalf("idle: %+v", got)
	}
	(&play{}).at(1) // a playback without a viewer is a no-op
}
