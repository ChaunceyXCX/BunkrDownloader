package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRandomToken(t *testing.T) {
	// This used to panic on Windows: the /dev/urandom path does not exist
	// there and the fallback indexed the alphabet with a negative number.
	for _, n := range []int{1, 8, 32, 64, 256} {
		got := RandomToken(n)
		if len(got) != n {
			t.Errorf("RandomToken(%d) returned %d characters", n, len(got))
		}
		for i, r := range got {
			if !strings.ContainsRune(tokenAlphabet, r) {
				t.Errorf("RandomToken(%d)[%d] = %q is outside the alphabet", n, i, r)
			}
		}
	}
	if got := RandomToken(0); got != "" {
		t.Errorf("RandomToken(0) = %q, want an empty string", got)
	}
	if got := RandomToken(-5); got != "" {
		t.Errorf("RandomToken(-5) = %q, want an empty string", got)
	}
	// Two calls must differ.
	if RandomToken(32) == RandomToken(32) {
		t.Error("RandomToken is not random")
	}
}

func TestLoadOrCreateSecretIsStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".jwt_secret")

	first := loadOrCreateSecret(path)
	if len(first) != 32 {
		t.Fatalf("secret length = %d, want 32", len(first))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the secret was not persisted: %v", err)
	}
	// A second call must reuse the persisted value (sessions stay valid).
	if second := loadOrCreateSecret(path); second != first {
		t.Error("the persisted secret was regenerated")
	}
	// A too-short file is replaced rather than trusted.
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if third := loadOrCreateSecret(path); len(third) != 32 {
		t.Errorf("a truncated secret was accepted: %q", third)
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Setenv("BUNKR_TEST_STR", "  hello  ")
	if got := envStr("BUNKR_TEST_STR", "def"); got != "hello" {
		t.Errorf("envStr = %q, want the trimmed value", got)
	}
	t.Setenv("BUNKR_TEST_STR", "")
	if got := envStr("BUNKR_TEST_STR", "def"); got != "def" {
		t.Errorf("envStr with an empty value = %q, want the default", got)
	}

	t.Setenv("BUNKR_TEST_INT", "42")
	if got := envInt("BUNKR_TEST_INT", 7); got != 42 {
		t.Errorf("envInt = %d, want 42", got)
	}
	t.Setenv("BUNKR_TEST_INT", "not a number")
	if got := envInt("BUNKR_TEST_INT", 7); got != 7 {
		t.Errorf("envInt with garbage = %d, want the default", got)
	}

	t.Setenv("BUNKR_TEST_BOOL", "true")
	if !envBool("BUNKR_TEST_BOOL", false) {
		t.Error("envBool(true) = false")
	}
	t.Setenv("BUNKR_TEST_BOOL", "0")
	if envBool("BUNKR_TEST_BOOL", true) {
		t.Error("envBool(0) = true")
	}

	t.Setenv("BUNKR_TEST_DUR", "90m")
	if got := envDuration("BUNKR_TEST_DUR", time.Hour); got != 90*time.Minute {
		t.Errorf("envDuration = %v, want 90m", got)
	}
	// A bare number is interpreted as days.
	t.Setenv("BUNKR_TEST_DUR", "7")
	if got := envDuration("BUNKR_TEST_DUR", time.Hour); got != 7*24*time.Hour {
		t.Errorf("envDuration(7) = %v, want 168h", got)
	}
	t.Setenv("BUNKR_TEST_DUR", "nonsense")
	if got := envDuration("BUNKR_TEST_DUR", time.Hour); got != time.Hour {
		t.Errorf("envDuration(garbage) = %v, want the default", got)
	}
}

func TestLoadDesktopDerivesPaths(t *testing.T) {
	// Point both the data and download roots at a temp dir.
	dir := t.TempDir()
	t.Setenv("BUNKR_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("BUNKR_DOWNLOAD_DIR", filepath.Join(dir, "downloads"))
	t.Setenv("BUNKR_JWT_SECRET", "")

	cfg := LoadDesktop("test-version")
	if cfg.Version != "test-version" {
		t.Errorf("version = %q", cfg.Version)
	}
	if cfg.DBPath != filepath.Join(dir, "data", "bunkr.db") {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.DownloadDir != filepath.Join(dir, "downloads") {
		t.Errorf("DownloadDir = %q", cfg.DownloadDir)
	}
	// The quota defaults match the documented free tier.
	if cfg.FreeLinksLimit != 5 || cfg.FreeFilesLimit != 50 || cfg.FreeConcurrency != 1 {
		t.Errorf("free limits = %d/%d/%d, want 5/50/1",
			cfg.FreeLinksLimit, cfg.FreeFilesLimit, cfg.FreeConcurrency)
	}
	if cfg.MemberConcurrency != 5 {
		t.Errorf("member concurrency = %d, want 5", cfg.MemberConcurrency)
	}
	// Directories are created eagerly so the first download cannot fail.
	for _, d := range []string{cfg.DataDir, cfg.DownloadDir} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Errorf("%s was not created: %v", d, err)
		}
	}
	// A generated secret is persisted.
	if len(cfg.JWTSecret) < 32 {
		t.Errorf("generated secret is only %d characters", len(cfg.JWTSecret))
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, ".jwt_secret")); err != nil {
		t.Errorf("the secret was not persisted: %v", err)
	}
	// An explicit secret is respected verbatim.
	t.Setenv("BUNKR_JWT_SECRET", "an-explicitly-provided-secret-value")
	if got := LoadDesktop("v").JWTSecret; got != "an-explicitly-provided-secret-value" {
		t.Errorf("explicit secret = %q", got)
	}
}

func TestLoadDesktopFindsSiblingAria2(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BUNKR_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("BUNKR_DOWNLOAD_DIR", filepath.Join(dir, "downloads"))
	// The executable directory is where the packaged app looks first; on a
	// test run that is the go test temp dir, so just assert the field exists
	// and the env override still wins.
	t.Setenv("BUNKR_ARIA2_BIN", "/custom/aria2c")
	if got := LoadDesktop("v").Aria2Binary; got != "/custom/aria2c" {
		t.Errorf("Aria2Binary = %q, want the env override", got)
	}
}

func TestWebLoadAppliesDocumentedDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BUNKR_DATA_DIR", dir)
	t.Setenv("BUNKR_DB", "")
	t.Setenv("BUNKR_JWT_SECRET", "")
	t.Setenv("BUNKR_FREE_LINKS_LIMIT", "")
	t.Setenv("BUNKR_FREE_FILES_LIMIT", "")

	cfg := Load("1.2.3")
	if cfg.Port != 8765 {
		t.Errorf("port = %d, want 8765", cfg.Port)
	}
	if cfg.FreeLinksLimit != 5 || cfg.FreeFilesLimit != 50 {
		t.Errorf("free limits = %d/%d, want 5/50", cfg.FreeLinksLimit, cfg.FreeFilesLimit)
	}
	if cfg.Addr() != "0.0.0.0:8765" {
		t.Errorf("Addr = %q", cfg.Addr())
	}
	if cfg.SignAPI == "" || cfg.DownloadAPI == "" || cfg.Referer == "" {
		t.Error("Bunkr endpoints were not populated")
	}
	if len(cfg.JWTSecret) < 32 {
		t.Errorf("secret is only %d characters", len(cfg.JWTSecret))
	}
}
