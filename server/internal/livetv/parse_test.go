package livetv

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"
	xunicode "golang.org/x/text/encoding/unicode"
)

func TestParseM3U(t *testing.T) {
	src := "#EXTM3U x-tvg-url=\"http://example.com/guide.xml\"\r\n" +
		"#EXTINF:-1 tvg-id=\"hrt1.hr\" tvg-name=\"HRT 1\" tvg-logo=\"http://img/hrt1.png\" group-title=\"Vijesti\" tvg-chno=\"1\",HRT 1 HD\r\n" +
		"http://iptv.example/hrt1.ts\r\n" +
		"#EXTINF:0 group-title=Sport tvg-id=arena1,Arena Sport 1, Live\n" +
		"#EXTVLCOPT:http-user-agent=VLC/3.0\n" +
		"https://iptv.example/arena1.m3u8\n" +
		"#EXTGRP:Djeca\n" +
		"#EXTINF:-1,Crtići\n" +
		"udp://@239.1.1.1:1234\n" +
		"#EXTINF:-1,Local file\n" +
		"/etc/passwd\n" +
		"#EXTINF:-1,Concat\n" +
		"concat:a.ts|b.ts\n" +
		"#EXTINF:-1,No info entry follows\n" +
		"rtsp://cam.local/stream\n" +
		"http://iptv.example/bare.ts\n"
	chans, skipped, err := ParseM3U(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2 (local file, concat)", skipped)
	}
	if len(chans) != 5 {
		t.Fatalf("got %d channels: %+v", len(chans), chans)
	}
	c := chans[0]
	if c.TvgID != "hrt1.hr" || c.Name != "HRT 1 HD" || c.Logo != "http://img/hrt1.png" || c.Group != "Vijesti" || c.Number != "1" ||
		c.URL != "http://iptv.example/hrt1.ts" {
		t.Fatalf("first: %+v", c)
	}
	// Unquoted attributes, a comma inside the name, a user agent from #EXTVLCOPT.
	if chans[1].TvgID != "arena1" || chans[1].Group != "Sport" || chans[1].Name != "Arena Sport 1, Live" || chans[1].UserAgent != "VLC/3.0" {
		t.Fatalf("second: %+v", chans[1])
	}
	if chans[2].Group != "Djeca" || chans[2].Name != "Crtići" {
		t.Fatalf("#EXTGRP: %+v", chans[2])
	}
	if chans[4].Name != "http://iptv.example/bare.ts" {
		t.Fatalf("bare URL gets its URL as name: %+v", chans[4])
	}
	if NormalizeCategory(chans[0].Group) != CatNews || NormalizeCategory(chans[1].Group) != CatSports || NormalizeCategory(chans[2].Group) != CatKids {
		t.Fatal("group categories")
	}
}

func TestParseM3UEncodings(t *testing.T) {
	text := "#EXTM3U\n#EXTINF:-1 tvg-id=\"rtl\",RTL Kockica Dječji\nhttp://x/rtl.ts\n"
	latin, err := charmap.Windows1252.NewEncoder().String("#EXTM3U\n#EXTINF:-1,Télé Café\nhttp://x/a.ts\n")
	if err != nil {
		t.Fatal(err)
	}
	u16, err := xunicode.UTF16(xunicode.LittleEndian, xunicode.UseBOM).NewEncoder().String(text)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ in, want string }{
		"utf8 bom":     {"\xEF\xBB\xBF" + text, "RTL Kockica Dječji"},
		"windows-1252": {latin, "Télé Café"},
		"utf16 bom":    {u16, "RTL Kockica Dječji"},
	}
	for name, c := range cases {
		chans, _, err := ParseM3U(strings.NewReader(c.in))
		if err != nil || len(chans) != 1 || chans[0].Name != c.want {
			t.Errorf("%s: %v %+v", name, err, chans)
		}
	}
	if _, _, err := ParseM3U(strings.NewReader("#EXTM3U\n# nothing\n")); err != ErrEmptyPlaylist {
		t.Fatalf("empty: %v", err)
	}
}

