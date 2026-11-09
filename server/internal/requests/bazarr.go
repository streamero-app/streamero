package requests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Bazarr finds and downloads subtitles for what Radarr and Sonarr manage. Its API
// addresses titles by Radarr movie id and Sonarr episode id, so a Streamero item is
// looked up in Radarr/Sonarr first. Downloaded subtitles land next to the video
// file, where the library scan picks them up.

// BazarrReady reports whether subtitles can be searched for movies (Radarr) or episodes (Sonarr).
func (s Settings) BazarrReady() bool { return s.BazarrURL != "" && s.BazarrKey != "" }

// SubtitleTarget identifies a title in Bazarr.
type SubtitleTarget struct {
	Kind     string // movie | episode
	ID       int    // Radarr movie id or Sonarr episode id
	SeriesID int    // Sonarr series id (episodes)
}

// SubtitleOffer is one subtitle a provider has for the title.
type SubtitleOffer struct {
	Provider       string   `json:"provider"`
	Language       string   `json:"language"`
	Forced         bool     `json:"forced"`
	HearingImpair  bool     `json:"hearingImpaired"`
	Score          float64  `json:"score"`
	Release        []string `json:"release"`
	Uploader       string   `json:"uploader,omitempty"`
	Subtitle       string   `json:"subtitle"` // Bazarr's opaque handle, passed back to download
	OriginalFormat bool     `json:"originalFormat"`
}

