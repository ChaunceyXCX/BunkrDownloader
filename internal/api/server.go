package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/config"
	"github.com/chaunceyxie1/BunkrDownloader/internal/downloads"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
	"github.com/chaunceyxie1/BunkrDownloader/internal/web"
)

// Deps bundles everything the HTTP layer needs.
type Deps struct {
	Cfg      *config.Config
	Store    *store.Store
	Issuer   *auth.Issuer
	Hub      *hub.Hub
	Manager  *downloads.Manager
	Log      *slog.Logger
	StaticFS http.FileSystem // the embedded SPA (nil in dev: use StaticDir)
	Started  time.Time
}

// Server owns the Gin engine.
type Server struct {
	cfg     *config.Config
	store   *store.Store
	issuer  *auth.Issuer
	hub     *hub.Hub
	manager *downloads.Manager
	log     *slog.Logger
	static  http.FileSystem
	started time.Time

	engine *gin.Engine
}

// NewServer builds the HTTP server.
func NewServer(d Deps) *Server {
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	started := d.Started
	if started.IsZero() {
		started = time.Now()
	}
	return &Server{
		cfg: d.Cfg, store: d.Store, issuer: d.Issuer, hub: d.Hub,
		manager: d.Manager, log: log, static: d.StaticFS, started: started,
	}
}

// Engine returns the configured Gin engine (building it on first call).
func (s *Server) Engine() *gin.Engine {
	if s.engine != nil {
		return s.engine
	}
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery(), s.requestLogger(), corsMiddleware(s.cfg.AllowOrigin))
	engine.RedirectTrailingSlash = true
	engine.MaxMultipartMemory = 8 << 20

	s.registerAPI(engine)
	s.registerStatic(engine)

	s.engine = engine
	return engine
}

// Run starts the HTTP server and blocks until the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Addr(),
		Handler:           s.Engine(),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // WebSockets are long-lived
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	s.log.Info("http server listening", "addr", s.cfg.Addr())
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// registerAPI mounts every /api route.
func (s *Server) registerAPI(engine *gin.Engine) {
	api := engine.Group("/api")

	// --- public ---
	api.GET("/health", s.handleHealth)
	api.GET("/membership/plans", s.handlePlans)

	// --- auth ---
	authLimiter := rateLimit(20, 60)
	api.POST("/auth/register", authLimiter, s.handleRegister)
	api.POST("/auth/login", authLimiter, s.handleLogin)
	api.POST("/auth/logout", s.handleLogout)
	api.GET("/auth/me", s.authRequired(), s.handleMe)
	api.POST("/auth/change-password", s.authRequired(), s.handleChangePassword)

	// --- authenticated ---
	priv := api.Group("", s.authRequired())
	priv.Use(s.userActivity())

	// membership
	priv.POST("/membership/orders", s.handleCreateOrder)
	priv.GET("/membership/orders", s.handleListOrders)
	priv.POST("/membership/orders/:id/pay", s.handlePayOrder)
	priv.POST("/membership/cancel-order", s.handleCancelOrder)
	priv.POST("/membership/redeem", s.handleRedeem)
	priv.GET("/membership/status", s.handleMembershipStatus)

	// tasks
	priv.GET("/tasks", s.handleListTasks)
	priv.POST("/tasks", s.handleCreateTask)
	priv.GET("/tasks/:id", s.handleGetTask)
	priv.DELETE("/tasks/:id", s.handleDeleteTask)
	priv.POST("/tasks/:id/start", s.handleTaskAction(store.TaskPending))
	priv.POST("/tasks/:id/pause", s.handlePause)
	priv.POST("/tasks/:id/resume", s.handleResume)
	priv.POST("/tasks/:id/cancel", s.handleCancel)
	priv.POST("/tasks/:id/retry", s.handleRetry)
	priv.GET("/tasks/:id/files", s.handleListFiles)
	priv.POST("/tasks/:id/files/:fileId/retry", s.handleRetryFile)
	priv.GET("/tasks/:id/events", s.handleTaskEvents)

	// system
	priv.GET("/events", s.handleAllEvents)
	priv.GET("/stats", s.handleStats)
	priv.GET("/settings", s.handleSettings)

	// websocket (token via query string, verified inside the handler)
	api.GET("/ws", s.handleWebSocket)
}

// requestLogger emits a compact structured log line per request.
func (s *Server) requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()
		if path == "/api/ws" || strings.HasPrefix(path, "/assets/") {
			return
		}
		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"ms", time.Since(start).Milliseconds(),
		}
		if uid := currentUserID(c); uid > 0 {
			attrs = append(attrs, "user", uid)
		}
		switch {
		case c.Writer.Status() >= 500:
			s.log.Error("http", attrs...)
		case c.Writer.Status() >= 400:
			s.log.Warn("http", attrs...)
		default:
			s.log.Info("http", attrs...)
		}
	}
}

// corsMiddleware allows the Vite dev server to call the API directly.
func corsMiddleware(origin string) gin.HandlerFunc {
	if origin == "" {
		origin = "*"
	}
	return func(c *gin.Context) {
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// userActivity re-reads the user on each request so plan changes take effect
// immediately (membership upgrades must not require a re-login).
func (s *Server) userActivity() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

// registerStatic serves the embedded SPA with a history-API fallback.
func (s *Server) registerStatic(engine *gin.Engine) {
	if s.static == nil {
		engine.NoRoute(func(c *gin.Context) {
			fail(c, http.StatusNotFound, CodeNotFound, "not found")
		})
		return
	}
	if !web.HasBuild() {
		// No frontend bundle in this binary: still expose a helpful page.
		engine.NoRoute(func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(web.PlaceholderHTML))
		})
		return
	}

	index := readIndex(s.static)
	fileServer := http.FileServer(s.static)

	engine.NoRoute(func(c *gin.Context) {
		path := strings.TrimPrefix(c.Request.URL.Path, "/")

		// A miss under /api is a genuine 404: never hand API paths to the SPA.
		if strings.HasPrefix(path, "api/") || path == "api" {
			fail(c, http.StatusNotFound, CodeNotFound, "接口不存在: /"+path)
			return
		}
		if path == "" {
			serveIndex(c, index)
			return
		}
		if f, err := s.static.Open("/" + path); err == nil {
			stat, statErr := f.Stat()
			_ = f.Close()
			if statErr == nil && !stat.IsDir() {
				if strings.HasPrefix(path, "assets/") {
					// Vite emits content-hashed asset names: cache hard.
					c.Header("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					c.Header("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		// Unknown path: hand it to the SPA router.
		serveIndex(c, index)
	})
}

func serveIndex(c *gin.Context, index []byte) {
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "text/html; charset=utf-8", index)
}

func readIndex(fs http.FileSystem) []byte {
	f, err := fs.Open("/index.html")
	if err != nil {
		return []byte("<!doctype html><meta charset=utf-8><title>BunkrDownloader</title><p>前端未构建：请在 frontend/ 执行 npm install && npm run build。")
	}
	defer f.Close()
	buf := make([]byte, 0, 32<<10)
	tmp := make([]byte, 32<<10)
	for {
		n, err := f.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf
}
