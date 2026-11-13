// Package requests connects Streamero to a Seerr/Jellyseerr server (search, trending,
// request a title for download through Radarr/Sonarr) and reads the Radarr/Sonarr
// calendars for upcoming releases. Only public HTTP APIs of those apps are used.
package requests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/streamero-app/streamero/server/internal/netx"
)

// KeySettings is the settings key.
const KeySettings = "integrations.requests"

// Settings holds the connections; API keys never leave the server.
type Settings struct {
	SeerrURL        string `json:"seerrUrl,omitempty"`
	SeerrKey        string `json:"seerrKey,omitempty"`
	UsersCanRequest bool   `json:"usersCanRequest"`
	RadarrURL       string `json:"radarrUrl,omitempty"`
	RadarrKey       string `json:"radarrKey,omitempty"`
	SonarrURL       string `json:"sonarrUrl,omitempty"`
	SonarrKey       string `json:"sonarrKey,omitempty"`
	BazarrURL       string `json:"bazarrUrl,omitempty"`
	BazarrKey       string `json:"bazarrKey,omitempty"`
}

// SeerrReady reports whether search and requests can work.
func (s Settings) SeerrReady() bool { return s.SeerrURL != "" && s.SeerrKey != "" }

// ErrNotConfigured means the needed connection is missing.
var ErrNotConfigured = errors.New("not configured")

// ErrUpstream wraps errors from the other app (unreachable, rejected key, ...).
type ErrUpstream struct {
	App    string
	Status int
	Err    error
}

func (e *ErrUpstream) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("%s answered HTTP %d", e.App, e.Status)
	}
	return fmt.Sprintf("%s: %v", e.App, e.Err)
}

// CleanURL validates an admin-entered base URL and trims the trailing slash.
func CleanURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("%q is not an http(s) address", raw)
	}
	return raw, nil
}

// Client talks to the configured apps.
type Client struct {
	HTTP *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 12 * time.Second, CheckRedirect: netx.SameHostRedirects}
}

