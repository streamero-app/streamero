package livetv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Tuner is a tuner source.
type Tuner struct {
	ID            uuid.UUID
	Kind          string // m3u | hdhomerun
	Name          string
	URL           string
	FilePath      string
	TunerCount    int
	UserAgent     string
	Enabled       bool
	DeviceID      string
	Model         string
	LastRefreshAt *time.Time
	Error         string
	ChannelCount  int
}

// TunerInput is what an administrator enters. TunerCount nil = default
// (unlimited for M3U, the device's count for HDHomeRun).
type TunerInput struct {
	Kind       string
	Name       string
	URL        string
	FilePath   string
	TunerCount *int
	UserAgent  string
	Enabled    *bool
}

// TestResult is the answer of a "Test" button.
type TestResult struct {
	OK           bool
	Message      string
	ChannelCount *int
	ProgramCount *int
	TunerCount   *int
	Model        string
}

const tunerCols = `t.id, t.kind, t.name, t.url, t.file_path, t.tuner_count, t.user_agent, t.enabled, t.device_id, t.model,
	t.last_refresh_at, t.error, (SELECT count(*) FROM livetv_channels c WHERE c.tuner_id = t.id)`

func scanTuner(r pgx.Row) (Tuner, error) {
	var t Tuner
	err := r.Scan(&t.ID, &t.Kind, &t.Name, &t.URL, &t.FilePath, &t.TunerCount, &t.UserAgent, &t.Enabled, &t.DeviceID, &t.Model,
		&t.LastRefreshAt, &t.Error, &t.ChannelCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// Tuners lists the tuner sources.
func (s *Service) Tuners(ctx context.Context) ([]Tuner, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+tunerCols+` FROM livetv_tuners t ORDER BY t.name, t.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tuner{}
	for rows.Next() {
		t, err := scanTuner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Tuner returns one tuner source.
func (s *Service) Tuner(ctx context.Context, id uuid.UUID) (Tuner, error) {
	return scanTuner(s.Pool.QueryRow(ctx, `SELECT `+tunerCols+` FROM livetv_tuners t WHERE t.id = $1`, id))
}

func (in *TunerInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)
	in.FilePath = strings.TrimSpace(in.FilePath)
	if in.Name == "" {
		return invalid("give the tuner a name")
	}
	switch in.Kind {
	case "m3u":
		if (in.URL == "") == (in.FilePath == "") {
			return invalid("give either a playlist URL or a playlist file")
		}
		if in.URL != "" {
			if _, err := httpURL(in.URL); err != nil {
				return invalid("the playlist URL must start with http:// or https://")
			}
		}
	case "hdhomerun":
		if in.URL == "" {
			return invalid("give the HDHomeRun address")
		}
		if _, err := hdhrBase(in.URL); err != nil {
			return invalid("the HDHomeRun address is not valid")
		}
		in.FilePath = ""
	default:
		return invalid("unknown tuner kind")
	}
	if in.TunerCount != nil && (*in.TunerCount < 0 || *in.TunerCount > 64) {
		return invalid("the tuner count must be 0 to 64")
	}
	return nil
}

// loadedChannel is a channel as a source lists it.
type loadedChannel struct {
	remoteID, number, name, tvgID, logo, group, url string
	hd                                             bool
	videoCodec, audioCodec                         string
}

// loadSource reads the channels of a tuner configuration (and, for HDHomeRun, the device).
func (s *Service) loadSource(ctx context.Context, in TunerInput) ([]loadedChannel, *HDHRDevice, error) {
	switch in.Kind {
	case "m3u":
		f, err := open(ctx, s.http(), s.FileRoots, Source{URL: in.URL, FilePath: in.FilePath, UserAgent: in.UserAgent})
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = f.Body.Close() }()
		list, _, err := ParseM3U(f.Body)
		if err != nil {
			return nil, nil, err
		}
		out := make([]loadedChannel, 0, len(list))
		seen := map[string]int{}
		for i, c := range list {
			key := c.TvgID
			if key == "" {
				sum := sha256.Sum256([]byte(c.URL))
				key = "u:" + hex.EncodeToString(sum[:8])
			}
			// The same tvg-id twice (e.g. an SD and an HD stream) stays two channels.
			seen[key]++
			if n := seen[key]; n > 1 {
				key += "#" + strconv.Itoa(n)
			}
			num := c.Number
			if num == "" {
				num = strconv.Itoa(i + 1)
			}
			out = append(out, loadedChannel{remoteID: key, number: num, name: c.Name, tvgID: c.TvgID, logo: c.Logo,
				group: c.Group, url: c.URL, hd: strings.Contains(strings.ToUpper(c.Name), "HD")})
		}
		return out, nil, nil
	case "hdhomerun":
		d, err := HDHRDiscover(ctx, s.http(), in.URL)
		if err != nil {
			return nil, nil, err
		}
		lineup, err := HDHRLineup(ctx, s.http(), in.URL, d)
		if err != nil {
			return nil, &d, err
		}
		base, _ := hdhrBase(in.URL)
		bu, _ := url.Parse(base)
		out := make([]loadedChannel, 0, len(lineup))
		for _, c := range lineup {
			stream := c.URL
			// Streams are always fetched from the configured device address (the
			// lineup could name any host); the device's port and path are kept.
			if u, err := url.Parse(c.URL); err == nil && bu != nil {
				port := u.Port()
				u.Host = bu.Hostname()
				if port != "" {
					u.Host += ":" + port
				}
				u.Scheme = bu.Scheme
				stream = u.String()
			}
			out = append(out, loadedChannel{remoteID: "n:" + c.GuideNumber, number: c.GuideNumber, name: c.GuideName,
				url: stream, hd: c.HD == 1, videoCodec: hdhrCodec(c.VideoCodec), audioCodec: hdhrCodec(c.AudioCodec)})
		}
		return out, &d, nil
	}
	return nil, nil, invalid("unknown tuner kind")
}

// TestTuner checks a configuration without saving it.
func (s *Service) TestTuner(ctx context.Context, in TunerInput) TestResult {
	if err := in.validate(); err != nil {
		return TestResult{Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	chans, dev, err := s.loadSource(ctx, in)
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	n := len(chans)
	res := TestResult{OK: true, ChannelCount: &n, Message: fmt.Sprintf("%d channels", n)}
	if dev != nil {
		tc := dev.TunerCount
		res.TunerCount = &tc
		res.Model = dev.ModelNumber
		res.Message = fmt.Sprintf("%s (%s): %d tuners, %d channels", dev.FriendlyName, dev.ModelNumber, tc, n)
	}
	return res
}

// CreateTuner adds a tuner source and loads its channels. A source that
// cannot be read is still saved, with the error shown.
func (s *Service) CreateTuner(ctx context.Context, in TunerInput) (Tuner, error) {
	if err := in.validate(); err != nil {
		return Tuner{}, err
	}
	id, _ := uuid.NewV7()
	count := 0
	if in.TunerCount != nil {
		count = *in.TunerCount
	}
	enabled := in.Enabled == nil || *in.Enabled
	if _, err := s.Pool.Exec(ctx, `INSERT INTO livetv_tuners (id, kind, name, url, file_path, tuner_count, user_agent, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, in.Kind, in.Name, in.URL, in.FilePath, count, strings.TrimSpace(in.UserAgent), enabled); err != nil {
		return Tuner{}, err
	}
	_ = s.refreshTuner(ctx, id, in.TunerCount == nil)
	return s.Tuner(ctx, id)
}

// UpdateTuner changes a tuner source and reloads its channels.
func (s *Service) UpdateTuner(ctx context.Context, id uuid.UUID, in TunerInput) (Tuner, error) {
	if err := in.validate(); err != nil {
		return Tuner{}, err
	}
	cur, err := s.Tuner(ctx, id)
	if err != nil {
		return Tuner{}, err
	}
	count := cur.TunerCount
	if in.TunerCount != nil {
		count = *in.TunerCount
	}
	enabled := in.Enabled == nil || *in.Enabled
	if _, err := s.Pool.Exec(ctx, `UPDATE livetv_tuners SET kind = $2, name = $3, url = $4, file_path = $5, tuner_count = $6,
		user_agent = $7, enabled = $8 WHERE id = $1`, id, in.Kind, in.Name, in.URL, in.FilePath, count, strings.TrimSpace(in.UserAgent), enabled); err != nil {
		return Tuner{}, err
	}
	_ = s.refreshTuner(ctx, id, false)
	s.Kick()
	return s.Tuner(ctx, id)
}

// DeleteTuner removes a tuner source, its channels and their live sessions.
// Recordings keep their files and history.
func (s *Service) DeleteTuner(ctx context.Context, id uuid.UUID) error {
	s.stopTunerSessions(id)
	tag, err := s.Pool.Exec(ctx, `DELETE FROM livetv_tuners WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.Kick()
	return nil
}

// RefreshTuner reloads the channel list of a tuner source.
func (s *Service) RefreshTuner(ctx context.Context, id uuid.UUID) (Tuner, error) {
	if _, err := s.Tuner(ctx, id); err != nil {
		return Tuner{}, err
	}
	err := s.refreshTuner(ctx, id, false)
	t, terr := s.Tuner(ctx, id)
	if terr != nil {
		return t, terr
	}
	_ = err // stored on the tuner
	return t, nil
}

// RefreshAllTuners reloads every enabled source (periodic).
func (s *Service) RefreshAllTuners(ctx context.Context) {
	ts, err := s.Tuners(ctx)
	if err != nil {
		return
	}
	for _, t := range ts {
		if t.Enabled {
			_ = s.refreshTuner(ctx, t.ID, false)
		}
	}
}

func (s *Service) refreshTuner(ctx context.Context, id uuid.UUID, takeDeviceCount bool) error {
	t, err := s.Tuner(ctx, id)
	if err != nil {
		return err
	}
	lctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	chans, dev, err := s.loadSource(lctx, TunerInput{Kind: t.Kind, URL: t.URL, FilePath: t.FilePath, UserAgent: t.UserAgent})
	if dev != nil {
		count := t.TunerCount
		if takeDeviceCount || count == 0 || count > dev.TunerCount {
			count = dev.TunerCount
		}
		_, _ = s.Pool.Exec(ctx, `UPDATE livetv_tuners SET device_id = $2, model = $3, tuner_count = $4 WHERE id = $1`,
			id, dev.DeviceID, dev.ModelNumber, count)
	}
	if err != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE livetv_tuners SET error = $2, last_refresh_at = now() WHERE id = $1`, id, err.Error())
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	keep := make([]string, 0, len(chans))
	for i, c := range chans {
		cid, _ := uuid.NewV7()
		sortNum := channelSort(c.number, i)
		// Codecs reported by the device replace what was probed; for M3U a changed
		// stream URL forgets the probed codecs.
		if _, err := tx.Exec(ctx, `INSERT INTO livetv_channels (id, tuner_id, remote_id, number, sort_number, name, tvg_id, logo_url,
				group_title, category, stream_url, hd, video_codec, audio_codec)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (tuner_id, remote_id) DO UPDATE SET number = EXCLUDED.number, sort_number = EXCLUDED.sort_number,
				name = EXCLUDED.name, tvg_id = EXCLUDED.tvg_id, logo_url = EXCLUDED.logo_url, group_title = EXCLUDED.group_title,
				category = EXCLUDED.category, hd = EXCLUDED.hd,
				video_codec = CASE WHEN EXCLUDED.video_codec <> '' THEN EXCLUDED.video_codec
					WHEN livetv_channels.stream_url <> EXCLUDED.stream_url THEN '' ELSE livetv_channels.video_codec END,
				audio_codec = CASE WHEN EXCLUDED.audio_codec <> '' THEN EXCLUDED.audio_codec
					WHEN livetv_channels.stream_url <> EXCLUDED.stream_url THEN '' ELSE livetv_channels.audio_codec END,
				probed_at = CASE WHEN livetv_channels.stream_url <> EXCLUDED.stream_url THEN NULL ELSE livetv_channels.probed_at END,
				stream_url = EXCLUDED.stream_url`,
			cid, id, c.remoteID, c.number, sortNum, c.name, c.tvgID, c.logo, c.group, NormalizeCategory(c.group), c.url, c.hd,
			c.videoCodec, c.audioCodec); err != nil {
			return err
		}
		keep = append(keep, c.remoteID)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM livetv_channels WHERE tuner_id = $1 AND NOT (remote_id = ANY($2))`, id, keep); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE livetv_tuners SET error = '', last_refresh_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return s.AutoMap(ctx)
}

// channelSort turns "5.1" or "101" into a sortable number (position as fallback).
func channelSort(number string, pos int) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(number, "-", ".")), 64); err == nil {
		return f
	}
	return 100000 + float64(pos)
}

// DiscoveredTuner is an HDHomeRun found on the network.
type DiscoveredTuner struct {
	HDHRDevice
	Added bool
}

// Discover looks for HDHomeRun devices on the local networks.
func (s *Service) Discover(ctx context.Context) ([]DiscoveredTuner, error) {
	devs := DiscoverHDHR(ctx, s.http(), 2*time.Second)
	ts, err := s.Tuners(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DiscoveredTuner, 0, len(devs))
	for _, d := range devs {
		added := false
		for _, t := range ts {
			if t.Kind == "hdhomerun" && t.DeviceID != "" && strings.EqualFold(t.DeviceID, d.DeviceID) {
				added = true
			}
		}
		out = append(out, DiscoveredTuner{HDHRDevice: d, Added: added})
	}
	return out, nil
}
