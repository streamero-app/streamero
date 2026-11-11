package library

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/books"
	"github.com/streamero-app/streamero/server/internal/exif"
)

// streamingKinds are the item kinds offered to Jellyfin-style apps (compat API)
// and DLNA televisions. Books, audiobooks and photos are deliberately left out
// there: those clients cannot read books, and photos/audiobooks would need
// their own client support (D-039).
var streamingKinds = map[string]bool{"movie": true, "series": true, "season": true, "episode": true,
	"artist": true, "album": true, "track": true}

// StreamingKindsSQL is the same list as an SQL condition on items i.
const StreamingKindsSQL = "i.kind IN ('movie', 'series', 'season', 'episode', 'artist', 'album', 'track')"

// IsStreamingKind reports an item kind the compat API and DLNA serve.
func IsStreamingKind(kind string) bool { return streamingKinds[kind] }

// IsStreamingLibrary reports a library kind the compat API and DLNA serve.
func IsStreamingLibrary(kind string) bool {
	return kind == "movies" || kind == "shows" || kind == "music" || kind == "mixed"
}

// MediaMeta is what a book, comic, audiobook or photo file says about itself
// (items.media_meta). The scanner owns it and rewrites it when the file changes.
type MediaMeta struct {
	books.Info
	Narrators []string   `json:"narrators,omitempty"`
	Photo     *exif.Info `json:"photo,omitempty"`
	// Error is why the file could not be read (it is still listed).
	Error string `json:"error,omitempty"`
}

