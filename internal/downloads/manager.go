// Package downloads orchestrates the end-to-end download pipeline:
//
//	crawl Bunkr page -> register files (quota) -> resolve signed links
//	-> hand off to aria2 -> poll progress -> persist -> broadcast
//
// A single manager owns every running task, enforces the per-plan concurrency
// limit and keeps SQLite in sync with the aria2 daemon.
package downloads

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkr"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// Config tunes the orchestrator.
type Config struct {
	DownloadDir string
	// MaxConcurrentPerUserFree / Member are enforced by the queue.
	FreeConcurrency   int
	MemberConcurrency int
	// ResolveConcurrency bounds concurrent item-page fetches per task.
	ResolveConcurrency int
	// PollInterval is the aria2 status refresh cadence.
	PollInterval time.Duration
	// UserAgent is sent with page and media requests.
	UserAgent string
	// DefaultOptions seeds tasks created without explicit options.
	DefaultOptions store.TaskOptions
	// Endpoints overrides the Bunkr services (mirrors, tests).
	Endpoints *bunkr.Endpoints
}

func (c *Config) applyDefaults() {
	if c.FreeConcurrency <= 0 {
		c.FreeConcurrency = 1
	}
	if c.MemberConcurrency <= 0 {
		c.MemberConcurrency = 5
	}
	if c.ResolveConcurrency <= 0 {
		c.ResolveConcurrency = 4
	}
	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}
	if c.DefaultOptions.MaxRetries <= 0 {
		c.DefaultOptions.MaxRetries = 5
	}
	if c.DefaultOptions.Connections <= 0 {
		c.DefaultOptions.Connections = 4
	}
}

// Manager coordinates all downloads.
type Manager struct {
	cfg  Config
	log  *slog.Logger
	st   *store.Store
	aria *aria2.Manager
	hub  *hub.Hub
	crwl *bunkr.Crawler

	mu      sync.Mutex
	runners map[int64]*runner
	queue   []*queueEntry

	// retryMu guards retryAt, the per-file retry backoff bookkeeping.
	retryMu sync.Mutex
	retryAt map[string]time.Time

	// globalLimit caps simultaneous downloads across all users.
	globalLimit int
	globalNow   atomic.Int32

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool
}

// runner is the per-task control block.
type runner struct {
	taskID int64
	userID int64

	cancel   context.CancelFunc
	done     chan struct{}
	paused   atomic.Bool
	canceled atomic.Bool
}

type queueEntry struct {
	taskID int64
	userID int64
	added  time.Time
}

// New builds a Manager. Call Start to begin polling.
func New(cfg Config, st *store.Store, aria *aria2.Manager, h *hub.Hub, log *slog.Logger) *Manager {
	cfg.applyDefaults()
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		cfg:         cfg,
		log:         log,
		st:          st,
		aria:        aria,
		hub:         h,
		crwl:        newCrawler(cfg),
		runners:     map[int64]*runner{},
		retryAt:     make(map[string]time.Time),
		globalLimit: 32,
	}
}

// Start launches the background poller and the queue dispatcher.
func (m *Manager) Start(parent context.Context) {
	m.mu.Lock()
	if m.ctx != nil {
		m.mu.Unlock()
		return // already started
	}
	m.mu.Unlock()

	m.ctx, m.cancel = context.WithCancel(parent)
	m.wg.Add(2)
	go m.pollLoop()
	go m.dispatchLoop()
	m.log.Info("download manager started", "pollInterval", m.cfg.PollInterval)
}

// Shutdown cancels every runner and waits for the loops to exit.
func (m *Manager) Shutdown(ctx context.Context) {
	if !m.closed.CompareAndSwap(false, true) {
		return
	}
	m.mu.Lock()
	ids := make([]int64, 0, len(m.runners))
	for id := range m.runners {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.stopRunner(ctx, id, store.TaskCanceled, "server shutting down")
	}

	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	m.log.Info("download manager stopped")
}

// ---------------------------------------------------------------- public API

// CreateTask registers a new task in the pending state.
func (m *Manager) CreateTask(ctx context.Context, userID int64, rawURL string, opts store.TaskOptions) (*store.Task, error) {
	normalized := bunkr.NormalizeURL(rawURL)
	if normalized == "" {
		return nil, &bunkr.ErrInvalidURL{URL: rawURL, Reason: "empty URL"}
	}
	kind, err := bunkr.ResolveURLType(normalized)
	if err != nil {
		return nil, err
	}
	merged := mergeOptions(opts, m.cfg.DefaultOptions)
	t, err := m.st.CreateTask(store.CreateTaskOptions{
		UserID: userID, URL: normalized, Kind: kind, Opts: merged,
	})
	if err != nil {
		return nil, err
	}
	m.logEvent(t, store.LevelInfo, "Task created", normalized)
	m.hub.Publish(userID, t.ID, "task_created", map[string]any{"task": t})
	return t, nil
}

