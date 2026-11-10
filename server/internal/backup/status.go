package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/streamero-app/streamero/server/internal/db"
)

// Restore states.
const (
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
)

// Status is the outcome of the last restore, kept next to the backups so it survives
// the restart that follows a restore.
type Status struct {
	Backup       string     `json:"backup"`
	State        string     `json:"state"`
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	Error        string     `json:"error,omitempty"`
	SafetyBackup string     `json:"safetyBackup,omitempty"`
	Warnings     []string   `json:"warnings,omitempty"`
}

const statusFile = ".restore-status.json"

// WriteStatus records the restore status (best effort).
func (s *Service) WriteStatus(st Status) error {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, statusFile+".tmp")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, statusFile))
}

// ReadStatus returns the last restore status, if any.
func (s *Service) ReadStatus() (*Status, bool) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, statusFile)) //nolint:gosec // G304: fixed name inside the backup folder
	if err != nil {
		return nil, false
	}
	var st Status
	if json.Unmarshal(raw, &st) != nil || st.Backup == "" {
		return nil, false
	}
	return &st, true
}

// Finish fills in the outcome of a restore.
func (st *Status) Finish(res *RestoreResult, err error, at time.Time) {
	st.FinishedAt = &at
	if res != nil {
		st.SafetyBackup = res.SafetyBackup
		st.Warnings = res.Warnings
	}
	if err != nil {
		st.State, st.Error = StateFailed, err.Error()
	} else {
		st.State = StateSucceeded
	}
}

// Import stores an uploaded archive in Dir after verifying it. The stored name is
// "uploaded-<time>-<version>.tar.gz"; the client's file name is never used.
func (s *Service) Import(src io.Reader) (Info, error) {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return Info{}, err
	}
	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	partial := filepath.Join(s.Dir, ".upload-"+hex.EncodeToString(rnd[:])+".partial")
	f, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: random name inside the backup folder
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = os.Remove(partial) }()
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		return Info{}, fmt.Errorf("upload: %w", err)
	}
	if err := f.Close(); err != nil {
		return Info{}, err
	}
	m, err := Verify(partial)
	if err != nil {
		if errors.Is(err, ErrInvalid) || errors.Is(err, errUnknownFormat) {
			return Info{}, ErrInvalid
		}
		return Info{}, err
	}
	name := s.fileName("uploaded", s.now(), m.ServerVersion)
	if err := os.Rename(partial, filepath.Join(s.Dir, name)); err != nil {
		return Info{}, err
	}
	fi, err := os.Stat(filepath.Join(s.Dir, name))
	if err != nil {
		return Info{}, err
	}
	info := Info{Name: name, Size: fi.Size(), Kind: KindUploaded, Manifest: m, ModTime: fi.ModTime().UTC()}
	if latest, err := db.LatestVersion(); err == nil && m.SchemaVersion > latest {
		info.Problem = "newer_schema"
	}
	return info, nil
}

// RecoverStatus marks a restore that is recorded as running but holds no lock (the
// process died mid-restore) as failed, so the admin UI does not wait for it forever.
func (s *Service) RecoverStatus(ctx context.Context) {
	st, ok := s.ReadStatus()
	if !ok || st.State != StateRunning {
		return
	}
	unlock, err := s.lock(ctx, false)
	if err != nil {
		return // a restore (or backup) is running elsewhere, or the database is unreachable
	}
	defer unlock()
	now := s.now()
	st.State, st.Error, st.FinishedAt = StateFailed, "the restore was interrupted; restore a backup again", &now
	if err := s.WriteStatus(*st); err != nil {
		s.log().Warn("could not record the interrupted restore", "err", err)
	}
}
