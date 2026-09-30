// Package store is the persistence layer backed by SQLite (pure-Go driver).
//
// It owns the schema, migrations and every query used by the API and the
// download orchestrator. All methods are safe for concurrent use.
package store

import "time"

// Plan identifiers.
const (
	PlanFree   = "free"
	PlanMember = "member"
)

// User roles.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// Task lifecycle states.
const (
	TaskPending   = "pending"
	TaskCrawling  = "crawling"
	TaskRunning   = "running"
	TaskPaused    = "paused"
	TaskCompleted = "completed"
	TaskFailed    = "failed"
	TaskCanceled  = "canceled"
)

// File lifecycle states.
const (
	FilePending     = "pending"
	FileDownloading = "downloading"
	FileCompleted   = "completed"
	FileFailed      = "failed"
	FileSkipped     = "skipped"
)

// Event severity levels.
const (
	LevelInfo    = "info"
	LevelSuccess = "success"
	LevelWarn    = "warn"
	LevelError   = "error"
)

// Order states.
const (
	OrderPending  = "pending"
	OrderPaid     = "paid"
	OrderCanceled = "canceled"
	OrderRefunded = "refunded"
)

// User is an account row.
type User struct {
	ID            int64      `json:"id"`
	Username      string     `json:"username"`
	Email         string     `json:"email"`
	PasswordHash  string     `json:"-"`
	Role          string     `json:"role"`
	Plan          string     `json:"plan"`
	PlanExpiresAt *time.Time `json:"plan_expires_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// IsMember reports whether the user currently holds an active membership.
func (u *User) IsMember(now time.Time) bool {
	if u.Plan != PlanMember {
		return false
	}
	return u.PlanExpiresAt == nil || u.PlanExpiresAt.After(now)
}

// Quota is the per-user download allowance, serialised in every user payload.
type Quota struct {
	Plan              string `json:"plan"`
	IsMember          bool   `json:"is_member"`
	LinksUsed         int    `json:"links_used"`
	LinksLimit        int    `json:"links_limit"`
	LinksUnlimited    bool   `json:"links_unlimited"`
	FilesUsed         int    `json:"files_used"`
	FilesLimit        int    `json:"files_limit"`
	FilesUnlimited    bool   `json:"files_unlimited"`
	ConcurrentLimit   int    `json:"concurrent_limit"`
	ConcurrentRunning int    `json:"concurrent_running"`
}

// TaskOptions are the per-task download settings persisted as JSON.
type TaskOptions struct {
	MaxRetries    int      `json:"max_retries"`
	Connections   int      `json:"connections"`
	RateLimitKbps int      `json:"rate_limit_kbps"`
	Ignore        []string `json:"ignore"`
	Include       []string `json:"include"`
	NoAlbumFolder bool     `json:"no_album_folder"`
	CleanName     bool     `json:"clean_name"`
	CustomPath    string   `json:"custom_path"`
}

// DefaultTaskOptions returns the defaults applied when a task omits options.
func DefaultTaskOptions() TaskOptions {
	return TaskOptions{
		MaxRetries:  5,
		Connections: 4,
		Ignore:      []string{},
		Include:     []string{},
	}
}

// Task aggregates the download of one Bunkr URL.
type Task struct {
	ID           int64       `json:"id"`
	UserID       int64       `json:"user_id"`
	URL          string      `json:"url"`
	Kind         string      `json:"kind"`
	AlbumID      string      `json:"album_id"`
	AlbumName    string      `json:"album_name"`
	Status       string      `json:"status"`
	DownloadPath string      `json:"download_path"`
	Options      TaskOptions `json:"options"`
	ErrorMessage string      `json:"error_message,omitempty"`

	TotalFiles       int64   `json:"total_files"`
	CompletedFiles   int64   `json:"completed_files"`
	FailedFiles      int64   `json:"failed_files"`
	SkippedFiles     int64   `json:"skipped_files"`
	PendingFiles     int64   `json:"pending_files"`
	DownloadingFiles int64   `json:"downloading_files"`
	TotalBytes       int64   `json:"total_bytes"`
	DownloadedBytes  int64   `json:"downloaded_bytes"`
	Speed            int64   `json:"speed"`
	Progress         float64 `json:"progress"`

	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// File is a single downloadable item inside a task.
type File struct {
	ID              int64      `json:"id"`
	TaskID          int64      `json:"task_id"`
	UserID          int64      `json:"user_id"`
	ItemURL         string     `json:"item_url"`
	Filename        string     `json:"filename"`
	DownloadLink    string     `json:"download_link"`
	GID             string     `json:"gid"`
	FileSize        int64      `json:"file_size"`
	DownloadedBytes int64      `json:"downloaded_bytes"`
	Speed           int64      `json:"speed"`
	Progress        float64    `json:"progress"`
	Status          string     `json:"status"`
	RetryCount      int        `json:"retry_count"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	ItemDate        *time.Time `json:"item_date"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Event is a timestamped log line attached to a task and/or file.
type Event struct {
	ID        int64     `json:"id"`
	TaskID    *int64    `json:"task_id"`
	FileID    *int64    `json:"file_id"`
	Level     string    `json:"level"`
	Event     string    `json:"event"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

// Order records a membership purchase.
type Order struct {
	ID          int64      `json:"id"`
	UserID      int64      `json:"user_id"`
	Plan        string     `json:"plan"`
	AmountCents int64      `json:"amount_cents"`
	Currency    string     `json:"currency"`
	Status      string     `json:"status"`
	Provider    string     `json:"provider"`
	TradeNo     string     `json:"trade_no"`
	CreatedAt   time.Time  `json:"created_at"`
	PaidAt      *time.Time `json:"paid_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

// Plan describes a purchasable membership tier.
type Plan struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	PriceCents  int64      `json:"price_cents"`
	Currency    string     `json:"currency"`
	PeriodDays  int        `json:"period_days"`
	Features    []string   `json:"features"`
	LinksLimit  int        `json:"-"` // -1 = unlimited
	FilesLimit  int        `json:"-"`
	Concurrency int        `json:"-"`
	Limits      PlanLimits `json:"limits"`
}

// PlanLimits is the JSON shape returned to clients for a plan.
type PlanLimits struct {
	Links      int `json:"links"`
	Files      int `json:"files"`
	Concurrent int `json:"concurrent"`
}
