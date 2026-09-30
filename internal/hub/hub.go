// Package hub implements the WebSocket fan-out used for live progress updates.
//
// A single goroutine owns every registered client; publishers push messages
// onto a buffered channel and never block on slow sockets.
package hub

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Frame is the envelope for every server → client message.
type Frame struct {
	Type string `json:"type"`
	TS   int64  `json:"ts"`
	Data any    `json:"data,omitempty"`
}

// SendFunc delivers a pre-marshalled frame to one client.
// A closer is optionally supplied so the hub can drop sockets on shutdown.
type SendFunc func(payload []byte) error

// Closer lets the hub close the underlying socket during shutdown.
type Closer interface{ Close() error }

// Subscription describes the per-connection view of the event stream.
type Subscription struct {
	Tasks map[int64]bool // task ids the client explicitly subscribed to
	All   bool           // when true the client receives every user event
}

// Client is one connected browser/tab.
type Client struct {
	ID   uint64
	User int64
	Send SendFunc
	// Close is called when the hub shuts down; may be nil.
	Close func() error

	mu   sync.RWMutex
	subs Subscription
	// dropped counts messages skipped because the socket was not writable.
	dropped atomic.Int64
}

// Subscribe adds a task to the client's filter.
func (c *Client) Subscribe(taskID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.subs.Tasks == nil {
		c.subs.Tasks = map[int64]bool{}
	}
	c.subs.Tasks[taskID] = true
}

// Unsubscribe removes a task from the client's filter.
func (c *Client) Unsubscribe(taskID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subs.Tasks, taskID)
}

// SetAll toggles the "every event" mode.
func (c *Client) SetAll(all bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs.All = all
}

// Wants reports whether an event for taskID should be delivered.
func (c *Client) Wants(taskID int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.subs.All {
		return true
	}
	return taskID == 0 || c.subs.Tasks[taskID]
}

// Dropped returns the number of messages discarded for this client.
func (c *Client) Dropped() int64 { return c.dropped.Load() }

// Hub fans frames out to the clients of a single user.
type Hub struct {
	log *slog.Logger

	mu      sync.RWMutex
	clients map[uint64]*Client
	nextID  atomic.Uint64

	queue  chan envelope
	closed chan struct{}
	once   sync.Once
}

type envelope struct {
	userID int64
	taskID int64
	frame  Frame
}

// New builds a hub. queueSize bounds the backlog before messages are dropped.
func New(log *slog.Logger, queueSize int) *Hub {
	if log == nil {
		log = slog.Default()
	}
	if queueSize <= 0 {
		queueSize = 1024
	}
	h := &Hub{
		log:     log,
		clients: make(map[uint64]*Client),
		queue:   make(chan envelope, queueSize),
		closed:  make(chan struct{}),
	}
	go h.run()
	return h
}

// Register adds a client and returns an unregister function.
func (h *Hub) Register(userID int64, send SendFunc) (*Client, func()) {
	c := &Client{
		ID:   h.nextID.Add(1),
		User: userID,
		Send: send,
		subs: Subscription{Tasks: map[int64]bool{}},
	}
	c.SetAll(true)

	h.mu.Lock()
	h.clients[c.ID] = c
	n := len(h.clients)
	h.mu.Unlock()

	h.log.Debug("ws client registered", "user", userID, "client", c.ID, "total", n)

	var once sync.Once
	return c, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.clients, c.ID)
			remaining := len(h.clients)
			h.mu.Unlock()
			h.log.Debug("ws client removed", "user", userID, "client", c.ID, "total", remaining)
		})
	}
}

// Clients reports the number of live connections for a user (or in total when
// userID is negative).
func (h *Hub) Clients(userID int64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if userID < 0 {
		return len(h.clients)
	}
	n := 0
	for _, c := range h.clients {
		if c.User == userID {
			n++
		}
	}
	return n
}

// Publish enqueues a frame for every client of userID interested in taskID.
// It never blocks: when the queue is saturated the message is dropped and
// clients recover on the next REST poll.
func (h *Hub) Publish(userID, taskID int64, frameType string, data any) {
	select {
	case <-h.closed:
		return
	default:
	}
	env := envelope{
		userID: userID,
		taskID: taskID,
		frame:  Frame{Type: frameType, TS: time.Now().Unix(), Data: data},
	}
	select {
	case h.queue <- env:
	default:
		h.log.Warn("ws queue full; dropping frame", "type", frameType, "user", userID)
	}
}

// Close shuts the hub down and closes all client sockets.
func (h *Hub) Close() {
	h.once.Do(func() {
		close(h.closed)
		h.mu.Lock()
		for _, c := range h.clients {
			if c.Close != nil {
				_ = c.Close()
			}
		}
		h.clients = map[uint64]*Client{}
		h.mu.Unlock()
	})
}

func (h *Hub) run() {
	for {
		select {
		case <-h.closed:
			return
		case env := <-h.queue:
			h.deliver(env)
		}
	}
}

func (h *Hub) deliver(env envelope) {
	payload, err := json.Marshal(env.frame)
	if err != nil {
		h.log.Error("ws marshal failed", "error", err, "type", env.frame.Type)
		return
	}
	h.mu.RLock()
	targets := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		if c.User != env.userID {
			continue
		}
		if env.taskID != 0 && !c.Wants(env.taskID) {
			continue
		}
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		if err := c.Send(payload); err != nil {
			c.dropped.Add(1)
		}
	}
}

// NewWSSender adapts a gorilla connection to SendFunc with a write deadline
// and returns the matching close function for hub shutdown.
func NewWSSender(ws *websocket.Conn) (SendFunc, func() error) {
	send := func(payload []byte) error {
		_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return ws.WriteMessage(websocket.TextMessage, payload)
	}
	return send, func() error { return ws.Close() }
}
