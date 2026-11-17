package api

import (
	"context"
	"sync"
	"syscall"
	"time"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/version"
)

// StatsPath is where monitoring dashboards read the statistics (outside the
// generated router; the router serves GET /system/stats there, D-052).
const StatsPath = "/api/stats"

// statsTTL is how long the database part of the statistics is reused: a
// dashboard polling every few seconds costs one set of queries per interval.
const statsTTL = 15 * time.Second

// statsCache holds the last database snapshot. The mutex is held while the
// snapshot is refreshed, so parallel polls share one set of queries.
type statsCache struct {
	mu  sync.Mutex
	at  time.Time
	val gen.SystemStats
}

// ForgetStats drops the cached statistics so the next call reads the database.
func (h *Handler) ForgetStats() {
	h.stats.mu.Lock()
	h.stats.at = time.Time{}
	h.stats.mu.Unlock()
}

// GetSystemStats implements GET /system/stats (and /api/stats).
func (h *Handler) GetSystemStats(ctx context.Context) (*gen.SystemStats, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	out, err := h.libraryStats(ctx)
	if err != nil {
		return nil, err
	}
	// What follows is live and costs nothing to read, so it is never cached.
	out.Version, out.Commit = version.Version, version.Commit
	if h.NowPlaying != nil {
		out.ActiveStreams = len(h.NowPlaying.Active())
	}
	if h.Stream != nil && h.Stream() != nil && h.Stream().HLS != nil {
		out.Transcodes = h.Stream().HLS.Active()
	}
	out.DiskTotalBytes, out.DiskFreeBytes = mediaDisk(h.Paths.Media)
	return &out, nil
}

// libraryStats returns the counts read from the database, at most statsTTL old.
func (h *Handler) libraryStats(ctx context.Context) (gen.SystemStats, error) {
	h.stats.mu.Lock()
	defer h.stats.mu.Unlock()
	if !h.stats.at.IsZero() && time.Since(h.stats.at) < statsTTL {
		return copyStats(h.stats.val), nil
	}
	s, err := h.queryStats(ctx)
	if err != nil {
		return gen.SystemStats{}, err
	}
	h.stats.val, h.stats.at = s, time.Now()
	return copyStats(s), nil
}

// copyStats keeps callers from sharing the cached slice.
func copyStats(s gen.SystemStats) gen.SystemStats {
	s.Jobs = append([]gen.JobQueueStats{}, s.Jobs...)
	return s
}

// queryStats reads the statistics in four aggregate queries: one pass over
// items, one over media_files, the two small tables, and River's jobs by queue.
// They are whole-table aggregates (no index can answer a count of everything),
// which is why the result is cached.
func (h *Handler) queryStats(ctx context.Context) (gen.SystemStats, error) {
	pool := h.lib().Pool
	out := gen.SystemStats{Jobs: []gen.JobQueueStats{}}

	// "Waiting for metadata" is what the metadata refresh would pick up: unlocked
	// top-level movies and series it has not visited yet.
	rows, err := pool.Query(ctx, `SELECT kind, count(*), max(added_at),
			count(*) FILTER (WHERE metadata_error IS NOT NULL),
			count(*) FILTER (WHERE parent_id IS NULL AND kind IN ('movie', 'series')
				AND metadata_refreshed_at IS NULL AND NOT metadata_locked)
		FROM items GROUP BY kind`)
	if err != nil {
		return out, err
	}
	var last *time.Time
	var pending int64
	for rows.Next() {
		var kind string
		var n, failed, waiting int64
		var added *time.Time
		if err := rows.Scan(&kind, &n, &added, &failed, &waiting); err != nil {
			rows.Close()
			return out, err
		}
		switch kind {
		case "movie":
			out.Movies = n
		case "series":
			out.Series = n
		case "episode":
			out.Episodes = n
		case "album":
			out.Albums = n
		case "track":
			out.Tracks = n
		case "book":
			out.Books = n
		case "audiobook":
			out.Audiobooks = n
		case "photo":
			out.Photos = n
		case "video":
			out.Videos = n
		}
		out.Failed += failed
		pending += waiting
		if added != nil && (last == nil || added.After(*last)) {
			last = added
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	// For parity with the field other services report: everything that is a video.
	out.Videos += out.Movies + out.Episodes
	if last != nil {
		out.LastAddedAt = gen.NewOptDateTime(*last)
	}
	// Without a metadata provider key nothing is fetched, so nothing is waiting.
	if pending > 0 && h.Settings != nil && h.Settings() != nil {
		if cfg, err := metadata.LoadSettings(ctx, h.Settings()); err == nil && cfg.TMDbKey != "" {
			out.Processing = pending
		}
	}

	var unreadable int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(size), 0)::bigint, count(*) FILTER (WHERE probe_error IS NOT NULL)
		FROM media_files`).Scan(&out.UsageBytes, &unreadable); err != nil {
		return out, err
	}
	out.Failed += unreadable
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM libraries), (SELECT count(*) FROM users)`).
		Scan(&out.Libraries, &out.Users); err != nil {
		return out, err
	}

	// River's states, as the Tasks page reads them: "discarded" is a job that
	// gave up (failed); "retryable" still has attempts left, so it is waiting.
	rows, err = pool.Query(ctx, `SELECT queue,
			count(*) FILTER (WHERE state IN ('available', 'pending', 'scheduled', 'retryable')),
			count(*) FILTER (WHERE state = 'running'),
			count(*) FILTER (WHERE state = 'discarded')
		FROM river_job GROUP BY queue ORDER BY queue`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	hasDefault := false
	for rows.Next() {
		var q gen.JobQueueStats
		if err := rows.Scan(&q.Queue, &q.Queued, &q.Running, &q.Failed); err != nil {
			return out, err
		}
		hasDefault = hasDefault || q.Queue == defaultQueue
		out.Jobs = append(out.Jobs, q)
	}
	// The server's own queue is always listed, also while it has no jobs.
	if !hasDefault {
		out.Jobs = append([]gen.JobQueueStats{{Queue: defaultQueue}}, out.Jobs...)
	}
	return out, rows.Err()
}

// defaultQueue is River's default queue, the one the server's workers use.
const defaultQueue = "default"

// mediaDisk sums the size and free space of the media storage, counting each
// filesystem once when several media roots live on the same one.
func mediaDisk(paths []string) (total, free int64) {
	seen := map[syscall.Fsid]bool{}
	for _, p := range paths {
		var st syscall.Statfs_t
		if err := syscall.Statfs(p, &st); err != nil || seen[st.Fsid] {
			continue
		}
		seen[st.Fsid] = true
		bs := int64(st.Bsize)          //nolint:unconvert // Bsize is int32 on some platforms
		total += int64(st.Blocks) * bs //nolint:gosec // block counts fit int64
		free += int64(st.Bavail) * bs  //nolint:gosec // block counts fit int64
	}
	return total, free
}
