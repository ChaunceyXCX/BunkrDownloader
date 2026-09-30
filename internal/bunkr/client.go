package bunkr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Bunkr service endpoints.
const (
	// BunkrAPISign signs a CDN media path and returns a short-lived token.
	BunkrAPISign = "https://glb-apisign.cdn.cr/sign"
	// DownloadAPI resolves an unsigned media URL for assets without a landing page.
	DownloadAPI = "https://dl.bunkr.cr/api/_001_v2"
	// DownloadReferer is sent with media requests.
	DownloadReferer = "https://dl.bunkrr.cr/"
)

// defaultUserAgent mimics a desktop Firefox build.
const defaultUserAgent = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:136.0) Gecko/20100101 Firefox/136.0"

const (
	httpTimeout      = 20 * time.Second
	apiTimeout       = 12 * time.Second
	maxRetries       = 3
	retryBaseDelay   = 2.0
	maxResponseBytes = 24 << 20
)

// jsVarsRe captures `var name = value;` assignments in inline scripts.
var jsVarsRe = regexp.MustCompile(`(?s)var\s+(\w+)\s*=\s*(".*?"|'.*?'|[^;]+);`)

// HTTPClient fetches pages and the Bunkr JSON APIs.
type HTTPClient struct {
	hc  *http.Client
	api *http.Client
	ua  string
}

// NewHTTPClient builds a client with connection pooling and sane timeouts.
func NewHTTPClient(userAgent string) *HTTPClient {
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	newTransport := func() *http.Transport {
		return &http.Transport{
			MaxIdleConns:          64,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 2 * time.Second,
			ForceAttemptHTTP2:     true,
		}
	}
	return &HTTPClient{
		hc:  &http.Client{Timeout: httpTimeout, Transport: newTransport()},
		api: &http.Client{Timeout: apiTimeout, Transport: newTransport()},
		ua:  userAgent,
	}
}

// UserAgent returns the configured user agent.
func (c *HTTPClient) UserAgent() string { return c.ua }

func (c *HTTPClient) newRequest(ctx context.Context, method, target string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	return req, nil
}

// FetchPage downloads an HTML page and parses it.
//
// It returns nil when the page cannot be retrieved; callers decide whether that
// is fatal (album discovery) or a per-item skip.
func (c *HTTPClient) FetchPage(ctx context.Context, target string) (*Document, error) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	req, err := c.newRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", target, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return nil, &HTTPStatusError{URL: target, Status: resp.StatusCode, StatusText: resp.Status}
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", target, err)
	}
	return ParseDocument(string(raw)), nil
}

// HTTPStatusError reports a non-2xx HTTP response.
type HTTPStatusError struct {
	URL        string
	Status     int
	StatusText string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP %d for %s", e.Status, e.URL)
}

// IsNotFound reports whether the status is 404.
func (e *HTTPStatusError) IsNotFound() bool { return e.Status == http.StatusNotFound }

// GetJSON performs a GET returning decoded JSON, with retries and backoff.
func (c *HTTPClient) GetJSON(ctx context.Context, endpoint string, params url.Values, out any) error {
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	return c.requestJSON(ctx, http.MethodGet, endpoint, nil, params, out, true)
}

// PostJSON performs a POST with a JSON body, returning decoded JSON.
func (c *HTTPClient) PostJSON(ctx context.Context, endpoint string, payload any, out any) error {
	return c.requestJSON(ctx, http.MethodPost, endpoint, payload, nil, out, false)
}

// requestJSON is the shared retrying JSON transport. For POST requests we ask
// for gzip/deflate explicitly: the download API answers with Brotli otherwise,
// which Go does not decode.
func (c *HTTPClient) requestJSON(ctx context.Context, method, endpoint string, payload any, params url.Values, out any, includeParamsInBody bool) error {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.doJSONOnce(ctx, method, endpoint, payload, params, out, includeParamsInBody)
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Only transient transport failures are worth retrying.
		if !isRetryable(err) {
			return err
		}
		if attempt < maxRetries {
			delay := time.Duration(retryBaseDelay * math.Pow(2, float64(attempt-1)) * float64(time.Second))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return lastErr
}

func (c *HTTPClient) doJSONOnce(ctx context.Context, method, endpoint string, payload any, params url.Values, out any, includeParamsInBody bool) error {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	var body io.Reader
	var raw []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		raw = b
		body = strings.NewReader(string(b))
	}

	target := endpoint
	if len(params) > 0 && method == http.MethodGet {
		target += "?" + params.Encode()
	}

	req, err := c.newRequest(ctx, method, target, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if raw != nil {
		req.Header.Set("Content-Type", "application/json")
		// Force a decodable encoding for the download API.
		req.Header.Set("Accept-Encoding", "gzip, deflate")
	}
	_ = includeParamsInBody

	resp, err := c.api.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return &HTTPStatusError{URL: endpoint, Status: resp.StatusCode, StatusText: resp.Status}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode json from %s: %w", endpoint, err)
	}
	return nil
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var se *HTTPStatusError
	if asHTTPStatus(err, &se) {
		return se.Status >= 500 || se.Status == http.StatusTooManyRequests
	}
	// Transport-level failures.
	msg := err.Error()
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "no such host")
}

func asHTTPStatus(err error, target **HTTPStatusError) bool {
	if e, ok := err.(*HTTPStatusError); ok {
		*target = e
		return true
	}
	return false
}

// parseInt64 is a defensive helper for the loosely typed aria2/Bunkr payloads.
func parseInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	case html.Node:
		return 0
	default:
		return 0
	}
}
