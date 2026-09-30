package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/config"
	"github.com/chaunceyxie1/BunkrDownloader/internal/downloads"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// testEnv is a fully wired server backed by a temporary database.
type testEnv struct {
	t    *testing.T
	srv  *Server
	eng  *gin.Engine
	st   *store.Store
	hub  *hub.Hub
	dl   *downloads.Manager
	stop func()
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	dlDir := filepath.Join(root, "downloads")
	stateDir := filepath.Join(root, "state")
	for _, d := range []string{dlDir, stateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	st, err := store.Open(filepath.Join(stateDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	st.Configure(
		store.QuotaLimits{Links: 5, Files: 50, Concurrent: 1},
		store.QuotaLimits{Links: -1, Files: -1, Concurrent: 5},
	)
	if err := st.EnsureRedeemCodes(); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Version:           "test",
		FreeLinksLimit:    5,
		FreeFilesLimit:    50,
		FreeConcurrency:   1,
		MemberConcurrency: 5,
		DownloadDir:       dlDir,
		PaymentAuto:       true,
		Aria2Enabled:      false,
	}
	// aria2 is intentionally absent: these tests exercise the HTTP surface, and
	// the download manager degrades gracefully when the daemon is down.
	ariaMgr := newNullAria2(stateDir)
	h := hub.New(log, 256)
	mgr := downloads.New(downloads.Config{
		DownloadDir:    dlDir,
		PollInterval:   time.Second,
		DefaultOptions: store.DefaultTaskOptions(),
	}, st, ariaMgr, h, log)
	// The manager is not started: no background loops run during handler tests.

	srv := NewServer(Deps{
		Cfg: cfg, Store: st,
		Issuer: auth.NewIssuer("test-secret-value", time.Hour),
		Hub:    h, Manager: mgr, Log: log,
		StaticFS: http.FS(os.DirFS(staticDir(t))),
		Started:  time.Now(),
	})
	env := &testEnv{t: t, srv: srv, eng: srv.Engine(), st: st, hub: h, dl: mgr}
	t.Cleanup(func() {
		h.Close()
		st.Close()
	})
	return env
}

// do issues a request and decodes the JSON response.
func (e *testEnv) do(method, path, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	e.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.eng.ServeHTTP(rec, req)

	out := map[string]any{}
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			e.t.Fatalf("%s %s: response is not JSON: %s", method, path, rec.Body.String())
		}
	}
	return rec, out
}

