// Package web embeds the built single-page application.
//
// The real assets are produced by `npm install && npm run build` in frontend/
// and copied into this directory by `make web` (and by the Dockerfile) before
// `go build`. Only .gitkeep is committed, so a fresh checkout still compiles and
// serves a helpful placeholder page instead of failing.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the built SPA rooted at its index directory.
func FS() (http.FileSystem, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}

// PlaceholderHTML is served when no real frontend build is embedded.
const PlaceholderHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>BunkrDownloader</title>
<style>
  :root{color-scheme:dark}
  body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0b0e14;color:#e6e9f0;
       font-family:system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif}
  .card{max-width:34rem;padding:2.5rem;background:#151922;border:1px solid rgba(255,255,255,.08);
        border-radius:16px;box-shadow:0 20px 60px rgba(0,0,0,.45)}
  h1{margin:0 0 .75rem;font-size:1.4rem;letter-spacing:-.01em}
  p{margin:.45rem 0;color:#98a2b8;line-height:1.75;font-size:.94rem}
  code{background:#1f2635;padding:.15rem .45rem;border-radius:6px;font-size:.86em;color:#a5b4fc}
  a{color:#7c9cff}
  .grad{background:linear-gradient(90deg,#7c5cff,#4f8cff,#22d3ee);
        -webkit-background-clip:text;background-clip:text;color:transparent}
</style></head>
<body><div class="card">
  <h1><span class="grad">BunkrDownloader</span> · 后端已就绪</h1>
  <p>API 正常运行，但前端静态资源尚未构建。</p>
  <p>请在 <code>frontend/</code> 目录执行：</p>
  <p><code>npm install &amp;&amp; npm run build</code></p>
  <p>或使用 <code>make build</code>（= web + go build）一步完成。</p>
  <p>接口自检：<a href="/api/health">/api/health</a></p>
</div></body></html>`

// HasBuild reports whether a real frontend bundle is embedded, i.e. whether
// dist/index.html references hashed assets under ./assets/.
func HasBuild() bool {
	b, err := distFS.ReadFile("dist/index.html")
	if err != nil {
		return false
	}
	return len(b) > 0 && contains(string(b), "./assets/")
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
