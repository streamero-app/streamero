// Package adult implements optional adult libraries (D-053): a scanner for scene
// and movie files named by Locarr (or by scene release names), metadata from
// ThePornDB and StashDB, and the access gate every adult endpoint goes through.
// Its content lives only in the adult_* tables.
package adult

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Parsed is what a file name says about a scene or movie.
type Parsed struct {
	Kind       string   `json:"kind"` // scene or movie
	Studio     string   `json:"studio,omitempty"`
	Date       string   `json:"date,omitempty"` // YYYY-MM-DD
	Year       int      `json:"year,omitempty"`
	Title      string   `json:"title"`
	Performers []string `json:"performers,omitempty"`
}

var (
	fullDate   = regexp.MustCompile(`(?:^|[\s._\-\[(])((?:19|20)\d{2})[-. _](0[1-9]|1[0-2])[-. _](0[1-9]|[12]\d|3[01])(?:$|[\s._\-\])])`)
	shortDate  = regexp.MustCompile(`(?:^| )(\d{2}) (0[1-9]|1[0-2]) (0[1-9]|[12]\d|3[01])(?:$| )`)
	movieYear  = regexp.MustCompile(`^(.+?)\s*\(((?:19|20)\d{2})\)$`)
	bracketEnd = regexp.MustCompile(`\s*\[([^\[\]]+)\]$`)
	// Quality tokens Locarr and release names put at the end ("WEBDL-1080p", "2160p", "XXX").
	qualityTok = regexp.MustCompile(`(?i)^(?:(?:web-?dl|webrip|web|blu-?ray|bdrip|brrip|hdtv|sdtv|dvd(?:rip)?|remux|raw-hd|unknown)(?:-(?:\d{3,4}p|4k|uhd|sd))?|\d{3,4}p|4k|uhd|xxx|hevc|x26[45]|h\.?26[45]|av1|mp4-\w+|repack|proper)$`)
)

// releaseStyle reports names like "Studio.24.01.15.Performer.Title.1080p" (dots or
// underscores instead of spaces).
func releaseStyle(s string) bool {
	return !strings.Contains(s, " ") && strings.Count(s, ".")+strings.Count(s, "_") >= 3
}

// stripQuality removes trailing quality and codec tokens.
func stripQuality(s string) string {
	for {
		s = strings.TrimRight(s, " -_.")
		i := strings.LastIndexAny(s, " ")
		tok := s[i+1:]
		if i < 0 || !qualityTok.MatchString(tok) {
			return s
		}
		s = s[:i]
	}
}

func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, " -_.,")
}

// splitNames splits "A, B & C" into names.
func splitNames(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '&' || r == ';' }) {
		if n := clean(part); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// ParseFile reads a scene or movie from its file name. folder is the name of
// the folder that holds it ("" when the file is directly in the library folder):
// Locarr puts scenes in a folder per studio ({Studio}) and movies in
// "{Title} ({Year})". Recognised file names (D-053):
//
//	{Studio} - {Date} - {Title} [{Performers}] {Quality}   Locarr's default
//	{Studio} {Date} {Title} {Performers}                    without separators
//	Studio.YY.MM.DD.Title.1080p                             scene release names
//	{Title} ({Year}) {Quality}                              movies
func ParseFile(folder, file string) Parsed {
	stem := strings.TrimSuffix(file, filepath.Ext(file))
	if releaseStyle(stem) {
		stem = strings.NewReplacer(".", " ", "_", " ").Replace(stem)
	}
	stem = stripQuality(clean(stem))
	p := Parsed{Kind: "scene"}
	if m := bracketEnd.FindStringSubmatch(stem); m != nil {
		p.Performers = splitNames(m[1])
		stem = clean(stem[:len(stem)-len(m[0])])
		stem = stripQuality(stem)
	}
	if loc := fullDate.FindStringSubmatchIndex(stem); loc != nil {
		y, mo, d := stem[loc[2]:loc[3]], stem[loc[4]:loc[5]], stem[loc[6]:loc[7]]
		if _, err := time.Parse("2006-01-02", y+"-"+mo+"-"+d); err == nil {
			p.Date = y + "-" + mo + "-" + d
			p.Studio = clean(stem[:loc[2]])
			p.Title = clean(stem[loc[7]:])
		}
	} else if loc := shortDate.FindStringSubmatchIndex(stem); loc != nil {
		date := "20" + stem[loc[2]:loc[3]] + "-" + stem[loc[4]:loc[5]] + "-" + stem[loc[6]:loc[7]]
		if _, err := time.Parse("2006-01-02", date); err == nil {
			p.Date = date
			p.Studio = clean(stem[:loc[2]])
			p.Title = clean(stem[loc[7]:])
		}
	}
	if p.Date != "" {
		p.Year, _ = strconv.Atoi(p.Date[:4])
		if p.Performers == nil {
			p.Title, p.Performers = splitTitlePerformers(p.Title)
		}
	} else {
		if m := movieYear.FindStringSubmatch(stem); m != nil {
			p.Kind, p.Title = "movie", clean(m[1])
			p.Year, _ = strconv.Atoi(m[2])
		} else if m := movieYear.FindStringSubmatch(clean(folder)); m != nil && folder != "" {
			p.Kind, p.Title = "movie", clean(m[1])
			p.Year, _ = strconv.Atoi(m[2])
		} else {
			p.Title = stem
		}
	}
	if p.Kind == "scene" && p.Studio == "" && folder != "" && movieYear.FindStringSubmatch(folder) == nil {
		p.Studio = clean(folder)
	}
	// "{Studio} {Date} {Title}" inside the studio's folder: the folder name is the
	// reliable spelling of the studio.
	if p.Kind == "scene" && folder != "" && strings.EqualFold(foldKey(p.Studio), foldKey(folder)) {
		p.Studio = clean(folder)
	}
	if p.Title == "" {
		p.Title = clean(stem)
	}
	return p
}

// splitTitlePerformers separates performers from the title for templates without
// brackets: "Title - A, B" splits at the last " - "; "Title A B, C D" takes every
// name after a comma and the last two words before the first one. Without a comma
// or a dash nothing is split (the metadata lookup fixes it).
func splitTitlePerformers(s string) (string, []string) {
	if i := strings.LastIndex(s, " - "); i > 0 {
		return clean(s[:i]), splitNames(s[i+3:])
	}
	parts := strings.Split(s, ",")
	if len(parts) < 2 {
		return s, nil
	}
	head := strings.Fields(parts[0])
	if len(head) < 3 {
		return s, nil
	}
	names := []string{strings.Join(head[len(head)-2:], " ")}
	for _, p := range parts[1:] {
		names = append(names, splitNames(p)...)
	}
	return strings.Join(head[:len(head)-2], " "), names
}

// foldKey is the matching key of a name: lower case letters and digits only.
func foldKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sortTitle drops a leading article for sorting.
func sortTitle(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(l, a) && len(l) > len(a) {
			return l[len(a):]
		}
	}
	return l
}
