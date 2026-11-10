package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/backup"
	"github.com/streamero-app/streamero/server/internal/db"
)

// Backups wires backup and restore into the API (D-030).
type Backups struct {
	Service *backup.Service
	// Enqueue queues a backup in the worker.
	Enqueue func(ctx context.Context) error
	// Restoring switches the API to "starting" while a restore runs.
	Restoring func(on bool)
	// AfterRestore is called when a restore that changed the database has ended (the
	// server restarts so no state from before the restore survives).
	AfterRestore func()
}

func (h *Handler) backups() (*Backups, error) {
	b := h.Backup.Load()
	if b == nil || b.Service == nil || h.lib() == nil {
		return nil, errStarting
	}
	return b, nil
}

func mapBackupError(err error) *StatusError {
	switch {
	case errors.Is(err, backup.ErrNotFound):
		return &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, backup.ErrInvalidName):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}
	case errors.Is(err, backup.ErrNewerSchema):
		return &StatusError{Status: http.StatusConflict, Code: "backup_newer_schema", Message: err.Error()}
	case errors.Is(err, backup.ErrInvalid):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_backup", Message: err.Error()}
	case errors.Is(err, backup.ErrBusy):
		return &StatusError{Status: http.StatusConflict, Code: "backup_busy", Message: err.Error()}
	}
	return nil
}

func toBackup(i backup.Info) gen.Backup {
	out := gen.Backup{Name: i.Name, SizeBytes: i.Size, CreatedAt: i.CreatedAt(), Kind: gen.BackupKind(i.Kind),
		Restorable: i.Restorable(), SkippedTables: []string{}}
	if m := i.Manifest; m != nil {
		out.ServerVersion, out.SchemaVersion = m.ServerVersion, m.SchemaVersion
		out.IncludesImages, out.ScanRunning = m.IncludesImages, m.ScanRunning
		out.SkippedTables = m.SkippedNames()
	}
	if i.Problem != "" {
		out.Problem = gen.NewOptString(i.Problem)
	}
	return out
}

func toBackupSettings(s backup.Settings) gen.BackupSettings {
	return gen.BackupSettings{Enabled: s.Enabled, KeepLast: s.KeepLast, IncludeImages: s.IncludeImages}
}

