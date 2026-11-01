package httpx

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SPA serves a static single-page app build: real files as-is (hashed assets
// cached for a year), everything else falls back to index.html (no-cache).
// Production puts Caddy in front instead (ADR-11, ADR-12); this is for
// development and single-binary setups.
//
// vars are pairs of placeholder and value filled into index.html as it's
// served (D-114): the site's address for search engines and link previews,
// which a static build can't know.
func SPA(dir string, vars ...string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	fill := strings.NewReplacer(vars...)
	index := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		page, err := os.ReadFile(filepath.Join(dir, "index.html"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fill.Replace(string(page))))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
		if p == "/index.html" {
			index(w, r)
			return
		}
		full := filepath.Join(dir, filepath.FromSlash(p))
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			if strings.HasPrefix(p, "/_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			// Module scripts (pdf.js's worker is .mjs) must be JavaScript, whatever
			// the system's MIME table says (Windows' may say text/plain).
			if ext := path.Ext(p); ext == ".js" || ext == ".mjs" {
				w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			}
			fs.ServeHTTP(w, r)
			return
		}
		index(w, r)
	})
}
