// Command streamero is the Streamero media server.
//
// Subcommands: serve | worker | migrate | backup | restore | healthcheck | version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/streamero-app/streamero/server/internal/activity"
	"github.com/streamero-app/streamero/server/internal/analyze"
	"github.com/streamero-app/streamero/server/internal/api"
	"github.com/streamero-app/streamero/server/internal/app"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/backup"
	"github.com/streamero-app/streamero/server/internal/compat"
	"github.com/streamero-app/streamero/server/internal/config"
	"github.com/streamero-app/streamero/server/internal/db"
	"github.com/streamero-app/streamero/server/internal/discovery"
	"github.com/streamero-app/streamero/server/internal/dlna"
	"github.com/streamero-app/streamero/server/internal/downloads"
	"github.com/streamero-app/streamero/server/internal/importer"
	"github.com/streamero-app/streamero/server/internal/jobs"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/livetv"
	"github.com/streamero-app/streamero/server/internal/logbuf"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/netx"
	"github.com/streamero-app/streamero/server/internal/notify"
	"github.com/streamero-app/streamero/server/internal/settings"
	"github.com/streamero-app/streamero/server/internal/storage"
	"github.com/streamero-app/streamero/server/internal/stream"
	"github.com/streamero-app/streamero/server/internal/syncplay"
	"github.com/streamero-app/streamero/server/internal/version"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = withConfig(ctx, serve)
	case "worker":
		err = withConfig(ctx, worker)
	case "migrate":
		err = withConfig(ctx, migrate)
	case "backup":
		err = withConfig(ctx, backupCmd)
	case "restore":
		err = withConfig(ctx, restoreCmd)
	case "healthcheck":
		err = healthcheck()
	case "version":
		fmt.Printf("streamero %s (%s)\n", version.Version, version.Commit)
	default:
		err = fmt.Errorf("unknown command %q (want serve|worker|migrate|backup|restore|healthcheck|version)", cmd)
	}
	if errors.Is(err, errRestart) {
		// A restore replaced the database: exit so the container restarts with fresh state.
		fmt.Fprintln(os.Stderr, "restarting after a backup restore")
		os.Exit(exitRestart)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// logs keeps recent records for the admin log viewer (shared through Valkey).
var logs *logbuf.Buffer

func newLogger(level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(logs.Handler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})))
}

