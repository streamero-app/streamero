package dlna

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// Object ids:
//
//	0                     root: one folder per library, plus "Continue watching"
//	l:<lib>               library: its sub-folders
//	l:<lib>:az|new|genres|artists|albums
//	l:<lib>:g:<hex genre> titles of one genre
//	cw                    continue watching (resumes where the viewer stopped)
//	cw:<item>             an item in continue watching
//	i:<item>              any library item (series, season, artist, album, movie, episode, track)
type node struct {
	kind  string // root, lib, sub, genre, cw, cwitem, item
	lib   uuid.UUID
	sub   string
	genre string
	item  uuid.UUID
}

var errNoObject = &soapError{Code: errNoSuchObject, Desc: "No such object"}

func parseID(id string) (node, bool) {
	switch {
	case id == "0":
		return node{kind: "root"}, true
	case id == "cw":
		return node{kind: "cw"}, true
	case strings.HasPrefix(id, "cw:"):
		u, err := uuid.Parse(id[3:])
		return node{kind: "cwitem", item: u}, err == nil
	case strings.HasPrefix(id, "i:"):
		u, err := uuid.Parse(id[2:])
		return node{kind: "item", item: u}, err == nil
	case strings.HasPrefix(id, "l:"):
		parts := strings.SplitN(id[2:], ":", 3)
		u, err := uuid.Parse(parts[0])
		if err != nil {
			return node{}, false
		}
		switch {
		case len(parts) == 1:
			return node{kind: "lib", lib: u}, true
		case len(parts) == 3 && parts[1] == "g":
			g, err := hex.DecodeString(parts[2])
			return node{kind: "genre", lib: u, genre: string(g)}, err == nil
		case len(parts) == 2 && slices.Contains([]string{"az", "new", "genres", "artists", "albums"}, parts[1]):
			return node{kind: "sub", lib: u, sub: parts[1]}, true
		}
	}
	return node{}, false
}

// subsOf lists the folders inside a library, by kind.
func subsOf(kind string) []string {
	switch kind {
	case "music":
		return []string{"artists", "albums", "new"}
	case "mixed":
		return []string{"az", "new"}
	}
	return []string{"az", "new", "genres"}
}

// request carries what a Browse needs to build objects.
type request struct {
	v    *Viewer
	prof Profile
	base string
}

// recentLimit caps "Recently added".
const recentLimit = 100

// sortFor maps a SortCriteria to the library's sort keys ("" = container default).
func sortFor(criteria string) string {
	for _, c := range strings.Split(criteria, ",") {
		switch strings.TrimLeft(strings.TrimSpace(c), "+-") {
		case "dc:title":
			return "title"
		case "dc:date":
			return "year"
		}
	}
	return ""
}

// Browse answers BrowseMetadata / BrowseDirectChildren.
func (s *Server) Browse(ctx context.Context, q request, id string, children bool, start, count int, sortCriteria string) ([]Object, int, error) {
	n, ok := parseID(id)
	if !ok {
		return nil, 0, errNoObject
	}
	if !children {
		o, err := s.metadata(ctx, q, n, id)
		if err != nil {
			return nil, 0, err
		}
		return []Object{o}, 1, nil
	}
	return s.children(ctx, q, n, id, start, count, sortFor(sortCriteria))
}

func page[T any](all []T, start, count int) []T {
	if start >= len(all) {
		return nil
	}
	end := len(all)
	if count > 0 && start+count < end {
		end = start + count
	}
	return all[start:end]
}

func (s *Server) library(ctx context.Context, q request, id uuid.UUID) (library.Library, error) {
	if !q.v.CanSeeLibraries() {
		return library.Library{}, errNoObject
	}
	l, err := s.Library.Get(ctx, id)
	if errors.Is(err, library.ErrNotFound) {
		return l, errNoObject
	}
	return l, err
}