func (e *testEnv) mustOK(method, path, token string, body any, wantStatus int) map[string]any {
	e.t.Helper()
	rec, out := e.do(method, path, token, body)
	if rec.Code != wantStatus {
		e.t.Fatalf("%s %s = %d, want %d (body: %s)", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	return out
}

// errCode extracts error.code from an error response.
func errCode(t *testing.T, body map[string]any) string {
	t.Helper()
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("response has no error object: %v", body)
	}
	code, _ := e["code"].(string)
	return code
}

// register creates an account and returns its token.
func (e *testEnv) register(name string) (string, int64) {
	e.t.Helper()
	out := e.mustOK(http.MethodPost, "/api/auth/register", "", map[string]string{
		"username": name, "email": name + "@test.local", "password": "passw0rd",
	}, http.StatusCreated)
	user := out["user"].(map[string]any)
	return out["token"].(string), int64(user["id"].(float64))
}

func TestHealthIsPublic(t *testing.T) {
	env := newTestEnv(t)
	out := env.mustOK(http.MethodGet, "/api/health", "", nil, http.StatusOK)
	if out["service"] != "bunkr-web" {
		t.Errorf("service = %v", out["service"])
	}
	// aria2 is down in this environment, so the service reports "degraded".
	if out["status"] != "degraded" && out["status"] != "ok" {
		t.Errorf("status = %v, want ok or degraded", out["status"])
	}
}

func TestPlansArePublic(t *testing.T) {
	env := newTestEnv(t)
	out := env.mustOK(http.MethodGet, "/api/membership/plans", "", nil, http.StatusOK)
	plans := out["plans"].([]any)
	if len(plans) != 3 {
		t.Fatalf("got %d plans, want 3", len(plans))
	}
}

func TestRegisterLoginLogoutFlow(t *testing.T) {
	env := newTestEnv(t)
	token, userID := env.register("alice")
	if userID <= 0 {
		t.Fatal("no user id issued")
	}

	me := env.mustOK(http.MethodGet, "/api/auth/me", token, nil, http.StatusOK)
	if me["user"].(map[string]any)["username"] != "alice" {
		t.Errorf("me returned %v", me["user"])
	}
	quota := me["quota"].(map[string]any)
	if int(quota["links_limit"].(float64)) != 5 || int(quota["files_limit"].(float64)) != 50 {
		t.Errorf("free quota = %v", quota)
	}

	// Login by email and by username both work.
	for _, account := range []string{"alice@test.local", "alice"} {
		out := env.mustOK(http.MethodPost, "/api/auth/login", "", map[string]string{
			"account": account, "password": "passw0rd",
		}, http.StatusOK)
		if out["token"] == "" {
			t.Errorf("login with %q returned no token", account)
		}
	}

	// A wrong password is rejected.
	rec, body := env.do(http.MethodPost, "/api/auth/login", "", map[string]string{
		"account": "alice", "password": "nope-nope",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad login = %d, want 400", rec.Code)
	}
	if errCode(t, body) != CodeInvalidCreds {
		t.Errorf("error code = %v", errCode(t, body))
	}

	// An unknown account gives the same answer, so accounts cannot be probed.
	rec, body = env.do(http.MethodPost, "/api/auth/login", "", map[string]string{
		"account": "ghost", "password": "nope-nope",
	})
	if rec.Code != http.StatusBadRequest || errCode(t, body) != CodeInvalidCreds {
		t.Errorf("unknown account leaked a different answer: %d %v", rec.Code, errCode(t, body))
	}

	env.mustOK(http.MethodPost, "/api/auth/logout", token, nil, http.StatusOK)
}

func TestRegisterRejectsBadInput(t *testing.T) {
	env := newTestEnv(t)
	cases := []struct {
		body map[string]string
		code string
	}{
		{map[string]string{"username": "ab", "email": "a@b.c", "password": "passw0rd"}, CodeWeakUsername},
		{map[string]string{"username": "good", "email": "bad", "password": "passw0rd"}, CodeInvalidEmail},
		{map[string]string{"username": "good", "email": "a@b.c", "password": "12"}, CodeWeakPassword},
	}
	for i, c := range cases {
		rec, body := env.do(http.MethodPost, "/api/auth/register", "", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("case %d = %d, want 400", i, rec.Code)
		}
		if got := errCode(t, body); got != c.code {
			t.Errorf("case %d code = %q, want %q", i, got, c.code)
		}
	}

	// Duplicates.
	env.register("bob")
	rec, body := env.do(http.MethodPost, "/api/auth/register", "", map[string]string{
		"username": "bob", "email": "other@test.local", "password": "passw0rd",
	})
	if errCode(t, body) != CodeUsernameTaken {
		t.Errorf("duplicate username code = %v", errCode(t, body))
	}
	rec, body = env.do(http.MethodPost, "/api/auth/register", "", map[string]string{
		"username": "other", "email": "BOB@test.local", "password": "passw0rd",
	})
	if rec.Code != http.StatusBadRequest || errCode(t, body) != CodeEmailTaken {
		t.Errorf("duplicate email = %d %v", rec.Code, errCode(t, body))
	}
}

func TestAuthGuard(t *testing.T) {
	env := newTestEnv(t)
	protected := []struct{ method, path string }{
		{http.MethodGet, "/api/tasks"},
		{http.MethodPost, "/api/tasks"},
		{http.MethodGet, "/api/stats"},
		{http.MethodGet, "/api/settings"},
		{http.MethodGet, "/api/auth/me"},
		{http.MethodGet, "/api/events"},
		{http.MethodPost, "/api/membership/orders"},
		{http.MethodGet, "/api/membership/orders"},
		{http.MethodGet, "/api/membership/status"},
	}
	for _, r := range protected {
		rec, body := env.do(r.method, r.path, "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token = %d, want 401", r.method, r.path, rec.Code)
		}
		if errCode(t, body) != CodeUnauthorized {
			t.Errorf("%s %s code = %v", r.method, r.path, errCode(t, body))
		}
	}

	rec, body := env.do(http.MethodGet, "/api/tasks", "not-a-real-token", nil)
	if rec.Code != http.StatusUnauthorized || errCode(t, body) != CodeUnauthorized {
		t.Errorf("a malformed token returned %d %v", rec.Code, errCode(t, body))
	}
}

func TestCreateTaskValidatesURLs(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("carl")

	for _, bad := range []string{
		"https://example.com/not-bunkr",
		"ftp://bunkr.si/a/ABC",
		"   ",
	} {
		rec, body := env.do(http.MethodPost, "/api/tasks", token, map[string]any{"url": bad})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("url %q = %d, want 400", bad, rec.Code)
		}
		if code := errCode(t, body); code != CodeInvalidURL && code != CodeBadRequest {
			t.Errorf("url %q code = %q", bad, code)
		}
	}
}

