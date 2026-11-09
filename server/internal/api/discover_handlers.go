package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/requests"
)

const tmdbImage = "https://image.tmdb.org/t/p/"

func (h *Handler) requestSettings(ctx context.Context) requests.Settings {
	s := requests.Settings{UsersCanRequest: true}
	if h.Settings != nil && h.Settings() != nil {
		_, _ = h.Settings().Get(ctx, requests.KeySettings, &s)
	}
	return s
}

func (h *Handler) requestsClient() *requests.Client {
	if h.Requests != nil {
		return h.Requests
	}
	return &requests.Client{}
}

func toRequestSettings(s requests.Settings) *gen.RequestSettings {
	return &gen.RequestSettings{SeerrUrl: optString(s.SeerrURL), SeerrConfigured: s.SeerrReady(), UsersCanRequest: s.UsersCanRequest,
		RadarrUrl: optString(s.RadarrURL), RadarrConfigured: s.RadarrURL != "" && s.RadarrKey != "",
		SonarrUrl: optString(s.SonarrURL), SonarrConfigured: s.SonarrURL != "" && s.SonarrKey != ""}
}

// GetRequestSettings implements GET /settings/requests.
func (h *Handler) GetRequestSettings(ctx context.Context) (*gen.RequestSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return toRequestSettings(h.requestSettings(ctx)), nil
}

// UpdateRequestSettings implements PUT /settings/requests.
func (h *Handler) UpdateRequestSettings(ctx context.Context, req *gen.RequestSettingsInput) (*gen.RequestSettings, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	s := h.requestSettings(ctx)
	bad := func(err error) error {
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_url", Message: err.Error()}
	}
	set := func(url *string, key *string, inURL, inKey gen.OptString) error {
		if v, ok := inURL.Get(); ok {
			clean, err := requests.CleanURL(v)
			if err != nil {
				return bad(err)
			}
			*url = clean
		}
		if v := strings.TrimSpace(inKey.Value); v != "" {
			*key = v
		}
		return nil
	}
	if err := set(&s.SeerrURL, &s.SeerrKey, req.SeerrUrl, req.SeerrApiKey); err != nil {
		return nil, err
	}
	if err := set(&s.RadarrURL, &s.RadarrKey, req.RadarrUrl, req.RadarrApiKey); err != nil {
		return nil, err
	}
	if err := set(&s.SonarrURL, &s.SonarrKey, req.SonarrUrl, req.SonarrApiKey); err != nil {
		return nil, err
	}
	for _, k := range req.RemoveKeys {
		switch k {
		case gen.RequestSettingsInputRemoveKeysItemSeerr:
			s.SeerrKey = ""
		case gen.RequestSettingsInputRemoveKeysItemRadarr:
			s.RadarrKey = ""
		case gen.RequestSettingsInputRemoveKeysItemSonarr:
			s.SonarrKey = ""
		}
	}
	s.UsersCanRequest = req.UsersCanRequest
	if err := h.Settings().Set(ctx, requests.KeySettings, s); err != nil {
		return nil, err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "settings.requests", nil)
	return toRequestSettings(s), nil
}

// TestRequestSettings implements POST /settings/requests/test.
func (h *Handler) TestRequestSettings(ctx context.Context) ([]gen.ConnectionCheck, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	s := h.requestSettings(ctx)
	out := []gen.ConnectionCheck{}
	for _, c := range []struct{ app, url, key string }{{"Seerr", s.SeerrURL, s.SeerrKey}, {"Radarr", s.RadarrURL, s.RadarrKey}, {"Sonarr", s.SonarrURL, s.SonarrKey}} {
		if c.url == "" {
			continue
		}
		check := gen.ConnectionCheck{App: c.app}
		if c.key == "" {
			check.Error = gen.NewOptString("no API key")
		} else if v, err := h.requestsClient().Ping(ctx, c.app, c.url, c.key); err != nil {
			check.Error = gen.NewOptString(err.Error())
		} else {
			check.Ok, check.Version = true, optString(v)
		}
		out = append(out, check)
	}
	return out, nil
}

func canRequest(p auth.Principal, s requests.Settings) bool {
	if p.Profile != nil && p.Profile.IsKid {
		return false
	}
	return s.SeerrReady() && (p.User.IsAdmin || s.UsersCanRequest)
}

// uiLanguage is the language Seerr should answer in.
func (h *Handler) uiLanguage(ctx context.Context, p auth.Principal) string {
	if a, err := h.Auth().Account(ctx, p.User.ID); err == nil && a.Language != "" {
		return a.Language
	}
	return "en"
}

