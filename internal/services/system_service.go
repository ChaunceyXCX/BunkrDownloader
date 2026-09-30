package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// SystemService exposes runtime information and desktop conveniences.
type SystemService struct {
	app       *App
	startedAt time.Time
	downloads string
	dataDir   string
}

// NewSystemService builds the binding for SystemService.
func NewSystemService(a *App, downloadDir string) *SystemService {
	return &SystemService{
		app:       a,
		startedAt: time.Now(),
		downloads: downloadDir,
		dataDir:   a.StorePath(),
	}
}

// Health mirrors GET /api/health.
type Health struct {
	Status        string    `json:"status"`
	Version       string    `json:"version"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	Platform      string    `json:"platform"`
	Aria2         Aria2Info `json:"aria2"`
}

// Aria2Info is the download engine status block.
type Aria2Info struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Binary    string `json:"binary,omitempty"`
}

// Health reports liveness plus the aria2 engine state.
func (s *SystemService) Health() *Health {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	available := s.app.Manager.Aria2Healthy(ctx)
	status := "ok"
	if !available {
		status = "degraded"
	}
	return &Health{
		Status:        status,
		Version:       Version,
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		Aria2: Aria2Info{
			Available: available,
			Version:   s.app.Manager.Aria2Version(ctx),
		},
	}
}

// Version is stamped at build time.
var Version = "1.0.0"

// Stats mirrors GET /api/stats.
type Stats struct {
	TotalTasks      int64              `json:"total_tasks"`
	Running         int64              `json:"running"`
	Pending         int64              `json:"pending"`
	Paused          int64              `json:"paused"`
	Completed       int64              `json:"completed"`
	Failed          int64              `json:"failed"`
	Canceled        int64              `json:"canceled"`
	TotalFiles      int64              `json:"total_files"`
	CompletedFiles  int64              `json:"completed_files"`
	DownloadedBytes int64              `json:"downloaded_bytes"`
	Speed           int64              `json:"speed"`
	ActiveFiles     int64              `json:"active_files"`
	Aria2           downloadsAria2Stat `json:"aria2"`
	Quota           *store.Quota       `json:"quota"`
}

type downloadsAria2Stat struct {
	Available     bool  `json:"available"`
	DownloadSpeed int64 `json:"download_speed"`
	Active        int   `json:"active"`
	Waiting       int   `json:"waiting"`
	Stopped       int   `json:"stopped"`
	NumOfFiles    int64 `json:"num_of_files"`
}

// Stats returns the dashboard counters.
func (s *SystemService) Stats(token string) (*Stats, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	st, err := s.app.Store.Stats(userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	quota, err := s.app.Store.Quota(userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	aria := s.app.Manager.Aria2Stats(ctx)

	return &Stats{
		TotalTasks: st.TotalTasks, Running: st.Running, Pending: st.Pending,
		Paused: st.Paused, Completed: st.Completed, Failed: st.Failed,
		Canceled: st.Canceled, TotalFiles: st.TotalFiles,
		CompletedFiles: st.CompletedFiles, DownloadedBytes: st.DownloadedBytes,
		Speed: st.Speed, ActiveFiles: st.ActiveFiles,
		Aria2: downloadsAria2Stat{
			Available: aria.Available, DownloadSpeed: aria.DownloadSpeed,
			Active: aria.Active, Waiting: aria.Waiting, Stopped: aria.Stopped,
			NumOfFiles: aria.NumOfFiles,
		},
		Quota: &quota,
	}, nil
}

// Settings describes the desktop app's configuration to the UI.
type Settings struct {
	DownloadDir string            `json:"download_dir"`
	Version     string            `json:"version"`
	Platform    string            `json:"platform"`
	Features    map[string]any    `json:"features"`
	Defaults    store.TaskOptions `json:"defaults"`
	QuotaLimits map[string]any    `json:"quota_limits"`
}

// Settings returns the non-secret runtime configuration.
func (s *SystemService) Settings() *Settings {
	return &Settings{
		DownloadDir: s.downloads,
		Version:     Version,
		Platform:    runtime.GOOS + "/" + runtime.GOARCH,
		Features: map[string]any{
			"aria2":   true,
			"payment": "mock",
			"desktop": true,
		},
		Defaults: store.DefaultTaskOptions(),
		QuotaLimits: map[string]any{
			"free": map[string]any{
				"links":      s.app.Store.FreeLimits().Links,
				"files":      s.app.Store.FreeLimits().Files,
				"concurrent": s.app.Store.FreeLimits().Concurrent,
			},
		},
	}
}

// SetDownloadDir changes where new downloads are stored and persists the
// choice so it survives a restart. Tasks that already resolved to a custom
// path keep it; existing default-path tasks keep the folder they were created
// with. Returns the refreshed settings so the UI can re-sync immediately.
func (s *SystemService) SetDownloadDir(token, path string) (*Settings, *APIError) {
	if _, apiErr := s.app.resolveUserID(token); apiErr != nil {
		return nil, apiErr
	}
	if err := s.app.Manager.SetDownloadDir(path); err != nil {
		return nil, &APIError{Code: CodeBadRequest, Message: err.Error()}
	}
	s.downloads = s.app.Manager.DownloadDir()
	prefs := LoadDesktopPrefs(s.dataDir)
	prefs.DownloadDir = s.downloads
	if err := prefs.save(s.dataDir); err != nil {
		s.app.Log.Warn("persist download dir", "error", err)
	}
	s.app.Log.Info("download dir changed", "dir", s.downloads)
	return s.Settings(), nil
}

// OpenDownloadDir reveals the download folder in the OS file manager.
// This is a desktop-only affordance the web build cannot offer.
func (s *SystemService) OpenDownloadDir() (bool, string) {
	dir := s.downloads
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, "无法创建目录: " + err.Error()
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// explorer returns a non-zero exit code even on success.
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return false, "无法打开目录: " + err.Error()
	}
	return true, dir
}

// RevealPath shows a specific file or folder, falling back to the parent dir.
func (s *SystemService) RevealPath(path string) (bool, string) {
	if path == "" {
		return s.OpenDownloadDir()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err.Error()
	}
	if _, err := os.Stat(abs); err != nil {
		// The file is gone (moved, renamed or already cleaned up). The most
		// useful fallback is to show where downloads land.
		if ok, msg := s.OpenDownloadDir(); ok {
			return true, s.downloads
		} else {
			return false, msg
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,", abs)
	case "darwin":
		cmd = exec.Command("open", "-R", abs)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(abs))
	}
	if err := cmd.Start(); err != nil {
		return false, "无法定位文件: " + err.Error()
	}
	return true, abs
}

// Aria2Restart forces a fresh aria2c process. Useful after a manual binary
// upgrade or when the engine reports itself unhealthy.
func (s *SystemService) Aria2Restart() (bool, string) {
	if !s.app.Manager.RestartAria2(context.Background()) {
		return false, "aria2 重启失败，请检查日志"
	}
	return true, "aria2 已重启"
}

// DataDir reports where the database and the aria2 session live.
func (s *SystemService) DataDir() string { return s.app.StorePath() }

// Aria2BinaryPath reports the resolved aria2c executable, which is the value
// the settings screen should show.
func (s *SystemService) Aria2BinaryPath() string { return resolveAria2ForDisplay() }

func resolveAria2ForDisplay() string {
	name := "aria2c"
	if runtime.GOOS == "windows" {
		name = "aria2c.exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if _, serr := os.Stat(candidate); serr == nil {
			return candidate
		}
	}
	return ""
}
