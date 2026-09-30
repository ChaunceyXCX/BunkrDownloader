package services

import (
	"context"

	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkr"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// verifyPassword re-exports the auth verifier so the services do not each
// import the auth package.
func verifyPassword(password, hash string) error { return auth.VerifyPassword(password, hash) }

// TaskService exposes the download manager to the desktop UI.
type TaskService struct{ app *App }

// NewTaskService builds the binding for TaskService.
func NewTaskService(a *App) *TaskService { return &TaskService{app: a} }

// TaskList is the paged result of ListTasks.
type TaskList struct {
	Tasks []*store.Task `json:"tasks"`
	Total int64         `json:"total"`
}

// FileList is the paged result of ListFiles.
type FileList struct {
	Files  []*store.File `json:"files"`
	Total  int64         `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

// EventList wraps task events.
type EventList struct {
	Events []*store.Event `json:"events"`
}

// TaskOptionsInput mirrors the web API's per-task options.
type TaskOptionsInput struct {
	MaxRetries    int      `json:"max_retries"`
	Connections   int      `json:"connections"`
	RateLimitKbps int      `json:"rate_limit_kbps"`
	Ignore        []string `json:"ignore"`
	Include       []string `json:"include"`
	NoAlbumFolder bool     `json:"no_album_folder"`
	CleanName     bool     `json:"clean_name"`
	CustomPath    string   `json:"custom_path"`
}

func (in *TaskOptionsInput) toStore() store.TaskOptions {
	if in == nil {
		return store.DefaultTaskOptions()
	}
	out := store.TaskOptions{
		MaxRetries:    in.MaxRetries,
		Connections:   in.Connections,
		RateLimitKbps: in.RateLimitKbps,
		Ignore:        in.Ignore,
		Include:       in.Include,
		NoAlbumFolder: in.NoAlbumFolder,
		CleanName:     in.CleanName,
		CustomPath:    in.CustomPath,
	}
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
	if out.Ignore == nil {
		out.Ignore = []string{}
	}
	if out.Include == nil {
		out.Include = []string{}
	}
	return out
}

// CreateTaskRequest is the payload for CreateTask.
type CreateTaskRequest struct {
	// URL accepts a newline-separated list, mirroring the web UI.
	URL       string            `json:"url"`
	URLs      []string          `json:"urls"`
	Options   *TaskOptionsInput `json:"options"`
	AutoStart bool              `json:"auto_start"`
}

// CreateResult reports the created task ids and the refreshed quota.
type CreateResult struct {
	TaskIDs []int64      `json:"task_ids"`
	Count   int          `json:"count"`
	Quota   *store.Quota `json:"quota"`
}

// ListTasks returns a filtered page of the caller's tasks.
func (s *TaskService) ListTasks(token, status, query, sort, dir string, limit, offset int) (*TaskList, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	tasks, total, err := s.app.Store.ListTasks(store.ListTasksParams{
		UserID: userID, Status: status, Query: query,
		Limit: limit, Offset: offset, Sort: sort, Dir: dir,
	})
	if err != nil {
		return nil, s.app.translate(err)
	}
	return &TaskList{Tasks: tasks, Total: total}, nil
}

// CreateTask validates, quota-checks and creates one or more tasks.
func (s *TaskService) CreateTask(token string, req CreateTaskRequest) (*CreateResult, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	urls := dedupe(collectURLs(req.URL, req.URLs))
	if len(urls) == 0 {
		return nil, &APIError{Code: CodeBadRequest, Message: "请至少提供一个链接"}
	}
	if len(urls) > 100 {
		return nil, &APIError{Code: CodeBadRequest, Message: "单次最多提交 100 个链接"}
	}

	normalized := make([]string, 0, len(urls))
	for _, raw := range urls {
		u := bunkr.NormalizeURL(raw)
		if !bunkr.IsHTTPScheme(u) {
			return nil, &APIError{
				Code:    CodeInvalidURL,
				Message: "无法识别的链接：" + raw + "（仅支持 http/https 地址）",
			}
		}
		if _, err := bunkr.ResolveURLType(u); err != nil {
			return nil, &APIError{
				Code:    CodeInvalidURL,
				Message: "无法识别的链接：" + raw + "（请确认是 Bunkr 相册 /a/ 或文件 /v/ 地址）",
			}
		}
		normalized = append(normalized, u)
	}

	// The whole batch is checked up front so a partial submission is impossible.
	if err := s.app.Store.CanAddLinks(userID, len(normalized)); err != nil {
		return nil, s.app.translate(err)
	}

	opts := req.Options.toStore()
	ctx := context.Background()
	ids := make([]int64, 0, len(normalized))
	for _, u := range normalized {
		task, err := s.app.Manager.CreateTask(ctx, userID, u, opts)
		if err != nil {
			return nil, s.app.translate(err)
		}
		ids = append(ids, task.ID)
	}
	if req.AutoStart {
		for _, id := range ids {
			if err := s.app.Manager.StartTask(ctx, id, userID); err != nil {
				s.app.Log.Warn("auto start task", "task", id, "error", err)
			}
		}
	}
	quota, err := s.app.Store.Quota(userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	return &CreateResult{TaskIDs: ids, Count: len(ids), Quota: &quota}, nil
}

// TaskDetail is a task plus its per-status file summary.
type TaskDetail struct {
	Task         *store.Task    `json:"task"`
	FilesSummary map[string]int `json:"files_summary"`
}

// GetTask returns one task owned by the caller.
func (s *TaskService) GetTask(token string, taskID int64) (*TaskDetail, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	task, err := s.app.Store.TaskForUser(taskID, userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	files, err := s.app.Store.AllFilesForTask(taskID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	summary := map[string]int{}
	for _, f := range files {
		summary[f.Status]++
	}
	return &TaskDetail{Task: task, FilesSummary: summary}, nil
}

// ListFiles returns a page of a task's files.
func (s *TaskService) ListFiles(token string, taskID int64, status, query, sort, dir string, limit, offset int) (*FileList, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	if _, err := s.app.Store.TaskForUser(taskID, userID); err != nil {
		return nil, s.app.translate(err)
	}
	files, total, err := s.app.Store.ListFiles(store.ListFilesParams{
		TaskID: taskID, Status: status, Query: query,
		Limit: limit, Offset: offset, Sort: sort, Dir: dir,
	})
	if err != nil {
		return nil, s.app.translate(err)
	}
	if limit <= 0 {
		limit = 50
	}
	return &FileList{Files: files, Total: total, Limit: limit, Offset: offset}, nil
}

// ListEvents returns a task's log, newest first.
func (s *TaskService) ListEvents(token string, taskID, beforeID int64, limit int) (*EventList, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	if taskID > 0 {
		if _, err := s.app.Store.TaskForUser(taskID, userID); err != nil {
			return nil, s.app.translate(err)
		}
	}
	events, err := s.app.Store.ListEvents(userID, taskID, beforeID, limit)
	if err != nil {
		return nil, s.app.translate(err)
	}
	return &EventList{Events: events}, nil
}

// Start begins (or queues) a task.
func (s *TaskService) Start(token string, taskID int64) (*store.Task, *APIError) {
	return s.action(token, taskID, func(ctx context.Context, userID int64) error {
		return s.app.Manager.StartTask(ctx, taskID, userID)
	})
}

// Pause suspends a task's transfers.
func (s *TaskService) Pause(token string, taskID int64) (*store.Task, *APIError) {
	return s.action(token, taskID, func(ctx context.Context, userID int64) error {
		return s.app.Manager.Pause(ctx, taskID, userID)
	})
}

// Resume continues a paused task from its saved position.
func (s *TaskService) Resume(token string, taskID int64) (*store.Task, *APIError) {
	return s.action(token, taskID, func(ctx context.Context, userID int64) error {
		return s.app.Manager.Resume(ctx, taskID, userID)
	})
}

// Cancel aborts a task and skips the remaining files.
func (s *TaskService) Cancel(token string, taskID int64) (*store.Task, *APIError) {
	return s.action(token, taskID, func(ctx context.Context, userID int64) error {
		return s.app.Manager.Cancel(ctx, taskID, userID)
	})
}

// Retry re-queues a task's failed files (or a specific subset).
func (s *TaskService) Retry(token string, taskID int64, fileIDs []int64) (int64, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return 0, apiErr
	}
	n, err := s.app.Manager.Retry(context.Background(), taskID, userID, fileIDs)
	if err != nil {
		return 0, s.app.translate(err)
	}
	return n, nil
}

// RetryFile re-queues a single file.
func (s *TaskService) RetryFile(token string, taskID, fileID int64) (*store.File, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	f, err := s.app.Store.GetFile(fileID)
	if err != nil || f.TaskID != taskID {
		return nil, &APIError{Code: CodeNotFound, Message: "文件不存在"}
	}
	if _, err := s.app.Store.TaskForUser(taskID, userID); err != nil {
		return nil, s.app.translate(err)
	}
	if _, err := s.app.Manager.Retry(context.Background(), taskID, userID, []int64{fileID}); err != nil {
		return nil, s.app.translate(err)
	}
	updated, err := s.app.Store.GetFile(fileID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	return updated, nil
}

// DeleteTask cancels and removes a task.
func (s *TaskService) DeleteTask(token string, taskID int64) (bool, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return false, apiErr
	}
	if err := s.app.Manager.DeleteTask(context.Background(), taskID, userID); err != nil {
		return false, s.app.translate(err)
	}
	return true, nil
}

// action runs a manager operation and returns the refreshed task.
func (s *TaskService) action(token string, taskID int64, fn func(context.Context, int64) error) (*store.Task, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	if err := fn(context.Background(), userID); err != nil {
		return nil, s.app.translate(err)
	}
	task, err := s.app.Store.TaskForUser(taskID, userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	return task, nil
}

// --------------------------------------------------------------- URL parsing

func collectURLs(raw string, list []string) []string {
	var out []string
	add := func(s string) {
		for _, part := range splitFields(s) {
			if part != "" {
				out = append(out, part)
			}
		}
	}
	add(raw)
	for _, item := range list {
		add(item)
	}
	return out
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		key := lower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}