func metaEqual(raw []byte, m *MediaMeta) bool {
	if m == nil || raw == nil {
		return m == nil && raw == nil
	}
	var a, b MediaMeta
	planned, _ := json.Marshal(m)
	if json.Unmarshal(planned, &a) != nil || json.Unmarshal(raw, &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

func timePtrEq(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}

var photoExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".heic": true, ".heif": true, ".webp": true,
	".avif": true, ".gif": true}

// IsPhoto reports a picture a photo library shows.
func IsPhoto(name string) bool { return photoExts[strings.ToLower(filepath.Ext(name))] }

// known is a file as stored at the last scan.
type known struct {
	size  int64
	mtime time.Time
	tags  map[string]string
}

func (k known) same(f planFile) bool { return k.size == f.size && k.mtime.Equal(f.mtime) }

// knownFiles loads the files of a library with their stored tags.
func (s *Scanner) knownFiles(ctx context.Context, libID uuid.UUID) (map[string]known, error) {
	out := map[string]known{}
	rows, err := s.Pool.Query(ctx, `SELECT path, size, mtime, tags FROM media_files WHERE library_id = $1`, libID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var k known
		var raw []byte
		if err := rows.Scan(&p, &k.size, &k.mtime, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &k.tags)
		out[p] = k
	}
	return out, rows.Err()
}

// knownMeta loads the stored media_meta of a library's items by path.
func (s *Scanner) knownMeta(ctx context.Context, libID uuid.UUID) (map[string]*MediaMeta, error) {
	out := map[string]*MediaMeta{}
	rows, err := s.Pool.Query(ctx, `SELECT path, media_meta FROM items WHERE library_id = $1 AND media_meta IS NOT NULL`, libID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var raw []byte
		if err := rows.Scan(&p, &raw); err != nil {
			return nil, err
		}
		m := &MediaMeta{}
		if json.Unmarshal(raw, m) == nil {
			out[p] = m
		}
	}
	return out, rows.Err()
}

// walkMedia lists the regular files under root that match keep (ignored names,
// ignore markers and symlinks are skipped, as everywhere else).
func walkMedia(ctx context.Context, root string, keep func(name string) bool) ([]planFile, error) {
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
		if IgnoredName(name) || !d.Type().IsRegular() || !keep(name) {
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

func stem(p string) string { return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) }

// depth is how many folders lie between root and p's folder (0 = directly in root).
func depth(root, p string) int {
	rel, err := filepath.Rel(root, filepath.Dir(p))
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// --- books -------------------------------------------------------------------------------

// planBooks makes one item per book or comic file. New and changed files are read
// (OPF, ComicInfo.xml, PDF, MOBI headers); unchanged files keep what was read before.
func (s *Scanner) planBooks(ctx context.Context, libID uuid.UUID, roots []string) (map[string]*planItem, map[string]probeOutcome, error) {
	knownF, err := s.knownFiles(ctx, libID)
	if err != nil {
		return nil, nil, err
	}
	knownM, err := s.knownMeta(ctx, libID)
	if err != nil {
		return nil, nil, err
	}
	plan := map[string]*planItem{}
	probed := map[string]probeOutcome{}
	for _, root := range roots {
		files, err := walkMedia(ctx, root, func(n string) bool { return books.FormatOf(n) != "" })
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", root, err)
		}
		for _, f := range files {
			format := books.FormatOf(f.path)
			meta := knownM[f.path]
			if k, ok := knownF[f.path]; !ok || !k.same(f) || meta == nil {
				in, err := books.Read(f.path)
				meta = &MediaMeta{Info: in}
				if err != nil {
					meta.Error = err.Error()
					s.Log.Warn("book metadata", "path", f.path, "err", err)
				}
				meta.Format = format
				probed[f.path] = probeOutcome{res: ProbeResult{Container: format}}
			}
			plan[f.path] = bookItem(root, f, meta)
		}
	}
	return plan, probed, nil
}

// bookItem fills in what the file did not say from its name and folders.
func bookItem(root string, f planFile, m *MediaMeta) *planItem {
	m = cloneMeta(m)
	name := books.ParseFileName(stem(f.path))
	parent := filepath.Base(filepath.Dir(f.path))
	atRoot := depth(root, f.path) == 0
	if books.IsComic(m.Format) {
		if m.Series == "" {
			if !atRoot {
				m.Series = books.ParseFileName(parent).Series
			} else if name.Volume != nil || name.Chapter != nil {
				m.Series = name.Series
			}
		}
		if m.Volume == nil {
			m.Volume = name.Volume
		}
		if m.Chapter == nil {
			m.Chapter = name.Chapter
		}
		if m.SeriesIndex == nil {
			m.SeriesIndex = m.Chapter
			if m.SeriesIndex == nil {
				m.SeriesIndex = m.Volume
			}
		}
	} else if len(m.Authors) == 0 && !atRoot {
		// Author/Book.epub or Author/Series/Book.epub.
		rel, _ := filepath.Rel(root, f.path)
		m.Authors = []string{strings.SplitN(rel, string(filepath.Separator), 2)[0]}
	}
	if m.Year == 0 {
		m.Year = name.Year
	}
	title := m.Title
	if title == "" {
		title = strings.TrimSpace(name.Title)
		if title == "" {
			title = stem(f.path)
		}
	}
	return &planItem{key: f.path, kind: "book", title: title, year: m.Year, ids: isbnIDs(m.ISBN), meta: m, files: []planFile{f}}
}

func isbnIDs(isbn string) map[string]string {
	if isbn == "" {
		return map[string]string{}
	}
	return map[string]string{"isbn": isbn}
}

func cloneMeta(m *MediaMeta) *MediaMeta {
	c := *m
	c.Authors = append([]string(nil), m.Authors...)
	c.Genres = append([]string(nil), m.Genres...)
	return &c
}

// --- audiobooks ----------------------------------------------------------------------

// IsAudiobookPart reports an audio file that can be (part of) an audiobook.
func IsAudiobookPart(name string) bool {
	return IsAudio(name) || strings.EqualFold(filepath.Ext(name), ".m4b")
}

func isDiscDir(name string) bool {
	d := strings.ToLower(name)
	return (strings.HasPrefix(d, "cd") || strings.HasPrefix(d, "disc") || strings.HasPrefix(d, "disk") || strings.HasPrefix(d, "part")) &&
		strings.IndexFunc(d, func(r rune) bool { return r >= '0' && r <= '9' }) > 0
}

// AudiobookPartOrder sorts the files of a multi-file audiobook: disc, track, then name.
func AudiobookPartOrder(paths []string, tags func(string) map[string]string) {
	key := func(p string) (int, int) {
		t := tags(p)
		disc, track := 0, 1<<30
		if n := leadingInt(t["disc"]); n != nil {
			disc = *n
		} else if d := filepath.Base(filepath.Dir(p)); isDiscDir(d) {
			if n := leadingInt(strings.TrimLeft(strings.ToLower(d), "cdiskpart _-")); n != nil {
				disc = *n
			}
		}
		if n := leadingInt(t["track"]); n != nil {
			track = *n
		}
		return disc, track
	}
	sort.SliceStable(paths, func(i, j int) bool {
		di, ti := key(paths[i])
		dj, tj := key(paths[j])
		if di != dj {
			return di < dj
		}
		if ti != tj {
			return ti < tj
		}
		return books.NaturalLess(paths[i], paths[j])
	})
}

// planAudiobooks groups audio files into books: every M4B file is a book; the
// other audio files of one folder (and its CD1/CD2 sub-folders) form one book.
func (s *Scanner) planAudiobooks(ctx context.Context, libID uuid.UUID, roots []string) (map[string]*planItem, map[string]probeOutcome, error) {
	knownF, err := s.knownFiles(ctx, libID)
	if err != nil {
		return nil, nil, err
	}
	type group struct {
		root, key string
		files     []planFile
	}
	groups := map[string]*group{}
	var all []planFile
	for _, root := range roots {
		files, err := walkMedia(ctx, root, IsAudiobookPart)
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", root, err)
		}
		for _, f := range files {
			all = append(all, f)
			key := filepath.Dir(f.path)
			if isDiscDir(filepath.Base(key)) && key != root {
				key = filepath.Dir(key)
			}
			if strings.EqualFold(filepath.Ext(f.path), ".m4b") || key == root {
				key = f.path // a single-file book
			}
			g := groups[key]
			if g == nil {
				g = &group{root: root, key: key}
				groups[key] = g
			}
			g.files = append(g.files, f)
		}
	}
	var toProbe []planFile
	for _, f := range all {
		if k, ok := knownF[f.path]; !ok || !k.same(f) {
			toProbe = append(toProbe, f)
		}
	}
	probed := s.probeAll(ctx, toProbe)
	tagsOf := func(p string) map[string]string {
		if o, ok := probed[p]; ok {
			return o.res.Tags
		}
		return knownF[p].tags
	}
	plan := map[string]*planItem{}
	for _, g := range groups {
		paths := make([]string, len(g.files))
		byPath := map[string]planFile{}
		for i, f := range g.files {
			paths[i] = f.path
			byPath[f.path] = f
		}
		AudiobookPartOrder(paths, tagsOf)
		files := make([]planFile, len(paths))
		for i, p := range paths {
			files[i] = byPath[p]
		}
		single := len(files) == 1 && g.key == files[0].path
		m := audiobookMeta(g.root, g.key, single, tagsOf(paths[0]))
		plan[g.key] = &planItem{key: g.key, kind: "audiobook", title: m.Title, year: m.Year, ids: isbnIDs(m.ISBN), meta: m, files: files}
	}
	return plan, probed, nil
}

func firstTag(t map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(t[k]); v != "" {
			return v
		}
	}
	return ""
}

