package livetv

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Recording is a scheduled, running or finished recording.
type Recording struct {
	ID               uuid.UUID
	ChannelID        *uuid.UUID
	ChannelName      string
	ChannelNumber    string
	ProgramID        *uuid.UUID
	SeriesRuleID     *uuid.UUID
	Title            string
	SubTitle         string
	Description      string
	Categories       []string
	Season, Episode  *int
	Year             *int
	EpisodeKey       string
	ImageURL         string
	RawImage         string
	Start, End       time.Time
	PrePadding       int
	PostPadding      int
	Priority         int
	Status           string
	Error            string
	Path             string
	SizeBytes        *int64
	ItemID           *uuid.UUID
	PostProcessError string
	CreatedAt        time.Time
	FinishedAt       *time.Time
}

const recordingCols = `r.id, r.channel_id, r.channel_name, r.channel_number, r.program_id, r.series_rule_id, r.title, r.subtitle,
	r.description, r.categories, r.season, r.episode, r.year, r.episode_key, r.image_url, r.start_at, r.end_at, r.pre_padding,
	r.post_padding, r.priority, r.status, r.error, r.path, r.size_bytes,
	(SELECT f.item_id FROM media_files f WHERE r.path <> '' AND f.path = r.path LIMIT 1), r.post_process_error, r.created_at, r.finished_at`

