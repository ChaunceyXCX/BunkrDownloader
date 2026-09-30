package bunkr

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"path"
	"regexp"
	"strconv"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Item is one downloadable file discovered on a Bunkr page.
type Item struct {
	URL        string     // absolute item page URL
	Filename   string     // display name from the item page
	Link       string     // signed direct download URL (empty until resolved)
	Size       int64      // known size in bytes, 0 when unknown
	ItemDate   *time.Time // upload date when the page exposes one
	AlbumID    string     // owning album, when known
	Downloaded bool
}

// Endpoints are the Bunkr services a crawler talks to. They are fields (rather
// than bare constants) so tests can point the pipeline at a local mock and so a
// user can run the tool against a mirror.
type Endpoints struct {
	SignAPI     string // signing API returning {token, ex}
	DownloadAPI string // unsigned-URL API for assets without a landing page
	Referer     string // Referer sent with media requests
}

// DefaultEndpoints returns the production Bunkr endpoints.
func DefaultEndpoints() Endpoints {
	return Endpoints{
		SignAPI:     BunkrAPISign,
		DownloadAPI: DownloadAPI,
		Referer:     DownloadReferer,
	}
}

// Crawler resolves Bunkr pages into items.
type Crawler struct {
	hc        *HTTPClient
	endpoints Endpoints
	maxPage   int // album pagination safety limit
}

// NewCrawler builds a crawler around an HTTP client using production endpoints.
func NewCrawler(hc *HTTPClient) *Crawler {
	return NewCrawlerWith(hc, DefaultEndpoints())
}

// NewCrawlerWith builds a crawler with explicit service endpoints.
func NewCrawlerWith(hc *HTTPClient, ep Endpoints) *Crawler {
	if hc == nil {
		hc = NewHTTPClient("")
	}
	if ep.SignAPI == "" {
		ep = DefaultEndpoints()
	}
	return &Crawler{hc: hc, endpoints: ep, maxPage: 500}
}

// Endpoints returns the service endpoints in use.
func (c *Crawler) Endpoints() Endpoints { return c.endpoints }

// HTTPClient exposes the underlying HTTP client so callers can reuse the
// connection pool (the manager reuses it for the album landing page).
func (c *Crawler) HTTPClient() *HTTPClient { return c.hc }

// itemLinkSelector matches the anchor class Bunkr uses for each grid entry.
const itemLinkSelector = `a.after\:absolute.after\:z-10.after\:inset-0[href]`

// ExtractItemPages returns the absolute item page URLs of an album listing.
func (c *Crawler) ExtractItemPages(doc *Document, host string) []string {
	if doc == nil {
		return nil
	}
	var out []string
	seen := make(map[string]bool)
	// The CSS class contains escaped colons; match on the marker substring
	// instead so a goquery CSS-engine upgrade cannot break the selector.
	doc.Find(`a[class]`).Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" {
			return
		}
		cls, _ := s.Attr("class")
		if !strings.Contains(cls, "after:inset-0") {
			return
		}
		abs := absolute(host, href)
		if abs == "" || seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, abs)
	})
	return out
}

// ExtractNextPages returns the paginated URLs of an album listing.
func (c *Crawler) ExtractNextPages(doc *Document, albumURL string) []string {
	if doc == nil {
		return nil
	}
	nav := doc.Find("nav.pagination")
	if nav.Length() == 0 {
		return nil // single-page album
	}
	text := nav.Text()
	max := 0
	for _, m := range regexp.MustCompile(`\d+`).FindAllString(text, -1) {
		if n, err := strconv.Atoi(m); err == nil && n > max {
			max = n
		}
	}
	if max <= 1 {
		return nil
	}
	if max > c.maxPage {
		max = c.maxPage
	}
	out := make([]string, 0, max-1)
	for page := 2; page <= max; page++ {
		sep := "?"
		idx := strings.Index(albumURL, "?")
		if idx >= 0 {
			sep = "&"
		}
		out = append(out, fmt.Sprintf("%s%spage=%d", albumURL, sep, page))
	}
	return out
}

