package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerRendersTypedError(t *testing.T) {
	h := Handler(func(w http.ResponseWriter, r *http.Request) error { return Conflict("taken") })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"code":"conflict"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestHandlerHidesUnknownErrors(t *testing.T) {
	h := Handler(func(w http.ResponseWriter, r *http.Request) error { return errors.New("db password is hunter2") })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("leaked internal error: %d %s", rec.Code, rec.Body)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	var dst struct{ A int }
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"A":1,"B":2}`))
	if err := Decode(httptest.NewRecorder(), req, &dst); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestRecover(t *testing.T) {
	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 {
		t.Fatalf("got %d", rec.Code)
	}
}
