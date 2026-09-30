package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no cgo required
)

// Sentinel errors returned by the repositories.
var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("already exists")
)

// Store owns the SQLite connection and all repositories.
type Store struct {
	db *sql.DB

	// writeMu serialises write transactions. SQLite in WAL mode allows one
	// writer at a time; this also makes multi-statement updates atomic.
	writeMu sync.Mutex

	// Quota limits per plan, configured at startup by Configure.
	freeLimits   QuotaLimits
	memberLimits QuotaLimits

	plans []Plan
}

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 10000;

CREATE TABLE IF NOT EXISTS users (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    username        TEXT    NOT NULL,
    email           TEXT    NOT NULL,
    password_hash   TEXT    NOT NULL,
    role            TEXT    NOT NULL DEFAULT 'user',
    plan            TEXT    NOT NULL DEFAULT 'free',
    plan_expires_at TIMESTAMP,
    created_at      TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email    ON users(email);

CREATE TABLE IF NOT EXISTS tasks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id          INTEGER NOT NULL,
    url              TEXT    NOT NULL,
    kind             TEXT    NOT NULL DEFAULT 'media',
    album_id         TEXT,
    album_name       TEXT,
    status           TEXT    NOT NULL DEFAULT 'pending',
    download_path    TEXT,
    options_json     TEXT    NOT NULL DEFAULT '{}',
    error_message    TEXT,
    total_files      INTEGER NOT NULL DEFAULT 0,
    completed_files  INTEGER NOT NULL DEFAULT 0,
    failed_files     INTEGER NOT NULL DEFAULT 0,
    skipped_files    INTEGER NOT NULL DEFAULT 0,
    total_bytes      INTEGER NOT NULL DEFAULT 0,
    downloaded_bytes INTEGER NOT NULL DEFAULT 0,
    speed            INTEGER NOT NULL DEFAULT 0,
    created_at       TIMESTAMP NOT NULL,
    started_at       TIMESTAMP,
    finished_at      TIMESTAMP,
    updated_at       TIMESTAMP NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_tasks_user   ON tasks(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);

CREATE TABLE IF NOT EXISTS files (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id          INTEGER NOT NULL,
    user_id          INTEGER NOT NULL,
    item_url         TEXT    NOT NULL,
    filename         TEXT,
    download_link    TEXT,
    gid              TEXT,
    file_size        INTEGER NOT NULL DEFAULT 0,
    downloaded_bytes INTEGER NOT NULL DEFAULT 0,
    speed            INTEGER NOT NULL DEFAULT 0,
    status           TEXT    NOT NULL DEFAULT 'pending',
    retry_count      INTEGER NOT NULL DEFAULT 0,
    error_message    TEXT,
    item_date        TIMESTAMP,
    created_at       TIMESTAMP NOT NULL,
    started_at       TIMESTAMP,
    finished_at      TIMESTAMP,
    updated_at       TIMESTAMP NOT NULL,
    UNIQUE (task_id, item_url),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_files_task   ON files(task_id, id);
CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);
CREATE INDEX IF NOT EXISTS idx_files_gid    ON files(gid);
CREATE INDEX IF NOT EXISTS idx_files_user   ON files(user_id, status);

CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER,
    task_id    INTEGER,
    file_id    INTEGER,
    level      TEXT    NOT NULL DEFAULT 'info',
    event      TEXT    NOT NULL,
    details    TEXT,
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_task    ON events(task_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(id DESC);

CREATE TABLE IF NOT EXISTS orders (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL,
    plan         TEXT    NOT NULL,
    amount_cents INTEGER NOT NULL DEFAULT 0,
    currency     TEXT    NOT NULL DEFAULT 'CNY',
    status       TEXT    NOT NULL DEFAULT 'pending',
    provider     TEXT    NOT NULL DEFAULT 'mock',
    trade_no     TEXT,
    created_at   TIMESTAMP NOT NULL,
    paid_at      TIMESTAMP,
    expires_at   TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_orders_user ON orders(user_id, id DESC);

CREATE TABLE IF NOT EXISTS redeem_codes (
    code        TEXT PRIMARY KEY,
    plan        TEXT NOT NULL,
    period_days INTEGER NOT NULL DEFAULT 30,
    used_by     INTEGER,
    used_at     TIMESTAMP,
    created_at  TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// Open initialises the database, applies the schema and returns a Store.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	// _txlock=immediate avoids SQLITE_BUSY upgrade deadlocks under concurrency.
	dsn := path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// WAL mode allows many concurrent readers alongside a single writer, and
	// writeMu serialises our write transactions. A pool of 1 would deadlock
	// whenever a query is issued while another row cursor is still open.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(0)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the raw handle (used by maintenance code and tests).
func (s *Store) DB() *sql.DB { return s.db }

// migrate performs small idempotent upgrades for schema drift.
func (s *Store) migrate() error {
	// Older builds may lack columns added later; add them defensively.
	additions := []struct{ table, column, ddl string }{
		{"tasks", "speed", "ALTER TABLE tasks ADD COLUMN speed INTEGER NOT NULL DEFAULT 0"},
		{"files", "gid", "ALTER TABLE files ADD COLUMN gid TEXT"},
		{"files", "speed", "ALTER TABLE files ADD COLUMN speed INTEGER NOT NULL DEFAULT 0"},
		{"files", "user_id", "ALTER TABLE files ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0"},
		{"tasks", "pending_files", "ALTER TABLE tasks ADD COLUMN pending_files INTEGER NOT NULL DEFAULT 0"},
		{"tasks", "downloading_files", "ALTER TABLE tasks ADD COLUMN downloading_files INTEGER NOT NULL DEFAULT 0"},
		{"events", "user_id", "ALTER TABLE events ADD COLUMN user_id INTEGER"},
		{"orders", "trade_no", "ALTER TABLE orders ADD COLUMN trade_no TEXT"},
	}
	for _, a := range additions {
		has, err := s.columnExists(a.table, a.column)
		if err != nil {
			return err
		}
		if !has {
			if _, err := s.db.Exec(a.ddl); err != nil &&
				!strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return fmt.Errorf("migrate %s.%s: %w", a.table, a.column, err)
			}
		}
	}
	return nil
}

func (s *Store) columnExists(table, column string) (bool, error) {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ------------------------------------------------------------------ helpers

// tx runs fn inside a write transaction.
func (s *Store) tx(fn func(*sql.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(t); err != nil {
		_ = t.Rollback()
		return err
	}
	return t.Commit()
}

// timePtr converts a nullable timestamp column into *time.Time.
func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed: unique")
}

func likeArg(q string) string { return "%" + strings.ToLower(strings.TrimSpace(q)) + "%" }

func nowUTC() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }
