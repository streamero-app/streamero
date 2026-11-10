package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
)

var typeMap = map[string]string{"movie": "Movie", "series": "Series", "season": "Season", "episode": "Episode",
	"artist": "MusicArtist", "album": "MusicAlbum", "track": "Audio"}

// kindsFor maps a client type name ("Movie", "Episode", …) to Streamero kinds.
func kindsFor(t string) []string {
	var out []string
	for k, v := range typeMap {
		if strings.EqualFold(v, strings.TrimSpace(t)) {
			out = append(out, k)
		}
	}
	return out
}

func isFolderKind(kind string) bool {
	return kind == "series" || kind == "season" || kind == "artist" || kind == "album"
}

type row struct {
	id, libraryID                   uuid.UUID
	parentID, seriesID              *uuid.UUID
	kind, title, sortTitle          string
	originalTitle, tagline          *string
	year, season, episode, epEnd    *int
	added                           time.Time
	ids                             map[string]string
	premiere                        *time.Time
	overview, officialRating        *string
	rating                          *float64
	genres, studios                 []string
	maxWidth, maxHeight, filesCount int
	durationMS                      *int64
	hasPoster, hasBackdrop, hasLogo bool
	imageVersion                    time.Time
	artist                          *string
	played, favorite                bool
	position                        int64
	playCount                       int
	lastPlayed                      *time.Time
	parentTitle                     *string
	seriesTitle                     *string
	seriesPoster, seriesBackdrop    bool
	seriesLogo                      bool
	childCount, unplayed, recursive *int
}

// fromSQL joins an item with the viewer's state ($owner), its parent and its series.
const fromSQL = ` FROM items i
	LEFT JOIN user_item_data u ON u.item_id = i.id AND u.owner_id = %s
	LEFT JOIN items par ON par.id = i.parent_id
	LEFT JOIN items ser ON ser.id = CASE
		WHEN i.kind = 'season' THEN i.parent_id
		WHEN i.kind = 'episode' AND par.kind = 'season' THEN par.parent_id
		WHEN i.kind = 'episode' THEN i.parent_id END `

// episodesUnder is the SQL set of episode ids below folder alias.id (series or season).
const episodesUnder = `(SELECT e.id FROM items e WHERE e.kind IN ('episode', 'track') AND (e.parent_id = %[1]s.id
	OR e.parent_id IN (SELECT s.id FROM items s WHERE s.parent_id = %[1]s.id)))`

var rowCols = `i.id, i.library_id, i.parent_id, ser.id,
	i.kind, COALESCE(i.metadata_title, i.title), i.sort_title, i.original_title, i.tagline,
	i.year, i.season_number, i.episode_number, i.episode_number_end, i.added_at, i.provider_ids,
	i.premiere_date::timestamptz, i.overview, i.official_rating, i.community_rating, i.genres, i.studios,
	COALESCE((SELECT max(f.width) FROM media_files f WHERE f.item_id = i.id), 0),
	COALESCE((SELECT max(f.height) FROM media_files f WHERE f.item_id = i.id), 0),
	(SELECT count(*) FROM media_files f WHERE f.item_id = i.id),
	(SELECT max(f.duration_ms) FROM media_files f WHERE f.item_id = i.id),
	i.has_poster, i.has_backdrop, i.has_logo, COALESCE(i.metadata_refreshed_at, i.updated_at), i.artist,
	COALESCE(u.played, false), COALESCE(u.favorite, false), COALESCE(u.position_ms, 0), COALESCE(u.play_count, 0),
	CASE WHEN u.play_count > 0 OR u.position_ms > 0 THEN u.last_played_at END,
	COALESCE(par.metadata_title, par.title), COALESCE(ser.metadata_title, ser.title),
	COALESCE(ser.has_poster, false), COALESCE(ser.has_backdrop, false), COALESCE(ser.has_logo, false),
	CASE WHEN i.kind IN ('series', 'season', 'artist', 'album') THEN (SELECT count(*) FROM items c WHERE c.parent_id = i.id)::int END,
	CASE WHEN i.kind IN ('series', 'season', 'artist', 'album') THEN (SELECT count(*) FROM ` + fmt.Sprintf(episodesUnder, "i") + ` x
		WHERE NOT EXISTS (SELECT 1 FROM user_item_data w WHERE w.item_id = x.id AND w.owner_id = u2.owner AND w.played))::int END,
	CASE WHEN i.kind IN ('series', 'season', 'artist', 'album') THEN (SELECT count(*) FROM ` + fmt.Sprintf(episodesUnder, "i") + ` x)::int END`

