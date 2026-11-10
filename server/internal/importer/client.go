// Package importer copies per-user watch history (played state, play counts, resume
// positions, favourites) and playlists from another media server into Streamero.
// The source server is read only through its publicly documented REST API.
package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUpstream wraps errors from the source server (unreachable, rejected key, ...).
type ErrUpstream struct {
	Status int
	Err    error
}

func (e *ErrUpstream) Error() string {
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		return fmt.Sprintf("the server rejected the API key (HTTP %d)", e.Status)
	}
	if e.Status != 0 {
		return fmt.Sprintf("the server answered HTTP %d", e.Status)
	}
	return fmt.Sprintf("the server is unreachable: %v", e.Err)
}

func (e *ErrUpstream) Unwrap() error { return e.Err }

// ErrNotFound is returned for HTTP 404 from the source server.
var ErrNotFound = errors.New("not found on the source server")

// CleanURL validates an admin-entered base URL and trims the trailing slash.
func CleanURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if raw == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("%q is not an http(s) address", raw)
	}
	return raw, nil
}

// Client reads one source server. Key is an API key created by its administrator.
type Client struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
	// PageSize is how many items one request returns (default 500).
	PageSize int
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	// Documented header for API keys of these servers.
	req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Client="Streamero", Device="Streamero import", DeviceId="streamero-import", Version="1", Token=%q`, c.Key))
	req.Header.Set("Accept", "application/json")
	resp, err := c.http().Do(req) //nolint:gosec // G704: base URL is set by an administrator
	if err != nil {
		return &ErrUpstream{Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return &ErrUpstream{Status: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(out); err != nil {
		return &ErrUpstream{Err: fmt.Errorf("unexpected answer: %w", err)}
	}
	return nil
}

// ServerInfo describes the source server.
type ServerInfo struct {
	ID         string `json:"Id"`
	ServerName string `json:"ServerName"`
	Version    string `json:"Version"`
}

// Info reads /System/Info (needs an administrator's key, which also proves the key works).
func (c *Client) Info(ctx context.Context) (ServerInfo, error) {
	var out ServerInfo
	err := c.get(ctx, "/System/Info", nil, &out)
	return out, err
}

// User is a user of the source server.
type User struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Policy struct {
		IsAdministrator bool `json:"IsAdministrator"`
		IsDisabled      bool `json:"IsDisabled"`
	} `json:"Policy"`
}

// Users lists all users.
func (c *Client) Users(ctx context.Context) ([]User, error) {
	var out []User
	err := c.get(ctx, "/Users", nil, &out)
	return out, err
}

// UserData is the per-user state of an item.
type UserData struct {
	PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
	PlayCount             int   `json:"PlayCount"`
	IsFavorite            bool  `json:"IsFavorite"`
	Played                bool  `json:"Played"`
	LastPlayedDate        *Time `json:"LastPlayedDate"`
}

// Time is a date from the source server. It accepts RFC 3339 with any number of
// fraction digits and, as some versions send, dates without a zone (taken as UTC).
// A date that cannot be read is treated as unknown rather than failing the import.
type Time struct{ time.Time }

// UnmarshalJSON implements json.Unmarshaler.
func (t *Time) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
		if v, err := time.Parse(layout, s); err == nil {
			t.Time = v.UTC()
			return nil
		}
	}
	return nil
}

// Item is the subset of an item the import needs.
type Item struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	Type              string            `json:"Type"`
	Path              string            `json:"Path"`
	ProductionYear    int               `json:"ProductionYear"`
	IndexNumber       *int              `json:"IndexNumber"`
	ParentIndexNumber *int              `json:"ParentIndexNumber"`
	SeriesID          string            `json:"SeriesId"`
	SeriesName        string            `json:"SeriesName"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	UserData          *UserData         `json:"UserData"`
}

type itemPage struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
}

const itemFields = "ProviderIds,Path"

func (c *Client) pageSize() int {
	if c.PageSize > 0 {
		return c.PageSize
	}
	return 500
}

