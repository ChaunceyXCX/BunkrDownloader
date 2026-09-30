// Package api wires the Gin HTTP surface: REST handlers, WebSocket upgrade,
// middleware and the embedded single-page application.
package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/bunkr"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// contextKey is the private type for values this package stores on the context.
type contextKey string

const (
	ctxUserKey   contextKey = "bunkr.user"
	ctxClaimsKey contextKey = "bunkr.claims"
)

// ErrorBody is the uniform error envelope (see docs/API.md §0.1).
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// fail writes a JSON error response.
func fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: ErrorBody{Code: code, Message: message}})
}

// failDetails writes a JSON error response with extra context.
func failDetails(c *gin.Context, status int, code, message string, details any) {
	c.AbortWithStatusJSON(status, errorEnvelope{
		Error: ErrorBody{Code: code, Message: message, Details: details},
	})
}

// Error codes shared with the frontend.
const (
	CodeBadRequest       = "bad_request"
	CodeInvalidURL       = "invalid_url"
	CodeWeakPassword     = "weak_password"
	CodeEmailTaken       = "email_taken"
	CodeUsernameTaken    = "username_taken"
	CodeInvalidCreds     = "invalid_credentials"
	CodeInvalidState     = "invalid_state"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeQuotaExceeded    = "quota_exceeded"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeTooManyRequests  = "too_many_requests"
	CodeInternal         = "internal_error"
	CodeUpstream         = "upstream_error"
	CodeAria2Unavailable = "aria2_unavailable"
	CodeWeakUsername     = "weak_username"
	CodeInvalidEmail     = "invalid_email"
	CodeInvalidCode      = "invalid_code"
)

// failStoreError maps store/auth errors onto HTTP responses.
func failStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, CodeNotFound, "资源不存在")
	case errors.Is(err, store.ErrEmailTaken):
		fail(c, http.StatusBadRequest, CodeEmailTaken, "该邮箱已被注册")
	case errors.Is(err, store.ErrUsernameTaken):
		fail(c, http.StatusBadRequest, CodeUsernameTaken, "该用户名已被使用")
	case errors.Is(err, store.ErrWeakPassword):
		fail(c, http.StatusBadRequest, CodeWeakPassword, "密码至少需要 6 个字符")
	case errors.Is(err, store.ErrInvalidUsername):
		fail(c, http.StatusBadRequest, CodeWeakUsername, "用户名需为 3-24 位字母、数字或下划线")
	case errors.Is(err, store.ErrInvalidEmail):
		fail(c, http.StatusBadRequest, CodeInvalidEmail, "邮箱格式不正确")
	case errors.Is(err, auth.ErrInvalidToken):
		fail(c, http.StatusUnauthorized, CodeUnauthorized, "登录状态已失效，请重新登录")
	case errors.Is(err, auth.ErrBadPassword):
		fail(c, http.StatusBadRequest, CodeInvalidCreds, "用户名/邮箱或密码错误")
	default:
		var qe *store.ErrQuota
		if errors.As(err, &qe) {
			failDetails(c, http.StatusForbidden, CodeQuotaExceeded, qe.Error(), map[string]any{
				"resource": qe.Resource, "limit": qe.Limit,
				"used": qe.Used, "requested": qe.Requested,
			})
			return
		}
		var iue *bunkr.ErrInvalidURL
		if errors.As(err, &iue) {
			fail(c, http.StatusBadRequest, CodeInvalidURL,
				"无法识别的链接，请确认是 Bunkr 相册或文件地址")
			return
		}
		fail(c, http.StatusInternalServerError, CodeInternal, "服务内部错误: "+err.Error())
	}
}

// ---------------------------------------------------------------- middleware

// authRequired rejects requests without a valid bearer token.
func (s *Server) authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := auth.BearerToken(c.GetHeader("Authorization"))
		if token == "" {
			// Allow ?token= for EventSource-style clients.
			token = c.Query("token")
		}
		if token == "" {
			fail(c, http.StatusUnauthorized, CodeUnauthorized, "请先登录")
			return
		}
		claims, err := s.issuer.Parse(token)
		if err != nil {
			fail(c, http.StatusUnauthorized, CodeUnauthorized, "登录状态已失效，请重新登录")
			return
		}
		user, err := s.store.GetUser(claims.UserID)
		if err != nil {
			fail(c, http.StatusUnauthorized, CodeUnauthorized, "账号不存在")
			return
		}
		c.Set(string(ctxUserKey), user)
		c.Set(string(ctxClaimsKey), claims)
		c.Next()
	}
}

// rateLimit is a small fixed-window limiter protecting the auth endpoints
// from credential stuffing.
func rateLimit(limit int, windowSec int) gin.HandlerFunc {
	type bucket struct {
		count int
		reset time.Time
	}
	var mu sync.Mutex
	buckets := map[string]*bucket{}

	return func(c *gin.Context) {
		key := c.ClientIP()
		now := time.Now()

		mu.Lock()
		b, ok := buckets[key]
		if !ok || now.After(b.reset) {
			b = &bucket{reset: now.Add(time.Duration(windowSec) * time.Second)}
			buckets[key] = b
		}
		allowed := b.count < limit
		if allowed {
			b.count++
		}
		mu.Unlock()

		if !allowed {
			c.Header("Retry-After", strconv.Itoa(windowSec))
			fail(c, http.StatusTooManyRequests, CodeTooManyRequests, "操作过于频繁，请稍后再试")
			return
		}
		c.Next()
	}
}

// currentUser returns the authenticated user from the context.
func currentUser(c *gin.Context) *store.User {
	v, ok := c.Get(string(ctxUserKey))
	if !ok {
		return nil
	}
	u, _ := v.(*store.User)
	return u
}

func currentUserID(c *gin.Context) int64 {
	if u := currentUser(c); u != nil {
		return u.ID
	}
	return 0
}

// ------------------------------------------------------------ query helpers

func queryInt(c *gin.Context, key string, def int) int {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func queryInt64Ptr(c *gin.Context, key string) *int64 {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func pathInt64(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
