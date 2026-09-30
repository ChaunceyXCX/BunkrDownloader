package downloads_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkr"
	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkrtest"
	"github.com/chaunceyxie1/BunkrDownloader/internal/downloads"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// harness bundles everything a download test needs.
type harness struct {
	t        *testing.T
	store    *store.Store
	aria     *aria2.Manager
	manager  *downloads.Manager
	hub      *hub.Hub
	mock     *bunkrtest.Server
	dlDir    string
	stateDir string
	cancel   context.CancelFunc
	done     chan struct{}
}

type harnessOpts struct {
	// skipAria2 leaves the download engine off, for pure-crawl assertions.
	skipAria2 bool
	logLevel  string
	// free limits; zero values fall back to the documented defaults.
	freeLinks, freeFiles, freeConc int
	aria2Binary                    string
}

func newHarness(t *testing.T, opts harnessOpts) *harness {
	t.Helper()

	if opts.freeLinks == 0 {
		opts.freeLinks = 5
	}
	if opts.freeFiles == 0 {
		opts.freeFiles = 50
	}
	if opts.freeConc == 0 {
		opts.freeConc = 1
	}

	root := t.TempDir()
	dlDir := filepath.Join(root, "downloads")
	stateDir := filepath.Join(root, "state")
	for _, d := range []string{dlDir, stateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	st, err := store.Open(filepath.Join(stateDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	st.Configure(
		store.QuotaLimits{Links: opts.freeLinks, Files: opts.freeFiles, Concurrent: opts.freeConc},
		store.QuotaLimits{Links: -1, Files: -1, Concurrent: 5},
	)
	t.Cleanup(func() { st.Close() })

	mock := bunkrtest.New()
	t.Cleanup(mock.Close)
	signAPI, downloadAPI := mock.Endpoints()
	eps := bunkr.Endpoints{SignAPI: signAPI, DownloadAPI: downloadAPI, Referer: mock.URL()}

	level := slog.LevelError
	if opts.logLevel == "debug" {
		level = slog.LevelDebug
	}
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ariaMgr := aria2.NewManager(aria2.Options{
		Logger:     quiet,
		BinaryPath: opts.aria2Binary,
		AutoFetch:  true,
		Dir:        filepath.Join(stateDir, "aria2"),
		Secret:     "test-secret",
		Host:       "127.0.0.1",
		MaxConc:    8,
	})

	ctx, cancel := context.WithCancel(context.Background())
	if !opts.skipAria2 {
		startCtx, startCancel := context.WithTimeout(ctx, 90*time.Second)
		err := ariaMgr.Start(startCtx)
		startCancel()
		if err != nil {
			cancel()
			t.Skipf("aria2c unavailable in this environment: %v", err)
		}
		t.Cleanup(func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer stopCancel()
			_ = ariaMgr.Stop(stopCtx)
		})
	}

	events := hub.New(quiet, 1024)
	mgr := downloads.New(downloads.Config{
		DownloadDir:       dlDir,
		FreeConcurrency:   opts.freeConc,
		MemberConcurrency: 5,
		PollInterval:      250 * time.Millisecond,
		UserAgent:         "bunkr-downloader-test/1.0",
		DefaultOptions:    store.DefaultTaskOptions(),
		Endpoints:         &eps,
	}, st, ariaMgr, events, quiet)
	mgr.Start(ctx)

	h := &harness{
		t: t, store: st, aria: ariaMgr, manager: mgr,
		hub: events, mock: mock, dlDir: dlDir, stateDir: stateDir,
		cancel: cancel, done: make(chan struct{}),
	}
	t.Cleanup(h.Close)
	return h
}

// Close stops the background loops.
func (h *harness) Close() {
	select {
	case <-h.done:
		return
	default:
	}
	close(h.done)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h.manager.Shutdown(ctx)
	h.hub.Close()
	h.cancel()
}

// newMember registers a user and upgrades it past the free quota.
func (h *harness) newMember(t *testing.T) *store.User {
	t.Helper()
	u, err := h.store.CreateUser("member", "member@test.local", "passw0rd")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := h.store.ActivateMembership(u.ID, store.PlanMember, 30); err != nil {
		t.Fatalf("activate membership: %v", err)
	}
	fresh, err := h.store.GetUser(u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return fresh
}

func (h *harness) newUser(t *testing.T, name string) *store.User {
	t.Helper()
	u, err := h.store.CreateUser(name, name+"@test.local", "passw0rd")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

// waitFor polls cond until it holds or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", desc)
}

// fileAt returns the on-disk path of slug inside the download tree.
func (h *harness) fileAt(name string) string {
	var found string
	_ = filepath.Walk(h.dlDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil //nolint:nilerr // best-effort search
		}
		if filepath.Base(path) == name {
			found = path
		}
		return nil
	})
	return found
}

// albumFixture registers a 3-file album on the mock and returns it.
func albumFixture(h *harness, id string, fileCount, size int) (bunkrtest.Album, map[string][]byte) {
	album := bunkrtest.Album{ID: id, Title: "Test Album " + id}
	want := map[string][]byte{}
	for i := 0; i < fileCount; i++ {
		slug := "file" + string(rune('a'+i))
		name := slug + ".bin"
		body := bunkrtest.Payload(id+"/"+name, size)
		album.Files = append(album.Files, bunkrtest.File{
			Slug: slug, Name: name, Content: body, AlbumID: id,
		})
		want[name] = body
	}
	h.mock.AddAlbum(album)
	return album, want
}

// maxDownloaded returns the largest downloaded byte count across a task's files.
func maxDownloaded(h *harness, taskID int64) int64 {
	files, err := h.store.AllFilesForTask(taskID)
	if err != nil {
		return 0
	}
	var max int64
	for _, f := range files {
		if f.DownloadedBytes > max {
			max = f.DownloadedBytes
		}
	}
	return max
}

// waitForStableBytes waits until the task's byte counter stops moving, which
// is how a pause manifests. It returns the settled value.
func waitForStableBytes(h *harness, taskID int64, timeout time.Duration) int64 {
	deadline := time.Now().Add(timeout)
	last := maxDownloaded(h, taskID)
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		current := maxDownloaded(h, taskID)
		if current != last {
			last = current
			stableSince = time.Now()
			continue
		}
		// Two consecutive identical readings means the transfer has stopped.
		if time.Since(stableSince) > 800*time.Millisecond {
			return current
		}
	}
	return last
}