func withConfig(ctx context.Context, fn func(context.Context, config.Config, *slog.Logger) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	source := "serve"
	if len(os.Args) > 1 {
		source = os.Args[1]
	}
	logs = logbuf.New(source)
	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)
	return fn(ctx, cfg, log)
}

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	for _, dir := range []string{cfg.CacheDir, cfg.TranscodeDir} {
		if err := storage.EnsureCacheDir(dir); err != nil {
			// Not fatal: local dev runs often have no /cache. Features needing it will fail loudly.
			log.Warn("cache dir unavailable", "dir", dir, "err", err)
		}
	}
	setupCode := cfg.SetupCode
	if setupCode == "" {
		var err error
		if setupCode, err = auth.NewSetupCode(); err != nil {
			return err
		}
	}
	state := &app.State{SetupCode: setupCode}
	roots, err := mediaRoots(cfg, log)
	if err != nil {
		return err
	}
	h := &api.Handler{State: state, ServerName: cfg.ServerName, StartedAt: time.Now().UTC(),
		Auth: state.Auth, Settings: state.Settings, Library: state.Library, Stream: state.Stream, LiveTV: state.LiveTV,
		NewTMDb:    func(key string) *metadata.TMDb { return &metadata.TMDb{Key: key} },
		NowPlaying: &activity.Registry{}, Logs: logs,
		Paths: api.Paths{Media: roots, Cache: cfg.CacheDir, Transcode: cfg.TranscodeDir}}
	h.Notify = &notify.Dispatcher{Settings: state.Settings, Log: log, Server: h.ServerNameFor}
	h.SyncPlay = &syncplay.Service{Hub: syncplay.NewHub(), Lookup: h.SyncLookup}
	go h.SyncPlay.Run(ctx)
	proxies, err := netx.ParsePrefixes(cfg.TrustedProxies)
	if err != nil {
		return err
	}
	opts := app.RouterOptions{TrustedProxies: proxies, NoCompatAtRoot: !cfg.CompatAtRoot}
	if cfg.AuthRatePerMin > 0 {
		opts.AuthLimiter = netx.NewLimiter(cfg.AuthRatePerMin, cfg.AuthRateBurst)
	}
	router, err := app.NewRouter(state, h, opts)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: router, ReadHeaderTimeout: 10 * time.Second}

	// Listen immediately so health checks answer 503 while dependencies come up.
	errc := make(chan error, 1)
	restartc := make(chan struct{}, 1)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr, "version", version.Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	deps, err := app.Bootstrap(ctx, cfg, state, log)
	if err == nil {
		defer deps.Close()
		logs.Attach(ctx, deps.Valkey)
		// Insert-only River client: the worker process runs the jobs.
		inserter, ierr := river.NewClient(riverpgxv5.New(deps.Pool), &river.Config{})
		if ierr != nil {
			return ierr
		}
		key, kerr := stream.LoadKey(ctx, state.Settings())
		if kerr != nil {
			return kerr
		}
		hls := &stream.Manager{Dir: cfg.TranscodeDir, FFmpeg: cfg.FFmpegPath, Log: log, Encoding: h.LoadEncoding}
		go hls.Reap(ctx)
		streamServer := &stream.Server{Pool: deps.Pool, Roots: roots, Signer: stream.Signer{Key: key}, HLS: hls, FFmpeg: cfg.FFmpegPath,
			PhotoCache: filepath.Join(cfg.CacheDir, "photos")}
		state.SetStream(streamServer)
		libs := &library.Service{Pool: deps.Pool, Roots: roots, ImageDir: filepath.Join(cfg.CacheDir, "images"), Resume: h.LoadResume,
			Enqueue: func(ctx context.Context, id uuid.UUID) error {
				_, err := inserter.Insert(ctx, jobs.LibraryScanArgs{LibraryID: id}, nil)
				return err
			},
			EnqueueMetadata: func(ctx context.Context, id uuid.UUID, force bool) error {
				_, err := inserter.Insert(ctx, jobs.MetadataRefreshArgs{LibraryID: id, Force: force}, nil)
				return err
			}}
		h.RunTaskFn = func(ctx context.Context, key string) error {
			var args river.JobArgs
			switch key {
			case "scan_all":
				args = jobs.LibraryScanAllArgs{}
			case "refresh_metadata":
				args = jobs.MetadataRefreshAllArgs{}
			case "cleanup_images":
				args = jobs.ImagesCleanupArgs{}
			case "analyze_media":
				args = jobs.MediaAnalyzeAllArgs{}
			case "detect_segments":
				args = jobs.SegmentDetectAllArgs{}
			case "backup":
				args = jobs.BackupArgs{BackupKind: backup.KindManual}
			case "refresh_guide":
				args = jobs.GuideRefreshDueArgs{}
			default:
				return fmt.Errorf("unknown task %q", key)
			}
			_, err := inserter.Insert(ctx, args, nil)
			return err
		}
		h.EnqueueImport = func(ctx context.Context, id uuid.UUID) error {
			_, err := inserter.Insert(ctx, jobs.ImportArgs{RunID: id}, nil)
			return err
		}
		state.SetLibrary(libs)
		// Offline downloads (D-040): the worker transcodes into the shared cache, serve hands out the files.
		dl := &downloads.Service{Pool: deps.Pool, Library: libs, Roots: roots, Signer: stream.Signer{Key: key},
			Dir: filepath.Join(cfg.CacheDir, "downloads"), Settings: state.Settings(),
			Enqueue: func(ctx context.Context, id uuid.UUID) error {
				_, err := inserter.Insert(ctx, jobs.DownloadTranscodeArgs{TranscodeID: id}, nil)
				return err
			}}
		streamServer.Downloads = dl
		h.Downloads.Store(dl)
		tv := newLiveTV(cfg, deps.Pool, state.Settings, roots, log)
		tv.Signer, tv.HLS, tv.Library = streamServer.Signer, hls, libs
		tv.EnqueueGuideRefresh = func(ctx context.Context, id uuid.UUID) error {
			_, err := inserter.Insert(ctx, jobs.GuideRefreshArgs{GuideID: id}, nil)
			return err
		}
		streamServer.Images = tv.ImageHandler()
		state.SetLiveTV(tv)
		go tv.Run(ctx)
		valkey := deps.Valkey
		backups := newBackupService(cfg, deps.Pool, log)
		backups.RecoverStatus(ctx)
		h.Backup.Store(&api.Backups{Service: backups,
			Enqueue: func(ctx context.Context) error {
				_, err := inserter.Insert(ctx, jobs.BackupArgs{BackupKind: backup.KindManual}, nil)
				return err
			},
			Restoring: state.SetRestoring,
			AfterRestore: func() {
				signalRestored(context.Background(), valkey, log)
				select {
				case restartc <- struct{}{}:
				default:
				}
			}})
		state.SetCompat(&compat.Server{Pool: deps.Pool, Auth: state.Auth(), Libraries: libs, Stream: state.Stream(),
			ServerID: state.ServerID(), Log: log, ServerName: h.ServerNameFor, OnPlayback: h.TrackPlayback, OnProgress: h.TrackProgress,
			Commands: h.NowPlaying, SyncPlay: h.SyncPlay, SyncIdentity: h.SyncIdentity, LiveTV: tv})
		if cfg.Discovery {
			startDiscovery(ctx, cfg, state.ServerID(), h.ServerNameFor, log)
		}
		if cfg.DLNA {
			d := &dlna.Server{Pool: deps.Pool, Auth: state.Auth(), Library: libs, Stream: state.Stream(), Settings: state.Settings,
				ServerID: state.ServerID(), ServerName: h.ServerNameFor, Encoding: h.LoadEncoding, FFmpeg: cfg.FFmpegPath,
				Version: version.Version, Log: log, NowPlaying: h.NowPlaying, BaseURL: cfg.DLNAURL, HTTPPort: discovery.PortOf(cfg.DLNAHTTPAddr)}
			h.DLNA.Store(d)
			d.Run(ctx, cfg.DLNAHTTPAddr, cfg.DLNASSDPAddr)
		}
		log.Info("ready", "server_id", state.ServerID(), "startup", time.Since(h.StartedAt).String())
		if !state.SetupCompleted(ctx) {
			// Printed on purpose: whoever can read the server log may create the first admin.
			log.Warn("setup required: open the web UI and enter this setup code", "setup_code", setupCode)
		}
	}

	restart := false
	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	case <-restartc:
		restart = true
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if restart {
		return errRestart
	}
	return nil
}