func TestParseXMLTVTime(t *testing.T) {
	zg, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Skip("no tzdata")
	}
	cases := []struct {
		in   string
		loc  *time.Location
		want string
	}{
		{"20261111203000 +0100", nil, "2026-11-11T19:30:00Z"},
		{"20261111203000 -0500", nil, "2026-11-12T01:30:00Z"},
		{"20261111203000 +05:30", nil, "2026-11-11T15:00:00Z"},
		{"20261111203000+0100", nil, "2026-11-11T19:30:00Z"},
		{"20261111203000 Z", nil, "2026-11-11T20:30:00Z"},
		{"20261111203000 UTC", nil, "2026-11-11T20:30:00Z"},
		{"202611112030", nil, "2026-11-11T20:30:00Z"},
		{"2026111120", nil, "2026-11-11T20:00:00Z"},
		{"20261111", nil, "2026-11-11T00:00:00Z"},
		// No offset: the source's zone, summer and winter time.
		{"20260715203000", zg, "2026-07-15T18:30:00Z"},
		{"20261111203000", zg, "2026-11-11T19:30:00Z"},
		{"20261111203000 +01", nil, "2026-11-11T19:30:00Z"},
	}
	for _, c := range cases {
		got, err := ParseXMLTVTime(c.in, c.loc)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got.UTC().Format(time.RFC3339) != c.want {
			t.Errorf("%q = %s, want %s", c.in, got.UTC().Format(time.RFC3339), c.want)
		}
	}
	// The night the clocks go back, 02:30 exists twice: either instant is acceptable.
	if got, err := ParseXMLTVTime("20261025023000", zg); err != nil ||
		(got.UTC().Format(time.RFC3339) != "2026-10-25T00:30:00Z" && got.UTC().Format(time.RFC3339) != "2026-10-25T01:30:00Z") {
		t.Errorf("ambiguous local time: %v %v", got, err)
	}
	for _, bad := range []string{"", "2026", "20261311000000", "20261111203000 +9999", "abc", "20261111203000 Mars/Olympus"} {
		if _, err := ParseXMLTVTime(bad, nil); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

const sampleXMLTV = `<?xml version="1.0" encoding="ISO-8859-2"?>
<!DOCTYPE tv SYSTEM "xmltv.dtd">
<tv generator-info-name="test">
  <channel id="hrt1.hr"><display-name lang="hr">HRT 1</display-name><display-name>1</display-name><icon src="http://img/hrt1.png"/></channel>
  <channel id="arena1"><display-name>Arena Sport 1 HD</display-name></channel>
  <programme start="20261111190000 +0100" stop="20261111193000 +0100" channel="hrt1.hr">
    <title lang="hr">Dnevnik</title><desc>Vijesti &amp; vrijeme</desc><category>News</category><category>Vijesti</category>
    <new/>
  </programme>
  <programme start="20261111193000 +0100" channel="hrt1.hr">
    <title>Lijepom našom</title><sub-title>Čakovec</sub-title>
    <episode-num system="xmltv_ns">2.9.0/1</episode-num><episode-num system="onscreen">S03E10</episode-num>
    <previously-shown/><icon src="http://img/lijepom.jpg"/><date>2019</date>
  </programme>
  <programme start="20261111210000 +0100" stop="20261111230000 +0100" channel="hrt1.hr">
    <title>Film</title><category>Movie / Drama</category><episode-num system="onscreen">Ep. 4 S2</episode-num>
  </programme>
  <programme start="bad" channel="hrt1.hr"><title>Broken</title></programme>
  <programme start="20261111200000 +0100" stop="20261111220000 +0100" channel="arena1">
    <title>Nogomet: HNL</title><category>Sports</category><episode-num system="dd_progid">EP1234.0005</episode-num><live/>
  </programme>
</tv>`

func TestParseXMLTV(t *testing.T) {
	doc, err := charmap.ISO8859_2.NewEncoder().String(sampleXMLTV)
	if err != nil {
		t.Fatal(err)
	}
	var chans []XMLTVChannel
	var progs []XMLTVProgramme
	err = ParseXMLTV(strings.NewReader(doc), nil, func(c XMLTVChannel) { chans = append(chans, c) },
		func(p XMLTVProgramme) { progs = append(progs, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(chans) != 2 || chans[0].ID != "hrt1.hr" || len(chans[0].Names) != 2 || chans[0].Icon != "http://img/hrt1.png" {
		t.Fatalf("channels: %+v", chans)
	}
	if len(progs) != 4 {
		t.Fatalf("programmes: %d %+v", len(progs), progs)
	}
	p := progs[0]
	if p.Title != "Dnevnik" || p.Desc != "Vijesti & vrijeme" || !p.New || CategoryOf(p.Categories) != CatNews ||
		p.Start.UTC().Format(time.RFC3339) != "2026-11-11T18:00:00Z" || p.Stop.Sub(p.Start) != 30*time.Minute {
		t.Fatalf("first: %+v", p)
	}
	q := progs[1]
	if q.Title != "Lijepom našom" || q.SubTitle != "Čakovec" || !q.Repeat || q.Season == nil || *q.Season != 3 || *q.Episode != 10 ||
		q.EpisodeKey != "s3e10" || q.Icon != "http://img/lijepom.jpg" || q.Year == nil || *q.Year != 2019 || !q.Stop.IsZero() {
		t.Fatalf("second: %+v", q)
	}
	if progs[2].Season != nil || CategoryOf(progs[2].Categories) != CatMovies {
		t.Fatalf("film: %+v", progs[2])
	}
	if progs[3].EpisodeKey != "id:ep1234.0005" || !progs[3].Live || CategoryOf(progs[3].Categories) != CatSports {
		t.Fatalf("sport: %+v", progs[3])
	}
	// A missing stop takes the next programme's start.
	pp := make([]parsedProgramme, len(progs))
	for i := range progs {
		pp[i] = parsedProgramme{progs[i]}
	}
	fillStops(pp)
	if !pp[1].Stop.Equal(pp[2].Start) {
		t.Fatalf("stop not filled: %v", pp[1].Stop)
	}
}

func TestParseXMLTVGzipAndErrors(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte(`<tv><programme start="20260101000000" stop="20260101010000" channel="a"><title>T</title></programme></tv>`))
	_ = gz.Close()
	n := 0
	if err := ParseXMLTV(&buf, time.UTC, nil, func(XMLTVProgramme) { n++ }); err != nil || n != 1 {
		t.Fatalf("gzip: %v %d", err, n)
	}
	if err := ParseXMLTV(strings.NewReader(`<html><body>login</body></html>`), nil, nil, nil); err != ErrNotXMLTV {
		t.Fatalf("not xmltv: %v", err)
	}
	if err := ParseXMLTV(strings.NewReader(`<?xml version="1.0" encoding="x-unknown-enc"?><tv/>`), nil, nil, nil); err == nil {
		t.Fatal("unknown encoding should fail")
	}
}

func TestEpisodeNumbers(t *testing.T) {
	s, e, ok := parseXMLTVNS("0 . 4/12 . ")
	if !ok || *s != 1 || *e != 5 {
		t.Fatalf("xmltv_ns: %v %v %v", s, e, ok)
	}
	s, e, ok = parseXMLTVNS(".7.")
	if !ok || s != nil || *e != 8 {
		t.Fatal("episode only")
	}
	if a, b, ok := parseOnscreen("S1 E12"); !ok || a != 1 || b != 12 {
		t.Fatal("onscreen")
	}
}

func TestNormNameAndCategories(t *testing.T) {
	cases := map[string]string{"HRT 1 HD": "hrt1", "hrt-1": "hrt1", "Nova TV FHD": "novatv", "Đuro Ćevapčić": "durocevapcic",
		"Sky Sports+": "skysportsplus", "4K": "4k"}
	for in, want := range cases {
		if got := normName(in); got != want {
			t.Errorf("normName(%q) = %q, want %q", in, got, want)
		}
	}
	cats := map[string]string{"Dječji program": CatKids, "Animated film": CatKids, "Sports event": CatSports, "Film / Drama": CatMovies,
		"Informativni": CatNews, "Current affairs": CatNews, "Documentary": "", "": ""}
	for in, want := range cats {
		if got := NormalizeCategory(in); got != want {
			t.Errorf("NormalizeCategory(%q) = %q, want %q", in, got, want)
		}
	}
}