func (s *Server) metadata(ctx context.Context, q request, n node, id string) (Object, error) {
	lang := q.v.Lang
	switch n.kind {
	case "root":
		kids, total, err := s.children(ctx, q, n, id, 0, 0, "")
		_ = kids
		return Object{ID: "0", ParentID: "-1", Title: s.FriendlyName(ctx), Class: classFolder, Container: true, ChildCount: total}, err
	case "cw":
		items, err := s.continueWatching(ctx, q)
		return Object{ID: "cw", ParentID: "0", Title: folderName(lang, "continue"), Class: classFolder, Container: true, ChildCount: len(items)}, err
	case "lib":
		l, err := s.library(ctx, q, n.lib)
		if err != nil {
			return Object{}, err
		}
		return Object{ID: id, ParentID: "0", Title: l.Name, Class: classFolder, Container: true, ChildCount: len(subsOf(l.Kind))}, nil
	case "sub":
		l, err := s.library(ctx, q, n.lib)
		if err != nil {
			return Object{}, err
		}
		return s.subFolder(ctx, q, l, n.sub), nil
	case "genre":
		if _, err := s.library(ctx, q, n.lib); err != nil {
			return Object{}, err
		}
		return Object{ID: id, ParentID: "l:" + n.lib.String() + ":genres", Title: n.genre, Class: classGenre, Container: true, ChildCount: -1}, nil
	case "item", "cwitem":
		it, files, kids, err := s.item(ctx, q, n.item)
		if err != nil {
			return Object{}, err
		}
		parent := "l:" + it.LibraryID.String() + ":az"
		if it.ParentID != nil {
			parent = "i:" + it.ParentID.String()
		}
		if n.kind == "cwitem" {
			if err := s.AttachSeriesOne(ctx, &it); err != nil {
				return Object{}, err
			}
			return s.object(q, it, files, len(kids), "cw:", "cw", true), nil
		}
		return s.object(q, it, files, len(kids), "i:", parent, false), nil
	}
	return Object{}, errNoObject
}

// AttachSeriesOne fills the series title of an episode.
func (s *Server) AttachSeriesOne(ctx context.Context, it *library.Item) error {
	items := []library.Item{*it}
	if err := s.Library.AttachSeries(ctx, items); err != nil {
		return err
	}
	*it = items[0]
	return nil
}

func (s *Server) subFolder(ctx context.Context, q request, l library.Library, sub string) Object {
	o := Object{ID: "l:" + l.ID.String() + ":" + sub, ParentID: "l:" + l.ID.String(), Title: folderName(q.v.Lang, sub),
		Class: classFolder, Container: true, ChildCount: -1}
	f := s.filter(q)
	switch sub {
	case "artists":
		f.Kind = "artist"
	case "albums":
		f.Kind = "album"
	case "new":
		if l.Kind == "music" {
			f.Kind = "album"
		}
	case "genres":
		if g, err := s.Library.Genres(ctx, l.ID); err == nil {
			o.ChildCount = len(g)
		}
		return o
	}
	if _, total, err := s.Library.ListItems(ctx, l.ID, q.v.Owner, "title", 0, 0, f); err == nil {
		o.ChildCount = total
		if sub == "new" {
			o.ChildCount = min(total, recentLimit)
		}
	}
	return o
}

func (s *Server) filter(q request) library.ItemFilter {
	var f library.ItemFilter
	for r := range q.v.Blocked {
		f.ExcludeRatings = append(f.ExcludeRatings, r)
	}
	return f
}

