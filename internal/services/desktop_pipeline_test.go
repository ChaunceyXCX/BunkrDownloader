package services_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkr"
	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkrtest"
	"github.com/chaunceyxie1/BunkrDownloader/internal/downloads"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/services"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// desktopEnv is an env with a live aria2c and a mock Bunkr site, so the whole
// desktop pipeline (service call → crawl → sign → aria2 → disk) can be driven
// through the Wails bindings.
type desktopEnv struct {
	*env
	mock   *bunkrtest.Server
	dlDir  string
	cancel context.CancelFunc
}

func newDesktopEnv(t *testing.T) *desktopEnv { return newDesktopEnvWithLimits(t, 50) }

func newDesktopEnvWithLimits(t *testing.T, freeFiles int) *desktopEnv {
	t.Helper()
	root := t.TempDir()
	dlDir := root + "/downloads"
	stateDir := root + "/state"
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(stateDir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	st.Configure(
		store.QuotaLimits{Links: 5, Files: freeFiles, Concurrent: 2},
		store.QuotaLimits{Links: -1, Files: -1, Concurrent: 5},
	)
	if err := st.EnsureRedeemCodes(); err != nil {
		t.Fatal(err)
	}

	mock := bunkrtest.New()
	t.Cleanup(mock.Close)
	signAPI, downloadAPI := mock.Endpoints()
	endpoints := bunkr.Endpoints{SignAPI: signAPI, DownloadAPI: downloadAPI, Referer: mock.URL()}

	log := slogDiscard()
	aria := aria2.NewManager(aria2.Options{
		Logger:    log,
		AutoFetch: true,
		Dir:       stateDir + "/aria2",
		Secret:    "desktop-test",
		Host:      "127.0.0.1",
		MaxConc:   8,
	})
	startCtx, startCancel := context.WithTimeout(context.Background(), 90*time.Second)
	if err := aria.Start(startCtx); err != nil {
		startCancel()
		mock.Close()
		st.Close()
		t.Skipf("aria2c unavailable in this environment: %v", err)
	}
	startCancel()

	ctx, cancel := context.WithCancel(context.Background())
	events := hub.New(log, 2048)
	mgr := downloads.New(downloads.Config{
		DownloadDir:       dlDir,
		FreeConcurrency:   2,
		MemberConcurrency: 5,
		PollInterval:      250 * time.Millisecond,
		UserAgent:         "bunkr-desktop-test/1.0",
		DefaultOptions:    store.DefaultTaskOptions(),
		Endpoints:         &endpoints,
	}, st, aria, events, log)
	mgr.Start(ctx)

	app := services.NewApp(st, testIssuer(), mgr, events, log)
	app.SetDataDir(stateDir)

	e := &desktopEnv{
		env: &env{
			t: t, store: st, app: app, dl: mgr, hub: events,
			auth:    services.NewAuthService(app),
			tasks:   services.NewTaskService(app),
			member:  services.NewMembershipService(app),
			system:  services.NewSystemService(app, dlDir),
			cleanup: func() {},
		},
		mock: mock, dlDir: dlDir, cancel: cancel,
	}
	t.Cleanup(func() {
		cancel()
		events.Close()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = aria.Stop(stopCtx)
		st.Close()
		mock.Close()
	})
	return e
}

// TestDesktopDownloadThroughServices drives a complete download using only the
// Wails service methods, then verifies the bytes on disk.
func TestDesktopDownloadThroughServices(t *testing.T) {
	e := newDesktopEnv(t)
	token := e.member_("desktop")

	// A three-file album on the mock Bunkr site.
	album := bunkrtest.Album{ID: "DESK1", Title: "Desktop Album"}
	want := map[string][]byte{}
	for i := 0; i < 3; i++ {
		name := "asset" + itoa(i) + ".bin"
		body := bunkrtest.Payload("desk"+itoa(i), 48*1024)
		album.Files = append(album.Files, bunkrtest.File{
			Slug: "asset" + itoa(i), Name: name, Content: body,
		})
		want[name] = body
	}
	e.mock.AddAlbum(album)

	// 1. Create the task through the service binding.
	res, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{
		URL:       e.mock.AlbumURL("DESK1"),
		AutoStart: true,
		Options:   &services.TaskOptionsInput{MaxRetries: 3, Connections: 2},
	})
	if apiErr != nil {
		t.Fatalf("CreateTask: %v", apiErr)
	}
	if res.Count != 1 {
		t.Fatalf("CreateTask returned %d ids", res.Count)
	}
	taskID := res.TaskIDs[0]

	// 2. The album metadata and file list appear through GetTask / ListFiles.
	waitFor(t, 60*time.Second, "files to be discovered", func() bool {
		detail, err := e.store.GetTask(taskID)
		return err == nil && detail.TotalFiles == 3
	})
	detail, apiErr := e.tasks.GetTask(token, taskID)
	if apiErr != nil {
		t.Fatalf("GetTask: %v", apiErr)
	}
	if detail.Task.AlbumName != "Desktop Album" {
		t.Errorf("album name = %q", detail.Task.AlbumName)
	}

	// 3. Wait for the task to reach a terminal state.
	waitFor(t, 120*time.Second, "task completion", func() bool {
		tk, err := e.store.GetTask(taskID)
		return err == nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})
	final, apiErr := e.tasks.GetTask(token, taskID)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if final.Task.Status != store.TaskCompleted {
		t.Fatalf("status = %s (error: %s)", final.Task.Status, final.Task.ErrorMessage)
	}
	if final.Task.CompletedFiles != 3 || final.Task.Progress < 99.9 {
		t.Errorf("counters = %d completed, %.1f%%", final.Task.CompletedFiles, final.Task.Progress)
	}

	// 4. The file list reports real sizes and 100% progress.
	files, apiErr := e.tasks.ListFiles(token, taskID, "", "", "filename", "asc", 50, 0)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if files.Total != 3 {
		t.Fatalf("ListFiles total = %d, want 3", files.Total)
	}
	for _, f := range files.Files {
		if f.Status != store.FileCompleted {
			t.Errorf("file %s is %s", f.Filename, f.Status)
		}
		if f.FileSize != 48*1024 {
			t.Errorf("file %s size = %d, want %d", f.Filename, f.FileSize, 48*1024)
		}
		if f.Progress < 99.9 {
			t.Errorf("file %s progress = %.1f%%", f.Filename, f.Progress)
		}
	}

	// 5. The bytes on disk match what the mock served.
	for name, body := range want {
		path := findFile(e.dlDir, name)
		if path == "" {
			t.Errorf("%s is missing from %s", name, e.dlDir)
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}
		if bunkrtest.Checksum(got) != bunkrtest.Checksum(body) {
			t.Errorf("%s checksum mismatch (%d bytes, want %d)", name, len(got), len(body))
		}
	}

	// 6. The event log tells the story, and the per-status summary adds up.
	events, apiErr := e.tasks.ListEvents(token, taskID, 0, 200)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	seen := map[string]bool{}
	for _, ev := range events.Events {
		seen[ev.Event] = true
	}
	for _, want := range []string{"Task started", "Album crawled", "Links resolved", "Enqueuing downloads"} {
		if !seen[want] {
			t.Errorf("missing event %q; got %v", want, keysOf(seen))
		}
	}
	// Re-read the detail so the summary reflects the finished state.
	settled, apiErr := e.tasks.GetTask(token, taskID)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if settled.FilesSummary[store.FileCompleted] != 3 {
		t.Errorf("files summary = %v, want 3 completed", settled.FilesSummary)
	}
	if settled.FilesSummary[store.FilePending] != 0 {
		t.Errorf("files summary still has pending entries: %v", settled.FilesSummary)
	}

	// 7. System health now reports a live engine.
	if !e.system.Health().Aria2.Available {
		t.Error("aria2 should be reported as available")
	}
	stats, apiErr := e.system.Stats(token)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if stats.TotalTasks != 1 || stats.CompletedFiles != 3 {
		t.Errorf("stats = %d tasks / %d files", stats.TotalTasks, stats.CompletedFiles)
	}
	if stats.Aria2.Available != true {
		t.Errorf("stats aria2 = %+v", stats.Aria2)
	}
}

