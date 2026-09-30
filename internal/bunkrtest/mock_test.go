package bunkrtest

import "testing"

func TestParseRange(t *testing.T) {
	cases := []struct {
		header     string
		total      int64
		start, end int64
		ok         bool
	}{
		{"bytes=0-99", 1000, 0, 99, true},
		{"bytes=100-", 1000, 100, 999, true},
		{"bytes=-100", 1000, 900, 999, true},
		{"bytes=0-99999", 1000, 0, 999, true}, // clamped to the body length
		{"bytes=0-10, 20-30", 1000, 0, 10, true},
		{"bytes=500-100", 1000, 0, 0, false}, // inverted
		{"bytes=2000-", 1000, 0, 0, false},   // past the end
		{"bytes=abc-def", 1000, 0, 0, false}, // unparseable
		{"items=0-10", 1000, 0, 0, false},    // wrong unit
		{"", 1000, 0, 0, false},              // absent
		{"bytes=-0", 1000, 0, 0, false},      // suffix of zero
		{"bytes=0-", 0, 0, 0, false},         // empty body
	}
	for _, c := range cases {
		start, end, ok := parseRange(c.header, c.total)
		if ok != c.ok {
			t.Errorf("parseRange(%q, %d) ok = %v, want %v", c.header, c.total, ok, c.ok)
			continue
		}
		if ok && (start != c.start || end != c.end) {
			t.Errorf("parseRange(%q, %d) = %d-%d, want %d-%d",
				c.header, c.total, start, end, c.start, c.end)
		}
		if ok && (end < start || end >= c.total) {
			t.Errorf("parseRange(%q, %d) produced an impossible range %d-%d",
				c.header, c.total, start, end)
		}
	}
}

func TestPayloadIsDeterministic(t *testing.T) {
	a := Payload("seed", 4096)
	b := Payload("seed", 4096)
	if Checksum(a) != Checksum(b) {
		t.Error("Payload is not deterministic for the same seed")
	}
	if len(a) != 4096 {
		t.Errorf("Payload length = %d, want 4096", len(a))
	}
	if Checksum(Payload("other", 4096)) == Checksum(a) {
		t.Error("different seeds produced identical payloads")
	}
}

func TestPayloadTinyAndLarge(t *testing.T) {
	if len(Payload("x", 1)) != 1 {
		t.Error("a 1-byte payload was not produced")
	}
	if len(Payload("x", 100000)) != 100000 {
		t.Error("a 100000-byte payload was truncated")
	}
}

func TestMockServesAlbumAndItem(t *testing.T) {
	s := New()
	defer s.Close()

	body := []byte("hello bunkr")
	s.AddAlbum(Album{
		ID: "T1", Title: "Unit Test",
		Files: []File{{Slug: "f1", Name: "f1.txt", Content: body}},
	})

	// The album page carries the item anchor and the title container.
	album := fetch(t, s.AlbumURL("T1"))
	if !contains(album, `href="/v/f1"`) {
		t.Error("the album page has no item link")
	}
	if !contains(album, "Unit Test") {
		t.Error("the album page has no title")
	}

	// The item page carries the file name and the CDN variable.
	item := fetch(t, s.URL()+"/v/f1")
	if !contains(item, "f1.txt") {
		t.Error("the item page has no file name")
	}
	if !contains(item, "var jsCDN") {
		t.Error("the item page has no jsCDN variable")
	}

	// The media endpoint serves the bytes.
	media := fetch(t, s.MediaURL("f1"))
	if media != string(body) {
		t.Errorf("media body = %q, want %q", media, body)
	}
	if _, mediaGets := s.Stats(); mediaGets != 1 {
		t.Errorf("media endpoint hit %d times, want 1", mediaGets)
	}
}

func TestMockArchiveFallback(t *testing.T) {
	s := New()
	defer s.Close()
	s.AddAlbum(Album{
		ID: "T2", Title: "Archive",
		Files: []File{{Slug: "a1", Name: "a1.zip", Content: []byte("zip"), NoLandingPage: true}},
	})
	item := fetch(t, s.URL()+"/v/a1")
	if contains(item, "var jsCDN") {
		t.Error("an archive page must not expose jsCDN")
	}
	if !contains(item, `data-file-id="a1"`) {
		t.Errorf("an archive page needs data-file-id: %s", item)
	}
}

func TestMockRangeRequests(t *testing.T) {
	s := New()
	defer s.Close()
	s.AddAlbum(Album{
		ID: "T3", Title: "Ranged",
		Files: []File{{Slug: "r1", Name: "r1.bin", Content: Payload("r1", 1000)}},
	})
	code, body, headers := request(t, s.MediaURL("r1"), "bytes=100-199")
	if code != 206 {
		t.Errorf("status = %d, want 206", code)
	}
	if len(body) != 100 {
		t.Errorf("body length = %d, want 100", len(body))
	}
	if got := headers.Get("Content-Range"); got != "bytes 100-199/1000" {
		t.Errorf("Content-Range = %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
