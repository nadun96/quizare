package httpx_test

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Search engines see the public pages only, at the site's address, and
// nothing at all on a private install (D-114).
func TestSearchEngines(t *testing.T) {
	read := func(h func(w *httptest.ResponseRecorder)) string {
		w := httptest.NewRecorder()
		h(w)
		b, _ := io.ReadAll(w.Result().Body)
		return string(b)
	}
	on := read(func(w *httptest.ResponseRecorder) { httpx.Robots("https://quiz.school.lk/", true)(w, httptest.NewRequest("GET", "/robots.txt", nil)) })
	for _, want := range []string{"Disallow: /api/", "Disallow: /t/", "Disallow: /j/", "Allow: /", "Sitemap: https://quiz.school.lk/sitemap.xml"} {
		if !strings.Contains(on, want) {
			t.Errorf("robots.txt lacks %q:\n%s", want, on)
		}
	}
	off := read(func(w *httptest.ResponseRecorder) { httpx.Robots("https://quiz.school.lk", false)(w, httptest.NewRequest("GET", "/robots.txt", nil)) })
	if !strings.Contains(off, "Disallow: /\n") || strings.Contains(off, "Sitemap") {
		t.Errorf("private robots.txt = %s", off)
	}
	sm := read(func(w *httptest.ResponseRecorder) {
		httpx.Sitemap("https://quiz.school.lk", true, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))(w, httptest.NewRequest("GET", "/sitemap.xml", nil))
	})
	if !strings.Contains(sm, "<loc>https://quiz.school.lk/</loc><lastmod>2026-11-01</lastmod><priority>1.0</priority>") || !strings.Contains(sm, "<loc>https://quiz.school.lk/join</loc>") || strings.Contains(sm, "/t/") {
		t.Errorf("sitemap = %s", sm)
	}

	// The page carries the site's address and the robots rule.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<link rel="canonical" href="__SITE_URL__/"><meta name="robots" content="__ROBOTS__">`), 0o644); err != nil {
		t.Fatal(err)
	}
	spa := httpx.SPA(dir, "__SITE_URL__", "https://quiz.school.lk", "__ROBOTS__", "index, follow")
	for _, p := range []string{"/", "/index.html", "/dashboard"} {
		w := httptest.NewRecorder()
		spa.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if got := w.Body.String(); !strings.Contains(got, `href="https://quiz.school.lk/"`) || !strings.Contains(got, `content="index, follow"`) {
			t.Errorf("%s = %s", p, got)
		}
	}
}