func (s *Server) children(ctx context.Context, q request, n node, id string, start, count int, sortBy string) ([]Object, int, error) {
	switch n.kind {
	case "root":
		var out []Object
		if q.v.CanSeeLibraries() {
			libs, err := s.Library.List(ctx)
			if err != nil {
				return nil, 0, err
			}
			for _, l := range libs {
				out = append(out, Object{ID: "l:" + l.ID.String(), ParentID: "0", Title: l.Name, Class: classFolder,
					Container: true, ChildCount: len(subsOf(l.Kind))})
			}
			cw, err := s.continueWatching(ctx, q)
			if err != nil {
				return nil, 0, err
			}
			if len(cw) > 0 {
				out = append(out, Object{ID: "cw", ParentID: "0", Title: folderName(q.v.Lang, "continue"), Class: classFolder,
					Container: true, ChildCount: len(cw)})
			}
		}
		return page(out, start, count), len(out), nil
	case "lib":
		l, err := s.library(ctx, q, n.lib)
		if err != nil {
			return nil, 0, err
		}
		var out []Object
		for _, sub := range subsOf(l.Kind) {
			out = append(out, s.subFolder(ctx, q, l, sub))
		}
		return page(out, start, count), len(out), nil
	case "sub", "genre":
		l, err := s.library(ctx, q, n.lib)
		if err != nil {
			return nil, 0, err
		}
		f := s.filter(q)
		parent := id
		order := "title"
		switch n.sub {
		case "genres":
			gs, err := s.Library.Genres(ctx, l.ID)
			if err != nil {
				return nil, 0, err
			}
			out := make([]Object, 0, len(gs))
			for _, g := range gs {
				out = append(out, Object{ID: "l:" + l.ID.String() + ":g:" + hex.EncodeToString([]byte(g)), ParentID: id,
					Title: g, Class: classGenre, Container: true, ChildCount: -1})
			}
			return page(out, start, count), len(out), nil
		case "artists":
			f.Kind = "artist"
		case "albums":
			f.Kind = "album"
		case "new":
			order = "added"
			if l.Kind == "music" {
				f.Kind = "album"
			}
		}
		if n.kind == "genre" {
			f.Genre = n.genre
		}
		if sortBy != "" && n.sub != "new" {
			order = sortBy
		}
		limit := count
		if limit <= 0 {
			limit = 500
		}
		if n.sub == "new" {
			if start >= recentLimit {
				return nil, recentLimit, nil
			}
			limit = min(limit, recentLimit-start)
		}
		items, total, err := s.Library.ListItems(ctx, l.ID, q.v.Owner, order, min(limit, 500), start, f)
		if err != nil {
			return nil, 0, err
		}
		if n.sub == "new" {
			total = min(total, recentLimit)
		}
		objs, err := s.objects(ctx, q, items, "i:", parent, false)
		return objs, total, err
	case "cw":
		items, err := s.continueWatching(ctx, q)
		if err != nil {
			return nil, 0, err
		}
		objs, err := s.objects(ctx, q, page(items, start, count), "cw:", "cw", true)
		return objs, len(items), err
	case "item":
		it, _, kids, err := s.item(ctx, q, n.item)
		if err != nil {
			return nil, 0, err
		}
		if !containerKind(it.Kind) {
			return nil, 0, nil
		}
		objs, err := s.objects(ctx, q, page(kids, start, count), "i:", id, false)
		return objs, len(kids), err
	case "cwitem":
		return nil, 0, nil
	}
	return nil, 0, errNoObject
}

func containerKind(kind string) bool {
	switch kind {
	case "series", "season", "artist", "album":
		return true
	}
	return false
}

// item loads an item the viewer may see.
func (s *Server) item(ctx context.Context, q request, id uuid.UUID) (library.Item, []library.MediaFile, []library.Item, error) {
	if !q.v.CanSeeLibraries() {
		return library.Item{}, nil, nil, errNoObject
	}
	blocked, err := s.blockedAmong(ctx, q.v, []uuid.UUID{id})
	if err != nil {
		return library.Item{}, nil, nil, err
	}
	if blocked[id] {
		return library.Item{}, nil, nil, errNoObject
	}
	it, files, kids, err := s.Library.GetItem(ctx, id, q.v.Owner)
	if errors.Is(err, library.ErrNotFound) {
		return it, nil, nil, errNoObject
	}
	return it, files, kids, err
}

