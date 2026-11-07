// Package compat serves the subset of the Jellyfin-style HTTP API that request
// managers such as Seerr/Jellyseerr and the *arr "Emby/Jellyfin" connection use.
// It is written from the public OpenAPI description only (clean-room, D-011/D-016);
// responses always identify the product as Streamero.
package compat

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
)

// APIVersion is the API level this layer implements (what clients compare against).
const APIVersion = "10.10.0"

// Server is the compat HTTP surface, mounted under a prefix (default /compat).
type Server struct {
	Pool       *pgxpool.Pool
	Auth       *auth.Service
	Libraries  *library.Service
	ServerID   uuid.UUID
	ServerName func(ctx context.Context) string
	Log        *slog.Logger

	keysMu sync.Mutex
	keys   []recentKey // secrets of keys created here, readable for a short time
}

type recentKey struct {
	app, secret string
	created     time.Time
}

// recentKeyTTL bounds how long a freshly created key can be read back. Clients
// create a key and immediately list keys to learn it; Streamero itself only
// stores hashes, so the plaintext lives in memory for this window only.
const recentKeyTTL = 10 * time.Minute

// Routes returns the handler (paths are case-insensitive like the reference API).
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(lowercasePath)
	r.Get("/system/info/public", s.publicInfo)
	r.Get("/web", webRedirect)
	r.Get("/web/*", webRedirect)
	r.Post("/users/authenticatebyname", s.authenticateByName)
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/system/info", s.info)
		r.Get("/users/me", s.me)
		r.Get("/users", s.users)
		r.Get("/users/{userId}", s.user)
		r.Get("/library/mediafolders", s.views)
		r.Get("/users/{userId}/views", s.views)
		r.Get("/items", s.items)
		r.Get("/users/{userId}/items", s.items)
		r.Get("/items/{itemId}", s.item)
		r.Get("/users/{userId}/items/latest", s.latest)
		r.Get("/users/{userId}/items/{itemId}", s.item)
		r.Get("/shows/{seriesId}/seasons", s.seasons)
		r.Get("/shows/{seriesId}/episodes", s.episodes)
		r.Get("/items/{itemId}/images/{type}", s.image)
		r.Post("/library/refresh", s.refreshAll)
		r.Post("/library/media/updated", s.mediaUpdated)
		r.Post("/library/movies/updated", s.mediaUpdated)
		r.Post("/library/series/updated", s.mediaUpdated)
		r.Post("/sessions/logout", s.logout)
		r.Get("/auth/keys", s.listKeys)
		r.Post("/auth/keys", s.createKey)
	})
	return r
}

// lowercasePath makes routing case-insensitive (clients send "/Users/AuthenticateByName").
// Path values are hex IDs, so lowercasing them is harmless.
func lowercasePath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePath != "" {
			rctx.RoutePath = strings.ToLower(rctx.RoutePath)
		} else {
			r.URL.Path = strings.ToLower(r.URL.Path)
			r.URL.RawPath = ""
		}
		next.ServeHTTP(w, r)
	})
}

// --- auth -------------------------------------------------------------------

type ctxKey struct{}

var tokenRe = regexp.MustCompile(`(?i)\btoken="([^"]*)"`)