func (s *Service) scanRecording(row pgx.Row) (Recording, error) {
	var r Recording
	err := row.Scan(&r.ID, &r.ChannelID, &r.ChannelName, &r.ChannelNumber, &r.ProgramID, &r.SeriesRuleID, &r.Title, &r.SubTitle,
		&r.Description, &r.Categories, &r.Season, &r.Episode, &r.Year, &r.EpisodeKey, &r.RawImage, &r.Start, &r.End, &r.PrePadding,
		&r.PostPadding, &r.Priority, &r.Status, &r.Error, &r.Path, &r.SizeBytes, &r.ItemID, &r.PostProcessError, &r.CreatedAt, &r.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	r.ImageURL = s.ImageURL(r.RawImage)
	return r, err
}

// Recordings lists recordings in the given states (default: all but
// cancelled and deleted): upcoming by start, then finished newest first.
func (s *Service) Recordings(ctx context.Context, statuses []string) ([]Recording, error) {
	if len(statuses) == 0 {
		statuses = []string{"scheduled", "conflict", "recording", "completed", "failed"}
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+recordingCols+` FROM livetv_recordings r WHERE r.status = ANY($1)
		ORDER BY (r.status IN ('scheduled', 'conflict', 'recording')) DESC,
			CASE WHEN r.status IN ('scheduled', 'conflict', 'recording') THEN r.start_at END ASC,
			r.start_at DESC`, statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recording{}
	for rows.Next() {
		r, err := s.scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Recording returns one recording.
func (s *Service) Recording(ctx context.Context, id uuid.UUID) (Recording, error) {
	return s.scanRecording(s.Pool.QueryRow(ctx, `SELECT `+recordingCols+` FROM livetv_recordings r WHERE r.id = $1`, id))
}

// RecordingInput schedules one programme.
type RecordingInput struct {
	ProgramID   uuid.UUID
	ChannelID   *uuid.UUID
	PrePadding  *int
	PostPadding *int
	Priority    *int
	CreatedBy   uuid.UUID
}

// CreateRecording schedules a programme (an existing active recording of the
// same airing on the same channel is returned instead of a duplicate).
func (s *Service) CreateRecording(ctx context.Context, in RecordingInput) (Recording, error) {
	p, ch, err := s.ProgramOn(ctx, uuid.Nil, in.ProgramID, in.ChannelID)
	if err != nil {
		return Recording{}, err
	}
	if !p.End.After(s.now()) {
		return Recording{}, invalid("this programme has already ended")
	}
	if p.RecordingID != nil && p.RecordingStatus != "failed" && p.RecordingStatus != "completed" {
		return s.Recording(ctx, *p.RecordingID)
	}
	st := s.LoadSettings(ctx)
	pre, post, prio := st.PrePadding, st.PostPadding, 0
	if in.PrePadding != nil {
		pre = *in.PrePadding
	}
	if in.PostPadding != nil {
		post = *in.PostPadding
	}
	if in.Priority != nil {
		prio = *in.Priority
	}
	id, err := s.insertRecording(ctx, s.Pool, p, ch, nil, pre, post, prio, in.CreatedBy)
	if err != nil {
		return Recording{}, err
	}
	if err := s.Schedule(ctx); err != nil {
		return Recording{}, err
	}
	return s.Recording(ctx, id)
}

func (s *Service) insertRecording(ctx context.Context, db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, p Program, ch Channel, rule *uuid.UUID, pre, post, prio int, by uuid.UUID) (uuid.UUID, error) {
	id, _ := uuid.NewV7()
	var createdBy *uuid.UUID
	if by != uuid.Nil {
		createdBy = &by
	}
	cats := p.Categories
	if cats == nil {
		cats = []string{}
	}
	var got uuid.UUID
	err := db.QueryRow(ctx, `INSERT INTO livetv_recordings (id, channel_id, channel_name, channel_number, program_id, series_rule_id,
			title, subtitle, description, categories, season, episode, episode_key, year, image_url, start_at, end_at,
			pre_padding, post_padding, priority, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT DO NOTHING RETURNING id`,
		id, ch.ID, ch.Name, ch.Number, p.ID, rule, p.Title, p.SubTitle, p.Description, cats, p.Season, p.Episode, p.EpisodeKey,
		p.Year, p.RawImage, p.Start, p.End, clampPad(pre, maxPrePadding), clampPad(post, maxPostPadding), prio, createdBy).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil // a rule already holds this airing
	}
	return got, err
}

func clampPad(v, limit int) int { return min(max(v, 0), limit) }

// RecordingUpdate changes padding or priority.
type RecordingUpdate struct {
	PrePadding, PostPadding, Priority *int
}

// UpdateRecording changes a scheduled (or running: only the end padding) recording.
func (s *Service) UpdateRecording(ctx context.Context, id uuid.UUID, u RecordingUpdate) (Recording, error) {
	r, err := s.Recording(ctx, id)
	if err != nil {
		return r, err
	}
	switch r.Status {
	case "scheduled", "conflict":
	case "recording":
		if u.PrePadding != nil || u.Priority != nil {
			return r, invalid("a running recording can only get a different end padding")
		}
	default:
		return r, ErrRecordingDone
	}
	if u.PrePadding != nil {
		r.PrePadding = clampPad(*u.PrePadding, maxPrePadding)
	}
	if u.PostPadding != nil {
		r.PostPadding = clampPad(*u.PostPadding, maxPostPadding)
	}
	if u.Priority != nil {
		r.Priority = *u.Priority
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE livetv_recordings SET pre_padding = $2, post_padding = $3, priority = $4 WHERE id = $1`,
		id, r.PrePadding, r.PostPadding, r.Priority); err != nil {
		return r, err
	}
	s.updateRunningEnd(id)
	if err := s.Schedule(ctx); err != nil {
		return r, err
	}
	return s.Recording(ctx, id)
}

// DeleteRecording cancels a scheduled recording, stops a running one (its
// file is kept) or hides a finished one from the list. Files are never deleted here.
func (s *Service) DeleteRecording(ctx context.Context, id uuid.UUID) error {
	r, err := s.Recording(ctx, id)
	if err != nil {
		return err
	}
	switch r.Status {
	case "scheduled", "conflict":
		_, err = s.Pool.Exec(ctx, `UPDATE livetv_recordings SET status = 'cancelled', finished_at = now() WHERE id = $1`, id)
	case "recording":
		s.stopRecorder(id, "stopped")
		return nil
	case "completed", "failed":
		// Hidden from the list; the file stays in the recordings library.
		_, err = s.Pool.Exec(ctx, `UPDATE livetv_recordings SET status = 'cancelled' WHERE id = $1`, id)
	default:
		return ErrNotFound
	}
	if err == nil {
		err = s.Schedule(ctx)
	}
	return err
}

// SeriesRule records every (or every new) airing of a title.
type SeriesRule struct {
	ID          uuid.UUID
	Title       string
	ChannelID   *uuid.UUID
	ChannelName string
	NewOnly     bool
	Priority    int
	PrePadding  *int
	PostPadding *int
	KeepLast    int
	Upcoming    int
	CreatedAt   time.Time
}

// SeriesRuleInput creates or changes a rule.
type SeriesRuleInput struct {
	Title       string
	ProgramID   *uuid.UUID
	ChannelID   *uuid.UUID
	NewOnly     *bool
	Priority    *int
	PrePadding  *int
	PostPadding *int
	KeepLast    *int
	CreatedBy   uuid.UUID
}

// SeriesRules lists the rules.
func (s *Service) SeriesRules(ctx context.Context) ([]SeriesRule, error) {
	rows, err := s.Pool.Query(ctx, `SELECT r.id, r.title, r.channel_id, COALESCE(c.name, ''), r.new_only, r.priority, r.pre_padding,
			r.post_padding, r.keep_last,
			(SELECT count(*) FROM livetv_recordings x WHERE x.series_rule_id = r.id AND x.status IN ('scheduled', 'conflict', 'recording')),
			r.created_at
		FROM livetv_series_rules r LEFT JOIN livetv_channels c ON c.id = r.channel_id ORDER BY lower(r.title), r.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SeriesRule{}
	for rows.Next() {
		var r SeriesRule
		if err := rows.Scan(&r.ID, &r.Title, &r.ChannelID, &r.ChannelName, &r.NewOnly, &r.Priority, &r.PrePadding, &r.PostPadding,
			&r.KeepLast, &r.Upcoming, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) seriesRule(ctx context.Context, id uuid.UUID) (SeriesRule, error) {
	rules, err := s.SeriesRules(ctx)
	if err != nil {
		return SeriesRule{}, err
	}
	for _, r := range rules {
		if r.ID == id {
			return r, nil
		}
	}
	return SeriesRule{}, ErrNotFound
}

func (s *Service) ruleValues(ctx context.Context, in SeriesRuleInput, cur *SeriesRule) (SeriesRule, error) {
	r := SeriesRule{NewOnly: true}
	if cur != nil {
		r = *cur
	}
	title := strings.TrimSpace(in.Title)
	if title == "" && in.ProgramID != nil {
		p, _, err := s.ProgramOn(ctx, uuid.Nil, *in.ProgramID, in.ChannelID)
		if err != nil {
			return r, err
		}
		title = p.Title
	}
	if title != "" {
		r.Title = title
	}
	if r.Title == "" {
		return r, invalid("give the title to record")
	}
	if in.ChannelID != nil {
		var ok bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM livetv_channels WHERE id = $1)`, *in.ChannelID).Scan(&ok); err != nil {
			return r, err
		}
		if !ok {
			return r, invalid("unknown channel")
		}
	}
	r.ChannelID = in.ChannelID
	if in.NewOnly != nil {
		r.NewOnly = *in.NewOnly
	}
	if in.Priority != nil {
		r.Priority = *in.Priority
	}
	r.PrePadding, r.PostPadding = in.PrePadding, in.PostPadding
	if in.KeepLast != nil {
		r.KeepLast = max(*in.KeepLast, 0)
	}
	return r, nil
}

// CreateSeriesRule adds a rule and schedules its airings at once.
func (s *Service) CreateSeriesRule(ctx context.Context, in SeriesRuleInput) (SeriesRule, error) {
	r, err := s.ruleValues(ctx, in, nil)
	if err != nil {
		return r, err
	}
	id, _ := uuid.NewV7()
	var by *uuid.UUID
	if in.CreatedBy != uuid.Nil {
		by = &in.CreatedBy
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO livetv_series_rules (id, title, channel_id, new_only, priority, pre_padding, post_padding,
		keep_last, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, r.Title, r.ChannelID, r.NewOnly, r.Priority, r.PrePadding, r.PostPadding, r.KeepLast, by); err != nil {
		return r, err
	}
	if err := s.Schedule(ctx); err != nil {
		return r, err
	}
	return s.seriesRule(ctx, id)
}

// UpdateSeriesRule changes a rule; its upcoming recordings are scheduled again.
func (s *Service) UpdateSeriesRule(ctx context.Context, id uuid.UUID, in SeriesRuleInput) (SeriesRule, error) {
	cur, err := s.seriesRule(ctx, id)
	if err != nil {
		return cur, err
	}
	r, err := s.ruleValues(ctx, in, &cur)
	if err != nil {
		return r, err
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE livetv_series_rules SET title = $2, channel_id = $3, new_only = $4, priority = $5,
		pre_padding = $6, post_padding = $7, keep_last = $8 WHERE id = $1`,
		id, r.Title, r.ChannelID, r.NewOnly, r.Priority, r.PrePadding, r.PostPadding, r.KeepLast); err != nil {
		return r, err
	}
	// Upcoming airings follow the new rule: drop the not-yet-started ones and expand again.
	if _, err := s.Pool.Exec(ctx, `DELETE FROM livetv_recordings WHERE series_rule_id = $1 AND status IN ('scheduled', 'conflict')`, id); err != nil {
		return r, err
	}
	if err := s.Schedule(ctx); err != nil {
		return r, err
	}
	return s.seriesRule(ctx, id)
}

// DeleteSeriesRule removes a rule and its upcoming recordings (finished ones stay).
func (s *Service) DeleteSeriesRule(ctx context.Context, id uuid.UUID) error {
	if _, err := s.Pool.Exec(ctx, `DELETE FROM livetv_recordings WHERE series_rule_id = $1 AND status IN ('scheduled', 'conflict')`, id); err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM livetv_series_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return s.Schedule(ctx)
}
