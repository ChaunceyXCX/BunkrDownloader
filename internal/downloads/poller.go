package downloads

import (
	"context"
	"fmt"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// pollLoop keeps SQLite and the WebSocket in sync with the aria2 daemon.
//
// One batched tellStatus call per tick covers every active download across
// every user, which keeps the RPC traffic linear in the number of running
// downloads rather than per-task.
func (m *Manager) pollLoop() {
	defer m.wg.Done()

	// Bootstrap: aria2 may still be starting up.
	waitForAria2(m.ctx, m.aria, m.log)

	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	statsTicker := time.NewTicker(2 * time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-statsTicker.C:
			m.publishStats()
		case <-ticker.C:
			m.pollOnce()
		}
	}
}

func waitForAria2(ctx context.Context, a *aria2.Manager, log interface{ Warn(string, ...any) }) {
	for i := 0; i < 120; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if a.Healthy(ctx) {
			return
		}
		time.Sleep(time.Second)
	}
	log.Warn("aria2 still unavailable after 2 minutes; downloads will stay queued")
}

// pollOnce performs a single progress sweep.
func (m *Manager) pollOnce() {
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	defer cancel()

	client := m.aria.Client()
	if client == nil || !m.aria.Healthy(ctx) {
		return
	}

	// Every file currently bound to a GID.
	var rows []*store.File
	files, err := m.activeFiles()
	if err != nil {
		m.log.Warn("load active files", "error", err)
		return
	}
	rows = files
	if len(rows) == 0 {
		return
	}

	gids := make([]string, 0, len(rows))
	byGID := make(map[string]*store.File, len(rows))
	for _, f := range rows {
		if f.GID == "" {
			continue
		}
		gids = append(gids, f.GID)
		byGID[f.GID] = f
	}
	if len(gids) == 0 {
		return
	}

	statuses, err := client.TellStatusBatch(ctx, gids,
		"gid", "status", "totalLength", "completedLength", "downloadSpeed",
		"errorCode", "errorMessage", "dir", "files")
	if err != nil {
		m.log.Debug("tellStatus failed", "error", err)
		return
	}

	// Group per task so counters are written once per task, not per file.
	agg := map[int64]*taskAgg{}
	terminalUpdates := map[string]store.FileUpdate{} // gid -> update

	finishedAt := time.Now().UTC()

	for gid, f := range byGID {
		st, ok := statuses[gid]
		if !ok {
			// aria2 forgot the download (e.g. session reload). Requeue it.
			terminalUpdates[gid] = store.FileUpdate{
				Status:       strPtr(store.FilePending),
				GID:          strPtr(""),
				Speed:        i64Ptr(0),
				ErrorMessage: strPtr("aria2 会话已丢失，等待重新入队"),
			}
			continue
		}

		a := agg[f.TaskID]
		if a == nil {
			a = &taskAgg{}
			agg[f.TaskID] = a
		}

		total := st.TotalLength.Int64()
		completed := st.CompletedLength.Int64()
		if total <= 0 && len(st.Files) > 0 {
			total = st.Files[0].Length.Int64()
			completed = st.Files[0].CompletedLength.Int64()
		}
		a.speed += st.DownloadSpeed.Int64()
		a.downloaded += completed
		a.total += total

		switch st.Status {
		case "active", "waiting":
			a.hasActive = true
			upd := store.FileUpdate{
				Speed:           i64Ptr(st.DownloadSpeed.Int64()),
				DownloadedBytes: i64Ptr(completed),
			}
			if total > 0 && total != f.FileSize {
				upd.FileSize = i64Ptr(total)
			}
			if st.Status == "active" {
				upd.Status = strPtr(store.FileDownloading)
			}
			terminalUpdates[gid] = upd
		case "paused":
			upd := store.FileUpdate{
				Speed:           i64Ptr(0),
				DownloadedBytes: i64Ptr(completed),
			}
			terminalUpdates[gid] = upd
		case "complete":
			a.terminating = true
			size := total
			if size <= 0 {
				size = completed
			}
			terminalUpdates[gid] = store.FileUpdate{
				Status:          strPtr(store.FileCompleted),
				Speed:           i64Ptr(0),
				DownloadedBytes: i64Ptr(completed),
				FileSize:        i64Ptr(size),
				FinishedAt:      &finishedAt,
			}
		case "error":
			a.terminating = true
			terminalUpdates[gid] = store.FileUpdate{
				Status:       strPtr(store.FileFailed),
				Speed:        i64Ptr(0),
				ErrorMessage: strPtr(shortErr(fmt.Errorf("aria2: %s", ariaError(st.ErrorCode, st.ErrorMessage)))),
			}
		case "removed":
			terminalUpdates[gid] = store.FileUpdate{
				Status: strPtr(store.FilePending), GID: strPtr(""), Speed: i64Ptr(0),
			}
		}
	}

	// Persist per-file updates.
	if len(terminalUpdates) > 0 {
		if err := m.st.UpdateFilesByGID(terminalUpdates); err != nil {
			m.log.Warn("persist file progress", "error", err)
		}
	}

	// Persist per-task counters and push frames to the UI.
	for taskID, a := range agg {
		if err := m.st.UpdateTask(taskID, store.TaskUpdate{
			Speed:           i64Ptr(a.speed),
			TotalBytes:      i64Ptr(a.total),
			DownloadedBytes: i64Ptr(a.downloaded),
		}); err != nil {
			m.log.Debug("update task speed", "task", taskID, "error", err)
		}
		t := m.reloadTask(taskID)
		if t != nil {
			m.hub.Publish(t.UserID, taskID, "task_progress", map[string]any{"task": t})
		}
	}

	// Broadcast individual file deltas (throttled by the poll interval).
	for _, f := range byGID {
		if f == nil {
			continue
		}
		t := m.reloadTask(f.TaskID)
		if t == nil {
			continue
		}
		if updated := m.reloadFile(f.ID); updated != nil {
			m.hub.Publish(t.UserID, f.TaskID, "file_progress", map[string]any{
				"task_id": f.TaskID, "file": updated,
			})
		}
	}

	// Reconcile completed / failed transitions.
	m.reconcile(ctx, client, agg)
}

