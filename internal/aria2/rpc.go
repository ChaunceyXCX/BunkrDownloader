// Package aria2 provides a JSON-RPC client for aria2c plus a supervisor that
// owns the lifecycle of the aria2c child process.
//
// The RPC surface mirrors the official aria2 documentation:
// https://aria2.github.io/manual/en/aria2c.html#rpc-interface
package aria2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client is a JSON-RPC 2.0 client for the aria2c XML-RPC-over-HTTP endpoint.
type Client struct {
	endpoint string
	secret   string
	http     *http.Client

	// tokenFn supplies the first positional argument for methods that require
	// the RPC secret (prefixed with "token:").
	tokenFn func() string

	mu   sync.Mutex
	next uint64
}

// NewClient builds a client for host:port guarded by secret.
func NewClient(host string, port int, secret string) *Client {
	return &Client{
		endpoint: fmt.Sprintf("http://%s:%d/jsonrpc", host, port),
		secret:   secret,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        32,
				MaxIdleConnsPerHost: 16,
				IdleConnTimeout:     60 * time.Second,
			},
		},
		tokenFn: func() string { return "token:" + secret },
	}
}

// SetSecret updates the RPC secret (used when aria2c is (re)started).
func (c *Client) SetSecret(secret string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.secret = secret
	c.tokenFn = func() string { return "token:" + secret }
}

// SetTimeout overrides the per-request HTTP timeout.
func (c *Client) SetTimeout(d time.Duration) { c.http.Timeout = d }

// Endpoint returns the JSON-RPC URL in use.
func (c *Client) Endpoint() string { return c.endpoint }

// RPCError is an error object returned by aria2c.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("aria2 rpc error %d: %s", e.Code, e.Message)
}

// Common aria2 error codes we branch on.
const (
	CodeNotFound      = 1
	CodeUnauthorized  = 3
	CodeCannotResume  = 32
	CodeNotEnoughDisk = 34
)

