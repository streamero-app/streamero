package library

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Watcher queues a library scan shortly after files in its folders change.
// Network shares often do not deliver events; the periodic scan covers them.
type Watcher struct {
	Pool     *pgxpool.Pool
	Roots    Roots
	Log      *slog.Logger
	Trigger  func(ctx context.Context, libraryID uuid.UUID) error
	Debounce time.Duration // quiet time after the last change (default 15s)
	Refresh  time.Duration // how often the library list is reloaded (default 1m)

	onWalk func(root string) // tests: called for every folder walk
}

type watchedLib struct {
	id    uuid.UUID
	roots []string
}

// Run watches until ctx ends.
func (w *Watcher) Run(ctx context.Context) error {
	if w.Debounce == 0 {
		w.Debounce = 15 * time.Second
	}
	if w.Refresh == 0 {
		w.Refresh = time.Minute
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer func() { _ = fsw.Close() }()

	var libs []watchedLib
	watched := map[string]bool{}
	walked := map[string]bool{} // library folders walked by reload
	limitWarned := false
	pending := map[uuid.UUID]time.Time{}

	add := func(root string) {
		if w.onWalk != nil {
			w.onWalk(root)
		}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if p != root && (IgnoredName(d.Name()) || IsExtraDir(d.Name())) {
				return filepath.SkipDir
			}
			if watched[p] {
				return nil
			}
			if err := fsw.Add(p); err != nil {
				if errors.Is(err, syscall.ENOSPC) && !limitWarned {
					limitWarned = true
					w.Log.Warn("inotify watch limit reached; relying on periodic scans (raise fs.inotify.max_user_watches)")
				}
				return nil
			}
			watched[p] = true
			return nil
		})
	}
	reload := func() {
		rows, err := w.Pool.Query(ctx, `SELECT id, paths FROM libraries`)
		if err != nil {
			w.Log.Warn("watcher: loading libraries failed", "err", err)
			return
		}
		var next []watchedLib
		for rows.Next() {
			var l watchedLib
			var paths []string
			if err := rows.Scan(&l.id, &paths); err != nil {
				continue
			}
			for _, p := range paths {
				if r, err := ResolveSafePath(w.Roots, p); err == nil {
					l.roots = append(l.roots, r)
				}
			}
			next = append(next, l)
		}
		rows.Close()
		libs = next
		keep := map[string]bool{}
		for _, l := range libs {
			for _, r := range l.roots {
				// Only folders that are new since the last reload are walked: the ones
				// already watched pick up their new sub-folders from Create events.
				// A root that could not be watched (offline) is tried again.
				if !walked[r] || !watched[r] {
					add(r)
					walked[r] = watched[r]
				}
				keep[r] = true
			}
		}
		for r := range walked {
			if !keep[r] {
				delete(walked, r)
			}
		}
		for p := range watched {
			inside := false
			for r := range keep {
				if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
					inside = true
					break
				}
			}
			if !inside {
				_ = fsw.Remove(p)
				delete(watched, p)
			}
		}
	}
	owner := func(path string) (uuid.UUID, bool) {
		for _, l := range libs {
			for _, r := range l.roots {
				if path == r || strings.HasPrefix(path, r+string(filepath.Separator)) {
					return l.id, true
				}
			}
		}
		return uuid.Nil, false
	}

	reload()
	refresh := time.NewTicker(w.Refresh)
	defer refresh.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-refresh.C:
			reload()
		case err := <-fsw.Errors:
			w.Log.Warn("watcher error", "err", err)
		case ev := <-fsw.Events:
			if IgnoredName(filepath.Base(ev.Name)) {
				continue
			}
			id, ok := owner(ev.Name)
			if !ok {
				continue
			}
			pending[id] = time.Now()
			if ev.Op.Has(fsnotify.Create) {
				add(ev.Name) // new folder: watch it (and anything copied into it)
			}
			if ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename) {
				delete(watched, ev.Name)
			}
		case now := <-tick.C:
			for id, last := range pending {
				if now.Sub(last) >= w.Debounce {
					delete(pending, id)
					if err := w.Trigger(ctx, id); err != nil {
						w.Log.Warn("watcher: queueing scan failed", "library", id, "err", err)
					} else {
						w.Log.Info("change detected, scan queued", "library", id)
					}
				}
			}
		}
	}
}
