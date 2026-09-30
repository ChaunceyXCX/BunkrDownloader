package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ListTasksParams controls filtering, sorting and pagination of ListTasks.
type ListTasksParams struct {
	UserID int64
	Status string
	Query  string
	Limit  int
	Offset int
	Sort   string // created_at | updated_at
	Dir    string // asc | desc
}

const taskSelect = `SELECT id, user_id, url, kind, album_id, album_name, status, download_path,
       options_json, error_message, total_files, completed_files, failed_files, skipped_files,
       pending_files, downloading_files,
       total_bytes, downloaded_bytes, speed, created_at, started_at, finished_at, updated_at
FROM tasks`

func scanTask(row rowScanner) (*Task, error) {
	var t Task
	var albumID, albumName, dlPath, errMsg, optsJSON sql.NullString
	var started, finished sql.NullTime
	err := row.Scan(&t.ID, &t.UserID, &t.URL, &t.Kind, &albumID, &albumName, &t.Status,
		&dlPath, &optsJSON, &errMsg, &t.TotalFiles, &t.CompletedFiles, &t.FailedFiles,
		&t.SkippedFiles, &t.PendingFiles, &t.DownloadingFiles,
		&t.TotalBytes, &t.DownloadedBytes, &t.Speed,
		&t.CreatedAt, &started, &finished, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.AlbumID = albumID.String
	t.AlbumName = albumName.String
	t.DownloadPath = dlPath.String
	t.ErrorMessage = errMsg.String
	t.StartedAt = timePtr(started)
	t.FinishedAt = timePtr(finished)
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	t.Options = decodeOptions(optsJSON.String)
	t.recomputeProgress()
	return &t, nil
}

func decodeOptions(raw string) TaskOptions {
	opts := DefaultTaskOptions()
	if raw == "" {
		return opts
	}
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		return DefaultTaskOptions()
	}
	if opts.Ignore == nil {
		opts.Ignore = []string{}
	}
	if opts.Include == nil {
		opts.Include = []string{}
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 5
	}
	if opts.Connections <= 0 {
		opts.Connections = 4
	}
	return opts
}

func (t *Task) recomputeProgress() {
	if t.TotalBytes > 0 {
		t.Progress = float64(t.DownloadedBytes) / float64(t.TotalBytes) * 100
		if t.Progress > 100 {
			t.Progress = 100
		}
	} else if t.TotalFiles > 0 {
		done := t.CompletedFiles + t.SkippedFiles
		t.Progress = float64(done) / float64(t.TotalFiles) * 100
	} else {
		t.Progress = 0
	}
	t.Progress = float64(int(t.Progress*100+0.5)) / 100
}

// CreateTaskOptions carries everything needed to insert a task row.
type CreateTaskOptions struct {
	UserID int64
	URL    string
	Kind   string
	Opts   TaskOptions
}

