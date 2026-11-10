package jobs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/streamero-app/streamero/server/internal/backup"
	"github.com/streamero-app/streamero/server/internal/settings"
)

// BackupArgs writes one backup.
type BackupArgs struct {
	BackupKind string `json:"kind"` // backup.KindManual or backup.KindScheduled
}

// Kind implements river.JobArgs.
func (BackupArgs) Kind() string { return "backup.create" }

// InsertOpts keeps at most one waiting backup.
func (BackupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 2, UniqueOpts: river.UniqueOpts{ByState: whileQueued.ByState}}
}

// BackupWorker writes backups and applies retention.
type BackupWorker struct {
	river.WorkerDefaults[BackupArgs]
	Backups  *backup.Service
	Settings *settings.Store
	Log      *slog.Logger
}

// Timeout allows large databases and artwork folders.
func (w *BackupWorker) Timeout(*river.Job[BackupArgs]) time.Duration { return 6 * time.Hour }

// Work implements river.Worker.
func (w *BackupWorker) Work(ctx context.Context, job *river.Job[BackupArgs]) error {
	st := backup.LoadSettings(ctx, w.Settings)
	kind := job.Args.BackupKind
	if kind != backup.KindScheduled {
		kind = backup.KindManual
	}
	_, err := w.Backups.Create(ctx, backup.CreateOptions{Kind: kind, IncludeImages: st.IncludeImages})
	if errors.Is(err, backup.ErrBusy) {
		w.Log.Info("backup skipped: another backup or restore is running")
		return nil
	}
	if err != nil {
		return err
	}
	removed, err := w.Backups.Prune(st.KeepLast)
	if err != nil {
		w.Log.Warn("backup retention failed", "err", err)
	}
	if len(removed) > 0 {
		w.Log.Info("old backups deleted", "removed", removed, "keep", st.KeepLast)
	}
	return nil
}

// BackupScheduleArgs checks hourly whether the daily backup is due. Checking often (rather
// than one 24 h period) keeps the schedule when the worker restarts more often than daily.
type BackupScheduleArgs struct{}

// Kind implements river.JobArgs.
func (BackupScheduleArgs) Kind() string { return "backup.schedule" }

// BackupScheduleWorker queues a scheduled backup when the newest one is a day old.
type BackupScheduleWorker struct {
	river.WorkerDefaults[BackupScheduleArgs]
	Backups  *backup.Service
	Settings *settings.Store
	Enqueue  func(ctx context.Context) error
}

// BackupInterval is how often scheduled backups are written.
const BackupInterval = 24 * time.Hour

// Work implements river.Worker.
func (w *BackupScheduleWorker) Work(ctx context.Context, _ *river.Job[BackupScheduleArgs]) error {
	if !backup.LoadSettings(ctx, w.Settings).Enabled {
		return nil
	}
	// A few minutes of slack so an hourly check does not slip the backup by an hour a day.
	if last := w.Backups.Newest(); !last.IsZero() && time.Since(last) < BackupInterval-10*time.Minute {
		return nil
	}
	return w.Enqueue(ctx)
}

// RegisterBackup adds the backup workers.
func RegisterBackup(workers *river.Workers, log *slog.Logger, svc *backup.Service, st *settings.Store, enqueue func(context.Context) error) {
	river.AddWorker(workers, &BackupWorker{Backups: svc, Settings: st, Log: log})
	river.AddWorker(workers, &BackupScheduleWorker{Backups: svc, Settings: st, Enqueue: enqueue})
}
