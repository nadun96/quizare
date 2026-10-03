package imageurl

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormaliseDriveLinks(t *testing.T) {
	const id = "1AbCdEfGhIjKlMnOp_q-rS"
	want := "https://drive.google.com/thumbnail?id=" + id + "&sz=w1000"
	for _, in := range []string{
		"https://drive.google.com/file/d/" + id + "/view?usp=sharing",
		"https://drive.google.com/file/d/" + id + "/preview",
		"https://drive.google.com/open?id=" + id,
		"https://drive.google.com/uc?export=view&id=" + id, // legacy form, broken since Jan 2024
		"https://docs.google.com/uc?id=" + id,
		"  https://drive.google.com/uc?id=" + id + "  ",
	} {
		got, err := Normalise(in)
		if err != nil || got != want {
			t.Errorf("Normalise(%q) = %q, %v", in, got, err)
		}
	}
	if got, _ := Normalise("https://example.com/a.png?x=1"); got != "https://example.com/a.png?x=1" {
		t.Errorf("non-Drive URL changed: %s", got)
	}
}

func TestNormaliseRejectsUnsafeURLs(t *testing.T) {
	for _, in := range []string{"", "javascript:alert(1)", "data:image/png;base64,AAA", "ftp://x/y.png",
		"https://user:pw@example.com/a.png", "/relative.png", "https://" + strings.Repeat("a", 2100)} {
		if _, err := Normalise(in); err == nil {
			t.Errorf("Normalise(%q) accepted", in)
		}
	}
}

func TestIsPublic(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "192.168.0.1", "172.16.5.4", "169.254.169.254", "::1", "fe80::1", "0.0.0.0", "100.64.0.1", "fc00::1"} {
		if IsPublic(net.ParseIP(s)) {
			t.Errorf("%s treated as public", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "142.250.72.14", "2606:4700::1111"} {
		if !IsPublic(net.ParseIP(s)) {
			t.Errorf("%s treated as private", s)
		}
	}
}

func TestCheckerBlocksPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("png"))
	}))
	defer srv.Close()
	if r := NewChecker(false).Check(context.Background(), srv.URL); r.OK {
		t.Fatal("SSRF: loopback address fetched")
	}
}

func TestCheckerValidatesImages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok.png", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("cookies must never be forwarded")
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(make([]byte, 100))
	})
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>"))
	})
	mux.HandleFunc("/private", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	mux.HandleFunc("/huge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(make([]byte, MaxBytes+10))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewChecker(true)
	ctx := context.Background()
	if r := c.Check(ctx, srv.URL+"/ok.png"); !r.OK || r.Bytes != 100 {
		t.Errorf("ok.png: %+v", r)
	}
	if r := c.Check(ctx, srv.URL+"/page"); r.OK || !strings.Contains(r.Message, "not point to an image") {
		t.Errorf("page: %+v", r)
	}
	if r := c.Check(ctx, srv.URL+"/private"); r.OK || !strings.Contains(r.Message, "not public") {
		t.Errorf("private: %+v", r)
	}
	if r := c.Check(ctx, srv.URL+"/huge"); r.OK || !strings.Contains(r.Message, "5 MB") {
		t.Errorf("huge: %+v", r)
	}
}
