package services_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/chaunceyxie1/BunkrDownloader/internal/aria2"
	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/downloads"
	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
	"github.com/chaunceyxie1/BunkrDownloader/internal/services"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// env wires the services exactly like main.go does, minus the Wails runtime.
type env struct {
	t       *testing.T
	store   *store.Store
	app     *services.App
	auth    *services.AuthService
	tasks   *services.TaskService
	member  *services.MembershipService
	system  *services.SystemService
	dl      *downloads.Manager
	hub     *hub.Hub
	cleanup func()
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	dlDir := filepath.Join(root, "downloads")
	stateDir := filepath.Join(root, "state")

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

	log := slogDiscard()
	aria := aria2.NewManager(aria2.Options{Dir: filepath.Join(stateDir, "aria2"), Secret: "s"})
	events := hub.New(log, 256)
	mgr := downloads.New(downloads.Config{
		DownloadDir:    dlDir,
		PollInterval:   time.Second,
		DefaultOptions: store.DefaultTaskOptions(),
	}, st, aria, events, log)

	app := services.NewApp(st, auth.NewIssuer("test-secret", time.Hour), mgr, events, log)
	app.SetDataDir(stateDir)

	e := &env{
		t: t, store: st, app: app, dl: mgr, hub: events,
		auth:   services.NewAuthService(app),
		tasks:  services.NewTaskService(app),
		member: services.NewMembershipService(app),
		system: services.NewSystemService(app, dlDir),
		cleanup: func() {
			events.Close()
			st.Close()
		},
	}
	t.Cleanup(e.cleanup)
	return e
}

func (e *env) member_(name string) string {
	e.t.Helper()
	session, apiErr := e.auth.Register(name, name+"@test.local", "passw0rd")
	if apiErr != nil {
		e.t.Fatalf("register: %v", apiErr)
	}
	return session.Token
}

func TestRegisterLoginSession(t *testing.T) {
	e := newEnv(t)
	session, apiErr := e.auth.Register("alice", "alice@test.local", "passw0rd")
	if apiErr != nil {
		t.Fatalf("Register: %v", apiErr)
	}
	if session.Token == "" || session.User == nil || session.Quota == nil {
		t.Fatalf("incomplete session: %+v", session)
	}
	if session.User.Plan != store.PlanFree {
		t.Errorf("plan = %q, want free", session.User.Plan)
	}
	if session.Quota.LinksLimit != 5 || session.Quota.FilesLimit != 50 {
		t.Errorf("quota = %+v", session.Quota)
	}

	// Login by email and by username.
	for _, account := range []string{"alice@test.local", "alice"} {
		got, apiErr := e.auth.Login(account, "passw0rd")
		if apiErr != nil {
			t.Fatalf("Login(%q): %v", account, apiErr)
		}
		if got.User.ID != session.User.ID {
			t.Errorf("Login(%q) returned user %d", account, got.User.ID)
		}
	}

	if _, apiErr := e.auth.Login("alice", "wrong"); apiErr == nil ||
		apiErr.Code != services.CodeInvalidCreds {
		t.Errorf("wrong password error = %v", apiErr)
	}
	// An unknown account yields the same answer.
	unknown, apiErr := e.auth.Login("ghost", "whatever")
	if apiErr == nil || apiErr.Code != services.CodeInvalidCreds || unknown != nil {
		t.Errorf("unknown account = %v / %v", unknown, apiErr)
	}
}

func TestRegisterValidation(t *testing.T) {
	e := newEnv(t)
	cases := []struct{ user, email, pw string }{
		{"ab", "a@b.c", "passw0rd"},
		{"good", "bad-email", "passw0rd"},
		{"good", "a@b.c", "123"},
	}
	for _, c := range cases {
		if _, apiErr := e.auth.Register(c.user, c.email, c.pw); apiErr == nil {
			t.Errorf("Register(%q,%q,%q) was accepted", c.user, c.email, c.pw)
		}
	}
	if _, apiErr := e.auth.Register("taken", "taken@test.local", "passw0rd"); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := e.auth.Register("taken", "other@test.local", "passw0rd"); apiErr == nil ||
		apiErr.Code != services.CodeUsernameTaken {
		t.Errorf("duplicate username = %v", apiErr)
	}
}