// clientInfo parses `Authorization: MediaBrowser Client="…", Device="…", Token="…"`.
func clientInfo(r *http.Request) (token, client, device string) {
	h := r.Header.Get("Authorization")
	if h == "" {
		h = r.Header.Get("X-Emby-Authorization")
	}
	field := func(name string) string {
		m := regexp.MustCompile(`(?i)\b` + name + `="([^"]*)"`).FindStringSubmatch(h)
		if m == nil {
			return ""
		}
		return m[1]
	}
	if m := tokenRe.FindStringSubmatch(h); m != nil {
		token = m[1]
	}
	if token == "" {
		for _, k := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
			if v := r.Header.Get(k); v != "" {
				token = v
			}
		}
	}
	if token == "" {
		token = r.URL.Query().Get("api_key")
		if token == "" {
			token = r.URL.Query().Get("ApiKey")
		}
	}
	if token == "" && strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimPrefix(h, "Bearer ")
	}
	return token, field("Client"), field("Device")
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _, _ := clientInfo(r)
		p, err := s.Auth.Authenticate(r.Context(), token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

func principal(r *http.Request) auth.Principal {
	p, _ := r.Context().Value(ctxKey{}).(auth.Principal)
	return p
}

// --- helpers ----------------------------------------------------------------

// ID formats a UUID the way clients of this API expect (32 hex digits).
func ID(u uuid.UUID) string { return strings.ReplaceAll(u.String(), "-", "") }

// ParseID accepts dashed and undashed forms.
func ParseID(s string) (uuid.UUID, error) { return uuid.Parse(s) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) name(ctx context.Context) string {
	if s.ServerName != nil {
		return s.ServerName(ctx)
	}
	return "Streamero"
}

// --- system & users -----------------------------------------------------------

func (s *Server) publicInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"LocalAddress": "", "ServerName": s.name(r.Context()), "Version": APIVersion,
		"ProductName": "Streamero", "OperatingSystem": "", "Id": ID(s.ServerID), "StartupWizardCompleted": true,
	})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ServerName": s.name(r.Context()), "Version": APIVersion, "ProductName": "Streamero", "Id": ID(s.ServerID),
		"StartupWizardCompleted": true, "OperatingSystem": "Linux",
	})
}

func (s *Server) userDTO(u auth.User) map[string]any {
	last := ""
	if u.LastLoginAt != nil {
		last = u.LastLoginAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"Name": u.Username, "ServerId": ID(s.ServerID), "Id": ID(u.ID), "HasPassword": true,
		"HasConfiguredPassword": true, "EnableAutoLogin": false, "LastLoginDate": last,
		"Policy": map[string]any{
			"IsAdministrator": u.IsAdmin, "IsDisabled": u.Disabled, "IsHidden": false,
			"EnableAllFolders": u.IsAdmin || u.Permissions.AllLibraries, "EnabledFolders": []string{},
		},
		"Configuration": map[string]any{},
	}
}

