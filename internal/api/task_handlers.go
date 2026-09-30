package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkr"
	"github.com/chaunceyxie1/BunkrDownloader/internal/downloads"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

type createTaskRequest struct {
	URL       string             `json:"url"`
	Options   *store.TaskOptions `json:"options"`
	AutoStart *bool              `json:"auto_start"`
}

// handleCreateTask accepts one URL or a newline/array separated batch.
//
// The whole batch is validated against the link quota before any task row is
// created, so a partially-quota'd submission is rejected as a whole.
func (s *Server) handleCreateTask(c *gin.Context) {
	user := currentUser(c)

	var req struct {
		URL       any                `json:"url"`
		URLs      []string           `json:"urls"`
		Options   *store.TaskOptions `json:"options"`
		AutoStart *bool              `json:"auto_start"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, CodeBadRequest, "请求格式不正确")
		return
	}

	urls := parseURLInput(req.URL, req.URLs)
	if len(urls) == 0 {
		fail(c, http.StatusBadRequest, CodeBadRequest, "请至少提供一个链接")
		return
	}
	if len(urls) > 100 {
		fail(c, http.StatusBadRequest, CodeBadRequest, "单次最多提交 100 个链接")
		return
	}

	// Normalise + validate every URL before touching the quota.
	normalized := make([]string, 0, len(urls))
	for _, raw := range urls {
		u := bunkr.NormalizeURL(raw)
		if u == "" {
			continue
		}
		if !bunkr.IsHTTPScheme(u) {
			fail(c, http.StatusBadRequest, CodeInvalidURL,
				"无法识别的链接："+raw+"（仅支持 http/https 地址）")
			return
		}
		if _, err := bunkr.ResolveURLType(u); err != nil {
			fail(c, http.StatusBadRequest, CodeInvalidURL,
				"无法识别的链接："+raw+"（请确认是 Bunkr 相册 /a/ 或文件 /v/ 地址）")
			return
		}
		normalized = append(normalized, u)
	}
	if len(normalized) == 0 {
		fail(c, http.StatusBadRequest, CodeInvalidURL, "没有有效的链接")
		return
	}

	if err := s.store.CanAddLinks(user.ID, len(normalized)); err != nil {
		failStoreError(c, err)
		return
	}

	opts := store.DefaultTaskOptions()
	if req.Options != nil {
		opts = sanitizeOptions(*req.Options)
	}
	autoStart := true
	if req.AutoStart != nil {
		autoStart = *req.AutoStart
	}

	created := make([]*store.Task, 0, len(normalized))
	for _, u := range normalized {
		t, err := s.manager.CreateTask(c.Request.Context(), user.ID, u, opts)
		if err != nil {
			failStoreError(c, err)
			return
		}
		created = append(created, t)
	}

	if autoStart {
		// Sequential submission keeps the free tier's concurrency quota honest.
		for _, t := range created {
			if err := s.manager.StartTask(c.Request.Context(), t.ID, user.ID); err != nil {
				s.log.Warn("auto start task", "task", t.ID, "error", err)
			}
		}
	}

	quota, err := s.store.Quota(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	ids := make([]int64, 0, len(created))
	for _, t := range created {
		ids = append(ids, t.ID)
	}
	resp := gin.H{"task_ids": ids, "count": len(ids), "quota": quota, "status": store.TaskPending}
	if len(ids) == 1 {
		resp["task_id"] = ids[0]
		resp["task"] = created[0]
	}
	c.JSON(http.StatusCreated, resp)
}

// parseURLInput accepts a string (newline separated) or a list.
func parseURLInput(raw any, list []string) []string {
	var out []string
	appendOne := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	switch v := raw.(type) {
	case string:
		for _, line := range strings.FieldsFunc(v, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ' '
		}) {
			appendOne(line)
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				appendOne(s)
			}
		}
	}
	for _, s := range list {
		appendOne(s)
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

// sanitizeOptions clamps client-supplied options into safe ranges.
func sanitizeOptions(in store.TaskOptions) store.TaskOptions {
	out := in
	if out.MaxRetries <= 0 {
		out.MaxRetries = 5
	}
	if out.MaxRetries > 20 {
		out.MaxRetries = 20
	}
	if out.Connections <= 0 {
		out.Connections = 4
	}
	if out.Connections > 16 {
		out.Connections = 16
	}
	if out.RateLimitKbps < 0 {
		out.RateLimitKbps = 0
	}
	out.Ignore = cleanList(out.Ignore)
	out.Include = cleanList(out.Include)
	if len(out.CustomPath) > 400 {
		out.CustomPath = out.CustomPath[:400]
	}
	return out
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && len(s) <= 100 {
			out = append(out, s)
		}
		if len(out) >= 50 {
			break
		}
	}
	return out
}

// handleListTasks returns a filtered, paginated task list.
func (s *Server) handleListTasks(c *gin.Context) {
	user := currentUser(c)
	tasks, total, err := s.store.ListTasks(store.ListTasksParams{
		UserID: user.ID,
		Status: strings.TrimSpace(c.Query("status")),
		Query:  c.Query("q"),
		Limit:  queryInt(c, "limit", 50),
		Offset: queryInt(c, "offset", 0),
		Sort:   c.Query("sort"),
		Dir:    c.Query("dir"),
	})
	if err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "total": total})
}

// handleGetTask returns a single task with its stats and file summary.
func (s *Server) handleGetTask(c *gin.Context) {
	user := currentUser(c)
	id, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
		return
	}
	task, err := s.store.TaskForUser(id, user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	files, err := s.store.AllFilesForTask(id)
	if err != nil {
		failStoreError(c, err)
		return
	}
	summary := gin.H{}
	for _, f := range files {
		if n, ok := summary[f.Status].(int); ok {
			summary[f.Status] = n + 1
		} else {
			summary[f.Status] = 1
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"task":          task,
		"stats":         task,
		"files_summary": summary,
	})
}

// handleDeleteTask removes a task and its files.
func (s *Server) handleDeleteTask(c *gin.Context) {
	user := currentUser(c)
	id, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
		return
	}
	if err := s.manager.DeleteTask(c.Request.Context(), id, user.ID); err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": id})
}

// handleTaskAction starts a task (gin passes the target status for the log line).
func (s *Server) handleTaskAction(_ string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := currentUser(c)
		id, ok := pathInt64(c, "id")
		if !ok {
			fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
			return
		}
		if err := s.manager.StartTask(c.Request.Context(), id, user.ID); err != nil {
			failManagerError(c, err)
			return
		}
		s.respondTask(c, id, user.ID)
	}
}

// handlePause suspends a running task.
func (s *Server) handlePause(c *gin.Context) {
	user := currentUser(c)
	id, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
		return
	}
	if err := s.manager.Pause(c.Request.Context(), id, user.ID); err != nil {
		failManagerError(c, err)
		return
	}
	s.respondTask(c, id, user.ID)
}

// handleResume restarts a paused task.
func (s *Server) handleResume(c *gin.Context) {
	user := currentUser(c)
	id, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
		return
	}
	if err := s.manager.Resume(c.Request.Context(), id, user.ID); err != nil {
		failManagerError(c, err)
		return
	}
	s.respondTask(c, id, user.ID)
}

// handleCancel aborts a task.
func (s *Server) handleCancel(c *gin.Context) {
	user := currentUser(c)
	id, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
		return
	}
	if err := s.manager.Cancel(c.Request.Context(), id, user.ID); err != nil {
		failManagerError(c, err)
		return
	}
	s.respondTask(c, id, user.ID)
}

type retryRequest struct {
	Files []int64 `json:"files"`
}

// handleRetry re-queues the failed files of a task.
func (s *Server) handleRetry(c *gin.Context) {
	user := currentUser(c)
	id, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
		return
	}
	var req retryRequest
	_ = c.ShouldBindJSON(&req)
	n, err := s.manager.Retry(c.Request.Context(), id, user.ID, req.Files)
	if err != nil {
		failManagerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "retried": n, "task": s.reloadTask(id, user.ID)})
}

// handleListFiles returns a page of a task's files.
func (s *Server) handleListFiles(c *gin.Context) {
	user := currentUser(c)
	id, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
		return
	}
	if _, err := s.store.TaskForUser(id, user.ID); err != nil {
		failStoreError(c, err)
		return
	}
	files, total, err := s.store.ListFiles(store.ListFilesParams{
		TaskID: id,
		Status: strings.TrimSpace(c.Query("status")),
		Query:  c.Query("q"),
		Limit:  queryInt(c, "limit", 50),
		Offset: queryInt(c, "offset", 0),
		Sort:   c.Query("sort"),
		Dir:    c.Query("dir"),
	})
	if err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"files": files, "total": total,
		"limit": queryInt(c, "limit", 50), "offset": queryInt(c, "offset", 0),
	})
}

// handleRetryFile re-queues a single file.
func (s *Server) handleRetryFile(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
		return
	}
	fileID, ok := pathInt64(c, "fileId")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "文件 ID 无效")
		return
	}
	f, err := s.store.GetFile(fileID)
	if err != nil || f.TaskID != taskID {
		fail(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}
	if _, err := s.store.TaskForUser(taskID, user.ID); err != nil {
		failStoreError(c, err)
		return
	}
	if _, err := s.manager.Retry(c.Request.Context(), taskID, user.ID, []int64{fileID}); err != nil {
		failManagerError(c, err)
		return
	}
	updated, _ := s.store.GetFile(fileID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "file": updated})
}

// handleTaskEvents returns the newest-first log of a task.
func (s *Server) handleTaskEvents(c *gin.Context) {
	user := currentUser(c)
	id, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "任务 ID 无效")
		return
	}
	if _, err := s.store.TaskForUser(id, user.ID); err != nil {
		failStoreError(c, err)
		return
	}
	var before int64
	if v := queryInt64Ptr(c, "before_id"); v != nil {
		before = *v
	}
	events, err := s.store.ListEvents(user.ID, id, before, queryInt(c, "limit", 200))
	if err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

// handleAllEvents returns the caller's global log.
func (s *Server) handleAllEvents(c *gin.Context) {
	user := currentUser(c)
	var before int64
	if v := queryInt64Ptr(c, "before_id"); v != nil {
		before = *v
	}
	events, err := s.store.ListEvents(user.ID, 0, before, queryInt(c, "limit", 200))
	if err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

// respondTask re-reads a task and returns it.
func (s *Server) respondTask(c *gin.Context, id, userID int64) {
	task := s.reloadTask(id, userID)
	if task == nil {
		fail(c, http.StatusNotFound, CodeNotFound, "任务不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "task": task})
}

func (s *Server) reloadTask(id, userID int64) *store.Task {
	t, err := s.store.TaskForUser(id, userID)
	if err != nil {
		return nil
	}
	return t
}

// failManagerError maps orchestrator errors onto HTTP responses.
func failManagerError(c *gin.Context, err error) {
	var ise *downloads.InvalidStateError
	if errors.As(err, &ise) {
		failDetails(c, http.StatusBadRequest, CodeInvalidState, ise.Error(),
			gin.H{"status": ise.Status, "action": ise.Action})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		fail(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	if strings.Contains(err.Error(), "aria2 unavailable") || strings.Contains(err.Error(), "aria2 未就绪") {
		fail(c, http.StatusServiceUnavailable, CodeAria2Unavailable, "aria2 下载引擎未就绪，请稍后重试")
		return
	}
	failStoreError(c, err)
}
