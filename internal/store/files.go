package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const fileSelect = `SELECT id, task_id, user_id, item_url, filename, download_link, gid,
       file_size, downloaded_bytes, speed, status, retry_count, error_message, item_date,
       created_at, started_at, finished_at, updated_at FROM files`

func scanFile(row rowScanner) (*File, error) {
	var f File
	var name, link, gid, errMsg sql.NullString
	var itemDate, started, finished sql.NullTime
	err := row.Scan(&f.ID, &f.TaskID, &f.UserID, &f.ItemURL, &name, &link, &gid,
		&f.FileSize, &f.DownloadedBytes, &f.Speed, &f.Status, &f.RetryCount, &errMsg,
		&itemDate, &f.CreatedAt, &started, &finished, &f.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	f.Filename = name.String
	f.DownloadLink = link.String
	f.GID = gid.String
	f.ErrorMessage = errMsg.String
	f.ItemDate = timePtr(itemDate)
	f.StartedAt = timePtr(started)
	f.FinishedAt = timePtr(finished)
	f.CreatedAt = f.CreatedAt.UTC()
	f.UpdatedAt = f.UpdatedAt.UTC()
	f.recomputeProgress()
	return &f, nil
}

func (f *File) recomputeProgress() {
	if f.FileSize > 0 {
		f.Progress = float64(f.DownloadedBytes) / float64(f.FileSize) * 100
		if f.Progress > 100 {
			f.Progress = 100
		}
	} else if f.Status == FileCompleted {
		f.Progress = 100
	} else {
		f.Progress = 0
	}
	f.Progress = float64(int(f.Progress*100+0.5)) / 100
}

// NewFile describes a file to register inside a task.
type NewFile struct {
	TaskID       int64
	UserID       int64
	ItemURL      string
	Filename     string
	DownloadLink string
	FileSize     int64
	ItemDate     *time.Time
}

// RegisterFiles inserts file rows, ignoring duplicates (same task + item URL).
// It returns the number of newly inserted rows.
func (s *Store) RegisterFiles(files []NewFile) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}
	now := nowUTC()
	inserted := 0
	err := s.tx(func(tx *sql.Tx) error {
		for _, f := range files {
			var id int64
			err := tx.QueryRow(`SELECT id FROM files WHERE task_id=? AND item_url=?`,
				f.TaskID, f.ItemURL).Scan(&id)
			switch {
			case err == nil:
				// Already known: refresh metadata without touching status.
				if _, err := tx.Exec(`
                    UPDATE files SET filename=COALESCE(NULLIF(?,''), filename),
                                     download_link=COALESCE(NULLIF(?,''), download_link),
                                     file_size=CASE WHEN ?>0 THEN ? ELSE file_size END,
                                     updated_at=?
                    WHERE id=?`,
					f.Filename, f.DownloadLink, f.FileSize, f.FileSize, now, id); err != nil {
					return err
				}
				continue
			case errors.Is(err, sql.ErrNoRows):
				// fallthrough to insert
			default:
				return err
			}

			_, err = tx.Exec(`
                INSERT INTO files (task_id, user_id, item_url, filename, download_link, file_size,
                                   item_date, status, created_at, updated_at)
                VALUES (?,?,?,?,?,?,?,?,?,?)`,
				f.TaskID, f.UserID, f.ItemURL, f.Filename, f.DownloadLink, f.FileSize,
				f.ItemDate, FilePending, now, now)
			if err != nil {
				if isUnique(err) {
					continue
				}
				return err
			}
			inserted++
		}
		return nil
	})
	return inserted, err
}

// GetFile loads a file by id.
func (s *Store) GetFile(id int64) (*File, error) {
	return scanFile(s.db.QueryRow(fileSelect+` WHERE id = ?`, id))
}

// GetFileByGID finds a file by its aria2 GID.
func (s *Store) GetFileByGID(gid string) (*File, error) {
	return scanFile(s.db.QueryRow(fileSelect+` WHERE gid = ? AND gid <> ''`, gid))
}

// ListFilesParams controls pagination of ListFiles.
type ListFilesParams struct {
	TaskID int64
	Status string
	Query  string
	Limit  int
	Offset int
	Sort   string // filename | size | status | created_at
	Dir    string
}

