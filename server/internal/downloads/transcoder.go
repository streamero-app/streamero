package downloads

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// ErrBusy means the configured number of download transcodes is running.
var ErrBusy = errors.New("download transcodes are at the limit")

// claimLock serialises claiming a transcode slot across worker goroutines.
const claimLock int64 = 0x5354524d444c4f41 // "STRMDLOA"

// StaleAfter is how old a running transcode's heartbeat may get before it counts as dead.
const StaleAfter = 2 * time.Minute

// Transcoder produces download transcodes; it runs in the worker process.
type Transcoder struct {
	Pool     *pgxpool.Pool
	Roots    library.Roots
	FFmpeg   string
	Dir      string
	Settings *settings.Store
	Log      *slog.Logger
	// Enqueue queues a transcode job (used when recovering after a restart).
	Enqueue func(ctx context.Context, transcodeID uuid.UUID) error
	// Tick is how often progress and the heartbeat are written (default 2 s).
	Tick time.Duration
}

func (t *Transcoder) log() *slog.Logger {
	if t.Log != nil {
		return t.Log
	}
	return slog.Default()
}

// claim marks a queued transcode as running when a slot is free.
func (t *Transcoder) claim(ctx context.Context, id uuid.UUID) (fileID uuid.UUID, quality string, audio int, ok bool, err error) {
	limit := LoadSettings(ctx, t.Settings).MaxTranscodes
	err = pgx.BeginFunc(ctx, t.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, claimLock); err != nil {
			return err
		}
		var state string
		if err := tx.QueryRow(ctx, `SELECT state FROM download_transcodes WHERE id = $1`, id).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		if state != "queued" {
			return nil
		}
		var running int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM download_transcodes WHERE state = 'transcoding'`).Scan(&running); err != nil {
			return err
		}
		if running >= limit {
			return ErrBusy
		}
		err := tx.QueryRow(ctx, `UPDATE download_transcodes SET state = 'transcoding', started_at = now(), heartbeat_at = now(),
				attempts = attempts + 1, progress = 0, error = '' WHERE id = $1 RETURNING file_id, quality, audio_index`, id).
			Scan(&fileID, &quality, &audio)
		ok = err == nil
		return err
	})
	return fileID, quality, audio, ok, err
}

// Run produces one transcode. It returns ErrBusy when no slot is free (the
// caller retries later) and nil when there is nothing to do. On shutdown the
// transcode goes back to the queue; on failure it is marked failed.
func (t *Transcoder) Run(ctx context.Context, id uuid.UUID) error {
	fileID, quality, audio, ok, err := t.claim(ctx, id)
	if err != nil || !ok {
		return err
	}
	err = t.produce(ctx, id, fileID, quality, audio)
	switch {
	case err == nil:
		return t.Evict(context.WithoutCancel(ctx))
	case errors.Is(err, errCancelled):
		return nil
	case ctx.Err() != nil:
		// Worker shutting down: start again after the restart.
		_, _ = t.Pool.Exec(context.WithoutCancel(ctx), `UPDATE download_transcodes SET state = 'queued', progress = 0 WHERE id = $1 AND state = 'transcoding'`, id)
		return ctx.Err()
	default:
		t.log().Warn("download transcode failed", "transcode", id, "err", err)
		msg := err.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		_, _ = t.Pool.Exec(context.WithoutCancel(ctx), `UPDATE download_transcodes SET state = 'failed', error = $2, finished_at = now() WHERE id = $1`, id, msg)
		return nil
	}
}

var errCancelled = errors.New("download transcode was removed")

func (t *Transcoder) produce(ctx context.Context, id, fileID uuid.UUID, quality string, audio int) error {
	preset, ok := Presets[quality]
	if !ok {
		return ErrBadQuality
	}
	var f library.MediaFile
	var audioRaw []byte
	err := t.Pool.QueryRow(ctx, `SELECT id, path, size, duration_ms, height, COALESCE(video_codec, ''), COALESCE(hdr, ''),
			COALESCE(video_bit_depth, 0), audio FROM media_files WHERE id = $1`, fileID).
		Scan(&f.ID, &f.Path, &f.Size, &f.DurationMS, &f.Height, &f.VideoCodec, &f.HDR, &f.VideoBitDepth, &audioRaw)
	if err != nil {
		return fmt.Errorf("load file: %w", err)
	}
	_ = json.Unmarshal(audioRaw, &f.Audio)
	input, err := library.ResolveSafePath(t.Roots, f.Path)
	if err != nil {
		return errors.New("the media file is outside the media folders or gone")
	}
	if f.VideoCodec == "" {
		return errors.New("the file has no video")
	}
	enc := stream.DefaultEncoding()
	if t.Settings != nil {
		_, _ = t.Settings.Get(ctx, stream.KeyEncoding, &enc)
	}
	if err := os.MkdirAll(t.Dir, 0o750); err != nil {
		return err
	}
	final := TranscodePath(t.Dir, id)
	part := strings.TrimSuffix(final, ".mp4") + ".part.mp4"
	plan := stream.DownloadPlan(f, audio, preset.Height, preset.Bitrate)
	args := stream.DownloadArgs(stream.Job{Input: input, AudioIndex: audio, Plan: plan}, enc, part)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, t.FFmpeg, args...) // #nosec G204 -- fixed binary, validated input path
	var stderr strings.Builder
	cmd.Stderr = &tailWriter{b: &stderr, n: 2000}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	var doneUS atomic.Int64
	go func() { readProgress(stdout, &doneUS) }()
	waitc := make(chan error, 1)
	go func() { waitc <- cmd.Wait() }()
	tick := t.Tick
	if tick == 0 {
		tick = 2 * time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	cancelled := false
	for {
		select {
		case err := <-waitc:
			if cancelled {
				_ = os.Remove(part)
				return errCancelled
			}
			if err != nil {
				_ = os.Remove(part)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
			}
			st, err := os.Stat(part)
			if err != nil {
				return err
			}
			if err := os.Rename(part, final); err != nil {
				return err
			}
			tag, err := t.Pool.Exec(context.WithoutCancel(ctx), `UPDATE download_transcodes SET state = 'ready', progress = 1, size_bytes = $2,
				finished_at = now(), heartbeat_at = now(), last_access_at = now() WHERE id = $1`, id, st.Size())
			if err != nil || tag.RowsAffected() == 0 {
				_ = os.Remove(final) // removed meanwhile
				if err == nil {
					err = errCancelled
				}
				return err
			}
			return nil
		case <-ticker.C:
			progress := 0.0
			if f.DurationMS != nil && *f.DurationMS > 0 {
				progress = min(float64(doneUS.Load())/1000/float64(*f.DurationMS), 0.99)
			}
			tag, err := t.Pool.Exec(ctx, `UPDATE download_transcodes SET progress = $2, heartbeat_at = now() WHERE id = $1 AND state = 'transcoding'`, id, progress)
			if err == nil && tag.RowsAffected() == 0 && !cancelled {
				// An administrator removed it: stop ffmpeg.
				cancelled = true
				cancel()
			}
		}
	}
}

// readProgress follows ffmpeg's -progress output (out_time_us=…).
func readProgress(r io.Reader, done *atomic.Int64) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok || (k != "out_time_us" && k != "out_time_ms") {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
			done.Store(n) // both keys are microseconds
		}
	}
	_, _ = io.Copy(io.Discard, r)
}

type tailWriter struct {
	b *strings.Builder
	n int
}

func (w *tailWriter) Write(p []byte) (int, error) {
	if w.b.Len() < w.n {
		rest := w.n - w.b.Len()
		if len(p) < rest {
			rest = len(p)
		}
		w.b.Write(p[:rest])
	}
	return len(p), nil
}

// Recover puts transcodes whose worker died (no heartbeat for staleAfter; 0 =
// every running one, used when the only worker starts) back in the queue and
// queues them again. With all, every waiting transcode is queued again too
// (worker start): jobs survive restarts.
func (t *Transcoder) Recover(ctx context.Context, staleAfter time.Duration, all bool) error {
	rows, err := t.Pool.Query(ctx, `UPDATE download_transcodes SET state = 'queued', progress = 0
		WHERE state = 'transcoding' AND (heartbeat_at IS NULL OR heartbeat_at < now() - make_interval(secs => $1))
		RETURNING id`, staleAfter.Seconds())
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	if all {
		rows, err := t.Pool.Query(ctx, `SELECT id FROM download_transcodes WHERE state = 'queued' ORDER BY created_at`)
		if err != nil {
			return err
		}
		if ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if t.Enqueue != nil {
			if err := t.Enqueue(ctx, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// Maintain cleans the download cache: old jobs, unused or expired transcodes,
// the size limit and stray files. Runs periodically in the worker.
func (t *Transcoder) Maintain(ctx context.Context) error {
	st := LoadSettings(ctx, t.Settings)
	// Jobs nobody collected (clients delete theirs once the file is on the device).
	if _, err := t.Pool.Exec(ctx, `DELETE FROM downloads WHERE created_at < now() - make_interval(days => $1)`, st.KeepDays+7); err != nil {
		return err
	}
	rows, err := t.Pool.Query(ctx, `DELETE FROM download_transcodes t WHERE state <> 'transcoding' AND (
			(NOT EXISTS (SELECT 1 FROM downloads d WHERE d.transcode_id = t.id) AND t.last_access_at < now() - interval '1 day')
			OR t.last_access_at < now() - make_interval(days => $1))
		RETURNING id`, st.KeepDays)
	if err != nil {
		return err
	}
	gone, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range gone {
		_ = os.Remove(TranscodePath(t.Dir, id))
	}
	if err := t.Evict(ctx); err != nil {
		return err
	}
	return t.removeStrays(ctx)
}

// Evict deletes the least recently fetched ready transcodes while the cache is over its limit.
func (t *Transcoder) Evict(ctx context.Context) error {
	limit := int64(LoadSettings(ctx, t.Settings).CacheLimitGB) << 30
	rows, err := t.Pool.Query(ctx, `SELECT id FROM (
			SELECT id, sum(size_bytes) OVER (ORDER BY last_access_at DESC, id) AS running FROM download_transcodes WHERE state = 'ready'
		) x WHERE running > $1`, limit)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := t.Pool.Exec(ctx, `DELETE FROM download_transcodes WHERE id = $1 AND state = 'ready'`, id); err != nil {
			return err
		}
		_ = os.Remove(TranscodePath(t.Dir, id))
	}
	return nil
}

// removeStrays deletes cache files that belong to no transcode (left by a crash).
func (t *Transcoder) removeStrays(ctx context.Context) error {
	entries, err := os.ReadDir(t.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	rows, err := t.Pool.Query(ctx, `SELECT id, state FROM download_transcodes`)
	if err != nil {
		return err
	}
	known := map[string]string{}
	for rows.Next() {
		var id uuid.UUID
		var state string
		if err := rows.Scan(&id, &state); err != nil {
			rows.Close()
			return err
		}
		known[id.String()] = state
	}
	rows.Close()
	for _, e := range entries {
		name := e.Name()
		id, part := strings.TrimSuffix(name, ".mp4"), false
		if strings.HasSuffix(id, ".part") {
			id, part = strings.TrimSuffix(id, ".part"), true
		}
		state, ok := known[id]
		keep := ok && ((part && state == "transcoding") || (!part && state == "ready"))
		if keep {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) < time.Hour {
			continue // may belong to a transcode that just started or finished
		}
		_ = os.Remove(filepath.Join(t.Dir, name))
	}
	return nil
}
