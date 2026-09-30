// Command bunkr-web is the BunkrDownloader web control panel.
//
// It serves the embedded Vue SPA, a REST API and a WebSocket stream, and it
// supervises an aria2c process that performs every actual download.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/api"
	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/config"
	"github.com/chaunceyxie1/BunkrDownloader/internal/downloads"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
	"github.com/chaunceyxie1/BunkrDownloader/internal/web"
)

// version is overridden at build time:
//
//	go build -ldflags "-X main.version=1.2.3"
var version = "1.0.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg := config.Load(version)
	log := newLogger()

	banner(cfg, log)

	// --- storage ---
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	st.Configure(
		store.QuotaLimits{
			Links:      cfg.FreeLinksLimit,
			Files:      cfg.FreeFilesLimit,
			Concurrent: cfg.FreeConcurrency,
		},
		store.QuotaLimits{
			Links:      -1, // members are unlimited
			Files:      -1,
			Concurrent: cfg.MemberConcurrency,
		},
	)
	if err := st.EnsureRedeemCodes(); err != nil {
		log.Warn("seed redeem codes", "error", err)
	}
	if n, err := st.RequeueInterrupted(); err != nil {
		log.Warn("requeue interrupted tasks", "error", err)
	} else if n > 0 {
		log.Info("interrupted tasks requeued", "count", n, "state", store.TaskPaused)
	}

	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.JWTTTL)
	events := hub.New(log, 4096)

	// --- aria2 ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ariaMgr *aria2.Manager
	if cfg.Aria2Enabled {
		ariaMgr = aria2.NewManager(aria2.Options{
			Logger:     log,
			BinaryPath: cfg.Aria2Binary,
			AutoFetch:  cfg.Aria2AutoFetch,
			Dir:        filepath.Join(cfg.DataDir, "aria2"),
			Secret:     cfg.Aria2Secret,
			Host:       cfg.Aria2Host,
			MaxConc:    cfg.Aria2MaxConc,
		})
		// A download engine failure degrades the service but must not abort
		// startup: the HTTP API stays available for auth and task management.
		if err := ariaMgr.Start(ctx); err != nil {
			log.Error("aria2 failed to start; downloads are disabled", "error", err)
		} else {
			defer func() {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer stopCancel()
				_ = ariaMgr.Stop(stopCtx)
			}()
		}
	} else {
		ariaMgr = aria2.NewManager(aria2.Options{Logger: log, Dir: filepath.Join(cfg.DataDir, "aria2")})
		log.Warn("aria2 is disabled by configuration")
	}

	// --- orchestration ---
	mgr := downloads.New(downloads.Config{
		DownloadDir:        cfg.DownloadDir,
		FreeConcurrency:    cfg.FreeConcurrency,
		MemberConcurrency:  cfg.MemberConcurrency,
		ResolveConcurrency: runtime.NumCPU(),
		PollInterval:       time.Second,
		UserAgent:          userAgent(),
		DefaultOptions:     store.DefaultTaskOptions(),
	}, st, ariaMgr, events, log)
	mgr.Start(ctx)
	defer mgr.Shutdown(context.Background())

	// --- http ---
	static, err := web.FS()
	if err != nil {
		return fmt.Errorf("load embedded frontend: %w", err)
	}
	if !web.HasBuild() {
		log.Warn("no frontend bundle embedded; serving placeholder page " +
			"(run `make web` to build it)")
	}

	srv := api.NewServer(api.Deps{
		Cfg: cfg, Store: st, Issuer: issuer, Hub: events,
		Manager: mgr, Log: log, StaticFS: static, Started: time.Now(),
	})

	// --- signals ---
	sigCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(sigCtx) }()

	// Periodic housekeeping: bound the event table.
	go housekeeping(sigCtx, st, log)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	case <-sigCtx.Done():
		log.Info("shutdown signal received")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()
	events.Close()
	mgr.Shutdown(shutdownCtx)
	return nil
}

// housekeeping trims the event log so the database cannot grow without bound.
func housekeeping(ctx context.Context, st *store.Store, log *slog.Logger) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := st.TrimEvents(5000); err != nil {
				log.Warn("trim events", "error", err)
			}
		}
	}
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToUpper(os.Getenv("BUNKR_LOG_LEVEL")) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	if isTerminal() {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func banner(cfg *config.Config, log *slog.Logger) {
	ui := "ok"
	if !web.HasBuild() {
		ui = "placeholder (frontend not built)"
	}
	log.Info("BunkrDownloader starting",
		"version", cfg.Version,
		"addr", cfg.Addr(),
		"dataDir", cfg.DataDir,
		"downloadDir", cfg.DownloadDir,
		"db", cfg.DBPath,
		"frontend", ui,
		"freeQuota", fmt.Sprintf("%d links / %d files", cfg.FreeLinksLimit, cfg.FreeFilesLimit),
	)
}

// userAgent mirrors the browser identity used by the original downloader.
func userAgent() string {
	if v := strings.TrimSpace(os.Getenv("BUNKR_USER_AGENT")); v != "" {
		return v
	}
	return "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:136.0) Gecko/20100101 Firefox/136.0"
}
