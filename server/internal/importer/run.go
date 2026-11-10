package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserMapping sends one source user's data to a Streamero user (nil UserID = skip).
type UserMapping struct {
	SourceUserID   string     `json:"sourceUserId"`
	SourceUserName string     `json:"sourceUserName"`
	UserID         *uuid.UUID `json:"userId,omitempty"`
}

// Options are what the administrator chose for one run.
type Options struct {
	Users        []UserMapping `json:"users"`
	PathMappings []PathMapping `json:"pathMappings"`
	Playlists    bool          `json:"playlists"`
}

// Progress is shown while a run is going.
type Progress struct {
	UsersDone   int    `json:"usersDone"`
	UsersTotal  int    `json:"usersTotal"`
	CurrentUser string `json:"currentUser,omitempty"`
}

// UserReport counts what one user got.
type UserReport struct {
	SourceUserName string    `json:"sourceUserName"`
	UserID         uuid.UUID `json:"userId"`
	Username       string    `json:"username"`
	Items          int       `json:"items"`
	Matched        int       `json:"matched"`
	Unmatched      int       `json:"unmatched"`
	Played         int       `json:"played"`
	Resume         int       `json:"resume"`
	Favorites      int       `json:"favorites"`
	Playlists      int       `json:"playlists"`
}

