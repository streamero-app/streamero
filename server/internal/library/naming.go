package library

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".wmv": true, ".ts": true,
	".m2ts": true, ".mts": true, ".webm": true, ".mpg": true, ".mpeg": true, ".flv": true, ".ogv": true,
}

var subtitleExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".vtt": true, ".sub": true, ".sup": true}

// IsVideo reports whether the file name has a video extension.
func IsVideo(name string) bool { return videoExts[strings.ToLower(filepath.Ext(name))] }

// IsSubtitle reports whether the file name has a subtitle extension.
func IsSubtitle(name string) bool { return subtitleExts[strings.ToLower(filepath.Ext(name))] }

// Folders holding extras; skipped when building items (extras come later in P2).
var extraDirs = map[string]bool{
	"trailers": true, "extras": true, "featurettes": true, "behind the scenes": true, "deleted scenes": true,
	"interviews": true, "scenes": true, "shorts": true, "other": true, "samples": true, "sample": true,
	"clips": true, "backdrops": true, "theme-music": true,
}

var extraSuffix = regexp.MustCompile(`(?i)[-._ ](trailer|sample|featurette|behindthescenes|deleted|deletedscene|interview|scene|short|other|clip)\d*$`)

// IgnoredName reports names that are never scanned (hidden files, NAS metadata, recycle bins).
func IgnoredName(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || strings.HasPrefix(name, "#") {
		return true
	}
	switch strings.ToLower(name) {
	case "$recycle.bin", "lost+found", "system volume information":
		return true
	}
	return false
}

// IsExtraDir reports whether a folder holds extras rather than the main feature.
func IsExtraDir(name string) bool { return extraDirs[strings.ToLower(name)] }

// IsExtraFile reports trailers/samples named like "Movie-trailer.mkv" or "sample.mkv".
func IsExtraFile(name string) bool {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return extraSuffix.MatchString(base) || strings.EqualFold(base, "sample") || strings.EqualFold(base, "trailer")
}

// Parsed is what a file or folder name says about a movie or series.
type Parsed struct {
	Title       string
	Year        int
	Label       string            // version label, e.g. "2160p" or "Director's Cut"
	ProviderIDs map[string]string // tmdb, imdb, tvdb
}

var (
	providerRe = regexp.MustCompile(`(?i)[\[{(]\s*(tmdb|tvdb|imdb)(?:id)?\s*[-=: ]\s*(tt\d+|\d+)\s*[\]})]`)
	bareImdbRe = regexp.MustCompile(`\btt\d{7,8}\b`)
	parenYear  = regexp.MustCompile(`\((19\d{2}|20\d{2})\)`)
	tokenYear  = regexp.MustCompile(`^(19\d{2}|20\d{2})$`)
	bracketed  = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	spaces     = regexp.MustCompile(`\s+`)
)

// Release tags that end the title in scene-style names.
var releaseTags = map[string]bool{
	"480p": true, "576p": true, "720p": true, "1080p": true, "1080i": true, "2160p": true, "4k": true, "uhd": true,
	"bluray": true, "blu-ray": true, "bdrip": true, "brrip": true, "bdremux": true, "remux": true, "web": true,
	"web-dl": true, "webdl": true, "webrip": true, "hdtv": true, "dvdrip": true, "dvd": true, "hdrip": true,
	"x264": true, "x265": true, "h264": true, "h265": true, "h.264": true, "h.265": true, "hevc": true, "avc": true,
	"av1": true, "xvid": true, "divx": true, "10bit": true, "8bit": true, "hdr": true, "hdr10": true, "hdr10+": true,
	"dv": true, "dovi": true, "sdr": true, "atmos": true, "dts": true, "dts-hd": true, "truehd": true, "aac": true,
	"ac3": true, "eac3": true, "ddp": true, "ddp5.1": true, "dd5.1": true, "flac": true, "proper": true,
	"repack": true, "extended": true, "unrated": true, "remastered": true, "imax": true, "internal": true,
	"limited": true, "multi": true, "dubbed": true, "subbed": true, "hc": true, "amzn": true, "nf": true,
	"dsnp": true, "hmax": true, "atvp": true,
}