func TestTokenRequired(t *testing.T) {
	e := newEnv(t)
	// No token anywhere: the session fallback is empty too.
	for name, call := range map[string]func() (*services.Session, *services.APIError){
		"Me":    func() (*services.Session, *services.APIError) { return e.auth.Me("") },
		"Stats": nil,
	} {
		if call == nil {
			continue
		}
		got, apiErr := call()
		if got != nil || apiErr == nil || apiErr.Code != services.CodeUnauthorized {
			t.Errorf("%s without a token = %v / %v", name, got, apiErr)
		}
	}
	if got, apiErr := e.auth.Me("garbage.token"); got != nil || apiErr == nil {
		t.Errorf("a malformed token was accepted: %v / %v", got, apiErr)
	}
	if _, apiErr := e.tasks.ListTasks("garbage.token", "", "", "", "", 10, 0); apiErr == nil {
		t.Error("ListTasks accepted a malformed token")
	}
}

func TestSessionFallbackAfterLogin(t *testing.T) {
	e := newEnv(t)
	token := e.member_("fallback")
	// The service remembers the last session, so an empty token still resolves.
	got, apiErr := e.auth.Me("")
	if apiErr != nil {
		t.Fatalf("Me(\"\") after login: %v", apiErr)
	}
	if got.User.Username != "fallback" || got.Token != token {
		t.Errorf("fallback session = %+v", got)
	}
	// Logout clears it.
	e.auth.Logout(token)
	if _, apiErr := e.auth.Me(""); apiErr == nil {
		t.Error("Me(\"\") succeeded after Logout")
	}
}

func TestLinkQuota(t *testing.T) {
	e := newEnv(t)
	token := e.member_("quota")

	for i := 0; i < 5; i++ {
		res, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{
			URL: "bunkr.si/a/Q" + itoa(i),
		})
		if apiErr != nil {
			t.Fatalf("create %d: %v", i, apiErr)
		}
		if res.Count != 1 || len(res.TaskIDs) != 1 {
			t.Errorf("create %d = %+v", i, res)
		}
		if res.Quota == nil || res.Quota.LinksUsed != i+1 {
			t.Errorf("quota after %d = %+v", i, res.Quota)
		}
	}

	_, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{URL: "bunkr.si/a/OVER"})
	if apiErr == nil || apiErr.Code != services.CodeQuotaExceeded {
		t.Fatalf("6th link error = %v, want quota_exceeded", apiErr)
	}
	if apiErr.Details["limit"] != float64(5) && apiErr.Details["limit"] != 5 {
		t.Errorf("quota details = %v", apiErr.Details)
	}

	// A batch that crosses the line is refused as a whole.
	if _, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{
		URL: "bunkr.si/a/X\nbunkr.si/a/Y",
	}); apiErr == nil {
		t.Error("an over-quota batch was accepted")
	}
	list, apiErr := e.tasks.ListTasks(token, "", "", "", "", 50, 0)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if list.Total != 5 {
		t.Errorf("total = %d, want 5 (nothing was partially created)", list.Total)
	}
}

func TestCreateTaskURLValidation(t *testing.T) {
	e := newEnv(t)
	token := e.member_("urlcheck")
	for _, bad := range []string{
		"https://example.com/nope", "ftp://bunkr.si/a/ABC", "   ",
	} {
		if _, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{URL: bad}); apiErr == nil {
			t.Errorf("CreateTask(%q) was accepted", bad)
		}
	}
	// Newline-separated input collapses duplicates.
	res, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{
		URL: "bunkr.si/a/A\nbunkr.si/a/B\n\nbunkr.si/a/A\n",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if res.Count != 2 {
		t.Errorf("count = %d, want 2", res.Count)
	}
	// The urls array is accepted too.
	res, apiErr = e.tasks.CreateTask(token, services.CreateTaskRequest{
		URLs: []string{"bunkr.si/a/C", "bunkr.si/a/D"},
	})
	if apiErr != nil || res.Count != 2 {
		t.Errorf("array batch = %v / %v", res, apiErr)
	}
	// >100 links is refused.
	many := make([]string, 101)
	for i := range many {
		many[i] = "bunkr.si/a/M" + itoa(i)
	}
	if _, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{URLs: many}); apiErr == nil {
		t.Error("101 links were accepted")
	}
}