// Unmatched is one source item no Streamero item was found for.
type Unmatched struct {
	User string `json:"user"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
}

// Report is the result of a run (for a dry run: what would be imported).
type Report struct {
	Users                []UserReport `json:"users"`
	SkippedUsers         []string     `json:"skippedUsers"`
	MatchedByProvider    int          `json:"matchedByProvider"`
	MatchedByPath        int          `json:"matchedByPath"`
	MatchedByTitle       int          `json:"matchedByTitle"`
	Unmatched            int          `json:"unmatched"`
	UnmatchedSample      []Unmatched  `json:"unmatchedSample"`
	PlaylistsCreated     int          `json:"playlistsCreated"`
	PlaylistsUpdated     int          `json:"playlistsUpdated"`
	PlaylistItemsMissing int          `json:"playlistItemsMissing"`
	KeptNewer            int          `json:"keptNewer"`
}

// SampleSize caps the list of unmatched items in the report.
const SampleSize = 50

// Runner executes imports.
type Runner struct {
	Pool *pgxpool.Pool
	HTTP *http.Client // nil = default client
	Log  *slog.Logger
	// Now is the clock for rows without a source date (tests set it).
	Now func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC().Truncate(time.Microsecond)
	}
	return time.Now().UTC().Truncate(time.Microsecond)
}

func (r *Runner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

// Run executes a queued run: it claims the row, imports, stores the report and
// always forgets the API key at the end. A run that is not queued is ignored.
func (r *Runner) Run(ctx context.Context, runID uuid.UUID) error {
	r.sweepStale(ctx)
	var srcURL, key string
	var dry bool
	var rawOpts []byte
	err := r.Pool.QueryRow(ctx, `UPDATE import_runs SET status = 'running', started_at = now()
		WHERE id = $1 AND status = 'queued' RETURNING source_url, COALESCE(secret, ''), dry_run, options`, runID).
		Scan(&srcURL, &key, &dry, &rawOpts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var opts Options
	report, runErr := Report{}, json.Unmarshal(rawOpts, &opts)
	if runErr == nil {
		client := &Client{BaseURL: srcURL, Key: key, HTTP: r.HTTP}
		report, runErr = r.Execute(ctx, client, opts, dry, func(p Progress) {
			raw, _ := json.Marshal(p)
			_, _ = r.Pool.Exec(ctx, `UPDATE import_runs SET progress = $2 WHERE id = $1`, runID, raw)
		})
	}
	status, msg := "done", ""
	if runErr != nil {
		status, msg = "failed", runErr.Error()
		r.log().Warn("import failed", "run", runID, "err", runErr)
	} else {
		r.log().Info("import finished", "run", runID, "dry_run", dry, "unmatched", report.Unmatched)
	}
	rawReport, _ := json.Marshal(report)
	_, err = r.Pool.Exec(context.WithoutCancel(ctx), `UPDATE import_runs SET status = $2, error = NULLIF($3, ''), report = $4,
		secret = NULL, finished_at = now() WHERE id = $1`, runID, status, msg, rawReport)
	return err
}

// StaleAfter is how long a run may stay queued or running before it is considered
// abandoned (a crashed or stopped worker); it is longer than the job timeout.
const StaleAfter = 4 * time.Hour

// SweepStale fails runs a crashed worker left behind, so their API keys do not linger
// and a new import can start.
func SweepStale(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `UPDATE import_runs SET status = 'failed', error = 'interrupted', secret = NULL, finished_at = now()
		WHERE status IN ('queued', 'running') AND created_at < now() - make_interval(secs => $1)`, StaleAfter.Seconds())
	return err
}

func (r *Runner) sweepStale(ctx context.Context) {
	if err := SweepStale(ctx, r.Pool); err != nil {
		r.log().Warn("import: clearing abandoned runs failed", "err", err)
	}
}

// LocalUser is a Streamero account that can receive imported data.
type LocalUser struct {
	ID       uuid.UUID
	Username string
}

// SuggestUsers proposes a Streamero user for each source user: the account with the
// same username, compared case-insensitively. Source users without one are absent.
func SuggestUsers(source []User, local []LocalUser) map[string]uuid.UUID {
	byName := make(map[string]uuid.UUID, len(local))
	for _, u := range local {
		byName[strings.ToLower(strings.TrimSpace(u.Username))] = u.ID
	}
	out := map[string]uuid.UUID{}
	for _, u := range source {
		if id, ok := byName[strings.ToLower(strings.TrimSpace(u.Name))]; ok {
			out[u.ID] = id
		}
	}
	return out
}

type session struct {
	r      *Runner
	c      *Client
	ix     *Index
	series map[string]*Item // source series by id
	report *Report
	dry    bool
}

// Execute imports with an already configured client; dryRun only counts.
func (r *Runner) Execute(ctx context.Context, c *Client, opts Options, dryRun bool, progress func(Progress)) (Report, error) {
	rep := Report{Users: []UserReport{}, SkippedUsers: []string{}, UnmatchedSample: []Unmatched{}}
	if progress == nil {
		progress = func(Progress) {}
	}
	info, err := c.Info(ctx)
	if err != nil {
		return rep, err
	}
	ix, err := LoadIndex(ctx, r.Pool, opts.PathMappings)
	if err != nil {
		return rep, err
	}
	s := &session{r: r, c: c, ix: ix, series: map[string]*Item{}, report: &rep, dry: dryRun}

	// Streamero users that receive data, by id.
	names := map[uuid.UUID]string{}
	var ids []uuid.UUID
	for _, m := range opts.Users {
		if m.UserID != nil {
			ids = append(ids, *m.UserID)
		}
	}
	rows, err := r.Pool.Query(ctx, `SELECT id, username FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var id uuid.UUID
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return rep, err
		}
		names[id] = n
	}
	rows.Close()

	var mapped []UserMapping
	for _, m := range opts.Users {
		if m.UserID == nil || names[*m.UserID] == "" {
			rep.SkippedUsers = append(rep.SkippedUsers, m.SourceUserName)
			continue
		}
		mapped = append(mapped, m)
	}
	total := len(mapped)
	if opts.Playlists {
		total++
	}
	userIdx := map[string]int{}
	for i, m := range mapped {
		progress(Progress{UsersDone: i, UsersTotal: total, CurrentUser: m.SourceUserName})
		ur := UserReport{SourceUserName: m.SourceUserName, UserID: *m.UserID, Username: names[*m.UserID]}
		if err := s.importUser(ctx, m, &ur); err != nil {
			return rep, fmt.Errorf("user %s: %w", m.SourceUserName, err)
		}
		userIdx[m.SourceUserID] = len(rep.Users)
		rep.Users = append(rep.Users, ur)
	}
	if opts.Playlists {
		progress(Progress{UsersDone: len(mapped), UsersTotal: total})
		if err := s.importPlaylists(ctx, info.ID, mapped, userIdx); err != nil {
			return rep, fmt.Errorf("playlists: %w", err)
		}
	}
	progress(Progress{UsersDone: total, UsersTotal: total})
	return rep, nil
}