func TestLinkQuotaEnforcement(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("dana")

	// A schemeless link is accepted and normalised.
	out := env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
		"url": "bunkr.si/a/ONE", "auto_start": false,
	}, http.StatusCreated)
	if out["count"].(float64) != 1 {
		t.Errorf("count = %v", out["count"])
	}
	if int(out["quota"].(map[string]any)["links_used"].(float64)) != 1 {
		t.Errorf("links_used = %v", out["quota"])
	}

	// Fill the allowance.
	for i := 2; i <= 5; i++ {
		env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
			"url": fmt.Sprintf("bunkr.si/a/L%d", i), "auto_start": false,
		}, http.StatusCreated)
	}

	rec, body := env.do(http.MethodPost, "/api/tasks", token, map[string]any{
		"url": "bunkr.si/a/SIX", "auto_start": false,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("6th link = %d, want 403", rec.Code)
	}
	if errCode(t, body) != CodeQuotaExceeded {
		t.Errorf("code = %v", errCode(t, body))
	}
	details := body["error"].(map[string]any)["details"].(map[string]any)
	if int(details["limit"].(float64)) != 5 || int(details["used"].(float64)) != 5 {
		t.Errorf("details = %v", details)
	}

	// A batch that would cross the line is refused as a whole.
	rec, _ = env.do(http.MethodPost, "/api/tasks", token, map[string]any{
		"url": "bunkr.si/a/A\nbunkr.si/a/B", "auto_start": false,
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("over-quota batch = %d, want 403", rec.Code)
	}
	// And nothing was created by the rejected batch.
	list := env.mustOK(http.MethodGet, "/api/tasks", token, nil, http.StatusOK)
	if int(list["total"].(float64)) != 5 {
		t.Errorf("total tasks = %v, want 5", list["total"])
	}
}

func TestMembershipPurchaseFlow(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("erin")

	// A free user hits the wall.
	for i := 0; i < 5; i++ {
		env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
			"url": fmt.Sprintf("bunkr.si/a/F%d", i), "auto_start": false,
		}, http.StatusCreated)
	}
	rec, _ := env.do(http.MethodPost, "/api/tasks", token, map[string]any{
		"url": "bunkr.si/a/OVER", "auto_start": false,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("free user was not limited: %d", rec.Code)
	}

	// Buy a membership.
	order := env.mustOK(http.MethodPost, "/api/membership/orders", token, map[string]any{
		"plan": "member_monthly",
	}, http.StatusCreated)["order"].(map[string]any)
	orderID := int64(order["id"].(float64))
	if order["status"] != "pending" {
		t.Errorf("order status = %v", order["status"])
	}

	paid := env.mustOK(http.MethodPost, fmt.Sprintf("/api/membership/orders/%d/pay", orderID),
		token, map[string]any{"pay_method": "alipay"}, http.StatusOK)
	if paid["order"].(map[string]any)["status"] != "paid" {
		t.Errorf("order status after pay = %v", paid["order"])
	}
	if paid["user"].(map[string]any)["plan"] != "member" {
		t.Errorf("plan after pay = %v", paid["user"])
	}
	quota := paid["quota"].(map[string]any)
	if quota["is_member"] != true || quota["links_unlimited"] != true || quota["files_unlimited"] != true {
		t.Errorf("quota after purchase = %v", quota)
	}

	// The ceiling is gone.
	env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
		"url": "bunkr.si/a/AFTER", "auto_start": false,
	}, http.StatusCreated)

	// Paying twice is idempotent.
	env.mustOK(http.MethodPost, fmt.Sprintf("/api/membership/orders/%d/pay", orderID),
		token, nil, http.StatusOK)

	// An unknown plan is rejected.
	rec, _ = env.do(http.MethodPost, "/api/membership/orders", token, map[string]any{"plan": "gold"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown plan = %d, want 400", rec.Code)
	}
	// The free plan is not orderable.
	rec, _ = env.do(http.MethodPost, "/api/membership/orders", token, map[string]any{"plan": "free"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("ordering the free plan = %d, want 400", rec.Code)
	}
	// A nonexistent order is a 404.
	rec, _ = env.do(http.MethodPost, "/api/membership/orders/99999/pay", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("paying a missing order = %d, want 404", rec.Code)
	}
}

func TestCancelOrder(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("frank")
	order := env.mustOK(http.MethodPost, "/api/membership/orders", token,
		map[string]any{"plan": "member_yearly"}, http.StatusCreated)["order"].(map[string]any)
	orderID := int64(order["id"].(float64))

	out := env.mustOK(http.MethodPost, "/api/membership/cancel-order", token,
		map[string]any{"order_id": orderID}, http.StatusOK)
	if out["order"].(map[string]any)["status"] != "canceled" {
		t.Errorf("status = %v", out["order"])
	}
	// Cancelling twice conflicts.
	rec, body := env.do(http.MethodPost, "/api/membership/cancel-order", token,
		map[string]any{"order_id": orderID})
	if rec.Code != http.StatusConflict {
		t.Errorf("second cancel = %d, want 409", rec.Code)
	}
	if errCode(t, body) != CodeInvalidState {
		t.Errorf("code = %v", errCode(t, body))
	}
}

func TestRedeemCode(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("gina")

	out := env.mustOK(http.MethodPost, "/api/membership/redeem", token,
		map[string]any{"code": "BUNKR-MEMBER-2024"}, http.StatusOK)
	if out["user"].(map[string]any)["plan"] != "member" {
		t.Errorf("plan = %v", out["user"])
	}
	rec, body := env.do(http.MethodPost, "/api/membership/redeem", token,
		map[string]any{"code": "BUNKR-MEMBER-2024"})
	if rec.Code != http.StatusBadRequest || errCode(t, body) != CodeInvalidCode {
		t.Errorf("reused code = %d %v", rec.Code, errCode(t, body))
	}
	rec, _ = env.do(http.MethodPost, "/api/membership/redeem", token, map[string]any{"code": "NOPE"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown code = %d, want 400", rec.Code)
	}
	rec, _ = env.do(http.MethodPost, "/api/membership/redeem", token, map[string]any{"code": ""})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty code = %d, want 400", rec.Code)
	}
}

func TestChangePassword(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("hana")

	rec, _ := env.do(http.MethodPost, "/api/auth/change-password", token, map[string]string{
		"old_password": "wrong", "new_password": "newpassw0rd",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("wrong old password = %d, want 400", rec.Code)
	}
	rec, _ = env.do(http.MethodPost, "/api/auth/change-password", token, map[string]string{
		"old_password": "passw0rd", "new_password": "short",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("weak new password = %d, want 400", rec.Code)
	}
	env.mustOK(http.MethodPost, "/api/auth/change-password", token, map[string]string{
		"old_password": "passw0rd", "new_password": "newpassw0rd",
	}, http.StatusOK)

	// The old password no longer works and the new one does.
	env.mustOK(http.MethodPost, "/api/auth/login", "", map[string]string{
		"account": "hana", "password": "newpassw0rd",
	}, http.StatusOK)
	rec, _ = env.do(http.MethodPost, "/api/auth/login", "", map[string]string{
		"account": "hana", "password": "passw0rd",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("old password still works: %d", rec.Code)
	}
}

func TestTaskOwnershipIsolation(t *testing.T) {
	env := newTestEnv(t)
	ownerToken, _ := env.register("ivy")
	otherToken, _ := env.register("jack")

	out := env.mustOK(http.MethodPost, "/api/tasks", ownerToken, map[string]any{
		"url": "bunkr.si/a/PRIVATE", "auto_start": false,
	}, http.StatusCreated)
	taskID := int64(out["task_ids"].([]any)[0].(float64))

	// The other user cannot see, mutate or delete it.
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, fmt.Sprintf("/api/tasks/%d", taskID)},
		{http.MethodDelete, fmt.Sprintf("/api/tasks/%d", taskID)},
		{http.MethodPost, fmt.Sprintf("/api/tasks/%d/pause", taskID)},
		{http.MethodPost, fmt.Sprintf("/api/tasks/%d/cancel", taskID)},
		{http.MethodGet, fmt.Sprintf("/api/tasks/%d/files", taskID)},
		{http.MethodGet, fmt.Sprintf("/api/tasks/%d/events", taskID)},
	} {
		rec, _ := env.do(r.method, r.path, otherToken, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as a non-owner = %d, want 404", r.method, r.path, rec.Code)
		}
	}

	// The owner's list only contains their own task.
	list := env.mustOK(http.MethodGet, "/api/tasks", otherToken, nil, http.StatusOK)
	if int(list["total"].(float64)) != 0 {
		t.Errorf("the other user sees %v tasks", list["total"])
	}
}

func TestTaskIDValidation(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("kim")
	for _, path := range []string{
		"/api/tasks/abc", "/api/tasks/0", "/api/tasks/-5", "/api/tasks/999999",
	} {
		rec, _ := env.do(http.MethodGet, path, token, nil)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 400 or 404", path, rec.Code)
		}
	}
}

func TestTaskListFiltering(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("leo")
	for i := 0; i < 3; i++ {
		env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
			"url": fmt.Sprintf("bunkr.si/a/LIST%d", i), "auto_start": false,
		}, http.StatusCreated)
	}
	out := env.mustOK(http.MethodGet, "/api/tasks?limit=2", token, nil, http.StatusOK)
	if len(out["tasks"].([]any)) != 2 || int(out["total"].(float64)) != 3 {
		t.Errorf("limit=2 returned %d of %v", len(out["tasks"].([]any)), out["total"])
	}
	filtered := env.mustOK(http.MethodGet, "/api/tasks?q=LIST1", token, nil, http.StatusOK)
	if int(filtered["total"].(float64)) != 1 {
		t.Errorf("search returned %v", filtered["total"])
	}
	byStatus := env.mustOK(http.MethodGet, "/api/tasks?status=completed", token, nil, http.StatusOK)
	if int(byStatus["total"].(float64)) != 0 {
		t.Errorf("status filter returned %v", byStatus["total"])
	}
	// A nonsense limit falls back to the default rather than erroring.
	env.mustOK(http.MethodGet, "/api/tasks?limit=abc&offset=xyz", token, nil, http.StatusOK)
}