func splitNames(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == '/' || r == '&' }) {
		if p = strings.TrimSpace(p); p != "" && !containsFold(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// audiobookMeta reads title, author, narrator and series from the tags, falling
// back to the Author/Book Title/ folder layout.
func audiobookMeta(root, key string, single bool, t map[string]string) *MediaMeta {
	m := &MediaMeta{}
	m.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(key)), ".")
	if !single {
		m.Format = "folder"
	}
	if single {
		m.Title = firstTag(t, "album", "title")
	} else {
		m.Title = firstTag(t, "album")
	}
	dir := key
	if single {
		dir = filepath.Dir(key)
		if m.Title == "" {
			m.Title = books.ParseFileName(stem(key)).Title
		}
	}
	if m.Title == "" {
		m.Title = filepath.Base(dir)
	}
	m.Authors = splitNames(firstTag(t, "author", "album_artist", "albumartist", "artist"))
	if len(m.Authors) == 0 {
		// Author/Book/ (folder book) or Author/Book.m4b (single file).
		authorDir := filepath.Dir(dir)
		if single {
			authorDir = dir
		}
		if authorDir != root && strings.HasPrefix(authorDir, root) {
			if rel, err := filepath.Rel(root, authorDir); err == nil {
				m.Authors = []string{strings.SplitN(rel, string(filepath.Separator), 2)[0]}
			}
		}
	}
	m.Narrators = splitNames(firstTag(t, "narrator", "narratedby", "composer", "performer"))
	m.Series = firstTag(t, "series", "mvnm", "movementname", "show")
	if m.Series != "" {
		idx := firstTag(t, "series-part", "seriespart", "mvin", "movement", "episode_sort")
		if f, err := strconv.ParseFloat(strings.ReplaceAll(idx, ",", "."), 64); err == nil && f >= 0 {
			m.SeriesIndex = &f
		}
	}
	m.Description = books.StripHTML(firstTag(t, "description", "synopsis", "comment", "desc"))
	m.Publisher = firstTag(t, "publisher", "label")
	m.ISBN = books.NormalizeISBN(firstTag(t, "isbn"))
	m.Language = firstTag(t, "language")
	m.Year = tagYear(t)
	m.Genres = append(m.Genres, splitNames(strings.ReplaceAll(t["genre"], ",", ";"))...)
	return m
}