// loadSeries fetches the source series of these items that are not cached yet.
func (s *session) loadSeries(ctx context.Context, userID string, items []Item) error {
	var missing []string
	seen := map[string]bool{}
	for _, it := range items {
		if it.SeriesID != "" && s.series[it.SeriesID] == nil && !seen[it.SeriesID] {
			seen[it.SeriesID] = true
			missing = append(missing, it.SeriesID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	found, err := s.c.ItemsByID(ctx, userID, missing)
	if err != nil {
		return err
	}
	for i := range found {
		s.series[found[i].ID] = &found[i]
	}
	return nil
}

// match finds Streamero items and records how (or that nothing was found).
func (s *session) match(it Item, user string) ([]uuid.UUID, bool) {
	ids, how := s.ix.Match(it, s.series[it.SeriesID])
	switch how {
	case ByProvider:
		s.report.MatchedByProvider++
	case ByPath:
		s.report.MatchedByPath++
	case ByTitle:
		s.report.MatchedByTitle++
	default:
		s.report.Unmatched++
		if len(s.report.UnmatchedSample) < SampleSize {
			name := it.Name
			if it.SeriesName != "" {
				name = it.SeriesName + " — " + name
				if it.ParentIndexNumber != nil && it.IndexNumber != nil {
					name = fmt.Sprintf("%s S%02dE%02d", name, *it.ParentIndexNumber, *it.IndexNumber)
				}
			}
			s.report.UnmatchedSample = append(s.report.UnmatchedSample, Unmatched{User: user, Name: name, Kind: localKind(it.Type), Path: it.Path})
		}
		return nil, false
	}
	return ids, true
}

func (s *session) importUser(ctx context.Context, m UserMapping, ur *UserReport) error {
	items, err := s.c.UserItems(ctx, m.SourceUserID)
	if err != nil {
		return err
	}
	if err := s.loadSeries(ctx, m.SourceUserID, items); err != nil {
		return err
	}
	incoming := map[uuid.UUID]Incoming{}
	for _, it := range items {
		if it.UserData == nil || localKind(it.Type) == "" {
			continue
		}
		in := FromUserData(*it.UserData)
		if !in.Played && in.PositionMS == 0 && !in.Favorite {
			continue
		}
		ur.Items++
		ids, ok := s.match(it, m.SourceUserName)
		if !ok {
			ur.Unmatched++
			continue
		}
		ur.Matched++
		switch {
		case in.Played:
			ur.Played++
		case in.PositionMS > 0:
			ur.Resume++
		}
		if in.Favorite {
			ur.Favorites++
		}
		for _, id := range ids {
			if prev, ok := incoming[id]; ok {
				in = Combine(prev, in)
			}
			incoming[id] = in
		}
	}
	return s.write(ctx, *m.UserID, incoming)
}

// write merges the imported state into user_item_data of the user's main profile.
func (s *session) write(ctx context.Context, owner uuid.UUID, incoming map[uuid.UUID]Incoming) error {
	if len(incoming) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(incoming))
	for id := range incoming {
		ids = append(ids, id)
	}
	now := s.r.now()
	apply := func(tx pgx.Tx) error {
		lock := ""
		if !s.dry {
			lock = " FOR UPDATE"
		}
		rows, err := tx.Query(ctx, `SELECT item_id, position_ms, played, play_count, last_played_at, favorite
			FROM user_item_data WHERE owner_id = $1 AND item_id = ANY($2)`+lock, owner, ids)
		if err != nil {
			return err
		}
		cur := map[uuid.UUID]State{}
		for rows.Next() {
			var id uuid.UUID
			var st State
			if err := rows.Scan(&id, &st.PositionMS, &st.Played, &st.PlayCount, &st.LastPlayedAt, &st.Favorite); err != nil {
				rows.Close()
				return err
			}
			st.LastPlayedAt = st.LastPlayedAt.UTC()
			cur[id] = st
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		batch := &pgx.Batch{}
		for _, id := range ids {
			var prev *State
			if st, ok := cur[id]; ok {
				prev = &st
			}
			next, changed, kept := Merge(prev, incoming[id], now)
			if kept {
				s.report.KeptNewer++
			}
			if !changed || s.dry {
				continue
			}
			batch.Queue(`INSERT INTO user_item_data (owner_id, item_id, position_ms, played, play_count, last_played_at, favorite)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (owner_id, item_id) DO UPDATE SET position_ms = EXCLUDED.position_ms, played = EXCLUDED.played,
					play_count = EXCLUDED.play_count, last_played_at = EXCLUDED.last_played_at, favorite = EXCLUDED.favorite`,
				owner, id, next.PositionMS, next.Played, next.PlayCount, next.LastPlayedAt, next.Favorite)
		}
		if batch.Len() == 0 {
			return nil
		}
		return tx.SendBatch(ctx, batch).Close()
	}
	if s.dry {
		tx, err := s.r.Pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		return apply(tx)
	}
	return pgx.BeginFunc(ctx, s.r.Pool, apply)
}

type sourcePlaylist struct {
	item    Item
	viewers []string // source user ids that see it, in user order
}

func (s *session) importPlaylists(ctx context.Context, serverID string, mapped []UserMapping, userIdx map[string]int) error {
	users, err := s.c.Users(ctx)
	if err != nil {
		return err
	}
	byID := map[string]UserMapping{}
	for _, m := range mapped {
		byID[m.SourceUserID] = m
	}
	// Who sees which playlist: owners, people it is shared with, everyone for open ones.
	var order []string
	lists := map[string]*sourcePlaylist{}
	for _, u := range users {
		pls, err := s.c.Playlists(ctx, u.ID)
		if err != nil {
			return err
		}
		for _, p := range pls {
			if lists[p.ID] == nil {
				lists[p.ID] = &sourcePlaylist{item: p}
				order = append(order, p.ID)
			}
			lists[p.ID].viewers = append(lists[p.ID].viewers, u.ID)
		}
	}
	for _, id := range order {
		pl := lists[id]
		access, err := s.c.PlaylistAccess(ctx, id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		shared := map[string]bool{}
		for _, sh := range access.Shares {
			shared[sh.UserID] = true
		}
		var candidates []string
		for _, v := range pl.viewers {
			if !shared[v] {
				candidates = append(candidates, v)
			}
		}
		// The owner is the one viewer it is not shared with. An open playlist is seen by
		// everyone, so its owner cannot be told apart: the first mapped viewer gets it as public.
		var owner *UserMapping
		if len(candidates) == 1 {
			if m, ok := byID[candidates[0]]; ok {
				owner = &m
			}
		} else {
			for _, c := range candidates {
				if m, ok := byID[c]; ok {
					owner = &m
					break
				}
			}
		}
		if owner == nil {
			continue
		}
		public := access.OpenAccess || len(candidates) > 1
		entries, err := s.c.PlaylistItems(ctx, id, owner.SourceUserID)
		if err != nil {
			return err
		}
		if err := s.loadSeries(ctx, owner.SourceUserID, entries); err != nil {
			return err
		}
		var itemIDs []uuid.UUID
		for _, e := range entries {
			ids, how := s.ix.Match(e, s.series[e.SeriesID])
			if how == "" {
				s.report.PlaylistItemsMissing++
				continue
			}
			itemIDs = append(itemIDs, ids[0])
		}
		created, err := s.savePlaylist(ctx, serverID, id, *owner.UserID, pl.item.Name, public, itemIDs)
		if err != nil {
			return err
		}
		if created {
			s.report.PlaylistsCreated++
		} else {
			s.report.PlaylistsUpdated++
		}
		if i, ok := userIdx[owner.SourceUserID]; ok {
			s.report.Users[i].Playlists++
		}
	}
	return nil
}

// savePlaylist creates the playlist on the first import and replaces its name and
// items on later ones (found through import_playlists).
func (s *session) savePlaylist(ctx context.Context, serverID, sourceID string, owner uuid.UUID, name string, public bool,
	items []uuid.UUID) (bool, error) {
	var existing uuid.UUID
	err := s.r.Pool.QueryRow(ctx, `SELECT playlist_id FROM import_playlists WHERE source_server = $1 AND source_id = $2 AND owner_id = $3`,
		serverID, sourceID, owner).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	created := existing == uuid.Nil
	if s.dry {
		return created, nil
	}
	if name == "" {
		name = "Playlist"
	}
	return created, pgx.BeginFunc(ctx, s.r.Pool, func(tx pgx.Tx) error {
		id := existing
		if created {
			var err error
			if id, err = uuid.NewV7(); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO playlists (id, owner_id, name, public) VALUES ($1, $2, $3, $4)`, id, owner, name, public); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO import_playlists (source_server, source_id, owner_id, playlist_id) VALUES ($1, $2, $3, $4)`,
				serverID, sourceID, owner, id); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE playlists SET name = $2, public = $3, updated_at = now() WHERE id = $1`, id, name, public); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM playlist_items WHERE playlist_id = $1`, id); err != nil {
				return err
			}
		}
		for i, item := range items {
			if _, err := tx.Exec(ctx, `INSERT INTO playlist_items (playlist_id, position, item_id) VALUES ($1, $2, $3)`, id, i, item); err != nil {
				return err
			}
		}
		return nil
	})
}
