package httpx

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPA(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644)
	os.MkdirAll(filepath.Join(dir, "_app", "immutable"), 0o755)
	os.WriteFile(filepath.Join(dir, "_app", "immutable", "x.js"), []byte("js"), 0o644)
	h := SPA(dir)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/_app/immutable/x.js", nil))
	if rec.Body.String() != "js" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %q %v", rec.Body, rec.Header())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/t/quizzes/123", nil))
	if !strings.Contains(rec.Body.String(), "app") || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("fallback: %q", rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/../spa_test_secret", nil))
	// Either the SPA fallback or net/http's 400 for ".." is fine; never a file outside dir.
	if rec.Code != 400 && !strings.Contains(rec.Body.String(), "app") {
		t.Fatalf("path traversal not contained: %d %q", rec.Code, rec.Body)
	}
}
