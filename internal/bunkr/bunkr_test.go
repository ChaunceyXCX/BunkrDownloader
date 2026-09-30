package bunkr

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"bunkr.si/a/ABC", "https://bunkr.si/a/ABC"},
		{"https://bunkr.si/a/ABC", "https://bunkr.si/a/ABC"},
		{"https://bunkr.si/a/ABC?page=3", "https://bunkr.si/a/ABC"},
		{"bunkr.si/v/XYZ?page=2&x=1", "https://bunkr.si/v/XYZ?x=1"},
		{"http://127.0.0.1:9000/a/T1", "http://127.0.0.1:9000/a/T1"},
		{"  https://bunkr.si/a/ABC  ", "https://bunkr.si/a/ABC"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeURL(c.in); got != c.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveURLType(t *testing.T) {
	albums := []string{
		"https://bunkr.si/a/AbCdEf", "https://bunkr.si/a/AbCdEf/",
		"https://bunkr.cr/a/AbCdEf", "https://bunkr.si/d/AbCdEf",
	}
	for _, u := range albums {
		got, err := ResolveURLType(u)
		if err != nil {
			t.Errorf("ResolveURLType(%q) errored: %v", u, err)
			continue
		}
		if got != TypeAlbum {
			t.Errorf("ResolveURLType(%q) = %q, want album", u, got)
		}
		if !IsAlbum(u) {
			t.Errorf("IsAlbum(%q) = false", u)
		}
	}

	media := []string{
		"https://bunkr.si/v/XyZ123", "https://bunkr.si/i/XyZ123",
		"https://bunkr.si/s/XyZ123", "https://bunkr.si/f/XyZ123",
	}
	for _, u := range media {
		got, err := ResolveURLType(u)
		if err != nil {
			t.Errorf("ResolveURLType(%q) errored: %v", u, err)
			continue
		}
		if got != TypeMedia {
			t.Errorf("ResolveURLType(%q) = %q, want media", u, got)
		}
	}

	for _, bad := range []string{
		"https://bunkr.si/", "https://example.com/x/y",
		"https://bunkr.si/zzzz/ABC",
	} {
		if _, err := ResolveURLType(bad); err == nil {
			t.Errorf("ResolveURLType(%q) accepted an invalid URL", bad)
		}
	}
}

func TestHostPagePreservesScheme(t *testing.T) {
	if got := HostPage("https://bunkr.si/a/ABC"); got != "https://bunkr.si" {
		t.Errorf("HostPage(https) = %q", got)
	}
	if got := HostPage("http://127.0.0.1:9000/a/ABC"); got != "http://127.0.0.1:9000" {
		t.Errorf("HostPage(http) = %q, want the scheme preserved", got)
	}
}

func TestAlbumIDAndIdentifier(t *testing.T) {
	if got := AlbumID("https://bunkr.si/a/AbCdEf"); got != "AbCdEf" {
		t.Errorf("AlbumID = %q", got)
	}
	if got := Identifier("https://bunkr.si/a/AbCdEf", ""); got != "AbCdEf" {
		t.Errorf("Identifier(album) = %q", got)
	}
	// A media slug that already looks valid comes straight from the URL.
	if got := Identifier("https://bunkr.si/v/XyZ_123", ""); got != "XyZ_123" {
		t.Errorf("Identifier(media) = %q", got)
	}
	// Otherwise fall back to the inline `const slug = "..."` declaration.
	page := `<script>const slug = "recoveredSlug";</script>`
	if got := Identifier("https://bunkr.si/v/some.long.path", page); got != "recoveredSlug" {
		t.Errorf("Identifier(script fallback) = %q, want recoveredSlug", got)
	}
}

func TestTruncateFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"video.mp4", "video.mp4"},
		{"bad:name?.mp4", "badname.mp4"},
		{"no-extension", "no-extension"},
		{"中文名.mp4", "中文名.mp4"},
	}
	for _, c := range cases {
		if got := TruncateFilename(c.in); got != c.want {
			t.Errorf("TruncateFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := TruncateFilename(strings.Repeat("x", 200) + ".mp4")
	if len(long) > MaxFilenameLen+len(".mp4") {
		t.Errorf("truncated name is %d bytes, want <= %d", len(long), MaxFilenameLen+4)
	}
}

func TestSanitizeDirectoryName(t *testing.T) {
	cases := map[string]string{
		`a/b\c:d*e?f"g<h>i|j`: "a_b_c_d_e_f_g_h_i_j",
		"normal name":         "normal name",
		"":                    "unnamed",
		"...":                 "unnamed",
	}
	for in, want := range cases {
		if got := SanitizeDirectoryName(in); got != want {
			t.Errorf("SanitizeDirectoryName(%q) = %q, want %q", in, got, want)
		}
	}
	long := SanitizeDirectoryName(strings.Repeat("n", 300))
	if len(long) != 100 {
		t.Errorf("long directory name is %d chars, want 100", len(long))
	}
}

func TestFormatDirectoryName(t *testing.T) {
	if got := FormatDirectoryName("My Album", "ABC"); got != "My Album (ABC)" {
		t.Errorf("FormatDirectoryName = %q", got)
	}
	if got := FormatDirectoryName("My Album", ""); got != "My Album" {
		t.Errorf("FormatDirectoryName(no id) = %q", got)
	}
	if got := FormatDirectoryName("", "ABC"); got != "ABC" {
		t.Errorf("FormatDirectoryName(no name) = %q", got)
	}
	if got := FormatDirectoryName("", ""); got != "" {
		t.Errorf("FormatDirectoryName(empty) = %q", got)
	}
}

func TestUnescapeText(t *testing.T) {
	cases := map[string]string{
		`Tom &amp; Jerry`: "Tom & Jerry",
		`a\u0026b`:        "a&b",
		`&lt;tag&gt;`:     "<tag>",
		`plain`:           "plain",
		`&amp;\u0026`:     "&&",
	}
	for in, want := range cases {
		if got := UnescapeText(in); got != want {
			t.Errorf("UnescapeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecryptCFEmail(t *testing.T) {
	// Build the Cloudflare encoding the same way their obfuscator does:
	// first byte is the XOR key, the rest are the address XORed with it.
	const plain = "test@example.com"
	raw := append([]byte{0x2a}, make([]byte, len(plain))...)
	for i, c := range []byte(plain) {
		raw[i+1] = c ^ 0x2a
	}
	encoded := hex.EncodeToString(raw)
	if got := DecryptCFEmail(encoded); got != plain {
		t.Errorf("DecryptCFEmail = %q, want %q", got, plain)
	}
	for _, bad := range []string{"", "zz", "abc"} {
		if got := DecryptCFEmail(bad); got != "" {
			t.Errorf("DecryptCFEmail(%q) = %q, want empty", bad, got)
		}
	}
}

func TestURLBasedFilename(t *testing.T) {
	if got := URLBasedFilename("https://cdn.example/path/file.mp4?token=x"); got != "file.mp4" {
		t.Errorf("URLBasedFilename = %q", got)
	}
	if got := URLBasedFilename("https://cdn.example.com/dir/"); got != "dir" {
		t.Errorf("URLBasedFilename(trailing slash) = %q", got)
	}
}

func TestParseDateString(t *testing.T) {
	cases := map[string]bool{
		"2024-03-15T10:22:03Z": true,
		"2024-03-15 10:22:03":  true,
		"2024-03-15":           true,
		"15/03/2024":           true,
		"March 15, 2024":       true,
		"10:22:03 15/03/2024":  true,
		"not a date":           false,
		"":                     false,
	}
	for in, wantOK := range cases {
		got := parseDateString(in)
		if (got != nil) != wantOK {
			t.Errorf("parseDateString(%q) = %v, want ok=%v", in, got, wantOK)
		}
		if got != nil && got.Location() != time.UTC {
			t.Errorf("parseDateString(%q) is not UTC", in)
		}
	}
}

func TestFormatItemFilename(t *testing.T) {
	cases := []struct {
		page, cdn, want string
	}{
		// Same stem, CDN adds nothing: keep the page name (it has the ext).
		{"filea.bin", "filea", "filea.bin"},
		// CDN disambiguates with a separator: the CDN name wins.
		{"video", "video-1.mp4", "video-1.mp4"},
		{"clip", "clip_alt.mkv", "clip_alt.mkv"},
		// Unrelated names: combine them.
		{"Nice Name", "a1b2c3.mp4", "Nice Name-a1b2c3.mp4"},
		// The page title has no extension: the CDN one is reused.
		{"Nice Name", "a1b2c3.mp4", "Nice Name-a1b2c3.mp4"},
		// Identical: unchanged.
		{"x.mp4", "x.mp4", "x.mp4"},
		// No page name: use the CDN name.
		{"", "a1b2.mp4", "a1b2.mp4"},
	}
	for _, c := range cases {
		if got := formatItemFilename(c.page, c.cdn); got != c.want {
			t.Errorf("formatItemFilename(%q, %q) = %q, want %q", c.page, c.cdn, got, c.want)
		}
	}
}

func TestExtractPageVars(t *testing.T) {
	html := `<html><head><script>
	var jsCDN = 'https:\/\/cdn.example.com\/media\/';
	var other = "x";
	</script></head><body></body></html>`
	vars := ExtractPageVars(ParseDocument(html))
	if got := vars["jsCDN"]; got != "https://cdn.example.com/media/" {
		t.Errorf("jsCDN = %q, want the unescaped URL", got)
	}
	if got := vars["other"]; got != "x" {
		t.Errorf("other = %q, want x", got)
	}
	if got := ExtractPageVars(ParseDocument("<html><body>none</body></html>")); len(got) != 0 {
		t.Errorf("no scripts produced %v, want an empty map", got)
	}
}

func TestUnescapeJSPath(t *testing.T) {
	if got := UnescapeJSPath(`https:\/\/cdn.example.com\/a\u0026b`); got != "https://cdn.example.com/a&b" {
		t.Errorf("UnescapeJSPath = %q", got)
	}
}

func TestExtractItemPages(t *testing.T) {
	html := `<html><body>
	  <a class="after:absolute after:z-10 after:inset-0" href="/v/fileA">A</a>
	  <a class="after:absolute after:z-10 after:inset-0" href="/v/fileB">B</a>
	  <a class="unrelated" href="/about">About</a>
	</body></html>`
	doc := ParseDocument(html)
	c := NewCrawler(nil)
	got := c.ExtractItemPages(doc, "https://bunkr.si")
	if len(got) != 2 {
		t.Fatalf("ExtractItemPages returned %d links, want 2: %v", len(got), got)
	}
	if got[0] != "https://bunkr.si/v/fileA" || got[1] != "https://bunkr.si/v/fileB" {
		t.Errorf("links = %v", got)
	}
}

func TestItemFilenameFromPage(t *testing.T) {
	html := `<html><body>
	  <h1 class="text-subs font-semibold text-base sm:text-lg truncate">Nice &amp; Clean.mp4</h1>
	</body></html>`
	c := NewCrawler(nil)
	if got := c.ItemFilename(ParseDocument(html)); got != "Nice & Clean.mp4" {
		t.Errorf("ItemFilename = %q", got)
	}
}

func TestItemFilenameDecryptsCloudflareEmail(t *testing.T) {
	plain := "test@example.com"
	raw := append([]byte{0x2a}, make([]byte, len(plain))...)
	for i, c := range []byte(plain) {
		raw[i+1] = c ^ 0x2a
	}
	html := `<html><body>
	  <h1 class="text-subs font-semibold text-base sm:text-lg truncate">
	    report-<span class="__cf_email__" data-cfemail="` + hex.EncodeToString(raw) + `"></span>.pdf
	  </h1>
	</body></html>`
	c := NewCrawler(nil)
	got := c.ItemFilename(ParseDocument(html))
	if got != "report-test@example.com.pdf" {
		t.Errorf("ItemFilename = %q, want the decrypted address", got)
	}
}