func (s *Server) authenticateByName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"Username"`
		Pw       string `json:"Pw"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_, client, device := clientInfo(r)
	if client == "" {
		client = "Compat client"
	}
	p, token, err := s.Auth.Login(r.Context(), body.Username, body.Pw, "", auth.Device{Name: device, Client: client})
	if err != nil {
		var locked *auth.LockedError
		switch {
		case errors.As(err, &locked):
			http.Error(w, "account locked", http.StatusTooManyRequests)
		case errors.Is(err, auth.ErrTOTPRequired):
			http.Error(w, "two-factor accounts cannot sign in here; use an API key", http.StatusUnauthorized)
		default:
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
		}
		return
	}
	writeJSON(w, map[string]any{
		"User": s.userDTO(p.User), "AccessToken": token, "ServerId": ID(s.ServerID),
		"SessionInfo": map[string]any{"Id": ID(p.SessionID), "UserId": ID(p.User.ID), "UserName": p.User.Username,
			"Client": client, "DeviceName": device},
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_ = s.Auth.Logout(r.Context(), principal(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.userDTO(principal(r).User))
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.IsAdmin {
		writeJSON(w, []any{s.userDTO(p.User)})
		return
	}
	users, err := s.Auth.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	out := make([]any, 0, len(users))
	for _, u := range users {
		out = append(out, s.userDTO(u))
	}
	writeJSON(w, out)
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	id, err := ParseID(chi.URLParam(r, "userId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if id == p.User.ID {
		writeJSON(w, s.userDTO(p.User))
		return
	}
	if !p.User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	users, err := s.Auth.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	for _, u := range users {
		if u.ID == id {
			writeJSON(w, s.userDTO(u))
			return
		}
	}
	http.NotFound(w, r)
}

func canSee(p auth.Principal) bool { return p.User.IsAdmin || p.User.Permissions.AllLibraries }

// --- libraries & items ------------------------------------------------------

func collectionType(kind string) string {
	switch kind {
	case "movies":
		return "movies"
	case "shows":
		return "tvshows"
	case "music":
		return "music"
	}
	return "mixed"
}

func (s *Server) views(w http.ResponseWriter, r *http.Request) {
	out := []any{}
	if canSee(principal(r)) {
		libs, err := s.Libraries.List(r.Context())
		if err != nil {
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
		for _, l := range libs {
			out = append(out, map[string]any{"Name": l.Name, "Id": ID(l.ID), "ServerId": ID(s.ServerID),
				"Type": "CollectionFolder", "CollectionType": collectionType(l.Kind), "IsFolder": true,
				"ChildCount": l.ItemCount, "LocationType": "FileSystem"})
		}
	}
	writeJSON(w, map[string]any{"Items": out, "TotalRecordCount": len(out), "StartIndex": 0})
}

var typeMap = map[string]string{"movie": "Movie", "series": "Series", "season": "Season", "episode": "Episode"}

type row struct {
	id, libraryID                   uuid.UUID
	parentID, seriesID              *uuid.UUID
	kind, title                     string
	year, season, episode           *int
	added                           time.Time
	ids                             map[string]string
	premiere                        *time.Time
	overview                        *string
	rating                          *float64
	maxWidth, maxHeight, filesCount int
	durationMS                      *int64
	hasPoster                       bool
	played, favorite                bool
	position                        int64
}

const rowCols = `i.id, i.library_id, i.parent_id,
	CASE i.kind WHEN 'episode' THEN (SELECT p.parent_id FROM items p WHERE p.id = i.parent_id) WHEN 'season' THEN i.parent_id END,
	i.kind, COALESCE(i.metadata_title, i.title), i.year, i.season_number, i.episode_number, i.added_at, i.provider_ids,
	i.premiere_date, i.overview, i.community_rating,
	COALESCE((SELECT max(f.width) FROM media_files f WHERE f.item_id = i.id), 0),
	COALESCE((SELECT max(f.height) FROM media_files f WHERE f.item_id = i.id), 0),
	(SELECT count(*) FROM media_files f WHERE f.item_id = i.id),
	(SELECT max(f.duration_ms) FROM media_files f WHERE f.item_id = i.id), i.has_poster,
	COALESCE(u.played, false), COALESCE(u.favorite, false), COALESCE(u.position_ms, 0)`

func (s *Server) query(ctx context.Context, owner uuid.UUID, where string, args []any, order string, limit, offset int) ([]row, int, error) {
	n := len(args)
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM items i LEFT JOIN user_item_data u ON u.item_id = i.id AND u.owner_id = $`+
		strconv.Itoa(n+1)+` WHERE `+where, append(args, owner)...).Scan(&total); err != nil {
		return nil, 0, err
	}
	// #nosec G202 -- where/order are built from fixed fragments with numbered parameters only
	rows, err := s.Pool.Query(ctx, `SELECT `+rowCols+` FROM items i LEFT JOIN user_item_data u ON u.item_id = i.id AND u.owner_id = $`+
		strconv.Itoa(n+1)+` WHERE `+where+` ORDER BY `+order+` LIMIT $`+strconv.Itoa(n+2)+` OFFSET $`+strconv.Itoa(n+3),
		append(args, owner, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		var x row
		var ids []byte
		if err := rows.Scan(&x.id, &x.libraryID, &x.parentID, &x.seriesID, &x.kind, &x.title, &x.year, &x.season, &x.episode,
			&x.added, &ids, &x.premiere, &x.overview, &x.rating, &x.maxWidth, &x.maxHeight, &x.filesCount, &x.durationMS,
			&x.hasPoster, &x.played, &x.favorite, &x.position); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(ids, &x.ids)
		out = append(out, x)
	}
	return out, total, rows.Err()
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
		}
	}
	d := map[string]any{
		"Name": x.title, "Id": ID(x.id), "ServerId": ID(s.ServerID), "Type": typeMap[x.kind],
		"IsFolder": x.kind == "series" || x.kind == "season", "ProviderIds": providers, "LocationType": "FileSystem",
		"DateCreated": x.added.UTC().Format(time.RFC3339), "MediaType": "Video",
		"UserData": map[string]any{"Played": x.played, "IsFavorite": x.favorite, "PlaybackPositionTicks": x.position * 10_000},
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
	if x.rating != nil {
		d["CommunityRating"] = *x.rating
	}
	if x.durationMS != nil {
		d["RunTimeTicks"] = *x.durationMS * 10_000 // ticks are 100 ns
	}
	if x.hasPoster {
		d["ImageTags"] = map[string]string{"Primary": ID(x.id)}
	}
	if x.parentID != nil {
		d["ParentId"] = ID(*x.parentID)
	}
	switch x.kind {
	case "season":
		if x.season != nil {
			d["IndexNumber"] = *x.season
		}
		if x.seriesID != nil {
			d["SeriesId"] = ID(*x.seriesID)
		}
	case "episode":
		if x.episode != nil {
			d["IndexNumber"] = *x.episode
		}
		if x.season != nil {
			d["ParentIndexNumber"] = *x.season
		}
		if x.parentID != nil {
			d["SeasonId"] = ID(*x.parentID)
		}
		if x.seriesID != nil {
			d["SeriesId"] = ID(*x.seriesID)
		}
	}
	if x.filesCount > 0 {
		d["MediaSources"] = []any{map[string]any{"Id": ID(x.id), "Protocol": "File", "Type": "Default",
			"MediaStreams": []any{map[string]any{"Type": "Video", "Width": x.maxWidth, "Height": x.maxHeight}}}}
		d["Width"], d["Height"] = x.maxWidth, x.maxHeight
	}
	return d
}

func intParam(r *http.Request, name string, def, maxV int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || v < 0 {
		return def
	}
	return min(v, maxV)
}

// param reads a query parameter case-insensitively.
func param(r *http.Request, name string) string {
	for k, v := range r.URL.Query() {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !canSee(p) {
		writeJSON(w, map[string]any{"Items": []any{}, "TotalRecordCount": 0, "StartIndex": 0})
		return
	}
	var conds []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if ids := param(r, "Ids"); ids != "" {
		var list []uuid.UUID
		for _, s := range strings.Split(ids, ",") {
			if u, err := ParseID(strings.TrimSpace(s)); err == nil {
				list = append(list, u)
			}
		}
		add("i.id = ANY(?)", list)
	}
	if types := param(r, "IncludeItemTypes"); types != "" {
		var kinds []string
		for _, t := range strings.Split(types, ",") {
			for k, v := range typeMap {
				if strings.EqualFold(v, strings.TrimSpace(t)) {
					kinds = append(kinds, k)
				}
			}
		}
		add("i.kind = ANY(?)", kinds)
	}
	recursive := strings.EqualFold(param(r, "Recursive"), "true")
	if parent := param(r, "ParentId"); parent != "" {
		pid, err := ParseID(parent)
		if err != nil {
			http.Error(w, "bad ParentId", http.StatusBadRequest)
			return
		}
		var isLibrary bool
		_ = s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM libraries WHERE id = $1)`, pid).Scan(&isLibrary)
		switch {
		case isLibrary && recursive:
			add("i.library_id = ?", pid)
		case isLibrary:
			add("i.library_id = ? AND i.parent_id IS NULL", pid)
		case recursive: // both placeholders become the same parameter
			add("(i.parent_id = ? OR i.parent_id IN (SELECT id FROM items WHERE parent_id = ?))", pid)
		default:
			add("i.parent_id = ?", pid)
		}
	} else if !recursive && param(r, "Ids") == "" {
		conds = append(conds, "i.parent_id IS NULL")
	}
	if term := param(r, "SearchTerm"); term != "" {
		add("lower(COALESCE(i.metadata_title, i.title)) LIKE ?", "%"+strings.ToLower(term)+"%")
	}
	if len(conds) == 0 {
		conds = append(conds, "true")
	}
	order := "i.sort_title, i.season_number NULLS FIRST, i.episode_number NULLS FIRST"
	if strings.Contains(strings.ToLower(param(r, "SortBy")), "datecreated") {
		order = "i.added_at DESC"
	}
	limit := 500
	if v, err := strconv.Atoi(param(r, "Limit")); err == nil {
		limit = max(0, min(v, 2000))
	}
	offset := 0
	if v := param(r, "StartIndex"); v != "" {
		offset, _ = strconv.Atoi(v)
		offset = max(0, offset)
	}
	rows, total, err := s.query(r.Context(), ownerOf(p), strings.Join(conds, " AND "), args, order, limit, offset)
	if err != nil {
		s.Log.Warn("compat items query failed", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	out := make([]any, 0, len(rows))
	for _, x := range rows {
		out = append(out, s.dto(x))
	}
	writeJSON(w, map[string]any{"Items": out, "TotalRecordCount": total, "StartIndex": offset})
}

func ownerOf(p auth.Principal) uuid.UUID {
	if p.Profile != nil {
		return p.Profile.ID
	}
	return p.User.ID
}

func (s *Server) one(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
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
	writeJSON(w, s.dto(rows[0]))
}

func (s *Server) item(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// A library ID asked as an item (clients do this for views).
	var lib library.Library
	if lib, err = s.Libraries.Get(r.Context(), id); err == nil {
		writeJSON(w, map[string]any{"Name": lib.Name, "Id": ID(lib.ID), "Type": "CollectionFolder",
			"CollectionType": collectionType(lib.Kind), "IsFolder": true, "ServerId": ID(s.ServerID)})
		return
	}
	s.one(w, r, id)
}

func (s *Server) latest(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !canSee(p) {
		writeJSON(w, []any{})
		return
	}
	where, args := "i.parent_id IS NULL", []any{}
	if parent := param(r, "ParentId"); parent != "" {
		if pid, err := ParseID(parent); err == nil {
			where, args = "i.parent_id IS NULL AND i.library_id = $1", []any{pid}
		}
	}
	rows, _, err := s.query(r.Context(), ownerOf(p), where, args, "i.added_at DESC", intParam(r, "Limit", 20, 100), 0)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	out := make([]any, 0, len(rows))
	for _, x := range rows {
		out = append(out, s.dto(x))
	}
	writeJSON(w, out)
}

func (s *Server) seasons(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "seriesId"))
	if err != nil || !canSee(principal(r)) {
		http.NotFound(w, r)
		return
	}
	rows, total, err := s.query(r.Context(), ownerOf(principal(r)), "i.parent_id = $1 AND i.kind = 'season'", []any{id},
		"i.season_number", 500, 0)
	s.list(w, rows, total, err)
}

func (s *Server) episodes(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "seriesId"))
	if err != nil || !canSee(principal(r)) {
		http.NotFound(w, r)
		return
	}
	where, args := "i.kind = 'episode' AND i.parent_id IN (SELECT id FROM items WHERE parent_id = $1)", []any{id}
	if season := param(r, "SeasonId"); season != "" {
		if sid, err := ParseID(season); err == nil {
			where, args = "i.kind = 'episode' AND i.parent_id = $1", []any{sid}
		}
	} else if n := param(r, "Season"); n != "" {
		if num, err := strconv.Atoi(n); err == nil {
			where = "i.kind = 'episode' AND i.parent_id IN (SELECT id FROM items WHERE parent_id = $1 AND season_number = $2)"
			args = append(args, num)
		}
	}
	rows, total, err := s.query(r.Context(), ownerOf(principal(r)), where, args, "i.season_number, i.episode_number", 2000, 0)
	s.list(w, rows, total, err)
}

