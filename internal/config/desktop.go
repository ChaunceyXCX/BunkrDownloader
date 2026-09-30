package config

import (
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// DesktopConfig is the runtime configuration of the Wails desktop build.
//
// The web build reads everything from the environment; a desktop app has no
// shell to export variables, so the paths are derived from the OS conventions
// and the few tunables are still overridable through the environment for
// debugging.
type DesktopConfig struct {
	DataDir     string
	DownloadDir string
	DBPath      string
	JWTSecret   string
	JWTTTL      time.Duration

	FreeLinksLimit    int
	FreeFilesLimit    int
	FreeConcurrency   int
	MemberConcurrency int

	Aria2Binary    string
	Aria2AutoFetch bool
	Aria2Secret    string
	PaymentAuto    bool
	UserAgent      string

	Version string
}

// LoadDesktop builds the desktop configuration.
//
// Paths follow each platform's convention:
//   - Windows: %APPDATA%\BunkrDownloader, downloads in %USERPROFILE%\Downloads
//   - macOS:   ~/Library/Application Support/BunkrDownloader
//   - Linux:   $XDG_CONFIG_HOME/bunkrdownloader (or ~/.config/...)
func LoadDesktop(version string) *DesktopConfig {
	dataDir := envStr("BUNKR_DATA_DIR", defaultDesktopDataDir())
	downloads := envStr("BUNKR_DOWNLOAD_DIR", defaultDesktopDownloadDir())

	c := &DesktopConfig{
		DataDir:     dataDir,
		DownloadDir: downloads,
		DBPath:      envStr("BUNKR_DB", filepath.Join(dataDir, "bunkr.db")),

		JWTSecret: envStr("BUNKR_JWT_SECRET", ""),
		JWTTTL:    envDuration("BUNKR_JWT_TTL", 30*24*time.Hour),

		FreeLinksLimit:    envInt("BUNKR_FREE_LINKS_LIMIT", 5),
		FreeFilesLimit:    envInt("BUNKR_FREE_FILES_LIMIT", 50),
		FreeConcurrency:   envInt("BUNKR_FREE_CONCURRENCY", 1),
		MemberConcurrency: envInt("BUNKR_MEMBER_CONCURRENCY", 5),

		Aria2Binary:    envStr("BUNKR_ARIA2_BIN", ""),
		Aria2AutoFetch: envBool("BUNKR_ARIA2_AUTO_FETCH", true),
		Aria2Secret:    envStr("BUNKR_ARIA2_SECRET", "bunkr-desktop"),
		PaymentAuto:    envBool("BUNKR_PAYMENT_AUTO", true),

		UserAgent: envStr("BUNKR_USER_AGENT", defaultUserAgent),
		Version:   version,
	}

	// aria2c shipped next to the executable wins over anything else.
	if c.Aria2Binary == "" {
		if exe, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(exe), aria2ExeName())
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
				c.Aria2Binary = candidate
			}
		}
	}

	if c.JWTSecret == "" {
		c.JWTSecret = loadOrCreateSecret(filepath.Join(dataDir, ".jwt_secret"))
	}
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(downloads, 0o755)
	return c
}

func aria2ExeName() string {
	if runtime.GOOS == "windows" {
		return "aria2c.exe"
	}
	return "aria2c"
}

func defaultDesktopDataDir() string {
	appName := "BunkrDownloader"
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil && dir != "" {
			return filepath.Join(dir, appName)
		}
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "bunkrdownloader")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".bunkr_downloader"
	}
	return filepath.Join(home, ".config", "bunkrdownloader")
}

func defaultDesktopDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "Downloads/BunkrDownloader"
	}
	// Prefer the platform's Downloads folder when it exists.
	candidates := []string{
		filepath.Join(home, "Downloads", "BunkrDownloader"),
		filepath.Join(home, "下载", "BunkrDownloader"),
		filepath.Join(home, "Desktop", "BunkrDownloader"),
	}
	for _, dir := range candidates {
		if fi, err := os.Stat(filepath.Dir(dir)); err == nil && fi.IsDir() {
			return dir
		}
	}
	return candidates[0]
}

// DefaultUserAgent is the browser identity used when fetching Bunkr pages.
const defaultUserAgent = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:136.0) Gecko/20100101 Firefox/136.0"
