package store

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Configure(
		QuotaLimits{Links: 5, Files: 50, Concurrent: 1},
		QuotaLimits{Links: -1, Files: -1, Concurrent: 5},
	)
	t.Cleanup(func() { st.Close() })
	return st
}

func mustUser(t *testing.T, st *Store, name string) *User {
	t.Helper()
	u, err := st.CreateUser(name, name+"@test.local", "passw0rd")
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", name, err)
	}
	return u
}

func TestOpenAppliesSchemaAndMigrations(t *testing.T) {
	// Re-opening an existing file must be idempotent.
	path := filepath.Join(t.TempDir(), "again.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.EnsureRedeemCodes(); err != nil {
		t.Fatal(err)
	}
	s1.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if _, err := s2.CreateUser("abc", "a@b.c", "passw0rd"); err != nil {
		t.Fatalf("create after reopen: %v", err)
	}
}

func TestCreateUserValidation(t *testing.T) {
	st := newTestStore(t)
	cases := []struct{ user, email, pw string }{
		{"ab", "a@b.c", "passw0rd"}, // username too short
		{"this-username-is-much-too-long-to-be-accepted", "a@b.c", "passw0rd"},
		{"valid", "not-an-email", "passw0rd"},
		{"valid", "a@b.c", "123"}, // password too short
		{"has space", "a@b.c", "passw0rd"},
	}
	for _, c := range cases {
		if _, err := st.CreateUser(c.user, c.email, c.pw); err == nil {
			t.Errorf("CreateUser(%q,%q,%q) accepted invalid input", c.user, c.email, c.pw)
		}
	}
	if _, err := st.CreateUser("good", "Good@Test.Local", "passw0rd"); err != nil {
		t.Fatalf("valid create failed: %v", err)
	}
}

func TestCreateUserUniqueness(t *testing.T) {
	st := newTestStore(t)
	mustUser(t, st, "alice")

	if _, err := st.CreateUser("alice", "other@test.local", "passw0rd"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("duplicate username error = %v, want ErrUsernameTaken", err)
	}
	if _, err := st.CreateUser("bob", "ALICE@test.local", "passw0rd"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("duplicate email error = %v, want ErrEmailTaken", err)
	}
	// The username check is case-insensitive.
	if _, err := st.CreateUser("ALICE", "x@test.local", "passw0rd"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("case-insensitive duplicate username error = %v", err)
	}
}

func TestPasswordIsHashed(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "carol")
	if u.PasswordHash == "passw0rd" {
		t.Fatal("the password was stored in plaintext")
	}
	if err := VerifyPasswordForTest(u.PasswordHash, "passw0rd"); err != nil {
		t.Errorf("stored hash does not verify: %v", err)
	}
	if err := st.ChangePassword(u.ID, "newpassw0rd"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	reloaded, _ := st.GetUser(u.ID)
	if err := VerifyPasswordForTest(reloaded.PasswordHash, "newpassw0rd"); err != nil {
		t.Errorf("new hash does not verify: %v", err)
	}
	if err := st.ChangePassword(u.ID, "123"); err == nil {
		t.Error("ChangePassword accepted a too-short password")
	}
}

func TestGetUserByAccount(t *testing.T) {
	st := newTestStore(t)
	mustUser(t, st, "dave")
	for _, account := range []string{"dave", "DAVE", "dave@test.local", "Dave@Test.Local"} {
		if _, err := st.GetUserByAccount(account); err != nil {
			t.Errorf("GetUserByAccount(%q) = %v", account, err)
		}
	}
	if _, err := st.GetUserByAccount("nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown account error = %v, want ErrNotFound", err)
	}
}

func TestQuotaFreeTier(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "erin")

	q, err := st.Quota(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if q.IsMember || q.LinksLimit != 5 || q.FilesLimit != 50 || q.ConcurrentLimit != 1 {
		t.Fatalf("free quota = %+v", q)
	}
	if q.LinksUnlimited || q.FilesUnlimited {
		t.Error("a free user reported unlimited quota")
	}

	// Fill the allowance to exactly 5 links, then prove the 6th is refused.
	for i := 0; i < 5; i++ {
		if err := st.CanAddLinks(u.ID, 1); err != nil {
			t.Fatalf("link %d rejected: %v", i+1, err)
		}
		mustTask(t, st, u.ID)
	}
	err = st.CanAddLinks(u.ID, 1)
	var qe *ErrQuota
	if !errors.As(err, &qe) {
		t.Fatalf("6th link error = %v, want ErrQuota", err)
	}
	if qe.Resource != "links" || qe.Limit != 5 || qe.Used != 5 {
		t.Errorf("quota error = %+v", qe)
	}
	// A batch is all-or-nothing.
	if err := st.CanAddLinks(u.ID, 2); err == nil {
		t.Error("an over-quota batch was accepted")
	}
}

