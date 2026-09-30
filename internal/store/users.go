package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
)

// QuotaLimits is the configured allowance per plan.
type QuotaLimits struct {
	Links      int
	Files      int
	Concurrent int
}

var (
	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,24}$`)
	emailRe    = regexp.MustCompile(`^[^@\s]+@[^@\s.]+\.[^@\s]+$`)
)

// ErrQuota is returned when a user exceeds their plan allowance.
type ErrQuota struct {
	Resource  string // "links" | "files"
	Limit     int
	Used      int
	Requested int
}

func (e *ErrQuota) Error() string {
	if e.Resource == "links" {
		return fmt.Sprintf("免费用户最多添加 %d 个链接（已用 %d，本次需要 %d）。开通会员即可无限添加。", e.Limit, e.Used, e.Requested)
	}
	return fmt.Sprintf("免费用户最多下载 %d 个文件（已用 %d，本次需要 %d）。开通会员即可无限下载。", e.Limit, e.Used, e.Requested)
}

// Code returns the machine-readable quota error code.
func (e *ErrQuota) Code() string { return "quota_exceeded" }

var (
	// ErrEmailTaken is returned when the email is already registered.
	ErrEmailTaken = errors.New("email already taken")
	// ErrUsernameTaken is returned when the username is already taken.
	ErrUsernameTaken = errors.New("username already taken")
	// ErrBadCredentials is returned on failed login.
	ErrBadCredentials = errors.New("invalid credentials")
	// ErrWeakPassword is returned when the password does not meet the minimum.
	ErrWeakPassword = errors.New("password too weak")
	// ErrInvalidEmail is returned for malformed email addresses.
	ErrInvalidEmail = errors.New("invalid email")
	// ErrInvalidUsername is returned for malformed usernames.
	ErrInvalidUsername = errors.New("invalid username")
	// ErrPlanNotFound is returned for an unknown plan id.
	ErrPlanNotFound = errors.New("plan not found")
	// ErrFreePlanNotOrderable is returned when the free tier is "purchased".
	ErrFreePlanNotOrderable = errors.New("the free plan cannot be ordered")
	// ErrCodeInvalid is returned when a redeem code is unknown or already used.
	ErrCodeInvalid = errors.New("invalid redeem code")
)

// ValidateUsername enforces the 3-24 alphanumeric/underscore rule.
func ValidateUsername(name string) error {
	if !usernameRe.MatchString(name) {
		return ErrInvalidUsername
	}
	return nil
}

// ValidateEmail enforces a basic RFC-ish email shape.
func ValidateEmail(email string) error {
	if len(email) > 190 || !emailRe.MatchString(email) {
		return ErrInvalidEmail
	}
	return nil
}

// ValidatePassword enforces the 6 character minimum.
func ValidatePassword(pw string) error {
	if len([]rune(pw)) < 6 {
		return ErrWeakPassword
	}
	return nil
}

// CreateUser registers a new account.
//
// It takes the plaintext password and hashes it itself, so no caller can
// accidentally store an unhashed secret.
func (s *Store) CreateUser(username, email, password string) (*User, error) {
	username = strings.TrimSpace(username)
	email = strings.ToLower(strings.TrimSpace(email))

	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := ValidateEmail(email); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}

	now := nowUTC()
	var id int64
	err = s.tx(func(tx *sql.Tx) error {
		// Pre-check for nicer error codes than the driver would produce.
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE lower(username)=lower(?)`, username).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrUsernameTaken
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE email=?`, email).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrEmailTaken
		}

		res, err := tx.Exec(`
            INSERT INTO users (username, email, password_hash, role, plan, created_at)
            VALUES (?, ?, ?, ?, ?, ?)`,
			username, email, passwordHash, RoleUser, PlanFree, now)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetUser(id)
}

// GetUser loads a user by id.
func (s *Store) GetUser(id int64) (*User, error) {
	return s.scanUser(s.db.QueryRow(userSelect+` WHERE id = ?`, id))
}

// GetUserByAccount loads a user by username or email (case-insensitive).
func (s *Store) GetUserByAccount(account string) (*User, error) {
	account = strings.TrimSpace(account)
	return s.scanUser(s.db.QueryRow(userSelect+` WHERE lower(username)=lower(?) OR email=?`, account, strings.ToLower(account)))
}

const userSelect = `SELECT id, username, email, password_hash, role, plan, plan_expires_at, created_at FROM users`

type rowScanner interface{ Scan(dest ...any) error }

func (s *Store) scanUser(row rowScanner) (*User, error) {
	var u User
	var expires sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role,
		&u.Plan, &expires, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.PlanExpiresAt = timePtr(expires)
	u.CreatedAt = u.CreatedAt.UTC()
	return &u, nil
}

// CountUsers returns the total number of registered accounts.
func (s *Store) CountUsers() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// ChangePassword sets a new password, hashing it internally.
func (s *Store) ChangePassword(userID int64, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, userID)
		return err
	})
}

// UpdateProfile changes the display name of an account.
func (s *Store) UpdateProfile(userID int64, username, email string) error {
	username = strings.TrimSpace(username)
	email = strings.ToLower(strings.TrimSpace(email))
	if err := ValidateUsername(username); err != nil {
		return err
	}
	if err := ValidateEmail(email); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE users SET username=?, email=? WHERE id=? AND username<>?`,
			username, email, userID, username); err != nil {
			return err
		}
		// A change of username/email may now collide with another account.
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE lower(username)=lower(?) AND id<>?`,
			username, userID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrUsernameTaken
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE email=? AND id<>?`, email, userID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrEmailTaken
		}
		return nil
	})
}

