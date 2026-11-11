package livetv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GuideSource is an XMLTV guide source.
type GuideSource struct {
	ID            uuid.UUID
	Name          string
	URL           string
	FilePath      string
	Timezone      string
	RefreshHours  int
	Enabled       bool
	ChannelCount  int
	ProgramCount  int
	LastRefreshAt *time.Time
	LastChangedAt *time.Time
	LastResult    string
	Error         string
}

// GuideInput is what an administrator enters.
type GuideInput struct {
	Name         string
	URL          string
	FilePath     string
	Timezone     string
	RefreshHours *int
	Enabled      *bool
}

const guideCols = `g.id, g.name, g.url, g.file_path, g.timezone, g.refresh_hours, g.enabled,
	(SELECT count(*) FROM livetv_guide_channels gc WHERE gc.guide_id = g.id),
	(SELECT count(*) FROM livetv_programs p WHERE p.guide_id = g.id),
	g.last_refresh_at, g.last_changed_at, g.last_result, g.error`

func scanGuide(r pgx.Row) (GuideSource, error) {
	var g GuideSource
	err := r.Scan(&g.ID, &g.Name, &g.URL, &g.FilePath, &g.Timezone, &g.RefreshHours, &g.Enabled, &g.ChannelCount, &g.ProgramCount,
		&g.LastRefreshAt, &g.LastChangedAt, &g.LastResult, &g.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// Guides lists the guide sources.
func (s *Service) Guides(ctx context.Context) ([]GuideSource, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+guideCols+` FROM livetv_guides g ORDER BY g.name, g.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GuideSource{}
	for rows.Next() {
		g, err := scanGuide(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GuideSource returns one guide source.
func (s *Service) GuideSource(ctx context.Context, id uuid.UUID) (GuideSource, error) {
	return scanGuide(s.Pool.QueryRow(ctx, `SELECT `+guideCols+` FROM livetv_guides g WHERE g.id = $1`, id))
}

func (in *GuideInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)
	in.FilePath = strings.TrimSpace(in.FilePath)
	in.Timezone = strings.TrimSpace(in.Timezone)
	if in.Name == "" {
		return invalid("give the guide a name")
	}
	if (in.URL == "") == (in.FilePath == "") {
		return invalid("give either a guide URL or a guide file")
	}
	if in.URL != "" {
		if _, err := httpURL(in.URL); err != nil {
			return invalid("the guide URL must start with http:// or https://")
		}
	}
	if in.Timezone != "" {
		if _, err := time.LoadLocation(in.Timezone); err != nil {
			return invalid("unknown time zone %q", in.Timezone)
		}
	}
	if in.RefreshHours != nil && (*in.RefreshHours < 1 || *in.RefreshHours > 168) {
		return invalid("refresh every 1 to 168 hours")
	}
	return nil
}

func zone(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	if l, err := time.LoadLocation(name); err == nil {
		return l
	}
	return time.UTC
}

// TestGuide reads a guide without saving it.
func (s *Service) TestGuide(ctx context.Context, in GuideInput) TestResult {
	if err := in.validate(); err != nil {
		return TestResult{Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	f, err := open(ctx, s.http(), s.FileRoots, Source{URL: in.URL, FilePath: in.FilePath})
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	defer func() { _ = f.Body.Close() }()
	var chans, progs int
	var first, last time.Time
	err = ParseXMLTV(f.Body, zone(in.Timezone), func(XMLTVChannel) { chans++ }, func(p XMLTVProgramme) {
		progs++
		if first.IsZero() || p.Start.Before(first) {
			first = p.Start
		}
		if p.Start.After(last) {
			last = p.Start
		}
	})
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	msg := fmt.Sprintf("%d channels, %d programmes", chans, progs)
	if progs > 0 {
		msg += fmt.Sprintf(" (%s – %s)", first.UTC().Format("2006-01-02 15:04"), last.UTC().Format("2006-01-02 15:04 MST"))
	}
	return TestResult{OK: true, Message: msg, ChannelCount: &chans, ProgramCount: &progs}
}

// CreateGuide adds a guide source and queues its first load.
func (s *Service) CreateGuide(ctx context.Context, in GuideInput) (GuideSource, error) {
	if err := in.validate(); err != nil {
		return GuideSource{}, err
	}
	id, _ := uuid.NewV7()
	hours := 12
	if in.RefreshHours != nil {
		hours = *in.RefreshHours
	}
	enabled := in.Enabled == nil || *in.Enabled
	if _, err := s.Pool.Exec(ctx, `INSERT INTO livetv_guides (id, name, url, file_path, timezone, refresh_hours, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, in.Name, in.URL, in.FilePath, in.Timezone, hours, enabled); err != nil {
		return GuideSource{}, err
	}
	if enabled {
		_ = s.QueueGuideRefresh(ctx, id)
	}
	return s.GuideSource(ctx, id)
}

// UpdateGuide changes a guide source; a changed address loads it again.
func (s *Service) UpdateGuide(ctx context.Context, id uuid.UUID, in GuideInput) (GuideSource, error) {
	if err := in.validate(); err != nil {
		return GuideSource{}, err
	}
	cur, err := s.GuideSource(ctx, id)
	if err != nil {
		return GuideSource{}, err
	}
	hours := cur.RefreshHours
	if in.RefreshHours != nil {
		hours = *in.RefreshHours
	}
	enabled := in.Enabled == nil || *in.Enabled
	if _, err := s.Pool.Exec(ctx, `UPDATE livetv_guides SET name = $2, url = $3, file_path = $4, timezone = $5, refresh_hours = $6,
		enabled = $7, content_hash = CASE WHEN url <> $3 OR file_path <> $4 OR timezone <> $5 THEN '' ELSE content_hash END,
		etag = CASE WHEN url <> $3 THEN '' ELSE etag END, last_modified = CASE WHEN url <> $3 THEN '' ELSE last_modified END
		WHERE id = $1`, id, in.Name, in.URL, in.FilePath, in.Timezone, hours, enabled); err != nil {
		return GuideSource{}, err
	}
	if enabled && (cur.URL != in.URL || cur.FilePath != in.FilePath || cur.Timezone != in.Timezone) {
		_ = s.QueueGuideRefresh(ctx, id)
	}
	return s.GuideSource(ctx, id)
}

// DeleteGuide removes a guide source with its programmes; channels mapped to it lose their mapping.
func (s *Service) DeleteGuide(ctx context.Context, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM livetv_guides WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE livetv_channels SET mapping_mode = 'auto', guide_channel = '' WHERE mapping_mode = 'manual' AND guide_id IS NULL`)
	return s.AutoMap(ctx)
}

// QueueGuideRefresh asks the worker to reload a guide (or does it in the background).
func (s *Service) QueueGuideRefresh(ctx context.Context, id uuid.UUID) error {
	if _, err := s.GuideSource(ctx, id); err != nil {
		return err
	}
	if s.EnqueueGuideRefresh != nil {
		return s.EnqueueGuideRefresh(ctx, id)
	}
	go func() { //nolint:gosec // G118: the refresh outlives the request that queued it
		rctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if _, err := s.RefreshGuide(rctx, id, true); err != nil {
			s.log().Warn("guide refresh failed", "guide", id, "err", err)
		}
	}()
	return nil
}

// RefreshDueGuides reloads every enabled guide whose interval has passed.
func (s *Service) RefreshDueGuides(ctx context.Context) error {
	rows, err := s.Pool.Query(ctx, `SELECT id FROM livetv_guides WHERE enabled AND
		(last_refresh_at IS NULL OR last_refresh_at < now() - make_interval(hours => refresh_hours))`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if _, err := s.RefreshGuide(ctx, id, false); err != nil {
			errs = append(errs, err)
		}
	}
	// Removes programmes that ended more than a day ago.
	if _, err := s.Pool.Exec(ctx, `DELETE FROM livetv_programs WHERE end_at < now() - interval '1 day'`); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// RefreshResult counts what a guide refresh did.
type RefreshResult struct {
	Skipped                           bool // the source had not changed
	Inserted, Updated, Unchanged, Deleted int
	Channels                          int
}

func (r RefreshResult) String() string {
	if r.Skipped {
		return "unchanged"
	}
	return fmt.Sprintf("%d new, %d changed, %d unchanged, %d removed", r.Inserted, r.Updated, r.Unchanged, r.Deleted)
}

type parsedProgramme struct {
	XMLTVProgramme
}

func programmeHash(p XMLTVProgramme) string {
	h := sha256.New()
	field := func(v string) { h.Write([]byte(v)); h.Write([]byte{0}) }
	field(p.Stop.UTC().Format(time.RFC3339))
	field(p.Title)
	field(p.SubTitle)
	field(p.Desc)
	field(strings.Join(p.Categories, "\x1f"))
	num := func(v *int) {
		if v != nil {
			field(fmt.Sprint(*v))
		} else {
			field("")
		}
	}
	num(p.Season)
	num(p.Episode)
	num(p.Year)
	field(p.EpisodeKey)
	field(p.Icon)
	field(fmt.Sprint(p.New, p.Premiere, p.Repeat, p.Live))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// RefreshGuide loads a guide source. An unchanged source (HTTP 304, same file
// time or same content hash) is skipped unless force; otherwise programmes are
// staged and only new or changed ones are written, so an unchanged programme
// keeps its row (and id) untouched.
func (s *Service) RefreshGuide(ctx context.Context, id uuid.UUID, force bool) (RefreshResult, error) {
	var g struct {
		url, file, tz, etag, lastMod, hash string
	}
	if err := s.Pool.QueryRow(ctx, `SELECT url, file_path, timezone, etag, last_modified, content_hash FROM livetv_guides WHERE id = $1`, id).
		Scan(&g.url, &g.file, &g.tz, &g.etag, &g.lastMod, &g.hash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RefreshResult{}, ErrNotFound
		}
		return RefreshResult{}, err
	}
	res, err := s.refreshGuide(ctx, id, g.url, g.file, g.tz, g.etag, g.lastMod, g.hash, force)
	if err != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE livetv_guides SET error = $2, last_refresh_at = now() WHERE id = $1`, id, err.Error())
		return res, err
	}
	s.Kick() // series rules see the new programmes
	return res, nil
}

