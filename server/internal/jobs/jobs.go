// Package jobs defines background jobs executed by `streamero worker` via River.
package jobs

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/streamero-app/streamero/server/internal/library"
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
func Register(workers *river.Workers, log *slog.Logger, scanner *library.Scanner) {
	river.AddWorker(workers, &HeartbeatWorker{Log: log})
	river.AddWorker(workers, &LibraryScanWorker{Scanner: scanner, Log: log})
}
