package services_test

import (
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// slogDiscard keeps the service logs out of the test output.
func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testIssuer builds a short-lived token issuer for the service tests.
func testIssuer() *auth.Issuer { return auth.NewIssuer("desktop-test-secret", time.Hour) }

// filepathWalk visits every file under root.
func filepathWalk(root string, fn func(path string)) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort search
		}
		if !d.IsDir() {
			fn(path)
		}
		return nil
	})
}

// filepathBase is a tiny indirection so the test file reads cleanly.
func filepathBase(p string) string { return filepath.Base(p) }

// mustUserID resolves the caller's id from a live session token.
func mustUserID(t *testing.T, e *env, token string) int64 {
	session, apiErr := e.auth.Me(token)
	if apiErr != nil {
		t.Fatalf("Me: %v", apiErr)
	}
	return session.User.ID
}

// mustUser reloads a user row.
func mustUser(t *testing.T, e *env, id int64) *store.User {
	t.Helper()
	u, err := e.store.GetUser(id)
	if err != nil {
		t.Fatalf("GetUser(%d): %v", id, err)
	}
	return u
}

// mustTask reloads a task row.
func mustTask(t *testing.T, e *env, id int64) *store.Task {
	t.Helper()
	task, err := e.store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask(%d): %v", id, err)
	}
	return task
}
