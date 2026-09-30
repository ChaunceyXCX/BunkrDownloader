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
