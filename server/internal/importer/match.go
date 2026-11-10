package importer

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// How an item was matched.
const (
	ByProvider = "provider"
	ByPath     = "path"
	ByTitle    = "title"
)

// LocalItem is a Streamero item as the matcher sees it.
type LocalItem struct {
	ID              uuid.UUID
	ParentID        uuid.UUID // Nil for top-level items
	Kind            string    // movie, series, season, episode, artist, album, track
	Path            string
	Title, AltTitle string // metadata title and the title from the file name
	Year            int
	Season, Episode int // -1 when unknown
	EpisodeEnd      int
	ProviderIDs     map[string]string
	FilePaths       []string
}

// PathMapping rewrites the source's path prefix to the one Streamero sees.
type PathMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Index finds Streamero items for source items.
type Index struct {
	items    map[uuid.UUID]*LocalItem
	provider map[string][]uuid.UUID // kind|provider|value
	paths    map[string]uuid.UUID
	titles   map[string][]uuid.UUID             // kind|normalized title|year
	children map[uuid.UUID]map[[2]int]uuid.UUID // series -> (season, episode) -> episode; episode 0 = season item
	mappings []PathMapping
}

// localKind maps a source item type to a Streamero item kind.
func localKind(t string) string {
	switch t {
	case "Movie":
		return "movie"
	case "Series":
		return "series"
	case "Season":
		return "season"
	case "Episode":
		return "episode"
	case "Audio":
		return "track"
	case "MusicAlbum":
		return "album"
	}
	return ""
}

// normTitle lowercases and keeps only letters and digits.
func normTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func providerKey(kind, provider, value string) string {
	return kind + "|" + strings.ToLower(provider) + "|" + strings.ToLower(strings.TrimSpace(value))
}

// Provider ids used for matching (source keys are compared case-insensitively).
var matchProviders = []string{"tmdb", "imdb", "tvdb"}

