package library

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true, ".oga": true, ".opus": true,
	".wav": true, ".wma": true, ".alac": true, ".aiff": true, ".aif": true, ".ape": true, ".wv": true, ".mka": true,
}

// IsAudio reports whether a file name looks like a music track.
func IsAudio(name string) bool { return audioExts[strings.ToLower(filepath.Ext(name))] }

// Music keys: artists and albums are grouped by name, case-insensitively.
func musicKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) }), " ")
}

// leadingInt reads "3", "03" or "3/12".
func leadingInt(s string) *int {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 || n > 9999 {
		return nil
	}
	return &n
}

func tagYear(tags map[string]string) int {
	for _, k := range []string{"date", "year", "originaldate", "tdrc", "tyer"} {
		if v := tags[k]; len(v) >= 4 {
			if y, err := strconv.Atoi(v[:4]); err == nil && y > 1800 && y < 3000 {
				return y
			}
		}
	}
	return 0
}

// trackFromName guesses "03 - Title" style names when tags are missing.
func trackFromName(stem string) (*int, string) {
	i := 0
	for i < len(stem) && stem[i] >= '0' && stem[i] <= '9' {
		i++
	}
	if i > 0 && i <= 3 {
		rest := strings.TrimLeft(stem[i:], " .-_")
		if rest != "" {
			return leadingInt(stem[:i]), rest
		}
	}
	return nil, stem
}

type musicTrack struct {
	file                planFile
	artist, albumArtist string
	album, title, genre string
	year                int
	track, disc         *int
}

// readTrack turns a file and its tags into a track, falling back to the
// Artist/Album/NN - Title folder layout for anything the tags do not say.
func readTrack(f planFile, tags map[string]string) musicTrack {
	dir := filepath.Dir(f.path)
	stem := strings.TrimSuffix(filepath.Base(f.path), filepath.Ext(f.path))
	t := musicTrack{file: f, title: tags["title"], album: tags["album"], artist: tags["artist"], genre: tags["genre"]}
	t.albumArtist = tags["album_artist"]
	if t.albumArtist == "" {
		t.albumArtist = tags["albumartist"]
	}
	t.track, t.disc = leadingInt(tags["track"]), leadingInt(tags["disc"])
	if t.disc == nil {
		t.disc = leadingInt(tags["discnumber"])
	}
	if t.track == nil {
		t.track = leadingInt(tags["tracknumber"])
	}
	t.year = tagYear(tags)
	if t.title == "" || t.track == nil {
		n, rest := trackFromName(stem)
		if t.track == nil {
			t.track = n
		}
		if t.title == "" {
			t.title = rest
		}
	}
	albumDir := dir
	if d := strings.ToLower(filepath.Base(dir)); strings.HasPrefix(d, "cd") || strings.HasPrefix(d, "disc") {
		albumDir = filepath.Dir(dir) // Album/CD1/track.flac
		if t.disc == nil {
			t.disc = leadingInt(strings.TrimLeft(d, "cdisk "))
		}
	}
	if t.album == "" {
		t.album = filepath.Base(albumDir)
	}
	if t.albumArtist == "" {
		t.albumArtist = t.artist
	}
	if t.albumArtist == "" {
		t.albumArtist = filepath.Base(filepath.Dir(albumDir))
	}
	if t.artist == "" {
		t.artist = t.albumArtist
	}
	return t
}

// collectAudio lists the audio files under root.
func collectAudio(ctx context.Context, root string) ([]planFile, error) {
	var out []planFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (IgnoredName(name) || hasIgnoreMarker(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if IgnoredName(name) || !d.Type().IsRegular() || !IsAudio(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, planFile{path: path, size: info.Size(), mtime: info.ModTime().UTC().Truncate(time.Microsecond)})
		return nil
	})
	return out, err
}

// planMusic groups audio files into artists, albums and tracks. Tags of new or
// changed files are read first (and handed to apply, which then does not probe
// them again); unchanged files use the tags stored at their last scan.
func (s *Scanner) planMusic(ctx context.Context, libID string, roots []string) (map[string]*planItem, map[string]probeOutcome, map[string][]string, error) {
	var files []planFile
	for _, root := range roots {
		fs, err := collectAudio(ctx, root)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("reading %s: %w", root, err)
		}
		files = append(files, fs...)
	}
	known := map[string]struct {
		size  int64
		mtime time.Time
		tags  map[string]string
	}{}
	rows, err := s.Pool.Query(ctx, `SELECT path, size, mtime, tags FROM media_files WHERE library_id = $1`, libID)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var path string
		var size int64
		var mtime time.Time
		var raw []byte
		if err := rows.Scan(&path, &size, &mtime, &raw); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		var tags map[string]string
		_ = json.Unmarshal(raw, &tags)
		known[path] = struct {
			size  int64
			mtime time.Time
			tags  map[string]string
		}{size, mtime, tags}
	}
	rows.Close()
	var toProbe []planFile
	for _, f := range files {
		if k, ok := known[f.path]; !ok || k.size != f.size || !k.mtime.Equal(f.mtime) {
			toProbe = append(toProbe, f)
		}
	}
	probed := s.probeAll(ctx, toProbe)

	plan := map[string]*planItem{}
	genres := map[string][]string{} // album key → genres
	for _, f := range files {
		tags := known[f.path].tags
		if p, ok := probed[f.path]; ok {
			tags = p.res.Tags
		}
		t := readTrack(f, tags)
		artistKey := "artist:" + musicKey(t.albumArtist)
		albumKey := "album:" + musicKey(t.albumArtist) + "/" + musicKey(t.album)
		if _, ok := plan[artistKey]; !ok {
			plan[artistKey] = &planItem{key: artistKey, kind: "artist", title: t.albumArtist, order: 0, ids: map[string]string{}}
		}
		al, ok := plan[albumKey]
		if !ok {
			al = &planItem{key: albumKey, kind: "album", parentKey: artistKey, title: t.album, order: 1, ids: map[string]string{}}
			plan[albumKey] = al
		}
		if t.year > 0 && (al.year == 0 || t.year < al.year) {
			al.year = t.year
		}
		if t.genre != "" {
			for _, g := range strings.FieldsFunc(t.genre, func(r rune) bool { return r == ';' || r == '/' || r == ',' }) {
				if g = strings.TrimSpace(g); g != "" && !containsFold(genres[albumKey], g) {
					genres[albumKey] = append(genres[albumKey], g)
				}
			}
		}
		tr := &planItem{key: f.path, kind: "track", parentKey: albumKey, title: t.title, year: t.year, season: t.disc, ep: t.track,
			order: 2, ids: map[string]string{}, files: []planFile{f}}
		if !strings.EqualFold(t.artist, t.albumArtist) {
			tr.artist = t.artist
		}
		plan[f.path] = tr
	}
	return plan, probed, genres, nil
}

func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}
