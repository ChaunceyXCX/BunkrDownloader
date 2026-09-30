package bunkr

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Document is a parsed HTML page.
type Document struct {
	*goquery.Document
	Raw string
}

// ParseDocument parses HTML into a Document, preserving the raw source so
// inline `<script>` contents remain available for the signing pipeline.
func ParseDocument(html string) *Document {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil || doc == nil {
		// goquery only fails on a broken reader; an empty document keeps
		// callers from having to nil-check.
		doc, _ = goquery.NewDocumentFromReader(strings.NewReader(""))
	}
	return &Document{Document: doc, Raw: html}
}

// Scripts returns the concatenated text of every inline <script> element.
func (d *Document) Scripts() string {
	if d == nil {
		return ""
	}
	var sb strings.Builder
	d.Find("script").Each(func(_ int, s *goquery.Selection) {
		if t := s.Text(); t != "" {
			sb.WriteString(t)
			sb.WriteString("\n")
		}
	})
	return sb.String()
}

// Attribute returns the first non-empty value of attr on the page.
func (d *Document) Attribute(sel, attr string) string {
	if d == nil {
		return ""
	}
	v, ok := d.Find(sel).First().Attr(attr)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}