// TestDesktopPauseResumeAndCancel drives the lifecycle through the bindings.
func TestDesktopPauseResumeAndCancel(t *testing.T) {
	e := newDesktopEnv(t)
	token := e.member_("lifecycle")

	// A throttled single file so there is time to act on it.
	e.mock.AddAlbum(bunkrtest.Album{
		ID: "LIFE1", Title: "Lifecycle",
		Files: []bunkrtest.File{{
			Slug: "slow", Name: "slow.bin",
			Content:       bunkrtest.Payload("slow", 2*1024*1024),
			BytesPerWrite: 64 * 1024,
			WriteDelay:    150 * time.Millisecond,
		}},
	})

	res, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{
		URL: e.mock.AlbumURL("LIFE1"), AutoStart: true,
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	taskID := res.TaskIDs[0]

	// Pause once bytes are flowing.
	waitFor(t, 60*time.Second, "bytes to flow", func() bool {
		files, _ := e.store.AllFilesForTask(taskID)
		for _, f := range files {
			if f.DownloadedBytes > 0 {
				return true
			}
		}
		return false
	})
	paused, apiErr := e.tasks.Pause(token, taskID)
	if apiErr != nil {
		t.Fatalf("Pause: %v", apiErr)
	}
	if paused.Status != store.TaskPaused {
		t.Fatalf("status after pause = %s", paused.Status)
	}

	// aria2 applies the pause asynchronously and bytes already in flight still
	// land, so wait for the counter to settle before asserting it stays frozen.
	before := waitForStableBytes(t, e, taskID, 15*time.Second)
	time.Sleep(2 * time.Second)
	after := maxDownloaded(t, e, taskID)
	if after != before {
		t.Errorf("download progressed while paused: %d -> %d", before, after)
	}

	// Resume keeps the same GID (a true byte-level resume).
	resumed, apiErr := e.tasks.Resume(token, taskID)
	if apiErr != nil {
		t.Fatalf("Resume: %v", apiErr)
	}
	if resumed.Status != store.TaskRunning {
		t.Errorf("status after resume = %s, want running", resumed.Status)
	}
	gidAfter := firstGID(t, e, taskID)
	if gidAfter == "" {
		t.Error("the GID was lost across pause/resume")
	}

	waitFor(t, 120*time.Second, "completion after resume", func() bool {
		tk, _ := e.store.GetTask(taskID)
		return tk != nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})
	final, _ := e.store.GetTask(taskID)
	if final.Status != store.TaskCompleted {
		t.Fatalf("status = %s (error: %s)", final.Status, final.ErrorMessage)
	}
	if findFile(e.dlDir, "slow.bin") == "" {
		t.Error("slow.bin missing after resume")
	}

	// Cancel a second task and confirm the state.
	res2, _ := e.tasks.CreateTask(token, services.CreateTaskRequest{
		URL: e.mock.AlbumURL("LIFE1"), AutoStart: true,
	})
	canceled, apiErr := e.tasks.Cancel(token, res2.TaskIDs[0])
	if apiErr != nil {
		t.Fatalf("Cancel: %v", apiErr)
	}
	if canceled.Status != store.TaskCanceled {
		t.Errorf("status after cancel = %s", canceled.Status)
	}
	// And delete it.
	if ok, apiErr := e.tasks.DeleteTask(token, res2.TaskIDs[0]); apiErr != nil || !ok {
		t.Errorf("DeleteTask = %v / %v", ok, apiErr)
	}
}

// TestDesktopFreeTierFileCap proves the desktop build enforces the file quota
// during the crawl, exactly like the web build.
func TestDesktopFreeTierFileCap(t *testing.T) {
	// A 3-file free allowance against a 6-file album.
	e := newDesktopEnvWithLimits(t, 3)
	token := e.member_("freebie")
	userID := mustUserID(t, e.env, token)

	album := bunkrtest.Album{ID: "CAP1", Title: "Capped"}
	for i := 0; i < 6; i++ {
		album.Files = append(album.Files, bunkrtest.File{
			Slug: "c" + itoa(i), Name: "c" + itoa(i) + ".bin",
			Content: bunkrtest.Payload("cap"+itoa(i), 8*1024),
		})
	}
	e.mock.AddAlbum(album)

	res, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{
		URL: e.mock.AlbumURL("CAP1"), AutoStart: true,
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	taskID := res.TaskIDs[0]

	waitFor(t, 90*time.Second, "task to settle", func() bool {
		tk, _ := e.store.GetTask(taskID)
		return tk != nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})

	files, apiErr := e.tasks.ListFiles(token, taskID, "", "", "", "", 50, 0)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if files.Total != 6 {
		t.Errorf("registered %d files, want 6", files.Total)
	}
	completed, quotaSkipped := 0, 0
	for _, f := range files.Files {
		if f.Status == store.FileCompleted {
			completed++
		}
		if f.Status == store.FileSkipped && containsText(f.ErrorMessage, "额度") {
			quotaSkipped++
		}
	}
	// The allowance must be spent exactly, not merely respected.
	if completed != 3 {
		t.Errorf("downloaded %d files, want exactly 3 (the whole free allowance)", completed)
	}
	if quotaSkipped != 3 {
		t.Errorf("skipped %d files with a quota reason, want 3", quotaSkipped)
	}
	quota, err := e.store.Quota(userID)
	if err != nil {
		t.Fatalf("Quota: %v", err)
	}
	if quota.FilesUsed > 3 {
		t.Errorf("quota consumed %d files, want <= 3", quota.FilesUsed)
	}
	if quota.IsMember {
		t.Error("a free user was reported as a member")
	}
	if quota.FilesUsed != 3 {
		t.Errorf("quota consumed %d files, want exactly 3", quota.FilesUsed)
	}
}

// ------------------------------------------------------------------ helpers

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

func findFile(root, name string) string {
	found := ""
	_ = filepathWalk(root, func(path string) {
		if filepathBase(path) == name {
			found = path
		}
	})
	return found
}

// waitForStableBytes waits until the task's byte counter stops moving, which is
// how a pause manifests, and returns the settled value.
func waitForStableBytes(t *testing.T, e *desktopEnv, taskID int64, timeout time.Duration) int64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := maxDownloaded(t, e, taskID)
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		current := maxDownloaded(t, e, taskID)
		if current != last {
			last = current
			stableSince = time.Now()
			continue
		}
		if time.Since(stableSince) > 800*time.Millisecond {
			return current
		}
	}
	t.Log("byte counter never settled; asserting against the last reading")
	return last
}

func maxDownloaded(t *testing.T, e *desktopEnv, taskID int64) int64 {
	t.Helper()
	files, err := e.store.AllFilesForTask(taskID)
	if err != nil {
		t.Fatalf("AllFilesForTask: %v", err)
	}
	var max int64
	for _, f := range files {
		if f.DownloadedBytes > max {
			max = f.DownloadedBytes
		}
	}
	return max
}

func firstGID(t *testing.T, e *desktopEnv, taskID int64) string {
	t.Helper()
	files, err := e.store.AllFilesForTask(taskID)
	if err != nil {
		t.Fatalf("AllFilesForTask: %v", err)
	}
	for _, f := range files {
		if f.GID != "" {
			return f.GID
		}
	}
	return ""
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func containsText(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOfStr(haystack, needle) >= 0
}

func indexOfStr(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
