package books

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Info is what a book or comic file says about itself.
type Info struct {
	Format      string   `json:"format,omitempty"` // epub, pdf, mobi, azw3, cbz, cbr, cb7, cbt
	Title       string   `json:"title,omitempty"`
	Authors     []string `json:"authors,omitempty"`
	Series      string   `json:"series,omitempty"`
	SeriesIndex *float64 `json:"seriesIndex,omitempty"`
	Volume      *float64 `json:"volume,omitempty"`
	Chapter     *float64 `json:"chapter,omitempty"`
	ISBN        string   `json:"isbn,omitempty"`
	Publisher   string   `json:"publisher,omitempty"`
	Language    string   `json:"language,omitempty"`
	Description string   `json:"description,omitempty"`
	Published   string   `json:"published,omitempty"`
	Year        int      `json:"year,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	PageCount   int      `json:"pageCount,omitempty"`
	RightToLeft bool     `json:"rtl,omitempty"`
}

// Formats maps book file extensions to their format names.
var Formats = map[string]string{".epub": "epub", ".pdf": "pdf", ".mobi": "mobi", ".azw3": "azw3", ".azw": "mobi",
	".cbz": "cbz", ".cbr": "cbr", ".cb7": "cb7", ".cbt": "cbt"}

// FormatOf returns the book format of a file name ("" if it is not a book).
func FormatOf(name string) string { return Formats[strings.ToLower(path.Ext(name))] }

// IsComic reports a comic archive format.
func IsComic(format string) bool {
	return format == "cbz" || format == "cbr" || format == "cb7" || format == "cbt"
}

// --- ISBN ----------------------------------------------------------------------------

// NormalizeISBN returns a valid ISBN-13 or ISBN-10 (digits only, check digit verified) or "".
func NormalizeISBN(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "urn:isbn:"), "isbn:")
	s = strings.TrimSpace(strings.TrimPrefix(s, "isbn"))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'x':
			b.WriteRune('X')
		case r == '-' || r == ' ':
		default:
			return ""
		}
	}
	d := b.String()
	switch len(d) {
	case 13:
		sum := 0
		for i := 0; i < 13; i++ {
			if d[i] == 'X' {
				return ""
			}
			n := int(d[i] - '0')
			if i%2 == 1 {
				n *= 3
			}
			sum += n
		}
		if sum%10 == 0 {
			return d
		}
	case 10:
		sum := 0
		for i := 0; i < 10; i++ {
			n := 10
			if d[i] != 'X' {
				n = int(d[i] - '0')
			} else if i != 9 {
				return ""
			}
			sum += n * (10 - i)
		}
		if sum%11 == 0 {
			return d
		}
	}
	return ""
}

// --- OPF (EPUB package document) ------------------------------------------------------

type opfDoc struct {
	Version  string `xml:"version,attr"`
	Metadata struct {
		Titles      []opfText `xml:"title"`
		Creators    []opfText `xml:"creator"`
		Identifiers []opfText `xml:"identifier"`
		Languages   []string  `xml:"language"`
		Publishers  []string  `xml:"publisher"`
		Description []string  `xml:"description"`
		Dates       []string  `xml:"date"`
		Subjects    []string  `xml:"subject"`
		Metas       []opfMeta `xml:"meta"`
	} `xml:"metadata"`
	Manifest []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		MediaType  string `xml:"media-type,attr"`
		Properties string `xml:"properties,attr"`
	} `xml:"manifest>item"`
	Spine struct {
		Direction string `xml:"page-progression-direction,attr"`
	} `xml:"spine"`
}

type opfText struct {
	ID     string `xml:"id,attr"`
	Role   string `xml:"role,attr"`   // EPUB 2 opf:role
	Scheme string `xml:"scheme,attr"` // EPUB 2 opf:scheme
	Value  string `xml:",chardata"`
}

type opfMeta struct {
	Name     string `xml:"name,attr"`
	Content  string `xml:"content,attr"`
	Property string `xml:"property,attr"`
	Refines  string `xml:"refines,attr"`
	ID       string `xml:"id,attr"`
	Value    string `xml:",chardata"`
}

// OPF is a parsed package document.
type OPF struct {
	Info
	// CoverHref is the cover image, relative to the OPF file.
	CoverHref string
}

func newDecoder(data []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil } // tolerate latin-1 declarations
	d.Entity = xml.HTMLEntity
	return d
}

// ParseOPF reads the metadata of an EPUB package document (EPUB 2 and 3).
func ParseOPF(data []byte) (OPF, error) {
	var doc opfDoc
	if err := newDecoder(data).Decode(&doc); err != nil {
		return OPF{}, err
	}
	m := doc.Metadata
	o := OPF{Info: Info{Format: "epub"}}
	// EPUB 3 refinements: <meta refines="#id" property="role">aut</meta>.
	refine := map[string]map[string]string{}
	for _, x := range m.Metas {
		if x.Refines != "" {
			id := strings.TrimPrefix(x.Refines, "#")
			if refine[id] == nil {
				refine[id] = map[string]string{}
			}
			refine[id][x.Property] = strings.TrimSpace(x.Value)
		}
	}
	for _, t := range m.Titles {
		if v := clean(t.Value); v != "" {
			if r := refine[t.ID]["title-type"]; r != "" && r != "main" && o.Title != "" {
				continue
			}
			if o.Title == "" || refine[t.ID]["title-type"] == "main" {
				o.Title = v
			}
		}
	}
	for _, c := range m.Creators {
		role := c.Role
		if r := refine[c.ID]["role"]; r != "" {
			role = r
		}
		if role == "" || role == "aut" {
			if v := clean(c.Value); v != "" {
				o.Authors = appendUnique(o.Authors, v)
			}
		}
	}
	for _, id := range m.Identifiers {
		if isbn := NormalizeISBN(id.Value); isbn != "" && (o.ISBN == "" || len(isbn) == 13) {
			o.ISBN = isbn
		}
	}
	if len(m.Languages) > 0 {
		o.Language = clean(m.Languages[0])
	}
	if len(m.Publishers) > 0 {
		o.Publisher = clean(m.Publishers[0])
	}
	if len(m.Description) > 0 {
		o.Description = StripHTML(m.Description[0])
	}
	if len(m.Dates) > 0 {
		o.Published = clean(m.Dates[0])
		o.Year = yearOf(o.Published)
	}
	for _, s := range m.Subjects {
		if v := clean(s); v != "" {
			o.Genres = appendUnique(o.Genres, v)
		}
	}
	coverID := ""
	for _, x := range m.Metas {
		switch {
		case x.Name == "cover":
			coverID = x.Content
		case x.Name == "calibre:series":
			o.Series = clean(x.Content)
		case x.Name == "calibre:series_index":
			o.SeriesIndex = parseNum(x.Content)
		case x.Property == "belongs-to-collection" && o.Series == "":
			// EPUB 3 series: a collection of type "series" (or untyped) with a position.
			if t := refine[x.ID]["collection-type"]; t == "" || t == "series" {
				o.Series = clean(x.Value)
				o.SeriesIndex = parseNum(refine[x.ID]["group-position"])
			}
		}
	}
	for _, it := range doc.Manifest {
		if strings.Contains(" "+it.Properties+" ", " cover-image ") {
			o.CoverHref = it.Href
		}
	}
	if o.CoverHref == "" && coverID != "" {
		for _, it := range doc.Manifest {
			if it.ID == coverID {
				o.CoverHref = it.Href
			}
		}
	}
	if o.CoverHref == "" {
		for _, it := range doc.Manifest {
			if strings.HasPrefix(it.MediaType, "image/") && strings.Contains(strings.ToLower(it.ID+it.Href), "cover") {
				o.CoverHref = it.Href
				break
			}
		}
	}
	o.RightToLeft = doc.Spine.Direction == "rtl"
	return o, nil
}

// --- ComicInfo.xml -------------------------------------------------------------------

type comicInfo struct {
	Title, Series, Number, Volume, Summary, Writer, Publisher, Genre, LanguageISO, Manga, GTIN, ISBN string
	Year, Month, Day, PageCount                                                                      int
}

// ParseComicInfo reads a ComicInfo.xml document (the community ComicInfo schema).
func ParseComicInfo(data []byte) (Info, error) {
	var c comicInfo
	if err := newDecoder(data).Decode(&c); err != nil {
		return Info{}, err
	}
	in := Info{Title: clean(c.Title), Series: clean(c.Series), Description: StripHTML(c.Summary),
		Publisher: clean(c.Publisher), Language: clean(c.LanguageISO), PageCount: max(c.PageCount, 0), Year: c.Year}
	if in.Year < 1000 || in.Year > 3000 {
		in.Year = 0
	}
	in.Chapter = parseNum(c.Number)
	in.Volume = parseNum(c.Volume)
	in.SeriesIndex = in.Chapter
	if in.SeriesIndex == nil {
		in.SeriesIndex = in.Volume
	}
	for _, w := range splitList(c.Writer) {
		in.Authors = appendUnique(in.Authors, w)
	}
	for _, g := range splitList(c.Genre) {
		in.Genres = appendUnique(in.Genres, g)
	}
	in.ISBN = NormalizeISBN(c.GTIN)
	if in.ISBN == "" {
		in.ISBN = NormalizeISBN(c.ISBN)
	}
	in.RightToLeft = strings.EqualFold(strings.TrimSpace(c.Manga), "YesAndRightToLeft")
	if in.Year > 0 && c.Month >= 1 && c.Month <= 12 {
		d := max(c.Day, 1)
		in.Published = strconv.Itoa(in.Year) + "-" + pad2(c.Month) + "-" + pad2(min(d, 31))
	}
	return in, nil
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// --- file names ----------------------------------------------------------------------

var (
	volRe     = regexp.MustCompile(`(?i)(?:^|[\s._\-(\[])(?:vol(?:ume)?\.?|v|tome|t|svezak|sv\.?|band)\s*\.?\s*(\d{1,4}(?:\.\d+)?)(?:$|[\s._\-)\]])`)
	chapterRe = regexp.MustCompile(`(?i)(?:^|[\s._\-(\[])(?:ch(?:apter)?\.?|c|#|issue|poglavlje|ep\.?)\s*\.?\s*(\d{1,4}(?:\.\d+)?)(?:$|[\s._\-)\]])`)
	trailNum  = regexp.MustCompile(`^(.*?\D)[\s._\-#]+(\d{1,4}(?:\.\d+)?)\s*$`)
	yearParen = regexp.MustCompile(`\s*[(\[]((?:19|20)\d{2})[)\]]\s*`)
	bracketed = regexp.MustCompile(`\s*[(\[{][^)\]}]*[)\]}]\s*`)
)

// NameInfo is what a comic or book file name says ("Saga v03 c017 (2014).cbz").
type NameInfo struct {
	Title           string
	Series          string
	Volume, Chapter *float64
	Year            int
}

// ParseFileName reads series, volume, chapter and year from a file name stem.
func ParseFileName(stem string) NameInfo {
	var n NameInfo
	s := strings.NewReplacer("_", " ").Replace(stem)
	if m := yearParen.FindStringSubmatch(s); m != nil {
		n.Year, _ = strconv.Atoi(m[1])
	}
	s = strings.TrimSpace(bracketed.ReplaceAllString(s, " "))
	cut := len(s)
	if loc := volRe.FindStringSubmatchIndex(s); loc != nil {
		n.Volume = parseNum(s[loc[2]:loc[3]])
		cut = min(cut, loc[0])
	}
	if loc := chapterRe.FindStringSubmatchIndex(s); loc != nil {
		n.Chapter = parseNum(s[loc[2]:loc[3]])
		cut = min(cut, loc[0])
	}
	if n.Volume == nil && n.Chapter == nil {
		// "Series 012" → issue 12 (but not "1984" alone).
		if m := trailNum.FindStringSubmatch(s); m != nil {
			n.Chapter = parseNum(m[2])
			cut = len(m[1])
		}
	}
	n.Series = strings.TrimSpace(strings.Trim(strings.TrimSpace(s[:cut]), "-.,#"))
	n.Title = strings.TrimSpace(s)
	if n.Title == "" {
		n.Title = stem
	}
	return n
}

// --- helpers -------------------------------------------------------------------------

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return list
		}
	}
	return append(list, v)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '&' }) {
		if p = clean(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseNum(s string) *float64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || f > 100000 {
		return nil
	}
	return &f
}

func yearOf(s string) int {
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil && y > 1000 && y < 3000 {
			return y
		}
	}
	return 0
}

// StripHTML turns an HTML description into plain text with paragraph breaks.
func StripHTML(s string) string {
	if !strings.ContainsAny(s, "<&") {
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	d := xml.NewDecoder(strings.NewReader("<x>" + s + "</x>"))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	for {
		tok, err := d.Token()
		if err != nil {
			if !errors.Is(err, io.EOF) && b.Len() == 0 {
				return strings.TrimSpace(s)
			}
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			if t.Name.Local == "br" {
				b.WriteString("\n")
			}
		case xml.EndElement:
			if t.Name.Local == "p" || t.Name.Local == "div" {
				b.WriteString("\n\n")
			}
		}
	}
	lines := strings.Split(b.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.FieldsFunc(l, unicode.IsSpace), " ")
	}
	out := strings.Join(lines, "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}