// ListFiles returns a page of files for a task plus the total count.
func (s *Store) ListFiles(p ListFilesParams) ([]*File, int64, error) {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	dir := "ASC"
	if strings.EqualFold(p.Dir, "desc") {
		dir = "DESC"
	}
	col := "filename"
	switch p.Sort {
	case "size":
		col = "file_size"
	case "status":
		col = "status"
	case "created_at":
		col = "id"
	}

	where := []string{"task_id = ?"}
	args := []any{p.TaskID}
	if p.Status != "" {
		where = append(where, "status = ?")
		args = append(args, p.Status)
	}
	if q := strings.TrimSpace(p.Query); q != "" {
		where = append(where, "lower(COALESCE(filename,'')) LIKE ?")
		args = append(args, likeArg(q))
	}
	clause := " WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM files`+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := fmt.Sprintf(`%s%s ORDER BY %s %s, id %s LIMIT ? OFFSET ?`,
		fileSelect, clause, col, dir, dir)
	rows, err := s.db.Query(q, append(append([]any{}, args...), p.Limit, p.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*File, 0, p.Limit)
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, f)
	}
	return out, total, rows.Err()
}

// AllFilesForTask returns every file of a task (used by the orchestrator).
func (s *Store) AllFilesForTask(taskID int64) ([]*File, error) {
	rows, err := s.db.Query(fileSelect+` WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FilesByStatus filters a task's files by status.
func (s *Store) FilesByStatus(taskID int64, status string) ([]*File, error) {
	rows, err := s.db.Query(fileSelect+` WHERE task_id = ? AND status = ? ORDER BY id`, taskID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FileUpdate carries the mutable file fields. Nil means "unchanged".
type FileUpdate struct {
	Status          *string
	DownloadLink    *string
	Filename        *string
	GID             *string
	FileSize        *int64
	DownloadedBytes *int64
	Speed           *int64
	RetryCount      *int
	ErrorMessage    *string
	StartedAt       *time.Time
	FinishedAt      *time.Time
	StartedNull     bool
	FinishedNull    bool
	ErrorNull       bool
}

var fileUpdatable = map[string]bool{
	"status": true, "download_link": true, "filename": true, "gid": true,
	"file_size": true, "downloaded_bytes": true, "speed": true, "retry_count": true,
	"error_message": true, "started_at": true, "finished_at": true,
}

// UpdateFile applies a partial update to one file row.
func (s *Store) UpdateFile(id int64, up FileUpdate) error {
	sets := make([]string, 0, 8)
	args := make([]any, 0, 8)
	add := func(col string, v any) {
		if !fileUpdatable[col] {
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
	str("download_link", up.DownloadLink)
	str("filename", up.Filename)
	str("gid", up.GID)
	str("error_message", up.ErrorMessage)
	if up.FileSize != nil {
		add("file_size", *up.FileSize)
	}
	if up.DownloadedBytes != nil {
		add("downloaded_bytes", *up.DownloadedBytes)
	}
	if up.Speed != nil {
		add("speed", *up.Speed)
	}
	if up.RetryCount != nil {
		add("retry_count", *up.RetryCount)
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
		_, err := tx.Exec(`UPDATE files SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
		return err
	})
}

