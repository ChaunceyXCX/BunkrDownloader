// Command bunkr is the BunkrDownloader desktop application.
//
// It reuses the whole domain layer of the web build (store / auth / bunkr /
// aria2 / downloads) and exposes it to a Vue 3 frontend through Wails v3
// service bindings plus a Wails event bridge in place of the WebSocket.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/config"
	"github.com/chaunceyxie1/BunkrDownloader/internal/downloads"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/services"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// version is overridden at build time: -ldflags "-X main.version=1.2.3"
var version = "1.0.0"

// Wails embeds everything in frontend/dist into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-version" {
		fmt.Println(version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "bunkr: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	log := newLogger()
	cfg := config.LoadDesktop(version)
	services.Version = version

	log.Info("BunkrDownloader desktop starting",
		"version", version, "platform", runtime.GOOS+"/"+runtime.GOARCH,
		"dataDir", cfg.DataDir, "downloadDir", cfg.DownloadDir)

	// --- storage ------------------------------------------------------------
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	st.Configure(
		store.QuotaLimits{
			Links: cfg.FreeLinksLimit, Files: cfg.FreeFilesLimit, Concurrent: cfg.FreeConcurrency,
		},
		store.QuotaLimits{Links: -1, Files: -1, Concurrent: cfg.MemberConcurrency},
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

	// --- aria2 --------------------------------------------------------------
	// A missing engine degrades the app (accounts still work) rather than
	// preventing it from starting.
	ariaMgr := aria2.NewManager(aria2.Options{
		Logger:     log,
		BinaryPath: cfg.Aria2Binary,
		AutoFetch:  cfg.Aria2AutoFetch,
		Dir:        filepath.Join(cfg.DataDir, "aria2"),
		Secret:     cfg.Aria2Secret,
		Host:       "127.0.0.1",
		MaxConc:    16,
	})
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 90*time.Second)
	ariaErr := ariaMgr.Start(startupCtx)
	startupCancel()
	if ariaErr != nil {
		log.Error("aria2 failed to start; downloads are disabled", "error", ariaErr)
	} else {
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer stopCancel()
			_ = ariaMgr.Stop(stopCtx)
		}()
	}

	// --- orchestration ------------------------------------------------------
	mgr := downloads.New(downloads.Config{
		DownloadDir:       cfg.DownloadDir,
		FreeConcurrency:   cfg.FreeConcurrency,
		MemberConcurrency: cfg.MemberConcurrency,
		PollInterval:      time.Second,
		UserAgent:         cfg.UserAgent,
		DefaultOptions:    store.DefaultTaskOptions(),
	}, st, ariaMgr, events, log)
	mgr.Start(context.Background())
	defer mgr.Shutdown(context.Background())

	// --- services -----------------------------------------------------------
	appCtx := services.NewApp(st, issuer, mgr, events, log)
	appCtx.SetDataDir(cfg.DataDir)

	authSvc := services.NewAuthService(appCtx)
	taskSvc := services.NewTaskService(appCtx)
	memberSvc := services.NewMembershipService(appCtx)
	systemSvc := services.NewSystemService(appCtx, cfg.DownloadDir)

	// The asset handler serves from the root of the given FS, but //go:embed
	// keeps the "frontend/dist" prefix. Sub it so that "/" resolves to
	// index.html and "/assets/..." to the hashed bundles.
	distFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return fmt.Errorf("open embedded frontend: %w", err)
	}

	// --- wails application --------------------------------------------------
	app := application.New(application.Options{
		Name:        "BunkrDownloader",
		Description: "Bunkr 相册批量下载器 · aria2 引擎 · 账号与会员",
		Services: []application.Service{
			application.NewService(authSvc),
			application.NewService(taskSvc),
			application.NewService(memberSvc),
			application.NewService(systemSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(distFS),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Bridge the download manager's event stream onto Wails events. The frontend
	// subscribes with the same frame types the web build uses over WebSocket.
	removeSink := events.AddSink(func(userID, taskID int64, frame hub.Frame) {
		// The desktop app is single-user, but frames stay user-scoped so the
		// frontend can reuse the web store logic unchanged.
		app.Event.Emit("bunkr:"+frame.Type, map[string]any{
			"type": frame.Type, "ts": frame.TS, "data": frame.Data,
			"user_id": userID, "task_id": taskID,
		})
	})
	defer removeSink()

	// The window must be created through the application's window manager:
	// the package-level application.NewWindow returns a WebviewWindow that is
	// never registered with the app, so app.Run() would start an event loop
	// with no visible window.
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "BunkrDownloader",
		Width:     1360,
		Height:    900,
		MinWidth:  960,
		MinHeight: 640,
		// The embedded assets are served from the root of the handler.
		URL: "/",
		// A desktop build has no devtools unless explicitly enabled.
		DevToolsEnabled:  false,
		BackgroundColour: application.RGBA{Red: 11, Green: 14, Blue: 20, Alpha: 255},
		Mac: application.MacWindow{
			// Inset traffic lights with a transparent bar blend into the
			// dark sidebar of the UI.
			TitleBar: application.MacTitleBar{
				AppearsTransparent: true,
				HideTitle:          true,
				FullSizeContent:    true,
			},
			WindowClass: application.MacWindowClassWindow,
		},
		Windows: application.WindowsWindow{
			Theme:                   application.Dark,
			BackdropType:            application.None,
			WindowDidMoveDebounceMS: 16,
		},
	})

	app.OnShutdown(func() {
		log.Info("shutting down")
		removeSink()
		events.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		mgr.Shutdown(shutdownCtx)
	})

	log.Info("main window created", "id", window.ID())
	log.Info("starting the Wails event loop")
	if err := app.Run(); err != nil {
		return fmt.Errorf("wails run: %w", err)
	}
	return nil
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch os.Getenv("BUNKR_LOG_LEVEL") {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}
	// A windowed app has no console, so logs go to a file next to the database.
	dir := os.Getenv("BUNKR_DATA_DIR")
	if dir == "" {
		if home, err := os.UserConfigDir(); err == nil {
			dir = filepath.Join(home, "BunkrDownloader")
		}
	}
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		if f, err := os.OpenFile(filepath.Join(dir, "bunkr.log"),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level}))
		}
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
