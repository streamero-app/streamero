// Package downloads implements offline downloads (D-040): per-profile download
// jobs, the shared cache of transcoded MP4 files produced by the worker, signed
// file serving with byte ranges, offline progress sync and cache cleanup.
package downloads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// Errors for the API layer.
var (
	ErrNoPermission    = errors.New("your account may not download media")
	ErrNoTranscode     = errors.New("your account may not use transcoded downloads")
	ErrParental        = errors.New("this title is above the profile's parental limit")
	ErrNotDownloadable = errors.New("this item has nothing to download")
	ErrBadQuality      = errors.New("unknown download quality")
)

// Qualities.
const (
	Original = "original"
	Source   = "source"
	Q1080    = "1080p"
	Q720     = "720p"
)

// Preset is the target of a transcoded quality.
type Preset struct {
	Height  int // 0 = keep
	Bitrate int // video bits per second, 0 = constant quality only
}

// Presets of the transcoded qualities.
var Presets = map[string]Preset{Source: {}, Q1080: {Height: 1080, Bitrate: 5_000_000}, Q720: {Height: 720, Bitrate: 2_000_000}}

// URLTTL is how long download links stay valid; clients ask for fresh ones with GET /downloads/{id}.
const URLTTL = 24 * time.Hour

// KeySettings is the settings key of the download limits.
const KeySettings = "downloads"

// Settings are the administrator's limits.
type Settings struct {
	MaxTranscodes int `json:"maxTranscodes"`
	CacheLimitGB  int `json:"cacheLimitGb"`
	KeepDays      int `json:"keepDays"`
}

// DefaultSettings allows one transcode at a time, 50 GB of cache, a week.
func DefaultSettings() Settings { return Settings{MaxTranscodes: 1, CacheLimitGB: 50, KeepDays: 7} }

// Normalize fills missing values with the defaults.
func (s Settings) Normalize() Settings {
	d := DefaultSettings()
	if s.MaxTranscodes <= 0 {
		s.MaxTranscodes = d.MaxTranscodes
	}
	if s.CacheLimitGB <= 0 {
		s.CacheLimitGB = d.CacheLimitGB
	}
	if s.KeepDays <= 0 {
		s.KeepDays = d.KeepDays
	}
	return s
}

// LoadSettings reads the limits (defaults when unset or unreadable).
func LoadSettings(ctx context.Context, st *settings.Store) Settings {
	s := DefaultSettings()
	if st != nil {
		_, _ = st.Get(ctx, KeySettings, &s)
	}
	return s.Normalize()
}

// TranscodePath is the cache file of a transcode (built from a UUID only).
func TranscodePath(dir string, id uuid.UUID) string { return filepath.Join(dir, id.String()+".mp4") }