// UpdateFilesByGID applies the same update to every file with the given GIDs.
// This is the hot path: aria2 progress polling touches many rows at once.
func (s *Store) UpdateFilesByGID(gids map[string]FileUpdate) error {
	if len(gids) == 0 {
		return nil
	}
	now := nowUTC()
	return s.tx(func(tx *sql.Tx) error {
		for gid, up := range gids {
			sets := make([]string, 0, 8)
			args := make([]any, 0, 8)
			add := func(col string, v any) {
				if !fileUpdatable[col] {
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
			str("error_message", up.ErrorMessage)
			str("filename", up.Filename)
			if up.FileSize != nil {
				add("file_size", *up.FileSize)
			}
			if up.DownloadedBytes != nil {
				add("downloaded_bytes", *up.DownloadedBytes)
			}
			if up.Speed != nil {
				add("speed", *up.Speed)
			}
			if up.Status != nil && (*up.Status == FileCompleted || *up.Status == FileFailed) {
				add("finished_at", now)
				add("downloaded_bytes", func() int64 {
					if up.DownloadedBytes != nil {
						return *up.DownloadedBytes
					}
					return 0
				}())
			}
			if len(sets) == 0 {
				continue
			}
			sets = append(sets, "updated_at = ?")
			args = append(args, now, gid)
			if _, err := tx.Exec(`UPDATE files SET `+strings.Join(sets, ", ")+` WHERE gid = ?`, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// ResetFileForRetry returns a file to the pending state.
func (s *Store) ResetFileForRetry(id int64) error {
	status := FilePending
	gid := ""
	now := nowUTC()
	return s.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
            UPDATE files SET status=?, gid=?, speed=0, error_message=NULL, started_at=NULL,
                             finished_at=NULL, retry_count=retry_count+1, updated_at=?
            WHERE id=?`, status, gid, now, id)
		return err
	})
}

// ResetTaskFailures moves every failed file of a task back to pending.
func (s *Store) ResetTaskFailures(taskID int64) (int64, error) {
	var n int64
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`
            UPDATE files SET status=?, gid='', speed=0, error_message=NULL, started_at=NULL,
                             finished_at=NULL, retry_count=retry_count+1, updated_at=?
            WHERE task_id=? AND status=?`, FilePending, nowUTC(), taskID, FileFailed)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

// MarkTaskFilesPending returns downloading files of a task to pending state.
func (s *Store) MarkTaskFilesPending(taskID int64) error {
	return s.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
            UPDATE files SET status=?, gid='', speed=0, updated_at=?
            WHERE task_id=? AND status=?`, FilePending, nowUTC(), taskID, FileDownloading)
		return err
	})
}

// SkipRemaining marks every non-terminal file of a task as skipped.
func (s *Store) SkipRemaining(taskID int64, reason string) (int64, error) {
	var n int64
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`
            UPDATE files SET status=?, error_message=?, finished_at=?, updated_at=?
            WHERE task_id=? AND status IN (?, ?)`,
			FileSkipped, reason, nowUTC(), nowUTC(), taskID, FilePending, FileDownloading)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

// PendingFilesNeedingLink returns files that still need a download link.
func (s *Store) PendingFilesNeedingLink(taskID int64) ([]*File, error) {
	rows, err := s.db.Query(fileSelect+`
        WHERE task_id=? AND status=? AND (download_link IS NULL OR download_link='')
        ORDER BY id`, taskID, FilePending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DownloadableFiles returns files eligible for hand-off to aria2.
func (s *Store) DownloadableFiles(taskID int64) ([]*File, error) {
	rows, err := s.db.Query(fileSelect+`
        WHERE task_id=? AND status=? AND download_link IS NOT NULL AND download_link<>''
        ORDER BY id`, taskID, FilePending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetFileDownloading marks a file as handed to aria2 and records its GID.
func (s *Store) SetFileDownloading(id int64, gid string) error {
	now := nowUTC()
	return s.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
            UPDATE files SET status=?, gid=?, started_at=COALESCE(started_at, ?), updated_at=?
            WHERE id=?`, FileDownloading, gid, now, now, id)
		return err
	})
}

// ClearGID detaches a file from an aria2 download (e.g. after force-remove).
func (s *Store) ClearGID(gid string) error {
	return s.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
            UPDATE files SET gid='', speed=0,
                   status=CASE WHEN status=? THEN ? ELSE status END,
                   updated_at=?
            WHERE gid=?`, FileDownloading, FilePending, nowUTC(), gid)
		return err
	})
}

// CountFiles counts a user's registered files (used for quota bookkeeping).
func (s *Store) CountFiles(userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM files WHERE user_id=? AND status<>?`,
		userID, FileSkipped).Scan(&n)
	return n, err
}

// BoundFiles returns every file currently attached to an aria2 download.
//
// It is deliberately a single query: iterating one cursor and issuing another
// query per row would exhaust a small connection pool.
func (s *Store) BoundFiles(limit int) ([]*File, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	rows, err := s.db.Query(fileSelect+`
        WHERE gid <> '' AND status IN ('downloading','paused')
        ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*File, 0, 64)
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DetachAllGIDs clears every aria2 handle, e.g. after the daemon restarted and
// the GIDs no longer exist. The files become pending again and are resubmitted.
func (s *Store) DetachAllGIDs() error {
	return s.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
            UPDATE files SET gid='', speed=0,
                   status=CASE WHEN status='downloading' THEN 'pending' ELSE status END,
                   updated_at=?
            WHERE gid <> ''`, nowUTC())
		return err
	})
}