// Discover implements GET /discover.
func (h *Handler) Discover(ctx context.Context, params gen.DiscoverParams) (*gen.DiscoverPage, error) {
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	s := h.requestSettings(ctx)
	out := &gen.DiscoverPage{Configured: s.SeerrReady(), CanRequest: canRequest(p, s), Titles: []gen.DiscoverTitle{}, Page: 1, TotalPages: 1}
	if !s.SeerrReady() {
		return out, nil
	}
	lang := h.uiLanguage(ctx, p)
	page := params.Page.Or(1)
	var res requests.Page
	if q := strings.TrimSpace(params.Q.Value); q != "" {
		res, err = h.requestsClient().Search(ctx, s, q, lang, page)
	} else {
		res, err = h.requestsClient().Trending(ctx, s, lang, page)
	}
	if err != nil {
		return nil, upstream(err)
	}
	out.Page, out.TotalPages = max(res.Page, 1), max(res.TotalPages, 1)
	local := h.localByTMDb(ctx, res.Titles)
	for _, t := range res.Titles {
		dt := gen.DiscoverTitle{TmdbId: t.TMDbID, MediaType: gen.DiscoverTitleMediaType(t.MediaType), Title: t.Title,
			Overview: optString(t.Overview), Status: gen.DiscoverTitleStatus(t.Status)}
		if t.Year > 0 {
			dt.Year = gen.NewOptInt(t.Year)
		}
		if t.PosterPath != "" {
			dt.PosterUrl = gen.NewOptString(tmdbImage + "w342" + t.PosterPath)
		}
		if t.BackdropPath != "" {
			dt.BackdropUrl = gen.NewOptString(tmdbImage + "w780" + t.BackdropPath)
		}
		if t.Rating > 0 {
			dt.Rating = gen.NewOptFloat64(t.Rating)
		}
		if id, ok := local[t.MediaType+":"+strconv.Itoa(t.TMDbID)]; ok {
			dt.ItemId = gen.NewOptUUID(id)
			dt.Status = gen.DiscoverTitleStatusAvailable
		}
		out.Titles = append(out.Titles, dt)
	}
	return out, nil
}

// localByTMDb finds which results are already in the libraries.
func (h *Handler) localByTMDb(ctx context.Context, titles []requests.Title) map[string]uuid.UUID {
	out := map[string]uuid.UUID{}
	if h.lib() == nil || len(titles) == 0 {
		return out
	}
	ids := make([]string, 0, len(titles))
	for _, t := range titles {
		ids = append(ids, strconv.Itoa(t.TMDbID))
	}
	rows, err := h.lib().Pool.Query(ctx, `SELECT id, kind, provider_ids->>'tmdb' FROM items
		WHERE parent_id IS NULL AND kind IN ('movie', 'series') AND provider_ids->>'tmdb' = ANY($1)`, ids)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var kind, tmdb string
		if rows.Scan(&id, &kind, &tmdb) == nil {
			mt := "movie"
			if kind == "series" {
				mt = "tv"
			}
			out[mt+":"+tmdb] = id
		}
	}
	return out
}

func upstream(err error) error {
	var up *requests.ErrUpstream
	if errors.As(err, &up) {
		return &StatusError{Status: http.StatusBadGateway, Code: "upstream_failed", Message: up.Error()}
	}
	if errors.Is(err, requests.ErrNotConfigured) {
		return &StatusError{Status: http.StatusConflict, Code: "not_configured", Message: "this connection is not set up"}
	}
	return err
}

// RequestTitle implements POST /discover/request.
func (h *Handler) RequestTitle(ctx context.Context, req *gen.TitleRequest) (*gen.TitleRequestResult, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	s := h.requestSettings(ctx)
	if !canRequest(p, s) {
		return nil, &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: "requests are not allowed for this account"}
	}
	st, err := h.requestsClient().Request(ctx, s, string(req.MediaType), req.TmdbId)
	if err != nil {
		return nil, upstream(err)
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "title.requested", map[string]any{"tmdbId": req.TmdbId, "type": string(req.MediaType)})
	return &gen.TitleRequestResult{Status: gen.TitleRequestResultStatus(st)}, nil
}

// ListUpcoming implements GET /upcoming.
func (h *Handler) ListUpcoming(ctx context.Context, params gen.ListUpcomingParams) ([]gen.UpcomingRelease, error) {
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	s := h.requestSettings(ctx)
	out := []gen.UpcomingRelease{}
	if s.RadarrURL == "" && s.SonarrURL == "" {
		return out, nil
	}
	from := time.Now().UTC().Truncate(24 * time.Hour)
	list, err := h.requestsClient().Calendar(ctx, s, from, from.AddDate(0, 0, params.Days.Or(30)))
	if err != nil {
		return nil, upstream(err)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Date.Before(list[j].Date) })
	for _, u := range list {
		r := gen.UpcomingRelease{Kind: gen.UpcomingReleaseKind(u.Kind), Title: u.Title, Date: u.Date, Downloaded: u.Downloaded,
			SeriesTitle: optString(u.SeriesTitle), Overview: optString(u.Overview), PosterUrl: optString(u.PosterURL)}
		if u.Kind == "episode" {
			r.Season, r.Episode = gen.NewOptInt(u.Season), gen.NewOptInt(u.Episode)
		}
		out = append(out, r)
	}
	return out, nil
}