// items pages through GET /Items with the given query.
func (c *Client) items(ctx context.Context, q url.Values) ([]Item, error) {
	var out []Item
	for start := 0; ; {
		q.Set("startIndex", fmt.Sprint(start))
		q.Set("limit", fmt.Sprint(c.pageSize()))
		var p itemPage
		if err := c.get(ctx, "/Items", q, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Items...)
		start += len(p.Items)
		if len(p.Items) == 0 || start >= p.TotalRecordCount {
			return out, nil
		}
	}
}

// Kinds of source items the import reads.
var (
	playableTypes = "Movie,Episode,Audio"
	favoriteTypes = "Movie,Series,Season,Episode,Audio,MusicAlbum"
)

// UserItems returns every item with user data worth importing for one user:
// played, resumable, or favourite. Items found by several filters appear once.
func (c *Client) UserItems(ctx context.Context, userID string) ([]Item, error) {
	seen := map[string]int{}
	var out []Item
	for _, f := range []struct{ filter, types string }{
		{"IsPlayed", playableTypes}, {"IsResumable", playableTypes}, {"IsFavorite", favoriteTypes},
	} {
		q := url.Values{"userId": {userID}, "recursive": {"true"}, "filters": {f.filter}, "includeItemTypes": {f.types},
			"fields": {itemFields}, "enableUserData": {"true"}, "enableImages": {"false"}}
		items, err := c.items(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if _, ok := seen[it.ID]; ok {
				continue
			}
			seen[it.ID] = len(out)
			out = append(out, it)
		}
	}
	return out, nil
}

// ItemsByID fetches items (e.g. the series of episodes) with their provider ids and paths.
func (c *Client) ItemsByID(ctx context.Context, userID string, ids []string) ([]Item, error) {
	var out []Item
	for len(ids) > 0 {
		n := min(len(ids), 100)
		q := url.Values{"userId": {userID}, "ids": {strings.Join(ids[:n], ",")}, "fields": {itemFields}, "enableImages": {"false"}}
		items, err := c.items(ctx, q)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		ids = ids[n:]
	}
	return out, nil
}

// Playlists lists the playlists a user can see.
func (c *Client) Playlists(ctx context.Context, userID string) ([]Item, error) {
	return c.items(ctx, url.Values{"userId": {userID}, "recursive": {"true"}, "includeItemTypes": {"Playlist"}, "enableImages": {"false"}})
}

// PlaylistAccess is who a playlist is shared with.
type PlaylistAccess struct {
	OpenAccess bool `json:"OpenAccess"`
	Shares     []struct {
		UserID string `json:"UserId"`
	} `json:"Shares"`
}

// PlaylistAccess reads sharing of a playlist; older servers answer ErrNotFound.
func (c *Client) PlaylistAccess(ctx context.Context, playlistID string) (PlaylistAccess, error) {
	var out PlaylistAccess
	err := c.get(ctx, "/Playlists/"+url.PathEscape(playlistID), nil, &out)
	return out, err
}

// PlaylistItems lists the entries of a playlist in order.
func (c *Client) PlaylistItems(ctx context.Context, playlistID, userID string) ([]Item, error) {
	var out []Item
	for start := 0; ; {
		q := url.Values{"userId": {userID}, "fields": {itemFields}, "enableImages": {"false"},
			"startIndex": {fmt.Sprint(start)}, "limit": {fmt.Sprint(c.pageSize())}}
		var p itemPage
		if err := c.get(ctx, "/Playlists/"+url.PathEscape(playlistID)+"/Items", q, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Items...)
		start += len(p.Items)
		if len(p.Items) == 0 || start >= p.TotalRecordCount {
			return out, nil
		}
	}
}

// TicksToMS converts the source's position ticks (100 ns each) to milliseconds.
func TicksToMS(ticks int64) int64 {
	if ticks <= 0 {
		return 0
	}
	return ticks / 10_000
}