// activeFiles loads every file that is bound to an aria2 download.
func (m *Manager) activeFiles() ([]*store.File, error) {
	return m.st.BoundFiles(5000)
}

// reconcile re-queues files that aria2 gave up on, respecting the retry budget,
// and lets runTask finalise tasks whose files are all terminal.
func (m *Manager) reconcile(ctx context.Context, client *aria2.Client, agg map[int64]*taskAgg) {
	tasks, err := m.st.AllActiveTasks()
	if err != nil {
		return
	}
	for _, t := range tasks {
		a := agg[t.ID]
		hasActivity := a != nil && (a.hasActive || a.terminating)

		if t.Status == store.TaskPaused {
			continue
		}
		// Retry files aria2 abandoned.
		failedFiles, err := m.st.FilesByStatus(t.ID, store.FileFailed)
		if err == nil {
			for _, f := range failedFiles {
				if f.RetryCount >= t.Options.MaxRetries || f.DownloadLink == "" {
					continue
				}
				// Only retry while the task still has work in flight.
				if a != nil && a.hasActive {
					continue
				}
				// Exponential backoff, capped, so a permanently broken link is
				// not retried in a tight loop.
				if wait := retryBackoff(f.RetryCount); !m.backoffElapsed(t.ID, f.ID, wait) {
					continue
				}
				if err := m.st.ResetFileForRetry(f.ID); err != nil {
					continue
				}
				if client != nil {
					dir := t.DownloadPath
					if dir == "" {
						dir = m.cfg.DownloadDir
					}
					opts := aria2.AddURIOptions{
						Dir:           dir,
						Out:           f.Filename,
						MaxConnection: t.Options.Connections,
						Continue:      true,
						Referer:       "https://dl.bunkrr.cr/",
						UserAgent:     m.cfg.UserAgent,
						CheckCert:     false,
						MaxTries:      3,
						RetryWait:     3,
					}
					if gid, err := client.AddURI(ctx, []string{f.DownloadLink}, opts); err == nil {
						_ = m.st.SetFileDownloading(f.ID, gid)
					}
				}
				m.logEventByID(t.ID, store.LevelWarn, "Retrying file",
					fmt.Sprintf("%s（第 %d 次）", f.Filename, f.RetryCount+1))
			}
		}

		// A runner exists and files remain in flight: nothing to do.
		if m.IsRunning(t.ID) {
			_ = hasActivity
			continue
		}
		// No runner: if anything is unfinished, (re)start the task.
		files, err := m.st.AllFilesForTask(t.ID)
		if err != nil {
			continue
		}
		if allTerminal(files) {
			m.finalizeTask(t.ID)
		}
	}
}

