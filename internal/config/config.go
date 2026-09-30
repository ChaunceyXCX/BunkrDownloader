// Package config loads runtime configuration from environment variables.
//
// Every value has a sane default so the binary runs with zero configuration.
// A .env file in the working directory is loaded automatically when present.
package config

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// big returns v as an int so the modulo below never divides by a typed zero.
func big(v int) int { return v }

// Config holds every tunable of the server.
type Config struct {
	// Server
	Host string
	Port int

	// Storage
	DataDir     string // SQLite database + aria2 runtime state live here
	DownloadDir string // where downloaded files are written
	DBPath      string

	// Auth
	JWTSecret string
	JWTTTL    time.Duration

	// Free-tier quotas
	FreeLinksLimit  int
	FreeFilesLimit  int
	FreeConcurrency int

	// Member
	MemberConcurrency int

	// Aria2
	Aria2Enabled   bool
	Aria2Host      string // bind address for aria2c RPC
	Aria2Port      int
	Aria2Secret    string
	Aria2Binary    string // explicit path to aria2c; empty = auto-resolve/download
	Aria2AutoFetch bool   // download aria2c when not found on PATH
	Aria2MaxConc   int    // global max simultaneous downloads

	// Payments (mock gateway)
	PaymentAuto bool

	// Bunkr service endpoints (overridable for mirrors and tests)
	SignAPI     string
	DownloadAPI string
	Referer     string

	// Misc
	Version     string
	AllowOrigin string
}

// Free tier constants (documented in docs/API.md).
const (
	FreePlanName = "free"
	MemberPlan   = "member"
	Unlimited    = -1
)

// Load builds a Config from the environment, loading .env first.
func Load(version string) *Config {
	loadDotEnv()

	dataDir := envStr("BUNKR_DATA_DIR", defaultDataDir())
	downloadDir := envStr("BUNKR_DOWNLOAD_DIR", filepath.Join(dataDir, "downloads"))

	c := &Config{
		Host:    envStr("BUNKR_HOST", "0.0.0.0"),
		Port:    envInt("BUNKR_PORT", 8765),
		DataDir: dataDir,

		DownloadDir: downloadDir,
		DBPath:      envStr("BUNKR_DB", filepath.Join(dataDir, "bunkr.db")),

		JWTSecret: envStr("BUNKR_JWT_SECRET", ""),
		JWTTTL:    envDuration("BUNKR_JWT_TTL", 30*24*time.Hour),

		FreeLinksLimit: envInt("BUNKR_FREE_LINKS_LIMIT", 5),
		FreeFilesLimit: envInt("BUNKR_FREE_FILES_LIMIT", 50),
		// Concurrency is unlimited by default (0). Only link count and file
		// (download) count gate a free account; simultaneous jobs are not
		// throttled. Set BUNKR_*_CONCURRENCY to a positive number to re-enable a cap.
		FreeConcurrency: envInt("BUNKR_FREE_CONCURRENCY", 0),

		MemberConcurrency: envInt("BUNKR_MEMBER_CONCURRENCY", 0),

		Aria2Enabled:   envBool("BUNKR_ARIA2_ENABLED", true),
		Aria2Host:      envStr("BUNKR_ARIA2_HOST", "127.0.0.1"),
		Aria2Port:      envInt("BUNKR_ARIA2_PORT", 6800),
		Aria2Secret:    envStr("BUNKR_ARIA2_SECRET", "bunkr"),
		Aria2Binary:    envStr("BUNKR_ARIA2_BIN", ""),
		Aria2AutoFetch: envBool("BUNKR_ARIA2_AUTO_FETCH", true),
		Aria2MaxConc:   envInt("BUNKR_ARIA2_MAX_CONCURRENT", 16),

		PaymentAuto: envBool("BUNKR_PAYMENT_AUTO", true),

		SignAPI:     envStr("BUNKR_SIGN_API", "https://glb-apisign.cdn.cr/sign"),
		DownloadAPI: envStr("BUNKR_DOWNLOAD_API", "https://dl.bunkr.cr/api/_001_v2"),
		Referer:     envStr("BUNKR_DOWNLOAD_REFERER", "https://dl.bunkrr.cr/"),

		Version:     version,
		AllowOrigin: envStr("BUNKR_ALLOW_ORIGIN", "*"),
	}

	if c.JWTSecret == "" {
		// Persist a generated secret so restarts do not invalidate live sessions.
		c.JWTSecret = loadOrCreateSecret(filepath.Join(dataDir, ".jwt_secret"))
	}
	c.ensureDirs()
	return c
}

func (c *Config) ensureDirs() {
	for _, d := range []string{c.DataDir, c.DownloadDir, filepath.Dir(c.DBPath)} {
		if d == "" {
			continue
		}
		_ = os.MkdirAll(d, 0o755)
	}
}

// Addr is the listen address of the HTTP server.
func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

func defaultDataDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".bunkr_downloader")
	}
	return ".bunkr_downloader"
}

// loadOrCreateSecret returns the JWT secret, generating and persisting one on
// first use so that sessions survive restarts.
func loadOrCreateSecret(path string) string {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		if secret := strings.TrimSpace(string(b)); len(secret) >= 32 {
			return secret
		}
	}
	secret := RandomToken(32)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	// 0600: the secret signs every session token.
	_ = os.WriteFile(path, []byte(secret), 0o600)
	return secret
}

// tokenAlphabet is the character set used for generated secrets.
const tokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// RandomToken returns a cryptographically random string of n characters.
//
// crypto/rand is portable (unlike /dev/urandom, which does not exist on
// Windows), so the primary path is used everywhere. The deterministic
// fallback exists only so a broken entropy source cannot crash startup.
func RandomToken(n int) string {
	if n <= 0 {
		return ""
	}
	alphabetLen := big(len(tokenAlphabet))
	buf := make([]byte, n)

	raw := make([]byte, n)
	if _, err := rand.Read(raw); err == nil {
		for i, b := range raw {
			buf[i] = tokenAlphabet[int(b)%alphabetLen]
		}
		return string(buf)
	}

	// Fallback: mask the shifted value so the index is always non-negative.
	seed := uint64(time.Now().UnixNano())
	for i := range buf {
		seed = seed*6364136223846793005 + 1442695040888963407
		buf[i] = tokenAlphabet[int((seed>>33)&0x7fffffff)%alphabetLen]
	}
	return string(buf)
}

// ---------------------------------------------------------------- env helpers

func loadDotEnv() {
	for _, name := range []string{".env", ".env.local"} {
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			// Real environment variables always win over the file.
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
		f.Close()
		return // only the first existing file is applied
	}
}

func envStr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		// Also accept a plain number of days.
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * 24 * time.Hour
		}
	}
	return def
}
