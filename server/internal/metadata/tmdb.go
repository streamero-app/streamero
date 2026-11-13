// Package metadata fetches descriptions and artwork from TMDb (The Movie Database)
// and stores them on library items.
package metadata

import (
	"context"
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
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// ErrInvalidKey means TMDb rejected the API key.
var ErrInvalidKey = errors.New("TMDb rejected the API key")

// TMDb is a small client for the TMDb v3 API. It accepts a v3 API key or a
// v4 Read Access Token (sent as a Bearer token).
type TMDb struct {
	Key       string
	BaseURL   string // default https://api.themoviedb.org/3
	ImageBase string // default https://image.tmdb.org/t/p
	HTTP      *http.Client

	mu   sync.Mutex
	next time.Time // simple pacing: TMDb allows ~40 requests/s
}

const minInterval = 30 * time.Millisecond

func (c *TMDb) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return "https://api.themoviedb.org/3"
}

func (c *TMDb) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *TMDb) pace(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	wait := c.next.Sub(now)
	if wait < 0 {
		wait = 0
	}
	c.next = now.Add(wait + minInterval)
	c.mu.Unlock()
	if wait == 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}

// ErrTemporary marks failures that are worth trying again later (TMDb busy or
// unreachable); the title is not marked as having no metadata.
var ErrTemporary = errors.New("TMDb is temporarily unavailable")

// Retries of get(): rate limits (429, honouring Retry-After), server errors and
// network failures are tried again with a growing pause before giving up.
var (
	retryAttempts = 4
	retryBase     = time.Second
	retryMax      = 30 * time.Second
)

// get fetches path, retrying temporary failures; what still fails temporarily
// after the retries wraps ErrTemporary.
func (c *TMDb) get(ctx context.Context, path string, q url.Values, out any) error {
	var err error
	for attempt := range retryAttempts {
		var wait time.Duration
		wait, err = c.getOnce(ctx, path, q, out)
		if err == nil || !errors.Is(err, ErrTemporary) || attempt == retryAttempts-1 {
			return err
		}
		if wait <= 0 {
			wait = retryBase << attempt
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(wait, retryMax)):
		}
	}
	return err
}

// getOnce makes one request; for a temporary failure it may say how long to wait.
func (c *TMDb) getOnce(ctx context.Context, path string, q url.Values, out any) (time.Duration, error) {
	if err := c.pace(ctx); err != nil {
		return 0, err
	}
	q = cloneValues(q)
	if q == nil {
		q = url.Values{}
	}
	bearer := len(c.Key) > 40 // v4 tokens are JWTs; v3 keys are 32 hex characters
	if !bearer {
		q.Set("api_key", c.Key)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+path+"?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	if bearer {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("%w: %s: %w", ErrTemporary, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return 0, ErrInvalidKey
	case resp.StatusCode == http.StatusNotFound:
		return 0, errNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return retryAfter(resp.Header.Get("Retry-After")), fmt.Errorf("%w: %s: HTTP 429", ErrTemporary, path)
	case resp.StatusCode >= 500:
		return retryAfter(resp.Header.Get("Retry-After")), fmt.Errorf("%w: %s: HTTP %d", ErrTemporary, path, resp.StatusCode)
	case resp.StatusCode >= 300:
		return 0, fmt.Errorf("TMDb %s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrTemporary, path, err) // cut-off response
	}
	return 0, nil
}

// retryAfter reads a Retry-After header in seconds (0 when absent or a date).
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

func cloneValues(q url.Values) url.Values {
	if q == nil {
		return nil
	}
	out := make(url.Values, len(q))
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}

var errNotFound = errors.New("not found on TMDb")

// Verify checks the key.
func (c *TMDb) Verify(ctx context.Context) error {
	var out map[string]any
	return c.get(ctx, "/configuration", nil, &out)
}

type searchResult struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Name          string `json:"name"`
	OriginalTitle string `json:"original_title"`
	OriginalName  string `json:"original_name"`
	ReleaseDate   string `json:"release_date"`
	FirstAirDate  string `json:"first_air_date"`
	Overview      string `json:"overview"`
	PosterPath    string `json:"poster_path"`
	Popularity    float64
}

// Candidate is one search hit offered when identifying an item by hand.
type Candidate struct {
	ID                   int
	Title, OriginalTitle string
	Year                 int
	Overview, PosterPath string
}

