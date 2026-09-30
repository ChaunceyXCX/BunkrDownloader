// Package services exposes the BunkrDownloader backend to the Wails frontend.
//
// The desktop app reuses the exact same domain layer as the web build
// (store / auth / bunkr / aria2 / downloads); these types are thin, typed
// adapters that the Wails binding generator turns into TypeScript clients.
//
// Authentication is identical to the web app: a JWT is issued on login and the
// frontend sends it back on every call, so the desktop build shares the same
// identity, quota and membership semantics.
package services

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkr"
	"github.com/chaunceyxie1/BunkrDownloader/internal/downloads"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// APIError is the error payload every service returns instead of a Go error.
// The frontend renders Message directly, so it is always user-facing Chinese.
type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string { return e.Message }

// Error codes mirrored from docs/API.md.
const (
	CodeBadRequest       = "bad_request"
	CodeInvalidURL       = "invalid_url"
	CodeWeakPassword     = "weak_password"
	CodeEmailTaken       = "email_taken"
	CodeUsernameTaken    = "username_taken"
	CodeInvalidCreds     = "invalid_credentials"
	CodeInvalidState     = "invalid_state"
	CodeUnauthorized     = "unauthorized"
	CodeQuotaExceeded    = "quota_exceeded"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeInvalidCode      = "invalid_code"
	CodeAria2Unavailable = "aria2_unavailable"
)

// Session is what Register/Login return.
type Session struct {
	User  *store.User  `json:"user"`
	Token string       `json:"token"`
	Quota *store.Quota `json:"quota"`
}

// App bundles the shared dependencies handed to every service.
type App struct {
	Store   *store.Store
	Issuer  *auth.Issuer
	Manager *downloads.Manager
	Hub     *hub.Hub
	Log     *slog.Logger

	// dataDir is reported to the settings screen.
	dataDir string

	mu sync.RWMutex
	// session remembers the last authenticated user so single-window desktop
	// use does not require passing a token on every call. The frontend still
	// holds and sends the token; this is the fallback for early calls.
	userID int64
	token  string
}

// NewApp wires the shared dependencies.
func NewApp(st *store.Store, iss *auth.Issuer, mgr *downloads.Manager, h *hub.Hub, log *slog.Logger) *App {
	if log == nil {
		log = slog.Default()
	}
	return &App{Store: st, Issuer: iss, Manager: mgr, Hub: h, Log: log}
}

