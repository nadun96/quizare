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
func SPA(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
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
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}
