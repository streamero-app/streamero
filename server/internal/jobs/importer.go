package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/streamero-app/streamero/server/internal/importer"
)

// ImportArgs runs one queued import (watch history from another media server).
// The API key is not part of the job; the worker reads it from import_runs.
type ImportArgs struct {
	RunID uuid.UUID `json:"run_id"`
}

// Kind implements river.JobArgs.
func (ImportArgs) Kind() string { return "import.run" }

// InsertOpts runs an import once: the key is forgotten when it ends, so a retry could not work.
func (ImportArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 1} }

// ImportWorker runs imports.
type ImportWorker struct {
	river.WorkerDefaults[ImportArgs]
	Runner *importer.Runner
}

// Timeout allows large libraries on slow servers.
func (w *ImportWorker) Timeout(*river.Job[ImportArgs]) time.Duration { return 3 * time.Hour }

// Work implements river.Worker.
func (w *ImportWorker) Work(ctx context.Context, job *river.Job[ImportArgs]) error {
	return w.Runner.Run(ctx, job.Args.RunID)
}

// RegisterImport adds the import worker.
func RegisterImport(workers *river.Workers, runner *importer.Runner) {
	river.AddWorker(workers, &ImportWorker{Runner: runner})
}