// Service is the API side of downloads.
type Service struct {
	Pool     *pgxpool.Pool
	Library  *library.Service
	Roots    library.Roots
	Signer   stream.Signer
	Dir      string // download cache
	Settings *settings.Store
	// Enqueue queues a transcode for the worker.
	Enqueue func(ctx context.Context, transcodeID uuid.UUID) error
	Now     func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Owner is the profile or user whose access and progress apply.
func Owner(p auth.Principal) uuid.UUID { return stream.Owner(p) }

// Allowed reports why p may not keep itemID offline (nil = allowed): library
// access, the download permission, the profile's parental limit, a gone item.
func (s *Service) Allowed(ctx context.Context, p auth.Principal, itemID uuid.UUID) error {
	if !p.User.IsAdmin && !p.User.Permissions.AllLibraries {
		return library.ErrNotFound
	}
	if !p.User.IsAdmin && !p.User.Permissions.Download {
		return ErrNoPermission
	}
	rows, err := s.Pool.Query(ctx, `WITH RECURSIVE anc AS (
			SELECT id, parent_id, official_rating, 0 AS depth FROM items WHERE id = $1
			UNION ALL
			SELECT i.id, i.parent_id, i.official_rating, a.depth + 1 FROM items i JOIN anc a ON i.id = a.parent_id WHERE a.depth < 5)
		SELECT COALESCE(official_rating, '') FROM anc`, itemID)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	var ratings []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return err
		}
		found = true
		ratings = append(ratings, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !found {
		return library.ErrNotFound
	}
	if p.Profile != nil && p.Profile.MaxRating != nil {
		for _, r := range ratings {
			if a, ok := library.RatingAge(r); ok && a > *p.Profile.MaxRating {
				return ErrParental
			}
		}
	}
	return nil
}

// Request asks for downloads.
type Request struct {
	ItemID       uuid.UUID
	Quality      string
	FileID       *uuid.UUID
	AudioIndex   *int
	NextEpisodes int
	Skip         []uuid.UUID
}

// Job is one download job as the API shows it.
type Job struct {
	ID, ItemID, FileID uuid.UUID
	Quality, State     string
	Progress           float64
	Size               int64
	Error              string
	URL                string
	URLExpires         time.Time
	FileName           string
	Container          string
	DurationMS         *int64
	AudioIndex         int
	Subtitles          []Subtitle
	CreatedAt          time.Time
}

// Subtitle is a text track offered as a sidecar.
type Subtitle struct {
	Index    int
	Language string
	Forced   bool
	SRT, VTT string
}

// targets lists the movies and episodes a request covers, in viewing order.
func (s *Service) targets(ctx context.Context, owner uuid.UUID, r Request) ([]uuid.UUID, error) {
	var kind string
	if err := s.Pool.QueryRow(ctx, `SELECT kind FROM items WHERE id = $1`, r.ItemID).Scan(&kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, library.ErrNotFound
		}
		return nil, err
	}
	switch kind {
	case "movie", "episode":
		return []uuid.UUID{r.ItemID}, nil
	case "series", "season":
	default:
		return nil, ErrNotDownloadable
	}
	// Episodes of the season, or of every season of the series, with files, in order.
	rows, err := s.Pool.Query(ctx, `SELECT e.id, COALESCE(u.played, false) FROM items e
			LEFT JOIN items se ON se.id = e.parent_id
			LEFT JOIN user_item_data u ON u.item_id = e.id AND u.owner_id = $2
		WHERE e.kind = 'episode' AND (e.parent_id = $1 OR se.parent_id = $1)
			AND EXISTS (SELECT 1 FROM media_files f WHERE f.item_id = e.id)
		ORDER BY e.season_number NULLS LAST, e.episode_number NULLS LAST, e.sort_title LIMIT 2000`, r.ItemID, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var eps []Episode
	for rows.Next() {
		var e Episode
		if err := rows.Scan(&e.ID, &e.Played); err != nil {
			return nil, err
		}
		eps = append(eps, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pickEpisodes(eps, r.NextEpisodes, r.Skip), nil
}

// Episode is an episode in viewing order.
type Episode struct {
	ID     uuid.UUID
	Played bool
}

// pickEpisodes chooses what a season or series download covers: every episode,
// or with next > 0 the next unwatched ones after the last watched episode.
// Episodes the device already has (skip) count towards next but are not returned.
func pickEpisodes(eps []Episode, next int, skip []uuid.UUID) []uuid.UUID {
	skipped := map[uuid.UUID]bool{}
	for _, id := range skip {
		skipped[id] = true
	}
	start := 0
	if next > 0 {
		for i, e := range eps {
			if e.Played {
				start = i + 1
			}
		}
	}
	var out []uuid.UUID
	count := 0
	for _, e := range eps[start:] {
		if next > 0 && (count >= next) {
			break
		}
		if next > 0 && e.Played {
			continue
		}
		count++
		if !skipped[e.ID] {
			out = append(out, e.ID)
		}
	}
	return out
}

// Create records download jobs (idempotent) and queues the transcodes they need.
func (s *Service) Create(ctx context.Context, p auth.Principal, r Request) ([]Job, error) {
	_, transcoded := Presets[r.Quality]
	if !transcoded && r.Quality != Original {
		return nil, ErrBadQuality
	}
	if err := s.Allowed(ctx, p, r.ItemID); err != nil {
		return nil, err
	}
	if transcoded && !p.User.IsAdmin && !p.User.Permissions.VideoTranscoding {
		return nil, ErrNoTranscode
	}
	owner := Owner(p)
	ids, err := s.targets(ctx, owner, r)
	if err != nil {
		return nil, err
	}
	prefs, _ := s.Library.GetPrefs(ctx, owner)
	out := []Job{}
	for _, id := range ids {
		if id != r.ItemID {
			// An episode can carry its own rating.
			if err := s.Allowed(ctx, p, id); err != nil {
				continue
			}
		}
		f, err := s.pickFile(ctx, id, r.FileID)
		if err != nil {
			if len(ids) == 1 {
				return nil, err
			}
			continue
		}
		audio := -1
		var transcodeID *uuid.UUID
		if transcoded {
			audio = library.PickAudio(f.Audio, prefs.AudioLanguage)
			if r.AudioIndex != nil && *r.AudioIndex >= 0 && *r.AudioIndex < len(f.Audio) {
				audio = *r.AudioIndex
			}
			if len(f.Audio) == 0 {
				audio = -1
			}
			tid, err := s.ensureTranscode(ctx, f, r.Quality, audio)
			if err != nil {
				return nil, err
			}
			transcodeID = &tid
		}
		var jobID uuid.UUID
		if err := s.Pool.QueryRow(ctx, `INSERT INTO downloads (user_id, owner_id, item_id, file_id, quality, audio_index, transcode_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (owner_id, file_id, quality, audio_index) DO UPDATE SET transcode_id = EXCLUDED.transcode_id
			RETURNING id`, p.User.ID, owner, id, f.ID, r.Quality, audio, transcodeID).Scan(&jobID); err != nil {
			return nil, err
		}
		j, err := s.Get(ctx, p, jobID)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	if len(out) == 0 && len(ids) > 0 {
		return nil, ErrParental
	}
	return out, nil
}

// pickFile returns the chosen or best video file of an item.
func (s *Service) pickFile(ctx context.Context, itemID uuid.UUID, fileID *uuid.UUID) (library.MediaFile, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, path, size, container, duration_ms, height, COALESCE(video_codec, ''),
			COALESCE(hdr, ''), COALESCE(video_bit_depth, 0), audio, subtitles
		FROM media_files WHERE item_id = $1 ORDER BY height DESC NULLS LAST, path`, itemID)
	if err != nil {
		return library.MediaFile{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var f library.MediaFile
		var audio, subs []byte
		if err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.Container, &f.DurationMS, &f.Height, &f.VideoCodec, &f.HDR,
			&f.VideoBitDepth, &audio, &subs); err != nil {
			return f, err
		}
		_ = json.Unmarshal(audio, &f.Audio)
		_ = json.Unmarshal(subs, &f.Subtitles)
		if fileID == nil || *fileID == f.ID {
			return f, nil
		}
	}
	if err := rows.Err(); err != nil {
		return library.MediaFile{}, err
	}
	if fileID != nil {
		return library.MediaFile{}, library.ErrNotFound
	}
	return library.MediaFile{}, ErrNotDownloadable
}

// ensureTranscode returns the transcode of a file in a quality, creating and
// queueing it when missing, failed or made from a since replaced file.
func (s *Service) ensureTranscode(ctx context.Context, f library.MediaFile, quality string, audio int) (uuid.UUID, error) {
	var id uuid.UUID
	var state string
	var queued bool
	err := s.Pool.QueryRow(ctx, `INSERT INTO download_transcodes (file_id, quality, audio_index, source_size)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (file_id, quality, audio_index) DO UPDATE SET
			state = CASE WHEN download_transcodes.state = 'failed' OR download_transcodes.source_size <> EXCLUDED.source_size
				THEN 'queued' ELSE download_transcodes.state END,
			error = CASE WHEN download_transcodes.state = 'failed' OR download_transcodes.source_size <> EXCLUDED.source_size
				THEN '' ELSE download_transcodes.error END,
			progress = CASE WHEN download_transcodes.source_size <> EXCLUDED.source_size THEN 0 ELSE download_transcodes.progress END,
			source_size = EXCLUDED.source_size,
			last_access_at = now()
		RETURNING id, state, (xmax = 0 OR state = 'queued')`, f.ID, quality, audio, f.Size).Scan(&id, &state, &queued)
	if err != nil {
		return id, err
	}
	if queued && s.Enqueue != nil {
		if err := s.Enqueue(ctx, id); err != nil {
			return id, fmt.Errorf("queue download transcode: %w", err)
		}
	}
	return id, nil
}

const jobCols = `d.id, d.item_id, d.file_id, d.quality, d.audio_index, d.created_at, f.path, f.size, f.duration_ms, f.subtitles,
	t.id, COALESCE(t.state, 'ready'), COALESCE(t.progress, 1), COALESCE(t.size_bytes, 0), COALESCE(t.error, '')`

func (s *Service) scanJob(row pgx.Row) (Job, error) {
	var j Job
	var path string
	var fileSize int64
	var subs []byte
	var tid *uuid.UUID
	var progress float32
	var tsize int64
	if err := row.Scan(&j.ID, &j.ItemID, &j.FileID, &j.Quality, &j.AudioIndex, &j.CreatedAt, &path, &fileSize, &j.DurationMS, &subs,
		&tid, &j.State, &progress, &tsize, &j.Error); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return j, library.ErrNotFound
		}
		return j, err
	}
	j.Progress = float64(progress)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if j.Quality == Original {
		j.Container = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		j.Size, j.State, j.Progress = fileSize, "ready", 1
	} else {
		j.Container, j.Size = "mp4", tsize
	}
	j.FileName = base + "." + j.Container
	if j.State == "ready" {
		scope := fmt.Sprintf("/stream/dl/%s/", j.ID)
		j.URLExpires = s.now().Add(URLTTL)
		j.URL = scope + "video." + j.Container + "?" + s.Signer.Query(scope, URLTTL)
	}
	var tracks []library.SubtitleStream
	_ = json.Unmarshal(subs, &tracks)
	subScope := fmt.Sprintf("/stream/sub/%s/", j.FileID)
	q := s.Signer.Query(subScope, URLTTL)
	for i, t := range tracks {
		if !stream.IsTextSubtitle(t.Codec) {
			continue
		}
		j.Subtitles = append(j.Subtitles, Subtitle{Index: i, Language: t.Language, Forced: t.Forced,
			SRT: fmt.Sprintf("%s%d.srt?%s", subScope, i, q), VTT: fmt.Sprintf("%s%d.vtt?%s", subScope, i, q)})
	}
	return j, nil
}

const jobFrom = ` FROM downloads d JOIN media_files f ON f.id = d.file_id LEFT JOIN download_transcodes t ON t.id = d.transcode_id `

// Get returns one of p's jobs.
func (s *Service) Get(ctx context.Context, p auth.Principal, id uuid.UUID) (Job, error) {
	return s.scanJob(s.Pool.QueryRow(ctx, `SELECT `+jobCols+jobFrom+`WHERE d.id = $1 AND d.owner_id = $2`, id, Owner(p)))
}

// List returns p's jobs, newest first.
func (s *Service) List(ctx context.Context, p auth.Principal) ([]Job, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+jobCols+jobFrom+`WHERE d.owner_id = $1 ORDER BY d.created_at DESC LIMIT 1000`, Owner(p))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := s.scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Delete forgets one of p's jobs.
func (s *Service) Delete(ctx context.Context, p auth.Principal, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM downloads WHERE id = $1 AND owner_id = $2`, id, Owner(p))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return library.ErrNotFound
	}
	return nil
}

// OriginalLink returns a signed attachment link to an item's original file.
func (s *Service) OriginalLink(ctx context.Context, p auth.Principal, itemID uuid.UUID, fileID *uuid.UUID) (link, name string, size int64, err error) {
	if err := s.Allowed(ctx, p, itemID); err != nil {
		return "", "", 0, err
	}
	f, err := s.pickFile(ctx, itemID, fileID)
	if err != nil {
		return "", "", 0, err
	}
	if _, err := library.ResolveSafePath(s.Roots, f.Path); err != nil {
		return "", "", 0, library.ErrNotFound
	}
	name = filepath.Base(f.Path)
	scope := fmt.Sprintf("/stream/orig/%s/", f.ID)
	// The name is part of the path for the browser's sake; it only sets Content-Disposition.
	return scope + url.PathEscape(name) + "?" + s.Signer.Query(scope, time.Hour), name, f.Size, nil
}

// ServeHTTP serves /stream/dl/{id}/{name} behind the stream signature: the
// original file (re-validated with ResolveSafePath) or the transcoded MP4 from
// the cache, with byte ranges. A deleted or revoked job answers 404.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var quality, path, title string
	var fileID uuid.UUID
	var tid *uuid.UUID
	var state *string
	err = s.Pool.QueryRow(r.Context(), `SELECT d.quality, d.file_id, f.path, i.title, d.transcode_id, t.state
		FROM downloads d JOIN media_files f ON f.id = d.file_id JOIN items i ON i.id = d.item_id
		LEFT JOIN download_transcodes t ON t.id = d.transcode_id WHERE d.id = $1`, id).
		Scan(&quality, &fileID, &path, &title, &tid, &state)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var p string
	if quality == Original {
		if p, err = library.ResolveSafePath(s.Roots, path); err != nil {
			http.NotFound(w, r)
			return
		}
	} else {
		if tid == nil || state == nil || *state != "ready" {
			http.NotFound(w, r)
			return
		}
		p = TranscodePath(s.Dir, *tid)
		_, _ = s.Pool.Exec(r.Context(), `UPDATE download_transcodes SET last_access_at = now() WHERE id = $1`, *tid)
	}
	f, err := os.Open(p) // #nosec G304 -- validated media path or a cache file named by UUID
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	ext := filepath.Ext(p)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Disposition", stream.Attachment(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+ext))
	if ext == ".mp4" {
		w.Header().Set("Content-Type", "video/mp4")
	}
	http.ServeContent(w, r, "video"+ext, st.ModTime(), f)
}
