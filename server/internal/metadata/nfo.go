package metadata

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"time"
)

// NFO is the subset of the Kodi .nfo format (movie, tvshow, episodedetails) that
// Streamero reads. Users keep these files next to their media; they win over
// downloaded metadata, field by field.
type NFO struct {
	XMLName       xml.Name
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle"`
	SortTitle     string     `xml:"sorttitle"`
	Plot          string     `xml:"plot"`
	Outline       string     `xml:"outline"`
	Tagline       string     `xml:"tagline"`
	Year          string     `xml:"year"`
	Premiered     string     `xml:"premiered"`
	Aired         string     `xml:"aired"`
	Rating        string     `xml:"rating"`
	Ratings       []nfoScore `xml:"ratings>rating"`
	MPAA          string     `xml:"mpaa"`
	Genres        []string   `xml:"genre"`
	Studios       []string   `xml:"studio"`
	Directors     []string   `xml:"director"`
	Writers       []string   `xml:"credits"`
	Actors        []NFOActor `xml:"actor"`
	UniqueIDs     []nfoID    `xml:"uniqueid"`
	IMDbID        string     `xml:"imdbid"`
	TMDbID        string     `xml:"tmdbid"`
	Set           nfoSet     `xml:"set"`
}

// NFOActor is one billed performer.
type NFOActor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role"`
	Order string `xml:"order"`
}

type nfoScore struct {
	Name    string `xml:"name,attr"`
	Default bool   `xml:"default,attr"`
	Value   string `xml:"value"`
}

type nfoID struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

// nfoSet accepts both <set>Name</set> and <set><name>Name</name></set>.
type nfoSet struct {
	Name string
}

func (s *nfoSet) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var raw struct {
		Text string `xml:",chardata"`
		Name string `xml:"name"`
	}
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	s.Name = strings.TrimSpace(raw.Name)
	if s.Name == "" {
		s.Name = strings.TrimSpace(raw.Text)
	}
	return nil
}

// ParseNFO reads an .nfo document. Files that are only a URL (an old Kodi
// convention) or not XML at all return an error.
func ParseNFO(r io.Reader) (*NFO, error) {
	data, err := io.ReadAll(io.LimitReader(r, 2<<20))
	if err != nil {
		return nil, err
	}
	// Some tools write a URL line after the XML; keep only the document.
	if i := bytes.LastIndexByte(data, '>'); i >= 0 {
		data = data[:i+1]
	}
	var n NFO
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	if err := dec.Decode(&n); err != nil {
		return nil, err
	}
	switch n.XMLName.Local {
	case "movie", "tvshow", "episodedetails":
	default:
		return nil, errNotNFO
	}
	n.Title = strings.TrimSpace(n.Title)
	return &n, nil
}

type nfoError string

func (e nfoError) Error() string { return string(e) }

const errNotNFO = nfoError("not a movie, tvshow or episodedetails document")

// Overview is the plot, else the short outline.
func (n *NFO) Overview() string {
	if p := strings.TrimSpace(n.Plot); p != "" {
		return p
	}
	return strings.TrimSpace(n.Outline)
}

// Score is the community rating on a 0–10 scale, if any.
func (n *NFO) Score() *float64 {
	pick := strings.TrimSpace(n.Rating)
	for _, r := range n.Ratings {
		if r.Default || pick == "" {
			pick = strings.TrimSpace(r.Value)
		}
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(pick, ",", "."), 64)
	if err != nil || v <= 0 || v > 10 {
		return nil
	}
	return &v
}

// Premiere is the release / air date, falling back to 1 January of the year.
func (n *NFO) Premiere() *time.Time {
	for _, s := range []string{n.Premiered, n.Aired} {
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(s)); err == nil {
			return &t
		}
	}
	if y, err := strconv.Atoi(strings.TrimSpace(n.Year)); err == nil && y > 1800 && y < 3000 {
		t := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		return &t
	}
	return nil
}

// ProviderIDs collects tmdb/imdb/tvdb ids.
func (n *NFO) ProviderIDs() map[string]string {
	out := map[string]string{}
	for _, u := range n.UniqueIDs {
		t := strings.ToLower(strings.TrimSpace(u.Type))
		v := strings.TrimSpace(u.Value)
		if v != "" && (t == "tmdb" || t == "imdb" || t == "tvdb") {
			out[t] = v
		}
	}
	if v := strings.TrimSpace(n.IMDbID); v != "" {
		out["imdb"] = v
	}
	if v := strings.TrimSpace(n.TMDbID); v != "" {
		out["tmdb"] = v
	}
	return out
}

// Clean trims every entry and drops empty ones and duplicates.
func clean(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		// Kodi writes "Action / Adventure" in one tag as well as one tag per genre.
		for _, part := range strings.Split(s, " / ") {
			p := strings.TrimSpace(part)
			if p != "" && !seen[strings.ToLower(p)] {
				seen[strings.ToLower(p)] = true
				out = append(out, p)
			}
		}
	}
	return out
}