// ------------------------------------------------------------------ quotas

// Limits returns the allowance for the user's current plan.
// Configure sets the per-plan quota limits; call once during startup.
func (s *Store) Configure(free, member QuotaLimits) {
	s.freeLimits = free
	s.memberLimits = member
	s.plans = buildPlans(free, member)
}

// SetFreeLimits updates free-tier limits at runtime (used by settings UI).
func (s *Store) SetFreeLimits(l QuotaLimits) { s.freeLimits = l }

// FreeLimits returns the currently configured free-tier limits.
func (s *Store) FreeLimits() QuotaLimits { return s.freeLimits }

// Plans returns the purchasable plan catalogue.
func (s *Store) Plans() []Plan { return s.plans }

// LimitsForPlan returns the quota allowance of a plan id.
func (s *Store) LimitsForPlan(plan string) QuotaLimits {
	if plan == PlanMember {
		return s.memberLimits
	}
	return s.freeLimits
}

// Quota computes the live quota snapshot for a user, counting used resources.
func (s *Store) Quota(userID int64) (Quota, error) {
	u, err := s.GetUser(userID)
	if err != nil {
		return Quota{}, err
	}
	limits := s.LimitsForPlan(u.Plan)
	if u.IsMember(time.Now()) {
		limits = s.memberLimits
	}
	unlimited := limits.Links < 0

	q := Quota{
		Plan:            u.Plan,
		IsMember:        u.IsMember(time.Now()),
		LinksLimit:      limits.Links,
		FilesLimit:      limits.Files,
		ConcurrentLimit: limits.Concurrent,
		LinksUnlimited:  unlimited,
		FilesUnlimited:  limits.Files < 0,
	}
	if q.LinksLimit < 0 {
		q.LinksLimit = 0
	}
	if q.FilesLimit < 0 {
		q.FilesLimit = 0
	}
	if q.ConcurrentLimit < 0 {
		q.ConcurrentLimit = 0
	}

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id=?`, userID).Scan(&q.LinksUsed); err != nil {
		return q, err
	}
	if err := s.db.QueryRow(`
        SELECT COUNT(*) FROM files
        WHERE user_id=? AND status NOT IN ('skipped')`, userID).Scan(&q.FilesUsed); err != nil {
		return q, err
	}
	if err := s.db.QueryRow(`
        SELECT COUNT(*) FROM tasks WHERE user_id=? AND status IN ('running','crawling')`,
		userID).Scan(&q.ConcurrentRunning); err != nil {
		return q, err
	}
	return q, nil
}

// CanAddLinks validates the link allowance before creating tasks.
func (s *Store) CanAddLinks(userID int64, count int) error {
	q, err := s.Quota(userID)
	if err != nil {
		return err
	}
	if q.LinksUnlimited {
		return nil
	}
	if q.LinksUsed+count > q.LinksLimit {
		return &ErrQuota{Resource: "links", Limit: q.LinksLimit, Used: q.LinksUsed, Requested: count}
	}
	return nil
}

// CanAddFiles validates the file allowance for n additional files.
// A non-positive n means "unknown until crawled": the caller checks the hard
// ceiling separately once the real number of items is known.
func (s *Store) CanAddFiles(userID int64, n int) error {
	q, err := s.Quota(userID)
	if err != nil {
		return err
	}
	if q.FilesUnlimited {
		return nil
	}
	if n <= 0 {
		// Unknown count: refuse only if the user is already at the ceiling.
		if q.FilesUsed >= q.FilesLimit {
			return &ErrQuota{Resource: "files", Limit: q.FilesLimit, Used: q.FilesUsed, Requested: 1}
		}
		return nil
	}
	if q.FilesUsed+n > q.FilesLimit {
		return &ErrQuota{Resource: "files", Limit: q.FilesLimit, Used: q.FilesUsed, Requested: n}
	}
	return nil
}

// RemainingFiles is how many more files a user may still register.
func (s *Store) RemainingFiles(userID int64) int {
	q, err := s.Quota(userID)
	if err != nil {
		return 0
	}
	if q.FilesUnlimited {
		return -1
	}
	rem := q.FilesLimit - q.FilesUsed
	if rem < 0 {
		return 0
	}
	return rem
}

// ------------------------------------------------------------- membership

// ActivateMembership grants (or extends) a membership for periodDays.
// A nil base means "start from now"; otherwise it extends the current expiry.
func (s *Store) ActivateMembership(userID int64, plan string, periodDays int) error {
	return s.tx(func(tx *sql.Tx) error {
		var curPlan string
		var expires sql.NullTime
		if err := tx.QueryRow(`SELECT plan, plan_expires_at FROM users WHERE id=?`, userID).
			Scan(&curPlan, &expires); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		base := nowUTC()
		if curPlan == PlanMember && expires.Valid && expires.Time.After(base) {
			base = expires.Time.UTC()
		}
		next := base.AddDate(0, 0, periodDays)
		_, err := tx.Exec(`UPDATE users SET plan=?, plan_expires_at=? WHERE id=?`, PlanMember, next, userID)
		return err
	})
}

// RedeemCode consumes a membership code for a user.
func (s *Store) RedeemCode(userID int64, code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	return s.tx(func(tx *sql.Tx) error {
		var plan string
		var days int
		var usedBy sql.NullInt64
		err := tx.QueryRow(`SELECT plan, period_days, used_by FROM redeem_codes WHERE code=?`, code).
			Scan(&plan, &days, &usedBy)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && usedBy.Valid) {
			return ErrCodeInvalid
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE redeem_codes SET used_by=?, used_at=? WHERE code=?`,
			userID, nowUTC(), code); err != nil {
			return err
		}
		var curPlan string
		var expires sql.NullTime
		if err := tx.QueryRow(`SELECT plan, plan_expires_at FROM users WHERE id=?`, userID).
			Scan(&curPlan, &expires); err != nil {
			return err
		}
		base := nowUTC()
		if curPlan == PlanMember && expires.Valid && expires.Time.After(base) {
			base = expires.Time.UTC()
		}
		_, err = tx.Exec(`UPDATE users SET plan=?, plan_expires_at=? WHERE id=?`,
			PlanMember, base.AddDate(0, 0, days), userID)
		return err
	})
}