// AlbumItems walks an album (including pagination) and returns its items.
func (c *Crawler) AlbumItems(ctx context.Context, albumURL string, first *Document) ([]Item, error) {
	doc := first
	if doc == nil {
		var err error
		doc, err = c.hc.FetchPage(ctx, albumURL)
		if err != nil {
			return nil, fmt.Errorf("fetch album %s: %w", albumURL, err)
		}
	}
	host := HostPage(albumURL)
	pages := c.ExtractItemPages(doc, host)
	if len(pages) == 0 {
		// A brand-new or fully mirrored album may only expose its files.
		if alt := c.ExtractItemPagesFallback(doc, host); len(alt) > 0 {
			pages = alt
		} else {
			return nil, fmt.Errorf("no items found in album %s", albumURL)
		}
	}

	dates := extractItemDates(doc)
	items := make([]Item, 0, len(pages))
	for i, u := range pages {
		it := Item{URL: u}
		if i < len(dates) && dates[i] != nil {
			it.ItemDate = dates[i]
		}
		items = append(items, it)
	}

	for _, next := range c.ExtractNextPages(doc, albumURL) {
		nd, err := c.hc.FetchPage(ctx, next)
		if err != nil {
			// A missing page should not abort the whole album.
			continue
		}
		nextPages := c.ExtractItemPages(nd, host)
		if len(nextPages) == 0 {
			nextPages = c.ExtractItemPagesFallback(nd, host)
		}
		nextDates := extractItemDates(nd)
		for i, u := range nextPages {
			it := Item{URL: u}
			if i < len(nextDates) && nextDates[i] != nil {
				it.ItemDate = nextDates[i]
			}
			items = append(items, it)
		}
	}
	return items, nil
}

// ExtractItemPagesFallback finds item links when the primary selector misses
// (some mirrors changed their markup). It only accepts links that look like
// Bunkr item pages.
func (c *Crawler) ExtractItemPagesFallback(doc *Document, host string) []string {
	if doc == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	doc.Find(`a[href]`).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		abs := absolute(host, href)
		if abs == "" || seen[abs] {
			return
		}
		if _, err := ResolveURLType(abs); err != nil {
			return
		}
		if IsAlbum(abs) {
			return
		}
		seen[abs] = true
		out = append(out, abs)
	})
	return out
}

// ResolveItem fetches an item page and returns the filename and signed link.
func (c *Crawler) ResolveItem(ctx context.Context, item Item, cleanName bool) (Item, error) {
	doc, err := c.hc.FetchPage(ctx, item.URL)
	if err != nil {
		return item, fmt.Errorf("fetch item %s: %w", item.URL, err)
	}
	item.Filename = c.ItemFilename(doc)
	if item.ItemDate == nil {
		item.ItemDate = itemDateFromDoc(doc)
	}
	link, err := c.ResolveDownloadLink(ctx, item.URL, doc)
	if err != nil {
		return item, err
	}
	item.Link = link

	if !cleanName {
		item.Filename = formatItemFilename(item.Filename, URLBasedFilename(link))
	}
	if item.Filename == "" {
		item.Filename = path.Base(item.URL)
	}
	return item, nil
}

// ItemFilename extracts the display file name from an item page,
// decrypting Cloudflare email obfuscation when present.
func (c *Crawler) ItemFilename(doc *Document) string {
	if doc == nil {
		return ""
	}
	sel := doc.Find(`h1.text-subs.font-semibold.text-base.sm\:text-lg.truncate`)
	if sel.Length() == 0 {
		sel = doc.Find("h1")
	}
	if sel.Length() == 0 {
		return ""
	}
	title, _ := sel.First().Attr("title")
	if title != "" {
		return UnescapeText(title)
	}

	// Any Cloudflare-obfuscated email inside the heading is decrypted while
	// the text is assembled, so the surrounding markup stays untouched.
	return strings.TrimSpace(UnescapeText(textWithDecryptedEmails(sel.First())))
}

