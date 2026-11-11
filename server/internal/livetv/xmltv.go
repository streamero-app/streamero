package livetv

import (
	"bufio"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/htmlindex"
)

// XMLTVChannel is a <channel> of an XMLTV document.
type XMLTVChannel struct {
	ID    string
	Names []string
	Icon  string
}

// XMLTVProgramme is a <programme> of an XMLTV document.
type XMLTVProgramme struct {
	Channel     string
	Start, Stop time.Time // Stop is zero when the guide leaves it out
	Title       string
	SubTitle    string
	Desc        string
	Categories  []string
	Season      *int
	Episode     *int
	EpisodeKey  string
	Year        *int
	Icon        string
	New         bool
	Premiere    bool
	Repeat      bool // <previously-shown>
	Live        bool
}

type xText struct {
	Lang string `xml:"lang,attr"`
	Text string `xml:",chardata"`
}

type xIcon struct {
	Src string `xml:"src,attr"`
}

type xChannel struct {
	ID    string  `xml:"id,attr"`
	Names []xText `xml:"display-name"`
	Icons []xIcon `xml:"icon"`
}

type xEpisodeNum struct {
	System string `xml:"system,attr"`
	Text   string `xml:",chardata"`
}

type xProgramme struct {
	Start       string        `xml:"start,attr"`
	Stop        string        `xml:"stop,attr"`
	Channel     string        `xml:"channel,attr"`
	Titles      []xText       `xml:"title"`
	SubTitles   []xText       `xml:"sub-title"`
	Descs       []xText       `xml:"desc"`
	Categories  []xText       `xml:"category"`
	EpisodeNums []xEpisodeNum `xml:"episode-num"`
	Date        string        `xml:"date"`
	Icons       []xIcon       `xml:"icon"`
	Images      []xText       `xml:"image"`
	New         *struct{}     `xml:"new"`
	Premiere    *struct{}     `xml:"premiere"`
	Previously  *struct{}     `xml:"previously-shown"`
	Live        *struct{}     `xml:"live"`
}

// maxGuideBytes bounds an XMLTV document after decompression.
const maxGuideBytes = 512 << 20

// ErrNotXMLTV means the document has no <tv> root.
var ErrNotXMLTV = errors.New("not an XMLTV document")

// charsetReader decodes XMLTV documents declared in legacy encodings
// (ISO-8859-x, Windows-125x, KOI8…), which many guide grabbers still produce.
func charsetReader(label string, in io.Reader) (io.Reader, error) {
	enc, err := htmlindex.Get(label)
	if err != nil {
		return nil, fmt.Errorf("unsupported guide encoding %q", label)
	}
	return enc.NewDecoder().Reader(in), nil
}

// ParseXMLTV streams an XMLTV document (plain or gzip-compressed) and calls the
// callbacks for every channel and programme. Times without an offset are read
// in loc (the XMLTV DTD leaves the zone to the producer; UTC when loc is nil).
func ParseXMLTV(r io.Reader, loc *time.Location, onChannel func(XMLTVChannel), onProgramme func(XMLTVProgramme)) error {
	if loc == nil {
		loc = time.UTC
	}
	br := bufio.NewReader(r)
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		br = bufio.NewReader(gz)
	}
	dec := xml.NewDecoder(&limitReader{r: br, n: maxGuideBytes})
	dec.CharsetReader = charsetReader
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	root := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("guide: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "tv":
			root = true
		case "channel":
			var c xChannel
			if err := dec.DecodeElement(&c, &se); err != nil {
				return fmt.Errorf("guide channel: %w", err)
			}
			if c.ID == "" {
				continue
			}
			out := XMLTVChannel{ID: strings.TrimSpace(c.ID)}
			for _, n := range c.Names {
				if t := strings.TrimSpace(n.Text); t != "" {
					out.Names = append(out.Names, t)
				}
			}
			if len(c.Icons) > 0 {
				out.Icon = strings.TrimSpace(c.Icons[0].Src)
			}
			if onChannel != nil {
				onChannel(out)
			}
		case "programme":
			var p xProgramme
			if err := dec.DecodeElement(&p, &se); err != nil {
				return fmt.Errorf("guide programme: %w", err)
			}
			if out, ok := convertProgramme(p, loc); ok && onProgramme != nil {
				onProgramme(out)
			}
		}
	}
	if !root {
		return ErrNotXMLTV
	}
	return nil
}