func (s *Service) refreshGuide(ctx context.Context, id uuid.UUID, u, file, tz, etag, lastMod, oldHash string, force bool) (RefreshResult, error) {
	src := Source{URL: u, FilePath: file}
	if !force {
		src.ETag, src.LastModified = etag, lastMod
	}
	f, err := open(ctx, s.http(), s.FileRoots, src)
	if err != nil {
		return RefreshResult{}, err
	}
	if f.NotModified {
		_, err := s.Pool.Exec(ctx, `UPDATE livetv_guides SET error = '', last_refresh_at = now(), last_result = 'unchanged' WHERE id = $1`, id)
		return RefreshResult{Skipped: true}, err
	}
	defer func() { _ = f.Body.Close() }()
	h := sha256.New()
	var chans []XMLTVChannel
	var progs []parsedProgramme
	err = ParseXMLTV(io.TeeReader(f.Body, h), zone(tz), func(c XMLTVChannel) { chans = append(chans, c) },
		func(p XMLTVProgramme) { progs = append(progs, parsedProgramme{XMLTVProgramme: p}) })
	if err != nil {
		return RefreshResult{}, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if !force && sum == oldHash {
		_, err := s.Pool.Exec(ctx, `UPDATE livetv_guides SET error = '', last_refresh_at = now(), last_result = 'unchanged',
			etag = $2, last_modified = $3 WHERE id = $1`, id, f.ETag, f.LastModified)
		return RefreshResult{Skipped: true}, err
	}
	fillStops(progs)
	res := RefreshResult{Channels: len(chans)}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// One refresh per guide at a time (the worker's schedule and an admin's "Refresh").
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "livetv.guide:"+id.String()); err != nil {
		return res, err
	}
	// Guide channels: replaced as a whole (small).
	if _, err := tx.Exec(ctx, `DELETE FROM livetv_guide_channels WHERE guide_id = $1`, id); err != nil {
		return res, err
	}
	seen := map[string]bool{}
	chanRows := make([][]any, 0, len(chans))
	for _, c := range chans {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		chanRows = append(chanRows, []any{id, c.ID, c.Names, c.Icon})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"livetv_guide_channels"}, []string{"guide_id", "xmltv_id", "names", "icon_url"},
		pgx.CopyFromRows(chanRows)); err != nil {
		return res, err
	}
	// Programmes: staged, then merged.
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE livetv_stage (LIKE livetv_programs INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
		return res, err
	}
	stage := make([][]any, 0, len(progs))
	dup := map[string]bool{}
	var minStart time.Time
	for _, p := range progs {
		k := p.Channel + "|" + p.Start.UTC().Format(time.RFC3339)
		if dup[k] || p.Stop.IsZero() {
			continue
		}
		dup[k] = true
		if minStart.IsZero() || p.Start.Before(minStart) {
			minStart = p.Start
		}
		pid, _ := uuid.NewV7()
		cats := p.Categories
		if cats == nil {
			cats = []string{}
		}
		stage = append(stage, []any{pid, id, p.Channel, p.Start, p.Stop, p.Title, p.SubTitle, p.Desc, cats, CategoryOf(p.Categories),
			p.Season, p.Episode, p.EpisodeKey, p.Year, p.New, p.Premiere, p.Repeat, p.Live, p.Icon, programmeHash(p.XMLTVProgramme)})
	}
	cols := []string{"id", "guide_id", "xmltv_channel", "start_at", "end_at", "title", "subtitle", "description", "categories", "category",
		"season", "episode", "episode_key", "year", "is_new", "is_premiere", "is_repeat", "is_live", "image_url", "hash"}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"livetv_stage"}, cols, pgx.CopyFromRows(stage)); err != nil {
		return res, err
	}
	tag, err := tx.Exec(ctx, `UPDATE livetv_programs p SET end_at = s.end_at, title = s.title, subtitle = s.subtitle,
			description = s.description, categories = s.categories, category = s.category, season = s.season, episode = s.episode,
			episode_key = s.episode_key, year = s.year, is_new = s.is_new, is_premiere = s.is_premiere, is_repeat = s.is_repeat,
			is_live = s.is_live, image_url = s.image_url, hash = s.hash
		FROM livetv_stage s WHERE p.guide_id = s.guide_id AND p.xmltv_channel = s.xmltv_channel AND p.start_at = s.start_at
			AND p.hash <> s.hash`)
	if err != nil {
		return res, err
	}
	res.Updated = int(tag.RowsAffected())
	tag, err = tx.Exec(ctx, `INSERT INTO livetv_programs SELECT s.* FROM livetv_stage s
		ON CONFLICT (guide_id, xmltv_channel, start_at) DO NOTHING`)
	if err != nil {
		return res, err
	}
	res.Inserted = int(tag.RowsAffected())
	res.Unchanged = len(stage) - res.Inserted - res.Updated
	if !minStart.IsZero() {
		// Airings the guide no longer lists (from its first programme on) are gone.
		tag, err = tx.Exec(ctx, `DELETE FROM livetv_programs p WHERE p.guide_id = $1 AND p.start_at >= $2
			AND NOT EXISTS (SELECT 1 FROM livetv_stage s WHERE s.xmltv_channel = p.xmltv_channel AND s.start_at = p.start_at)`, id, minStart)
		if err != nil {
			return res, err
		}
		res.Deleted = int(tag.RowsAffected())
	}
	changed := res.Inserted+res.Updated+res.Deleted > 0
	if _, err := tx.Exec(ctx, `UPDATE livetv_guides SET error = '', last_refresh_at = now(), content_hash = $2, etag = $3,
		last_modified = $4, last_result = $5, last_changed_at = CASE WHEN $6 THEN now() ELSE last_changed_at END WHERE id = $1`,
		id, sum, f.ETag, f.LastModified, res.String(), changed); err != nil {
		return res, err
	}
	if err := tx.Commit(ctx); err != nil {
		return res, err
	}
	s.log().Info("guide refreshed", "guide", id, "result", res.String())
	return res, s.AutoMap(ctx)
}

