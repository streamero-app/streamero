package library

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Prefs are a viewer's playback preferences.
type Prefs struct {
	AudioLanguage    string `json:"audioLanguage,omitempty"`
	SubtitleLanguage string `json:"subtitleLanguage,omitempty"`
	SubtitleMode     string `json:"subtitleMode,omitempty"`
	AutoplayNext     *bool  `json:"autoplayNext,omitempty"`
	// Player and display preferences (applied by the clients; bitrate and height
	// are also enforced by the server).
	SkipBackSeconds    int      `json:"skipBackSeconds,omitempty"`
	SkipForwardSeconds int      `json:"skipForwardSeconds,omitempty"`
	MaxBitrateKbps     int      `json:"maxBitrateKbps,omitempty"`
	MaxHeight          int      `json:"maxHeight,omitempty"`
	UpNextSeconds      *int     `json:"upNextSeconds,omitempty"`
	SkipIntro          string   `json:"skipIntro,omitempty"`
	SkipCredits        string   `json:"skipCredits,omitempty"`
	SubtitleSize       string   `json:"subtitleSize,omitempty"`
	SubtitleColor      string   `json:"subtitleColor,omitempty"`
	SubtitleBackground string   `json:"subtitleBackground,omitempty"`
	SubtitlePosition   int      `json:"subtitlePosition,omitempty"`
	HomeSections       []string `json:"homeSections,omitempty"`
}

// Autoplay reports whether the next episode starts on its own (default on).
func (p Prefs) Autoplay() bool { return p.AutoplayNext == nil || *p.AutoplayNext }

// Mode returns the subtitle mode, "default" when unset.
func (p Prefs) Mode() string {
	if p.SubtitleMode == "" {
		return "default"
	}
	return p.SubtitleMode
}

// GetPrefs returns the preferences of owner (defaults when none are stored).
func (s *Service) GetPrefs(ctx context.Context, owner uuid.UUID) (Prefs, error) {
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT prefs FROM viewer_preferences WHERE owner_id = $1`, owner).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, err
	}
	var p Prefs
	return p, json.Unmarshal(raw, &p)
}

// SavePrefs stores the preferences of owner.
func (s *Service) SavePrefs(ctx context.Context, owner uuid.UUID, p Prefs) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO viewer_preferences (owner_id, prefs) VALUES ($1, $2)
		ON CONFLICT (owner_id) DO UPDATE SET prefs = EXCLUDED.prefs, updated_at = now()`, owner, raw)
	return err
}

// languages maps ISO 639-1 codes to their 639-2 forms (B and T) so "hr" matches "hrv".
var languages = map[string][]string{
	"hr": {"hrv", "scr"}, "en": {"eng"}, "de": {"ger", "deu"}, "fr": {"fre", "fra"}, "es": {"spa"}, "it": {"ita"},
	"sr": {"srp", "scc"}, "bs": {"bos"}, "sl": {"slv"}, "mk": {"mac", "mkd"}, "ja": {"jpn"}, "ko": {"kor"},
	"zh": {"chi", "zho"}, "ru": {"rus"}, "pl": {"pol"}, "pt": {"por"}, "nl": {"dut", "nld"}, "sv": {"swe"},
	"no": {"nor", "nob", "nno"}, "da": {"dan"}, "fi": {"fin"}, "cs": {"cze", "ces"}, "sk": {"slo", "slk"},
	"hu": {"hun"}, "tr": {"tur"}, "el": {"gre", "ell"}, "ro": {"rum", "ron"}, "bg": {"bul"}, "uk": {"ukr"},
	"ar": {"ara"}, "he": {"heb"}, "hi": {"hin"}, "th": {"tha"}, "vi": {"vie"}, "id": {"ind"},
}

// LangKey normalizes a language code to its ISO 639-1 form when known.
func LangKey(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	if c == "" {
		return ""
	}
	if _, ok := languages[c]; ok {
		return c
	}
	for k, alts := range languages {
		for _, a := range alts {
			if a == c {
				return k
			}
		}
	}
	return c
}

// SameLanguage compares codes across ISO 639-1/-2 forms.
func SameLanguage(a, b string) bool {
	return a != "" && b != "" && LangKey(a) == LangKey(b)
}

// PickAudio returns the first track in the preferred language, else the default (0).
func PickAudio(tracks []AudioStream, pref string) int {
	if pref != "" {
		for i, t := range tracks {
			if SameLanguage(t.Language, pref) {
				return i
			}
		}
	}
	return 0
}

// SubtitleChoice is a subtitle as seen by PickSubtitle.
type SubtitleChoice struct {
	Language string
	Forced   bool
}

// PickSubtitle applies the viewer's subtitle mode; it returns a position in subs or -1 (off).
func PickSubtitle(subs []SubtitleChoice, audioLang string, p Prefs) int {
	find := func(lang string, forced bool) int {
		for i, s := range subs {
			if s.Forced == forced && (lang == "" || SameLanguage(s.Language, lang)) {
				return i
			}
		}
		return -1
	}
	forcedForAudio := func() int {
		if audioLang != "" {
			if i := find(audioLang, true); i >= 0 {
				return i
			}
		}
		// A forced track without a language usually belongs to the main audio.
		for i, s := range subs {
			if s.Forced && s.Language == "" {
				return i
			}
		}
		return -1
	}
	foreignAudio := p.SubtitleLanguage != "" && audioLang != "" && !SameLanguage(audioLang, p.SubtitleLanguage)
	switch p.Mode() {
	case "none":
		return -1
	case "forced":
		return forcedForAudio()
	case "always":
		if p.SubtitleLanguage == "" {
			return -1
		}
		if i := find(p.SubtitleLanguage, false); i >= 0 {
			return i
		}
		return find(p.SubtitleLanguage, true)
	case "smart":
		if foreignAudio {
			return find(p.SubtitleLanguage, false)
		}
		return -1
	default:
		if i := forcedForAudio(); i >= 0 {
			return i
		}
		if foreignAudio {
			return find(p.SubtitleLanguage, false)
		}
		return -1
	}
}

