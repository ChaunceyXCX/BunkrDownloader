// Package bunkr implements the Bunkr page crawler: it resolves album/media
// URLs, walks paginated album listings and signs the CDN download links.
//
// The logic mirrors the original Python implementation in src/crawlers.
package bunkr

import (
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// URL types.
const (
	TypeAlbum = "album"
	TypeMedia = "media"
)

// knownURLTypes maps the URL path segment to its kind.
var knownURLTypes = map[string]string{
	"a":    TypeAlbum, // album
	"i":    TypeMedia, // image
	"v":    TypeMedia, // video
	"s":    TypeMedia, // stream
	"f":    TypeMedia, // file
	"p":    TypeMedia, // post
	"c":    TypeMedia, // collection item
	"go":   TypeAlbum, // go link album
	"d":    TypeAlbum, // dump album
	"ps":   TypeAlbum, // ps album
	"m":    TypeMedia, // music
	"b":    TypeMedia, // blog attachment
	"cf":   TypeMedia, // cloudflare bridged
	"em":   TypeMedia, // embed
	"dmp4": TypeMedia,
}

var (
	mediaSlugRe   = regexp.MustCompile(`const\s+slug\s*=\s*"([a-zA-Z0-9_-]+)"`)
	validSlugRe   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	invalidCharRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
)

// MaxFilenameLen caps the sanitised file name (extension excluded).
const MaxFilenameLen = 120

// ErrInvalidURL is returned for URLs that are not recognisable Bunkr links.
type ErrInvalidURL struct {
	URL    string
	Reason string
}

func (e *ErrInvalidURL) Error() string {
	return fmt.Sprintf("invalid Bunkr URL %q: %s", e.URL, e.Reason)
}

// NormalizeURL cleans a user-supplied link: it supplies the https scheme when
// none was given, keeps an explicit http scheme (self-hosted mirrors run over
// plain http) and drops the pagination parameter.
//
//nolint:gocritic // the two-prefix check is intentional and cheap
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
		// keep the caller's scheme
	case hasOtherScheme(lower):
		// ftp://, file://, javascript:// … are not Bunkr links; leave the
		// string intact so the caller can report a precise error.
		return raw
	default:
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if q := u.Query(); q.Has("page") {
		q.Del("page")
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// hasOtherScheme reports whether the string already carries a non-HTTP scheme.
func hasOtherScheme(lower string) bool {
	i := strings.Index(lower, "://")
	if i <= 0 {
		return false
	}
	for _, r := range lower[:i] {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// SchemeOf returns the lowercased URL scheme.
func SchemeOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Scheme)
}

// IsHTTPScheme reports whether raw is an http(s) URL.
func IsHTTPScheme(raw string) bool {
	s := SchemeOf(raw)
	return s == "http" || s == "https"
}

// HostPage returns the scheme+host prefix of a URL, preserving the scheme so
// item links resolve against the same origin as the album page
// ("https://bunkr.si", or "http://127.0.0.1:9000" for a local mirror).
func HostPage(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + u.Host
}

// ResolveURLType reports whether a URL points at an album or a single item.
func ResolveURLType(raw string) (string, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(raw), "/")
	seg := path.Base(trimmed)
	parent := path.Base(path.Dir(trimmed))
	if t, ok := knownURLTypes[strings.ToLower(parent)]; ok {
		return t, nil
	}
	// Fallback: some mirrors expose the type in the final segment.
	if t, ok := knownURLTypes[strings.ToLower(seg)]; ok {
		return t, nil
	}
	return "", &ErrInvalidURL{URL: raw, Reason: fmt.Sprintf("unrecognised URL type %q", parent)}
}

// IsAlbum reports whether a URL is an album listing.
func IsAlbum(raw string) bool {
	t, err := ResolveURLType(raw)
	return err == nil && t == TypeAlbum
}

// AlbumID extracts the trailing identifier of a URL.
func AlbumID(raw string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(raw), "/")
	return path.Base(trimmed)
}

// MediaSlug derives a stable identifier for a media URL, falling back to the
// page's inline JavaScript when the URL slug is not usable.
func MediaSlug(raw, pageHTML string) string {
	slug := path.Base(strings.TrimSuffix(strings.TrimSpace(raw), "/"))
	if validSlugRe.MatchString(slug) {
		return slug
	}
	if m := mediaSlugRe.FindStringSubmatch(pageHTML); len(m) == 2 {
		return m[1]
	}
	return slug
}

