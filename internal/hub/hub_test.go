package hub

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// recorder is a fake client socket that records every payload it receives.
type recorder struct {
	mu       sync.Mutex
	frames   [][]byte
	failNext bool
	closed   bool
}

func (r *recorder) send(payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNext {
		r.failNext = false
		return errFake
	}
	r.frames = append(r.frames, append([]byte(nil), payload...))
	return nil
}

func (r *recorder) closeFn() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

func (r *recorder) decode(t *testing.T) []Frame {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Frame, 0, len(r.frames))
	for _, raw := range r.frames {
		var f Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("frame is not valid JSON: %s", raw)
		}
		out = append(out, f)
	}
	return out
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames)
}

func (r *recorder) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

var errFake = &fakeError{}

type fakeError struct{}

func (e *fakeError) Error() string { return "fake send failure" }

// waitFor polls until cond holds.
func waitFor(t *testing.T, d time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", desc)
}

func TestPublishReachesSubscribers(t *testing.T) {
	h := New(nil, 64)
	defer h.Close()

	rec := &recorder{}
	client, unregister := h.Register(7, rec.send)
	defer unregister()
	_ = client

	h.Publish(7, 0, "task_progress", map[string]any{"task": 1})
	waitFor(t, 2*time.Second, "the frame to arrive", func() bool { return rec.count() == 1 })

	frames := rec.decode(t)
	if frames[0].Type != "task_progress" {
		t.Errorf("frame type = %q", frames[0].Type)
	}
	if frames[0].TS == 0 {
		t.Error("frame timestamp was not set")
	}
	if frames[0].Data == nil {
		t.Error("frame payload was dropped")
	}
}

func TestPublishIsIsolatedPerUser(t *testing.T) {
	h := New(nil, 64)
	defer h.Close()

	alice := &recorder{}
	bob := &recorder{}
	_, un1 := h.Register(1, alice.send)
	defer un1()
	_, un2 := h.Register(2, bob.send)
	defer un2()

	h.Publish(1, 0, "quota", map[string]any{"n": 1})
	waitFor(t, 2*time.Second, "alice's frame", func() bool { return alice.count() == 1 })
	time.Sleep(80 * time.Millisecond)
	if bob.count() != 0 {
		t.Errorf("bob received %d frames addressed to alice", bob.count())
	}
}

func TestTaskSubscriptionFilter(t *testing.T) {
	h := New(nil, 64)
	defer h.Close()

	rec := &recorder{}
	client, unregister := h.Register(1, rec.send)
	defer unregister()

	// The default is "all", so unfiltered publishing reaches everyone.
	h.Publish(1, 42, "task_progress", nil)
	waitFor(t, time.Second, "the unfiltered frame", func() bool { return rec.count() == 1 })

	// Switch to a single-task view and prove the filter bites.
	client.SetAll(false)
	h.Publish(1, 99, "task_progress", nil)
	time.Sleep(80 * time.Millisecond)
	if rec.count() != 1 {
		t.Errorf("a non-subscribed task leaked a frame (%d total)", rec.count())
	}

	client.Subscribe(99)
	h.Publish(1, 99, "task_progress", nil)
	waitFor(t, time.Second, "the subscribed frame", func() bool { return rec.count() == 2 })

	client.Unsubscribe(99)
	h.Publish(1, 99, "task_progress", nil)
	time.Sleep(80 * time.Millisecond)
	if rec.count() != 2 {
		t.Errorf("an unsubscribed task still delivered (%d total)", rec.count())
	}

	// Global (task 0) frames are always delivered.
	h.Publish(1, 0, "stats", nil)
	waitFor(t, time.Second, "the global frame", func() bool { return rec.count() == 3 })
}

func TestUnregisterStopsDelivery(t *testing.T) {
	h := New(nil, 64)
	defer h.Close()

	rec := &recorder{}
	_, unregister := h.Register(5, rec.send)

	h.Publish(5, 0, "a", nil)
	waitFor(t, time.Second, "the first frame", func() bool { return rec.count() == 1 })

	unregister()
	if h.Clients(5) != 0 {
		t.Errorf("Clients after unregister = %d, want 0", h.Clients(5))
	}
	h.Publish(5, 0, "b", nil)
	time.Sleep(80 * time.Millisecond)
	if rec.count() != 1 {
		t.Errorf("a removed client still received %d frames", rec.count())
	}
}

func TestUnregisterIsIdempotent(t *testing.T) {
	h := New(nil, 16)
	defer h.Close()
	_, unregister := h.Register(1, (&recorder{}).send)
	unregister()
	unregister() // must not panic
}

func TestPublishDoesNotBlockOnSlowClients(t *testing.T) {
	// A tiny queue would drop frames; the publisher must still return fast.
	h := New(nil, 4)
	defer h.Close()
	rec := &recorder{}
	_, unregister := h.Register(1, rec.send)
	defer unregister()

	start := time.Now()
	for i := 0; i < 500; i++ {
		h.Publish(1, 0, "flood", i)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("publishing 500 frames took %v; the hub blocks", elapsed)
	}
}

func TestSendFailureIsCounted(t *testing.T) {
	h := New(nil, 64)
	defer h.Close()

	rec := &recorder{failNext: true}
	client, unregister := h.Register(1, rec.send)
	defer unregister()

	h.Publish(1, 0, "task_progress", nil)
	waitFor(t, time.Second, "the failed send", func() bool { return client.Dropped() == 1 })
}

func TestCloseClosesSockets(t *testing.T) {
	h := New(nil, 16)
	rec := &recorder{}
	client, _ := h.Register(1, rec.send)
	client.Close = rec.closeFn

	h.Close()
	if !rec.isClosed() {
		t.Error("Close did not close the underlying socket")
	}
	// Publishing after Close must be a no-op rather than a panic.
	h.Publish(1, 0, "late", nil)
	time.Sleep(50 * time.Millisecond)
	if rec.count() != 0 {
		t.Error("a frame was delivered after Close")
	}
	// Close is idempotent.
	h.Close()
}

func TestClientsCounting(t *testing.T) {
	h := New(nil, 16)
	defer h.Close()
	_, u1 := h.Register(1, (&recorder{}).send)
	_, u2 := h.Register(1, (&recorder{}).send)
	_, u3 := h.Register(2, (&recorder{}).send)
	defer u1()
	defer u2()
	defer u3()

	if got := h.Clients(1); got != 2 {
		t.Errorf("Clients(1) = %d, want 2", got)
	}
	if got := h.Clients(-1); got != 3 {
		t.Errorf("Clients(all) = %d, want 3", got)
	}
	u1()
	if got := h.Clients(1); got != 1 {
		t.Errorf("Clients(1) after unregister = %d, want 1", got)
	}
}

func TestWants(t *testing.T) {
	c := &Client{ID: 1, User: 1, subs: Subscription{Tasks: map[int64]bool{}}}
	if !c.Wants(0) {
		t.Error("global events should always be delivered")
	}
	if c.Wants(5) {
		t.Error("an unsubscribed task was accepted")
	}
	c.Subscribe(5)
	if !c.Wants(5) {
		t.Error("a subscribed task was rejected")
	}
	c.SetAll(true)
	if !c.Wants(999) {
		t.Error("All mode should accept every task")
	}
}