// CreateTask inserts a new task in the pending state.
func (s *Store) CreateTask(in CreateTaskOptions) (*Task, error) {
	now := nowUTC()
	raw, _ := json.Marshal(in.Opts)
	if in.Kind == "" {
		in.Kind = "media"
	}
	var id int64
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`
            INSERT INTO tasks (user_id, url, kind, status, options_json, download_path, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			in.UserID, in.URL, in.Kind, TaskPending, string(raw), in.Opts.CustomPath, now, now)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(id)
}

// GetTask loads a task by id.
func (s *Store) GetTask(id int64) (*Task, error) {
	return scanTask(s.db.QueryRow(taskSelect+` WHERE id = ?`, id))
}

// TaskForUser loads a task and verifies ownership.
func (s *Store) TaskForUser(id, userID int64) (*Task, error) {
	t, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	if t.UserID != userID {
		return nil, ErrNotFound
	}
	return t, nil
}

// ListTasks returns a page of tasks plus the total number of matches.
func (s *Store) ListTasks(p ListTasksParams) ([]*Task, int64, error) {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	dir := "DESC"
	if strings.EqualFold(p.Dir, "asc") {
		dir = "ASC"
	}
	col := "created_at"
	if p.Sort == "updated_at" {
		col = "updated_at"
	}

	where := []string{"user_id = ?"}
	args := []any{p.UserID}
	if p.Status != "" {
		where = append(where, "status = ?")
		args = append(args, p.Status)
	}
	if q := strings.TrimSpace(p.Query); q != "" {
		where = append(where, "(lower(url) LIKE ? OR lower(COALESCE(album_name,'')) LIKE ?)")
		args = append(args, likeArg(q), likeArg(q))
	}
	clause := " WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks`+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := fmt.Sprintf(`%s%s ORDER BY %s %s, id %s LIMIT ? OFFSET ?`,
		taskSelect, clause, col, dir, dir)
	rows, err := s.db.Query(q, append(append([]any{}, args...), p.Limit, p.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*Task, 0, p.Limit)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// TaskUpdate carries the mutable fields of a task. Nil means "unchanged".
type TaskUpdate struct {
	Status          *string
	Kind            *string
	AlbumID         *string
	AlbumName       *string
	DownloadPath    *string
	ErrorMessage    *string
	Speed           *int64
	TotalBytes      *int64
	DownloadedBytes *int64
	Options         *TaskOptions
	StartedAt       *time.Time
	FinishedAt      *time.Time
	StartedNull     bool // clear started_at
	FinishedNull    bool // clear finished_at
}

var taskUpdatable = map[string]bool{
	"status": true, "kind": true, "album_id": true, "album_name": true,
	"download_path": true, "error_message": true, "speed": true,
	"total_bytes": true, "downloaded_bytes": true,
	"options_json": true, "started_at": true, "finished_at": true,
}

// UpdateTask applies a partial update and recomputes the cached counters.
func (s *Store) UpdateTask(id int64, up TaskUpdate) error {
	sets := make([]string, 0, 8)
	args := make([]any, 0, 8)

	add := func(col string, v any) {
		if !taskUpdatable[col] {
			return
		}
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	str := func(col string, p *string) {
		if p != nil {
			add(col, *p)
		}
	}
	str("status", up.Status)
	str("kind", up.Kind)
	str("album_id", up.AlbumID)
	str("album_name", up.AlbumName)
	str("download_path", up.DownloadPath)
	str("error_message", up.ErrorMessage)
	if up.Speed != nil {
		add("speed", *up.Speed)
	}
	if up.TotalBytes != nil {
		add("total_bytes", *up.TotalBytes)
	}
	if up.DownloadedBytes != nil {
		add("downloaded_bytes", *up.DownloadedBytes)
	}
	if up.Options != nil {
		raw, _ := json.Marshal(*up.Options)
		add("options_json", string(raw))
	}
	if up.StartedAt != nil {
		add("started_at", *up.StartedAt)
	} else if up.StartedNull {
		sets = append(sets, "started_at = NULL")
	}
	if up.FinishedAt != nil {
		add("finished_at", *up.FinishedAt)
	} else if up.FinishedNull {
		sets = append(sets, "finished_at = NULL")
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, nowUTC(), id)

	return s.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE tasks SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
		return err
	})
}

// RecomputeTaskStats refreshes the denormalised counters from the files table.
func (s *Store) RecomputeTaskStats(taskID int64) error {
	return s.tx(func(tx *sql.Tx) error {
		var total, completed, failed, skipped, pending, downloading, bytes, done int64
		err := tx.QueryRow(`
            SELECT COUNT(*),
                   COALESCE(SUM(status='completed'),0),
                   COALESCE(SUM(status='failed'),0),
                   COALESCE(SUM(status='skipped'),0),
                   COALESCE(SUM(status='pending'),0),
                   COALESCE(SUM(status='downloading'),0),
                   COALESCE(SUM(file_size),0),
                   COALESCE(SUM(downloaded_bytes),0)
            FROM files WHERE task_id=?`, taskID).
			Scan(&total, &completed, &failed, &skipped, &pending, &downloading, &bytes, &done)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`
            UPDATE tasks SET total_files=?, completed_files=?, failed_files=?, skipped_files=?,
                            pending_files=?, downloading_files=?,
                            total_bytes=?, downloaded_bytes=?, updated_at=?
            WHERE id=?`,
			total, completed, failed, skipped, pending, downloading,
			bytes, done, nowUTC(), taskID)
		return err
	})
}