func (s *Server) list(w http.ResponseWriter, rows []row, total int, err error) {
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	out := make([]any, 0, len(rows))
	for _, x := range rows {
		out = append(out, s.dto(x))
	}
	writeJSON(w, map[string]any{"Items": out, "TotalRecordCount": total, "StartIndex": 0})
}

func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil || !canSee(principal(r)) || s.Libraries.ImageDir == "" {
		http.NotFound(w, r)
		return
	}
	kind := "poster"
	if strings.EqualFold(chi.URLParam(r, "type"), "backdrop") {
		kind = "backdrop"
	}
	// Path from a parsed UUID and a fixed word only.
	http.ServeFile(w, r, metadata.ImagePath(s.Libraries.ImageDir, id, kind)) // #nosec G703 -- UUID + fixed word
}

// --- library change notifications (*arr "Emby/Jellyfin" connection) ---------

func (s *Server) refreshAll(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	libs, err := s.Libraries.List(r.Context())
	if err == nil {
		for _, l := range libs {
			_ = s.Libraries.Scan(r.Context(), l.ID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// mediaUpdated scans the libraries containing the reported paths (or all when
// the paths are unknown, e.g. the sender sees a different mount point).
func (s *Server) mediaUpdated(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Updates []struct {
			Path string `json:"Path"`
		} `json:"Updates"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
	libs, err := s.Libraries.List(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	queued := map[uuid.UUID]bool{}
	for _, u := range body.Updates {
		p := filepath.Clean(u.Path)
		for _, l := range libs {
			for _, root := range l.Paths {
				if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
					queued[l.ID] = true
				}
			}
		}
	}
	if len(queued) == 0 { // unknown paths (different mount points): scan everything
		for _, l := range libs {
			queued[l.ID] = true
		}
	}
	for id := range queued {
		_ = s.Libraries.Scan(r.Context(), id)
	}
	s.Log.Info("library update notification", "libraries", len(queued), "paths", len(body.Updates))
	w.WriteHeader(http.StatusNoContent)
}

// webRedirect turns "…/web/#/details?id=<32 hex>" links (as built by request
// managers for "Play on") into the Streamero item page. The id is read in the
// browser because URL fragments never reach the server.
func webRedirect(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+webRedirectHash+"'")
	_, _ = w.Write([]byte(webRedirectHTML))
}

const webRedirectScript = `var m=(location.hash+location.search).match(/[?&]id=([0-9a-fA-F-]{32,36})/);` +
	`if(m){var h=m[1].replace(/-/g,"").toLowerCase();` +
	`location.replace("/item/"+h.slice(0,8)+"-"+h.slice(8,12)+"-"+h.slice(12,16)+"-"+h.slice(16,20)+"-"+h.slice(20));}` +
	`else{location.replace("/");}`

// webRedirectHash allows exactly this inline script in the CSP.
var webRedirectHash = func() string {
	sum := sha256.Sum256([]byte(webRedirectScript))
	return base64.StdEncoding.EncodeToString(sum[:])
}()

const webRedirectHTML = `<!doctype html><meta charset="utf-8"><title>Streamero</title><script>` + webRedirectScript + `</script>`

// createKey implements POST /Auth/Keys?App=<name> (admin): an integration API key.
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	app := strings.TrimSpace(param(r, "App"))
	if app == "" {
		app = "Integration"
	}
	_, secret, err := s.Auth.CreateAPIKey(r.Context(), p, app)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	s.keysMu.Lock()
	now := time.Now()
	kept := s.keys[:0]
	for _, k := range s.keys {
		if now.Sub(k.created) < recentKeyTTL {
			kept = append(kept, k)
		}
	}
	s.keys = append(kept, recentKey{app: app, secret: secret, created: now})
	s.keysMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// listKeys implements GET /Auth/Keys (admin): keys created here in the last minutes.
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.keysMu.Lock()
	out := []any{}
	now := time.Now()
	for _, k := range s.keys {
		if now.Sub(k.created) < recentKeyTTL {
			out = append(out, map[string]any{"AccessToken": k.secret, "AppName": k.app,
				"DateCreated": k.created.UTC().Format(time.RFC3339), "IsActive": true})
		}
	}
	s.keysMu.Unlock()
	writeJSON(w, map[string]any{"Items": out, "TotalRecordCount": len(out), "StartIndex": 0})
}
