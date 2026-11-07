package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/streamero-app/streamero/server/internal/library"
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
