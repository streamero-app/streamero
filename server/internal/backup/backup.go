// Package backup writes and restores Streamero backups (D-030).
//
// A backup is one .tar.gz archive: a manifest, a logical dump of every application table
// taken in a single repeatable-read snapshot, the files under the config folder, profile
// pictures, and optionally the cached artwork. Media files are never part of a backup.
package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/db"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
)

// Kinds of backups. The kind of uploaded and pre-restore backups comes from the file
// name, the others from the manifest.
const (
	KindScheduled  = "scheduled"
	KindManual     = "manual"
	KindPreRestore = "pre_restore"
	KindUploaded   = "uploaded"
)

// Errors returned to callers (the API maps them to status codes).
var (
	ErrNotFound      = errors.New("backup not found")
	ErrInvalidName   = errors.New("invalid backup name")
	ErrInvalid       = errors.New("the file is not a readable Streamero backup")
	ErrNewerSchema   = errors.New("the backup was made by a newer Streamero version; update the server first")
	ErrBusy          = errors.New("another backup or restore is running")
	errUnknownFormat = errors.New("unsupported backup format")
)

// Service creates, lists and restores backups.
type Service struct {
	Pool *pgxpool.Pool
	// Dir is the backup folder (STREAMERO_BACKUP_DIR, default /config/backups).
	Dir string
	// ConfigDir is archived as config/ (without Dir when Dir lies inside it).
	ConfigDir string
	// CacheDir holds avatars/ (always archived) and images/ (only on request).
	CacheDir string
	// Version and Commit of this server, recorded in the manifest and the file name.
	Version string
	Commit  string
	Log     *slog.Logger
	// Now is the clock (tests); nil means time.Now.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.New(slog.DiscardHandler)
}

// Info describes one backup file.
type Info struct {
	Name     string
	Size     int64
	Kind     string
	Manifest *Manifest // nil when the archive cannot be read
	Problem  string    // "", "unreadable" or "newer_schema"
	ModTime  time.Time
}

// CreatedAt is when the backup was taken (file time when the manifest is unreadable).
func (i Info) CreatedAt() time.Time {
	if i.Manifest != nil {
		return i.Manifest.CreatedAt
	}
	return i.ModTime
}

// Restorable reports whether this server can restore the backup.
func (i Info) Restorable() bool { return i.Manifest != nil && i.Problem == "" }

// SettingsKey is where the backup settings are stored.
const SettingsKey = "backup"

// Settings control scheduled backups.
type Settings struct {
	Enabled       bool `json:"enabled"`
	KeepLast      int  `json:"keepLast"`
	IncludeImages bool `json:"includeImages"`
}

// DefaultSettings returns a daily backup, the last seven kept, artwork left out.
func DefaultSettings() Settings { return Settings{Enabled: true, KeepLast: 7} }

// Normalize keeps values in range.
func (st Settings) Normalize() Settings {
	st.KeepLast = min(max(st.KeepLast, 1), 100)
	return st
}

// LoadSettings returns the stored settings (defaults when unset or unreadable).
func LoadSettings(ctx context.Context, store *settings.Store) Settings {
	st := DefaultSettings()
	if store != nil {
		_, _ = store.Get(ctx, SettingsKey, &st)
	}
	return st.Normalize()
}

// ScanRunning reports whether a library scan is running.
func ScanRunning(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) bool {
	var running bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM libraries WHERE scan_state = 'scanning')`).Scan(&running)
	return err == nil && running
}

// validName accepts only names this package produces (and the API's path pattern).
var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,180}\.tar\.gz$`)

// Path resolves a backup name to a file inside Dir. Names are checked against a strict
// pattern and the result passes library.ResolveSafePath, so nothing outside Dir is reachable.
func (s *Service) Path(name string) (string, error) {
	if !validName.MatchString(name) || strings.Contains(name, "..") {
		return "", ErrInvalidName
	}
	dir, err := filepath.Abs(s.Dir)
	if err != nil {
		return "", err
	}
	p, err := library.ResolveSafePath(library.Roots{dir}, filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", ErrInvalidName
	}
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return p, nil
}

