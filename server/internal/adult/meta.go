package adult

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/streamero-app/streamero/server/internal/netx"
)

// Provider limits (D-053): every call has a timeout, a size limit, a rate limit
// and a cache; keys are never put in URLs or error messages.
const (
	providerTimeout = 15 * time.Second
	maxResponse     = 4 << 20
	maxImage        = 10 << 20
	// searchTTL: search answers carry everything a scene needs, so they are the
	// only calls and are kept for half a day.
	searchTTL   = 12 * time.Hour
	minInterval = 500 * time.Millisecond // 2 requests a second per service
)

// Default addresses of the services (public API documentation).
const (
	DefaultTPDBURL    = "https://api.theporndb.net"
	DefaultStashDBURL = "https://stashdb.org/graphql"
)

const keysSetting = "adult.metadata"

// Keys are the metadata service keys (decrypted).
type Keys struct{ TPDB, StashDB string }

type storedKeys struct {
	TPDB    string `json:"tpdb,omitempty"`    // sealed
	StashDB string `json:"stashdb,omitempty"` // sealed
}

// LoadKeys returns the decrypted keys ("" for keys that are not set or can no
// longer be decrypted).
func (s *Service) LoadKeys(ctx context.Context) (Keys, error) {
	var st storedKeys
	if s.Settings == nil {
		return Keys{}, nil
	}
	if _, err := s.Settings.Get(ctx, keysSetting, &st); err != nil {
		return Keys{}, err
	}
	if s.Box == nil {
		return Keys{}, nil
	}
	var k Keys
	k.TPDB, _ = s.Box.Open(st.TPDB)
	k.StashDB, _ = s.Box.Open(st.StashDB)
	return k, nil
}

// KeysInput changes the keys (empty = unchanged).
type KeysInput struct {
	TPDB, StashDB           string
	ClearTPDB, ClearStashDB bool
}

// SaveKeys checks new keys against their service and stores them encrypted.
func (s *Service) SaveKeys(ctx context.Context, in KeysInput) (Keys, error) {
	if s.Box == nil {
		return Keys{}, errors.New("secret storage is not available")
	}
	var st storedKeys
	if _, err := s.Settings.Get(ctx, keysSetting, &st); err != nil {
		return Keys{}, err
	}
	if k := strings.TrimSpace(in.TPDB); k != "" {
		if err := s.tpdb(k).verify(ctx); err != nil {
			return Keys{}, err
		}
		sealed, err := s.Box.Seal(k)
		if err != nil {
			return Keys{}, err
		}
		st.TPDB = sealed
	} else if in.ClearTPDB {
		st.TPDB = ""
	}
	if k := strings.TrimSpace(in.StashDB); k != "" {
		if err := s.stashdb(k).verify(ctx); err != nil {
			return Keys{}, err
		}
		sealed, err := s.Box.Seal(k)
		if err != nil {
			return Keys{}, err
		}
		st.StashDB = sealed
	} else if in.ClearStashDB {
		st.StashDB = ""
	}
	if err := s.Settings.Set(ctx, keysSetting, st); err != nil {
		return Keys{}, err
	}
	if in.TPDB != "" || in.StashDB != "" {
		// A new key: look up again every scene that has not been matched yet.
		if _, err := s.Pool.Exec(ctx, `UPDATE adult_scenes SET metadata_refreshed_at = NULL WHERE provider_ids = '{}'::jsonb`); err != nil {
			return Keys{}, err
		}
	}
	return s.LoadKeys(ctx)
}

// Match is a scene or movie found by a metadata service.
type Match struct {
	Provider, ID           string
	Title, Date, Details   string
	URL                    string
	Studio, StudioParent   string
	StudioURL, StudioImage string
	StudioID               string
	Performers             []PerformerInfo
	Tags                   []string
	Poster, Backdrop       string
	DurationS              int
}

// PerformerInfo is a performer as a metadata service describes them.
type PerformerInfo struct {
	ID, Name, Image                 string
	Gender, Birthdate, Country, Bio string
	Aliases                         []string
}

// Query is what is known about a scene before the lookup.
type Query struct {
	Kind, Studio, Date, Title string
	Year                      int
	Performers                []string
}