// StartTask begins (or queues) a task. It respects the per-plan concurrency
// limit, parking extra work in the FIFO queue when the user is at capacity.
func (m *Manager) StartTask(ctx context.Context, taskID, userID int64) error {
	return m.startTask(ctx, taskID, userID, false)
}

// startTask is the shared entry point. allowPaused lets resume/retry pick up a
// task that is already in the paused state instead of rejecting it.
func (m *Manager) startTask(ctx context.Context, taskID, userID int64, allowPaused bool) error {
	t, err := m.st.TaskForUser(taskID, userID)
	if err != nil {
		return err
	}
	switch t.Status {
	case store.TaskRunning, store.TaskCrawling:
		return &InvalidStateError{Status: t.Status, Action: "start"}
	case store.TaskPaused:
		if !allowPaused {
			return &InvalidStateError{Status: t.Status, Action: "start"}
		}
	}
	if m.closed.Load() {
		return errors.New("download manager is shutting down")
	}
	if m.ctx == nil {
		return errors.New("download manager is not running")
	}

	m.mu.Lock()
	if _, busy := m.runners[taskID]; busy {
		m.mu.Unlock()
		return &InvalidStateError{Status: t.Status, Action: "start"}
	}
	quota, qerr := m.st.Quota(userID)
	if qerr != nil {
		m.mu.Unlock()
		return qerr
	}
	// Count live runners rather than trusting the database: a task row flips to
	// "running" a moment after start, and two links submitted back to back
	// would otherwise both squeeze past the limit.
	active := 0
	for _, r := range m.runners {
		if r.userID == userID {
			active++
		}
	}
	if quota.ConcurrentLimit > 0 && active >= quota.ConcurrentLimit {
		// Queue it: the dispatcher will pick it up when a slot frees.
		m.queue = append(m.queue, &queueEntry{taskID: taskID, userID: userID, added: time.Now()})
		m.mu.Unlock()
		m.st.UpdateTask(taskID, store.TaskUpdate{Status: strPtr(store.TaskPending)})
		m.logEventByID(taskID, store.LevelInfo, "Task queued",
			fmt.Sprintf("已达到同时下载上限（%d），排队等待中", quota.ConcurrentLimit))
		m.publishTask(taskID)
		return nil
	}
	m.mu.Unlock()

	m.launch(taskID, userID)
	return nil
}

// baseContext returns the manager's lifetime context. A request can arrive
// before Start (or after Shutdown), so it must never panic on a nil context.
func (m *Manager) baseContext() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *Manager) launch(taskID, userID int64) *runner {
	rctx, cancel := context.WithCancel(m.baseContext())
	r := &runner{
		taskID: taskID,
		userID: userID,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	m.mu.Lock()
	m.runners[taskID] = r
	m.mu.Unlock()
	m.globalNow.Add(1)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.globalNow.Add(-1)
		defer close(r.done)
		defer m.removeRunner(taskID, r)
		m.runTask(rctx, r)
	}()
	return r
}

func (m *Manager) removeRunner(taskID int64, r *runner) {
	m.mu.Lock()
	if cur, ok := m.runners[taskID]; ok && cur == r {
		delete(m.runners, taskID)
	}
	m.mu.Unlock()
}

// InvalidStateError signals an operation that the task state forbids.
type InvalidStateError struct {
	Status string
	Action string
}

func (e *InvalidStateError) Error() string {
	return fmt.Sprintf("cannot %s a task in state %q", e.Action, e.Status)
}

// Pause suspends a running task and all of its aria2 downloads.
//
// Only the byte transfers stop: a crawl that is already in flight finishes
// resolving links so that Resume can simply unpause instead of re-resolving.
func (m *Manager) Pause(ctx context.Context, taskID, userID int64) error {
	if _, err := m.st.TaskForUser(taskID, userID); err != nil {
		return err
	}
	m.mu.Lock()
	r, busy := m.runners[taskID]
	m.mu.Unlock()

	if busy {
		r.paused.Store(true)
	}
	if err := m.ariaEach(ctx, taskID, func(c *aria2.Client, gid string) error {
		return c.PauseAll(ctx, gid)
	}); err != nil {
		m.log.Warn("pause: some downloads could not be paused", "task", taskID, "error", err)
	}

	if err := m.st.UpdateTask(taskID, store.TaskUpdate{Status: strPtr(store.TaskPaused)}); err != nil {
		return err
	}
	m.logEventByID(taskID, store.LevelInfo, "Task paused", "已暂停下载")
	m.publishTask(taskID)
	return nil
}

