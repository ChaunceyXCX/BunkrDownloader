package downloads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkr"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// runTask executes one task end to end: crawl, resolve links, enqueue into
// aria2, then wait until every file reaches a terminal state.
func (m *Manager) runTask(ctx context.Context, r *runner) {
	taskID := r.taskID
	task, err := m.st.GetTask(taskID)
	if err != nil {
		m.log.Error("task lookup failed", "task", taskID, "error", err)
		return
	}

	if err := m.st.UpdateTask(taskID, store.TaskUpdate{
		Status:      strPtr(store.TaskRunning),
		Speed:       i64Ptr(0),
		StartedNull: false,
	}); err != nil {
		m.log.Error("task status update failed", "task", taskID, "error", err)
	}
	if task.StartedAt == nil {
		now := time.Now().UTC()
		_ = m.st.UpdateTask(taskID, store.TaskUpdate{StartedAt: &now})
	}
	m.logEventByID(taskID, store.LevelInfo, "Task started", task.URL)
	m.publishTask(taskID)

	// Files left "downloading" by a previous process are resumable.
	if err := m.st.MarkTaskFilesPending(taskID); err != nil {
		m.log.Warn("reset stale files", "task", taskID, "error", err)
	}

	// Phase 1: discover the files to download. This also fills in the album
	// name, which the download directory is derived from, so it must run first.
	if err := m.discoverFiles(ctx, r, task); err != nil {
		if ctx.Err() != nil {
			return
		}
		m.failTask(taskID, err.Error())
		return
	}
	if fresh, err := m.st.GetTask(taskID); err == nil {
		task = fresh // pick up album name / id discovered above
	}

	downloadDir, err := m.resolveDownloadDir(task)
	if err != nil {
		m.failTask(taskID, err.Error())
		return
	}
	if err := m.st.UpdateTask(taskID, store.TaskUpdate{DownloadPath: strPtr(downloadDir)}); err != nil {
		m.log.Warn("store download path", "task", taskID, "error", err)
	}
	task.DownloadPath = downloadDir
	if err := m.st.RecomputeTaskStats(taskID); err != nil {
		m.log.Warn("recompute stats", "task", taskID, "error", err)
	}
	m.publishTask(taskID)

	// Discovery is done: back to running so the UI stops showing "crawling".
	if !r.paused.Load() && !r.canceled.Load() {
		if err := m.st.UpdateTask(taskID, store.TaskUpdate{Status: strPtr(store.TaskRunning)}); err != nil {
			m.log.Warn("set running", "task", taskID, "error", err)
		}
	}

	// Phase 2: resolve signed links and hand the files to aria2.
	if err := m.enqueueFiles(ctx, r, task, downloadDir); err != nil {
		if ctx.Err() != nil {
			return
		}
		m.log.Warn("enqueue incomplete", "task", taskID, "error", err)
	}

	// Phase 3: wait for completion; the poller drives status updates.
	m.waitForCompletion(ctx, r, taskID)
}

// discoverFiles crawls the task URL and registers its files in SQLite.
func (m *Manager) discoverFiles(ctx context.Context, r *runner, task *store.Task) error {
	// Resume path: if the album was already crawled, keep the known file list.
	existing, err := m.st.AllFilesForTask(task.ID)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		pending := 0
		for _, f := range existing {
			if f.Status == store.FilePending || f.Status == store.FileFailed {
				pending++
			}
		}
		if pending > 0 || allTerminal(existing) {
			// The file list is already known, but a previous process may have
			// died before the album name was recorded; fill it in now.
			if err := m.ensureAlbumMeta(ctx, task); err != nil {
				m.log.Warn("album metadata", "task", task.ID, "error", err)
			}
			m.logEventByID(task.ID, store.LevelInfo, "Using cached album state",
				fmt.Sprintf("复用已登记的 %d 个文件（待下载 %d 个）", len(existing), pending))
			return nil
		}
	}

	if err := m.st.UpdateTask(task.ID, store.TaskUpdate{Status: strPtr(store.TaskCrawling)}); err != nil {
		return err
	}
	m.publishTask(task.ID)

	doc, err := m.crwlHTTP().FetchPage(ctx, task.URL)
	if err != nil {
		return fmt.Errorf("无法打开页面: %w", err)
	}
	albumName := bunkr.AlbumName(doc)

	items, err := m.crwl.AlbumItems(ctx, task.URL, doc)
	if err != nil {
		if !bunkr.IsAlbum(task.URL) {
			// A single media page behaves like a one-item album.
			items = []bunkr.Item{{URL: task.URL}}
		} else {
			return err
		}
	}

	albumID := ""
	if bunkr.IsAlbum(task.URL) {
		albumID = bunkr.AlbumID(task.URL)
	}
	if albumName != "" || albumID != "" {
		_ = m.st.UpdateTask(task.ID, store.TaskUpdate{
			AlbumName: strPtr(albumName),
			AlbumID:   strPtr(albumID),
		})
	}

	m.logEventByID(task.ID, store.LevelInfo, "Album crawled",
		fmt.Sprintf("发现 %d 个文件", len(items)))

	// Every item is registered; the include/ignore rules and the free-tier
	// file quota are applied once the real file name is known, so a filtered
	// file never consumes the user's allowance.
	if len(items) == 0 {
		return fmt.Errorf("该链接下没有可下载的文件")
	}

	newFiles := make([]store.NewFile, 0, len(items))
	for _, it := range items {
		newFiles = append(newFiles, store.NewFile{
			TaskID:   task.ID,
			UserID:   task.UserID,
			ItemURL:  it.URL,
			ItemDate: it.ItemDate,
		})
	}
	if _, err := m.st.RegisterFiles(newFiles); err != nil {
		return fmt.Errorf("register files: %w", err)
	}
	return nil
}