func TestQuotaFileCap(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "frank")

	if remaining := st.RemainingFiles(u.ID); remaining != 50 {
		t.Errorf("RemainingFiles = %d, want 50", remaining)
	}
	if err := st.CanAddFiles(u.ID, 50); err != nil {
		t.Errorf("50 files rejected: %v", err)
	}
	if err := st.CanAddFiles(u.ID, 51); err == nil {
		t.Error("51 files accepted under a 50-file cap")
	}

	task := mustTask(t, st, u.ID)
	mustFiles(t, st, task.ID, u.ID, 50)
	if remaining := st.RemainingFiles(u.ID); remaining != 0 {
		t.Errorf("RemainingFiles after 50 = %d, want 0", remaining)
	}
	if err := st.CanAddFiles(u.ID, 1); err == nil {
		t.Error("a file was accepted at the cap")
	}

	// Skipped files do not consume the allowance.
	st2 := newTestStore(t)
	v := mustUser(t, st2, "gina")
	t2 := mustTask(t, st2, v.ID)
	mustFiles(t, st2, t2.ID, v.ID, 10)
	if _, err := st2.DB().Exec(`UPDATE files SET status=? WHERE task_id=?`, FileSkipped, t2.ID); err != nil {
		t.Fatal(err)
	}
	if remaining := st2.RemainingFiles(v.ID); remaining != 50 {
		t.Errorf("RemainingFiles with skipped files = %d, want 50", remaining)
	}
}

func TestMembershipUnlocksQuota(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "hank")

	if err := st.ActivateMembership(u.ID, PlanMember, 30); err != nil {
		t.Fatal(err)
	}
	q, err := st.Quota(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !q.IsMember || !q.LinksUnlimited || !q.FilesUnlimited {
		t.Fatalf("member quota = %+v, want unlimited", q)
	}
	if q.ConcurrentLimit != 5 {
		t.Errorf("member concurrency = %d, want 5", q.ConcurrentLimit)
	}
	if q.LinksLimit != 0 || q.FilesLimit != 0 {
		t.Errorf("unlimited limits should report 0, got links=%d files=%d", q.LinksLimit, q.FilesLimit)
	}
	// Unlimited means no ceiling at all.
	for i := 0; i < 10; i++ {
		mustTask(t, st, u.ID)
	}
	if err := st.CanAddLinks(u.ID, 1); err != nil {
		t.Errorf("member hit a link ceiling: %v", err)
	}
	if err := st.CanAddFiles(u.ID, 1000); err != nil {
		t.Errorf("member hit a file ceiling: %v", err)
	}
}

func TestMembershipExpiry(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "iris")

	// An already-lapsed membership must not count as active.
	if _, err := st.DB().Exec(
		`UPDATE users SET plan=?, plan_expires_at=? WHERE id=?`,
		PlanMember, time.Now().Add(-time.Hour).UTC(), u.ID); err != nil {
		t.Fatal(err)
	}
	fresh, _ := st.GetUser(u.ID)
	if fresh.IsMember(time.Now()) {
		t.Error("an expired membership reported as active")
	}
	q, _ := st.Quota(u.ID)
	if q.IsMember {
		t.Error("quota reports an expired membership as active")
	}
}

func TestMembershipStacking(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "jack")
	if err := st.ActivateMembership(u.ID, PlanMember, 30); err != nil {
		t.Fatal(err)
	}
	first, _ := st.GetUser(u.ID)
	if err := st.ActivateMembership(u.ID, PlanMember, 30); err != nil {
		t.Fatal(err)
	}
	second, _ := st.GetUser(u.ID)
	if !second.PlanExpiresAt.After(*first.PlanExpiresAt) {
		t.Errorf("a second purchase did not extend the period: %v -> %v",
			first.PlanExpiresAt, second.PlanExpiresAt)
	}
}

func TestRedeemCode(t *testing.T) {
	st := newTestStore(t)
	if err := st.EnsureRedeemCodes(); err != nil {
		t.Fatal(err)
	}
	a := mustUser(t, st, "kate")
	b := mustUser(t, st, "liam")

	if err := st.RedeemCode(a.ID, "bunkr-member-2024"); err != nil { // case-insensitive
		t.Fatalf("RedeemCode: %v", err)
	}
	if u, _ := st.GetUser(a.ID); !u.IsMember(time.Now()) {
		t.Error("the code did not activate the membership")
	}
	if err := st.RedeemCode(b.ID, "BUNKR-MEMBER-2024"); !errors.Is(err, ErrCodeInvalid) {
		t.Errorf("reusing a code returned %v, want ErrCodeInvalid", err)
	}
	if err := st.RedeemCode(b.ID, "NOT-A-CODE"); !errors.Is(err, ErrCodeInvalid) {
		t.Errorf("unknown code returned %v, want ErrCodeInvalid", err)
	}
}