// Resume restarts a paused task.
//
// When the runner is still alive the existing aria2 downloads are simply
// unpaused (true byte-level resume). After a process restart no runner exists,
// so the files are requeued from the persisted state instead.
func (m *Manager) Resume(ctx context.Context, taskID, userID int64) error {
	t, err := m.st.TaskForUser(taskID, userID)
	if err != nil {
		return err
	}
	switch t.Status {
	case store.TaskPaused, store.TaskFailed, store.TaskPending, store.TaskRunning, store.TaskCrawling:
	default:
		return &InvalidStateError{Status: t.Status, Action: "resume"}
	}

	if r, busy := m.runnerFor(taskID); busy {
		r.paused.Store(false)
		r.canceled.Store(false)
		if err := m.ariaEach(ctx, taskID, func(c *aria2.Client, gid string) error {
			return c.UnpauseAll(ctx, gid)
		}); err != nil {
			m.log.Warn("resume: some downloads could not be unpaused", "task", taskID, "error", err)
		}
		if err := m.st.UpdateTask(taskID, store.TaskUpdate{Status: strPtr(store.TaskRunning)}); err != nil {
			return err
		}
		m.logEventByID(taskID, store.LevelInfo, "Task resumed", "继续下载（断点续传）")
		m.publishTask(taskID)
		return nil
	}

	// No live runner: rebuild from the database.
	if err := m.st.MarkTaskFilesPending(taskID); err != nil {
		return err
	}
	m.logEventByID(taskID, store.LevelInfo, "Task resumed", "继续下载（从断点重建）")
	return m.startTask(ctx, taskID, userID, true)
}

// Cancel aborts a task and detaches all of its aria2 downloads.
func (m *Manager) Cancel(ctx context.Context, taskID, userID int64) error {
	if _, err := m.st.TaskForUser(taskID, userID); err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := m.st.UpdateTask(taskID, store.TaskUpdate{
		Status:     strPtr(store.TaskCanceled),
		FinishedAt: &now,
		Speed:      i64Ptr(0),
	}); err != nil {
		return err
	}
	if _, err := m.st.SkipRemaining(taskID, "已取消"); err != nil {
		return err
	}
	_ = m.st.RecomputeTaskStats(taskID)
	m.stopRunner(ctx, taskID, store.TaskCanceled, "已取消")
	if err := m.ariaEach(ctx, taskID, func(c *aria2.Client, gid string) error {
		if err := c.ForceRemove(ctx, gid); err != nil && !aria2.IsNotFoundError(err) {
			return err
		}
		_ = c.RemoveDownloadResult(ctx, gid)
		return nil
	}); err != nil {
		m.log.Warn("cancel: cleanup incomplete", "task", taskID, "error", err)
	}
	m.detachTaskGIDs(taskID)
	m.logEventByID(taskID, store.LevelWarn, "Task canceled", "任务已取消")
	m.publishTask(taskID)
	return nil
}

// Retry resets every failed file of a task and restarts it.
func (m *Manager) Retry(ctx context.Context, taskID, userID int64, fileIDs []int64) (int64, error) {
	if _, err := m.st.TaskForUser(taskID, userID); err != nil {
		return 0, err
	}
	var n int64
	var err error
	if len(fileIDs) > 0 {
		for _, id := range fileIDs {
			f, err := m.st.GetFile(id)
			if err != nil || f.TaskID != taskID {
				continue
			}
			if err := m.st.ResetFileForRetry(id); err != nil {
				return n, err
			}
			n++
		}
	} else {
		n, err = m.st.ResetTaskFailures(taskID)
		if err != nil {
			return 0, err
		}
	}
	_ = err
	if err := m.st.RecomputeTaskStats(taskID); err != nil {
		return n, err
	}
	m.logEventByID(taskID, store.LevelInfo, "Task retry", fmt.Sprintf("重新排队 %d 个文件", n))

	// A live runner is already parked in its wait loop, so the requeued files
	// must be handed to aria2 here instead of by a fresh start.
	if r, busy := m.runnerFor(taskID); busy {
		if !r.paused.Load() {
			if err := m.resubmitPending(ctx, taskID); err != nil {
				m.log.Warn("retry: resubmit failed", "task", taskID, "error", err)
			}
		}
		return n, nil
	}

	// No live runner: the task is finished or was interrupted, so restart it.
	if err := m.st.UpdateTask(taskID, store.TaskUpdate{
		Status:       strPtr(store.TaskPending),
		FinishedNull: true,
	}); err != nil {
		return n, err
	}
	if err := m.startTask(ctx, taskID, userID, true); err != nil {
		return n, err
	}
	return n, nil
}

