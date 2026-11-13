package compat

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
)

// --- single items -------------------------------------------------------------------

func (s *Server) item(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "itemId")
	if tag, _ := syntheticKind(raw); tag != "" {
		s.syntheticItem(w, r, raw, tag)
		return
	}
	id, err := ParseID(raw)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// A library ID asked as an item (clients do this for views).
	if lib, err := s.Libraries.Get(r.Context(), id); err == nil {
		if !library.IsStreamingLibrary(lib.Kind) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, s.viewDTO(lib))
		return
	}
	if s.writeLiveItem(w, r, id) {
		return
	}
	p := principal(r)
	if !canSee(p) {
		http.NotFound(w, r)
		return
	}
	rows, _, err := s.query(r.Context(), ownerOf(p), "i.id = $1", []any{id}, "i.id", 1, 0)
	if err != nil || len(rows) == 0 {
		http.NotFound(w, r)
		return
	}
	// A single item always carries the details clients show on its page.
	f := parseFields(r)
	f["people"], f["mediasources"] = true, true
	out, err := s.dtos(r.Context(), rows, f)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	d := out[0].(map[string]any)
	d["Chapters"] = s.chapters(r, id)
	writeJSON(w, d)
}

// chapters lists the best version's chapters.
func (s *Server) chapters(r *http.Request, id uuid.UUID) []any {
	var raw []byte
	_ = s.Pool.QueryRow(r.Context(), `SELECT chapters FROM media_files WHERE item_id = $1 ORDER BY height DESC NULLS LAST, path LIMIT 1`, id).Scan(&raw)
	var chs []struct {
		StartMS int64  `json:"startMs"`
		Title   string `json:"title"`
	}
	_ = json.Unmarshal(raw, &chs)
	out := []any{}
	for _, c := range chs {
		out = append(out, map[string]any{"StartPositionTicks": ticks(c.StartMS), "Name": c.Title,
			"ImageDateModified": "0001-01-01T00:00:00Z"})
	}
	return out
}

// syntheticItem answers a person or genre asked as an item.
func (s *Server) syntheticItem(w http.ResponseWriter, r *http.Request, raw, tag string) {
	switch tag {
	case tagPerson, tagPersonName:
		var name string
		var hasImage bool
		var err error
		if n, ok := ParsePersonID(raw); ok {
			err = s.Pool.QueryRow(r.Context(), `SELECT name, bool_or(has_image) FROM item_people WHERE tmdb_person_id = $1
				GROUP BY name LIMIT 1`, n).Scan(&name, &hasImage)
		} else {
			_, payload := syntheticKind(raw)
			err = s.Pool.QueryRow(r.Context(), `SELECT name FROM item_people WHERE `+nameHashSQL("name")+` = $1 LIMIT 1`, payload).Scan(&name)
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		images := map[string]string{}
		if hasImage {
			images["Primary"] = personTag(raw)
		}
		writeJSON(w, map[string]any{"Name": name, "Id": strings.ToLower(raw), "ServerId": ID(s.ServerID), "Type": "Person",
			"MediaType": "Unknown", "IsFolder": false, "ImageTags": images, "BackdropImageTags": []string{},
			"ProviderIds": map[string]string{}, "UserData": map[string]any{"PlaybackPositionTicks": 0, "PlayCount": 0,
				"IsFavorite": false, "Played": false, "Key": strings.ToLower(raw), "ItemId": strings.ToLower(raw)}})
	case tagGenre:
		_, payload := syntheticKind(raw)
		var name string
		if err := s.Pool.QueryRow(r.Context(), `SELECT g FROM items, unnest(genres) g WHERE `+nameHashSQL("g")+` = $1 LIMIT 1`,
			payload).Scan(&name); err != nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"Name": name, "Id": strings.ToLower(raw), "ServerId": ID(s.ServerID), "Type": "Genre",
			"MediaType": "Unknown", "IsFolder": true, "ImageTags": map[string]string{}, "BackdropImageTags": []string{}})
	default:
		http.NotFound(w, r)
	}
}

// --- home rows ----------------------------------------------------------------------