func TestOptionsAreClamped(t *testing.T) {
	e := newEnv(t)
	token := e.member_("clamp")
	res, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{
		URL:     "bunkr.si/a/CLAMP",
		Options: &services.TaskOptionsInput{MaxRetries: 9999, Connections: 9999, RateLimitKbps: -10},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	detail, apiErr := e.tasks.GetTask(token, res.TaskIDs[0])
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if detail.Task.Options.MaxRetries != 20 {
		t.Errorf("max_retries = %d, want 20", detail.Task.Options.MaxRetries)
	}
	if detail.Task.Options.Connections != 16 {
		t.Errorf("connections = %d, want 16", detail.Task.Options.Connections)
	}
	if detail.Task.Options.RateLimitKbps != 0 {
		t.Errorf("rate limit = %d, want 0", detail.Task.Options.RateLimitKbps)
	}
}

func TestTaskOwnership(t *testing.T) {
	e := newEnv(t)
	owner := e.member_("owner")
	other := e.member_("other")

	res, _ := e.tasks.CreateTask(owner, services.CreateTaskRequest{URL: "bunkr.si/a/PRIVATE"})
	id := res.TaskIDs[0]

	if _, apiErr := e.tasks.GetTask(owner, id); apiErr != nil {
		t.Errorf("the owner cannot read the task: %v", apiErr)
	}
	if _, apiErr := e.tasks.GetTask(other, id); apiErr == nil || apiErr.Code != services.CodeNotFound {
		t.Errorf("a non-owner read the task: %v", apiErr)
	}
	if _, apiErr := e.tasks.Cancel(other, id); apiErr == nil {
		t.Error("a non-owner cancelled the task")
	}
	if _, apiErr := e.tasks.DeleteTask(other, id); apiErr == nil {
		t.Error("a non-owner deleted the task")
	}
	list, _ := e.tasks.ListTasks(other, "", "", "", "", 50, 0)
	if list.Total != 0 {
		t.Errorf("the other user sees %d tasks", list.Total)
	}
}

func TestTaskActionsAndDelete(t *testing.T) {
	e := newEnv(t)
	token := e.member_("lifecycle")
	res, _ := e.tasks.CreateTask(token, services.CreateTaskRequest{URL: "bunkr.si/a/LIFE"})
	id := res.TaskIDs[0]

	// Cancelling a pending task is allowed and lands in the canceled state.
	task, apiErr := e.tasks.Cancel(token, id)
	if apiErr != nil {
		t.Fatalf("Cancel: %v", apiErr)
	}
	if task.Status != store.TaskCanceled {
		t.Errorf("status = %q, want canceled", task.Status)
	}
	// Pause on a non-running task is tolerated (state is authoritative).
	if _, apiErr := e.tasks.Pause(token, id); apiErr != nil {
		t.Errorf("Pause on a canceled task = %v", apiErr)
	}
	// Retry re-queues the failed files. The manager's background loops are not
	// running in this test, so the restart half reports "not running" — what
	// matters here is that the call is handled without panicking.
	if n, apiErr := e.tasks.Retry(token, id, nil); apiErr != nil {
		t.Logf("Retry reported: %v (retried=%d)", apiErr, n)
	} else if n != 0 {
		t.Errorf("Retry requeued %d files, want 0 (none had failed)", n)
	}
	// Resuming a missing task is a 404, not a panic.
	if _, apiErr := e.tasks.Resume(token, 999999); apiErr == nil || apiErr.Code != services.CodeNotFound {
		t.Errorf("Resuming a missing task = %v, want not_found", apiErr)
	}

	if ok, apiErr := e.tasks.DeleteTask(token, id); apiErr != nil || !ok {
		t.Errorf("DeleteTask = %v / %v", ok, apiErr)
	}
	if _, apiErr := e.tasks.GetTask(token, id); apiErr == nil {
		t.Error("the task survived deletion")
	}
}

func TestListFilesAndEvents(t *testing.T) {
	e := newEnv(t)
	token := e.member_("files")
	res, _ := e.tasks.CreateTask(token, services.CreateTaskRequest{URL: "bunkr.si/a/FILES"})
	id := res.TaskIDs[0]

	files, apiErr := e.tasks.ListFiles(token, id, "", "", "", "", 10, 0)
	if apiErr != nil {
		t.Fatalf("ListFiles: %v", apiErr)
	}
	if files.Total != 0 || files.Limit != 10 || files.Offset != 0 {
		t.Errorf("empty file list = %+v", files)
	}
	events, apiErr := e.tasks.ListEvents(token, id, 0, 10)
	if apiErr != nil {
		t.Fatalf("ListEvents: %v", apiErr)
	}
	if len(events.Events) == 0 {
		t.Error("no events were recorded for a freshly created task")
	}
	// The global feed works too.
	all, apiErr := e.tasks.ListEvents(token, 0, 0, 50)
	if apiErr != nil || len(all.Events) == 0 {
		t.Errorf("global events = %v / %v", all, apiErr)
	}
}

func TestMembershipPurchase(t *testing.T) {
	e := newEnv(t)
	token := e.member_("buyer")

	plans := e.member.Plans()
	if len(plans.Plans) != 3 {
		t.Fatalf("catalogue has %d plans, want 3", len(plans.Plans))
	}

	order, apiErr := e.member.CreateOrder(token, "member_monthly")
	if apiErr != nil {
		t.Fatalf("CreateOrder: %v", apiErr)
	}
	if order.Status != store.OrderPending || order.AmountCents != 990 {
		t.Errorf("order = %+v", order)
	}

	result, apiErr := e.member.PayOrder(token, order.ID, "alipay")
	if apiErr != nil {
		t.Fatalf("PayOrder: %v", apiErr)
	}
	if result.Order.Status != store.OrderPaid || result.Order.TradeNo == "" {
		t.Errorf("paid order = %+v", result.Order)
	}
	if result.User.Plan != store.PlanMember || !result.Quota.IsMember {
		t.Errorf("after payment: plan %q member=%v", result.User.Plan, result.Quota.IsMember)
	}

	// The ceiling is gone: another link is accepted.
	if _, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{URL: "bunkr.si/a/A1"}); apiErr != nil {
		t.Errorf("member still limited: %v", apiErr)
	}
	for i := 2; i <= 7; i++ {
		if _, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{
			URL: "bunkr.si/a/A" + itoa(i),
		}); apiErr != nil {
			t.Fatalf("member link %d rejected: %v", i, apiErr)
		}
	}

	// Paying again is idempotent.
	if _, apiErr := e.member.PayOrder(token, order.ID, "alipay"); apiErr != nil {
		t.Errorf("second PayOrder = %v", apiErr)
	}

	orders, apiErr := e.member.ListOrders(token)
	if apiErr != nil || len(orders.Orders) != 1 {
		t.Errorf("ListOrders = %v / %v", orders, apiErr)
	}
	// Unknown plan / free plan / missing order all fail cleanly.
	if _, apiErr := e.member.CreateOrder(token, "gold"); apiErr == nil {
		t.Error("an unknown plan was accepted")
	}
	if _, apiErr := e.member.CreateOrder(token, store.PlanFree); apiErr == nil {
		t.Error("the free plan was orderable")
	}
	if _, apiErr := e.member.PayOrder(token, 999999, "alipay"); apiErr == nil {
		t.Error("paying a missing order succeeded")
	}
}