// Candidates lists up to 12 TMDb matches for a manual identify.
func (c *TMDb) Candidates(ctx context.Context, kind, title string, year int, lang string) ([]Candidate, error) {
	q := url.Values{"query": {title}, "language": {lang}, "include_adult": {"false"}}
	if year > 0 {
		if kind == "movie" {
			q.Set("year", fmt.Sprint(year))
		} else {
			q.Set("first_air_date_year", fmt.Sprint(year))
		}
	}
	var out struct {
		Results []searchResult `json:"results"`
	}
	if err := c.get(ctx, "/search/"+kind, q, &out); err != nil {
		return nil, err
	}
	res := make([]Candidate, 0, len(out.Results))
	for _, r := range out.Results[:min(12, len(out.Results))] {
		cd := Candidate{ID: r.ID, Title: firstNonEmpty(r.Title, r.Name), OriginalTitle: firstNonEmpty(r.OriginalTitle, r.OriginalName),
			Overview: r.Overview, PosterPath: r.PosterPath}
		if d := firstNonEmpty(r.ReleaseDate, r.FirstAirDate); len(d) >= 4 {
			_, _ = fmt.Sscanf(d[:4], "%d", &cd.Year)
		}
		res = append(res, cd)
	}
	return res, nil
}

// Search finds a movie ("movie") or series ("tv") by title and optional year.
func (c *TMDb) Search(ctx context.Context, kind, title string, year int, lang string) (int, error) {
	try := func(withYear bool) (int, error) {
		q := url.Values{"query": {title}, "language": {lang}, "include_adult": {"false"}}
		if withYear && year > 0 {
			if kind == "movie" {
				q.Set("year", fmt.Sprint(year))
			} else {
				q.Set("first_air_date_year", fmt.Sprint(year))
			}
		}
		var out struct {
			Results []searchResult `json:"results"`
		}
		if err := c.get(ctx, "/search/"+kind, q, &out); err != nil {
			return 0, err
		}
		if len(out.Results) == 0 {
			return 0, errNotFound
		}
		// Prefer an exact title match among the first results.
		want := foldTitle(title)
		for _, r := range out.Results[:min(5, len(out.Results))] {
			if foldTitle(r.Title) == want || foldTitle(r.Name) == want ||
				foldTitle(r.OriginalTitle) == want || foldTitle(r.OriginalName) == want {
				return r.ID, nil
			}
		}
		if !withYear && year > 0 {
			// Without the year filter the first hit is often another film of a similar
			// name: accept it only when its year is within one of the file's.
			for _, r := range out.Results[:min(5, len(out.Results))] {
				if y := resultYear(r); y > 0 && y >= year-1 && y <= year+1 {
					return r.ID, nil
				}
			}
			return 0, errNotFound
		}
		return out.Results[0].ID, nil
	}
	id, err := try(true)
	if errors.Is(err, errNotFound) && year > 0 {
		return try(false) // year in the file name may be off by one
	}
	return id, err
}

// resultYear is the release (or first air) year of a search hit, 0 if unknown.
func resultYear(r searchResult) int {
	d := firstNonEmpty(r.ReleaseDate, r.FirstAirDate)
	if len(d) < 4 {
		return 0
	}
	y, err := strconv.Atoi(d[:4])
	if err != nil {
		return 0
	}
	return y
}

// foldTitle compares titles without case, accents and punctuation
// ("Amélie" = "amelie", "Spider-Man" = "spider man").
func foldTitle(s string) string {
	if s == "" {
		return ""
	}
	t, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), s)
	if err != nil {
		t = s
	}
	t = strings.NewReplacer("đ", "d", "Đ", "d", "ß", "ss", "æ", "ae", "ø", "o", "&", " and ").Replace(strings.ToLower(t))
	return strings.Join(strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
}

// FindIMDb resolves an IMDb ID to a TMDb movie or series ID.
func (c *TMDb) FindIMDb(ctx context.Context, kind, imdbID string) (int, error) {
	var out struct {
		Movies []searchResult `json:"movie_results"`
		TV     []searchResult `json:"tv_results"`
	}
	if err := c.get(ctx, "/find/"+url.PathEscape(imdbID), url.Values{"external_source": {"imdb_id"}}, &out); err != nil {
		return 0, err
	}
	list := out.Movies
	if kind == "tv" {
		list = out.TV
	}
	if len(list) == 0 {
		return 0, errNotFound
	}
	return list[0].ID, nil
}