func (c *Client) do(ctx context.Context, app, method, base, key, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req) //nolint:gosec // G704: base URL is set by an administrator
	if err != nil {
		return &ErrUpstream{App: app, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return &ErrUpstream{App: app, Status: resp.StatusCode}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// Status of a title in Seerr (mediaInfo.status).
const (
	StatusNone       = "none"
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusPartial    = "partial"
	StatusAvailable  = "available"
)

func seerrStatus(code int) string {
	switch code {
	case 2:
		return StatusPending
	case 3:
		return StatusProcessing
	case 4:
		return StatusPartial
	case 5:
		return StatusAvailable
	default:
		return StatusNone
	}
}

// Title is one search or trending result.
type Title struct {
	TMDbID       int
	MediaType    string // movie | tv
	Title        string
	Year         int
	Overview     string
	PosterPath   string
	BackdropPath string
	Status       string
	Rating       float64
}

type seerrResult struct {
	ID           int     `json:"id"`
	MediaType    string  `json:"mediaType"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"releaseDate"`
	FirstAirDate string  `json:"firstAirDate"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"posterPath"`
	BackdropPath string  `json:"backdropPath"`
	VoteAverage  float64 `json:"voteAverage"`
	MediaInfo    *struct {
		Status int `json:"status"`
	} `json:"mediaInfo"`
}

type seerrPage struct {
	Page         int           `json:"page"`
	TotalPages   int           `json:"totalPages"`
	TotalResults int           `json:"totalResults"`
	Results      []seerrResult `json:"results"`
}

func (r seerrResult) title() (Title, bool) {
	if r.MediaType != "movie" && r.MediaType != "tv" {
		return Title{}, false // people and collections are not requestable
	}
	t := Title{TMDbID: r.ID, MediaType: r.MediaType, Title: r.Title, Overview: r.Overview, PosterPath: r.PosterPath,
		BackdropPath: r.BackdropPath, Status: StatusNone, Rating: r.VoteAverage}
	date := r.ReleaseDate
	if r.MediaType == "tv" {
		t.Title, date = r.Name, r.FirstAirDate
	}
	if len(date) >= 4 {
		_, _ = fmt.Sscanf(date[:4], "%d", &t.Year)
	}
	if r.MediaInfo != nil {
		t.Status = seerrStatus(r.MediaInfo.Status)
	}
	return t, true
}

// Page is a page of titles.
type Page struct {
	Titles     []Title
	Page       int
	TotalPages int
}

func (p seerrPage) titles() Page {
	out := Page{Page: p.Page, TotalPages: p.TotalPages}
	for _, r := range p.Results {
		if t, ok := r.title(); ok {
			out.Titles = append(out.Titles, t)
		}
	}
	return out
}

// Search finds movies and series by name.
func (c *Client) Search(ctx context.Context, s Settings, query, lang string, page int) (Page, error) {
	if !s.SeerrReady() {
		return Page{}, ErrNotConfigured
	}
	q := url.Values{"query": {query}, "page": {fmt.Sprint(max(page, 1))}}
	if lang != "" {
		q.Set("language", lang)
	}
	var p seerrPage
	// Seerr expects spaces as %20 in the query string.
	path := "/api/v1/search?" + strings.ReplaceAll(q.Encode(), "+", "%20")
	if err := c.do(ctx, "Seerr", http.MethodGet, s.SeerrURL, s.SeerrKey, path, nil, &p); err != nil {
		return Page{}, err
	}
	return p.titles(), nil
}

// Trending lists what is popular now.
func (c *Client) Trending(ctx context.Context, s Settings, lang string, page int) (Page, error) {
	if !s.SeerrReady() {
		return Page{}, ErrNotConfigured
	}
	q := url.Values{"page": {fmt.Sprint(max(page, 1))}}
	if lang != "" {
		q.Set("language", lang)
	}
	var p seerrPage
	if err := c.do(ctx, "Seerr", http.MethodGet, s.SeerrURL, s.SeerrKey, "/api/v1/discover/trending?"+q.Encode(), nil, &p); err != nil {
		return Page{}, err
	}
	return p.titles(), nil
}

// Request asks Seerr to get a title; series are requested with all seasons.
// Seerr hands the request to Radarr or Sonarr (automatically when approved).
func (c *Client) Request(ctx context.Context, s Settings, mediaType string, tmdbID int) (string, error) {
	if !s.SeerrReady() {
		return "", ErrNotConfigured
	}
	body := map[string]any{"mediaType": mediaType, "mediaId": tmdbID}
	if mediaType == "tv" {
		body["seasons"] = "all"
	}
	var out struct {
		Status int `json:"status"` // 1 pending approval, 2 approved, 3 declined
		Media  struct {
			Status int `json:"status"`
		} `json:"media"`
	}
	if err := c.do(ctx, "Seerr", http.MethodPost, s.SeerrURL, s.SeerrKey, "/api/v1/request", body, &out); err != nil {
		return "", err
	}
	if out.Status == 2 {
		if st := seerrStatus(out.Media.Status); st != StatusNone && st != StatusPending {
			return st, nil
		}
		return StatusProcessing, nil
	}
	return StatusPending, nil
}

// Ping checks a connection: Seerr /api/v1/status, Radarr/Sonarr /api/v3/system/status.
func (c *Client) Ping(ctx context.Context, app, base, key string) (string, error) {
	path := "/api/v3/system/status"
	if app == "Seerr" {
		path = "/api/v1/status"
	}
	var out struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, app, http.MethodGet, base, key, path, nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// Upcoming is one release on the Radarr/Sonarr calendars.
type Upcoming struct {
	Kind        string // movie | episode
	Title       string
	SeriesTitle string
	Season      int
	Episode     int
	Date        time.Time
	Overview    string
	PosterURL   string
	Downloaded  bool
}

type arrImage struct {
	CoverType string `json:"coverType"`
	RemoteURL string `json:"remoteUrl"`
}

func poster(images []arrImage) string {
	for _, im := range images {
		if im.CoverType == "poster" && strings.HasPrefix(im.RemoteURL, "https://") {
			return im.RemoteURL
		}
	}
	return ""
}

// Calendar lists releases between from and to from whichever of Radarr and Sonarr is set up.
func (c *Client) Calendar(ctx context.Context, s Settings, from, to time.Time) ([]Upcoming, error) {
	if s.RadarrURL == "" && s.SonarrURL == "" {
		return nil, ErrNotConfigured
	}
	q := url.Values{"start": {from.UTC().Format(time.RFC3339)}, "end": {to.UTC().Format(time.RFC3339)}}
	var out []Upcoming
	var firstErr error
	if s.RadarrURL != "" && s.RadarrKey != "" {
		var movies []struct {
			Title           string     `json:"title"`
			Overview        string     `json:"overview"`
			DigitalRelease  *time.Time `json:"digitalRelease"`
			PhysicalRelease *time.Time `json:"physicalRelease"`
			InCinemas       *time.Time `json:"inCinemas"`
			HasFile         bool       `json:"hasFile"`
			Images          []arrImage `json:"images"`
		}
		if err := c.do(ctx, "Radarr", http.MethodGet, s.RadarrURL, s.RadarrKey, "/api/v3/calendar?"+q.Encode(), nil, &movies); err != nil {
			firstErr = err
		}
		for _, m := range movies {
			// The home release is what matters for a media server.
			var d *time.Time
			for _, c := range []*time.Time{m.DigitalRelease, m.PhysicalRelease, m.InCinemas} {
				if c != nil && !c.Before(from) && !c.After(to) {
					d = c
					break
				}
			}
			if d == nil {
				continue
			}
			out = append(out, Upcoming{Kind: "movie", Title: m.Title, Date: *d, Overview: m.Overview, PosterURL: poster(m.Images), Downloaded: m.HasFile})
		}
	}
	if s.SonarrURL != "" && s.SonarrKey != "" {
		q2 := url.Values{"start": q["start"], "end": q["end"], "includeSeries": {"true"}}
		var eps []struct {
			Title         string     `json:"title"`
			Overview      string     `json:"overview"`
			SeasonNumber  int        `json:"seasonNumber"`
			EpisodeNumber int        `json:"episodeNumber"`
			AirDateUTC    *time.Time `json:"airDateUtc"`
			HasFile       bool       `json:"hasFile"`
			Series        struct {
				Title  string     `json:"title"`
				Images []arrImage `json:"images"`
			} `json:"series"`
		}
		if err := c.do(ctx, "Sonarr", http.MethodGet, s.SonarrURL, s.SonarrKey, "/api/v3/calendar?"+q2.Encode(), nil, &eps); err != nil && firstErr == nil {
			firstErr = err
		}
		for _, e := range eps {
			if e.AirDateUTC == nil {
				continue
			}
			out = append(out, Upcoming{Kind: "episode", Title: e.Title, SeriesTitle: e.Series.Title, Season: e.SeasonNumber,
				Episode: e.EpisodeNumber, Date: *e.AirDateUTC, Overview: e.Overview, PosterURL: poster(e.Series.Images), Downloaded: e.HasFile})
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// Link finds where a title is managed: Radarr by TMDb id, Sonarr by TVDB or TMDb id.
// It returns the app ("radarr"/"sonarr") and the page URL in that app.
func (c *Client) Link(ctx context.Context, s Settings, kind string, ids map[string]string) (string, string, error) {
	switch kind {
	case "movie":
		if s.RadarrURL == "" || s.RadarrKey == "" || ids["tmdb"] == "" {
			return "", "", ErrNotConfigured
		}
		var movies []struct {
			TitleSlug string `json:"titleSlug"`
			TMDbID    int    `json:"tmdbId"`
		}
		if err := c.do(ctx, "Radarr", http.MethodGet, s.RadarrURL, s.RadarrKey, "/api/v3/movie?tmdbId="+url.QueryEscape(ids["tmdb"]), nil, &movies); err != nil {
			return "", "", err
		}
		for _, m := range movies {
			if fmt.Sprint(m.TMDbID) == ids["tmdb"] && m.TitleSlug != "" {
				return "radarr", s.RadarrURL + "/movie/" + url.PathEscape(m.TitleSlug), nil
			}
		}
	case "series":
		if s.SonarrURL == "" || s.SonarrKey == "" || (ids["tvdb"] == "" && ids["tmdb"] == "") {
			return "", "", ErrNotConfigured
		}
		var series []struct {
			TitleSlug string `json:"titleSlug"`
			TVDbID    int    `json:"tvdbId"`
			TMDbID    int    `json:"tmdbId"`
		}
		if err := c.do(ctx, "Sonarr", http.MethodGet, s.SonarrURL, s.SonarrKey, "/api/v3/series", nil, &series); err != nil {
			return "", "", err
		}
		for _, x := range series {
			if (ids["tvdb"] != "" && fmt.Sprint(x.TVDbID) == ids["tvdb"]) || (ids["tmdb"] != "" && fmt.Sprint(x.TMDbID) == ids["tmdb"]) {
				return "sonarr", s.SonarrURL + "/series/" + url.PathEscape(x.TitleSlug), nil
			}
		}
	}
	return "", "", ErrNotFound
}

// ErrNotFound means the app does not manage this title.
var ErrNotFound = errors.New("not managed there")