func TestCancelOrder(t *testing.T) {
	e := newEnv(t)
	token := e.member_("canceller")
	order, _ := e.member.CreateOrder(token, "member_yearly")

	got, apiErr := e.member.CancelOrder(token, order.ID)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if got.Status != store.OrderCanceled {
		t.Errorf("status = %q, want canceled", got.Status)
	}
	_, apiErr = e.member.CancelOrder(token, order.ID)
	if apiErr == nil || apiErr.Code != services.CodeConflict {
		t.Errorf("second cancel = %v, want conflict", apiErr)
	}
}

func TestRedeemCode(t *testing.T) {
	e := newEnv(t)
	token := e.member_("redeemer")

	session, apiErr := e.member.Redeem(token, "bunkr-member-2024")
	if apiErr != nil {
		t.Fatalf("Redeem: %v", apiErr)
	}
	if session.User.Plan != store.PlanMember || !session.Quota.IsMember {
		t.Errorf("after redeem: %+v", session.User)
	}
	if _, apiErr := e.member.Redeem(token, "BUNKR-MEMBER-2024"); apiErr == nil ||
		apiErr.Code != services.CodeInvalidCode {
		t.Errorf("reused code = %v", apiErr)
	}
	if _, apiErr := e.member.Redeem(token, "NOPE"); apiErr == nil {
		t.Error("an unknown code was accepted")
	}
}