func TestStatsAndSettings(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("mia")
	env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
		"url": "bunkr.si/a/STAT", "auto_start": false,
	}, http.StatusCreated)

	stats := env.mustOK(http.MethodGet, "/api/stats", token, nil, http.StatusOK)
	for _, key := range []string{"total_tasks", "running", "pending", "completed",
		"failed", "total_files", "downloaded_bytes", "speed", "aria2", "quota"} {
		if _, ok := stats[key]; !ok {
			t.Errorf("stats is missing %q", key)
		}
	}
	if int(stats["total_tasks"].(float64)) != 1 {
		t.Errorf("total_tasks = %v", stats["total_tasks"])
	}

	settings := env.mustOK(http.MethodGet, "/api/settings", token, nil, http.StatusOK)
	if settings["version"] != "test" {
		t.Errorf("version = %v", settings["version"])
	}
	if _, ok := settings["features"].(map[string]any)["aria2"]; !ok {
		t.Error("settings is missing the aria2 feature flag")
	}
}

func TestDeleteTask(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("nate")
	out := env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
		"url": "bunkr.si/a/DEL", "auto_start": false,
	}, http.StatusCreated)
	taskID := int64(out["task_ids"].([]any)[0].(float64))

	env.mustOK(http.MethodDelete, fmt.Sprintf("/api/tasks/%d", taskID), token, nil, http.StatusOK)
	rec, _ := env.do(http.MethodGet, fmt.Sprintf("/api/tasks/%d", taskID), token, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("after delete = %d, want 404", rec.Code)
	}
}

