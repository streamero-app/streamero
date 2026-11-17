// Package webui serves the embedded web client (built by web/ into dist/).
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves static assets and falls back to index.html for client-side routes.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return FromFS(sub)
}

// FromFS serves a built web client from sub (tests use a small in-memory one).
func FromFS(sub fs.FS) http.Handler {
	files := http.FileServer(http.FS(sub))
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if notAPage(w, r, strings.TrimPrefix(path.Clean(r.URL.Path), "/")) {
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(notBuilt))
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			if notAPage(w, r, p) {
				return
			}
			// Unknown path: let the SPA router handle it.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			p = "index.html"
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

// notAPage answers the paths that are never the web client: robots.txt (a private
// media server has nothing for crawlers) and what scanners look for (.env, .git,
// *.php…), where answering 200 with the app would mark the server as a hit.
func notAPage(w http.ResponseWriter, r *http.Request, p string) bool {
	switch {
	case p == "robots.txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
	case probe(p):
		http.NotFound(w, r)
	default:
		return false
	}
	return true
}

// probeExt are file types the web client never serves and scanners ask for.
var probeExt = map[string]bool{".php": true, ".asp": true, ".aspx": true, ".jsp": true, ".cgi": true, ".pl": true,
	".env": true, ".sql": true, ".bak": true, ".old": true, ".zip": true, ".tar": true, ".gz": true, ".tgz": true, ".rar": true,
	".7z": true, ".ini": true, ".conf": true, ".cfg": true, ".yml": true, ".yaml": true, ".log": true, ".sh": true, ".pem": true,
	".key": true, ".db": true, ".sqlite": true, ".xml": true}

// probe reports a path no web client route uses: a dot-file or dot-folder
// (except /.well-known/) or a file type from probeExt.
func probe(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") && seg != ".well-known" {
			return true
		}
	}
	return probeExt[strings.ToLower(path.Ext(p))]
}

const notBuilt = `<!doctype html><html><head><meta charset="utf-8"><title>Streamero</title></head>` +
	`<body>Web client not built. Run <code>cd web &amp;&amp; pnpm build</code>.</body></html>`
