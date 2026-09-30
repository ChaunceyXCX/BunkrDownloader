// Package bunkrtest provides a local mock of the Bunkr web frontend.
//
// It serves HTML shaped exactly like the real site (album grid with the
// `after:inset-0` item anchors, item pages carrying the filename <h1>, the
// `var jsCDN` script block and `data-file-id`), plus the signing and download
// APIs. That lets the whole crawl → sign → aria2 → disk pipeline be exercised
// deterministically in tests without touching the live service.
package bunkrtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// File is one item served by the mock.
type File struct {
	Slug     string // URL slug under /v/
	Name     string // display name shown on the item page
	Content  []byte // bytes served by the media endpoint
	AlbumID  string
	ItemDate string // "HH:MM:SS DD/MM/YYYY" as rendered by the album grid
	// NoLandingPage mimics archives: the item page exposes no jsCDN variable,
	// forcing the download-API fallback path.
	NoLandingPage bool
	// OmitFromAlbum hides the file from the album grid.
	OmitFromAlbum bool
	// BytesPerWrite throttles the media endpoint so a download takes a
	// measurable amount of time. 0 writes the whole body at once.
	BytesPerWrite int
	// WriteDelay is the pause between throttled writes.
	WriteDelay time.Duration
}

// Album is a collection of files exposed under /a/<id>.
type Album struct {
	ID    string
	Title string
	Files []File
}

// Server is a running mock Bunkr instance.
type Server struct {
	HTTP *httptest.Server

	mu     sync.Mutex
	albums map[string]*Album
	// counters let tests assert how often each endpoint was hit.
	signCalls int
	fileGets  int
}

// New starts a mock server. Callers must Close it.
func New() *Server {
	s := &Server{albums: map[string]*Album{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	s.HTTP = httptest.NewServer(mux)
	return s
}

// Close shuts the mock down.
func (s *Server) Close() { s.HTTP.Close() }

// URL is the base URL of the mock (e.g. http://127.0.0.1:PORT).
func (s *Server) URL() string { return s.HTTP.URL }

// Endpoints returns the sign/download API locations for the crawler.
func (s *Server) Endpoints() (signAPI, downloadAPI string) {
	return s.HTTP.URL + "/api/sign", s.HTTP.URL + "/api/download"
}

// AddAlbum registers an album.
func (s *Server) AddAlbum(a Album) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.albums[a.ID] = &a
}

// AlbumURL returns the public URL of an album.
func (s *Server) AlbumURL(id string) string { return s.HTTP.URL + "/a/" + id }

// MediaURL returns the signed media URL the crawler is expected to build.
func (s *Server) MediaURL(slug string) string { return s.HTTP.URL + "/media/" + slug }

// Stats returns how many sign/media requests the mock served.
func (s *Server) Stats() (signCalls, fileGets int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.signCalls, s.fileGets
}

// ---------------------------------------------------------- HTTP handlers

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/sign":
		s.handleSign(w, r)
	case path == "/api/download":
		s.handleDownloadAPI(w, r)
	case strings.HasPrefix(path, "/a/"):
		s.handleAlbum(w, strings.TrimPrefix(path, "/a/"))
	case strings.HasPrefix(path, "/v/"):
		s.handleItem(w, strings.TrimPrefix(path, "/v/"))
	case strings.HasPrefix(path, "/media/"):
		s.handleMedia(w, r, strings.TrimPrefix(path, "/media/"))
	case path == "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>bunkr mock</title>`))
	default:
		http.NotFound(w, r)
	}
}

