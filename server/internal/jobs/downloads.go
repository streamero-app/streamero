package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/streamero-app/streamero/server/internal/downloads"
)

// DownloadTranscodeArgs produces one download transcode (D-040).
type DownloadTranscodeArgs struct {
	TranscodeID uuid.UUID `json:"transcodeId"`
}

// Kind implements river.JobArgs.
func (DownloadTranscodeArgs) Kind() string { return "downloads.transcode" }

// InsertOpts allows many attempts: transcodes may run for hours and are retried by the transcoder's own state.
func (DownloadTranscodeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1000}
}

// DownloadTranscodeWorker handles DownloadTranscodeArgs.
type DownloadTranscodeWorker struct {
	river.WorkerDefaults[DownloadTranscodeArgs]
	Transcoder *downloads.Transcoder
}

// Timeout is unlimited: a film can take hours; the transcoder keeps its own heartbeat.
func (w *DownloadTranscodeWorker) Timeout(*river.Job[DownloadTranscodeArgs]) time.Duration { return -1 }

// Work implements river.Worker. While every slot is taken the job waits (snoozes).
func (w *DownloadTranscodeWorker) Work(ctx context.Context, job *river.Job[DownloadTranscodeArgs]) error {
	err := w.Transcoder.Run(ctx, job.Args.TranscodeID)
	if errors.Is(err, downloads.ErrBusy) {
		return river.JobSnooze(15 * time.Second)
	}
	return err
}

// DownloadMaintainArgs cleans the download cache.
type DownloadMaintainArgs struct{}

// Kind implements river.JobArgs.
func (DownloadMaintainArgs) Kind() string { return "downloads.maintain" }

// DownloadMaintainWorker handles DownloadMaintainArgs.
type DownloadMaintainWorker struct {
	river.WorkerDefaults[DownloadMaintainArgs]
	Transcoder *downloads.Transcoder
}

// Work implements river.Worker: dead transcodes go back to the queue, old files go.
func (w *DownloadMaintainWorker) Work(ctx context.Context, _ *river.Job[DownloadMaintainArgs]) error {
	if err := w.Transcoder.Recover(ctx, downloads.StaleAfter, false); err != nil {
		return err
	}
	return w.Transcoder.Maintain(ctx)
}

// RegisterDownloads adds the download workers.
func RegisterDownloads(workers *river.Workers, t *downloads.Transcoder) {
	river.AddWorker(workers, &DownloadTranscodeWorker{Transcoder: t})
	river.AddWorker(workers, &DownloadMaintainWorker{Transcoder: t})
}
