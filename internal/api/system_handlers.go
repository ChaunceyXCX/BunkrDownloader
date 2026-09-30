package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// handleHealth is public and used by the Docker HEALTHCHECK.
func (s *Server) handleHealth(c *gin.Context) {
	ariaOK := s.manager != nil && s.manager.Aria2Healthy(c.Request.Context())
	status := "ok"
	if !ariaOK {
		// The HTTP surface is still healthy; aria2 being down is degraded.
		status = "degraded"
	}
	version := ""
	if s.manager != nil {
		version = s.manager.Aria2Version(c.Request.Context())
	}
	c.JSON(http.StatusOK, gin.H{
		"status":         status,
		"service":        "bunkr-web",
		"version":        s.cfg.Version,
		"uptime_seconds": int64(time.Since(s.started).Seconds()),
		"aria2": gin.H{
			"available": ariaOK,
			"version":   version,
		},
	})
}

// handleStats powers the dashboard counters.
func (s *Server) handleStats(c *gin.Context) {
	user := currentUser(c)
	stats, err := s.store.Stats(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	quota, err := s.store.Quota(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total_tasks":      stats.TotalTasks,
		"running":          stats.Running,
		"pending":          stats.Pending,
		"paused":           stats.Paused,
		"completed":        stats.Completed,
		"failed":           stats.Failed,
		"canceled":         stats.Canceled,
		"total_files":      stats.TotalFiles,
		"completed_files":  stats.CompletedFiles,
		"downloaded_bytes": stats.DownloadedBytes,
		"speed":            stats.Speed,
		"active_files":     stats.ActiveFiles,
		"aria2":            s.manager.Aria2Stats(c.Request.Context()),
		"quota":            quota,
	})
}

// handleSettings exposes non-secret runtime configuration to the UI.
func (s *Server) handleSettings(c *gin.Context) {
	paymentProvider := "mock"
	if !s.cfg.PaymentAuto {
		paymentProvider = "manual"
	}
	c.JSON(http.StatusOK, gin.H{
		"download_dir": s.cfg.DownloadDir,
		"version":      s.cfg.Version,
		"features": gin.H{
			"aria2":   s.cfg.Aria2Enabled,
			"payment": paymentProvider,
		},
		"defaults": store.DefaultTaskOptions(),
		"quota_limits": gin.H{
			"free": gin.H{
				"links":      s.store.FreeLimits().Links,
				"files":      s.store.FreeLimits().Files,
				"concurrent": s.store.FreeLimits().Concurrent,
			},
		},
	})
}