// ParseName parses a movie/series folder or file name (without extension).
func ParseName(name string) Parsed {
	p := Parsed{ProviderIDs: map[string]string{}}
	for _, m := range providerRe.FindAllStringSubmatch(name, -1) {
		p.ProviderIDs[strings.ToLower(m[1])] = m[2]
	}
	if _, ok := p.ProviderIDs["imdb"]; !ok {
		if m := bareImdbRe.FindString(name); m != "" {
			p.ProviderIDs["imdb"] = m
		}
	}
	name = providerRe.ReplaceAllString(name, " ")
	name = bareImdbRe.ReplaceAllString(name, " ")

	// "Title (Year) - Label" style.
	if loc := parenYear.FindStringSubmatchIndex(name); loc != nil {
		p.Year, _ = strconv.Atoi(name[loc[2]:loc[3]])
		rest := strings.TrimSpace(name[loc[1]:])
		if strings.HasPrefix(rest, "- ") || strings.HasPrefix(rest, "-") {
			p.Label = cleanTitle(strings.TrimLeft(rest, "- "))
		}
		p.Title = cleanTitle(name[:loc[0]])
		return p
	}

	// Scene style: "Title.Year.1080p.BluRay" / "Title 2019 WEB-DL".
	clean := bracketed.ReplaceAllString(name, " ")
	if !strings.Contains(strings.TrimSpace(clean), " ") {
		clean = strings.NewReplacer(".", " ", "_", " ").Replace(clean)
	} else {
		clean = strings.ReplaceAll(clean, "_", " ")
	}
	tokens := strings.Fields(clean)
	cut := len(tokens)
	for i, tok := range tokens {
		low := strings.ToLower(strings.Trim(tok, "()[]-"))
		if releaseTags[low] {
			cut = i
			break
		}
	}
	// The year is the last year-looking token before the tags (titles like "1917 (2019)" or "2001 A Space Odyssey 1968").
	for i := cut - 1; i > 0; i-- {
		if tokenYear.MatchString(strings.Trim(tokens[i], "()")) {
			p.Year, _ = strconv.Atoi(strings.Trim(tokens[i], "()"))
			cut = i
			break
		}
	}
	// " - Label" without a year: "Movie - Director's Cut".
	title := strings.Join(tokens[:cut], " ")
	if idx := strings.LastIndex(title, " - "); idx > 0 && p.Year == 0 {
		p.Label = cleanTitle(title[idx+3:])
		title = title[:idx]
	}
	p.Title = cleanTitle(title)
	return p
}

func cleanTitle(s string) string {
	s = strings.Trim(strings.TrimSpace(spaces.ReplaceAllString(s, " ")), "-–._ ")
	return s
}

// SortTitle lowercases and drops leading articles ("The Matrix" sorts as "matrix").
func SortTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(t, a) && len(t) > len(a) {
			return t[len(a):]
		}
	}
	return t
}

// Episode is what an episode file name says.
type Episode struct {
	Season     int // -1 when unknown (use the folder)
	Episode    int
	EpisodeEnd int  // multi-episode files, 0 otherwise
	Daily      bool // aired date instead of numbers (Season = year, Episode = MMDD)
	Absolute   bool // anime absolute numbering
	Title      string
}

var (
	sxxexx    = regexp.MustCompile(`(?i)\bS(\d{1,4})[ ._-]?E(\d{1,4})(?:(?:[ ._-]?-?[ ._-]?E|-)(\d{1,4}))?`)
	nxnn      = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{2,3})(?:-(?:\d{1,2}x)?(\d{2,3}))?\b`)
	dailyRe   = regexp.MustCompile(`\b(19\d{2}|20\d{2})[-. ](\d{2})[-. ](\d{2})\b`)
	absRe     = regexp.MustCompile(`(?i)(?:^|[ ._-])(?:-\s*|ep?\.?\s*|#)(\d{2,4})(?:v\d)?(?:[ ._\[(]|$)`)
	seasonDir = regexp.MustCompile(`(?i)^(?:season|series|staffel|sezona|saison|temporada)[ ._-]*(\d{1,4})$|^s(\d{1,4})$`)
)

// ParseEpisode parses an episode file name (without extension).
func ParseEpisode(name string) (Episode, bool) {
	if m := sxxexx.FindStringSubmatch(name); m != nil {
		e := Episode{}
		e.Season, _ = strconv.Atoi(m[1])
		e.Episode, _ = strconv.Atoi(m[2])
		if m[3] != "" {
			e.EpisodeEnd, _ = strconv.Atoi(m[3])
		}
		return e, true
	}
	if m := dailyRe.FindStringSubmatch(name); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			return Episode{Season: y, Episode: mo*100 + d, Daily: true, Title: m[1] + "-" + m[2] + "-" + m[3]}, true
		}
	}
	if m := nxnn.FindStringSubmatch(name); m != nil {
		e := Episode{}
		e.Season, _ = strconv.Atoi(m[1])
		e.Episode, _ = strconv.Atoi(m[2])
		if m[3] != "" {
			e.EpisodeEnd, _ = strconv.Atoi(m[3])
		}
		return e, true
	}
	// Anime: "[Group] Show - 101 [1080p]". Strip bracketed tags and resolutions first.
	stripped := bracketed.ReplaceAllString(name, " ")
	stripped = regexp.MustCompile(`(?i)\b\d{3,4}p\b`).ReplaceAllString(stripped, " ")
	if m := absRe.FindStringSubmatch(stripped); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n > 0 && (n < 1900 || n > 2099) {
			return Episode{Season: -1, Episode: n, Absolute: true}, true
		}
	}
	return Episode{}, false
}

// SeasonFromDir returns the season number of a folder like "Season 02" or "Specials".
func SeasonFromDir(name string) (int, bool) {
	if strings.EqualFold(name, "specials") {
		return 0, true
	}
	if m := seasonDir.FindStringSubmatch(strings.TrimSpace(name)); m != nil {
		v := m[1]
		if v == "" {
			v = m[2]
		}
		n, _ := strconv.Atoi(v)
		return n, true
	}
	return 0, false
}
