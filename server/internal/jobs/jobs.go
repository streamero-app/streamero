// Package jobs defines background jobs executed by `streamero worker` via River.
package jobs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/streamero-app/streamero/server/internal/analyze"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/notify"
)

// HeartbeatArgs is a periodic no-op job that proves the worker is alive.
type HeartbeatArgs struct{}

// Kind implements river.JobArgs.
func (HeartbeatArgs) Kind() string { return "system.heartbeat" }

// HeartbeatWorker handles HeartbeatArgs.
type HeartbeatWorker struct {
	river.WorkerDefaults[HeartbeatArgs]
	Log *slog.Logger
}

// Work implements river.Worker.
func (w *HeartbeatWorker) Work(_ context.Context, job *river.Job[HeartbeatArgs]) error {
	w.Log.Debug("worker heartbeat", "job_id", job.ID)
	return nil
}

// Register adds all workers to the registry.
func Register(workers *river.Workers, log *slog.Logger, scanner *library.Scanner, libraries *library.Service,
	refresher *metadata.Refresher, afterScan func(context.Context, uuid.UUID) error, n *notify.Dispatcher,
	analyzer *analyze.Analyzer, enqueueAnalyze func(context.Context, uuid.UUID) error,
	detector *analyze.Detector, enqueueDetect func(context.Context, uuid.UUID) error) {
	river.AddWorker(workers, &HeartbeatWorker{Log: log})
	river.AddWorker(workers, &LibraryScanWorker{Scanner: scanner, Log: log, AfterScan: afterScan, Notify: n})
	river.AddWorker(workers, &MetadataRefreshWorker{Refresher: refresher, Log: log, Analyze: enqueueAnalyze})
	river.AddWorker(workers, &MediaAnalyzeWorker{Analyzer: analyzer, Log: log, Then: enqueueDetect})
	river.AddWorker(workers, &MediaAnalyzeAllWorker{Libraries: libraries, Analyze: enqueueAnalyze})
	river.AddWorker(workers, &SegmentDetectWorker{Detector: detector, Log: log})
	river.AddWorker(workers, &SegmentDetectAllWorker{Libraries: libraries, Detect: enqueueDetect})
	river.AddWorker(workers, &LibraryScanAllWorker{Libraries: libraries})
	river.AddWorker(workers, &MetadataRefreshAllWorker{Libraries: libraries, Refresh: afterScan})
	river.AddWorker(workers, &ImagesCleanupWorker{Refresher: refresher})
}
