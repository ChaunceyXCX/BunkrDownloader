package aria2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAria2 is an in-process stand-in for the aria2 JSON-RPC daemon.
type fakeAria2 struct {
	mu sync.Mutex

	version string
	status  map[string]FileStatus
	added   []string
	removed []string
	paused  []string
	unpause []string
	options map[string]any

	// failNext, when set, makes the next call return an RPCError.
	failNext *RPCError
	calls    []string
}

func newFakeAria2() *fakeAria2 {
	return &fakeAria2{
		version: "1.37.0",
		status:  map[string]FileStatus{},
		options: map[string]any{},
	}
}

func (f *fakeAria2) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		f.mu.Lock()
		f.calls = append(f.calls, req.Method)
		fail := f.failNext
		f.failNext = nil
		f.mu.Unlock()

		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if fail != nil {
			resp["error"] = fail
		} else {
			f.mu.Lock()
			result := f.dispatchLocked(req.Method, req.Params)
			f.mu.Unlock()
			resp["result"] = result
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// dispatch runs a method under the fake's own lock.
func (f *fakeAria2) dispatch(method string, params []json.RawMessage) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dispatchLocked(method, params)
}

// dispatchLocked must be called with f.mu held. system.multicall recurses into
// this, so it must not re-acquire the mutex.
func (f *fakeAria2) dispatchLocked(method string, params []json.RawMessage) any {
	switch method {
	case "aria2.getVersion":
		return map[string]any{"version": f.version, "enabledFeatures": []string{"HTTPS"}}
	case "aria2.addUri":
		uri := ""
		if len(params) > 1 {
			var uris []string
			_ = json.Unmarshal(params[1], &uris)
			if len(uris) > 0 {
				uri = uris[0]
			}
		}
		gid := "gid" + itoa(len(f.added)+1)
		f.added = append(f.added, uri)
		f.status[gid] = FileStatus{GID: gid, Status: "active", TotalLength: 100, URIs: []URIEntry{{URI: uri}}}
		return gid
	case "aria2.tellStatus":
		gid := firstString(params, 1)
		st, ok := f.status[gid]
		if !ok {
			return nil
		}
		return st
	case "system.multicall":
		// aria2 answers with a one-element array per call containing the bare
		// result; the client must unwrap it.
		var calls []struct {
			MethodName string            `json:"methodName"`
			Params     []json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(params[0], &calls)
		out := make([]any, 0, len(calls))
		for _, c := range calls {
			// aria2 wraps each result in a one-element array.
			out = append(out, []any{f.dispatchLocked(c.MethodName, c.Params)})
		}
		return out
	case "aria2.getGlobalStat":
		return GlobalStat{
			DownloadSpeed: 4096, NumActive: 1, NumWaiting: 2, NumStopped: 3,
			NumOfFiles: 6, DownloadedBytes: 999,
		}
	case "aria2.pause":
		f.paused = append(f.paused, firstString(params, 1))
		return "OK"
	case "aria2.unpause":
		f.unpause = append(f.unpause, firstString(params, 1))
		return "OK"
	case "aria2.forceRemove", "aria2.remove", "aria2.removeDownloadResult":
		f.removed = append(f.removed, firstString(params, 1))
		return "OK"
	case "aria2.changeGlobalOption":
		var o map[string]any
		_ = json.Unmarshal(params[1], &o)
		for k, v := range o {
			f.options[k] = v
		}
		return "OK"
	case "aria2.tellActive":
		out := []FileStatus{}
		for _, st := range f.status {
			if st.Status == "active" {
				out = append(out, st)
			}
		}
		return out
	}
	return nil
}

func firstString(params []json.RawMessage, idx int) string {
	// The secret occupies index 0, so the first real argument is index 1.
	if idx >= len(params) {
		return ""
	}
	var s string
	_ = json.Unmarshal(params[idx], &s)
	return s
}

func (f *fakeAria2) setFail(err *RPCError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext = err
}

func (f *fakeAria2) snapshot() fakeAria2 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeAria2{
		added:   append([]string(nil), f.added...),
		removed: append([]string(nil), f.removed...),
		paused:  append([]string(nil), f.paused...),
		unpause: append([]string(nil), f.unpause...),
		options: f.options,
	}
}

func startFake(t *testing.T) (*Client, *fakeAria2) {
	t.Helper()
	f := newFakeAria2()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	u := strings.TrimPrefix(srv.URL, "http://")
	host, port := splitHostPort(t, u)
	return NewClient(host, port, "secret"), f
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		t.Fatalf("bad address %q", addr)
	}
	port := 0
	for _, c := range parts[1] {
		port = port*10 + int(c-'0')
	}
	return parts[0], port
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

func TestGetVersion(t *testing.T) {
	c, _ := startFake(t)
	v, err := c.GetVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != "1.37.0" {
		t.Errorf("version = %q", v.Version)
	}
}