// EnsureRedeemCodes seeds the default demo codes when the table is empty.
func (s *Store) EnsureRedeemCodes() error {
	return s.tx(func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM redeem_codes`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		now := nowUTC()
		seeds := []struct {
			code, plan string
			days       int
		}{
			{"BUNKR-MEMBER-2024", PlanMember, 30},
			{"BUNKR-MEMBER-2025", PlanMember, 365},
		}
		for _, sd := range seeds {
			if _, err := tx.Exec(
				`INSERT INTO redeem_codes (code, plan, period_days, created_at) VALUES (?,?,?,?)`,
				sd.code, sd.plan, sd.days, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildPlans(free, member QuotaLimits) []Plan {
	return []Plan{
		{
			ID: PlanFree, Name: "免费版", PriceCents: 0, Currency: "CNY", PeriodDays: 0,
			Features: []string{
				"最多添加 5 个链接",
				"最多下载 50 个文件",
				"同时运行 1 个下载任务",
			},
			LinksLimit: free.Links, FilesLimit: free.Files, Concurrency: free.Concurrent,
			Limits: PlanLimits{Links: free.Links, Files: free.Files, Concurrent: free.Concurrent},
		},
		{
			ID: "member_monthly", Name: "会员 · 月付", PriceCents: 990, Currency: "CNY", PeriodDays: 30,
			Features: []string{
				"无限链接、无限文件",
				"同时运行 5 个下载任务",
				"高优先级队列",
				"全功能支持",
			},
			LinksLimit: member.Links, FilesLimit: member.Files, Concurrency: member.Concurrent,
			Limits: PlanLimits{Links: member.Links, Files: member.Files, Concurrent: member.Concurrent},
		},
		{
			ID: "member_yearly", Name: "会员 · 年付", PriceCents: 9900, Currency: "CNY", PeriodDays: 365,
			Features: []string{
				"无限链接、无限文件",
				"同时运行 5 个下载任务",
				"高优先级队列",
				"全功能支持",
				"比月付省 17%",
			},
			LinksLimit: member.Links, FilesLimit: member.Files, Concurrency: member.Concurrent,
			Limits: PlanLimits{Links: member.Links, Files: member.Files, Concurrent: member.Concurrent},
		},
	}
}