type rpcRequest struct {
	Version string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

// Call issues a single RPC request and decodes the result into out.
// Methods that need the secret must pass token as the first parameter;
// Call prepends it automatically.
func (c *Client) Call(ctx context.Context, method string, out any, params ...any) error {
	c.mu.Lock()
	c.next++
	id := fmt.Sprintf("%d", c.next)
	c.mu.Unlock()

	if needsToken(method) {
		c.mu.Lock()
		tok := c.tokenFn()
		c.mu.Unlock()
		params = append([]any{tok}, params...)
	}

	body, err := json.Marshal(rpcRequest{
		Version: "2.0", ID: id, Method: method, Params: params,
	})
	if err != nil {
		return fmt.Errorf("marshal %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("read %s: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: http %d: %s", method, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var rr rpcResponse
	if err := json.Unmarshal(raw, &rr); err != nil {
		return fmt.Errorf("decode %s: %w (body: %s)", method, err, truncate(string(raw), 200))
	}
	if rr.Error != nil {
		return rr.Error
	}
	if out == nil || len(rr.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(rr.Result, out); err != nil {
		return fmt.Errorf("decode result of %s: %w", method, err)
	}
	return nil
}

// SystemMulticall issues several method calls in one HTTP round trip.
//
// aria2's response shape is inconsistent between versions and even between
// success and failure, so each element is decoded by a tolerant reader that
// accepts all of:
//   - [{ "result": ... }]
//   - [[ <bare result> ]]
//   - [{ "faultCode": n, "faultMessage": "..." }]
//   - [{ "error": { code, message } }]
func (c *Client) SystemMulticall(ctx context.Context, calls []Call) ([]Result, error) {
	type inner struct {
		MethodName string `json:"methodName"`
		Params     []any  `json:"params"`
	}
	payload := make([]inner, 0, len(calls))
	c.mu.Lock()
	tok := c.tokenFn()
	c.mu.Unlock()
	for _, cl := range calls {
		params := cl.Params
		if cl.Auth {
			params = append([]any{tok}, params...)
		}
		payload = append(payload, inner{MethodName: cl.Method, Params: params})
	}

	var raw []json.RawMessage
	if err := c.Call(ctx, "system.multicall", &raw, payload); err != nil {
		return nil, err
	}

	out := make([]Result, 0, len(raw))
	for i, item := range raw {
		res := Result{Index: i}
		res.Value, res.Err = decodeMulticallElement(item)
		out = append(out, res)
	}
	return out, nil
}

// decodeMulticallElement unwraps one system.multicall response element.
func decodeMulticallElement(item json.RawMessage) (json.RawMessage, *RPCError) {
	trimmed := bytes.TrimSpace(item)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	// The element may itself be a one-element array.
	if trimmed[0] == '[' {
		var innerArr []json.RawMessage
		if err := json.Unmarshal(trimmed, &innerArr); err == nil {
			if len(innerArr) == 0 {
				return nil, nil
			}
			return decodeMulticallElement(innerArr[0])
		}
	}
	// A fault envelope.
	var probe struct {
		FaultCode    *int    `json:"faultCode"`
		FaultMessage *string `json:"faultMessage"`
	}
	if err := json.Unmarshal(trimmed, &probe); err == nil && probe.FaultCode != nil {
		return nil, &RPCError{
			Code:    *probe.FaultCode,
			Message: deref(probe.FaultMessage),
		}
	}
	// An {result: ...} envelope.
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err == nil && (envelope.Result != nil || envelope.Error != nil) {
		return envelope.Result, envelope.Error
	}
	// Otherwise the element is the bare result.
	return trimmed, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Call is a single entry of a multicall batch.
type Call struct {
	Method string
	Params []any
	Auth   bool
}

// Result is one entry of a multicall response.
type Result struct {
	Index int
	Value json.RawMessage
	Err   *RPCError
}

// ---------------------------------------------------------------- domain types

// AddURIOptions are the tunable per-download settings handed to aria2.
type AddURIOptions struct {
	Dir              string   `json:"dir,omitempty"`
	Out              string   `json:"out,omitempty"`
	MaxConnection    int      `json:"max-connection-per-server,omitempty"`
	Split            string   `json:"split,omitempty"`
	MinSplitSize     string   `json:"min-split-size,omitempty"`
	MaxLimitRate     string   `json:"max-download-limit,omitempty"`
	Continue         bool     `json:"continue,omitempty"`
	AutoFileRenaming bool     `json:"auto-file-renaming,omitempty"`
	AllowOverwrite   bool     `json:"allow-overwrite,omitempty"`
	Header           []string `json:"header,omitempty"`
	Referer          string   `json:"referer,omitempty"`
	UserAgent        string   `json:"user-agent,omitempty"`
	Timeout          int      `json:"timeout,omitempty"`
	ConnectTimeout   int      `json:"connect-timeout,omitempty"`
	MaxTries         int      `json:"max-tries,omitempty"`
	RetryWait        int      `json:"retry-wait,omitempty"`
	CheckCert        bool     `json:"check-certificate"`
	RemoteTime       bool     `json:"remote-time,omitempty"`
}

// URIEntry is one member of an aria2 URI list. aria2 returns objects here, not
// bare strings, so the list needs a real type.
type URIEntry struct {
	URI    string `json:"uri"`
	Status string `json:"status"`
}

// FileStatus mirrors aria2's tellStatus "status" field.
//
// Every numeric field uses Int64 because aria2 serialises numbers as strings.
type FileStatus struct {
	GID             string     `json:"gid"`
	Status          string     `json:"status"` // active | waiting | paused | error | complete | removed
	TotalLength     Int64      `json:"totalLength"`
	CompletedLength Int64      `json:"completedLength"`
	DownloadSpeed   Int64      `json:"downloadSpeed"`
	UploadSpeed     Int64      `json:"uploadSpeed"`
	Connections     Int64      `json:"connections"`
	ErrorCode       string     `json:"errorCode"`
	ErrorMessage    string     `json:"errorMessage"`
	Dir             string     `json:"dir"`
	URIs            []URIEntry `json:"uris"`
	Files           []struct {
		Path            string     `json:"path"`
		Length          Int64      `json:"length"`
		CompletedLength Int64      `json:"completedLength"`
		Selected        string     `json:"selected"`
		URIs            []URIEntry `json:"uris"`
	} `json:"files"`
	Following string `json:"following"`
	BelongsTo string `json:"belongsTo"`
	NumPieces Int64  `json:"numPieces"`
	Bitfield  string `json:"bitfield"`
	CreatedAt Int64  `json:"createdAt"`
}

// IsTerminal reports whether the download reached a final state.
func (s FileStatus) IsTerminal() bool {
	switch s.Status {
	case "complete", "error", "removed":
		return true
	}
	return false
}

// IsPaused reports whether the download is currently paused.
func (s FileStatus) IsPaused() bool { return s.Status == "paused" }

// IsActive reports whether the download is running right now.
func (s FileStatus) IsActive() bool { return s.Status == "active" }

// Progress is the completion ratio in the range 0..1.
func (s FileStatus) Progress() float64 {
	if s.TotalLength <= 0 {
		return 0
	}
	p := float64(s.CompletedLength) / float64(s.TotalLength)
	if p > 1 {
		return 1
	}
	return p
}

// GlobalStat is the aggregate aria2 daemon statistic.
type GlobalStat struct {
	DownloadSpeed   Int64 `json:"downloadSpeed"`
	UploadSpeed     Int64 `json:"uploadSpeed"`
	NumActive       Int64 `json:"numActive"`
	NumWaiting      Int64 `json:"numWaiting"`
	NumStopped      Int64 `json:"numStopped"`
	NumStoppedTotal Int64 `json:"numStoppedTotal"`
	DownloadedBytes Int64 `json:"downloadedBytesTotal"`
	NumOfFiles      Int64 `json:"numOfFilesTotal"`
	NumOfFilePieces Int64 `json:"numOfFilePiecesTotal"`
	BitfieldLength  Int64 `json:"bitfieldLengthTotal"`
	SessionLength   Int64 `json:"sessionLength"`
	SessionCount    Int64 `json:"sessionCount"`
}

// Version is the result of getVersion.
type Version struct {
	Version         string   `json:"version"`
	EnabledFeatures []string `json:"enabledFeatures"`
}

func needsToken(method string) bool {
	// "system.*" methods never take a token; everything else does.
	return !strings.HasPrefix(method, "system.")
}

// IsNotFoundError reports whether err is aria2's "not found" response.
func IsNotFoundError(err error) bool {
	var re *RPCError
	if errors.As(err, &re) {
		return re.Code == CodeNotFound
	}
	return false
}

// IsUnauthorized reports whether err is an authentication failure.
func IsUnauthorized(err error) bool {
	var re *RPCError
	if errors.As(err, &re) {
		return re.Code == CodeUnauthorized
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