// currentToken returns the effective token, echoing the session token when the
// caller passed none.
func (a *App) currentToken(token string) string {
	if strings.TrimSpace(token) != "" {
		return token
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.token
}

// currentUser resolves the caller from an explicit token, falling back to the
// session captured at login.
func (a *App) currentUser(token string) (*store.User, *APIError) {
	token = strings.TrimSpace(token)
	if token == "" {
		a.mu.RLock()
		token = a.token
		a.mu.RUnlock()
	}
	if token == "" {
		return nil, &APIError{Code: CodeUnauthorized, Message: "请先登录"}
	}
	claims, err := a.Issuer.Parse(token)
	if err != nil {
		return nil, &APIError{Code: CodeUnauthorized, Message: "登录状态已失效，请重新登录"}
	}
	user, err := a.Store.GetUser(claims.UserID)
	if err != nil {
		return nil, &APIError{Code: CodeUnauthorized, Message: "账号不存在"}
	}
	a.mu.Lock()
	a.userID, a.token = user.ID, token
	a.mu.Unlock()
	return user, nil
}

// resolveUserID is the common "who is calling" helper.
func (a *App) resolveUserID(token string) (int64, *APIError) {
	user, apiErr := a.currentUser(token)
	if apiErr != nil {
		return 0, apiErr
	}
	return user.ID, nil
}

func (a *App) setSession(userID int64, token string) {
	a.mu.Lock()
	a.userID, a.token = userID, token
	a.mu.Unlock()
}

func (a *App) clearSession() {
	a.mu.Lock()
	a.userID, a.token = 0, ""
	a.mu.Unlock()
}

// translate maps a store/domain error onto an APIError.
func (a *App) translate(err error) *APIError {
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	var qe *store.ErrQuota
	if errors.As(err, &qe) {
		return &APIError{
			Code: CodeQuotaExceeded, Message: qe.Error(),
			Details: map[string]any{
				"resource": qe.Resource, "limit": qe.Limit,
				"used": qe.Used, "requested": qe.Requested,
			},
		}
	}
	var ise *downloads.InvalidStateError
	if errors.As(err, &ise) {
		return &APIError{
			Code: CodeInvalidState, Message: ise.Error(),
			Details: map[string]any{"status": ise.Status, "action": ise.Action},
		}
	}
	var urlErr *bunkr.ErrInvalidURL
	if errors.As(err, &urlErr) {
		return &APIError{
			Code:    CodeInvalidURL,
			Message: "无法识别的链接，请确认是 Bunkr 相册（/a/）或文件（/v/）地址",
		}
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return &APIError{Code: CodeNotFound, Message: "资源不存在"}
	case errors.Is(err, store.ErrEmailTaken):
		return &APIError{Code: CodeEmailTaken, Message: "该邮箱已被注册"}
	case errors.Is(err, store.ErrUsernameTaken):
		return &APIError{Code: CodeUsernameTaken, Message: "该用户名已被使用"}
	case errors.Is(err, store.ErrWeakPassword):
		return &APIError{Code: CodeWeakPassword, Message: "密码至少需要 6 个字符"}
	case errors.Is(err, store.ErrInvalidUsername):
		return &APIError{Code: CodeBadRequest, Message: "用户名需为 3-24 位字母、数字或下划线"}
	case errors.Is(err, store.ErrInvalidEmail):
		return &APIError{Code: CodeBadRequest, Message: "邮箱格式不正确"}
	case errors.Is(err, store.ErrCodeInvalid):
		return &APIError{Code: CodeInvalidCode, Message: "兑换码无效或已被使用"}
	case errors.Is(err, store.ErrFreePlanNotOrderable):
		return &APIError{Code: CodeBadRequest, Message: "免费版无需购买"}
	case errors.Is(err, store.ErrPlanNotFound):
		return &APIError{Code: CodeBadRequest, Message: "套餐不存在"}
	case errors.Is(err, auth.ErrBadPassword):
		return &APIError{Code: CodeInvalidCreds, Message: "用户名/邮箱或密码错误"}
	}
	var orderState *store.ErrOrderState
	if errors.As(err, &orderState) {
		return &APIError{
			Code:    CodeConflict,
			Message: fmt.Sprintf("订单当前状态为 %s，无法继续该操作", orderState.Status),
			Details: map[string]any{"status": orderState.Status},
		}
	}
	if strings.Contains(err.Error(), "aria2") {
		return &APIError{Code: CodeAria2Unavailable, Message: "aria2 下载引擎未就绪，请稍后重试"}
	}
	a.Log.Error("unhandled service error", "error", err)
	return &APIError{Code: "internal_error", Message: "服务内部错误: " + err.Error()}
}

// session builds the login/register response.
func (a *App) session(user *store.User) (*Session, *APIError) {
	token, _, err := a.Issuer.Token(user.ID, user.Username, user.Email, user.Plan)
	if err != nil {
		return nil, a.translate(err)
	}
	quota, err := a.Store.Quota(user.ID)
	if err != nil {
		return nil, a.translate(err)
	}
	a.setSession(user.ID, token)
	return &Session{User: user, Token: token, Quota: &quota}, nil
}

// StorePath reports the database directory, which the settings screen shows.
func (a *App) StorePath() string {
	if a.dataDir == "" {
		return "."
	}
	return a.dataDir
}

// SetDataDir records where the database lives.
func (a *App) SetDataDir(dir string) { a.dataDir = dir }