// DecryptCFEmail reverses Cloudflare's email protection encoding.
func DecryptCFEmail(hexStr string) string {
	if hexStr == "" || len(hexStr)%2 != 0 {
		return ""
	}
	raw := make([]byte, 0, len(hexStr)/2)
	for i := 0; i+1 < len(hexStr); i += 2 {
		n, err := strconv.ParseUint(hexStr[i:i+2], 16, 8)
		if err != nil {
			return ""
		}
		raw = append(raw, byte(n))
	}
	if len(raw) < 2 {
		return ""
	}
	key := raw[0]
	out := make([]byte, 0, len(raw)-1)
	for _, b := range raw[1:] {
		out = append(out, b^key)
	}
	return string(out)
}

// formatItemFilename merges the page title with the CDN file name, matching
// the original Python heuristic.
func formatItemFilename(original, fromURL string) string {
	if original == "" {
		return fromURL
	}
	if fromURL == "" || original == fromURL {
		return original
	}
	origBase := strings.TrimSuffix(path.Base(original), path.Ext(original))
	urlBase := strings.TrimSuffix(fromURL, path.Ext(fromURL))

	// Prefer the page name's extension; fall back to the CDN one when the page
	// title carries no extension at all.
	ext := path.Ext(original)
	if ext == "" {
		ext = path.Ext(fromURL)
	}

	// The CDN name adds no information beyond the page title, so keep the
	// page title: it is the only side that is known to carry the extension.
	if urlBase == origBase {
		return original
	}
	// Bunkr prefixes the CDN name with the page title plus a separator when it
	// has to disambiguate; in that case the CDN name is the authoritative one.
	if strings.HasPrefix(urlBase, origBase+"-") ||
		strings.HasPrefix(urlBase, origBase+"_") {
		return fromURL
	}
	clean := RemoveInvalidCharacters(origBase)
	if clean == "" {
		return fromURL
	}
	return clean + "-" + urlBase + ext
}

// extractItemDates reads the per-entry upload dates of an album listing,
// preserving document order so they can be zipped with the item URLs.
func extractItemDates(doc *Document) []*time.Time {
	if doc == nil {
		return nil
	}
	out := make([]*time.Time, 0, 32)
	doc.Find(".theDate").Each(func(_ int, s *goquery.Selection) {
		out = append(out, parseDateString(strings.TrimSpace(s.Text())))
	})
	return out
}