func worker(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	// The worker never runs migrations itself: it waits for serve/migrate.
	cfg.AutoMigrate = false
	state := &app.State{}
	deps, err := app.Bootstrap(ctx, cfg, state, log)
	if err != nil {
		return err
	}
	defer deps.Close()
	logs.Attach(ctx, deps.Valkey)

	roots, err := mediaRoots(cfg, log)
	if err != nil {
		return err
	}
	// Finished scan/refresh jobs from older versions still carry a uniqueness bitmask that
	// includes "completed" and would block new ones until River cleans them (~24 h).
	if _, err := deps.Pool.Exec(ctx, `UPDATE river_job SET unique_key = NULL
		WHERE unique_key IS NOT NULL AND state IN ('completed', 'cancelled', 'discarded')
			AND kind IN ('library.scan', 'metadata.refresh_library')`); err != nil {
		log.Warn("could not release finished unique jobs", "err", err)
	}
	// The library service queues scans through this worker's own River client (set below).
	var client *river.Client[pgx.Tx]
	libraries := &library.Service{Pool: deps.Pool, Roots: roots, Enqueue: func(ctx context.Context, id uuid.UUID) error {
		_, err := client.Insert(ctx, jobs.LibraryScanArgs{LibraryID: id}, nil)
		return err
	}}
	workers := river.NewWorkers()
	refresher := &metadata.Refresher{Pool: deps.Pool, Settings: state.Settings(), ImageDir: filepath.Join(cfg.CacheDir, "images"), Log: log,
		FFmpeg: cfg.FFmpegPath, ResolvePath: func(p string) (string, error) { return library.ResolveSafePath(roots, p) }}
	notifier := &notify.Dispatcher{Settings: state.Settings, Log: log, Server: func(ctx context.Context) string {
		var name string
		if ok, _ := state.Settings().Get(ctx, settings.KeyServerName, &name); ok && name != "" {
			return name
		}
		return cfg.ServerName
	}}
	jobs.Register(workers, log, &library.Scanner{Pool: deps.Pool, Roots: roots, Probe: library.FFprobe(cfg.FFprobePath), Log: log},
		libraries, refresher, func(ctx context.Context, id uuid.UUID) error {
			_, err := client.Insert(ctx, jobs.MetadataRefreshArgs{LibraryID: id}, nil)
			return err
		}, notifier,
		&analyze.Analyzer{Pool: deps.Pool, FFmpeg: cfg.FFmpegPath, FFprobe: cfg.FFprobePath, CacheDir: cfg.CacheDir, Trickplay: true, Log: log,
			ResolvePath: func(p string) (string, error) { return library.ResolveSafePath(roots, p) }},
		func(ctx context.Context, id uuid.UUID) error {
			_, err := client.Insert(ctx, jobs.MediaAnalyzeArgs{LibraryID: id}, nil)
			return err
		},
		&analyze.Detector{Pool: deps.Pool, FFmpeg: cfg.FFmpegPath, Fpcalc: cfg.FpcalcPath, Log: log,
			ResolvePath: func(p string) (string, error) { return library.ResolveSafePath(roots, p) }},
		func(ctx context.Context, id uuid.UUID) error {
			_, err := client.Insert(ctx, jobs.SegmentDetectArgs{LibraryID: id}, nil)
			return err
		})
	jobs.RegisterImport(workers, &importer.Runner{Pool: deps.Pool, Log: log})
	transcoder := &downloads.Transcoder{Pool: deps.Pool, Roots: roots, FFmpeg: cfg.FFmpegPath, Dir: filepath.Join(cfg.CacheDir, "downloads"),
		Settings: state.Settings(), Log: log, Enqueue: func(ctx context.Context, id uuid.UUID) error {
			_, err := client.Insert(ctx, jobs.DownloadTranscodeArgs{TranscodeID: id}, nil)
			return err
		}}
	jobs.RegisterDownloads(workers, transcoder)
	jobs.RegisterLiveTV(workers, newLiveTV(cfg, deps.Pool, state.Settings, roots, log))
	backups := newBackupService(cfg, deps.Pool, log)
	jobs.RegisterBackup(workers, log, backups, state.Settings(), func(ctx context.Context) error {
		_, err := client.Insert(ctx, jobs.BackupArgs{BackupKind: backup.KindScheduled}, nil)
		return err
	})
	client, err = river.NewClient(riverpgxv5.New(deps.Pool), &river.Config{
		Logger:  log,
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 10}},
		Workers: workers,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(time.Minute),
				func() (river.JobArgs, *river.InsertOpts) { return jobs.HeartbeatArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true}),
			river.NewPeriodicJob(river.PeriodicInterval(6*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return jobs.LibraryScanAllArgs{}, nil }, nil),
			river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return jobs.ImagesCleanupArgs{}, nil }, nil),
			river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return jobs.MediaAnalyzeAllArgs{}, nil }, nil),
			river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return jobs.SegmentDetectAllArgs{}, nil }, nil),
			river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return jobs.BackupScheduleArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true}),
			river.NewPeriodicJob(river.PeriodicInterval(15*time.Minute),
				func() (river.JobArgs, *river.InsertOpts) { return jobs.DownloadMaintainArgs{}, nil }, nil),
			river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return jobs.GuideRefreshDueArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true}),
		},
	})
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return err
	}
	log.Info("worker started")
	// This is the only worker: transcodes that were running when it stopped start again.
	if err := transcoder.Recover(ctx, 0, true); err != nil {
		log.Warn("could not resume download transcodes", "err", err)
	}
	restored := watchRestores(ctx, deps.Valkey, log)
	go func() {
		w := &library.Watcher{Pool: deps.Pool, Roots: roots, Log: log, Trigger: libraries.Scan}
		if err := w.Run(ctx); err != nil {
			log.Warn("file watcher stopped; periodic scans still run", "err", err)
		}
	}()
	restart := false
	select {
	case <-ctx.Done():
	case <-restored:
		restart = true
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	err = client.Stop(stopCtx)
	if restart {
		return errRestart
	}
	return err
}