// blockedAmong returns the ids whose item or any ancestor carries a rating
// above the viewer's limit.
func (s *Server) blockedAmong(ctx context.Context, v *Viewer, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(v.Blocked) == 0 || len(ids) == 0 {
		return out, nil
	}
	blocked := make([]string, 0, len(v.Blocked))
	for r := range v.Blocked {
		blocked = append(blocked, r)
	}
	rows, err := s.Pool.Query(ctx, `WITH RECURSIVE up AS (
			SELECT id AS start, parent_id, official_rating FROM items WHERE id = ANY($1)
			UNION ALL SELECT up.start, i.parent_id, i.official_rating FROM items i JOIN up ON i.id = up.parent_id)
		SELECT DISTINCT start FROM up WHERE official_rating = ANY($2)`, ids, blocked)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *Server) continueWatching(ctx context.Context, q request) ([]library.Item, error) {
	if !q.v.CanSeeLibraries() {
		return nil, nil
	}
	items, err := s.Library.ContinueWatching(ctx, q.v.Owner, 50)
	if err != nil {
		return nil, err
	}
	return s.visible(ctx, q.v, items)
}

func (s *Server) visible(ctx context.Context, v *Viewer, items []library.Item) ([]library.Item, error) {
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	blocked, err := s.blockedAmong(ctx, v, ids)
	if err != nil {
		return nil, err
	}
	out := items[:0:0]
	for _, it := range items {
		if !blocked[it.ID] {
			out = append(out, it)
		}
	}
	return out, nil
}

// objects converts items, loading media files and child counts in two queries.
func (s *Server) objects(ctx context.Context, q request, items []library.Item, prefix, parent string, resume bool) ([]Object, error) {
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	files, err := s.filesOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	counts := map[uuid.UUID]int{}
	rows, err := s.Pool.Query(ctx, `SELECT parent_id, count(*) FROM items WHERE parent_id = ANY($1) GROUP BY parent_id`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var n int
		if rows.Scan(&id, &n) == nil {
			counts[id] = n
		}
	}
	rows.Close()
	out := make([]Object, 0, len(items))
	for _, it := range items {
		out = append(out, s.object(q, it, files[it.ID], counts[it.ID], prefix, parent, resume))
	}
	return out, nil
}