func TestAddURI(t *testing.T) {
	c, f := startFake(t)
	gid, err := c.AddURI(context.Background(),
		[]string{"https://cdn.example/file.mp4"}, AddURIOptions{Dir: "/tmp", Out: "file.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if gid == "" {
		t.Fatal("AddURI returned an empty GID")
	}
	snap := f.snapshot()
	if len(snap.added) != 1 || snap.added[0] != "https://cdn.example/file.mp4" {
		t.Errorf("added = %v", snap.added)
	}
}

func TestAddURIRejectsEmptyList(t *testing.T) {
	c, _ := startFake(t)
	if _, err := c.AddURI(context.Background(), nil, AddURIOptions{}); err == nil {
		t.Error("AddURI accepted an empty URI list")
	}
}

func TestTellStatusAndBatch(t *testing.T) {
	c, f := startFake(t)
	gid, err := c.AddURI(context.Background(), []string{"https://x/a"}, AddURIOptions{})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.TellStatus(context.Background(), gid)
	if err != nil {
		t.Fatal(err)
	}
	if st.GID != gid || st.Status != "active" {
		t.Errorf("status = %+v", st)
	}

	batch, err := c.TellStatusBatch(context.Background(), []string{gid, "missing-gid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 {
		t.Errorf("batch returned %d entries, want 1 (the unknown GID is skipped)", len(batch))
	}
	if _, ok := batch[gid]; !ok {
		t.Error("the known GID is missing from the batch result")
	}
	_ = f
}

func TestTellStatusBatchEmpty(t *testing.T) {
	c, _ := startFake(t)
	got, err := c.TellStatusBatch(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Errorf("empty batch = %v, %v", got, err)
	}
}

func TestGetGlobalStat(t *testing.T) {
	c, _ := startFake(t)
	st, err := c.GetGlobalStat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.DownloadSpeed != 4096 || st.NumActive != 1 || st.NumOfFiles != 6 {
		t.Errorf("global stat = %+v", st)
	}
}

func TestPauseUnpauseRemove(t *testing.T) {
	c, f := startFake(t)
	ctx := context.Background()
	gid, _ := c.AddURI(ctx, []string{"https://x/a"}, AddURIOptions{})

	if err := c.PauseAll(ctx, gid); err != nil {
		t.Fatal(err)
	}
	if err := c.UnpauseAll(ctx, gid); err != nil {
		t.Fatal(err)
	}
	if err := c.ForceRemove(ctx, gid); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot()
	if len(snap.paused) != 1 || snap.paused[0] != gid {
		t.Errorf("paused = %v", snap.paused)
	}
	if len(snap.unpause) != 1 || snap.unpause[0] != gid {
		t.Errorf("unpaused = %v", snap.unpause)
	}
	if len(snap.removed) != 1 || snap.removed[0] != gid {
		t.Errorf("removed = %v", snap.removed)
	}
}

func TestChangeGlobalOption(t *testing.T) {
	c, f := startFake(t)
	if err := c.ChangeGlobalOption(context.Background(), map[string]any{
		"max-concurrent-downloads": 16,
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.snapshot().options["max-concurrent-downloads"]; got != float64(16) {
		t.Errorf("option = %v, want 16", got)
	}
}

func TestRPCErrorIsSurfaced(t *testing.T) {
	c, f := startFake(t)
	f.setFail(&RPCError{Code: CodeNotFound, Message: "No such download"})

	_, err := c.TellStatus(context.Background(), "nope")
	if err == nil {
		t.Fatal("an RPC error was swallowed")
	}
	if !IsNotFoundError(err) {
		t.Errorf("IsNotFoundError = false for %v", err)
	}
	if !strings.Contains(err.Error(), "No such download") {
		t.Errorf("error text lost the daemon message: %v", err)
	}

	f.setFail(&RPCError{Code: CodeUnauthorized, Message: "Unauthorized"})
	_, err = c.GetVersion(context.Background())
	if !IsUnauthorized(err) {
		t.Errorf("IsUnauthorized = false for %v", err)
	}
	if IsNotFoundError(err) {
		t.Error("IsNotFoundError matched an auth error")
	}
}

func TestUnauthorizedEndpoint(t *testing.T) {
	// Point the client at a server that always returns an auth error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": "1",
			"error": map[string]any{"code": CodeUnauthorized, "message": "Unauthorized"},
		})
	}))
	defer srv.Close()
	host, port := splitHostPort(t, strings.TrimPrefix(srv.URL, "http://"))
	c := NewClient(host, port, "wrong")

	if _, err := c.GetVersion(context.Background()); !IsUnauthorized(err) {
		t.Errorf("error = %v, want an auth error", err)
	}
}

func TestUnreachableEndpoint(t *testing.T) {
	c := NewClient("127.0.0.1", 1, "x") // nothing listens on port 1
	c.SetTimeout(500 * time.Millisecond)
	if _, err := c.GetVersion(context.Background()); err == nil {
		t.Error("a dead endpoint returned no error")
	}
}

// ------------------------------------------------------------ numeric types