// itemDateFromDoc finds an item's upload date from several common patterns.
func itemDateFromDoc(doc *Document) *time.Time {
	if doc == nil {
		return nil
	}
	if v, ok := doc.Find("time").First().Attr("datetime"); ok {
		if d := parseDateString(v); d != nil {
			return d
		}
	}
	var found *time.Time
	doc.Find(`[title]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		title, _ := s.Attr("title")
		text := strings.ToLower(s.Text())
		if !strings.Contains(text, "ago") && !strings.Contains(text, "yesterday") && !strings.Contains(text, "today") {
			return true
		}
		if d := parseDateString(title); d != nil {
			found = d
			return false
		}
		return true
	})
	return found
}

// dateLayouts are the formats observed in the wild, tried in order.
var dateLayouts = []string{
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"02/01/2006",
	"02 Jan 2006",
	"Jan 02, 2006",
	"January 02, 2006",
	"15:04:05 02/01/2006",
}

// parseDateString is a best-effort parser returning a UTC timestamp.
func parseDateString(text string) *time.Time {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	// RFC3339 with a trailing Z.
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		u := t.UTC()
		return &u
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, text); err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}

// absolute joins a relative href against a host page.
func absolute(host, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if !strings.HasPrefix(href, "/") {
		return ""
	}
	return host + href
}

// ---------------------------------------------------------------- link signing

// signResponse is the payload of the Bunkr signing API.
type signResponse struct {
	Token string `json:"token"`
	Ex    string `json:"ex"`
}

// downloadResponse is the payload of the direct download API.
type downloadResponse struct {
	MediaFiles string `json:"mediafiles"`
	Path       string `json:"path"`
}

// ResolveDownloadLink signs the CDN URL for an item page.
//
// Pipeline (identical to the original implementation):
//  1. read the jsCDN variable from the inline script
//  2. if absent, ask the download API for an unsigned URL
//  3. build the media path and exchange it for a signed token
func (c *Crawler) ResolveDownloadLink(ctx context.Context, itemURL string, doc *Document) (string, error) {
	vars := ExtractPageVars(doc)
	cdnURL := vars["jsCDN"]

	var unsigned string
	if cdnURL == "" {
		if fileID := ExtractFileID(doc); fileID != "" {
			unsigned, _ = c.GetUnsignedURL(ctx, fileID)
		}
	}
	if cdnURL == "" && unsigned == "" {
		return "", fmt.Errorf("could not resolve download URL for %s", itemURL)
	}
	if cdnURL != "" {
		cdnURL = UnescapeJSPath(cdnURL)
	}

	mediaSlug := path.Base(mustParsePath(unsigned))
	if mediaSlug == "." || mediaSlug == "/" {
		mediaSlug = path.Base(mustParsePath(itemURL))
	}

	mediaPath := mustParsePath(cdnURL)
	if cdnURL == "" {
		mediaPath = "/storage/media/" + mediaSlug
	}

	var signed signResponse
	err := c.hc.GetJSON(ctx, c.endpoints.SignAPI, url.Values{"path": {mediaPath}}, &signed)
	if err != nil {
		return "", fmt.Errorf("sign %s: %w", itemURL, err)
	}
	if signed.Token != "" && signed.Ex != "" {
		base := cdnURL
		if base == "" {
			base = unsigned
		}
		return base + "?token=" + signed.Token + "&ex=" + signed.Ex, nil
	}
	if cdnURL != "" {
		return cdnURL, nil
	}
	return unsigned, nil
}

// GetUnsignedURL asks the download API for an unsigned media URL.
func (c *Crawler) GetUnsignedURL(ctx context.Context, fileID string) (string, error) {
	var data downloadResponse
	if err := c.hc.PostJSON(ctx, c.endpoints.DownloadAPI, map[string]string{"id": fileID}, &data); err != nil {
		return "", err
	}
	if data.MediaFiles == "" || data.Path == "" {
		return "", nil
	}
	u, err := url.Parse(data.MediaFiles)
	if err != nil {
		return "", err
	}
	u.Path = data.Path
	return u.String(), nil
}

// ExtractPageVars reads `var jsCDN = ...;`-style assignments from inline scripts.
func ExtractPageVars(doc *Document) map[string]string {
	out := map[string]string{}
	if doc == nil {
		return out
	}
	found := false
	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		text := s.Text()
		if !strings.Contains(text, "var jsCDN") {
			return true
		}
		found = true
		for _, m := range jsVarsRe.FindAllStringSubmatch(text, -1) {
			out[m[1]] = strings.Trim(unescapeJSPath(m[2]), `"'`)
		}
		return false
	})
	if found {
		return out
	}
	// Some pages only expose the variables in a different script; scan them all.
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		for _, m := range jsVarsRe.FindAllStringSubmatch(s.Text(), -1) {
			if _, exists := out[m[1]]; !exists {
				out[m[1]] = strings.Trim(unescapeJSPath(m[2]), `"'`)
			}
		}
	})
	return out
}

// ExtractFileID reads the `data-file-id` marker from the first script tag.
func ExtractFileID(doc *Document) string {
	return doc.Attribute("script", "data-file-id")
}

// UnescapeJSPath normalises JS-escaped URL fragments.
func UnescapeJSPath(v string) string {
	v = strings.ReplaceAll(v, `\/`, "/")
	v = strings.ReplaceAll(v, `\\`, `\`)
	return decodeJSUescapes(v)
}

func unescapeJSPath(v string) string { return UnescapeJSPath(v) }

func mustParsePath(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

// textWithDecryptedEmails renders a node's text, transparently decrypting any
// Cloudflare email-protection spans it contains.
func textWithDecryptedEmails(node *goquery.Selection) string {
	if node == nil || node.Length() == 0 {
		return ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "span" {
				if hex, ok := attrValue(n, "data-cfemail"); ok {
					if mail := DecryptCFEmail(hex); mail != "" {
						sb.WriteString(mail)
						return
					}
					return
				}
			}
			if n.Data == "script" || n.Data == "style" {
				return
			}
		}
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node.Nodes[0])
	return sb.String()
}

func attrValue(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}
