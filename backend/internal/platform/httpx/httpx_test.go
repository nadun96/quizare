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

func TestClientIPTrustsOnlyConfiguredProxies(t *testing.T) {
	t.Cleanup(func() { trusted = nil })
	req := func(remote, xff string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	cases := []struct{ name, remote, xff, want string }{
		{"direct client", "203.0.113.5:4000", "", "203.0.113.5"},
		{"direct client can't spoof", "203.0.113.5:4000", "198.51.100.1", "203.0.113.5"},
		{"loopback proxy", "127.0.0.1:4000", "198.51.100.1", "198.51.100.1"},
		{"loopback proxy, spoofed entry ignored", "127.0.0.1:4000", "1.2.3.4, 198.51.100.1", "198.51.100.1"},
		{"untrusted container peer", "172.30.0.10:4000", "198.51.100.1", "172.30.0.10"},
		{"loopback proxy without header", "[::1]:4000", "", "::1"},
	}
	for _, c := range cases {
		if got := ClientIP(req(c.remote, c.xff)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	if err := TrustProxies([]string{"172.30.0.10", "10.8.0.0/16"}); err != nil {
		t.Fatal(err)
	}
	cases = []struct{ name, remote, xff, want string }{
		{"trusted container proxy", "172.30.0.10:4000", "198.51.100.1", "198.51.100.1"},
		{"chain of trusted proxies", "172.30.0.10:4000", "1.2.3.4, 198.51.100.1, 10.8.3.4", "198.51.100.1"},
		{"neighbour in the subnet isn't trusted", "172.30.0.11:4000", "198.51.100.1", "172.30.0.11"},
		{"only proxies in the header", "172.30.0.10:4000", "10.8.0.1", "10.8.0.1"},
	}
	for _, c := range cases {
		if got := ClientIP(req(c.remote, c.xff)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseProxiesRejectsJunk(t *testing.T) {
	for _, bad := range []string{"caddy", "300.1.1.1", "10.0.0.0/99"} {
		if _, err := ParseProxies([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	nets, err := ParseProxies([]string{" 172.30.0.10 ", "", "fd00::/8"})
	if err != nil || len(nets) != 2 {
		t.Fatalf("got %v, %v", nets, err)
	}
}
