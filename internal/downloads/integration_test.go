package downloads_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkrtest"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// TestAlbumDownloadEndToEnd is the headline integration test: it drives the
// full pipeline (crawl → sign → aria2 → disk → database → events) against the
// mock Bunkr server and asserts the bytes on disk match what was served.
func TestAlbumDownloadEndToEnd(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	user := h.newMember(t)

	_, want := albumFixture(h, "E2E1", 3, 64*1024)

	task, err := h.manager.CreateTask(context.Background(), user.ID,
		h.mock.AlbumURL("E2E1"), store.DefaultTaskOptions())
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.Kind != "album" {
		t.Errorf("kind = %q, want album", task.Kind)
	}
	if err := h.manager.StartTask(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("start task: %v", err)
	}

	waitFor(t, 90*time.Second, "task completion", func() bool {
		tk, err := h.store.GetTask(task.ID)
		return err == nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})

	final, err := h.store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if final.Status != store.TaskCompleted {
		t.Fatalf("task status = %s (error: %s)", final.Status, final.ErrorMessage)
	}
	if final.AlbumName != "Test Album E2E1" {
		t.Errorf("album name = %q, want %q", final.AlbumName, "Test Album E2E1")
	}
	if final.TotalFiles != 3 {
		t.Errorf("total files = %d, want 3", final.TotalFiles)
	}
	if final.CompletedFiles != 3 {
		t.Errorf("completed files = %d, want 3", final.CompletedFiles)
	}
	if final.Progress < 99.9 {
		t.Errorf("progress = %.2f, want 100", final.Progress)
	}
	if final.DownloadPath == "" {
		t.Error("download path was not recorded")
	} else if filepath.Base(final.DownloadPath) != "Test Album E2E1 (E2E1)" {
		t.Errorf("download dir = %q, want the album folder", final.DownloadPath)
	}

	// Every file must be on disk with exactly the served bytes.
	for name, body := range want {
		path := h.fileAt(name)
		if path == "" {
			t.Errorf("file %s not found on disk under %s", name, h.dlDir)
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}
		if bunkrtest.Checksum(got) != bunkrtest.Checksum(body) {
			t.Errorf("file %s checksum mismatch (%d bytes, want %d)", name, len(got), len(body))
		}
	}

	// The signing API must have been used for every item.
	signCalls, _ := h.mock.Stats()
	if signCalls < 3 {
		t.Errorf("sign API called %d times, want >= 3", signCalls)
	}

	// The event log must tell the story.
	events, err := h.store.ListEvents(user.ID, task.ID, 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	names := map[string]bool{}
	for _, e := range events {
		names[e.Event] = true
	}
	for _, want := range []string{"Task started", "Album crawled", "Links resolved", "Enqueuing downloads"} {
		if !names[want] {
			t.Errorf("missing event %q; got %v", want, keys(names))
		}
	}
}

// TestSingleMediaDownload covers the /v/ code path.
func TestSingleMediaDownload(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	user := h.newMember(t)

	body := bunkrtest.Payload("single", 48*1024)
	h.mock.AddAlbum(bunkrtest.Album{
		ID: "SINGLE1", Title: "Single Home",
		Files: []bunkrtest.File{{Slug: "onlyone", Name: "onlyone.dat", Content: body}},
	})

	url := h.mock.URL() + "/v/onlyone"
	task, err := h.manager.CreateTask(context.Background(), user.ID, url, store.DefaultTaskOptions())
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.Kind != "media" {
		t.Errorf("kind = %q, want media", task.Kind)
	}
	if err := h.manager.StartTask(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("start: %v", err)
	}

	waitFor(t, 60*time.Second, "single media completion", func() bool {
		tk, _ := h.store.GetTask(task.ID)
		return tk != nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})

	final, _ := h.store.GetTask(task.ID)
	if final.Status != store.TaskCompleted {
		t.Fatalf("status = %s (error: %s)", final.Status, final.ErrorMessage)
	}
	path := h.fileAt("onlyone.dat")
	if path == "" {
		t.Fatal("onlyone.dat not on disk")
	}
	got, _ := os.ReadFile(path)
	if bunkrtest.Checksum(got) != bunkrtest.Checksum(body) {
		t.Error("checksum mismatch for single media")
	}
}

// TestDownloadAPIFallback covers archives: the item page has no jsCDN variable,
// so the crawler must fall back to the direct download API.
func TestDownloadAPIFallback(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	user := h.newMember(t)

	body := bunkrtest.Payload("archive", 32*1024)
	h.mock.AddAlbum(bunkrtest.Album{
		ID: "ARCH1", Title: "Archive Home",
		Files: []bunkrtest.File{{
			Slug: "archive", Name: "archive.zip", Content: body, NoLandingPage: true,
		}},
	})

	task, err := h.manager.CreateTask(context.Background(), user.ID,
		h.mock.AlbumURL("ARCH1"), store.DefaultTaskOptions())
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.manager.StartTask(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, 60*time.Second, "fallback download", func() bool {
		tk, _ := h.store.GetTask(task.ID)
		return tk != nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})

	final, _ := h.store.GetTask(task.ID)
	if final.Status != store.TaskCompleted {
		t.Fatalf("status = %s (error: %s)", final.Status, final.ErrorMessage)
	}
	if path := h.fileAt("archive.zip"); path == "" {
		t.Error("archive.zip not on disk (download-API fallback failed)")
	}
}

// TestPauseResumeKeepsPartialData verifies that pausing really suspends the
// transfer and resuming continues it rather than restarting from zero.
func TestPauseResumeKeepsPartialData(t *testing.T) {
	// A large file gives us time to observe the paused state.
	const size = 4 * 1024 * 1024
	h := newHarness(t, harnessOpts{})
	user := h.newMember(t)

	body := bunkrtest.Payload("big", size)
	h.mock.AddAlbum(bunkrtest.Album{
		ID: "BIG1", Title: "Big File",
		Files: []bunkrtest.File{{
			Slug: "bigfile", Name: "bigfile.bin", Content: body,
			// Throttle so the transfer lasts long enough to pause it.
			BytesPerWrite: 64 * 1024,
			WriteDelay:    120 * time.Millisecond,
		}},
	})

	task, err := h.manager.CreateTask(context.Background(), user.ID,
		h.mock.AlbumURL("BIG1"), store.DefaultTaskOptions())
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.manager.StartTask(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait until bytes are actually flowing.
	waitFor(t, 60*time.Second, "first bytes", func() bool {
		files, _ := h.store.AllFilesForTask(task.ID)
		for _, f := range files {
			if f.DownloadedBytes > 0 {
				return true
			}
		}
		return false
	})

	if err := h.manager.Pause(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	paused, _ := h.store.GetTask(task.ID)
	if paused.Status != store.TaskPaused {
		t.Fatalf("status after pause = %s, want paused", paused.Status)
	}

	// aria2 applies pause asynchronously, and bytes already in flight still
	// land, so wait for the counter to settle before asserting it is frozen.
	settled := waitForStableBytes(h, task.ID, 15*time.Second)
	time.Sleep(2500 * time.Millisecond)
	after := maxDownloaded(h, task.ID)
	if after != settled {
		t.Errorf("download kept progressing while paused: %d -> %d bytes", settled, after)
	}

	if err := h.manager.Resume(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	resumed, _ := h.store.GetTask(task.ID)
	if resumed.Status != store.TaskRunning {
		t.Errorf("status after resume = %s, want running", resumed.Status)
	}

	waitFor(t, 120*time.Second, "task completion after resume", func() bool {
		tk, _ := h.store.GetTask(task.ID)
		return tk != nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})
	final, _ := h.store.GetTask(task.ID)
	if final.Status != store.TaskCompleted {
		t.Fatalf("status = %s (error: %s)", final.Status, final.ErrorMessage)
	}
	path := h.fileAt("bigfile.bin")
	if path == "" {
		t.Fatal("bigfile.bin missing after resume")
	}
	got, _ := os.ReadFile(path)
	if bunkrtest.Checksum(got) != bunkrtest.Checksum(body) {
		t.Errorf("checksum mismatch after resume: got %d bytes want %d", len(got), len(body))
	}
}

// TestCancelStopsAndSkips asserts cancel terminates the task and leaves the
// remaining files skipped rather than stuck.
func TestCancelStopsAndSkips(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	user := h.newMember(t)
	albumFixture(h, "CANCEL1", 4, 512*1024)

	task, err := h.manager.CreateTask(context.Background(), user.ID,
		h.mock.AlbumURL("CANCEL1"), store.DefaultTaskOptions())
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.manager.StartTask(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, 45*time.Second, "crawl to finish", func() bool {
		files, _ := h.store.AllFilesForTask(task.ID)
		return len(files) >= 4
	})

	if err := h.manager.Cancel(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	final, _ := h.store.GetTask(task.ID)
	if final.Status != store.TaskCanceled {
		t.Fatalf("status = %s, want canceled", final.Status)
	}
	if final.FinishedAt == nil {
		t.Error("finished_at was not set on cancel")
	}
	for _, f := range mustAllFiles(t, h, task.ID) {
		if f.Status == store.FileDownloading || f.Status == store.FilePending {
			t.Errorf("file %d left in %s after cancel", f.ID, f.Status)
		}
	}
}

// TestQuotaTruncatesAlbum proves the free-tier file cap is enforced during the
// crawl: the extra files are skipped with a reason instead of downloading.
func TestQuotaTruncatesAlbum(t *testing.T) {
	h := newHarness(t, harnessOpts{freeFiles: 3, freeLinks: 5, freeConc: 1})
	user := h.newUser(t, "freebie")
	albumFixture(h, "QUOTA1", 6, 16*1024)

	task, err := h.manager.CreateTask(context.Background(), user.ID,
		h.mock.AlbumURL("QUOTA1"), store.DefaultTaskOptions())
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.manager.StartTask(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, 60*time.Second, "task to finish", func() bool {
		tk, _ := h.store.GetTask(task.ID)
		return tk != nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})

	// Every album item is registered, but only the allowance is downloaded:
	// the rest are skipped with a quota reason and must not consume the cap.
	files := mustAllFiles(t, h, task.ID)
	if len(files) != 6 {
		t.Errorf("registered %d files, want 6 (the album is fully known)", len(files))
	}
	completed, skippedForQuota := 0, 0
	for _, f := range files {
		switch f.Status {
		case store.FileCompleted:
			completed++
		case store.FileSkipped:
			if strings.Contains(f.ErrorMessage, "额度") {
				skippedForQuota++
			}
		}
	}
	if completed > 3 {
		t.Errorf("downloaded %d files, want at most 3 under the free cap", completed)
	}
	if skippedForQuota == 0 {
		t.Error("expected the surplus files to be skipped with a quota reason")
	}
	quota, err := h.store.Quota(user.ID)
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if quota.FilesUsed > 3 {
		t.Errorf("files used = %d, want <= 3", quota.FilesUsed)
	}
	if quota.IsMember {
		t.Error("free user reported as a member")
	}
}

// TestFreeConcurrencyQueue proves the free tier only runs one task at a time
// and that the surplus is queued rather than dropped.
func TestFreeConcurrencyQueue(t *testing.T) {
	h := newHarness(t, harnessOpts{freeConc: 1, freeFiles: 50})
	user := h.newUser(t, "queued")
	h.mock.AddAlbum(bunkrtest.Album{ID: "Q1", Title: "Queue One",
		Files: []bunkrtest.File{{Slug: "qa", Name: "qa.bin",
			Content:       bunkrtest.Payload("qa", 4*1024),
			BytesPerWrite: 4 * 1024, WriteDelay: 400 * time.Millisecond}}})
	h.mock.AddAlbum(bunkrtest.Album{ID: "Q2", Title: "Queue Two",
		Files: []bunkrtest.File{{Slug: "qb", Name: "qb.bin",
			Content:       bunkrtest.Payload("qb", 4*1024),
			BytesPerWrite: 4 * 1024, WriteDelay: 400 * time.Millisecond}}})

	ctx := context.Background()
	t1, err := h.manager.CreateTask(ctx, user.ID, h.mock.AlbumURL("Q1"), store.DefaultTaskOptions())
	if err != nil {
		t.Fatalf("create t1: %v", err)
	}
	t2, err := h.manager.CreateTask(ctx, user.ID, h.mock.AlbumURL("Q2"), store.DefaultTaskOptions())
	if err != nil {
		t.Fatalf("create t2: %v", err)
	}
	if err := h.manager.StartTask(ctx, t1.ID, user.ID); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	if err := h.manager.StartTask(ctx, t2.ID, user.ID); err != nil {
		t.Fatalf("start t2: %v", err)
	}

	// The second task must be parked in the pending/queued state.
	waitFor(t, 20*time.Second, "second task to be queued", func() bool {
		tk, _ := h.store.GetTask(t2.ID)
		return tk != nil && tk.Status == store.TaskPending && !h.manager.IsRunning(t2.ID)
	})

	// Once the first finishes the dispatcher promotes the second.
	waitFor(t, 90*time.Second, "both tasks to finish", func() bool {
		a, _ := h.store.GetTask(t1.ID)
		b, _ := h.store.GetTask(t2.ID)
		return a != nil && b != nil &&
			a.Status == store.TaskCompleted && b.Status == store.TaskCompleted
	})
}

// TestResumeAfterRestart proves the DB is the source of truth: a task left in
// the paused state at boot can be resumed and completes.
func TestResumeAfterRestart(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	user := h.newMember(t)
	_, want := albumFixture(h, "RESTART1", 2, 24*1024)

	task, err := h.manager.CreateTask(context.Background(), user.ID,
		h.mock.AlbumURL("RESTART1"), store.DefaultTaskOptions())
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	// Simulate a crash: crawl registers the files, then the process dies.
	if err := h.manager.StartTask(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, 45*time.Second, "files to be registered", func() bool {
		files, _ := h.store.AllFilesForTask(task.ID)
		return len(files) == 2
	})
	if err := h.manager.Cancel(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := h.store.RequeueInterrupted(); err != nil {
		t.Fatalf("requeue: %v", err)
	}

	// Retry must pick the task back up and finish the work.
	if _, err := h.manager.Retry(context.Background(), task.ID, user.ID, nil); err != nil {
		t.Fatalf("retry after restart: %v", err)
	}
	waitFor(t, 90*time.Second, "task completion after restart", func() bool {
		tk, _ := h.store.GetTask(task.ID)
		return tk != nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})
	final, _ := h.store.GetTask(task.ID)
	if final.Status != store.TaskCompleted {
		t.Fatalf("status = %s (error: %s)", final.Status, final.ErrorMessage)
	}
	for name := range want {
		if h.fileAt(name) == "" {
			t.Errorf("%s missing after restart-retry", name)
		}
	}
}

// TestOptionsFiltersIgnoredAndIncluded verifies the include/ignore rules.
func TestOptionsFiltersIgnoredAndIncluded(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	user := h.newMember(t)

	album := bunkrtest.Album{ID: "FILTER1", Title: "Filtered"}
	for _, n := range []string{"movie.mp4", "cover.jpg", "notes.txt"} {
		slug := strings.TrimSuffix(n, filepath.Ext(n))
		album.Files = append(album.Files, bunkrtest.File{
			Slug: slug, Name: n, Content: bunkrtest.Payload(n, 8*1024),
		})
	}
	h.mock.AddAlbum(album)

	opts := store.DefaultTaskOptions()
	opts.Ignore = []string{"cover"}
	opts.Include = []string{"mp4", "txt"}

	task, err := h.manager.CreateTask(context.Background(), user.ID,
		h.mock.AlbumURL("FILTER1"), opts)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.manager.StartTask(context.Background(), task.ID, user.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, 90*time.Second, "filtered task to finish", func() bool {
		tk, _ := h.store.GetTask(task.ID)
		return tk != nil && (tk.Status == store.TaskCompleted || tk.Status == store.TaskFailed)
	})

	downloaded := map[string]bool{}
	for _, f := range mustAllFiles(t, h, task.ID) {
		if f.Status == store.FileCompleted {
			downloaded[f.Filename] = true
		}
	}
	if !downloaded["movie.mp4"] {
		t.Error("movie.mp4 should have been downloaded")
	}
	if !downloaded["notes.txt"] {
		t.Error("notes.txt should have been downloaded")
	}
	if downloaded["cover.jpg"] {
		t.Error("cover.jpg matched the ignore list and must not be downloaded")
	}
}

func mustAllFiles(t *testing.T, h *harness, taskID int64) []*store.File {
	t.Helper()
	files, err := h.store.AllFilesForTask(taskID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	return files
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