// Identifier returns the album ID for albums and the media slug otherwise.
func Identifier(raw, pageHTML string) string {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		decoded = raw
	}
	if IsAlbum(decoded) {
		return AlbumID(decoded)
	}
	return MediaSlug(decoded, pageHTML)
}

// AlbumName extracts the album title from a listing page.
func AlbumName(doc *Document) string {
	if doc == nil {
		return ""
	}
	sel := doc.Find(`div.text-subs.font-semibold.flex.text-base.sm\:text-lg h1`)
	if sel.Length() == 0 {
		sel = doc.Find(`div.text-subs.font-semibold.flex h1`)
	}
	if sel.Length() == 0 {
		sel = doc.Find(`h1`)
	}
	if sel.Length() == 0 {
		return ""
	}
	return repairMojibake(unescapeText(sel.First().Text()))
}

// repairMojibake fixes UTF-8 that was decoded as Latin-1 by the upstream site.
func repairMojibake(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if isLatin1(b) {
		if fixed, err := decodeUTF8FromLatin1(b); err == nil && fixed != s {
			return fixed
		}
	}
	return s
}

func isLatin1(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return true
		}
	}
	return false
}

func decodeUTF8FromLatin1(b []byte) (string, error) {
	// Latin-1 -> Unicode code points -> UTF-8 bytes.
	runes := make([]rune, 0, len(b))
	for _, c := range b {
		runes = append(runes, rune(c))
	}
	return string(runes), nil
}

// URLBasedFilename extracts the file name from a download link.
func URLBasedFilename(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return path.Base(strings.TrimSuffix(u.Path, "/"))
}

// RemoveInvalidCharacters strips characters that are illegal in file names.
func RemoveInvalidCharacters(s string) string {
	return invalidCharRe.ReplaceAllString(s, "")
}

// SanitizeDirectoryName makes a string safe to use as a directory name on
// Windows, macOS and Linux.
func SanitizeDirectoryName(s string) string {
	replacer := strings.NewReplacer(
		"<", "_", ">", "_", ":", "_", `"`, "_", "/", "_", "\\", "_",
		"|", "_", "?", "_", "*", "_",
	)
	s = replacer.Replace(s)
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	out = strings.Trim(out, ".")
	if out == "" {
		return "unnamed"
	}
	if len(out) > 100 {
		out = strings.TrimSpace(out[:100])
	}
	return out
}

// TruncateFilename sanitises and length-limits a file name, keeping the
// extension intact.
func TruncateFilename(name string) string {
	if name == "" {
		return "unnamed"
	}
	ext := path.Ext(name)
	stem := RemoveInvalidCharacters(strings.TrimSuffix(name, ext))
	if stem == "" {
		stem = "unnamed"
	}
	if len(stem) > MaxFilenameLen {
		stem = strings.TrimSpace(stem[:MaxFilenameLen])
	}
	return stem + ext
}

// FormatDirectoryName renders "Title (ID)" when an ID is present.
func FormatDirectoryName(name, id string) string {
	switch {
	case name == "" && id == "":
		return ""
	case name == "":
		return id
	case id == "":
		return name
	default:
		return name + " (" + id + ")"
	}
}

// UnescapeText decodes HTML entities and JavaScript \\uXXXX escapes.
func UnescapeText(s string) string {
	s = html.UnescapeString(s)
	s = decodeJSUescapes(s)
	return fixDoubleEncoding(s)
}

var jsUnicodeRe = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)

func decodeJSUescapes(s string) string {
	return jsUnicodeRe.ReplaceAllStringFunc(s, func(m string) string {
		n, err := strconvParseHex(m[2:])
		if err != nil {
			return m
		}
		return string(rune(n))
	})
}

func strconvParseHex(s string) (int, error) {
	var n int
	for _, c := range s {
		n <<= 4
		switch {
		case c >= '0' && c <= '9':
			n |= int(c - '0')
		case c >= 'a' && c <= 'f':
			n |= int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			n |= int(c-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex")
		}
	}
	return n, nil
}

func unescapeText(s string) string { return UnescapeText(s) }

// fixDoubleEncoding re-encodes a Latin-1 decoded UTF-8 sequence when needed.
func fixDoubleEncoding(s string) string { return repairMojibake(s) }