func (s *Server) latest(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !canSee(p) {
		writeJSON(w, []any{})
		return
	}
	var q queryBuilder
	kinds := []string{}
	for _, t := range splitList(param(r, "IncludeItemTypes"), ",") {
		kinds = append(kinds, kindsFor(t)...)
	}
	if len(kinds) > 0 {
		q.add("i.kind = ANY(?)", kinds)
	} else {
		q.conds = append(q.conds, "i.parent_id IS NULL")
	}
	if parent := param(r, "ParentId"); parent != "" {
		if pid, err := ParseID(parent); err == nil {
			q.add("(i.library_id = ? OR i.parent_id = ?)", pid)
		}
	}
	if v, ok := boolParam(r, "IsPlayed"); ok {
		q.add("COALESCE(u.played, false) = ?", v)
	}
	limit, _ := page(r, 20)
	rows, _, err := s.query(r.Context(), ownerOf(p), q.where(), q.args, "i.added_at DESC, i.id", min(limit, 100), 0)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	out, err := s.dtos(r.Context(), rows, parseFields(r))
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, orAny(out))
}

// resume lists started, unfinished movies and episodes (most recent first).
func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !canSee(p) {
		writeJSON(w, queryResult(nil, 0, 0))
		return
	}
	var q queryBuilder
	q.conds = append(q.conds, "u.position_ms > 0", "NOT u.played", "i.kind IN ('movie', 'episode', 'track')")
	if mt := param(r, "MediaTypes"); mt != "" && !strings.Contains(strings.ToLower(mt), "audio") {
		q.conds = append(q.conds, "i.kind <> 'track'")
	}
	if types := param(r, "IncludeItemTypes"); types != "" {
		kinds := []string{}
		for _, t := range splitList(types, ",") {
			kinds = append(kinds, kindsFor(t)...)
		}
		q.add("i.kind = ANY(?)", kinds)
	}
	if parent := param(r, "ParentId"); parent != "" {
		if pid, err := ParseID(parent); err == nil {
			q.add("i.library_id = ?", pid)
		}
	}
	if term := param(r, "SearchTerm"); term != "" {
		q.add("lower(COALESCE(i.metadata_title, i.title)) LIKE ?", "%"+strings.ToLower(term)+"%")
	}
	limit, offset := page(r, 20)
	rows, total, err := s.query(r.Context(), ownerOf(p), q.where(), q.args, "u.last_played_at DESC, i.id", limit, offset)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	s.writeRows(w, r, rows, total, offset)
}

// nextUp lists the next episode of each series in progress, or of one series.
func (s *Server) nextUp(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !canSee(p) {
		writeJSON(w, queryResult(nil, 0, 0))
		return
	}
	limit, _ := page(r, 20)
	var ids []uuid.UUID
	if series := param(r, "SeriesId"); series != "" {
		sid, err := ParseID(series)
		if err != nil {
			http.Error(w, "bad SeriesId", http.StatusBadRequest)
			return
		}
		// The first unwatched episode after the furthest watched one (or the first episode).
		var id uuid.UUID
		err = s.Pool.QueryRow(r.Context(), `WITH eps AS (
				SELECT e.id, COALESCE(e.season_number, 1) AS season, COALESCE(e.episode_number, 0) AS episode,
					COALESCE(x.played, false) AS played
				FROM items e LEFT JOIN items par ON par.id = e.parent_id
				LEFT JOIN user_item_data x ON x.item_id = e.id AND x.owner_id = $2
				WHERE e.kind = 'episode' AND (e.parent_id = $1 OR par.parent_id = $1) AND COALESCE(e.season_number, 1) > 0),
			last AS (SELECT season, episode FROM eps WHERE played ORDER BY season DESC, episode DESC LIMIT 1)
			SELECT id FROM eps WHERE NOT played
				AND (NOT EXISTS (SELECT 1 FROM last) OR (season, episode) > (SELECT season, episode FROM last))
			ORDER BY season, episode LIMIT 1`, sid, ownerOf(p)).Scan(&id)
		if err == nil {
			ids = append(ids, id)
		}
	} else {
		items, err := s.Libraries.NextUp(r.Context(), ownerOf(p), limit)
		if err != nil {
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
		for _, it := range items {
			ids = append(ids, it.ID)
		}
	}
	rows, err := s.byIDs(r.Context(), ownerOf(p), ids)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	s.writeRows(w, r, rows, len(rows), 0)
}

func (s *Server) seasons(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "seriesId"))
	if err != nil || !canSee(principal(r)) {
		http.NotFound(w, r)
		return
	}
	var q queryBuilder
	q.add("i.parent_id = ? AND i.kind = 'season'", id)
	if v, ok := boolParam(r, "IsPlayed"); ok {
		q.add("COALESCE(u.played, false) = ?", v)
	}
	rows, total, err := s.query(r.Context(), ownerOf(principal(r)), q.where(), q.args, "i.season_number NULLS LAST, i.sort_title", 500, 0)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	s.writeRows(w, r, rows, total, 0)
}

