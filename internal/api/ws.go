package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

const (
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 70 * time.Second
	wsPingPeriod = 25 * time.Second
	wsMaxMessage = 8 << 10
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 16 << 10,
	// The SPA is same-origin in production; the Vite dev server is allowed in dev.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // native clients send no Origin
		}
		return strings.HasPrefix(origin, "http://localhost") ||
			strings.HasPrefix(origin, "http://127.0.0.1") ||
			origin == "null"
	},
}

// handleWebSocket upgrades the connection and streams live progress frames.
func (s *Server) handleWebSocket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		token = auth.BearerToken(c.GetHeader("Authorization"))
	}
	claims, err := s.issuer.Parse(token)
	if err != nil {
		fail(c, http.StatusUnauthorized, CodeUnauthorized, "请先登录")
		return
	}
	user, err := s.store.GetUser(claims.UserID)
	if err != nil {
		fail(c, http.StatusUnauthorized, CodeUnauthorized, "账号不存在")
		return
	}

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.log.Warn("ws upgrade failed", "error", err)
		return
	}

	send, closer := hub.NewWSSender(ws)
	client, unregister := s.hub.Register(user.ID, func(p []byte) error { return send(p) })
	client.Close = closer
	defer unregister()
	defer func() { _ = ws.Close() }()

	// The first frame is a full snapshot so the UI can render immediately.
	s.sendHello(c, user, send)

	// Writer goroutine: ping keepalive + serialised writes.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(wsPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
				if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	ws.SetReadLimit(wsMaxMessage)
	_ = ws.SetReadDeadline(time.Now().Add(wsPongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				s.log.Debug("ws closed unexpectedly", "user", user.ID, "error", err)
			}
			break
		}
		_ = ws.SetReadDeadline(time.Now().Add(wsPongWait))
		s.handleWSCommand(c, user, client, send, raw)
	}
	close(done)
}

// sendHello writes the initial snapshot frame.
func (s *Server) sendHello(c *gin.Context, user *store.User, send func([]byte) error) {
	quota, err := s.store.Quota(user.ID)
	if err != nil {
		quota = store.Quota{Plan: user.Plan}
	}
	tasks, _, err := s.store.ListTasks(store.ListTasksParams{UserID: user.ID, Limit: 100})
	if err != nil {
		tasks = nil
	}
	stats, err := s.store.Stats(user.ID)
	if err != nil {
		stats = store.GlobalStats{}
	}
	frame := hub.Frame{
		Type: "hello",
		TS:   time.Now().Unix(),
		Data: map[string]any{
			"user":  user,
			"quota": quota,
			"stats": stats,
			"tasks": tasks,
			"aria2": s.manager.Aria2Stats(c.Request.Context()),
		},
	}
	if payload, err := marshalFrame(frame); err == nil {
		_ = send(payload)
	}
}

// handleWSCommand processes a client → server control message.
func (s *Server) handleWSCommand(c *gin.Context, user *store.User, client *hub.Client, send func([]byte) error, raw []byte) {
	cmd, err := decodeCommand(raw)
	if err != nil {
		_ = sendFrame(send, hub.Frame{Type: "error", TS: time.Now().Unix(),
			Data: map[string]any{"code": "bad_request", "message": "无法解析的消息"}})
		return
	}
	switch cmd.Action {
	case "ping":
		_ = sendFrame(send, hub.Frame{Type: "pong", TS: time.Now().Unix(),
			Data: map[string]any{"ts": time.Now().Unix()}})
	case "subscribe":
		if cmd.TaskID > 0 {
			if _, err := s.store.TaskForUser(cmd.TaskID, user.ID); err == nil {
				client.Subscribe(cmd.TaskID)
			}
		}
	case "unsubscribe":
		if cmd.TaskID > 0 {
			client.Unsubscribe(cmd.TaskID)
		}
	case "all":
		client.SetAll(cmd.All)
	case "resync":
		// The client reconnected after a gap: resend the snapshot.
		s.sendHello(c, user, send)
	}
}
