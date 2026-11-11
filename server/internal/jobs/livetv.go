package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/streamero-app/streamero/server/internal/livetv"
)

// GuideRefreshArgs reloads one XMLTV guide source now (admin "Refresh").
type GuideRefreshArgs struct {
	GuideID uuid.UUID `json:"guide_id"`
}

// Kind implements river.JobArgs.
func (GuideRefreshArgs) Kind() string { return "livetv.guide_refresh" }

// InsertOpts keeps at most one waiting refresh per guide.
func (GuideRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: whileQueued}
}

// GuideRefreshWorker handles GuideRefreshArgs.
type GuideRefreshWorker struct {
	river.WorkerDefaults[GuideRefreshArgs]
	LiveTV *livetv.Service
}

// Timeout allows large guides.
func (w *GuideRefreshWorker) Timeout(*river.Job[GuideRefreshArgs]) time.Duration { return 30 * time.Minute }

// Work implements river.Worker.
func (w *GuideRefreshWorker) Work(ctx context.Context, job *river.Job[GuideRefreshArgs]) error {
	_, err := w.LiveTV.RefreshGuide(ctx, job.Args.GuideID, true)
	if errors.Is(err, livetv.ErrNotFound) {
		return nil // removed meanwhile
	}
	return err
}

// GuideRefreshDueArgs reloads the guides whose refresh interval has passed and
// the tuner lineups (periodic, hourly; also the "Refresh guide" task).
type GuideRefreshDueArgs struct{}

// Kind implements river.JobArgs.
func (GuideRefreshDueArgs) Kind() string { return "livetv.guide_refresh_due" }

// GuideRefreshDueWorker handles GuideRefreshDueArgs.
type GuideRefreshDueWorker struct {
	river.WorkerDefaults[GuideRefreshDueArgs]
	LiveTV *livetv.Service
}

// Timeout allows several large guides.
func (w *GuideRefreshDueWorker) Timeout(*river.Job[GuideRefreshDueArgs]) time.Duration { return time.Hour }

// Work implements river.Worker.
func (w *GuideRefreshDueWorker) Work(ctx context.Context, _ *river.Job[GuideRefreshDueArgs]) error {
	w.LiveTV.RefreshAllTuners(ctx)
	return w.LiveTV.RefreshDueGuides(ctx)
}

// RegisterLiveTV adds the Live TV workers.
func RegisterLiveTV(workers *river.Workers, svc *livetv.Service) {
	river.AddWorker(workers, &GuideRefreshWorker{LiveTV: svc})
	river.AddWorker(workers, &GuideRefreshDueWorker{LiveTV: svc})
}
