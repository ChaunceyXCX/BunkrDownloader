package downloads

import (
	"context"
	"strings"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// Aria2Stats returns the current daemon counters for the health/stats endpoints.
func (m *Manager) Aria2Stats(ctx context.Context) GlobalAriaStat {
	out := GlobalAriaStat{Available: false}
	client := m.aria.Client()
	if client == nil {
		return out
	}
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	st, err := client.GetGlobalStat(rctx)
	if err != nil {
		return out
	}
	return GlobalAriaStat{
		Available:     true,
		DownloadSpeed: st.DownloadSpeed.Int64(),
		Active:        int(st.NumActive.Int64()),
		Waiting:       int(st.NumWaiting.Int64()),
		Stopped:       int(st.NumStopped.Int64()),
		NumOfFiles:    st.NumOfFiles.Int64(),
	}
}

// ariaEach runs fn for every GID currently bound to a task's files. The
// callback receives a per-call context with a bounded timeout.
//
// It deliberately does NOT touch the database: pause/resume must keep the GID
// so the download stays attached to its file row.
func (m *Manager) ariaEach(ctx context.Context, taskID int64, fn func(*aria2.Client, string) error) error {
	return m.ariaEachCtx(ctx, taskID, func(c *aria2.Client, gid string, _ context.Context) error {
		return fn(c, gid)
	})
}

func (m *Manager) ariaEachCtx(ctx context.Context, taskID int64, fnWithCtx func(*aria2.Client, string, context.Context) error) error {
	client := m.aria.Client()
	if client == nil {
		return nil
	}
	files, err := m.st.AllFilesForTask(taskID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, f := range files {
		if f.GID == "" {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := fnWithCtx(client, f.GID, rctx)
		cancel()
		if err != nil && !aria2.IsNotFoundError(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// detachTaskGIDs clears the aria2 handle from every file of a task. It is used
// only after the downloads have been removed from the daemon, so the rows are
// no longer tracking a live transfer.
func (m *Manager) detachTaskGIDs(taskID int64) {
	files, err := m.st.AllFilesForTask(taskID)
	if err != nil {
		return
	}
	for _, f := range files {
		if f.GID == "" {
			continue
		}
		if err := m.st.UpdateFile(f.ID, store.FileUpdate{
			GID:   strPtr(""),
			Speed: i64Ptr(0),
		}); err != nil {
			m.log.Warn("detach gid", "file", f.ID, "error", err)
		}
	}
}

// ---------------------------------------------------------------- utilities

func strPtr(s string) *string { return &s }
func i64Ptr(v int64) *int64   { return &v }

// mergeOptions fills unset fields from the server defaults.
func mergeOptions(user, defaults store.TaskOptions) store.TaskOptions {
	out := user
	if out.MaxRetries <= 0 {
		out.MaxRetries = defaults.MaxRetries
	}
	if out.Connections <= 0 {
		out.Connections = defaults.Connections
	}
	if out.Ignore == nil {
		out.Ignore = []string{}
	}
	if out.Include == nil {
		out.Include = []string{}
	}
	return out
}

// shortErr trims an error string to something log-friendly.
func shortErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}

// logEvent appends a task-scoped event and mirrors it to connected clients.
func (m *Manager) logEvent(task *store.Task, level, event, details string) {
	if task == nil {
		return
	}
	m.logEventByID(task.ID, level, event, details)
}

func (m *Manager) logEventByID(taskID int64, level, event, details string) {
	userID := m.userOf(taskID)
	id, err := m.st.LogEvent(userID, taskID, 0, level, event, details)
	if err != nil {
		return
	}
	m.hub.Publish(userID, taskID, "log", map[string]any{
		"task_id": taskID,
		"event": map[string]any{
			"id": id, "task_id": taskID, "level": level,
			"event": event, "details": details,
			"created_at": timeNowUTC(),
		},
	})
}

// publishTask pushes the current task snapshot to the UI.
func (m *Manager) publishTask(taskID int64) {
	t := m.reloadTask(taskID)
	if t == nil {
		return
	}
	m.hub.Publish(t.UserID, taskID, "task_updated", map[string]any{"task": t})
}

func (m *Manager) reloadTask(taskID int64) *store.Task {
	t, err := m.st.GetTask(taskID)
	if err != nil {
		return nil
	}
	return t
}

func (m *Manager) reloadFile(fileID int64) *store.File {
	f, err := m.st.GetFile(fileID)
	if err != nil {
		return nil
	}
	return f
}

// timeNowUTC is the clock used for event timestamps.
func timeNowUTC() time.Time { return time.Now().UTC() }

// Aria2Healthy reports whether the aria2 daemon answers RPC right now.
func (m *Manager) Aria2Healthy(ctx context.Context) bool { return m.aria.Healthy(ctx) }

// Aria2Version returns the running aria2c version string.
func (m *Manager) Aria2Version(ctx context.Context) string { return m.aria.Version(ctx) }

// RestartAria2 replaces the aria2c process. Queued downloads keep their
// persisted state and are re-queued by the poller once the engine is back.
func (m *Manager) RestartAria2(ctx context.Context) bool {
	if err := m.aria.Stop(ctx); err != nil {
		m.log.Warn("stopping aria2 for restart", "error", err)
	}
	if err := m.aria.Start(ctx); err != nil {
		m.log.Error("restarting aria2", "error", err)
		return false
	}
	// Detach the stale GIDs so the files are resubmitted against the new daemon.
	if err := m.st.DetachAllGIDs(); err != nil {
		m.log.Warn("detaching gids after restart", "error", err)
	}
	return true
}
