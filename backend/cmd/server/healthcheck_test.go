package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthURL(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0:8080":   "http://127.0.0.1:8080/healthz",
		":9000":          "http://127.0.0.1:9000/healthz",
		"[::]:8080":      "http://127.0.0.1:8080/healthz",
		"127.0.0.1:8080": "http://127.0.0.1:8080/healthz",
		"10.0.0.5:81":    "http://10.0.0.5:81/healthz",
		"garbage":        "http://127.0.0.1:8080/healthz",
	} {
		if got := healthURL(in); got != want {
			t.Errorf("healthURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	t.Setenv("QP_LISTEN", "0.0.0.0:"+port)
	if code := healthcheck(); code != 0 {
		t.Fatalf("healthy server: exit %d", code)
	}
	status = http.StatusServiceUnavailable
	if code := healthcheck(); code != 1 {
		t.Fatalf("unhealthy server: exit %d", code)
	}
	srv.Close()
	if code := healthcheck(); code != 1 {
		t.Fatalf("no server: exit %d", code)
	}
}