func allTerminal(files []*store.File) bool {
	for _, f := range files {
		switch f.Status {
		case store.FileCompleted, store.FileSkipped:
		default:
			return false
		}
	}
	return true
}

// filterReason applies the include/ignore rules to a resolved file name.
// It returns the skip reason and whether the file should be dropped.
func filterReason(filename string, opts store.TaskOptions) (string, bool) {
	name := strings.ToLower(filename)
	for _, w := range opts.Ignore {
		w = strings.ToLower(strings.TrimSpace(w))
		if w != "" && strings.Contains(name, w) {
			return "文件名包含忽略词 " + w, true
		}
	}
	if len(opts.Include) > 0 {
		for _, w := range opts.Include {
			w = strings.ToLower(strings.TrimSpace(w))
			if w != "" && strings.Contains(name, w) {
				return "", false
			}
		}
		return "文件名不包含任何包含词", true
	}
	return "", false
}

// enqueueFiles resolves the download link of every pending file and hands the
// result to aria2.
func (m *Manager) enqueueFiles(ctx context.Context, r *runner, task *store.Task, downloadDir string) error {
	if err := m.st.RecomputeTaskStats(task.ID); err != nil {
		return err
	}
	pending, err := m.st.DownloadableFiles(task.ID)
	if err != nil {
		return err
	}
	needLinks, err := m.st.PendingFilesNeedingLink(task.ID)
	if err != nil {
		return err
	}
	if len(needLinks) > 0 {
		m.resolveLinks(ctx, r, task, needLinks)
		pending, err = m.st.DownloadableFiles(task.ID)
		if err != nil {
			return err
		}
	}
	if len(pending) == 0 {
		return nil
	}

	client := m.aria.Client()
	if client == nil {
		return fmt.Errorf("aria2 未就绪，无法开始下载")
	}

	// Free-tier file cap: files beyond the allowance are skipped with a clear
	// reason instead of silently dropped.
	// Exclude this task's own pending rows: they are what we are deciding about.
	if remaining := m.st.RemainingFilesExcluding(task.UserID, task.ID); remaining >= 0 && len(pending) > remaining {
		over := len(pending) - remaining
		if remaining <= 0 {
			m.logEventByID(task.ID, store.LevelError, "Quota exceeded",
				fmt.Sprintf("免费用户最多下载 %d 个文件，已用完。开通会员可解锁无限下载。",
					m.st.FreeLimits().Files))
		} else {
			m.logEventByID(task.ID, store.LevelWarn, "Quota truncated",
				fmt.Sprintf("免费额度剩余 %d 个文件，本任务仅下载 %d 个（其余 %d 个已跳过）。开通会员解锁无限下载。",
					remaining, remaining, over))
		}
		for _, extra := range pending[remaining:] {
			_ = m.st.UpdateFile(extra.ID, store.FileUpdate{
				Status:       strPtr(store.FileSkipped),
				ErrorMessage: strPtr("超出免费用户文件额度，开通会员后可继续下载"),
			})
		}
		pending = pending[:remaining]
	}

	if len(pending) == 0 {
		_ = m.st.RecomputeTaskStats(task.ID)
		return nil
	}

	m.logEventByID(task.ID, store.LevelInfo, "Enqueuing downloads",
		fmt.Sprintf("向 aria2 提交 %d 个下载任务", len(pending)))

	var (
		mu       sync.Mutex
		failures int
		firstErr error
	)
	sem := make(chan struct{}, m.cfg.ResolveConcurrency)
	var wg sync.WaitGroup

	for _, f := range pending {
		if r.canceled.Load() {
			break
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(file *store.File) {
			defer wg.Done()
			defer func() { <-sem }()

			gid, err := m.addToAria2(ctx, client, task, file, downloadDir)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				if firstErr == nil {
					firstErr = err
				}
				_ = m.st.UpdateFile(file.ID, store.FileUpdate{
					Status:       strPtr(store.FileFailed),
					ErrorMessage: strPtr(err.Error()),
				})
				return
			}
			if err := m.st.SetFileDownloading(file.ID, gid); err != nil {
				m.log.Warn("mark file downloading", "file", file.ID, "error", err)
			}
			m.hub.Publish(task.UserID, task.ID, "file_created", map[string]any{
				"task_id": task.ID, "file": m.reloadFile(file.ID),
			})
		}(f)
	}
	wg.Wait()

	if failures > 0 {
		m.logEventByID(task.ID, store.LevelError, "Enqueue failed",
			fmt.Sprintf("%d 个文件提交失败：%v", failures, firstErr))
	}
	return nil
}