// List returns the backups in Dir, newest first.
func (s *Service) List() ([]Info, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	latest, err := db.LatestVersion()
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, e := range entries {
		if !e.Type().IsRegular() || !validName.MatchString(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		info := Info{Name: e.Name(), Size: fi.Size()}
		m, err := ReadManifest(filepath.Join(s.Dir, e.Name()))
		switch {
		case err != nil:
			info.Problem = "unreadable"
		case m.SchemaVersion > latest:
			info.Manifest, info.Problem = m, "newer_schema"
		default:
			info.Manifest = m
		}
		info.Kind = kindOf(e.Name(), info.Manifest)
		info.ModTime = fi.ModTime().UTC()
		out = append(out, info)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].CreatedAt(), out[j].CreatedAt()
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

func kindOf(name string, m *Manifest) string {
	switch {
	case strings.HasPrefix(name, "streamero-pre-restore-"):
		return KindPreRestore
	case strings.HasPrefix(name, "uploaded-"):
		return KindUploaded
	case m != nil && m.Kind == KindScheduled:
		return KindScheduled
	default:
		return KindManual
	}
}

// Delete removes one backup file.
func (s *Service) Delete(name string) error {
	p, err := s.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// Prune deletes the oldest scheduled and manual backups beyond keep. Uploaded and
// pre-restore backups are only deleted by hand.
func (s *Service) Prune(keep int) ([]string, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	var removed []string
	n := 0
	for _, b := range list {
		if b.Manifest == nil || (b.Kind != KindScheduled && b.Kind != KindManual) {
			continue
		}
		n++
		if n <= keep {
			continue
		}
		if err := s.Delete(b.Name); err != nil {
			return removed, err
		}
		removed = append(removed, b.Name)
	}
	return removed, nil
}

// Newest returns when the newest scheduled or manual backup was taken (zero when none).
func (s *Service) Newest() time.Time {
	list, err := s.List()
	if err != nil {
		return time.Time{}
	}
	for _, b := range list {
		if b.Manifest != nil && (b.Kind == KindScheduled || b.Kind == KindManual) {
			return b.Manifest.CreatedAt
		}
	}
	return time.Time{}
}

var (
	unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	manyDots    = regexp.MustCompile(`\.{2,}`)
)

// fileName builds "<prefix>-20261110T153000Z-<version>.tar.gz", unique inside Dir.
func (s *Service) fileName(prefix string, at time.Time, ver string) string {
	ver = strings.Trim(manyDots.ReplaceAllString(unsafeChars.ReplaceAllString(ver, "-"), "."), "-.")
	if ver == "" {
		ver = "dev"
	}
	if len(ver) > 40 {
		ver = ver[:40]
	}
	base := fmt.Sprintf("%s-%s-%s", prefix, at.Format("20060102T150405Z"), ver)
	name := base + ".tar.gz"
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(s.Dir, name)); errors.Is(err, os.ErrNotExist) {
			return name
		}
		name = fmt.Sprintf("%s-%d.tar.gz", base, i)
	}
}

// lockID is the advisory lock held while a backup or restore runs (across processes).
const lockID int64 = 0x5354524d424b5550 // "STRMBKUP"

// lock takes the backup lock on a dedicated connection. With wait=false it fails with
// ErrBusy instead of waiting.
func (s *Service) lock(ctx context.Context, wait bool) (func(), error) {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if wait {
		if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockID); err != nil {
			conn.Release()
			return nil, err
		}
	} else {
		var ok bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, lockID).Scan(&ok); err != nil || !ok {
			conn.Release()
			if err != nil {
				return nil, err
			}
			return nil, ErrBusy
		}
	}
	return func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, lockID)
		conn.Release()
	}, nil
}