// limiter spaces calls to one service.
type limiter struct {
	mu   sync.Mutex
	next time.Time
}

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	at := l.next
	if at.Before(now) {
		at = now
	}
	l.next = at.Add(minInterval)
	l.mu.Unlock()
	if d := time.Until(at); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

var limiters sync.Map // service name → *limiter

func limiterFor(name string) *limiter {
	l, _ := limiters.LoadOrStore(name, &limiter{})
	return l.(*limiter)
}

func (s *Service) httpClient() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: providerTimeout, CheckRedirect: netx.SameHostRedirects}
}

// call sends one request and returns at most maxResponse bytes of a 2xx answer.
// Answers are cached by (service, request) for ttl; failures are not cached.
func (s *Service) call(ctx context.Context, service string, req *http.Request, body []byte, ttl time.Duration) ([]byte, error) {
	h := sha256.New()
	h.Write([]byte(service + "\n" + req.Method + " " + req.URL.String() + "\n"))
	h.Write(body)
	key := service + ":" + hex.EncodeToString(h.Sum(nil))
	if ttl > 0 {
		var cached []byte
		err := s.Pool.QueryRow(ctx, `SELECT body FROM adult_meta_cache WHERE key = $1 AND fetched_at > now() - $2::interval`,
			key, fmt.Sprintf("%d seconds", int(ttl.Seconds()))).Scan(&cached)
		if err == nil {
			return cached, nil
		}
	}
	if err := limiterFor(service).wait(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	res, err := s.httpClient().Do(req.WithContext(ctx))
	if err != nil {
		// The URL never carries the key; the error is reduced to its kind anyway.
		return nil, fmt.Errorf("%w: %s did not answer", ErrProvider, service)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrProvider, service)
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w (%s)", ErrBadKey, service)
	case res.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case res.StatusCode < 200 || res.StatusCode > 299:
		return nil, fmt.Errorf("%w: %s answered HTTP %d", ErrProvider, service, res.StatusCode)
	case len(data) > maxResponse:
		return nil, fmt.Errorf("%w: %s answered more than %d bytes", ErrProvider, service, maxResponse)
	}
	if ttl > 0 {
		_, _ = s.Pool.Exec(ctx, `INSERT INTO adult_meta_cache (key, body, fetched_at) VALUES ($1, $2, now())
			ON CONFLICT (key) DO UPDATE SET body = EXCLUDED.body, fetched_at = now()`, key, data)
	}
	return data, nil
}

// --- ThePornDB (REST, Bearer token) ----------------------------------------------------

type tpdbClient struct {
	s    *Service
	base string
	key  string
}

func (s *Service) tpdb(key string) tpdbClient {
	base := s.TPDBURL
	if base == "" {
		base = DefaultTPDBURL
	}
	return tpdbClient{s: s, base: strings.TrimRight(base, "/"), key: key}
}

func (c tpdbClient) get(ctx context.Context, path string, q url.Values, ttl time.Duration) ([]byte, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	return c.s.call(ctx, "tpdb", req, []byte(c.key), ttl)
}

func (c tpdbClient) verify(ctx context.Context) error {
	_, err := c.get(ctx, "/scenes", url.Values{"per_page": {"1"}}, 0)
	return err
}

// flexString accepts a JSON string or number.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		*f = flexString(n.String())
	}
	return nil
}

type tpdbImageSet struct {
	Full  string `json:"full"`
	Large string `json:"large"`
}

type tpdbPerformer struct {
	ID    flexString `json:"id"`
	Name  string     `json:"name"`
	Image string     `json:"image"`
	Face  string     `json:"face"`
	Bio   string     `json:"bio"`
	Extra struct {
		Gender      string `json:"gender"`
		Birthday    string `json:"birthday"`
		Nationality string `json:"nationality"`
	} `json:"extra"`
	Aliases []string `json:"aliases"`
}