// query runs a filtered, ordered page of items for owner. where and order are
// built from fixed fragments; values are numbered parameters only.
func (s *Server) query(ctx context.Context, owner uuid.UUID, where string, args []any, order string, limit, offset int) ([]row, int, error) {
	n := len(args)
	ownerParam := "$" + strconv.Itoa(n+1)
	from := fmt.Sprintf(fromSQL, ownerParam) + ` CROSS JOIN (SELECT ` + ownerParam + `::uuid AS owner) u2 `
	var total int
	// #nosec G202 -- fixed fragments with numbered parameters only
	if err := s.Pool.QueryRow(ctx, `SELECT count(*)`+from+` WHERE `+where, append(args, owner)...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit == 0 {
		return nil, total, nil
	}
	// #nosec G202 -- fixed fragments with numbered parameters only
	rows, err := s.Pool.Query(ctx, `SELECT `+rowCols+from+` WHERE `+where+` ORDER BY `+order+
		` LIMIT $`+strconv.Itoa(n+2)+` OFFSET $`+strconv.Itoa(n+3), append(args, owner, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		var x row
		var ids []byte
		if err := rows.Scan(&x.id, &x.libraryID, &x.parentID, &x.seriesID, &x.kind, &x.title, &x.sortTitle, &x.originalTitle,
			&x.tagline, &x.year, &x.season, &x.episode, &x.epEnd, &x.added, &ids, &x.premiere, &x.overview, &x.officialRating,
			&x.rating, &x.genres, &x.studios, &x.maxWidth, &x.maxHeight, &x.filesCount, &x.durationMS,
			&x.hasPoster, &x.hasBackdrop, &x.hasLogo, &x.imageVersion, &x.artist,
			&x.played, &x.favorite, &x.position, &x.playCount, &x.lastPlayed, &x.parentTitle, &x.seriesTitle,
			&x.seriesPoster, &x.seriesBackdrop, &x.seriesLogo, &x.childCount, &x.unplayed, &x.recursive); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(ids, &x.ids)
		out = append(out, x)
	}
	return out, total, rows.Err()
}

// byIDs loads items in the given order (missing ones are skipped).
func (s *Server) byIDs(ctx context.Context, owner uuid.UUID, ids []uuid.UUID) ([]row, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, _, err := s.query(ctx, owner, "i.id = ANY($1)", []any{ids}, "i.id", len(ids), 0)
	if err != nil {
		return nil, err
	}
	pos := map[uuid.UUID]row{}
	for _, r := range rows {
		pos[r.id] = r
	}
	out := make([]row, 0, len(rows))
	for _, id := range ids {
		if r, ok := pos[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// tag is an image cache key that changes when the artwork may have changed.
func tag(id uuid.UUID, kind string, v time.Time) string {
	return fmt.Sprintf("%x", []byte(kind))[:4] + strconv.FormatInt(v.Unix(), 16) + ID(id)[:8]
}

// fields lists what the client asked for in Fields (lower-case).
type fields map[string]bool

func parseFields(r *http.Request) fields {
	f := fields{}
	for _, v := range strings.Split(param(r, "Fields"), ",") {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			f[v] = true
		}
	}
	return f
}

func ticks(ms int64) int64 { return ms * 10_000 }

func (s *Server) userData(x row) map[string]any {
	d := map[string]any{"PlaybackPositionTicks": ticks(x.position), "PlayCount": x.playCount, "IsFavorite": x.favorite,
		"Played": x.played, "Key": ID(x.id), "ItemId": ID(x.id)}
	if x.lastPlayed != nil {
		d["LastPlayedDate"] = x.lastPlayed.UTC().Format(time.RFC3339)
	}
	if x.durationMS != nil && *x.durationMS > 0 && x.position > 0 {
		d["PlayedPercentage"] = float64(x.position) * 100 / float64(*x.durationMS)
	}
	if x.unplayed != nil {
		d["UnplayedItemCount"] = *x.unplayed
		if x.recursive != nil && *x.recursive > 0 && *x.unplayed == 0 {
			d["Played"] = true
		}
	}
	return d
}

func (s *Server) dto(x row) map[string]any {
	providers := map[string]string{}
	for k, v := range x.ids {
		switch k {
		case "tmdb":
			providers["Tmdb"] = v
		case "imdb":
			providers["Imdb"] = v
		case "tvdb":
			providers["Tvdb"] = v
		case "musicbrainz":
			providers["MusicBrainzReleaseGroup"] = v
		}
	}
	mediaType := "Video"
	if x.kind == "track" {
		mediaType = "Audio"
	} else if isFolderKind(x.kind) {
		mediaType = "Unknown"
	}
	d := map[string]any{
		"Name": x.title, "SortName": x.sortTitle, "Id": ID(x.id), "ServerId": ID(s.ServerID), "Type": typeMap[x.kind],
		"IsFolder": isFolderKind(x.kind), "ProviderIds": providers, "LocationType": "FileSystem",
		"DateCreated": x.added.UTC().Format(time.RFC3339), "MediaType": mediaType, "Etag": tag(x.id, "etag", x.imageVersion),
		"UserData": s.userData(x), "Genres": orEmpty(x.genres), "Taglines": []string{}, "BackdropImageTags": []string{},
		"ImageTags": map[string]string{}, "CanDelete": false, "CanDownload": false, "PlayAccess": "Full",
	}
	genreItems := make([]any, 0, len(x.genres))
	for _, g := range x.genres {
		genreItems = append(genreItems, map[string]any{"Name": g, "Id": GenreID(g)})
	}
	d["GenreItems"] = genreItems
	studios := make([]any, 0, len(x.studios))
	for _, st := range x.studios {
		studios = append(studios, map[string]any{"Name": st, "Id": synthetic("73747564", nameHash(st))})
	}
	d["Studios"] = studios
	if x.originalTitle != nil && *x.originalTitle != "" {
		d["OriginalTitle"] = *x.originalTitle
	}
	if x.tagline != nil && *x.tagline != "" {
		d["Taglines"] = []string{*x.tagline}
	}
	if x.year != nil {
		d["ProductionYear"] = *x.year
	}
	if x.premiere != nil {
		d["PremiereDate"] = x.premiere.UTC().Format(time.RFC3339)
	}
	if x.overview != nil {
		d["Overview"] = *x.overview
	}
	if x.officialRating != nil && *x.officialRating != "" {
		d["OfficialRating"] = *x.officialRating
	}
	if x.rating != nil {
		d["CommunityRating"] = *x.rating
	}
	if x.durationMS != nil {
		d["RunTimeTicks"] = ticks(*x.durationMS)
	}
	images := map[string]string{}
	if x.hasPoster {
		images["Primary"] = tag(x.id, "primary", x.imageVersion)
		switch x.kind {
		case "episode":
			d["PrimaryImageAspectRatio"] = 16.0 / 9.0
		case "track", "album", "artist":
			d["PrimaryImageAspectRatio"] = 1.0
		default:
			d["PrimaryImageAspectRatio"] = 2.0 / 3.0
		}
	}
	if x.hasLogo {
		images["Logo"] = tag(x.id, "logo", x.imageVersion)
	}
	if x.hasBackdrop {
		images["Thumb"] = tag(x.id, "thumb", x.imageVersion)
		d["BackdropImageTags"] = []string{tag(x.id, "backdrop", x.imageVersion)}
	}
	d["ImageTags"] = images
	if x.parentID != nil {
		d["ParentId"] = ID(*x.parentID)
	}
	if x.seriesID != nil && x.seriesTitle != nil {
		sid := ID(*x.seriesID)
		d["SeriesId"], d["SeriesName"] = sid, *x.seriesTitle
		if x.seriesPoster {
			d["SeriesPrimaryImageTag"] = tag(*x.seriesID, "primary", x.imageVersion)
		}
		if x.seriesBackdrop {
			d["ParentBackdropItemId"] = sid
			d["ParentBackdropImageTags"] = []string{tag(*x.seriesID, "backdrop", x.imageVersion)}
			d["ParentThumbItemId"], d["ParentThumbImageTag"] = sid, tag(*x.seriesID, "thumb", x.imageVersion)
		}
		if x.seriesLogo {
			d["ParentLogoItemId"], d["ParentLogoImageTag"] = sid, tag(*x.seriesID, "logo", x.imageVersion)
		}
	}
	switch x.kind {
	case "season":
		if x.season != nil {
			d["IndexNumber"] = *x.season
		}
	case "episode", "track":
		if x.episode != nil {
			d["IndexNumber"] = *x.episode
		}
		if x.epEnd != nil {
			d["IndexNumberEnd"] = *x.epEnd
		}
		if x.season != nil {
			d["ParentIndexNumber"] = *x.season
		}
		if x.kind == "episode" && x.parentID != nil && x.seriesID != nil && *x.parentID != *x.seriesID {
			d["SeasonId"] = ID(*x.parentID)
			if x.parentTitle != nil {
				d["SeasonName"] = *x.parentTitle
			}
		}
		if x.kind == "track" {
			if x.parentID != nil {
				d["AlbumId"] = ID(*x.parentID)
			}
			if x.parentTitle != nil {
				d["Album"] = *x.parentTitle
			}
		}
	}
	if x.artist != nil && *x.artist != "" {
		d["Artists"] = []string{*x.artist}
		d["AlbumArtist"] = *x.artist
	}
	if x.childCount != nil {
		d["ChildCount"] = *x.childCount
	}
	if x.recursive != nil {
		d["RecursiveItemCount"] = *x.recursive
	}
	if x.filesCount > 0 {
		d["VideoType"] = "VideoFile"
		d["HasSubtitles"] = false
		if x.kind != "track" {
			d["Width"], d["Height"] = x.maxWidth, x.maxHeight
			d["IsHD"] = x.maxHeight >= 720
		}
		d["LocationType"] = "FileSystem"
	}
	return d
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// --- listing ------------------------------------------------------------------

// queryBuilder collects numbered SQL conditions.
type queryBuilder struct {
	conds []string
	args  []any
}

// add appends cond with each "?" replaced by the parameter for v.
func (q *queryBuilder) add(cond string, v any) {
	q.args = append(q.args, v)
	q.conds = append(q.conds, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(q.args))))
}

func (q *queryBuilder) where() string {
	if len(q.conds) == 0 {
		return "true"
	}
	return strings.Join(q.conds, " AND ")
}

func splitList(v, sep string) []string {
	var out []string
	for _, s := range strings.Split(v, sep) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseIDs(v string) []uuid.UUID {
	var out []uuid.UUID
	for _, s := range splitList(v, ",") {
		if u, err := ParseID(s); err == nil {
			out = append(out, u)
		}
	}
	return out
}

func boolParam(r *http.Request, name string) (bool, bool) {
	v := param(r, name)
	if v == "" {
		return false, false
	}
	b, err := strconv.ParseBool(v)
	return b, err == nil
}

// sortColumns maps sort keys to SQL expressions (whitelist; nothing from the client reaches SQL).
var sortColumns = map[string]string{
	"sortname": "i.sort_title", "name": "i.sort_title", "default": "i.sort_title",
	"datecreated": "i.added_at", "datelastcontentadded": "i.added_at",
	"premieredate": "i.premiere_date", "productionyear": "i.year",
	"communityrating": "i.community_rating", "criticrating": "i.community_rating",
	"officialrating": "i.official_rating", "random": "random()",
	"runtime":    "(SELECT max(f.duration_ms) FROM media_files f WHERE f.item_id = i.id)",
	"dateplayed": "u.last_played_at", "seriesdateplayed": "u.last_played_at", "playcount": "u.play_count",
	"isplayed": "COALESCE(u.played, false)", "isunplayed": "NOT COALESCE(u.played, false)",
	"isfavoriteorliked": "COALESCE(u.favorite, false)", "isfolder": "(i.kind IN ('series', 'season', 'artist', 'album'))",
	"indexnumber": "i.episode_number", "parentindexnumber": "i.season_number",
	"airedepisodeorder": "i.season_number, i.episode_number",
	"seriessortname":    "COALESCE(ser.sort_title, i.sort_title)",
	"album":             "par.sort_title", "albumartist": "i.artist", "artist": "i.artist",
}

// orderBy builds ORDER BY from SortBy / SortOrder (comma lists; one order may apply to all keys).
func orderBy(sortBy, sortOrder, fallback string) string {
	keys := splitList(sortBy, ",")
	orders := splitList(sortOrder, ",")
	var parts []string
	for n, k := range keys {
		col, ok := sortColumns[strings.ToLower(k)]
		if !ok {
			continue
		}
		dir := "ASC"
		o := ""
		if n < len(orders) {
			o = orders[n]
		} else if len(orders) > 0 {
			o = orders[len(orders)-1]
		}
		if strings.EqualFold(o, "Descending") {
			dir = "DESC"
		}
		if col == "random()" {
			parts = append(parts, col)
			continue
		}
		for _, c := range strings.Split(col, ", ") {
			parts = append(parts, c+" "+dir+" NULLS LAST")
		}
	}
	if len(parts) == 0 {
		return fallback
	}
	return strings.Join(append(parts, "i.sort_title", "i.id"), ", ")
}

const defaultOrder = "i.season_number NULLS FIRST, i.episode_number NULLS FIRST, i.sort_title, i.id"

// itemFilter adds the filters clients use on /Items to q.
func (s *Server) itemFilter(ctx context.Context, r *http.Request, q *queryBuilder) error {
	if ids := param(r, "Ids"); ids != "" {
		q.add("i.id = ANY(?)", parseIDs(ids))
	}
	if ex := param(r, "ExcludeItemIds"); ex != "" {
		q.add("NOT (i.id = ANY(?))", parseIDs(ex))
	}
	if types := param(r, "IncludeItemTypes"); types != "" {
		kinds := []string{}
		for _, t := range splitList(types, ",") {
			kinds = append(kinds, kindsFor(t)...)
		}
		q.add("i.kind = ANY(?)", kinds)
	}
	if types := param(r, "ExcludeItemTypes"); types != "" {
		kinds := []string{}
		for _, t := range splitList(types, ",") {
			kinds = append(kinds, kindsFor(t)...)
		}
		q.add("NOT (i.kind = ANY(?))", kinds)
	}
	if mt := param(r, "MediaTypes"); mt != "" {
		kinds := []string{}
		for _, t := range splitList(mt, ",") {
			switch strings.ToLower(t) {
			case "video":
				kinds = append(kinds, "movie", "episode")
			case "audio":
				kinds = append(kinds, "track")
			}
		}
		q.add("i.kind = ANY(?)", kinds)
	}
	recursive, _ := boolParam(r, "Recursive")
	if parent := param(r, "ParentId"); parent != "" {
		pid, err := ParseID(parent)
		if err != nil {
			return errBadRequest
		}
		var isLibrary bool
		_ = s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM libraries WHERE id = $1)`, pid).Scan(&isLibrary)
		switch {
		case isLibrary && recursive:
			q.add("i.library_id = ?", pid)
		case isLibrary:
			q.add("i.library_id = ? AND i.parent_id IS NULL", pid)
		case recursive: // both placeholders become the same parameter
			q.add("(i.parent_id = ? OR i.parent_id IN (SELECT id FROM items WHERE parent_id = ?))", pid)
		default:
			q.add("i.parent_id = ?", pid)
		}
	} else if !recursive && param(r, "Ids") == "" {
		q.conds = append(q.conds, "i.parent_id IS NULL")
	}
	if term := strings.ToLower(strings.TrimSpace(param(r, "SearchTerm"))); term != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(term) + "%"
		q.args = append(q.args, like, term)
		a, b := "$"+strconv.Itoa(len(q.args)-1), "$"+strconv.Itoa(len(q.args))
		q.conds = append(q.conds, "(lower(COALESCE(i.metadata_title, i.title)) LIKE "+a+
			" OR word_similarity("+b+", lower(COALESCE(i.metadata_title, i.title))) >= 0.35)")
	}
	for _, f := range splitList(param(r, "Filters"), ",") {
		switch strings.ToLower(f) {
		case "isplayed":
			q.conds = append(q.conds, "COALESCE(u.played, false)")
		case "isunplayed":
			q.conds = append(q.conds, "NOT COALESCE(u.played, false)")
		case "isfavorite", "isfavoriteorlikes", "likes":
			q.conds = append(q.conds, "COALESCE(u.favorite, false)")
		case "isresumable":
			q.conds = append(q.conds, "COALESCE(u.position_ms, 0) > 0 AND NOT COALESCE(u.played, false)")
		case "isfolder":
			q.conds = append(q.conds, "i.kind IN ('series', 'season', 'artist', 'album')")
		case "isnotfolder":
			q.conds = append(q.conds, "i.kind NOT IN ('series', 'season', 'artist', 'album')")
		}
	}
	if v, ok := boolParam(r, "IsFavorite"); ok {
		q.add("COALESCE(u.favorite, false) = ?", v)
	}
	if v, ok := boolParam(r, "IsPlayed"); ok {
		q.add("COALESCE(u.played, false) = ?", v)
	}
	if g := param(r, "Genres"); g != "" {
		q.add("i.genres && ?::text[]", splitList(g, "|"))
	}
	if g := param(r, "GenreIds"); g != "" {
		var hashes []string
		for _, id := range splitList(g, ",") {
			if t, p := syntheticKind(id); t == tagGenre {
				hashes = append(hashes, p)
			}
		}
		q.add("EXISTS (SELECT 1 FROM unnest(i.genres) g WHERE "+nameHashSQL("g")+" = ANY(?))", hashes)
	}
	if st := param(r, "Studios"); st != "" {
		q.add("i.studios && ?::text[]", splitList(st, "|"))
	}
	if ys := param(r, "Years"); ys != "" {
		var years []int
		for _, y := range splitList(ys, ",") {
			if n, err := strconv.Atoi(y); err == nil {
				years = append(years, n)
			}
		}
		q.add("i.year = ANY(?)", years)
	}
	if v := param(r, "OfficialRatings"); v != "" {
		q.add("i.official_rating = ANY(?)", splitList(v, "|"))
	}
	if v := param(r, "MinCommunityRating"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			q.add("i.community_rating >= ?", f)
		}
	}
	if v := param(r, "NameStartsWith"); v != "" {
		q.add("lower(i.sort_title) LIKE ?", strings.ToLower(strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(v))+"%")
	}
	if v := param(r, "NameStartsWithOrGreater"); v != "" {
		q.add("lower(i.sort_title) >= ?", strings.ToLower(v))
	}
	if v := param(r, "NameLessThan"); v != "" {
		q.add("lower(i.sort_title) < ?", strings.ToLower(v))
	}
	if v := param(r, "PersonIds"); v != "" {
		var tmdb []int64
		var names []string
		for _, id := range splitList(v, ",") {
			if n, ok := ParsePersonID(id); ok {
				tmdb = append(tmdb, n)
			} else if t, p := syntheticKind(id); t == tagPersonName {
				names = append(names, p)
			}
		}
		q.args = append(q.args, tmdb, names)
		a, b := "$"+strconv.Itoa(len(q.args)-1), "$"+strconv.Itoa(len(q.args))
		q.conds = append(q.conds, "EXISTS (SELECT 1 FROM item_people p WHERE p.item_id = i.id AND (p.tmdb_person_id = ANY("+a+
			"::bigint[]) OR "+nameHashSQL("p.name")+" = ANY("+b+")))")
	}
	if v := param(r, "Person"); v != "" {
		q.add("EXISTS (SELECT 1 FROM item_people p WHERE p.item_id = i.id AND lower(p.name) = lower(?))", v)
	}
	for name, key := range map[string]string{"HasTmdbId": "tmdb", "HasImdbId": "imdb", "HasTvdbId": "tvdb"} {
		if v, ok := boolParam(r, name); ok {
			q.add("(i.provider_ids->>'"+key+"' IS NOT NULL) = ?", v) // key is a constant
		}
	}
	if v, ok := boolParam(r, "IsHd"); ok {
		q.add("(COALESCE((SELECT max(f.height) FROM media_files f WHERE f.item_id = i.id), 0) >= 720) = ?", v)
	}
	if v, ok := boolParam(r, "Is4K"); ok {
		q.add("(COALESCE((SELECT max(f.width) FROM media_files f WHERE f.item_id = i.id), 0) >= 3800) = ?", v)
	}
	if v := param(r, "ParentIndexNumber"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.add("i.season_number = ?", n)
		}
	}
	if v := param(r, "IndexNumber"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.add("i.episode_number = ?", n)
		}
	}
	if v := param(r, "AdjacentTo"); v != "" {
		if id, err := ParseID(v); err == nil {
			// The item itself and its direct neighbours among its siblings.
			q.add(`i.id IN (SELECT x.id FROM (
				SELECT c.id, lag(c.id) OVER w AS prev, lead(c.id) OVER w AS next FROM items c
				WHERE c.parent_id = (SELECT parent_id FROM items WHERE id = ?)
				WINDOW w AS (ORDER BY c.season_number NULLS FIRST, c.episode_number NULLS FIRST, c.sort_title)) x
				WHERE x.id = ? OR x.prev = ? OR x.next = ?)`, id)
		}
	}
	return nil
}

type httpError struct {
	status int
	msg    string
}

func (e httpError) Error() string { return e.msg }

var errBadRequest = httpError{http.StatusBadRequest, "bad request"}

// page reads StartIndex / Limit (Limit absent = everything up to a cap).
func page(r *http.Request, def int) (limit, offset int) {
	limit = def
	if v, err := strconv.Atoi(param(r, "Limit")); err == nil {
		limit = max(0, min(v, 2000))
	}
	if v, err := strconv.Atoi(param(r, "StartIndex")); err == nil {
		offset = max(0, v)
	}
	return limit, offset
}

func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !canSee(p) {
		writeJSON(w, queryResult(nil, 0, 0))
		return
	}
	var q queryBuilder
	if err := s.itemFilter(r.Context(), r, &q); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit, offset := page(r, 500)
	order := orderBy(param(r, "SortBy"), param(r, "SortOrder"), defaultOrder)
	rows, total, err := s.query(r.Context(), ownerOf(p), q.where(), q.args, order, limit, offset)
	if err != nil {
		s.Log.Warn("compat items query failed", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	s.writeRows(w, r, rows, total, offset)
}

// writeRows answers a query result, adding detail fields the client asked for.
func (s *Server) writeRows(w http.ResponseWriter, r *http.Request, rows []row, total, offset int) {
	out, err := s.dtos(r.Context(), rows, parseFields(r))
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, queryResult(out, total, offset))
}

func queryResult(items []any, total, offset int) map[string]any {
	if items == nil {
		items = []any{}
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": offset}
}

// dtos converts rows, adding media sources and people when f asks for them.
func (s *Server) dtos(ctx context.Context, rows []row, f fields) ([]any, error) {
	out := make([]any, 0, len(rows))
	var ids []uuid.UUID
	for _, x := range rows {
		ids = append(ids, x.id)
	}
	// Sources are always included: clients pick the version to play from them.
	files, err := s.filesOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	var people map[uuid.UUID][]any
	if f["people"] {
		if people, err = s.peopleOf(ctx, ids); err != nil {
			return nil, err
		}
	}
	for _, x := range rows {
		d := s.dto(x)
		if fs, ok := files[x.id]; ok && len(fs) > 0 {
			sources := make([]any, 0, len(fs))
			for _, mf := range fs {
				sources = append(sources, mediaSource(mf))
			}
			d["MediaSources"] = sources
			d["MediaStreams"] = sources[0].(map[string]any)["MediaStreams"]
			d["HasSubtitles"] = len(fs[0].Subtitles) > 0
			d["Container"] = fs[0].Container
		}
		if people != nil {
			d["People"] = orAny(people[x.id])
		}
		out = append(out, d)
	}
	return out, nil
}

func orAny(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

// filesOf loads the media files of items, best version first.
func (s *Server) filesOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]library.MediaFile, error) {
	rows, err := s.Pool.Query(ctx, `SELECT item_id, id, path, label, size, container, duration_ms, width, height, video_codec, hdr,
		audio, subtitles FROM media_files WHERE item_id = ANY($1) ORDER BY item_id, height DESC NULLS LAST, path`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]library.MediaFile{}
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

// personType maps Streamero credits to the client's person kinds.
func personType(kind, role string) string {
	if kind == "cast" {
		return "Actor"
	}
	for _, t := range []string{"Director", "Writer", "Producer", "Composer"} {
		if strings.Contains(strings.ToLower(role), strings.ToLower(t)) {
			return t
		}
	}
	return "Unknown"
}

func (s *Server) peopleOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]any, error) {
	rows, err := s.Pool.Query(ctx, `SELECT item_id, kind, name, role, tmdb_person_id, has_image FROM item_people
		WHERE item_id = ANY($1) ORDER BY item_id, kind, position`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]any{}
	for rows.Next() {
		var item uuid.UUID
		var kind, name, role string
		var tmdb *int64
		var hasImage bool
		if err := rows.Scan(&item, &kind, &name, &role, &tmdb, &hasImage); err != nil {
			return nil, err
		}
		pd := map[string]any{"Name": name, "Role": role, "Type": personType(kind, role), "ImageBlurHashes": map[string]any{}}
		if tmdb != nil {
			pd["Id"] = PersonID(*tmdb)
			if hasImage {
				pd["PrimaryImageTag"] = personTag(PersonID(*tmdb))
			}
		} else {
			pd["Id"] = PersonNameID(name)
		}
		out[item] = append(out[item], pd)
	}
	return out, rows.Err()
}