func (s *Server) episodes(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "seriesId"))
	if err != nil || !canSee(principal(r)) {
		http.NotFound(w, r)
		return
	}
	var q queryBuilder
	q.add("i.kind = 'episode' AND (i.parent_id = ? OR i.parent_id IN (SELECT id FROM items WHERE parent_id = ?))", id)
	if season := param(r, "SeasonId"); season != "" {
		if sid, err := ParseID(season); err == nil {
			q.add("i.parent_id = ?", sid)
		}
	} else if n := param(r, "Season"); n != "" {
		if num, err := strconv.Atoi(n); err == nil {
			q.add("i.season_number = ?", num)
		}
	}
	if start := param(r, "StartItemId"); start != "" {
		if sid, err := ParseID(start); err == nil {
			q.add(`(COALESCE(i.season_number, 1), COALESCE(i.episode_number, 0)) >= (SELECT COALESCE(season_number, 1),
				COALESCE(episode_number, 0) FROM items WHERE id = ?)`, sid)
		}
	}
	if v, ok := boolParam(r, "IsMissing"); ok && v {
		q.conds = append(q.conds, "false") // every listed episode has a file
	}
	if v := param(r, "AdjacentTo"); v != "" {
		if aid, err := ParseID(v); err == nil {
			q.add(`i.id IN (SELECT x.id FROM (
				SELECT c.id, lag(c.id) OVER w AS prev, lead(c.id) OVER w AS next FROM items c LEFT JOIN items cp ON cp.id = c.parent_id
				WHERE c.kind = 'episode' AND (c.parent_id = (SELECT parent_id FROM items WHERE id = ?) OR cp.parent_id = (
					SELECT CASE WHEN p2.kind = 'season' THEN p2.parent_id ELSE p2.id END FROM items e2 JOIN items p2 ON p2.id = e2.parent_id WHERE e2.id = ?))
				WINDOW w AS (ORDER BY COALESCE(c.season_number, 1), c.episode_number NULLS FIRST, c.sort_title)) x
				WHERE x.id = ? OR x.prev = ? OR x.next = ?)`, aid)
		}
	}
	limit, offset := page(r, 2000)
	order := orderBy(param(r, "SortBy"), param(r, "SortOrder"), "COALESCE(i.season_number, 1), i.episode_number NULLS FIRST, i.sort_title, i.id")
	rows, total, err := s.query(r.Context(), ownerOf(principal(r)), q.where(), q.args, order, limit, offset)
	if err != nil {
		s.Log.Warn("compat episodes query failed", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	s.writeRows(w, r, rows, total, offset)
}

// similar lists items of the same kind sharing a genre, best rated first.
func (s *Server) similar(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil || !canSee(principal(r)) {
		writeJSON(w, queryResult(nil, 0, 0))
		return
	}
	limit, _ := page(r, 12)
	rows, _, err := s.query(r.Context(), ownerOf(principal(r)), `i.id <> $1 AND i.parent_id IS NULL AND (i.kind, i.library_id) =
		(SELECT kind, library_id FROM items WHERE id = $1) AND i.genres && (SELECT genres FROM items WHERE id = $1)`,
		[]any{id}, "i.community_rating DESC NULLS LAST, i.sort_title, i.id", min(limit, 50), 0)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	s.writeRows(w, r, rows, len(rows), 0)
}

// ancestors lists the parents of an item up to its library.
func (s *Server) ancestors(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil || !canSee(principal(r)) {
		writeJSON(w, []any{})
		return
	}
	var parents []uuid.UUID
	var lib uuid.UUID
	err = s.Pool.QueryRow(r.Context(), `WITH RECURSIVE up AS (
			SELECT parent_id, library_id, 0 AS depth FROM items WHERE id = $1
			UNION ALL SELECT i.parent_id, i.library_id, up.depth + 1 FROM items i JOIN up ON i.id = up.parent_id)
		SELECT COALESCE(array_agg(parent_id ORDER BY depth) FILTER (WHERE parent_id IS NOT NULL), '{}'), min(library_id::text)::uuid FROM up`, id).
		Scan(&parents, &lib)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	rows, err := s.byIDs(r.Context(), ownerOf(principal(r)), parents)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	out := make([]any, 0, len(rows)+1)
	for _, x := range rows {
		out = append(out, s.dto(x))
	}
	if l, err := s.Libraries.Get(r.Context(), lib); err == nil {
		out = append(out, s.viewDTO(l))
	}
	writeJSON(w, out)
}

// searchHints implements /Search/Hints with Streamero's typo-tolerant search.
func (s *Server) searchHints(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !canSee(p) {
		writeJSON(w, map[string]any{"SearchHints": []any{}, "TotalRecordCount": 0})
		return
	}
	limit, _ := page(r, 40)
	found, err := s.Libraries.Search(r.Context(), ownerOf(p), param(r, "SearchTerm"), min(max(limit, 1), 100))
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	allowed := map[string]bool{}
	for _, t := range splitList(param(r, "IncludeItemTypes"), ",") {
		for _, k := range kindsFor(t) {
			allowed[k] = true
		}
	}
	var ids []uuid.UUID
	for _, it := range found {
		if library.IsStreamingKind(it.Kind) && (len(allowed) == 0 || allowed[it.Kind]) {
			ids = append(ids, it.ID)
		}
	}
	rows, err := s.byIDs(r.Context(), ownerOf(p), ids)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	hints := []any{}
	for _, x := range rows {
		d := s.dto(x)
		h := map[string]any{"ItemId": d["Id"], "Id": d["Id"], "Name": d["Name"], "Type": d["Type"], "MediaType": d["MediaType"],
			"Artists": []string{}, "IsFolder": d["IsFolder"], "MatchedTerm": param(r, "SearchTerm")}
		for _, k := range []string{"ProductionYear", "RunTimeTicks", "IndexNumber", "ParentIndexNumber", "SeriesId", "AlbumId", "Album", "AlbumArtist"} {
			if v, ok := d[k]; ok {
				h[k] = v
			}
		}
		if v, ok := d["SeriesName"]; ok {
			h["Series"] = v
		}
		if t, ok := d["ImageTags"].(map[string]string)["Primary"]; ok {
			h["PrimaryImageTag"] = t
		}
		if t, ok := d["ImageTags"].(map[string]string)["Thumb"]; ok {
			h["ThumbImageTag"], h["ThumbImageItemId"] = t, d["Id"]
		}
		if bt, ok := d["BackdropImageTags"].([]string); ok && len(bt) > 0 {
			h["BackdropImageTag"], h["BackdropImageItemId"] = bt[0], d["Id"]
		}
		hints = append(hints, h)
	}
	writeJSON(w, map[string]any{"SearchHints": hints, "TotalRecordCount": len(hints)})
}

// genres lists genres (optionally of one library) as items.
func (s *Server) genres(w http.ResponseWriter, r *http.Request) {
	if !canSee(principal(r)) {
		writeJSON(w, queryResult(nil, 0, 0))
		return
	}
	names, err := s.genreNames(r)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	out := []any{}
	for _, g := range names {
		out = append(out, map[string]any{"Name": g, "Id": GenreID(g), "ServerId": ID(s.ServerID), "Type": "Genre",
			"MediaType": "Unknown", "IsFolder": true, "ImageTags": map[string]string{}, "BackdropImageTags": []string{}})
	}
	writeJSON(w, queryResult(out, len(out), 0))
}

func (s *Server) genreNames(r *http.Request) ([]string, error) {
	where, args := "parent_id IS NULL", []any{}
	if parent := param(r, "ParentId"); parent != "" {
		if pid, err := ParseID(parent); err == nil {
			where, args = "parent_id IS NULL AND library_id = $1", []any{pid}
		}
	}
	// #nosec G202 -- fixed fragments
	rows, err := s.Pool.Query(r.Context(), `SELECT DISTINCT g FROM items, unnest(genres) g WHERE `+where+` ORDER BY g`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// filters implements /Items/Filters (genres, years, ratings) and /Items/Filters2.
func (s *Server) filters(legacy bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		names, err := s.genreNames(r)
		if err != nil || !canSee(principal(r)) {
			names = []string{}
		}
		if !legacy {
			gs := []any{}
			for _, g := range names {
				gs = append(gs, map[string]any{"Name": g, "Id": GenreID(g)})
			}
			writeJSON(w, map[string]any{"Genres": gs, "Tags": []string{}})
			return
		}
		years := []int{}
		ratings := []string{}
		if canSee(principal(r)) {
			rows, err := s.Pool.Query(r.Context(), `SELECT DISTINCT year FROM items WHERE year IS NOT NULL AND parent_id IS NULL ORDER BY year`)
			if err == nil {
				for rows.Next() {
					var y int
					if rows.Scan(&y) == nil {
						years = append(years, y)
					}
				}
				rows.Close()
			}
			rows, err = s.Pool.Query(r.Context(), `SELECT DISTINCT official_rating FROM items WHERE official_rating <> '' ORDER BY 1`)
			if err == nil {
				for rows.Next() {
					var v string
					if rows.Scan(&v) == nil {
						ratings = append(ratings, v)
					}
				}
				rows.Close()
			}
		}
		writeJSON(w, map[string]any{"Genres": names, "Tags": []string{}, "OfficialRatings": ratings, "Years": years})
	}
}

// emptyResult answers lists Streamero has nothing for (trailers, extras, themes).
func emptyResult(w http.ResponseWriter, _ *http.Request) { writeJSON(w, queryResult(nil, 0, 0)) }

func emptyList(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []any{}) }

func (s *Server) themeMedia(w http.ResponseWriter, r *http.Request) {
	owner := strings.ToLower(chi.URLParam(r, "itemId"))
	empty := map[string]any{"Items": []any{}, "TotalRecordCount": 0, "StartIndex": 0, "OwnerId": owner}
	writeJSON(w, map[string]any{"ThemeVideosResult": empty, "ThemeSongsResult": empty, "SoundtrackSongsResult": empty})
}

func (s *Server) themeList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"Items": []any{}, "TotalRecordCount": 0, "StartIndex": 0,
		"OwnerId": strings.ToLower(chi.URLParam(r, "itemId"))})
}