// addToAria2 submits a single file to the daemon.
func (m *Manager) addToAria2(ctx context.Context, client *aria2.Client, task *store.Task, file *store.File, downloadDir string) (string, error) {
	name := m.uniqueName(task, file)
	opts := aria2.AddURIOptions{
		Dir:              downloadDir,
		Out:              name,
		MaxConnection:    task.Options.Connections,
		Split:            "4",
		MinSplitSize:     "1M",
		Continue:         true,
		AutoFileRenaming: false,
		AllowOverwrite:   false,
		Referer:          m.crwl.Endpoints().Referer,
		UserAgent:        m.cfg.UserAgent,
		Header:           []string{"Referer: " + m.crwl.Endpoints().Referer},
		Timeout:          60,
		ConnectTimeout:   20,
		MaxTries:         3,
		RetryWait:        3,
		CheckCert:        false,
	}
	if task.Options.RateLimitKbps > 0 {
		opts.MaxLimitRate = fmt.Sprintf("%dK", task.Options.RateLimitKbps)
	}
	return client.AddURI(ctx, []string{file.DownloadLink}, opts)
}

// uniqueName keeps file names collision-free inside one task directory.
func (m *Manager) uniqueName(task *store.Task, file *store.File) string {
	base := bunkr.TruncateFilename(file.Filename)
	if base == "" {
		base = fmt.Sprintf("file_%d", file.ID)
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return stem + ext
}

// resolveLinks fetches every item page and stores the signed download URL.
// Failures mark the file failed so the user can retry it individually.
func (m *Manager) resolveLinks(ctx context.Context, r *runner, task *store.Task, files []*store.File) {
	if len(files) == 0 {
		return
	}
	m.logEventByID(task.ID, store.LevelInfo, "Resolving links",
		fmt.Sprintf("解析 %d 个文件的直链", len(files)))

	opts := task.Options
	sem := make(chan struct{}, m.cfg.ResolveConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	resolved, failed, skipped := 0, 0, 0

	for _, f := range files {
		if r.canceled.Load() {
			break
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(file *store.File) {
			defer wg.Done()
			defer func() { <-sem }()

			item := bunkr.Item{URL: file.ItemURL}
			resolvedItem, err := m.crwl.ResolveItem(ctx, item, opts.CleanName)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				// Filters run on the real file name: Bunkr slugs are random ids
				// and carry no extension, so matching on the URL is useless.
				if reason, filtered := filterReason(resolvedItem.Filename, opts); filtered {
					skipped++
					_ = m.st.UpdateFile(file.ID, store.FileUpdate{
						Status:       strPtr(store.FileSkipped),
						Filename:     strPtr(resolvedItem.Filename),
						ErrorMessage: strPtr(reason),
					})
					return
				}
			}
			if err != nil {
				failed++
				_ = m.st.UpdateFile(file.ID, store.FileUpdate{
					Status:       strPtr(store.FileFailed),
					ErrorMessage: strPtr(shortErr(err)),
				})
				return
			}
			upd := store.FileUpdate{
				Filename:     strPtr(resolvedItem.Filename),
				DownloadLink: strPtr(resolvedItem.Link),
			}
			if resolvedItem.Size > 0 {
				upd.FileSize = i64Ptr(resolvedItem.Size)
			}
			_ = m.st.UpdateFile(file.ID, upd)
			resolved++
		}(f)
	}
	wg.Wait()

	level := store.LevelInfo
	details := fmt.Sprintf("已解析 %d 个直链", resolved)
	if failed > 0 {
		level = store.LevelWarn
		details += fmt.Sprintf("，%d 个失败", failed)
	}
	if skipped > 0 {
		details += fmt.Sprintf("，%d 个被过滤规则跳过", skipped)
	}
	m.logEventByID(task.ID, level, "Links resolved", details)
	_ = m.st.RecomputeTaskStats(task.ID)
	m.publishTask(task.ID)
}

// waitForCompletion blocks until every file of the task is terminal.
func (m *Manager) waitForCompletion(ctx context.Context, r *runner, taskID int64) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		files, err := m.st.AllFilesForTask(taskID)
		if err != nil {
			m.log.Warn("poll task files", "task", taskID, "error", err)
			return
		}
		if len(files) == 0 {
			m.finalizeTask(taskID)
			return
		}
		if allTerminal(files) {
			m.finalizeTask(taskID)
			return
		}
		if r.paused.Load() {
			continue // keep waiting; the poller keeps aria2 paused
		}
		if !m.anyActive(files) {
			// Nothing is moving: if aria2 is gone the task cannot progress.
			if m.aria.Client() == nil || !m.aria.Healthy(ctx) {
				m.failTask(taskID, "aria2 服务不可用，任务已停止")
				return
			}
		}
	}
}

