package jobs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
)

// LibraryScanArgs scans one library.
type LibraryScanArgs struct {
	LibraryID uuid.UUID `json:"library_id"`
}

// Kind implements river.JobArgs.
func (LibraryScanArgs) Kind() string { return "library.scan" }

// InsertOpts keeps one pending scan per library.
func (LibraryScanArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// LibraryScanWorker runs scans.
type LibraryScanWorker struct {
	river.WorkerDefaults[LibraryScanArgs]
	Scanner *library.Scanner
	Log     *slog.Logger
	// AfterScan queues follow-up work (metadata for new items).
	AfterScan func(ctx context.Context, libraryID uuid.UUID) error
}

// Timeout allows long first scans of big libraries.
func (w *LibraryScanWorker) Timeout(*river.Job[LibraryScanArgs]) time.Duration { return 6 * time.Hour }

// Work implements river.Worker.
func (w *LibraryScanWorker) Work(ctx context.Context, job *river.Job[LibraryScanArgs]) error {
	start := time.Now()
	res, err := w.Scanner.Scan(ctx, job.Args.LibraryID)
	if err != nil {
		w.Log.Warn("library scan failed", "library", job.Args.LibraryID, "err", err)
		// A missing folder will not fix itself on retry; the library shows the error instead.
		return river.JobCancel(err)
	}
	w.Log.Info("library scanned", "library", job.Args.LibraryID, "files", res.Files, "probed", res.Probed,
		"removed", res.Removed, "took", time.Since(start).String())
	if w.AfterScan != nil {
		return w.AfterScan(ctx, job.Args.LibraryID)
	}
	return nil
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
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// MetadataRefreshWorker runs metadata refreshes.
type MetadataRefreshWorker struct {
	river.WorkerDefaults[MetadataRefreshArgs]
	Refresher *metadata.Refresher
	Log       *slog.Logger
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
	return w.Refresher.CleanupImages(ctx)
}