// publishStats pushes the aggregated dashboard numbers to every client.
func (m *Manager) publishStats() {
	ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
	defer cancel()

	var aria GlobalAriaStat
	if client := m.aria.Client(); client != nil {
		if st, err := client.GetGlobalStat(ctx); err == nil {
			aria = GlobalAriaStat{
				DownloadSpeed: st.DownloadSpeed.Int64(),
				Active:        int(st.NumActive.Int64()),
				Waiting:       int(st.NumWaiting.Int64()),
				Stopped:       int(st.NumStopped.Int64()),
				NumOfFiles:    st.NumOfFiles.Int64(),
			}
		}
	}
	aria.Available = m.aria.Healthy(ctx)

	m.mu.Lock()
	users := map[int64]bool{}
	for _, r := range m.runners {
		users[r.userID] = true
	}
	m.mu.Unlock()

	// Always broadcast to users with live downloads; for everyone else the
	// REST endpoint remains the source of truth.
	m.broadcastStats(users, aria)
}

func (m *Manager) broadcastStats(users map[int64]bool, aria GlobalAriaStat) {
	for userID := range users {
		stats, err := m.st.Stats(userID)
		if err != nil {
			continue
		}
		quota, err := m.st.Quota(userID)
		if err != nil {
			continue
		}
		payload := map[string]any{
			"stats": stats,
			"aria2": aria,
			"quota": quota,
		}
		m.hub.Publish(userID, 0, "stats", payload)
	}
}

// taskAgg accumulates per-task counters within a single poll sweep so the
// database is written once per task instead of once per file.
type taskAgg struct {
	speed       int64
	downloaded  int64
	total       int64
	hasActive   bool
	terminating bool
}

// retryBackoff returns how long to wait before attempt n+1 of a file.
func retryBackoff(attempt int) time.Duration {
	d := time.Duration(1<<minInt(attempt, 5)) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// backoffElapsed reports whether the per-file retry delay has passed, and
// records the attempt when it has.
func (m *Manager) backoffElapsed(taskID, fileID int64, wait time.Duration) bool {
	m.retryMu.Lock()
	defer m.retryMu.Unlock()
	if m.retryAt == nil {
		m.retryAt = make(map[string]time.Time)
	}
	key := fmt.Sprintf("%d/%d", taskID, fileID)
	now := time.Now()
	if until, seen := m.retryAt[key]; seen && now.Before(until) {
		return false
	}
	m.retryAt[key] = now.Add(wait)
	return true
}

// GlobalAriaStat is the aria2 portion of the stats frame.
type GlobalAriaStat struct {
	Available     bool  `json:"available"`
	DownloadSpeed int64 `json:"download_speed"`
	Active        int   `json:"active"`
	Waiting       int   `json:"waiting"`
	Stopped       int   `json:"stopped"`
	NumOfFiles    int64 `json:"num_of_files"`
}

// ariaError renders a human message from an aria2 error code.
func ariaError(code, msg string) string {
	if msg != "" {
		return msg
	}
	return "download failed (code " + code + ")"
}
