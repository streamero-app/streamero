package livetv

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Channel is a channel with everything the API shows.
type Channel struct {
	ID               uuid.UUID
	TunerID          uuid.UUID
	TunerName        string
	TunerKind        string
	TunerUserAgent   string
	Number           string
	Name             string
	Group            string
	Category         string // effective (override or source)
	CategoryOverride string
	LogoURL          string // signed
	StreamURL        string
	HD               bool
	Hidden           bool
	Favorite         bool
	Mapping          string
	GuideID          *uuid.UUID
	GuideChannel     string
	VideoCodec       string
	AudioCodec       string
	Height           *int
	Now, Next        *Program
}

// Program is a programme on a channel.
type Program struct {
	ID              uuid.UUID
	ChannelID       uuid.UUID
	ChannelName     string
	ChannelNumber   string
	Title           string
	SubTitle        string
	Description     string
	Start, End      time.Time
	Categories      []string
	Category        string
	Season          *int
	Episode         *int
	Year            *int
	EpisodeKey      string
	IsNew           bool
	IsPremiere      bool
	IsRepeat        bool
	IsLive          bool
	ImageURL        string // signed
	RawImage        string
	RecordingID     *uuid.UUID
	RecordingStatus string
	SeriesRuleID    *uuid.UUID
}

// ChannelFilter narrows channel lists and the guide.
type ChannelFilter struct {
	Category      string
	FavoritesOnly bool
	IncludeHidden bool
	IDs           []uuid.UUID
}

const channelCols = `c.id, c.tuner_id, t.name, t.kind, t.user_agent, c.number, c.name, c.group_title,
	COALESCE(NULLIF(c.category_override, ''), c.category), c.category_override, c.logo_url, c.stream_url, c.hd, c.hidden,
	(f.owner_id IS NOT NULL), c.mapping_mode, c.guide_id, c.guide_channel, c.video_codec, c.audio_codec, c.height`

func (s *Service) scanChannel(r pgx.Row) (Channel, error) {
	var c Channel
	var logo string
	err := r.Scan(&c.ID, &c.TunerID, &c.TunerName, &c.TunerKind, &c.TunerUserAgent, &c.Number, &c.Name, &c.Group, &c.Category,
		&c.CategoryOverride, &logo, &c.StreamURL, &c.HD, &c.Hidden, &c.Favorite, &c.Mapping, &c.GuideID, &c.GuideChannel,
		&c.VideoCodec, &c.AudioCodec, &c.Height)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	c.LogoURL = s.ImageURL(logo)
	return c, err
}