// resubmitPending hands a task's freshly requeued files to aria2. It is used by
// retry when a runner already exists in its wait loop.
func (m *Manager) resubmitPending(ctx context.Context, taskID int64) error {
	task, err := m.st.GetTask(taskID)
	if err != nil {
		return err
	}
	files, err := m.st.DownloadableFiles(taskID)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	client := m.aria.Client()
	if client == nil {
		return errAria2Unavailable
	}
	dir := task.DownloadPath
	if dir == "" {
		dir = m.cfg.DownloadDir
	}
	for _, f := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		gid, err := m.addToAria2(ctx, client, task, f, dir)
		if err != nil {
			_ = m.st.UpdateFile(f.ID, store.FileUpdate{
				Status:       strPtr(store.FileFailed),
				ErrorMessage: strPtr(shortErr(err)),
			})
			continue
		}
		if err := m.st.SetFileDownloading(f.ID, gid); err != nil {
			m.log.Warn("mark file downloading", "file", f.ID, "error", err)
		}
	}
	return nil
}

// DeleteTask cancels and removes a task.
func (m *Manager) DeleteTask(ctx context.Context, taskID, userID int64) error {
	if _, err := m.st.TaskForUser(taskID, userID); err != nil {
		return err
	}
	now := time.Now().UTC()
	_ = m.st.UpdateTask(taskID, store.TaskUpdate{Status: strPtr(store.TaskCanceled), FinishedAt: &now})
	_ = m.ariaEach(ctx, taskID, func(c *aria2.Client, gid string) error {
		if err := c.ForceRemove(ctx, gid); err != nil && !aria2.IsNotFoundError(err) {
			return err
		}
		return nil
	})
	m.detachTaskGIDs(taskID)
	m.stopRunner(ctx, taskID, store.TaskCanceled, "")
	if err := m.st.DeleteTask(taskID); err != nil {
		return err
	}
	m.hub.Publish(userID, taskID, "task_deleted", map[string]any{"task_id": taskID})
	return nil
}

func (m *Manager) runnerFor(taskID int64) (*runner, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runners[taskID]
	return r, ok
}

func (m *Manager) stopRunner(ctx context.Context, taskID int64, status, reason string) {
	m.mu.Lock()
	r, ok := m.runners[taskID]
	m.mu.Unlock()
	if !ok {
		return
	}
	r.canceled.Store(true)
	r.cancel()

	select {
	case <-r.done:
	case <-time.After(12 * time.Second):
		m.log.Warn("runner did not stop in time", "task", taskID)
	}
	if status != "" {
		now := time.Now().UTC()
		_ = m.st.UpdateTask(taskID, store.TaskUpdate{Status: &status, FinishedAt: &now, Speed: i64Ptr(0)})
	}
	_ = reason
}

// IsRunning reports whether a task currently occupies a runner slot.
func (m *Manager) IsRunning(taskID int64) bool {
	_, ok := m.runnerFor(taskID)
	return ok
}

// RunningCount returns how many tasks are active for a user.
func (m *Manager) RunningCount(userID int64) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.runners {
		if r.userID == userID {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------- dispatcher

// dispatchLoop promotes queued tasks into runners as slots free up.
func (m *Manager) dispatchLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.dispatch()
		}
	}
}

func (m *Manager) dispatch() {
	for {
		m.mu.Lock()
		if len(m.queue) == 0 {
			m.mu.Unlock()
			return
		}
		// Drop entries whose task vanished.
		kept := m.queue[:0]
		for _, e := range m.queue {
			if _, ok := m.runners[e.taskID]; ok {
				continue
			}
			kept = append(kept, e)
		}
		m.queue = kept
		if len(m.queue) == 0 {
			m.mu.Unlock()
			return
		}
		if int(m.globalNow.Load()) >= m.globalLimit {
			m.mu.Unlock()
			return
		}
		entry := m.queue[0]
		m.queue = m.queue[1:]

		quota, err := m.st.Quota(entry.userID)
		if err != nil {
			m.mu.Unlock()
			continue
		}
		busy := 0
		for _, r := range m.runners {
			if r.userID == entry.userID {
				busy++
			}
		}
		if quota.ConcurrentLimit > 0 && busy >= quota.ConcurrentLimit {
			// Put it back at the end so other users get a chance.
			m.queue = append(m.queue, entry)
			m.mu.Unlock()
			if len(m.queue) == 1 {
				return // only this user's tasks are blocked
			}
			continue
		}
		m.mu.Unlock()

		t, err := m.st.GetTask(entry.taskID)
		if err != nil {
			continue
		}
		if t.Status == store.TaskCanceled {
			continue
		}
		m.launch(entry.taskID, entry.userID)
	}
}

// errAria2Unavailable is returned when the daemon is not ready.
var errAria2Unavailable = errors.New("aria2 unavailable")

// newCrawler builds the page crawler honouring any endpoint override.
func newCrawler(cfg Config) *bunkr.Crawler {
	hc := bunkr.NewHTTPClient(cfg.UserAgent)
	if cfg.Endpoints != nil {
		return bunkr.NewCrawlerWith(hc, *cfg.Endpoints)
	}
	return bunkr.NewCrawler(hc)
}