// ListBackups implements GET /backups.
func (h *Handler) ListBackups(ctx context.Context) (*gen.BackupOverview, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	b, err := h.backups()
	if err != nil {
		return nil, err
	}
	list, err := b.Service.List()
	if err != nil {
		return nil, err
	}
	latest, err := db.LatestVersion()
	if err != nil {
		return nil, err
	}
	out := &gen.BackupOverview{Dir: b.Service.Dir, Backups: make([]gen.Backup, 0, len(list)),
		Settings: toBackupSettings(backup.LoadSettings(ctx, h.Settings())), SchemaVersion: latest,
		ScanRunning: backup.ScanRunning(ctx, h.lib().Pool)}
	for _, i := range list {
		out.Backups = append(out.Backups, toBackup(i))
	}
	_ = h.lib().Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM river_job WHERE kind = 'backup.create'
		AND state IN ('available', 'pending', 'running', 'scheduled', 'retryable'))`).Scan(&out.Running)
	if st, ok := b.Service.ReadStatus(); ok {
		rs := gen.RestoreStatus{Backup: st.Backup, State: gen.RestoreStatusState(st.State), StartedAt: st.StartedAt,
			Warnings: st.Warnings, SafetyBackup: optString(st.SafetyBackup), Error: optString(st.Error)}
		if st.FinishedAt != nil {
			rs.FinishedAt = gen.NewOptDateTime(*st.FinishedAt)
		}
		out.LastRestore = gen.NewOptRestoreStatus(rs)
	}
	return out, nil
}

// CreateBackup implements POST /backups.
func (h *Handler) CreateBackup(ctx context.Context) (*gen.BackupQueued, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	b, err := h.backups()
	if err != nil {
		return nil, err
	}
	if b.Enqueue == nil {
		return nil, errStarting
	}
	if err := b.Enqueue(ctx); err != nil {
		return nil, err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "backup.started", nil)
	// A scan does not block the backup (the snapshot is consistent); the client warns.
	return &gen.BackupQueued{ScanRunning: backup.ScanRunning(ctx, h.lib().Pool)}, nil
}

// UpdateBackupSettings implements PUT /backups/settings.
func (h *Handler) UpdateBackupSettings(ctx context.Context, req *gen.BackupSettings) (*gen.BackupSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if h.Settings() == nil {
		return nil, errStarting
	}
	st := backup.Settings{Enabled: req.Enabled, KeepLast: req.KeepLast, IncludeImages: req.IncludeImages}.Normalize()
	if err := h.Settings().Set(ctx, backup.SettingsKey, st); err != nil {
		return nil, err
	}
	out := toBackupSettings(st)
	return &out, nil
}

// UploadBackup implements POST /backups/upload.
func (h *Handler) UploadBackup(ctx context.Context, req gen.UploadBackupReq) (*gen.Backup, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	b, err := h.backups()
	if err != nil {
		return nil, err
	}
	info, err := b.Service.Import(req.Data)
	if err != nil {
		return nil, err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "backup.uploaded", map[string]any{"backup": info.Name})
	out := toBackup(info)
	return &out, nil
}

// DeleteBackup implements DELETE /backups/{backupName}.
func (h *Handler) DeleteBackup(ctx context.Context, params gen.DeleteBackupParams) error {
	p, err := admin(ctx)
	if err != nil {
		return err
	}
	b, err := h.backups()
	if err != nil {
		return err
	}
	if err := b.Service.Delete(params.BackupName); err != nil {
		return err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "backup.deleted", map[string]any{"backup": params.BackupName})
	return nil
}

// DownloadBackup implements GET /backups/{backupName}/download.
func (h *Handler) DownloadBackup(ctx context.Context, params gen.DownloadBackupParams) (*gen.DownloadBackupOKHeaders, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	b, err := h.backups()
	if err != nil {
		return nil, err
	}
	p, err := b.Service.Path(params.BackupName)
	if err != nil {
		return nil, err
	}
	f, err := openFile(p)
	if err != nil {
		return nil, backup.ErrNotFound
	}
	// The name matched a strict [A-Za-z0-9._-] pattern, so it needs no quoting beyond this.
	return &gen.DownloadBackupOKHeaders{ContentDisposition: gen.NewOptString(`attachment; filename="` + params.BackupName + `"`),
		Response: gen.DownloadBackupOK{Data: f}}, nil
}

// RestoreBackup implements POST /backups/{backupName}/restore.
func (h *Handler) RestoreBackup(ctx context.Context, req *gen.RestoreBackupRequest, params gen.RestoreBackupParams) error {
	p, err := admin(ctx)
	if err != nil {
		return err
	}
	b, err := h.backups()
	if err != nil {
		return err
	}
	if req.Confirm != params.BackupName {
		return &StatusError{Status: http.StatusBadRequest, Code: "backup_confirm_mismatch", Message: "type the backup name to confirm"}
	}
	path, err := b.Service.Path(params.BackupName)
	if err != nil {
		return err
	}
	// Refuse damaged archives and newer schemas before anything changes.
	if _, err := backup.Prepare(path); err != nil {
		return err
	}
	if !h.restoring.CompareAndSwap(false, true) {
		return &StatusError{Status: http.StatusConflict, Code: "backup_busy", Message: "a restore is already running"}
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "backup.restore_started", map[string]any{"backup": params.BackupName})
	st := backup.Status{Backup: params.BackupName, State: backup.StateRunning, StartedAt: time.Now().UTC()}
	_ = b.Service.WriteStatus(st)
	// Detached from the request: restores can take longer than any proxy waits.
	go h.runRestore(context.WithoutCancel(ctx), b, path, st)
	return nil
}

// runRestore restores in the background and records the outcome.
func (h *Handler) runRestore(ctx context.Context, b *Backups, path string, st backup.Status) {
	defer h.restoring.Store(false)
	if b.Restoring != nil {
		b.Restoring(true)
	}
	res, err := b.Service.Restore(ctx, path, backup.RestoreOptions{SafetyBackup: true})
	st.Finish(res, err, time.Now().UTC())
	if werr := b.Service.WriteStatus(st); werr != nil {
		slog.Error("could not record restore status", "err", werr)
	}
	if err != nil {
		slog.Error("restore failed", "backup", st.Backup, "err", err, "database_changed", res.Changed)
	}
	if (err == nil || res.Changed) && b.AfterRestore != nil {
		b.AfterRestore()
		return
	}
	if b.Restoring != nil {
		b.Restoring(false)
	}
}

func openFile(p string) (*os.File, error) {
	return os.Open(p) //nolint:gosec // G304: p came from backup.Service.Path (ResolveSafePath inside the backup folder)
}