func migrate(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	log.Info("migrations applied")
	return nil
}

func healthcheck() error {
	addr := os.Getenv("STREAMERO_HTTP_ADDR")
	if addr == "" {
		addr = ":8787"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 3 * time.Second}
	// Address comes from our own env and always targets the local process.
	resp, err := c.Get("http://" + addr + "/healthz") //nolint:gosec // G704: loopback health probe
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: %s", resp.Status)
	}
	return nil
}

// startDiscovery answers media clients' LAN discovery broadcast (D-033). A port
// that cannot be bound (another server answers there) is logged, not fatal.
func startDiscovery(ctx context.Context, cfg config.Config, id uuid.UUID, name func(context.Context) string, log *slog.Logger) {
	r := &discovery.Responder{URL: cfg.DiscoveryURL, HTTPPort: discovery.PortOf(cfg.HTTPAddr), Log: log,
		Info: func(ctx context.Context) (string, string) { return compat.ID(id), name(ctx) }}
	go func() {
		log.Info("answering client discovery", "addr", cfg.DiscoveryAddr)
		if err := r.ListenAndServe(ctx, cfg.DiscoveryAddr); err != nil {
			log.Warn("client discovery unavailable", "addr", cfg.DiscoveryAddr, "err", err)
		}
	}()
}

