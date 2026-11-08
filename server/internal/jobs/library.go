package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/streamero-app/streamero/server/internal/analyze"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/notify"
)

// LibraryScanArgs scans one library.
type LibraryScanArgs struct {
	LibraryID uuid.UUID `json:"library_id"`
}

// Kind implements river.JobArgs.
func (LibraryScanArgs) Kind() string { return "library.scan" }

// InsertOpts keeps one pending scan per library.
// whileQueued makes a job unique only while one is waiting or running. River's default
// also counts completed jobs (until they are cleaned, ~24 h), which silently dropped
// every rescan after the first one.
var whileQueued = river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
	rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateScheduled,
}}

// InsertOpts keeps at most one waiting scan per library.
func (LibraryScanArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: whileQueued}
}

// LibraryScanWorker runs scans.
type LibraryScanWorker struct {
	river.WorkerDefaults[LibraryScanArgs]
	Scanner *library.Scanner
	Log     *slog.Logger
	// AfterScan queues follow-up work (metadata for new items).
	AfterScan func(ctx context.Context, libraryID uuid.UUID) error
	// Notify announces new titles and failed scans (nil disables it).
	Notify *notify.Dispatcher
}

// Timeout allows long first scans of big libraries.
func (w *LibraryScanWorker) Timeout(*river.Job[LibraryScanArgs]) time.Duration { return 6 * time.Hour }

// Work implements river.Worker.
func (w *LibraryScanWorker) Work(ctx context.Context, job *river.Job[LibraryScanArgs]) error {
	start := time.Now()
	res, err := w.Scanner.Scan(ctx, job.Args.LibraryID)
	if err != nil {
		w.Log.Warn("library scan failed", "library", job.Args.LibraryID, "err", err)
		w.Notify.Emit(ctx, notify.Event{Kind: notify.ScanFailed, Title: "Library scan failed", Message: err.Error(),
			Data: map[string]any{"libraryId": job.Args.LibraryID}})
		// A missing folder will not fix itself on retry; the library shows the error instead.
		return river.JobCancel(err)
	}
	w.announceNew(ctx, job.Args.LibraryID, start)
	w.Log.Info("library scanned", "library", job.Args.LibraryID, "files", res.Files, "probed", res.Probed,
		"removed", res.Removed, "took", time.Since(start).String())
	if w.AfterScan != nil {
		return w.AfterScan(ctx, job.Args.LibraryID)
	}
	return nil
}

// announceNew sends one "new in library" notification for titles this scan added.
func (w *LibraryScanWorker) announceNew(ctx context.Context, libraryID uuid.UUID, since time.Time) {
	if w.Notify == nil {
		return
	}
	pool := w.Scanner.Pool
	var libName string
	if err := pool.QueryRow(ctx, `SELECT name FROM libraries WHERE id = $1`, libraryID).Scan(&libName); err != nil {
		return
	}
	rows, err := pool.Query(ctx, `SELECT i.kind, COALESCE(s.title, i.title), i.year, i.season_number, i.episode_number
		FROM items i
		LEFT JOIN items se ON se.id = i.parent_id
		LEFT JOIN items s ON s.id = COALESCE(se.parent_id, i.parent_id) AND i.kind = 'episode'
		WHERE i.library_id = $1 AND i.added_at >= $2 AND i.kind IN ('movie', 'series', 'episode')
		ORDER BY i.added_at LIMIT 200`, libraryID, since)
	if err != nil {
		return
	}
	defer rows.Close()
	var lines []string
	total := 0
	for rows.Next() {
		var kind, title string
		var year, season, ep *int
		if rows.Scan(&kind, &title, &year, &season, &ep) != nil {
			return
		}
		total++
		if len(lines) >= 10 {
			continue
		}
		switch {
		case kind == "episode" && season != nil && ep != nil:
			lines = append(lines, fmt.Sprintf("%s S%02dE%02d", title, *season, *ep))
		case year != nil:
			lines = append(lines, fmt.Sprintf("%s (%d)", title, *year))
		default:
			lines = append(lines, title)
		}
	}
	if total == 0 {
		return
	}
	msg := strings.Join(lines, "\n")
	if total > len(lines) {
		msg += fmt.Sprintf("\n… +%d", total-len(lines))
	}
	w.Notify.Emit(ctx, notify.Event{Kind: notify.ItemAdded, Title: "New in " + libName, Message: msg,
		Data: map[string]any{"libraryId": libraryID, "count": total}})
}

// LibraryScanAllArgs queues a scan of every library (periodic safety net for
// shares that do not deliver file events).
type LibraryScanAllArgs struct{}

// Kind implements river.JobArgs.
func (LibraryScanAllArgs) Kind() string { return "library.scan_all" }

// LibraryScanAllWorker queues scans.
type LibraryScanAllWorker struct {
	river.WorkerDefaults[LibraryScanAllArgs]
	Libraries *library.Service
}

// Work implements river.Worker.
func (w *LibraryScanAllWorker) Work(ctx context.Context, _ *river.Job[LibraryScanAllArgs]) error {
	libs, err := w.Libraries.List(ctx)
	if err != nil {
		return err
	}
	for _, l := range libs {
		if err := w.Libraries.Scan(ctx, l.ID); err != nil {
			return err
		}
	}
	return nil
}

// MetadataRefreshArgs fetches metadata for a library (missing items, or all when Force).
type MetadataRefreshArgs struct {
	LibraryID uuid.UUID `json:"library_id"`
	Force     bool      `json:"force"`
}

// Kind implements river.JobArgs.
func (MetadataRefreshArgs) Kind() string { return "metadata.refresh_library" }

// InsertOpts keeps one pending refresh per library and mode.
func (MetadataRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: whileQueued}
}