// DeleteTask removes a task and its dependent rows.
func (s *Store) DeleteTask(id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM events WHERE task_id=?`, id); err != nil {
			return err
		}
		// Clear the GIDs first: rows may be referenced by an aria2 session.
		if _, err := tx.Exec(`DELETE FROM files WHERE task_id=?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM tasks WHERE id=?`, id)
		return err
	})
}

// CountActiveTasks counts tasks occupying a concurrency slot.
func (s *Store) CountActiveTasks(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id=? AND status IN ('running','crawling')`,
		userID).Scan(&n)
	return n, err
}

// AllActiveTasks lists every task currently running or crawling, across users.
func (s *Store) AllActiveTasks() ([]*Task, error) {
	rows, err := s.db.Query(taskSelect + ` WHERE status IN ('running','crawling') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RequeueInterrupted moves tasks that were mid-flight when the process died
// back to a resumable state.
func (s *Store) RequeueInterrupted() (int64, error) {
	res, err := s.db.Exec(`
        UPDATE tasks SET status=?, updated_at=?
        WHERE status IN ('running','crawling')`,
		TaskPaused, nowUTC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		// Files left in "downloading" are unknown after a crash.
		if _, err := s.db.Exec(`
            UPDATE files SET status=?, speed=0, updated_at=?
            WHERE status=?`, FilePending, nowUTC(), FileDownloading); err != nil {
			return n, err
		}
	}
	return n, nil
}

// GlobalStats aggregates the dashboard counters for a user.
type GlobalStats struct {
	TotalTasks      int64 `json:"total_tasks"`
	Running         int64 `json:"running"`
	Pending         int64 `json:"pending"`
	Paused          int64 `json:"paused"`
	Completed       int64 `json:"completed"`
	Failed          int64 `json:"failed"`
	Canceled        int64 `json:"canceled"`
	TotalFiles      int64 `json:"total_files"`
	CompletedFiles  int64 `json:"completed_files"`
	DownloadedBytes int64 `json:"downloaded_bytes"`
	Speed           int64 `json:"speed"`
	ActiveFiles     int64 `json:"active_files"`
}

// Stats computes the global counters scoped to one user.
func (s *Store) Stats(userID int64) (GlobalStats, error) {
	var st GlobalStats
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM tasks WHERE user_id=? GROUP BY status`, userID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	st.TotalTasks = 0
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return st, err
		}
		st.TotalTasks += n
		switch status {
		case TaskRunning, TaskCrawling:
			st.Running += n
		case TaskPending:
			st.Pending += n
		case TaskPaused:
			st.Paused += n
		case TaskCompleted:
			st.Completed += n
		case TaskFailed:
			st.Failed += n
		case TaskCanceled:
			st.Canceled += n
		}
	}
	if err := rows.Err(); err != nil {
		return st, err
	}

	err = s.db.QueryRow(`
        SELECT COUNT(*),
               COALESCE(SUM(status='completed'),0),
               COALESCE(SUM(downloaded_bytes),0)
        FROM files WHERE user_id=?`, userID).
		Scan(&st.TotalFiles, &st.CompletedFiles, &st.DownloadedBytes)
	if err != nil {
		return st, err
	}
	err = s.db.QueryRow(`
        SELECT COALESCE(SUM(speed),0), COUNT(*) FROM files WHERE user_id=? AND status=?`,
		userID, FileDownloading).Scan(&st.Speed, &st.ActiveFiles)
	return st, err
}