func (c *Client) bazarr(ctx context.Context, s Settings, method, path string, q url.Values, out any) error {
	if !s.BazarrReady() {
		return ErrNotConfigured
	}
	u := s.BazarrURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-KEY", s.BazarrKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http().Do(req) //nolint:gosec // G704: base URL is set by an administrator
	if err != nil {
		return &ErrUpstream{App: "Bazarr", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return &ErrUpstream{App: "Bazarr", Status: resp.StatusCode}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// BazarrVersion checks the connection.
func (c *Client) BazarrVersion(ctx context.Context, s Settings) (string, error) {
	var out struct {
		Data struct {
			Version string `json:"bazarr_version"`
		} `json:"data"`
	}
	if err := c.bazarr(ctx, s, http.MethodGet, "/api/system/status", nil, &out); err != nil {
		return "", err
	}
	return out.Data.Version, nil
}

func (t SubtitleTarget) params() (string, url.Values) {
	if t.Kind == "episode" {
		return "episodes", url.Values{"episodeid": {strconv.Itoa(t.ID)}}
	}
	return "movies", url.Values{"radarrid": {strconv.Itoa(t.ID)}}
}

// SearchSubtitles asks every Bazarr provider for subtitles of the title (can take a while).
func (c *Client) SearchSubtitles(ctx context.Context, s Settings, t SubtitleTarget) ([]SubtitleOffer, error) {
	kind, q := t.params()
	var out struct {
		Data []struct {
			Provider       string   `json:"provider"`
			Language       string   `json:"language"`
			Forced         any      `json:"forced"`
			HearingImpair  any      `json:"hearing_impaired"`
			Score          float64  `json:"score"`
			ReleaseInfo    []string `json:"release_info"`
			Uploader       string   `json:"uploader"`
			Subtitle       string   `json:"subtitle"`
			OriginalFormat any      `json:"original_format"`
		} `json:"data"`
	}
	if err := c.bazarr(ctx, s, http.MethodGet, "/api/providers/"+kind, q, &out); err != nil {
		return nil, err
	}
	offers := make([]SubtitleOffer, 0, len(out.Data))
	for _, d := range out.Data {
		offers = append(offers, SubtitleOffer{Provider: d.Provider, Language: d.Language, Forced: truthy(d.Forced), HearingImpair: truthy(d.HearingImpair),
			Score: d.Score, Release: d.ReleaseInfo, Uploader: d.Uploader, Subtitle: d.Subtitle, OriginalFormat: truthy(d.OriginalFormat)})
	}
	return offers, nil
}

// truthy reads Bazarr's booleans, which arrive as true/false or "True"/"False".
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	case float64:
		return x != 0
	}
	return false
}

// DownloadSubtitle downloads one offer from SearchSubtitles.
func (c *Client) DownloadSubtitle(ctx context.Context, s Settings, t SubtitleTarget, o SubtitleOffer) error {
	kind, q := t.params()
	q.Set("provider", o.Provider)
	q.Set("subtitle", o.Subtitle)
	q.Set("hi", strconv.FormatBool(o.HearingImpair))
	q.Set("forced", strconv.FormatBool(o.Forced))
	q.Set("original_format", strconv.FormatBool(o.OriginalFormat))
	return c.bazarr(ctx, s, http.MethodPost, "/api/providers/"+kind, q, nil)
}

// SearchMissing lets Bazarr fetch the missing languages of its profile on its own.
func (c *Client) SearchMissing(ctx context.Context, s Settings, t SubtitleTarget) error {
	if t.Kind == "episode" {
		// Bazarr runs missing-subtitle searches per series.
		return c.bazarr(ctx, s, http.MethodPatch, "/api/series", url.Values{"seriesid": {strconv.Itoa(t.SeriesID)}, "action": {"search-missing"}}, nil)
	}
	return c.bazarr(ctx, s, http.MethodPatch, "/api/movies", url.Values{"radarrid": {strconv.Itoa(t.ID)}, "action": {"search-missing"}}, nil)
}

// FindSubtitleTarget finds a Streamero title in Radarr (movies, by TMDb id) or
// Sonarr (episodes, by the series' TVDB/TMDb id plus season and episode number).
func (c *Client) FindSubtitleTarget(ctx context.Context, s Settings, kind string, ids map[string]string, season, episode int) (SubtitleTarget, error) {
	switch kind {
	case "movie":
		if s.RadarrURL == "" || s.RadarrKey == "" || ids["tmdb"] == "" {
			return SubtitleTarget{}, ErrNotConfigured
		}
		var movies []struct {
			ID     int `json:"id"`
			TMDbID int `json:"tmdbId"`
		}
		if err := c.do(ctx, "Radarr", http.MethodGet, s.RadarrURL, s.RadarrKey, "/api/v3/movie?tmdbId="+url.QueryEscape(ids["tmdb"]), nil, &movies); err != nil {
			return SubtitleTarget{}, err
		}
		for _, m := range movies {
			if fmt.Sprint(m.TMDbID) == ids["tmdb"] {
				return SubtitleTarget{Kind: "movie", ID: m.ID}, nil
			}
		}
	case "episode":
		if s.SonarrURL == "" || s.SonarrKey == "" || (ids["tvdb"] == "" && ids["tmdb"] == "") {
			return SubtitleTarget{}, ErrNotConfigured
		}
		var series []struct {
			ID     int `json:"id"`
			TVDbID int `json:"tvdbId"`
			TMDbID int `json:"tmdbId"`
		}
		if err := c.do(ctx, "Sonarr", http.MethodGet, s.SonarrURL, s.SonarrKey, "/api/v3/series", nil, &series); err != nil {
			return SubtitleTarget{}, err
		}
		for _, x := range series {
			if (ids["tvdb"] != "" && fmt.Sprint(x.TVDbID) == ids["tvdb"]) || (ids["tmdb"] != "" && fmt.Sprint(x.TMDbID) == ids["tmdb"]) {
				var eps []struct {
					ID            int `json:"id"`
					SeasonNumber  int `json:"seasonNumber"`
					EpisodeNumber int `json:"episodeNumber"`
				}
				if err := c.do(ctx, "Sonarr", http.MethodGet, s.SonarrURL, s.SonarrKey, "/api/v3/episode?seriesId="+strconv.Itoa(x.ID), nil, &eps); err != nil {
					return SubtitleTarget{}, err
				}
				for _, e := range eps {
					if e.SeasonNumber == season && e.EpisodeNumber == episode {
						return SubtitleTarget{Kind: "episode", ID: e.ID, SeriesID: x.ID}, nil
					}
				}
			}
		}
	}
	return SubtitleTarget{}, ErrNotFound
}