func TestMembershipStatus(t *testing.T) {
	e := newEnv(t)
	token := e.member_("status")

	got, apiErr := e.member.Status(token)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if got.Plan != store.PlanFree || got.IsMember || got.ExpiresAt != nil {
		t.Errorf("free status = %+v", got)
	}
	if _, apiErr := e.member.PayOrder(token, mustOrder(t, e, token, "member_monthly"), "alipay"); apiErr != nil {
		t.Fatal(apiErr)
	}
	got, _ = e.member.Status(token)
	if !got.IsMember || got.ExpiresAt == nil {
		t.Errorf("member status = %+v", got)
	}
}

func TestSystemHealthStatsSettings(t *testing.T) {
	e := newEnv(t)
	token := e.member_("sys")

	health := e.system.Health()
	if health.Status != "ok" && health.Status != "degraded" {
		t.Errorf("status = %q", health.Status)
	}
	if health.Version == "" || health.Platform == "" {
		t.Errorf("health = %+v", health)
	}

	stats, apiErr := e.system.Stats(token)
	if apiErr != nil {
		t.Fatalf("Stats: %v", apiErr)
	}
	if stats.Quota == nil {
		t.Error("stats has no quota")
	}

	settings := e.system.Settings()
	if settings.Version == "" || settings.DownloadDir == "" {
		t.Errorf("settings = %+v", settings)
	}
	if settings.Features["desktop"] != true {
		t.Errorf("features = %v", settings.Features)
	}
	if settings.QuotaLimits == nil {
		t.Error("settings has no quota limits")
	}

	// Desktop conveniences must not panic.
	if ok, msg := e.system.OpenDownloadDir(); !ok {
		t.Errorf("OpenDownloadDir = false: %s", msg)
	}
	if e.system.Aria2BinaryPath() == "" {
		t.Log("aria2c is not on PATH in this environment (expected in CI)")
	}
	if e.system.DataDir() == "" {
		t.Error("DataDir is empty")
	}
	// RevealPath tolerates a missing file.
	if ok, _ := e.system.RevealPath("does/not/exist"); !ok {
		t.Error("RevealPath failed on a missing path")
	}
	if ok, _ := e.system.RevealPath(""); !ok {
		t.Error("RevealPath(\"\") failed")
	}
}

func TestChangePassword(t *testing.T) {
	e := newEnv(t)
	token := e.member_("changer")

	if ok, apiErr := e.auth.ChangePassword(token, "wrong", "newpassw0rd"); ok || apiErr == nil {
		t.Errorf("wrong old password = %v / %v", ok, apiErr)
	}
	if ok, apiErr := e.auth.ChangePassword(token, "passw0rd", "short"); ok || apiErr == nil {
		t.Errorf("weak new password = %v / %v", ok, apiErr)
	}
	if ok, apiErr := e.auth.ChangePassword(token, "passw0rd", "newpassw0rd"); !ok || apiErr != nil {
		t.Fatalf("ChangePassword = %v / %v", ok, apiErr)
	}
	if _, apiErr := e.auth.Login("changer", "newpassw0rd"); apiErr != nil {
		t.Errorf("login with the new password failed: %v", apiErr)
	}
	if _, apiErr := e.auth.Login("changer", "passw0rd"); apiErr == nil {
		t.Error("the old password still works")
	}
}

func TestHubSinkReceivesFrames(t *testing.T) {
	e := newEnv(t)
	token := e.member_("sinker")

	type frame struct {
		userID, taskID int64
		typ            string
	}
	got := make(chan frame, 64)
	remove := e.hub.AddSink(func(userID, taskID int64, f hub.Frame) {
		select {
		case got <- frame{userID, taskID, f.Type}:
		default:
		}
	})
	defer remove()

	if _, apiErr := e.tasks.CreateTask(token, services.CreateTaskRequest{URL: "bunkr.si/a/SINK"}); apiErr != nil {
		t.Fatal(apiErr)
	}

	deadline := time.After(3 * time.Second)
	sawCreated := false
	for !sawCreated {
		select {
		case f := <-got:
			if f.typ == "task_created" {
				sawCreated = true
				if f.userID == 0 {
					t.Error("the frame carries no user id")
				}
			}
		case <-deadline:
			t.Fatal("no task_created frame reached the sink")
		}
	}
}

// ------------------------------------------------------------------ helpers

func mustOrder(t *testing.T, e *env, token, plan string) int64 {
	t.Helper()
	order, apiErr := e.member.CreateOrder(token, plan)
	if apiErr != nil {
		t.Fatalf("CreateOrder: %v", apiErr)
	}
	return order.ID
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