// filesOf loads the media files of items, best version first.
func (s *Server) filesOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]library.MediaFile, error) {
	out := map[uuid.UUID][]library.MediaFile{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT item_id, id, path, label, size, container, duration_ms, width, height, video_codec, hdr,
		audio, subtitles FROM media_files WHERE item_id = ANY($1) ORDER BY item_id, height DESC NULLS LAST, path`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item uuid.UUID
		var f library.MediaFile
		var audio, subs []byte
		if err := rows.Scan(&item, &f.ID, &f.Path, &f.Label, &f.Size, &f.Container, &f.DurationMS, &f.Width, &f.Height,
			&f.VideoCodec, &f.HDR, &audio, &subs); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(audio, &f.Audio)
		_ = json.Unmarshal(subs, &f.Subtitles)
		out[item] = append(out[item], f)
	}
	return out, rows.Err()
}

func clock(ms int64) string {
	s := ms / 1000
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// title of an item as TVs list it.
func (s *Server) title(q request, it library.Item, resume bool) string {
	t := it.Title
	switch it.Kind {
	case "season":
		if it.Season != nil {
			if *it.Season == 0 {
				t = folderName(q.v.Lang, "specials")
			} else {
				t = fmt.Sprintf(folderName(q.v.Lang, "season"), *it.Season)
			}
		}
	case "episode":
		if it.Episode != nil {
			code := fmt.Sprintf("E%02d", *it.Episode)
			if it.Season != nil {
				code = fmt.Sprintf("S%02dE%02d", *it.Season, *it.Episode)
			}
			t = code + " · " + t
		}
		if resume && it.SeriesTitle != "" {
			t = it.SeriesTitle + " · " + t
		}
	case "track":
		if it.Episode != nil && !resume {
			t = fmt.Sprintf("%02d. %s", *it.Episode, t)
		}
	}
	if resume && it.PositionMS > 0 {
		t += " — " + fmt.Sprintf(folderName(q.v.Lang, "resume"), clock(it.PositionMS))
	}
	return t
}

// object builds the DIDL object of an item.
func (s *Server) object(q request, it library.Item, files []library.MediaFile, kids int, prefix, parent string, resume bool) Object {
	o := Object{ID: prefix + it.ID.String(), ParentID: parent, Title: s.title(q, it, resume), ChildCount: kids}
	if it.Year != nil {
		o.Date = fmt.Sprintf("%04d-01-01", *it.Year)
	}
	switch it.Kind {
	case "series", "season":
		o.Class, o.Container = classFolder, true
	case "artist":
		o.Class, o.Container = classArtist, true
	case "album":
		o.Class, o.Container = classAlbum, true
		if it.Artist != nil {
			o.Artist = *it.Artist
		}
	case "movie":
		o.Class = classMovie
	case "track":
		o.Class = classTrack
		if it.Artist != nil {
			o.Artist = *it.Artist
		}
		if it.Album != nil {
			o.Album = *it.Album
		}
		if it.Episode != nil {
			o.TrackNumber = *it.Episode
		}
	default:
		o.Class = classVideo
	}
	artID := it.ID
	if !it.HasPoster && it.ParentID != nil && (it.Kind == "track" || it.Kind == "season" || it.Kind == "episode") {
		artID = *it.ParentID
	}
	if it.HasPoster || artID != it.ID {
		scope := "/dlna/a/" + artID.String() + "/"
		o.ArtURL = q.base + scope + "tn.jpg?" + s.Stream.Signer.Query(scope, stream.URLTTL)
	}
	if o.Container || len(files) == 0 {
		return o
	}
	start := int64(0)
	if resume {
		start = it.PositionMS
	}
	o.Res, o.Subtitle = s.resFor(q, files[0], start)
	if o.Subtitle != "" {
		o.SubtitleRes, o.CaptionInfoEx = q.prof.SubtitleRes, q.prof.CaptionHeader
	}
	return o
}

// tsVideo are the video codecs carried in MPEG-TS without re-encoding.
var tsVideo = []string{"h264", "hevc", "mpeg2video"}

// PlanFor decides how a file reaches a TV: direct (the file as-is, with byte
// seeking) or a stream ffmpeg writes on the fly (MPEG-TS, or MP3 for music)
// that seeks by time. A start position always needs the latter.
func PlanFor(f library.MediaFile, audio int, prof Profile, startMS int64) stream.Plan {
	plan := stream.Decide(f, audio, prof.Device)
	if plan.Method == "direct" && startMS == 0 {
		return plan
	}
	if plan.Method == "direct" {
		plan.Method, plan.Reason = "remux", "start position"
	}
	if f.VideoCodec == "" {
		plan.AudioOnly, plan.TranscodeAudio = true, true
		plan.Method = "transcode"
		return plan
	}
	if !plan.TranscodeVideo && !has(tsVideo, f.VideoCodec) {
		plan.TranscodeVideo, plan.Method = true, "transcode"
	}
	if plan.TranscodeVideo && prof.Device.MaxHeight > 0 && f.Height != nil && *f.Height > prof.Device.MaxHeight {
		plan.Height = prof.Device.MaxHeight
	}
	if !plan.TranscodeAudio && audio >= 0 && audio < len(f.Audio) && !has(prof.TSAudio, f.Audio[audio].Codec) {
		plan.TranscodeAudio = true
	}
	return plan
}

func has(list []string, v string) bool {
	return slices.ContainsFunc(list, func(x string) bool { return strings.EqualFold(x, v) })
}

// allowed applies the user's transcoding permissions to a plan.
func allowed(v *Viewer, plan stream.Plan) bool {
	u := v.Principal.User
	if u.IsAdmin || plan.Method == "direct" {
		return true
	}
	p := u.Permissions
	return (!plan.TranscodeVideo || p.VideoTranscoding) && (!plan.TranscodeAudio || p.AudioTranscoding) &&
		(plan.Method != "remux" || p.Remux)
}

// resFor builds the <res> of a file and the sidecar subtitle URL (if any).
func (s *Server) resFor(q request, f library.MediaFile, startMS int64) ([]Res, string) {
	audio := library.PickAudio(f.Audio, q.v.Prefs.AudioLanguage)
	if len(f.Audio) == 0 {
		audio = -1
	}
	plan := PlanFor(f, audio, q.prof, startMS)
	if !allowed(q.v, plan) {
		if startMS > 0 {
			return nil, ""
		}
		plan = stream.Plan{Method: "direct"} // best effort: the TV may still play it
	}
	var dur int64
	if f.DurationMS != nil {
		dur = *f.DurationMS - startMS
	}
	audioOnly := f.VideoCodec == ""
	r := Res{DurationMS: dur}
	if audio >= 0 && audio < len(f.Audio) {
		r.Channels = f.Audio[audio].Channels
	}
	id := f.ID.String()
	if plan.Method == "direct" {
		scope := "/dlna/f/" + id + "/"
		mime := q.prof.MIMEType(f.Container, audioOnly)
		pn := ""
		if audioOnly {
			pn = audioPN(mime)
		}
		r.URL = q.base + scope + "media" + strings.ToLower(filepath.Ext(f.Path)) + "?" + s.Stream.Signer.Query(scope, stream.URLTTL)
		r.ProtocolInfo = ProtocolInfo(mime, Features(pn, true, false))
		r.Size = f.Size
		if f.Width != nil && f.Height != nil && !audioOnly {
			r.Resolution = strconv.Itoa(*f.Width) + "x" + strconv.Itoa(*f.Height)
		}
	} else {
		scope := "/dlna/t/" + id + "/"
		name, mime, pn := "media.ts", "video/mpeg", ""
		if audioOnly {
			name, mime, pn = "media.mp3", "audio/mpeg", "MP3"
		} else if plan.TranscodeAudio {
			r.Channels = 2
		}
		v := "p=" + q.prof.Name + "&a=" + strconv.Itoa(audio)
		if startMS > 0 {
			v += "&s=" + strconv.FormatInt(startMS, 10)
		}
		r.URL = q.base + scope + name + "?" + v + "&" + s.Stream.Signer.Query(scope, stream.URLTTL)
		r.ProtocolInfo = ProtocolInfo(mime, Features(pn, false, true))
	}
	// Subtitles only line up with streams that start at zero.
	if audioOnly || startMS > 0 || (!q.prof.SubtitleRes && !q.prof.CaptionHeader) {
		return []Res{r}, ""
	}
	if i := pickSubtitle(f, audio, q.v.Prefs); i >= 0 {
		scope := "/dlna/s/" + id + "/"
		return []Res{r}, q.base + scope + strconv.Itoa(i) + ".srt?" + s.Stream.Signer.Query(scope, stream.URLTTL)
	}
	return []Res{r}, ""
}

// pickSubtitle chooses the sidecar subtitle to offer (position in f.Subtitles,
// -1 = none): the viewer's choice, else a full (not forced) track in the
// preferred language, else the first full track. TVs show it only when the
// viewer turns subtitles on, so offering one is harmless.
func pickSubtitle(f library.MediaFile, audio int, p library.Prefs) int {
	var idx []int
	var choices []library.SubtitleChoice
	for i, sub := range f.Subtitles {
		if sub.External || textCodec(sub.Codec) {
			idx = append(idx, i)
			choices = append(choices, library.SubtitleChoice{Language: sub.Language, Forced: sub.Forced})
		}
	}
	if len(idx) == 0 || p.Mode() == "none" {
		return -1
	}
	audioLang := ""
	if audio >= 0 && audio < len(f.Audio) {
		audioLang = f.Audio[audio].Language
	}
	if i := library.PickSubtitle(choices, audioLang, p); i >= 0 {
		return idx[i]
	}
	for i, c := range choices {
		if !c.Forced && p.SubtitleLanguage != "" && library.SameLanguage(c.Language, p.SubtitleLanguage) {
			return idx[i]
		}
	}
	for i, c := range choices {
		if !c.Forced {
			return idx[i]
		}
	}
	return -1
}

func textCodec(codec string) bool {
	switch codec {
	case "subrip", "srt", "ass", "ssa", "webvtt", "vtt", "mov_text", "text":
		return true
	}
	return false
}
