package downloads

import (
	"context"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
)

// TranscodeInfo is a download transcode for the administrator.
type TranscodeInfo struct {
	ID, ItemID     uuid.UUID
	Title          string
	Quality, State string
	Progress       float64
	Size           int64
	Error          string
	Jobs           int
	Users          []string
	CreatedAt      time.Time
	LastAccessAt   time.Time
}

// Overview is the administrator's view of the download cache.
type Overview struct {
	Settings        Settings
	Running, Queued int
	CacheBytes      int64
	Transcodes      []TranscodeInfo
}

// Admin lists the download transcodes, running first.
func (s *Service) Admin(ctx context.Context) (Overview, error) {
	o := Overview{Settings: LoadSettings(ctx, s.Settings), Transcodes: []TranscodeInfo{}}
	rows, err := s.Pool.Query(ctx, `SELECT t.id, f.item_id, CASE WHEN i.kind = 'episode' AND s.title IS NOT NULL
				THEN s.title || ' – ' || i.title ELSE i.title END,
			t.quality, t.state, t.progress, t.size_bytes, t.error, t.created_at, t.last_access_at,
			(SELECT count(*) FROM downloads d WHERE d.transcode_id = t.id),
			COALESCE((SELECT array_agg(DISTINCT u.username) FROM downloads d JOIN users u ON u.id = d.user_id WHERE d.transcode_id = t.id), '{}')
		FROM download_transcodes t JOIN media_files f ON f.id = t.file_id JOIN items i ON i.id = f.item_id
			LEFT JOIN items se ON se.id = i.parent_id LEFT JOIN items s ON s.id = se.parent_id
		ORDER BY CASE t.state WHEN 'transcoding' THEN 0 WHEN 'queued' THEN 1 WHEN 'failed' THEN 2 ELSE 3 END, t.created_at DESC
		LIMIT 500`)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	for rows.Next() {
		var x TranscodeInfo
		var progress float32
		if err := rows.Scan(&x.ID, &x.ItemID, &x.Title, &x.Quality, &x.State, &progress, &x.Size, &x.Error, &x.CreatedAt,
			&x.LastAccessAt, &x.Jobs, &x.Users); err != nil {
			return o, err
		}
		x.Progress = float64(progress)
		switch x.State {
		case "transcoding":
			o.Running++
		case "queued":
			o.Queued++
		case "ready":
			o.CacheBytes += x.Size
		}
		o.Transcodes = append(o.Transcodes, x)
	}
	return o, rows.Err()
}

// AdminDelete stops (the worker notices within seconds) and removes a transcode and its jobs.
func (s *Service) AdminDelete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM download_transcodes WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return library.ErrNotFound
	}
	_ = os.Remove(TranscodePath(s.Dir, id))
	return nil
}