// seriesOf is the SQL expression for an episode's series (episodes sit under a
// season or directly under the series).
const seriesOf = `CASE WHEN par.kind = 'season' THEN par.parent_id ELSE e.parent_id END`

// episodesOf is a subquery of the episodes of a series (id, season_number,
// episode_number): those directly under it and those in its seasons. Both halves
// look the parent up in items_parent_idx; matching on seriesOf instead would read
// every episode of every library for each series.
func episodesOf(series string) string {
	return `SELECT e.id, e.season_number, e.episode_number FROM items e
			WHERE e.parent_id = ` + series + ` AND e.kind = 'episode'
		UNION ALL
		SELECT e.id, e.season_number, e.episode_number FROM items s JOIN items e ON e.parent_id = s.id
			WHERE s.parent_id = ` + series + ` AND s.kind = 'season' AND e.kind = 'episode'`
}

// NextUp returns, for each series the owner has watched an episode of, the next
// unwatched episode after the furthest one watched; most recently watched first.
func (s *Service) NextUp(ctx context.Context, owner uuid.UUID, limit int) ([]Item, error) {
	items, err := s.queryItems(ctx, `WITH watched AS (
			SELECT DISTINCT ON (series_id) series_id, season, episode, last_played_at FROM (
				SELECT `+seriesOf+` AS series_id, COALESCE(e.season_number, 1) AS season, e.episode_number AS episode, u.last_played_at
				FROM user_item_data u
				JOIN items e ON e.id = u.item_id AND e.kind = 'episode'
				JOIN items par ON par.id = e.parent_id
				WHERE u.owner_id = $1 AND u.played AND COALESCE(e.season_number, 1) > 0
			) w ORDER BY series_id, season DESC, episode DESC
		)
		SELECT `+itemCols+` FROM watched w
		JOIN LATERAL (
			SELECT e.id FROM (`+episodesOf("w.series_id")+`) e
			LEFT JOIN user_item_data x ON x.item_id = e.id AND x.owner_id = $1
			WHERE COALESCE(e.season_number, 1) > 0
				AND (COALESCE(e.season_number, 1), e.episode_number) > (w.season, w.episode)
				AND NOT COALESCE(x.played, false) AND COALESCE(x.position_ms, 0) = 0
			ORDER BY COALESCE(e.season_number, 1), e.episode_number LIMIT 1
		) nxt ON true
		JOIN items i ON i.id = nxt.id
		LEFT JOIN user_item_data u ON u.item_id = i.id AND u.owner_id = $1
		ORDER BY w.last_played_at DESC LIMIT $2`, owner, limit)
	if err != nil {
		return nil, err
	}
	return items, s.AttachSeries(ctx, items)
}

// NextEpisode returns the episode after id in its series (any watch state).
func (s *Service) NextEpisode(ctx context.Context, owner, id uuid.UUID) (Item, error) {
	items, err := s.queryItems(ctx, `WITH cur AS (
			SELECT `+seriesOf+` AS series_id, COALESCE(e.season_number, 1) AS season, e.episode_number AS episode
			FROM items e JOIN items par ON par.id = e.parent_id WHERE e.id = $2 AND e.kind = 'episode'
		)
		SELECT `+itemCols+` FROM cur
		JOIN LATERAL (
			SELECT e.id FROM (`+episodesOf("cur.series_id")+`) e
			WHERE (COALESCE(e.season_number, 1), e.episode_number) > (cur.season, cur.episode)
				AND COALESCE(e.season_number, 1) > 0
			ORDER BY COALESCE(e.season_number, 1), e.episode_number LIMIT 1
		) nxt ON true
		JOIN items i ON i.id = nxt.id
		LEFT JOIN user_item_data u ON u.item_id = i.id AND u.owner_id = $1`, owner, id)
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, ErrNotFound
	}
	if err := s.AttachSeries(ctx, items); err != nil {
		return Item{}, err
	}
	return items[0], nil
}

// AttachSeries fills SeriesID/SeriesTitle of episodes.
func (s *Service) AttachSeries(ctx context.Context, items []Item) error {
	var ids []uuid.UUID
	for _, it := range items {
		if it.Kind == "episode" {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT e.id, ser.id, COALESCE(ser.metadata_title, ser.title)
		FROM items e JOIN items par ON par.id = e.parent_id
		JOIN items ser ON ser.id = `+seriesOf+`
		WHERE e.id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	type series struct {
		id    uuid.UUID
		title string
	}
	m := map[uuid.UUID]series{}
	for rows.Next() {
		var id uuid.UUID
		var se series
		if err := rows.Scan(&id, &se.id, &se.title); err != nil {
			return err
		}
		m[id] = se
	}
	for i := range items {
		if se, ok := m[items[i].ID]; ok {
			id := se.id
			items[i].SeriesID, items[i].SeriesTitle = &id, se.title
		}
	}
	return rows.Err()
}