func TestOptionsAreSanitised(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("olive")
	out := env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
		"url": "bunkr.si/a/OPTS", "auto_start": false,
		"options": map[string]any{
			"max_retries": 9999, "connections": 9999,
			"rate_limit_kbps": -50, "ignore": []string{"", "  ", "sample"},
			"include": []string{"mp4"},
		},
	}, http.StatusCreated)
	taskID := int64(out["task_ids"].([]any)[0].(float64))

	detail := env.mustOK(http.MethodGet, fmt.Sprintf("/api/tasks/%d", taskID), token, nil, http.StatusOK)
	opts := detail["task"].(map[string]any)["options"].(map[string]any)
	if int(opts["max_retries"].(float64)) != 20 {
		t.Errorf("max_retries clamped to %v, want 20", opts["max_retries"])
	}
	if int(opts["connections"].(float64)) != 16 {
		t.Errorf("connections clamped to %v, want 16", opts["connections"])
	}
	if int(opts["rate_limit_kbps"].(float64)) != 0 {
		t.Errorf("negative rate limit = %v, want 0", opts["rate_limit_kbps"])
	}
	ignore := opts["ignore"].([]any)
	if len(ignore) != 1 || ignore[0] != "sample" {
		t.Errorf("ignore list = %v, want just [sample]", ignore)
	}
}