// mediaRoots are the configured media folders plus the recordings folder (the
// recordings library lives there, D-041).
func mediaRoots(cfg config.Config, log *slog.Logger) (library.Roots, error) {
	roots, err := library.ParseRoots(cfg.MediaRoots)
	if err != nil {
		return nil, err
	}
	if cfg.RecordingsDir == "" || !filepath.IsAbs(cfg.RecordingsDir) {
		return roots, nil
	}
	if err := os.MkdirAll(cfg.RecordingsDir, 0o750); err != nil {
		log.Warn("recordings folder unavailable; recordings cannot be made", "dir", cfg.RecordingsDir, "err", err)
		return roots, nil
	}
	return append(roots, filepath.Clean(cfg.RecordingsDir)), nil
}

// newLiveTV builds the Live TV service; serve adds the stream parts and runs the DVR.
func newLiveTV(cfg config.Config, pool *pgxpool.Pool, st func() *settings.Store, roots library.Roots, log *slog.Logger) *livetv.Service {
	// Playlist and guide files may come from the media folders or <config>/livetv.
	files := append(library.Roots{}, roots...)
	if dir := filepath.Join(cfg.ConfigDir, "livetv"); os.MkdirAll(dir, 0o750) == nil {
		files = append(files, dir)
	}
	return &livetv.Service{Pool: pool, Settings: st, FFmpeg: cfg.FFmpegPath, FFprobe: cfg.FFprobePath, FileRoots: files,
		RecordingsDir: filepath.Clean(cfg.RecordingsDir), HTTP: livetv.NewHTTPClient(2*time.Minute, false),
		ImageDir: filepath.Join(cfg.CacheDir, "livetv-images"), Log: log}
}
