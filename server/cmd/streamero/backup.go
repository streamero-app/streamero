package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/streamero-app/streamero/server/internal/backup"
	"github.com/streamero-app/streamero/server/internal/config"
	"github.com/streamero-app/streamero/server/internal/db"
	"github.com/streamero-app/streamero/server/internal/settings"
	"github.com/streamero-app/streamero/server/internal/version"
)

// errRestart ends serve/worker after a restore so the container starts them afresh.
var errRestart = errors.New("restart after restore")

// exitRestart is the exit code after a restore (non-zero so every restart policy applies).
const exitRestart = 75

// restoreKey is bumped in Valkey after every restore; running workers restart on a change.
const restoreKey = "streamero:restore:generation"

func newBackupService(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) *backup.Service {
	return &backup.Service{Pool: pool, Dir: cfg.BackupDir, ConfigDir: cfg.ConfigDir, CacheDir: cfg.CacheDir,
		Version: version.Version, Commit: version.Commit, Log: log}
}

func signalRestored(ctx context.Context, vk *redis.Client, log *slog.Logger) {
	if vk == nil {
		return
	}
	if err := vk.Incr(ctx, restoreKey).Err(); err != nil {
		log.Warn("could not tell the worker about the restore; restart it by hand", "err", err)
	}
}

// watchRestores closes the returned channel when another process restored a backup.
func watchRestores(ctx context.Context, vk *redis.Client, log *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	start, _ := vk.Get(ctx, restoreKey).Result()
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			v, err := vk.Get(ctx, restoreKey).Result()
			if err != nil && !errors.Is(err, redis.Nil) {
				continue
			}
			if v != start {
				log.Info("a backup was restored; restarting the worker")
				close(done)
				return
			}
		}
	}()
	return done
}

// backupCmd: streamero backup [--images]
func backupCmd(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	images := fs.Bool("images", false, "also archive cached artwork")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	svc := newBackupService(cfg, pool, log)
	info, err := svc.Create(ctx, backup.CreateOptions{Kind: backup.KindManual, IncludeImages: *images})
	if err != nil {
		return err
	}
	fmt.Println(filepath.Join(svc.Dir, info.Name))
	if info.Manifest.ScanRunning {
		fmt.Fprintln(os.Stderr, "warning: a library scan was running; the backup is consistent but may miss items being added")
	}
	_, _ = svc.Prune(backup.LoadSettings(ctx, settings.New(pool)).KeepLast)
	return nil
}

// restoreCmd: streamero restore [--yes] [--no-safety-backup] [--skip-files] <file or name>
func restoreCmd(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	noSafety := fs.Bool("no-safety-backup", false, "do not back up the current state first")
	skipFiles := fs.Bool("skip-files", false, "restore only the database, not config files, avatars or artwork")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: streamero restore [--yes] [--no-safety-backup] [--skip-files] <backup file or name>")
	}
	arg := fs.Arg(0)
	path := arg
	if _, err := os.Stat(path); err != nil && !strings.ContainsRune(arg, os.PathSeparator) {
		path = filepath.Join(cfg.BackupDir, arg) // a bare name from the backup folder
	}
	m, err := backup.Prepare(path)
	if err != nil {
		return err
	}
	fmt.Printf("Backup:   %s\nTaken:    %s by Streamero %s (schema %d)\nTables:   %d\n",
		path, m.CreatedAt.Format(time.RFC3339), m.ServerVersion, m.SchemaVersion, len(m.Tables))
	if len(m.Skipped) > 0 {
		fmt.Printf("Missing:  %s (unreadable when the backup was taken)\n", strings.Join(m.SkippedNames(), ", "))
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	var others int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid()`).Scan(&others)
	if others > 1 {
		fmt.Printf("Warning:  %d other database connections are open. Stop the streamero and worker containers first.\n", others)
	}
	fmt.Println("Everything in the database is replaced by this backup.")
	if !*yes {
		if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
			return errors.New("not a terminal: pass --yes to confirm the restore")
		}
		fmt.Print("Type RESTORE to continue: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != "RESTORE" {
			return errors.New("restore cancelled")
		}
	}
	svc := newBackupService(cfg, pool, log)
	st := backup.Status{Backup: filepath.Base(path), State: backup.StateRunning, StartedAt: time.Now().UTC()}
	_ = svc.WriteStatus(st)
	res, err := svc.Restore(ctx, path, backup.RestoreOptions{SafetyBackup: !*noSafety, SkipFiles: *skipFiles})
	st.Finish(res, err, time.Now().UTC())
	_ = svc.WriteStatus(st)
	if res.SafetyBackup != "" {
		fmt.Println("Safety backup of the previous state:", filepath.Join(svc.Dir, res.SafetyBackup))
	}
	for _, w := range res.Warnings {
		fmt.Println("Warning:", w)
	}
	if err != nil {
		return err
	}
	// A worker that kept running notices this and restarts.
	if opts, perr := redis.ParseURL(cfg.ValkeyURL); perr == nil {
		vk := redis.NewClient(opts)
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		signalRestored(pctx, vk, log)
		cancel()
		_ = vk.Close()
	}
	fmt.Println("Restore finished. Start (or restart) the streamero and worker containers.")
	return nil
}