// Details is the common shape of movie and series details.
type Details struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	OriginalName  string  `json:"original_name"`
	Overview      string  `json:"overview"`
	Tagline       string  `json:"tagline"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	VoteAverage   float64 `json:"vote_average"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	IMDbID        string  `json:"imdb_id"`
	Companies     []struct {
		Name string `json:"name"`
	} `json:"production_companies"`
	Networks []struct {
		Name string `json:"name"`
	} `json:"networks"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Images struct {
		Logos []struct {
			FilePath string  `json:"file_path"`
			Language string  `json:"iso_639_1"`
			Vote     float64 `json:"vote_average"`
		} `json:"logos"`
	} `json:"images"`
	Credits struct {
		Cast []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
		} `json:"cast"`
		Crew []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	} `json:"credits"`
	ReleaseDates struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	ExternalIDs struct {
		IMDbID string `json:"imdb_id"`
		TVDbID int    `json:"tvdb_id"`
	} `json:"external_ids"`
	Seasons []struct {
		SeasonNumber int    `json:"season_number"`
		PosterPath   string `json:"poster_path"`
	} `json:"seasons"`
	Collection *struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		PosterPath   string `json:"poster_path"`
		BackdropPath string `json:"backdrop_path"`
	} `json:"belongs_to_collection"`
}

// DisplayTitle returns the localized title.
func (d Details) DisplayTitle() string {
	if d.Title != "" {
		return d.Title
	}
	return d.Name
}

// Original returns the original-language title.
func (d Details) Original() string {
	if d.OriginalTitle != "" {
		return d.OriginalTitle
	}
	return d.OriginalName
}

// Date returns the release or first-air date.
func (d Details) Date() string {
	if d.ReleaseDate != "" {
		return d.ReleaseDate
	}
	return d.FirstAirDate
}

// Certification returns the age rating for country, falling back to the US one.
func (d Details) Certification(country string) string {
	pick := func(c string) string {
		for _, r := range d.ReleaseDates.Results {
			if r.Country == c {
				for _, rd := range r.Dates {
					if rd.Certification != "" {
						return rd.Certification
					}
				}
			}
		}
		for _, r := range d.ContentRatings.Results {
			if r.Country == c && r.Rating != "" {
				return r.Rating
			}
		}
		return ""
	}
	if v := pick(country); v != "" {
		return v
	}
	return pick("US")
}

// imageLanguages asks for logos in the UI language, then English, then language-neutral.
func imageLanguages(lang string) string {
	l := strings.ToLower(strings.SplitN(lang, "-", 2)[0])
	if l == "" || l == "en" {
		return "en,null"
	}
	return l + ",en,null"
}

// LogoPath picks the title logo: UI language first, then English, then neutral; best voted within each.
func (d Details) LogoPath(lang string) string {
	want := []string{strings.ToLower(strings.SplitN(lang, "-", 2)[0]), "en", ""}
	for _, l := range want {
		best, score := "", -1.0
		for _, g := range d.Images.Logos {
			if g.Language == l && g.Vote > score && strings.HasSuffix(strings.ToLower(g.FilePath), ".png") {
				best, score = g.FilePath, g.Vote
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

// Movie fetches movie details with credits and release dates.
func (c *TMDb) Movie(ctx context.Context, id int, lang string) (Details, error) {
	var d Details
	err := c.get(ctx, fmt.Sprintf("/movie/%d", id), url.Values{"language": {lang}, "append_to_response": {"credits,release_dates,images"},
		"include_image_language": {imageLanguages(lang)}}, &d)
	return d, err
}

// Series fetches series details with credits, content ratings and external IDs.
func (c *TMDb) Series(ctx context.Context, id int, lang string) (Details, error) {
	var d Details
	err := c.get(ctx, fmt.Sprintf("/tv/%d", id), url.Values{"language": {lang}, "append_to_response": {"credits,content_ratings,external_ids,images"},
		"include_image_language": {imageLanguages(lang)}}, &d)
	return d, err
}

// Episode is one episode of a season.
type Episode struct {
	EpisodeNumber int     `json:"episode_number"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	AirDate       string  `json:"air_date"`
	StillPath     string  `json:"still_path"`
	VoteAverage   float64 `json:"vote_average"`
}

// Season fetches the episodes of a season.
func (c *TMDb) Season(ctx context.Context, seriesID, season int, lang string) ([]Episode, string, error) {
	d, err := c.SeasonDetails(ctx, seriesID, season, lang)
	return d.Episodes, d.Overview, err
}

// SeasonDetails is a season with its poster and episodes.
type SeasonDetails struct {
	Overview   string    `json:"overview"`
	PosterPath string    `json:"poster_path"`
	Episodes   []Episode `json:"episodes"`
}

// SeasonDetails fetches a season with its poster and episodes.
func (c *TMDb) SeasonDetails(ctx context.Context, seriesID, season int, lang string) (SeasonDetails, error) {
	var out SeasonDetails
	err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", seriesID, season), url.Values{"language": {lang}}, &out)
	return out, err
}

// Image downloads artwork in the given size (e.g. "w500", "w1280").
func (c *TMDb) Image(ctx context.Context, path, size string) ([]byte, error) {
	if path == "" {
		return nil, errNotFound
	}
	base := c.ImageBase
	if base == "" {
		base = "https://image.tmdb.org/t/p"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/"+size+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image %s: HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

// StudioNames lists the networks (series) and production companies, at most five.
func (d Details) StudioNames() []string {
	out := []string{}
	for _, n := range d.Networks {
		out = append(out, n.Name)
	}
	for _, c := range d.Companies {
		out = append(out, c.Name)
	}
	out = clean(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}