type tpdbScene struct {
	ID          flexString   `json:"id"`
	Title       string       `json:"title"`
	Date        string       `json:"date"`
	Description string       `json:"description"`
	URL         string       `json:"url"`
	Image       string       `json:"image"`
	Poster      string       `json:"poster"`
	BackImage   string       `json:"back_image"`
	Posters     tpdbImageSet `json:"posters"`
	Background  tpdbImageSet `json:"background"`
	Duration    json.Number  `json:"duration"`
	DeletedAt   *string      `json:"deleted_at"`
	Disputed    bool         `json:"is_disputed"`
	Site        *struct {
		ID     flexString `json:"id"`
		Name   string     `json:"name"`
		URL    string     `json:"url"`
		Logo   string     `json:"logo"`
		Parent *struct {
			Name string `json:"name"`
		} `json:"parent"`
		Network *struct {
			Name string `json:"name"`
		} `json:"network"`
	} `json:"site"`
	Performers []tpdbPerformer `json:"performers"`
	Tags       []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

func (t tpdbScene) match() Match {
	m := Match{Provider: "tpdb", ID: string(t.ID), Title: t.Title, Date: t.Date, Details: t.Description, URL: t.URL}
	m.Poster = firstOf(t.Posters.Large, t.Poster, t.Image)
	m.Backdrop = firstOf(t.Background.Large, t.Background.Full, t.Image, t.BackImage)
	if d, err := strconv.ParseFloat(t.Duration.String(), 64); err == nil {
		m.DurationS = int(d)
	}
	if t.Site != nil {
		m.Studio, m.StudioURL, m.StudioImage, m.StudioID = t.Site.Name, t.Site.URL, t.Site.Logo, string(t.Site.ID)
		switch {
		case t.Site.Parent != nil && t.Site.Parent.Name != "" && t.Site.Parent.Name != t.Site.Name:
			m.StudioParent = t.Site.Parent.Name
		case t.Site.Network != nil && t.Site.Network.Name != t.Site.Name:
			m.StudioParent = t.Site.Network.Name
		}
	}
	for _, p := range t.Performers {
		m.Performers = append(m.Performers, PerformerInfo{ID: string(p.ID), Name: p.Name, Image: firstOf(p.Image, p.Face),
			Gender: p.Extra.Gender, Birthdate: p.Extra.Birthday, Country: p.Extra.Nationality, Bio: p.Bio, Aliases: p.Aliases})
	}
	for _, tg := range t.Tags {
		m.Tags = append(m.Tags, tg.Name)
	}
	return m
}

func firstOf(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func (c tpdbClient) search(ctx context.Context, q Query) ([]Match, error) {
	path, params := "/scenes", url.Values{"per_page": {"10"}}
	if q.Kind == "movie" {
		path = "/movies"
		params.Set("q", q.Title)
		if q.Year > 0 {
			params.Set("year", strconv.Itoa(q.Year))
		}
	} else {
		params.Set("parse", strings.TrimSpace(strings.Join([]string{q.Studio, q.Date, q.Title}, " ")))
	}
	data, err := c.get(ctx, path, params, searchTTL)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data []tpdbScene `json:"data"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("%w: ThePornDB answered in an unknown format", ErrProvider)
	}
	var out []Match
	for _, sc := range res.Data {
		if sc.DeletedAt != nil || sc.Disputed {
			continue // removed or disputed entries are never used
		}
		out = append(out, sc.match())
	}
	return out, nil
}

// --- StashDB (GraphQL, ApiKey header) ------------------------------------------------------

type stashClient struct {
	s   *Service
	url string
	key string
}

func (s *Service) stashdb(key string) stashClient {
	u := s.StashDBURL
	if u == "" {
		u = DefaultStashDBURL
	}
	return stashClient{s: s, url: u, key: key}
}

func (c stashClient) query(ctx context.Context, q string, vars map[string]any, ttl time.Duration, out any) error {
	body, err := json.Marshal(map[string]any{"query": q, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("ApiKey", c.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	data, err := c.s.call(ctx, "stashdb", req, append(body, c.key...), ttl)
	if err != nil {
		return err
	}
	var res struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return fmt.Errorf("%w: StashDB answered in an unknown format", ErrProvider)
	}
	if len(res.Errors) > 0 {
		msg := strings.ToLower(res.Errors[0].Message)
		if strings.Contains(msg, "unauthorized") || strings.Contains(msg, "not authorized") || strings.Contains(msg, "api key") {
			return fmt.Errorf("%w (stashdb)", ErrBadKey)
		}
		return fmt.Errorf("%w: StashDB: %s", ErrProvider, res.Errors[0].Message)
	}
	return json.Unmarshal(res.Data, out)
}

const stashSceneFields = `id title details release_date duration deleted
	urls { url }
	studio { id name urls { url } images { url } parent { name } }
	images { url width height }
	performers { performer { id name aliases gender birth_date country deleted images { url } } }
	tags { name }`

type stashScene struct {
	ID          string  `json:"id"`
	Title       *string `json:"title"`
	Details     *string `json:"details"`
	ReleaseDate *string `json:"release_date"`
	Duration    *int    `json:"duration"`
	Deleted     bool    `json:"deleted"`
	URLs        []struct {
		URL string `json:"url"`
	} `json:"urls"`
	Studio *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		URLs []struct {
			URL string `json:"url"`
		} `json:"urls"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
		Parent *struct {
			Name string `json:"name"`
		} `json:"parent"`
	} `json:"studio"`
	Images []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"images"`
	Performers []struct {
		Performer struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Aliases   []string `json:"aliases"`
			Gender    *string  `json:"gender"`
			BirthDate *string  `json:"birth_date"`
			Country   *string  `json:"country"`
			Deleted   bool     `json:"deleted"`
			Images    []struct {
				URL string `json:"url"`
			} `json:"images"`
		} `json:"performer"`
	} `json:"performers"`
	Tags []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (t stashScene) match() Match {
	m := Match{Provider: "stashdb", ID: t.ID, Title: deref(t.Title), Date: deref(t.ReleaseDate), Details: deref(t.Details)}
	if len(t.URLs) > 0 {
		m.URL = t.URLs[0].URL
	}
	if t.Duration != nil {
		m.DurationS = *t.Duration
	}
	// Scene images are landscape: the best one is both the card and the backdrop.
	best, area := "", 0
	for _, im := range t.Images {
		if a := im.Width * im.Height; best == "" || a > area {
			best, area = im.URL, a
		}
	}
	m.Poster, m.Backdrop = best, best
	if t.Studio != nil {
		m.Studio, m.StudioID = t.Studio.Name, t.Studio.ID
		if len(t.Studio.URLs) > 0 {
			m.StudioURL = t.Studio.URLs[0].URL
		}
		if len(t.Studio.Images) > 0 {
			m.StudioImage = t.Studio.Images[0].URL
		}
		if t.Studio.Parent != nil {
			m.StudioParent = t.Studio.Parent.Name
		}
	}
	for _, p := range t.Performers {
		if p.Performer.Deleted {
			continue
		}
		pi := PerformerInfo{ID: p.Performer.ID, Name: p.Performer.Name, Aliases: p.Performer.Aliases,
			Gender: strings.ToLower(deref(p.Performer.Gender)), Birthdate: deref(p.Performer.BirthDate), Country: deref(p.Performer.Country)}
		if len(p.Performer.Images) > 0 {
			pi.Image = p.Performer.Images[0].URL
		}
		m.Performers = append(m.Performers, pi)
	}
	for _, tg := range t.Tags {
		m.Tags = append(m.Tags, tg.Name)
	}
	return m
}

func (c stashClient) verify(ctx context.Context) error {
	var out json.RawMessage
	return c.query(ctx, `query($term: String!) { searchScenes(term: $term, limit: 1) { count } }`,
		map[string]any{"term": "a"}, 0, &out)
}

func (c stashClient) search(ctx context.Context, q Query) ([]Match, error) {
	if q.Kind == "movie" {
		return nil, nil // StashDB has scenes only
	}
	term := strings.TrimSpace(strings.Join([]string{q.Studio, q.Date, q.Title}, " "))
	var res struct {
		SearchScenes struct {
			Scenes []stashScene `json:"scenes"`
		} `json:"searchScenes"`
	}
	if err := c.query(ctx, `query($term: String!) { searchScenes(term: $term, limit: 10) { scenes { `+stashSceneFields+` } } }`,
		map[string]any{"term": term}, searchTTL, &res); err != nil {
		return nil, err
	}
	var out []Match
	for _, sc := range res.SearchScenes.Scenes {
		if sc.Deleted {
			continue
		}
		out = append(out, sc.match())
	}
	return out, nil
}
