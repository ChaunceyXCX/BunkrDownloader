package bunkrtest

import (
	"io"
	"net/http"
	"testing"
)

// fetch GETs a URL and returns the body, failing the test on any error status.
func fetch(t *testing.T, url string) string {
	t.Helper()
	code, body, _ := request(t, url, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d", url, code)
	}
	return body
}

// request performs a GET with an optional Range header.
func request(t *testing.T, url, rangeHeader string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw), resp.Header
}
