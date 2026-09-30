package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
)

// newNullAria2 builds an aria2 supervisor that is never started. The handlers
// treat a missing daemon as a degraded state, so the HTTP surface is still
// fully testable without a download engine.
func newNullAria2(stateDir string) *aria2.Manager {
	return aria2.NewManager(aria2.Options{
		Dir:    filepath.Join(stateDir, "aria2"),
		Secret: "test",
		Host:   "127.0.0.1",
	})
}

// staticDir creates a minimal SPA directory so the history fallback is exercised.
func staticDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `<!doctype html><html><head><title>Bunkr</title></head>
<script type="module" src="./assets/index.js"></script></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