// --- images -------------------------------------------------------------------------

// imageKinds maps client image types to Streamero's cached artwork.
var imageKinds = map[string]string{"primary": "poster", "backdrop": "backdrop", "thumb": "backdrop", "logo": "logo"}

func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "itemId")
	if s.liveImage(w, r, raw, strings.ToLower(chi.URLParam(r, "type"))) {
		return
	}
	if s.Libraries.ImageDir == "" {
		http.NotFound(w, r)
		return
	}
	typ := strings.ToLower(chi.URLParam(r, "type"))
	var path string
	if n, ok := ParsePersonID(raw); ok {
		if typ != "primary" {
			http.NotFound(w, r)
			return
		}
		path = metadata.PersonImagePath(s.Libraries.ImageDir, n) // built from an integer
	} else {
		id, err := ParseID(raw)
		kind, ok := imageKinds[typ]
		if err != nil || !ok {
			http.NotFound(w, r)
			return
		}
		path = metadata.ImagePath(s.Libraries.ImageDir, id, kind) // built from a UUID and a fixed word
	}
	f, err := os.Open(path) // #nosec G304 G703 -- see above: no client text in the path
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// --- user data ------------------------------------------------------------------------

// setUserData applies played/favorite/position changes and answers the new state.
func (s *Server) setUserData(w http.ResponseWriter, r *http.Request, id uuid.UUID, favorite, played *bool, positionTicks *int64) {
	p := principal(r)
	if !canSee(p) {
		http.NotFound(w, r)
		return
	}
	if favorite != nil || played != nil {
		if err := s.Libraries.SetUserData(r.Context(), ownerOf(p), id, favorite, played); err != nil {
			if errors.Is(err, library.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
	}
	if positionTicks != nil && (played == nil || !*played) {
		if _, err := s.Pool.Exec(r.Context(), `INSERT INTO user_item_data (owner_id, item_id, position_ms)
			SELECT $1, $2, $3 WHERE EXISTS (SELECT 1 FROM items WHERE id = $2)
			ON CONFLICT (owner_id, item_id) DO UPDATE SET position_ms = EXCLUDED.position_ms, last_played_at = now()`,
			ownerOf(p), id, max(0, *positionTicks/10_000)); err != nil {
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
	}
	s.writeUserData(w, r, id)
}

func (s *Server) writeUserData(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	rows, _, err := s.query(r.Context(), ownerOf(principal(r)), "i.id = $1", []any{id}, "i.id", 1, 0)
	if err != nil || len(rows) == 0 {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, s.userData(rows[0]))
}

// markUserData handles POST/DELETE on PlayedItems and FavoriteItems.
func (s *Server) markUserData(field string, value bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := ParseID(chi.URLParam(r, "itemId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if field == "played" {
			s.setUserData(w, r, id, nil, &value, nil)
		} else {
			s.setUserData(w, r, id, &value, nil, nil)
		}
	}
}

func (s *Server) getUserData(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil || !canSee(principal(r)) {
		http.NotFound(w, r)
		return
	}
	s.writeUserData(w, r, id)
}

// postUserData implements POST /UserItems/{itemId}/UserData (partial update).
func (s *Server) postUserData(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Played                *bool  `json:"Played"`
		IsFavorite            *bool  `json:"IsFavorite"`
		PlaybackPositionTicks *int64 `json:"PlaybackPositionTicks"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.setUserData(w, r, id, body.IsFavorite, body.Played, body.PlaybackPositionTicks)
}
