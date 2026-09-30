package store

import (
	"database/sql"
	"time"
)

// LogEvent appends a log line and returns its id.
// Pass nil for taskID/fileID to record a global (user-scoped) event.
func (s *Store) LogEvent(userID, taskID, fileID int64, level, event, details string) (int64, error) {
	now := nowUTC()
	var id int64
	err := s.tx(func(tx *sql.Tx) error {
		var taskPtr, filePtr, userPtr any
		if taskID > 0 {
			taskPtr = taskID
		}
		if fileID > 0 {
			filePtr = fileID
		}
		if userID > 0 {
			userPtr = userID
		}
		res, err := tx.Exec(`
            INSERT INTO events (user_id, task_id, file_id, level, event, details, created_at)
            VALUES (?,?,?,?,?,?,?)`, userPtr, taskPtr, filePtr, level, event, details, now)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// ListEvents returns newest-first events, optionally scoped to a task.
func (s *Store) ListEvents(userID, taskID int64, beforeID int64, limit int) ([]*Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	where := []string{}
	args := []any{}
	if taskID > 0 {
		where = append(where, "task_id = ?")
		args = append(args, taskID)
	} else if userID > 0 {
		where = append(where, "(user_id = ? OR user_id IS NULL)")
		args = append(args, userID)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + joinAnd(where)
	}
	if beforeID > 0 {
		if clause == "" {
			clause = " WHERE id < ?"
		} else {
			clause += " AND id < ?"
		}
		args = append(args, beforeID)
	}

	rows, err := s.db.Query(`
        SELECT id, task_id, file_id, level, event, COALESCE(details,''), created_at
        FROM events`+clause+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*Event, 0, limit)
	for rows.Next() {
		var e Event
		var taskID, fileID sql.NullInt64
		if err := rows.Scan(&e.ID, &taskID, &fileID, &e.Level, &e.Event, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		if taskID.Valid {
			v := taskID.Int64
			e.TaskID = &v
		}
		if fileID.Valid {
			v := fileID.Int64
			e.FileID = &v
		}
		e.CreatedAt = e.CreatedAt.UTC()
		out = append(out, &e)
	}
	return out, rows.Err()
}

// TrimEvents keeps only the newest keepLast events to bound table growth.
func (s *Store) TrimEvents(keepLast int) error {
	if keepLast <= 0 {
		keepLast = 5000
	}
	_, err := s.db.Exec(`
        DELETE FROM events WHERE id NOT IN (
            SELECT id FROM events ORDER BY id DESC LIMIT ?
        )`, keepLast)
	return err
}

// OldestEventID lets the hub resume from a known point after a reconnect.
func (s *Store) OldestEventID() (int64, error) {
	var id sql.NullInt64
	if err := s.db.QueryRow(`SELECT MIN(id) FROM events`).Scan(&id); err != nil {
		return 0, err
	}
	return id.Int64, nil
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

// TouchTaskEvent is a convenience wrapper for task-scoped logging.
func (s *Store) TouchTaskEvent(taskID int64, level, event, details string) (int64, error) {
	t, err := s.GetTask(taskID)
	userID := int64(0)
	if err == nil {
		userID = t.UserID
	}
	return s.LogEvent(userID, taskID, 0, level, event, details)
}

var _ = time.Time{}