// channels lists channels (of enabled sources) for a viewer.
func (s *Service) channels(ctx context.Context, owner uuid.UUID, f ChannelFilter) ([]Channel, error) {
	where := []string{"t.enabled"}
	args := []any{owner}
	if !f.IncludeHidden {
		where = append(where, "NOT c.hidden")
	}
	if f.FavoritesOnly {
		where = append(where, "f.owner_id IS NOT NULL")
	}
	if len(f.IDs) > 0 {
		args = append(args, f.IDs)
		where = append(where, "c.id = ANY($2)")
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+channelCols+` FROM livetv_channels c JOIN livetv_tuners t ON t.id = c.tuner_id
		LEFT JOIN livetv_favorites f ON f.channel_id = c.id AND f.owner_id = $1
		WHERE `+strings.Join(where, " AND ")+` ORDER BY c.sort_number, c.name, t.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := s.scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChannelByID returns one channel (also hidden ones; not of disabled sources).
func (s *Service) ChannelByID(ctx context.Context, owner, id uuid.UUID) (Channel, error) {
	list, err := s.channels(ctx, owner, ChannelFilter{IDs: []uuid.UUID{id}, IncludeHidden: true})
	if err != nil {
		return Channel{}, err
	}
	if len(list) == 0 {
		return Channel{}, ErrNotFound
	}
	if err := s.attachNowNext(ctx, list); err != nil {
		return Channel{}, err
	}
	return list[0], nil
}

// Channels lists the channels with now/next. A category filter keeps
// channels of that category and channels airing a programme of it now.
func (s *Service) Channels(ctx context.Context, owner uuid.UUID, f ChannelFilter) ([]Channel, error) {
	list, err := s.channels(ctx, owner, ChannelFilter{FavoritesOnly: f.FavoritesOnly})
	if err != nil {
		return nil, err
	}
	if err := s.attachNowNext(ctx, list); err != nil {
		return nil, err
	}
	if f.Category == "" {
		return list, nil
	}
	out := []Channel{}
	for _, c := range list {
		if c.Category == f.Category || (c.Now != nil && c.Now.Category == f.Category) {
			out = append(out, c)
		}
	}
	return out, nil
}

// OnNow lists channels with a programme now, favourites first.
func (s *Service) OnNow(ctx context.Context, owner uuid.UUID, limit int) ([]Channel, error) {
	list, err := s.Channels(ctx, owner, ChannelFilter{})
	if err != nil {
		return nil, err
	}
	out := []Channel{}
	for _, c := range list {
		if c.Now != nil {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Favorite && !out[j].Favorite })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// programsFor loads the programmes of channels inside [from, to).
func (s *Service) programsFor(ctx context.Context, chans []Channel, from, to time.Time) (map[uuid.UUID][]Program, error) {
	ids := make([]uuid.UUID, 0, len(chans))
	byID := map[uuid.UUID]Channel{}
	for _, c := range chans {
		if c.GuideID != nil && c.GuideChannel != "" {
			ids = append(ids, c.ID)
			byID[c.ID] = c
		}
	}
	out := map[uuid.UUID][]Program{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT c.id, `+programCols+` FROM livetv_channels c
		JOIN livetv_programs p ON p.guide_id = c.guide_id AND p.xmltv_channel = c.guide_channel
		WHERE c.id = ANY($1) AND p.end_at > $2 AND p.start_at < $3 ORDER BY p.start_at`, ids, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid uuid.UUID
		p, err := s.scanProgram(rows, &cid)
		if err != nil {
			return nil, err
		}
		c := byID[cid]
		p.ChannelID, p.ChannelName, p.ChannelNumber = c.ID, c.Name, c.Number
		out[cid] = append(out[cid], p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.annotate(ctx, out)
}

const programCols = `p.id, p.title, p.subtitle, p.description, p.start_at, p.end_at, p.categories, p.category, p.season, p.episode,
	p.year, p.episode_key, p.is_new, p.is_premiere, p.is_repeat, p.is_live, p.image_url`

func (s *Service) scanProgram(r pgx.Row, lead ...any) (Program, error) {
	var p Program
	dst := append(lead, &p.ID, &p.Title, &p.SubTitle, &p.Description, &p.Start, &p.End, &p.Categories, &p.Category, &p.Season,
		&p.Episode, &p.Year, &p.EpisodeKey, &p.IsNew, &p.IsPremiere, &p.IsRepeat, &p.IsLive, &p.RawImage)
	err := r.Scan(dst...)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	p.ImageURL = s.ImageURL(p.RawImage)
	return p, err
}

// annotate adds scheduled recordings and matching series rules.
func (s *Service) annotate(ctx context.Context, progs map[uuid.UUID][]Program) error {
	var pids []uuid.UUID
	for _, list := range progs {
		for _, p := range list {
			pids = append(pids, p.ID)
		}
	}
	if len(pids) == 0 {
		return nil
	}
	type key struct{ prog, ch uuid.UUID }
	recs := map[key]struct {
		id     uuid.UUID
		status string
	}{}
	rows, err := s.Pool.Query(ctx, `SELECT id, program_id, channel_id, status FROM livetv_recordings
		WHERE program_id = ANY($1) AND channel_id IS NOT NULL AND status NOT IN ('cancelled', 'deleted')`, pids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, pid, cid uuid.UUID
		var st string
		if err := rows.Scan(&id, &pid, &cid, &st); err != nil {
			rows.Close()
			return err
		}
		recs[key{pid, cid}] = struct {
			id     uuid.UUID
			status string
		}{id, st}
	}
	rows.Close()
	rules, err := s.SeriesRules(ctx)
	if err != nil {
		return err
	}
	for cid, list := range progs {
		for i := range list {
			p := &list[i]
			if r, ok := recs[key{p.ID, cid}]; ok {
				id := r.id
				p.RecordingID, p.RecordingStatus = &id, r.status
			}
			for _, rule := range rules {
				if sameTitle(rule.Title, p.Title) && (rule.ChannelID == nil || *rule.ChannelID == cid) {
					rid := rule.ID
					p.SeriesRuleID = &rid
					break
				}
			}
		}
	}
	return nil
}

func (s *Service) attachNowNext(ctx context.Context, chans []Channel) error {
	now := s.now()
	progs, err := s.programsFor(ctx, chans, now, now.Add(12*time.Hour))
	if err != nil {
		return err
	}
	for i := range chans {
		for _, p := range progs[chans[i].ID] {
			p := p
			switch {
			case !p.Start.After(now) && p.End.After(now) && chans[i].Now == nil:
				chans[i].Now = &p
			case p.Start.After(now) && chans[i].Next == nil:
				chans[i].Next = &p
			}
		}
	}
	return nil
}

// GuideRow is a channel with its programmes in the window.
type GuideRow struct {
	Channel  Channel
	Programs []Program
}

// Guide returns the grid for [from, to).
func (s *Service) Guide(ctx context.Context, owner uuid.UUID, from, to time.Time, f ChannelFilter) ([]GuideRow, error) {
	chans, err := s.channels(ctx, owner, ChannelFilter{FavoritesOnly: f.FavoritesOnly})
	if err != nil {
		return nil, err
	}
	progs, err := s.programsFor(ctx, chans, from, to)
	if err != nil {
		return nil, err
	}
	out := []GuideRow{}
	for _, c := range chans {
		ps := progs[c.ID]
		if f.Category != "" && c.Category != f.Category {
			hit := false
			for _, p := range ps {
				if p.Category == f.Category {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		if ps == nil {
			ps = []Program{}
		}
		out = append(out, GuideRow{Channel: c, Programs: ps})
	}
	return out, nil
}

// ProgramOn returns a programme as shown on a channel (the first channel
// showing it when channelID is nil).
func (s *Service) ProgramOn(ctx context.Context, owner, programID uuid.UUID, channelID *uuid.UUID) (Program, Channel, error) {
	var guideID uuid.UUID
	var xch string
	var start, end time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT guide_id, xmltv_channel, start_at, end_at FROM livetv_programs WHERE id = $1`, programID).
		Scan(&guideID, &xch, &start, &end); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Program{}, Channel{}, ErrNotFound
		}
		return Program{}, Channel{}, err
	}
	chans, err := s.channels(ctx, owner, ChannelFilter{IncludeHidden: true})
	if err != nil {
		return Program{}, Channel{}, err
	}
	var ch *Channel
	for i, c := range chans {
		if c.GuideID == nil || *c.GuideID != guideID || c.GuideChannel != xch {
			continue
		}
		if channelID == nil || *channelID == c.ID {
			ch = &chans[i]
			break
		}
	}
	if ch == nil {
		return Program{}, Channel{}, ErrNotFound
	}
	progs, err := s.programsFor(ctx, []Channel{*ch}, start, end)
	if err != nil {
		return Program{}, Channel{}, err
	}
	for _, p := range progs[ch.ID] {
		if p.ID == programID {
			return p, *ch, nil
		}
	}
	return Program{}, Channel{}, ErrNotFound
}

// SetFavorite marks or unmarks a channel for a viewer.
func (s *Service) SetFavorite(ctx context.Context, owner, channelID uuid.UUID, on bool) error {
	if _, err := s.ChannelByID(ctx, owner, channelID); err != nil {
		return err
	}
	var err error
	if on {
		_, err = s.Pool.Exec(ctx, `INSERT INTO livetv_favorites (owner_id, channel_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, owner, channelID)
	} else {
		_, err = s.Pool.Exec(ctx, `DELETE FROM livetv_favorites WHERE owner_id = $1 AND channel_id = $2`, owner, channelID)
	}
	return err
}

// GuideChannel is a channel a guide describes.
type GuideChannel struct {
	GuideID uuid.UUID
	ID      string
	Name    string
}

// Mapping returns every channel (also of disabled sources and hidden ones)
// with the guide channels to choose from.
func (s *Service) Mapping(ctx context.Context) ([]Channel, []GuideChannel, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+channelCols+` FROM livetv_channels c JOIN livetv_tuners t ON t.id = c.tuner_id
		LEFT JOIN livetv_favorites f ON false ORDER BY t.name, c.sort_number, c.name`)
	if err != nil {
		return nil, nil, err
	}
	chans := []Channel{}
	for rows.Next() {
		c, err := s.scanChannel(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		chans = append(chans, c)
	}
	rows.Close()
	grow, err := s.Pool.Query(ctx, `SELECT gc.guide_id, gc.xmltv_id, COALESCE(gc.names[1], gc.xmltv_id) FROM livetv_guide_channels gc
		JOIN livetv_guides g ON g.id = gc.guide_id ORDER BY g.name, 3`)
	if err != nil {
		return nil, nil, err
	}
	gcs := []GuideChannel{}
	for grow.Next() {
		var g GuideChannel
		if err := grow.Scan(&g.GuideID, &g.ID, &g.Name); err != nil {
			grow.Close()
			return nil, nil, err
		}
		gcs = append(gcs, g)
	}
	grow.Close()
	return chans, gcs, grow.Err()
}

// ChannelUpdate changes the mapping, category or visibility of a channel.
type ChannelUpdate struct {
	Mapping      *string
	GuideID      *uuid.UUID
	GuideChannel *string
	Hidden       *bool
	Category     *string // "auto" or a category
}

// UpdateChannel applies a ChannelUpdate.
func (s *Service) UpdateChannel(ctx context.Context, id uuid.UUID, u ChannelUpdate) (Channel, error) {
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM livetv_channels WHERE id = $1)`, id).Scan(&exists); err != nil {
		return Channel{}, err
	}
	if !exists {
		return Channel{}, ErrNotFound
	}
	if u.Mapping != nil {
		switch *u.Mapping {
		case "auto":
			if _, err := s.Pool.Exec(ctx, `UPDATE livetv_channels SET mapping_mode = 'auto' WHERE id = $1`, id); err != nil {
				return Channel{}, err
			}
		case "none":
			if _, err := s.Pool.Exec(ctx, `UPDATE livetv_channels SET mapping_mode = 'none', guide_id = NULL, guide_channel = '' WHERE id = $1`, id); err != nil {
				return Channel{}, err
			}
		case "manual":
			if u.GuideID == nil || u.GuideChannel == nil || *u.GuideChannel == "" {
				return Channel{}, invalid("choose the guide channel")
			}
			var ok bool
			if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM livetv_guide_channels WHERE guide_id = $1 AND xmltv_id = $2)`,
				*u.GuideID, *u.GuideChannel).Scan(&ok); err != nil {
				return Channel{}, err
			}
			if !ok {
				return Channel{}, invalid("unknown guide channel")
			}
			if _, err := s.Pool.Exec(ctx, `UPDATE livetv_channels SET mapping_mode = 'manual', guide_id = $2, guide_channel = $3 WHERE id = $1`,
				id, *u.GuideID, *u.GuideChannel); err != nil {
				return Channel{}, err
			}
		default:
			return Channel{}, invalid("unknown mapping mode")
		}
	}
	if u.Hidden != nil {
		if _, err := s.Pool.Exec(ctx, `UPDATE livetv_channels SET hidden = $2 WHERE id = $1`, id, *u.Hidden); err != nil {
			return Channel{}, err
		}
	}
	if u.Category != nil {
		c := *u.Category
		if c == "auto" {
			c = ""
		} else if !validCategory(c) {
			return Channel{}, invalid("unknown category")
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE livetv_channels SET category_override = $2 WHERE id = $1`, id, c); err != nil {
			return Channel{}, err
		}
	}
	if err := s.AutoMap(ctx); err != nil {
		return Channel{}, err
	}
	chans, _, err := s.Mapping(ctx)
	if err != nil {
		return Channel{}, err
	}
	for _, c := range chans {
		if c.ID == id {
			return c, nil
		}
	}
	return Channel{}, ErrNotFound
}