func TestOrders(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "mary")

	if _, err := st.CreateOrder(u.ID, "nope"); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("unknown plan error = %v", err)
	}
	if _, err := st.CreateOrder(u.ID, PlanFree); err == nil {
		t.Error("the free plan was orderable")
	}

	order, err := st.CreateOrder(u.ID, "member_monthly")
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != OrderPending || order.AmountCents != 990 {
		t.Errorf("order = %+v", order)
	}

	if _, _, err := st.PayOrder(order.ID, u.ID+999, "alipay"); !errors.Is(err, ErrNotFound) {
		t.Error("another user could pay this order")
	}

	paid, updated, err := st.PayOrder(order.ID, u.ID, "alipay")
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != OrderPaid || paid.PaidAt == nil || paid.TradeNo == "" {
		t.Errorf("paid order = %+v", paid)
	}
	if updated.Plan != PlanMember {
		t.Errorf("plan after payment = %q, want member", updated.Plan)
	}

	// Paying twice must be idempotent, not an error.
	if _, _, err := st.PayOrder(order.ID, u.ID, "alipay"); err != nil {
		t.Errorf("second PayOrder = %v, want idempotent success", err)
	}

	orders, err := st.ListOrders(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Errorf("ListOrders returned %d orders, want 1", len(orders))
	}
}

func TestCancelOrder(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "nate")
	order, _ := st.CreateOrder(u.ID, "member_yearly")

	canceled, err := st.CancelOrder(order.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if canceled.Status != OrderCanceled {
		t.Errorf("status = %q, want canceled", canceled.Status)
	}
	if _, _, err := st.PayOrder(order.ID, u.ID, "alipay"); err == nil {
		t.Error("a canceled order could still be paid")
	}
}

func TestPlansCatalogue(t *testing.T) {
	st := newTestStore(t)
	plans := st.Plans()
	if len(plans) != 3 {
		t.Fatalf("catalogue has %d plans, want 3", len(plans))
	}
	if plans[0].ID != PlanFree || plans[0].PriceCents != 0 {
		t.Errorf("plan 0 = %+v", plans[0])
	}
	if plans[0].Limits.Links != 5 || plans[0].Limits.Files != 50 {
		t.Errorf("free limits = %+v", plans[0].Limits)
	}
	if plans[1].Limits.Links != -1 {
		t.Errorf("member links limit = %d, want -1 (unlimited)", plans[1].Limits.Links)
	}
	if plans[1].PeriodDays != 30 || plans[2].PeriodDays != 365 {
		t.Errorf("periods = %d / %d", plans[1].PeriodDays, plans[2].PeriodDays)
	}
}