// NewIndex builds the matcher over Streamero items. Mappings are tried longest prefix first.
func NewIndex(items []LocalItem, mappings []PathMapping) *Index {
	ix := &Index{items: map[uuid.UUID]*LocalItem{}, provider: map[string][]uuid.UUID{}, paths: map[string]uuid.UUID{},
		titles: map[string][]uuid.UUID{}, children: map[uuid.UUID]map[[2]int]uuid.UUID{}}
	for i := range items {
		it := &items[i]
		ix.items[it.ID] = it
	}
	for _, it := range ix.items {
		for _, p := range matchProviders {
			if v := it.ProviderIDs[p]; v != "" {
				k := providerKey(it.Kind, p, v)
				ix.provider[k] = append(ix.provider[k], it.ID)
			}
		}
		if it.Path != "" {
			ix.paths[it.Path] = it.ID
		}
		for _, f := range it.FilePaths {
			ix.paths[f] = it.ID
		}
		seen := map[string]bool{}
		for _, t := range []string{it.Title, it.AltTitle} {
			n := normTitle(t)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			k := it.Kind + "|" + n + "|" + itoa(it.Year)
			ix.titles[k] = append(ix.titles[k], it.ID)
		}
		switch it.Kind {
		case "season", "episode":
			s := ix.seriesOf(it)
			if s == uuid.Nil {
				continue
			}
			if ix.children[s] == nil {
				ix.children[s] = map[[2]int]uuid.UUID{}
			}
			season := it.Season
			if season < 0 && it.Kind == "episode" {
				if p := ix.items[it.ParentID]; p != nil && p.Kind == "season" {
					season = p.Season
				}
			}
			if it.Kind == "season" && season >= 0 {
				ix.children[s][[2]int{season, 0}] = it.ID
			}
			if it.Kind == "episode" && season >= 0 && it.Episode > 0 {
				for e := it.Episode; e <= max(it.Episode, it.EpisodeEnd); e++ {
					ix.children[s][[2]int{season, e}] = it.ID
				}
			}
		}
	}
	for _, list := range [](map[string][]uuid.UUID){ix.provider, ix.titles} {
		for _, ids := range list {
			sort.Slice(ids, func(a, b int) bool { return ids[a].String() < ids[b].String() })
		}
	}
	for _, m := range mappings {
		from, to := strings.TrimRight(strings.ReplaceAll(m.From, `\`, "/"), "/"), strings.TrimRight(m.To, "/")
		if from != "" && to != "" {
			ix.mappings = append(ix.mappings, PathMapping{From: from, To: to})
		}
	}
	sort.SliceStable(ix.mappings, func(a, b int) bool { return len(ix.mappings[a].From) > len(ix.mappings[b].From) })
	return ix
}

func itoa(n int) string { return strconv.Itoa(n) }

// seriesOf walks up from a season or episode to its series.
func (ix *Index) seriesOf(it *LocalItem) uuid.UUID {
	for p, depth := ix.items[it.ParentID], 0; p != nil && depth < 3; p, depth = ix.items[p.ParentID], depth+1 {
		if p.Kind == "series" {
			return p.ID
		}
	}
	return uuid.Nil
}

// Len is the number of Streamero items in the index.
func (ix *Index) Len() int { return len(ix.items) }

// localPaths returns the source path and its rewritten forms, best first.
func (ix *Index) localPaths(p string) []string {
	if p == "" {
		return nil
	}
	p = strings.ReplaceAll(p, `\`, "/")
	var out []string
	for _, m := range ix.mappings {
		if p == m.From || strings.HasPrefix(p, m.From+"/") {
			out = append(out, path.Clean(m.To+strings.TrimPrefix(p, m.From)))
			break
		}
	}
	return append(out, p)
}

func (ix *Index) byPath(p, kind string) uuid.UUID {
	for _, lp := range ix.localPaths(p) {
		if id, ok := ix.paths[lp]; ok && (kind == "" || ix.items[id].Kind == kind) {
			return id
		}
	}
	return uuid.Nil
}

func (ix *Index) byProvider(kind string, ids map[string]string) []uuid.UUID {
	for _, p := range matchProviders {
		for k, v := range ids {
			if strings.EqualFold(k, p) && v != "" {
				if found := ix.provider[providerKey(kind, p, v)]; len(found) > 0 {
					return found
				}
			}
		}
	}
	return nil
}

func (ix *Index) byTitle(kind, title string, year int) []uuid.UUID {
	n := normTitle(title)
	if n == "" {
		return nil
	}
	if found := ix.titles[kind+"|"+n+"|"+itoa(year)]; len(found) > 0 || year == 0 {
		return found
	}
	return nil
}

// Match finds the Streamero items for a source item. Several results mean the
// same title exists in more than one library; all copies get the data.
// series is the source series of an episode or season (nil otherwise).
func (ix *Index) Match(it Item, series *Item) ([]uuid.UUID, string) {
	kind := localKind(it.Type)
	switch kind {
	case "movie", "series":
		if ids := ix.byProvider(kind, it.ProviderIDs); len(ids) > 0 {
			return ids, ByProvider
		}
		if id := ix.byPath(it.Path, kind); id != uuid.Nil {
			return []uuid.UUID{id}, ByPath
		}
		if ids := ix.byTitle(kind, it.Name, it.ProductionYear); len(ids) > 0 {
			return ids, ByTitle
		}
	case "season", "episode":
		key, ok := childKey(it)
		if ids := ix.byProvider(kind, it.ProviderIDs); len(ids) > 0 {
			return ids, ByProvider
		}
		var seriesIDs []uuid.UUID
		var how string
		if series != nil {
			seriesIDs, how = ix.Match(*series, nil)
		}
		if ok && how == ByProvider {
			if found := ix.children1(seriesIDs, key); len(found) > 0 {
				return found, ByProvider
			}
		}
		if id := ix.byPath(it.Path, kind); id != uuid.Nil {
			return []uuid.UUID{id}, ByPath
		}
		if ok && how != "" {
			if found := ix.children1(seriesIDs, key); len(found) > 0 {
				return found, how
			}
		}
	case "track", "album":
		if id := ix.byPath(it.Path, kind); id != uuid.Nil {
			return []uuid.UUID{id}, ByPath
		}
	}
	return nil, ""
}

func childKey(it Item) ([2]int, bool) {
	if it.Type == "Season" {
		if it.IndexNumber == nil {
			return [2]int{}, false
		}
		return [2]int{*it.IndexNumber, 0}, true
	}
	if it.IndexNumber == nil || it.ParentIndexNumber == nil {
		return [2]int{}, false
	}
	return [2]int{*it.ParentIndexNumber, *it.IndexNumber}, true
}

func (ix *Index) children1(seriesIDs []uuid.UUID, key [2]int) []uuid.UUID {
	var out []uuid.UUID
	for _, s := range seriesIDs {
		if id, ok := ix.children[s][key]; ok {
			out = append(out, id)
		}
	}
	return out
}

// LoadIndex reads every Streamero item and media file into a matcher.
func LoadIndex(ctx context.Context, pool *pgxpool.Pool, mappings []PathMapping) (*Index, error) {
	rows, err := pool.Query(ctx, `SELECT id, parent_id, kind, path, COALESCE(metadata_title, title), title, COALESCE(year, 0),
		COALESCE(season_number, -1), COALESCE(episode_number, -1), COALESCE(episode_number_end, -1), provider_ids FROM items`)
	if err != nil {
		return nil, err
	}
	var items []LocalItem
	pos := map[uuid.UUID]int{}
	for rows.Next() {
		var it LocalItem
		var parent *uuid.UUID
		var raw []byte
		if err := rows.Scan(&it.ID, &parent, &it.Kind, &it.Path, &it.Title, &it.AltTitle, &it.Year, &it.Season, &it.Episode,
			&it.EpisodeEnd, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if parent != nil {
			it.ParentID = *parent
		}
		ids := map[string]any{}
		_ = json.Unmarshal(raw, &ids)
		it.ProviderIDs = map[string]string{}
		for k, v := range ids {
			switch x := v.(type) {
			case string:
				it.ProviderIDs[strings.ToLower(k)] = x
			case float64:
				it.ProviderIDs[strings.ToLower(k)] = itoa(int(x))
			}
		}
		pos[it.ID] = len(items)
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	frows, err := pool.Query(ctx, `SELECT item_id, path FROM media_files`)
	if err != nil {
		return nil, err
	}
	defer frows.Close()
	for frows.Next() {
		var id uuid.UUID
		var p string
		if err := frows.Scan(&id, &p); err != nil {
			return nil, err
		}
		if i, ok := pos[id]; ok {
			items[i].FilePaths = append(items[i].FilePaths, p)
		}
	}
	if err := frows.Err(); err != nil {
		return nil, err
	}
	return NewIndex(items, mappings), nil
}