// --- photos --------------------------------------------------------------------------

// planPhotos makes an album per folder that holds pictures or videos (nested
// folders are nested albums), and an item per picture and video.
func (s *Scanner) planPhotos(ctx context.Context, libID uuid.UUID, roots []string) (map[string]*planItem, map[string]probeOutcome, error) {
	knownF, err := s.knownFiles(ctx, libID)
	if err != nil {
		return nil, nil, err
	}
	knownM, err := s.knownMeta(ctx, libID)
	if err != nil {
		return nil, nil, err
	}
	plan := map[string]*planItem{}
	probed := map[string]probeOutcome{}
	var videos []planFile
	type entry struct {
		root string
		f    planFile
	}
	var entries []entry
	for _, root := range roots {
		files, err := walkMedia(ctx, root, func(n string) bool { return IsPhoto(n) || IsVideo(n) })
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", root, err)
		}
		for _, f := range files {
			entries = append(entries, entry{root, f})
			if k, ok := knownF[f.path]; IsVideo(f.path) && (!ok || !k.same(f)) {
				videos = append(videos, f)
			}
		}
	}
	for p, o := range s.probeAll(ctx, videos) {
		probed[p] = o
	}
	for _, e := range entries {
		f := e.f
		var it *planItem
		if IsVideo(f.path) {
			tags := knownF[f.path].tags
			if o, ok := probed[f.path]; ok {
				tags = o.res.Tags
			}
			taken := f.mtime
			if ts, err := time.Parse(time.RFC3339Nano, tags["creation_time"]); err == nil && ts.Year() > 1970 {
				taken = ts.UTC().Truncate(time.Second)
			}
			it = &planItem{key: f.path, kind: "video", title: stem(f.path), ids: map[string]string{}, files: []planFile{f},
				takenAt: &taken, order: 1000}
		} else {
			meta := knownM[f.path]
			if k, ok := knownF[f.path]; !ok || !k.same(f) || meta == nil {
				info, err := exif.ReadFile(f.path)
				meta = &MediaMeta{Photo: &info}
				if err != nil {
					meta.Error = err.Error()
				}
				meta.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(f.path)), ".")
				w, h := info.Width, info.Height
				if info.Orientation >= 5 { // rotated 90°: stored sideways
					w, h = h, w
				}
				probed[f.path] = probeOutcome{res: ProbeResult{Container: meta.Format, Width: w, Height: h}}
			}
			taken := f.mtime
			if meta.Photo != nil && meta.Photo.TakenAt != nil {
				taken = *meta.Photo.TakenAt
			}
			it = &planItem{key: f.path, kind: "photo", title: stem(f.path), ids: map[string]string{}, files: []planFile{f},
				meta: meta, takenAt: &taken, order: 1000}
		}
		if d := filepath.Dir(f.path); d != e.root {
			it.parentKey = d
			addAlbums(plan, e.root, d, *it.takenAt)
		}
		plan[f.path] = it
	}
	return plan, probed, nil
}

// addAlbums makes dir and every folder above it (below root) an album; an album's
// date is its newest picture's.
func addAlbums(plan map[string]*planItem, root, dir string, taken time.Time) {
	for d := dir; d != root && strings.HasPrefix(d, root); d = filepath.Dir(d) {
		al := plan[d]
		if al == nil {
			al = &planItem{key: d, kind: "photoalbum", title: filepath.Base(d), ids: map[string]string{}, order: depth(root, d+"/x")}
			if p := filepath.Dir(d); p != root {
				al.parentKey = p
			}
			plan[d] = al
		}
		if al.takenAt == nil || taken.After(*al.takenAt) {
			t := taken
			al.takenAt = &t
		}
	}
}