type limitReader struct {
	r io.Reader
	n int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errTooLarge
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

func firstText(v []xText) string {
	for _, t := range v {
		if s := strings.TrimSpace(t.Text); s != "" {
			return s
		}
	}
	return ""
}

func convertProgramme(p xProgramme, loc *time.Location) (XMLTVProgramme, bool) {
	start, err := ParseXMLTVTime(p.Start, loc)
	if err != nil || p.Channel == "" {
		return XMLTVProgramme{}, false
	}
	out := XMLTVProgramme{Channel: strings.TrimSpace(p.Channel), Start: start, Title: firstText(p.Titles),
		SubTitle: firstText(p.SubTitles), Desc: firstText(p.Descs),
		New: p.New != nil, Premiere: p.Premiere != nil, Repeat: p.Previously != nil, Live: p.Live != nil}
	if out.Title == "" {
		return XMLTVProgramme{}, false
	}
	if p.Stop != "" {
		if stop, err := ParseXMLTVTime(p.Stop, loc); err == nil && stop.After(start) {
			out.Stop = stop
		}
	}
	seen := map[string]bool{}
	for _, c := range p.Categories {
		if t := strings.TrimSpace(c.Text); t != "" && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			out.Categories = append(out.Categories, t)
		}
	}
	if len(p.Icons) > 0 {
		out.Icon = strings.TrimSpace(p.Icons[0].Src)
	}
	if out.Icon == "" && len(p.Images) > 0 {
		out.Icon = strings.TrimSpace(p.Images[0].Text)
	}
	if len(p.Date) >= 4 {
		if y, err := strconv.Atoi(p.Date[:4]); err == nil && y > 1880 && y < 2200 {
			out.Year = &y
		}
	}
	var progID string
	for _, e := range p.EpisodeNums {
		switch strings.ToLower(strings.TrimSpace(e.System)) {
		case "xmltv_ns":
			if s, ep, ok := parseXMLTVNS(e.Text); ok {
				if s != nil {
					out.Season = s
				}
				if ep != nil {
					out.Episode = ep
				}
			}
		case "onscreen", "sxxexx", "":
			if out.Season == nil && out.Episode == nil {
				if s, ep, ok := parseOnscreen(e.Text); ok {
					out.Season, out.Episode = &s, &ep
				}
			}
		case "dd_progid":
			progID = strings.TrimSpace(e.Text)
		}
	}
	out.EpisodeKey = episodeKey(out, progID)
	return out, true
}

// episodeKey identifies an episode for "new episodes only": season and episode
// numbers when known, else the guide's programme id, else the episode title.
func episodeKey(p XMLTVProgramme, progID string) string {
	switch {
	case p.Season != nil && p.Episode != nil:
		return fmt.Sprintf("s%de%d", *p.Season, *p.Episode)
	case progID != "":
		return "id:" + strings.ToLower(progID)
	case p.SubTitle != "":
		return "t:" + normName(p.SubTitle)
	}
	return ""
}

// parseXMLTVNS reads "season.episode.part" with zero-based numbers and
// optional "/total" parts, e.g. "2.9.0/1" = season 3, episode 10.
func parseXMLTVNS(s string) (*int, *int, bool) {
	parts := strings.Split(strings.ReplaceAll(s, " ", ""), ".")
	if len(parts) < 2 {
		return nil, nil, false
	}
	num := func(v string) *int {
		v, _, _ = strings.Cut(v, "/")
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil
		}
		n++
		return &n
	}
	s0, e0 := num(parts[0]), num(parts[1])
	return s0, e0, s0 != nil || e0 != nil
}

var onscreenRE = regexp.MustCompile(`(?i)S\s*(\d{1,4})\s*[ .:-]?\s*E[Pp]?\s*(\d{1,4})`)

func parseOnscreen(s string) (int, int, bool) {
	m := onscreenRE.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return a, b, true
}

// ParseXMLTVTime reads XMLTV times: "YYYYMMDDhhmmss +hhmm", where seconds,
// minutes and hours may be left out and the offset may be "+hh", "+hhmm",
// "+hh:mm", "Z", "UTC"/"GMT" or absent (then loc applies).
func ParseXMLTVTime(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	digits := 0
	for digits < len(s) && s[digits] >= '0' && s[digits] <= '9' {
		digits++
	}
	if digits < 8 || digits > 14 || digits%2 != 0 {
		return time.Time{}, fmt.Errorf("bad guide time %q", s)
	}
	field := func(from, to int) int {
		if to > digits {
			return 0
		}
		n, _ := strconv.Atoi(s[from:to])
		return n
	}
	y, mo, d := field(0, 4), field(4, 6), field(6, 8)
	h, mi, sec := field(8, 10), field(10, 12), field(12, 14)
	if mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || sec > 60 {
		return time.Time{}, fmt.Errorf("bad guide time %q", s)
	}
	rest := strings.TrimSpace(s[digits:])
	switch {
	case rest == "":
		if loc == nil {
			loc = time.UTC
		}
		return time.Date(y, time.Month(mo), d, h, mi, sec, 0, loc), nil
	case strings.EqualFold(rest, "Z"), strings.EqualFold(rest, "UTC"), strings.EqualFold(rest, "GMT"):
		return time.Date(y, time.Month(mo), d, h, mi, sec, 0, time.UTC), nil
	case rest[0] == '+' || rest[0] == '-':
		off := strings.ReplaceAll(rest[1:], ":", "")
		if len(off) != 2 && len(off) != 4 {
			return time.Time{}, fmt.Errorf("bad guide time zone %q", rest)
		}
		oh, err1 := strconv.Atoi(off[:2])
		om := 0
		var err2 error
		if len(off) == 4 {
			om, err2 = strconv.Atoi(off[2:])
		}
		if err1 != nil || err2 != nil || oh > 14 || om > 59 {
			return time.Time{}, fmt.Errorf("bad guide time zone %q", rest)
		}
		secs := oh*3600 + om*60
		if rest[0] == '-' {
			secs = -secs
		}
		return time.Date(y, time.Month(mo), d, h, mi, sec, 0, time.FixedZone("", secs)).UTC(), nil
	}
	// A zone name such as "CET": try it as a location, else refuse.
	if l, err := time.LoadLocation(rest); err == nil {
		return time.Date(y, time.Month(mo), d, h, mi, sec, 0, l).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("bad guide time zone %q", rest)
}
