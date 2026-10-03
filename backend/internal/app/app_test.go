package app

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/nadun96/quizplatform/internal/platform/config"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func TestHealthz(t *testing.T) {
	pool := dbtest.New(t)
	a, err := Build(config.Config{BaseURL: "http://localhost"}, slog.New(slog.NewTextHandler(io.Discard, nil)), pool, false)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("healthz = %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
}
