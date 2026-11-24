package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/streamero-app/streamero/server/internal/adult"
)

// AdultEnrichArgs looks up the new scenes of an adult library in ThePornDB /
// StashDB and grabs frames as artwork (D-053).
type AdultEnrichArgs struct {
	LibraryID uuid.UUID `json:"library_id"`
	Force     bool      `json:"force,omitempty"`
}

// Kind implements river.JobArgs.
func (AdultEnrichArgs) Kind() string { return "adult.enrich" }

// InsertOpts keeps at most one waiting run per library.
func (AdultEnrichArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 2, UniqueOpts: whileQueued}
}

// AdultEnrichWorker runs lookups. Its log lines carry ids only, never titles.
type AdultEnrichWorker struct {
	river.WorkerDefaults[AdultEnrichArgs]
	Adult *adult.Service
	Log   *slog.Logger
}

// Timeout allows a large first lookup (rate limited to 2 calls a second).
func (w *AdultEnrichWorker) Timeout(*river.Job[AdultEnrichArgs]) time.Duration { return 2 * time.Hour }

// Work implements river.Worker.
func (w *AdultEnrichWorker) Work(ctx context.Context, job *river.Job[AdultEnrichArgs]) error {
	n, err := w.Adult.Enrich(ctx, job.Args.LibraryID, job.Args.Force)
	if err != nil {
		w.Log.Warn("adult metadata lookup stopped", "library", job.Args.LibraryID, "err", err)
		return river.JobCancel(err)
	}
	w.Log.Info("adult metadata lookup done", "library", job.Args.LibraryID, "matched", n)
	return nil
}

// RegisterAdult adds the adult workers.
func RegisterAdult(workers *river.Workers, a *adult.Service, log *slog.Logger) {
	river.AddWorker(workers, &AdultEnrichWorker{Adult: a, Log: log})
}