func (m *Manager) anyActive(files []*store.File) bool {
	for _, f := range files {
		if f.Status == store.FileDownloading {
			return true
		}
	}
	return false
}

// finalizeTask writes the terminal status once every file resolved.
func (m *Manager) finalizeTask(taskID int64) {
	files, err := m.st.AllFilesForTask(taskID)
	if err != nil {
		return
	}
	failed := 0
	for _, f := range files {
		if f.Status == store.FileFailed {
			failed++
		}
	}
	status := store.TaskCompleted
	if failed > 0 {
		status = store.TaskFailed
	}
	now := time.Now().UTC()
	if err := m.st.UpdateTask(taskID, store.TaskUpdate{
		Status: &status, FinishedAt: &now, Speed: i64Ptr(0),
	}); err != nil {
		return
	}
	_ = m.st.RecomputeTaskStats(taskID)

	level, msg := store.LevelSuccess, "全部完成"
	detail := fmt.Sprintf("成功 %d 个", len(files)-failed)
	if failed > 0 {
		level, msg = store.LevelError, "完成但有失败"
		detail = fmt.Sprintf("成功 %d 个，失败 %d 个", len(files)-failed, failed)
	}
	m.logEventByID(taskID, level, "Task "+msg, detail)
	m.publishTask(taskID)
	m.hub.Publish(m.userOf(taskID), taskID, "task_completed", map[string]any{
		"task": m.reloadTask(taskID),
	})
}

func (m *Manager) failTask(taskID int64, reason string) {
	now := time.Now().UTC()
	status := store.TaskFailed
	_ = m.st.UpdateTask(taskID, store.TaskUpdate{
		Status: &status, ErrorMessage: strPtr(reason), FinishedAt: &now, Speed: i64Ptr(0),
	})
	_, _ = m.st.SkipRemaining(taskID, "任务失败")
	_ = m.st.RecomputeTaskStats(taskID)
	m.logEventByID(taskID, store.LevelError, "Task failed", reason)
	m.publishTask(taskID)
	m.hub.Publish(m.userOf(taskID), taskID, "task_completed", map[string]any{
		"task": m.reloadTask(taskID),
	})
}

func (m *Manager) userOf(taskID int64) int64 {
	if t, err := m.st.GetTask(taskID); err == nil {
		return t.UserID
	}
	return 0
}

// resolveDownloadDir computes and creates the target directory for a task.
func (m *Manager) resolveDownloadDir(task *store.Task) (string, error) {
	base := task.Options.CustomPath
	if base == "" {
		base = m.cfg.DownloadDir
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("创建下载目录失败: %w", err)
	}

	if !task.Options.NoAlbumFolder && bunkr.IsAlbum(task.URL) {
		name := bunkr.FormatDirectoryName(task.AlbumName, task.AlbumID)
		if name == "" {
			name = bunkr.AlbumID(task.URL)
		}
		dir := filepath.Join(base, bunkr.SanitizeDirectoryName(name))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("创建相册目录失败: %w", err)
		}
		return dir, nil
	}
	return base, nil
}

// crwlHTTP exposes the crawler's HTTP client (used for the first page fetch).
func (m *Manager) crwlHTTP() *bunkr.HTTPClient { return m.crwl.HTTPClient() }

// ensureAlbumMeta fetches the landing page when the album name is still unknown
// so a resumed task still lands in a correctly named directory.
func (m *Manager) ensureAlbumMeta(ctx context.Context, task *store.Task) error {
	if !bunkr.IsAlbum(task.URL) {
		return nil
	}
	if task.AlbumName != "" {
		return nil
	}
	doc, err := m.crwlHTTP().FetchPage(ctx, task.URL)
	if err != nil {
		return err
	}
	upd := store.TaskUpdate{AlbumID: strPtr(bunkr.AlbumID(task.URL))}
	if name := bunkr.AlbumName(doc); name != "" {
		upd.AlbumName = strPtr(name)
	}
	return m.st.UpdateTask(task.ID, upd)
}