// handleAlbum renders the album grid, mirroring the real markup.
func (s *Server) handleAlbum(w http.ResponseWriter, id string) {
	s.mu.Lock()
	album, ok := s.albums[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, nil)
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html><html><head><title>%s</title></head><body>`, album.Title)
	// The album name lives in the exact container the crawler looks for.
	fmt.Fprintf(&b, `<div class="text-subs font-semibold flex text-base sm:text-lg"><h1>%s</h1></div>`,
		escape(album.Title))
	b.WriteString(`<div class="grid grid-cols-2">`)

	for i, f := range album.Files {
		if f.OmitFromAlbum {
			continue
		}
		// The real markup uses Tailwind arbitrary-value classes; the crawler
		// matches on the `after:inset-0` marker substring.
		fmt.Fprintf(&b,
			`<div class="item"><a class="after:absolute after:z-10 after:inset-0" href="/v/%s">`+
				`<span class="theDate">%s</span></a></div>`,
			escape(f.Slug), escape(orDefault(f.ItemDate, "10:00:00 01/01/2024")))
		_ = i
	}
	b.WriteString(`</div></body></html>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// handleItem renders a single file page.
func (s *Server) handleItem(w http.ResponseWriter, slug string) {
	f, ok := s.find(slug)
	if !ok {
		http.NotFound(w, nil)
		return
	}
	sum := sha256.Sum256(f.Content)
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><title>item</title></head><body>`)
	if !f.NoLandingPage {
		// Real pages declare the CDN base in an inline script.
		fmt.Fprintf(&b, `<script>var jsCDN = '%s';</script>`,
			s.HTTP.URL+"/media/"+f.Slug)
	}
	// Archives expose no CDN variable; the data-file-id marker on the first
	// script is what drives the direct-download-API fallback.
	if f.NoLandingPage {
		fmt.Fprintf(&b, `<script data-file-id="%s"></script>`, escape(f.Slug))
	}
	// The crawler reads the filename from this <h1> class list.
	fmt.Fprintf(&b,
		`<h1 class="text-subs font-semibold text-base sm:text-lg truncate" title="%s">%s</h1>`,
		escape(f.Name), escape(f.Name))
	b.WriteString(`</body></html>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
	_ = sum
}

// handleSign mimics the Bunkr signing API.
func (s *Server) handleSign(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.signCalls++
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Query().Get("path")
	seed := "0"
	if path != "" {
		seed = string(path[0])
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token": "mocktoken" + seed,
		"ex":    "9999999999",
	})
}

// handleDownloadAPI mimics the direct download API.
func (s *Server) handleDownloadAPI(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"mediafiles": s.HTTP.URL,
		"path":       "/media/" + body.ID,
	})
}

// handleMedia serves the actual bytes, honouring HTTP Range requests the way a
// real CDN does. aria2 splits a file into pieces and asks for byte ranges, so a
// mock that ignored Range would break every multi-connection download.
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request, slug string) {
	f, ok := s.find(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	s.fileGets++
	s.mu.Unlock()

	total := int64(len(f.Content))
	head := w.Header()
	head.Set("Content-Type", "application/octet-stream")
	head.Set("Accept-Ranges", "bytes")

	start, end, partial := parseRange(r.Header.Get("Range"), total)
	if !partial {
		head.Set("Content-Length", strconv.FormatInt(total, 10))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		s.writeBody(w, f.Content, f)
		return
	}

	head.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	head.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	if r.Method == http.MethodHead {
		return
	}
	s.writeBody(w, f.Content[start:end+1], f)
}

// parseRange interprets a single-range Range header against a known size.
func parseRange(header string, total int64) (start, end int64, ok bool) {
	if !strings.HasPrefix(header, "bytes=") || total <= 0 {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	spec, _, _ = strings.Cut(spec, ",") // only the first range is honoured
	rawStart, rawEnd, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false
	}
	if rawStart == "" {
		// suffix range: last N bytes
		n, err := strconv.ParseInt(rawEnd, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > total {
			n = total
		}
		return total - n, total - 1, true
	}
	start, err := strconv.ParseInt(rawStart, 10, 64)
	if err != nil || start < 0 || start >= total {
		return 0, 0, false
	}
	end = total - 1
	if rawEnd != "" {
		if v, err := strconv.ParseInt(rawEnd, 10, 64); err == nil && v < end {
			end = v
		}
	}
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}

// writeBody emits the payload, optionally throttled to simulate a real link.
func (s *Server) writeBody(w http.ResponseWriter, body []byte, f File) {
	if f.BytesPerWrite <= 0 {
		_, _ = w.Write(body)
		return
	}
	flusher, _ := w.(http.Flusher)
	for off := 0; off < len(body); off += f.BytesPerWrite {
		end := off + f.BytesPerWrite
		if end > len(body) {
			end = len(body)
		}
		if _, err := w.Write(body[off:end]); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		if f.WriteDelay > 0 {
			time.Sleep(f.WriteDelay)
		}
	}
}

// find locates a file by slug across every album.
func (s *Server) find(slug string) (File, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, album := range s.albums {
		for _, f := range album.Files {
			if f.Slug == slug {
				return f, true
			}
		}
	}
	return File{}, false
}

// ------------------------------------------------------------- fixture data

// Payload builds deterministic test content of the requested size.
func Payload(name string, size int) []byte {
	seed := sha256.Sum256([]byte(name))
	out := make([]byte, 0, size)
	for len(out) < size {
		next := sha256.Sum256(seed[:])
		out = append(out, next[:]...)
		seed = next
	}
	return out[:size]
}

// Checksum is a hex SHA-256 of the given bytes.
func Checksum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func escape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