func TestTaskLifecycleAndStats(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "olive")

	task, err := st.CreateTask(CreateTaskOptions{
		UserID: u.ID, URL: "https://bunkr.si/a/ABC", Kind: "album",
		Opts: TaskOptions{MaxRetries: 3, Connections: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskPending {
		t.Errorf("initial status = %q, want pending", task.Status)
	}

	files := mustFiles(t, st, task.ID, u.ID, 3)
	// Sizes: 1 KB done, 2 KB pending, 3 KB failed.
	if _, err := st.DB().Exec(
		`UPDATE files SET file_size=1000, downloaded_bytes=1000, status=? WHERE id=?`,
		FileCompleted, files[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE files SET file_size=2000 WHERE id=?`, files[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(
		`UPDATE files SET file_size=3000, status=? WHERE id=?`, FileFailed, files[2].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.RecomputeTaskStats(task.ID); err != nil {
		t.Fatal(err)
	}

	got, _ := st.GetTask(task.ID)
	if got.TotalFiles != 3 || got.CompletedFiles != 1 || got.FailedFiles != 1 ||
		got.PendingFiles != 1 || got.DownloadingFiles != 0 {
		t.Errorf("counters = total %d completed %d failed %d pending %d downloading %d; want 3/1/1/1/0",
			got.TotalFiles, got.CompletedFiles, got.FailedFiles, got.PendingFiles, got.DownloadingFiles)
	}
	if got.TotalBytes != 6000 || got.DownloadedBytes != 1000 {
		t.Errorf("bytes = %d/%d, want 1000/6000", got.DownloadedBytes, got.TotalBytes)
	}
	wantProgress := 1000.0 / 6000.0 * 100
	if diff := got.Progress - wantProgress; diff > 0.01 || diff < -0.01 {
		t.Errorf("progress = %.2f, want %.2f", got.Progress, wantProgress)
	}

	stats, err := st.Stats(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalTasks != 1 || stats.Pending != 1 || stats.TotalFiles != 3 {
		t.Errorf("global stats = %+v", stats)
	}
}

func TestListTasksFiltersAndPaging(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "peggy")
	for i := 0; i < 7; i++ {
		mustTask(t, st, u.ID)
	}
	if err := st.UpdateTask(1, TaskUpdate{Status: strPtr(TaskCompleted)}); err != nil {
		t.Fatal(err)
	}

	all, total, err := st.ListTasks(ListTasksParams{UserID: u.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 7 || len(all) != 7 {
		t.Errorf("ListTasks = %d rows, total %d; want 7/7", len(all), total)
	}

	page, total, err := st.ListTasks(ListTasksParams{UserID: u.ID, Limit: 3, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 3 || total != 7 {
		t.Errorf("first page = %d rows, total %d; want 3/7", len(page), total)
	}
	page2, _, _ := st.ListTasks(ListTasksParams{UserID: u.ID, Limit: 3, Offset: 3})
	if len(page2) != 3 {
		t.Errorf("second page = %d rows, want 3", len(page2))
	}

	completed, total, _ := st.ListTasks(ListTasksParams{UserID: u.ID, Status: TaskCompleted, Limit: 50})
	if total != 1 || len(completed) != 1 {
		t.Errorf("status filter returned %d/%d, want 1/1", len(completed), total)
	}
	if _, _, err := st.ListTasks(ListTasksParams{UserID: u.ID, Limit: 5000}); err != nil {
		t.Errorf("an oversized limit errored: %v", err)
	}
}

func TestTaskOwnership(t *testing.T) {
	st := newTestStore(t)
	owner := mustUser(t, st, "quinn")
	other := mustUser(t, st, "rita")
	task := mustTask(t, st, owner.ID)

	if _, err := st.TaskForUser(task.ID, owner.ID); err != nil {
		t.Errorf("the owner could not read the task: %v", err)
	}
	if _, err := st.TaskForUser(task.ID, other.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a non-owner read the task: %v", err)
	}
}

func TestDeleteTaskCascades(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "sam")
	task := mustTask(t, st, u.ID)
	mustFiles(t, st, task.ID, u.ID, 3)
	if _, err := st.LogEvent(u.ID, task.ID, 0, LevelInfo, "e", "d"); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTask(task.ID); !errors.Is(err, ErrNotFound) {
		t.Error("the task survived deletion")
	}
	files, err := st.AllFilesForTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("%d files survived the task deletion", len(files))
	}
	events, _ := st.ListEvents(u.ID, task.ID, 0, 100)
	if len(events) != 0 {
		t.Errorf("%d events survived the task deletion", len(events))
	}
}

func TestRequeueInterrupted(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "tina")
	task := mustTask(t, st, u.ID)
	files := mustFiles(t, st, task.ID, u.ID, 2)
	if _, err := st.DB().Exec(
		`UPDATE files SET status=?, gid='abc' WHERE id=?`, FileDownloading, files[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateTask(task.ID, TaskUpdate{Status: strPtr(TaskRunning)}); err != nil {
		t.Fatal(err)
	}

	n, err := st.RequeueInterrupted()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("RequeueInterrupted moved %d tasks, want 1", n)
	}
	got, _ := st.GetTask(task.ID)
	if got.Status != TaskPaused {
		t.Errorf("task status = %q, want paused", got.Status)
	}
	f, _ := st.GetFile(files[0].ID)
	if f.Status != FilePending {
		t.Errorf("file status = %q, want pending after a crash", f.Status)
	}
}

func TestFilePaginationAndSorting(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "ursula")
	task := mustTask(t, st, u.ID)
	for i := 0; i < 5; i++ {
		mustFiles(t, st, task.ID, u.ID, 1)
	}
	page, total, err := st.ListFiles(ListFilesParams{TaskID: task.ID, Limit: 2, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || total != 5 {
		t.Errorf("page = %d rows, total %d; want 2/5", len(page), total)
	}
	desc, _, _ := st.ListFiles(ListFilesParams{TaskID: task.ID, Limit: 5, Sort: "id", Dir: "desc"})
	if len(desc) != 5 || desc[0].ID <= desc[len(desc)-1].ID {
		t.Error("descending sort did not order by id")
	}
}

func TestResetFileForRetryIncrementsCounter(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "vic")
	task := mustTask(t, st, u.ID)
	files := mustFiles(t, st, task.ID, u.ID, 1)
	f := files[0]

	if err := st.UpdateFile(f.ID, FileUpdate{
		Status: strPtr(FileFailed), ErrorMessage: strPtr("boom"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetFileForRetry(f.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetFile(f.ID)
	if got.Status != FilePending || got.RetryCount != 1 || got.ErrorMessage != "" {
		t.Errorf("after reset = %+v", got)
	}

	n, err := st.ResetTaskFailures(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("ResetTaskFailures reset %d files, want 0 (none are failed)", n)
	}
}

func TestEvents(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "walt")
	task := mustTask(t, st, u.ID)

	for i := 0; i < 5; i++ {
		if _, err := st.LogEvent(u.ID, task.ID, 0, LevelInfo, "e", "d"); err != nil {
			t.Fatal(err)
		}
	}
	events, err := st.ListEvents(u.ID, task.ID, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("limit 3 returned %d events", len(events))
	}
	// Newest first.
	if events[0].ID < events[2].ID {
		t.Error("events are not newest-first")
	}
	// Paging backwards with before_id.
	older, _ := st.ListEvents(u.ID, task.ID, events[2].ID, 10)
	for _, e := range older {
		if e.ID >= events[2].ID {
			t.Errorf("before_id paging returned event %d", e.ID)
		}
	}

	if err := st.TrimEvents(2); err != nil {
		t.Fatal(err)
	}
	all, _ := st.ListEvents(u.ID, 0, 0, 100)
	if len(all) != 2 {
		t.Errorf("after trim there are %d events, want 2", len(all))
	}
}

func TestEventsAreUserScoped(t *testing.T) {
	st := newTestStore(t)
	a := mustUser(t, st, "xena")
	b := mustUser(t, st, "yuri")
	ta := mustTask(t, st, a.ID)
	tb := mustTask(t, st, b.ID)
	if _, err := st.LogEvent(a.ID, ta.ID, 0, LevelInfo, "a-event", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LogEvent(b.ID, tb.ID, 0, LevelInfo, "b-event", ""); err != nil {
		t.Fatal(err)
	}

	got, _ := st.ListEvents(a.ID, 0, 0, 100)
	for _, e := range got {
		if e.Event == "b-event" {
			t.Error("user A can read user B's events")
		}
	}
}

func TestConcurrentWrites(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "zoe")
	task := mustTask(t, st, u.ID)
	mustFiles(t, st, task.ID, u.ID, 20)

	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 25; j++ {
				if err := st.UpdateTask(task.ID, TaskUpdate{Speed: i64Ptr(int64(n))}); err != nil {
					t.Errorf("concurrent update: %v", err)
					return
				}
				if _, err := st.LogEvent(u.ID, task.ID, 0, LevelInfo, "spam", ""); err != nil {
					t.Errorf("concurrent event: %v", err)
					return
				}
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	events, _ := st.ListEvents(u.ID, 0, 0, 1000)
	if len(events) != 200 {
		t.Errorf("recorded %d events, want 200", len(events))
	}
}

// ------------------------------------------------------------------ helpers

func mustTask(t *testing.T, st *Store, userID int64) *Task {
	t.Helper()
	task, err := st.CreateTask(CreateTaskOptions{
		UserID: userID, URL: "https://bunkr.si/a/T", Kind: "album",
		Opts: DefaultTaskOptions(),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

func mustFiles(t *testing.T, st *Store, taskID, userID int64, n int) []*File {
	t.Helper()
	if n == 0 {
		return nil
	}
	// The unique key is (task_id, item_url), so every file needs a distinct URL.
	task, _ := st.GetTask(taskID)
	batch := make([]NewFile, 0, n)
	for i := 0; i < n; i++ {
		seq := fileSeq.Add(1)
		batch = append(batch, NewFile{
			TaskID: taskID, UserID: userID,
			ItemURL: task.URL + "#" + itoa64(seq),
		})
	}
	if _, err := st.RegisterFiles(batch); err != nil {
		t.Fatalf("RegisterFiles: %v", err)
	}
	files, err := st.AllFilesForTask(taskID)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// fileSeq makes generated item URLs unique across the whole test binary.
var fileSeq atomic.Int64

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func strPtr(s string) *string { return &s }
func i64Ptr(v int64) *int64   { return &v }
