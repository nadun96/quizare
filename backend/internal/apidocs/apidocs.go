// Package apidocs serves the OpenAPI 3.1 description of the HTTP API and an
// embedded Swagger UI at /api/docs. Everything is compiled into the binary;
// no CDN is contacted, and no inline script is used (ADR-16 CSP).
package apidocs

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"go.yaml.in/yaml/v3"
)

//go:embed openapi.yaml
var specYAML []byte

//go:embed ui
var uiFS embed.FS

// Spec returns the raw OpenAPI document.
func Spec() []byte { return specYAML }

var (
	jsonOnce sync.Once
	specJSON []byte
	jsonErr  error
)

// SpecJSON returns the document converted to JSON (for tools that want it).
func SpecJSON() ([]byte, error) {
	jsonOnce.Do(func() {
		var doc any
		if jsonErr = yaml.Unmarshal(specYAML, &doc); jsonErr == nil {
			specJSON, jsonErr = json.Marshal(doc)
		}
	})
	return specJSON, jsonErr
}

const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Classroom Quiz API</title>
<link rel="icon" href="/api/docs/ui/favicon-32x32.png">
<link rel="stylesheet" href="/api/docs/ui/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="/api/docs/ui/swagger-ui-bundle.js"></script>
<script src="/api/docs/ui/init.js"></script>
</body>
</html>`

// Routes mounts the docs under the router it is given (e.g. /api/docs).
func Routes(r chi.Router) {
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(page))
	})
	r.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(specYAML)
	})
	r.Get("/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		b, err := SpecJSON()
		if err != nil {
			http.Error(w, "spec unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	sub, _ := fs.Sub(uiFS, "ui")
	files := http.StripPrefix("/api/docs/ui/", http.FileServer(http.FS(sub)))
	r.Get("/ui/*", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		files.ServeHTTP(w, r)
	})
}