// MetadataRefreshWorker runs metadata refreshes.
type MetadataRefreshWorker struct {
	river.WorkerDefaults[MetadataRefreshArgs]
	Refresher *metadata.Refresher
	Log       *slog.Logger
	// Analyze queues chapter/segment/trickplay analysis once metadata is in.
	Analyze func(ctx context.Context, libraryID uuid.UUID) error
}

// Timeout allows large libraries (TMDb is paced at ~30 requests/s).
func (w *MetadataRefreshWorker) Timeout(*river.Job[MetadataRefreshArgs]) time.Duration {
	return 12 * time.Hour
}

// Work implements river.Worker.
func (w *MetadataRefreshWorker) Work(ctx context.Context, job *river.Job[MetadataRefreshArgs]) error {
	n, err := w.Refresher.RefreshLibrary(ctx, job.Args.LibraryID, job.Args.Force)
	if err != nil {
		if errors.Is(err, metadata.ErrInvalidKey) {
			return river.JobCancel(err) // retrying will not help until an admin fixes the key
		}
		return err
	}
	w.Log.Info("metadata refreshed", "library", job.Args.LibraryID, "items", n)
	// Whatever TMDb could not illustrate gets a frame from its own video.
	if g, err := w.Refresher.GrabFrames(ctx, job.Args.LibraryID); err != nil {
		w.Log.Warn("frame grab failed", "library", job.Args.LibraryID, "err", err)
	} else if g > 0 {
		w.Log.Info("frame artwork created", "library", job.Args.LibraryID, "items", g)
	}
	if w.Analyze != nil {
		if err := w.Analyze(ctx, job.Args.LibraryID); err != nil {
			w.Log.Warn("could not queue media analysis", "err", err)
		}
	}
	return w.Refresher.CleanupImages(ctx)
}

// MetadataRefreshAllArgs queues a metadata refresh of every library (missing items only).
type MetadataRefreshAllArgs struct{}

// Kind implements river.JobArgs.
func (MetadataRefreshAllArgs) Kind() string { return "metadata.refresh_all" }

// MetadataRefreshAllWorker fans out per-library refresh jobs.
type MetadataRefreshAllWorker struct {
	river.WorkerDefaults[MetadataRefreshAllArgs]
	Libraries *library.Service
	Refresh   func(ctx context.Context, libraryID uuid.UUID) error
}

// Work implements river.Worker.
func (w *MetadataRefreshAllWorker) Work(ctx context.Context, _ *river.Job[MetadataRefreshAllArgs]) error {
	libs, err := w.Libraries.List(ctx)
	if err != nil {
		return err
	}
	for _, l := range libs {
		if err := w.Refresh(ctx, l.ID); err != nil {
			return err
		}
	}
	return nil
}

// ImagesCleanupArgs removes cached artwork of deleted items.
type ImagesCleanupArgs struct{}

// Kind implements river.JobArgs.
func (ImagesCleanupArgs) Kind() string { return "images.cleanup" }

// ImagesCleanupWorker runs Refresher.CleanupImages.
type ImagesCleanupWorker struct {
	river.WorkerDefaults[ImagesCleanupArgs]
	Refresher *metadata.Refresher
}

// Work implements river.Worker.
func (w *ImagesCleanupWorker) Work(ctx context.Context, _ *river.Job[ImagesCleanupArgs]) error {
	return w.Refresher.CleanupImages(ctx)
}

// MediaAnalyzeArgs analyzes new files of a library (chapters, intro/credits, trickplay).
type MediaAnalyzeArgs struct {
	LibraryID uuid.UUID `json:"library_id"`
}

// Kind implements river.JobArgs.
func (MediaAnalyzeArgs) Kind() string { return "media.analyze" }

// InsertOpts keeps at most one waiting analysis per library.
func (MediaAnalyzeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 2, UniqueOpts: whileQueued}
}

// MediaAnalyzeWorker runs the analyzer.
type MediaAnalyzeWorker struct {
	river.WorkerDefaults[MediaAnalyzeArgs]
	Analyzer *analyze.Analyzer
	Log      *slog.Logger
}

// Timeout allows trickplay of large libraries.
func (w *MediaAnalyzeWorker) Timeout(*river.Job[MediaAnalyzeArgs]) time.Duration {
	return 24 * time.Hour
}

// Work implements river.Worker.
func (w *MediaAnalyzeWorker) Work(ctx context.Context, job *river.Job[MediaAnalyzeArgs]) error {
	start := time.Now()
	n, err := w.Analyzer.Run(ctx, job.Args.LibraryID)
	if err != nil {
		return err
	}
	if n > 0 {
		w.Log.Info("media analyzed", "library", job.Args.LibraryID, "files", n, "took", time.Since(start).String())
	}
	return nil
}

// MediaAnalyzeAllArgs queues analysis of every library.
type MediaAnalyzeAllArgs struct{}

// Kind implements river.JobArgs.
func (MediaAnalyzeAllArgs) Kind() string { return "media.analyze_all" }

// MediaAnalyzeAllWorker fans out per-library analysis.
type MediaAnalyzeAllWorker struct {
	river.WorkerDefaults[MediaAnalyzeAllArgs]
	Libraries *library.Service
	Analyze   func(ctx context.Context, libraryID uuid.UUID) error
}

// Work implements river.Worker.
func (w *MediaAnalyzeAllWorker) Work(ctx context.Context, _ *river.Job[MediaAnalyzeAllArgs]) error {
	libs, err := w.Libraries.List(ctx)
	if err != nil {
		return err
	}
	for _, l := range libs {
		if err := w.Analyze(ctx, l.ID); err != nil {
			return err
		}
	}
	return nil
}