// fillStops gives programmes without a stop time the start of the next
// programme on the same channel (the XMLTV DTD makes stop optional).
func fillStops(progs []parsedProgramme) {
	byChan := map[string][]int{}
	for i, p := range progs {
		byChan[p.Channel] = append(byChan[p.Channel], i)
	}
	for _, idx := range byChan {
		sort.Slice(idx, func(a, b int) bool { return progs[idx[a]].Start.Before(progs[idx[b]].Start) })
		for n, i := range idx {
			if !progs[i].Stop.IsZero() {
				continue
			}
			if n+1 < len(idx) {
				progs[i].Stop = progs[idx[n+1]].Start
			}
		}
	}
}

// AutoMap maps channels in "auto" mode to guide channels: first by tvg-id
// against XMLTV channel ids, then by name (accents, case, punctuation and
// quality suffixes ignored) against display names, then by channel number
// against a display name that is that number.
func (s *Service) AutoMap(ctx context.Context) error {
	type gch struct {
		guide uuid.UUID
		id    string
		names []string
	}
	rows, err := s.Pool.Query(ctx, `SELECT gc.guide_id, gc.xmltv_id, gc.names FROM livetv_guide_channels gc
		JOIN livetv_guides g ON g.id = gc.guide_id WHERE g.enabled ORDER BY g.created_at, gc.xmltv_id`)
	if err != nil {
		return err
	}
	var gcs []gch
	for rows.Next() {
		var g gch
		if err := rows.Scan(&g.guide, &g.id, &g.names); err != nil {
			rows.Close()
			return err
		}
		gcs = append(gcs, g)
	}
	rows.Close()
	byID := map[string]gch{}
	byName := map[string]gch{}
	byNumber := map[string]gch{}
	for _, g := range gcs {
		if _, ok := byID[strings.ToLower(g.id)]; !ok {
			byID[strings.ToLower(g.id)] = g
		}
		for _, n := range g.names {
			if k := normName(n); k != "" {
				if _, ok := byName[k]; !ok {
					byName[k] = g
				}
			}
			if f := strings.Fields(n); len(f) > 0 && isChannelNumber(f[0]) {
				if _, ok := byNumber[f[0]]; !ok {
					byNumber[f[0]] = g
				}
			}
		}
		if k := normName(g.id); k != "" {
			if _, ok := byName[k]; !ok {
				byName[k] = g
			}
		}
	}
	crow, err := s.Pool.Query(ctx, `SELECT c.id, c.tvg_id, c.name, c.number, t.kind FROM livetv_channels c
		JOIN livetv_tuners t ON t.id = c.tuner_id WHERE c.mapping_mode = 'auto'`)
	if err != nil {
		return err
	}
	type upd struct {
		id    uuid.UUID
		guide *uuid.UUID
		gch   string
	}
	var ups []upd
	for crow.Next() {
		var id uuid.UUID
		var tvg, name, number, kind string
		if err := crow.Scan(&id, &tvg, &name, &number, &kind); err != nil {
			crow.Close()
			return err
		}
		var hit *gch
		if g, ok := byID[strings.ToLower(tvg)]; ok && tvg != "" {
			hit = &g
		} else if g, ok := byName[normName(name)]; ok && normName(name) != "" {
			hit = &g
		} else if g, ok := byName[normName(tvg)]; ok && normName(tvg) != "" {
			hit = &g
		} else if g, ok := byNumber[number]; ok && kind == "hdhomerun" {
			hit = &g
		}
		if hit != nil {
			gid := hit.guide
			ups = append(ups, upd{id, &gid, hit.id})
		} else {
			ups = append(ups, upd{id, nil, ""})
		}
	}
	crow.Close()
	if err := crow.Err(); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, u := range ups {
		batch.Queue(`UPDATE livetv_channels SET guide_id = $2, guide_channel = $3 WHERE id = $1 AND mapping_mode = 'auto'
			AND (guide_id IS DISTINCT FROM $2 OR guide_channel <> $3)`, u.id, u.guide, u.gch)
	}
	if batch.Len() == 0 {
		return nil
	}
	return s.Pool.SendBatch(ctx, batch).Close()
}

func isChannelNumber(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case (r == '.' || r == '-') && !dot && i > 0 && i < len(s)-1:
			dot = true
		default:
			return false
		}
	}
	return true
}