func TestBatchURLInput(t *testing.T) {
	env := newTestEnv(t)
	token, _ := env.register("pia")

	// A newline-separated string, an array, and duplicates all work.
	out := env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
		"url": "bunkr.si/a/A1\nbunkr.si/a/A2\n\nbunkr.si/a/A1\n", "auto_start": false,
	}, http.StatusCreated)
	if int(out["count"].(float64)) != 2 {
		t.Errorf("newline batch count = %v, want 2 (duplicates collapsed)", out["count"])
	}

	out = env.mustOK(http.MethodPost, "/api/tasks", token, map[string]any{
		"urls": []string{"bunkr.si/a/B1", "bunkr.si/a/B2"}, "auto_start": false,
	}, http.StatusCreated)
	if int(out["count"].(float64)) != 2 {
		t.Errorf("array batch count = %v, want 2", out["count"])
	}

	// An empty submission is a 400.
	rec, _ := env.do(http.MethodPost, "/api/tasks", token, map[string]any{"url": ""})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty batch = %d, want 400", rec.Code)
	}
	// More than 100 links at once is rejected.
	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("bunkr.si/a/M%d", i)
	}
	rec, _ = env.do(http.MethodPost, "/api/tasks", token, map[string]any{"urls": many})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("101 links = %d, want 400", rec.Code)
	}
}

func TestCORSAndPreflight(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(http.MethodOptions, "/api/tasks", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	env.eng.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("preflight response is missing the CORS origin header")
	}
}

func TestUnknownRouteReturnsJSONError(t *testing.T) {
	env := newTestEnv(t)
	rec, body := env.do(http.MethodGet, "/api/does-not-exist", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown API route = %d, want 404", rec.Code)
	}
	if errCode(t, body) != CodeNotFound {
		t.Errorf("code = %v", errCode(t, body))
	}
}