func TestInt64Decoding(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{`"1024"`, 1024},
		{`1024`, 1024},
		{`"0"`, 0},
		{`0`, 0},
		{`null`, 0},
		{`""`, 0},
		{`"1.0"`, 1},
		{`-42`, -42},
	}
	for _, c := range cases {
		var v Int64
		if err := json.Unmarshal([]byte(c.in), &v); err != nil {
			t.Errorf("Unmarshal(%s) = %v", c.in, err)
			continue
		}
		if int64(v) != c.want {
			t.Errorf("Unmarshal(%s) = %d, want %d", c.in, int64(v), c.want)
		}
	}
	var v Int64
	if err := json.Unmarshal([]byte(`"abc"`), &v); err == nil {
		t.Error("a non-numeric string was accepted")
	}
	out, err := json.Marshal(Int64(7))
	if err != nil || string(out) != "7" {
		t.Errorf("Marshal = %s, %v", out, err)
	}
}

func TestFileStatusDecodesAria2Shape(t *testing.T) {
	// This is a trimmed but structurally exact aria2 tellStatus payload:
	// every number is a string and the URI lists are objects.
	payload := `{
      "gid": "abc", "status": "active",
      "totalLength": "104857600", "completedLength": "52428800",
      "downloadSpeed": "131072", "connections": "4",
      "dir": "/downloads", "numPieces": "10", "createdAt": "1700000000",
      "files": [{
        "path": "/downloads/movie.mp4", "length": "104857600",
        "completedLength": "52428800", "selected": "true",
        "uris": [{"uri": "https://cdn/movie.mp4", "status": "used"}]
      }]
    }`
	var st FileStatus
	if err := json.Unmarshal([]byte(payload), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.GID != "abc" || st.Status != "active" {
		t.Errorf("identity = %+v", st)
	}
	if st.TotalLength != 104857600 || st.CompletedLength != 52428800 {
		t.Errorf("lengths = %d / %d", st.TotalLength, st.CompletedLength)
	}
	if st.DownloadSpeed != 131072 || st.Connections != 4 || st.NumPieces != 10 {
		t.Errorf("counters = %+v", st)
	}
	if len(st.Files) != 1 || st.Files[0].Length != 104857600 {
		t.Fatalf("files = %+v", st.Files)
	}
	if len(st.Files[0].URIs) != 1 || st.Files[0].URIs[0].URI != "https://cdn/movie.mp4" {
		t.Errorf("uris = %+v", st.Files[0].URIs)
	}
	if got := st.Progress(); got < 0.499 || got > 0.501 {
		t.Errorf("Progress = %v, want ~0.5", got)
	}
	if st.IsTerminal() {
		t.Error("an active download reported as terminal")
	}
}

func TestMulticallShapeTolerance(t *testing.T) {
	shapes := []string{
		`[{"result": {"gid": "g1", "status": "complete"}}]`,
		`[[{"gid": "g1", "status": "complete"}]]`,
		`[{"gid": "g1", "status": "complete"}]`,
	}
	for _, shape := range shapes {
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(shape), &raw); err != nil {
			t.Fatalf("%s: %v", shape, err)
		}
		val, rpcErr := decodeMulticallElement(raw[0])
		if rpcErr != nil {
			t.Errorf("%s produced an error: %v", shape, rpcErr)
			continue
		}
		var st FileStatus
		if err := json.Unmarshal(val, &st); err != nil {
			t.Errorf("%s: result does not decode: %v", shape, err)
			continue
		}
		if st.GID != "g1" {
			t.Errorf("%s: gid = %q, want g1", shape, st.GID)
		}
	}
}

func TestMulticallFaultShape(t *testing.T) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(`[{"faultCode": 1, "faultMessage": "gone"}]`), &raw); err != nil {
		t.Fatal(err)
	}
	_, rpcErr := decodeMulticallElement(raw[0])
	if rpcErr == nil {
		t.Fatal("a fault was reported as success")
	}
	if rpcErr.Code != 1 || rpcErr.Message != "gone" {
		t.Errorf("fault = %+v", rpcErr)
	}
}

func TestNeedsToken(t *testing.T) {
	if needsToken("system.multicall") {
		t.Error("system.* must not receive a token")
	}
	if !needsToken("aria2.addUri") {
		t.Error("aria2.* must receive a token")
	}
}

func TestSortGIDs(t *testing.T) {
	m := map[string]FileStatus{"c": {}, "a": {}, "b": {}}
	got := SortGIDs(m)
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SortGIDs = %v, want %v", got, want)
			break
		}
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	if _, err := safeJoin(dest, "../../etc/passwd"); err == nil {
		t.Error("a traversal path was accepted")
	}
	if _, err := safeJoin(dest, "/absolute/path"); err == nil {
		t.Error("an absolute path was accepted")
	}
	ok, err := safeJoin(dest, "sub/dir/file.txt")
	if err != nil {
		t.Fatalf("a safe path was rejected: %v", err)
	}
	if !strings.HasPrefix(ok, dest) {
		t.Errorf("safeJoin escaped the destination: %q", ok)
	}
}

func TestInt64Helper(t *testing.T) {
	if Int64(5).Int64() != 5 {
		t.Error("Int64().Int64() did not round-trip")
	}
}
