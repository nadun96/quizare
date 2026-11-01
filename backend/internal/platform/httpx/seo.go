package httpx

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Search engines (D-114). Only the public pages are listed: the home page
// and the pages for joining, logging in and signing up. Everything behind a
// login, every join code and the API stay out. With indexing off (a private
// school install, QP_SEARCH_INDEX=0) robots.txt turns every crawler away.

// PublicPages are the pages search engines may list, in the sitemap's order.
var PublicPages = []string{"/", "/join", "/register", "/login"}

// private are the paths robots.txt keeps out even with indexing on.
var private = []string{"/api/", "/ws/", "/internal/", "/admin", "/t/", "/my", "/dashboard", "/inbox", "/account", "/attempt/", "/learn/", "/live/",
	"/r/", "/m/", "/j/", "/c/", "/p/", "/s/", "/slides/", "/tutor/", "/tutoring", "/docs", "/verify-email", "/reset-password"}

// Robots serves /robots.txt.
func Robots(baseURL string, index bool) http.HandlerFunc {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	if !index {
		b.WriteString("Disallow: /\n")
	} else {
		for _, p := range private {
			fmt.Fprintf(&b, "Disallow: %s\n", p)
		}
		b.WriteString("Allow: /\n\n")
		fmt.Fprintf(&b, "Sitemap: %s/sitemap.xml\n", strings.TrimRight(baseURL, "/"))
	}
	body := b.String()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte(body))
	}
}

// Sitemap serves /sitemap.xml: the public pages, or none with indexing off.
func Sitemap(baseURL string, index bool, built time.Time) http.HandlerFunc {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	if index {
		base := strings.TrimRight(baseURL, "/")
		for i, p := range PublicPages {
			prio := "0.5"
			if i == 0 {
				prio = "1.0"
			}
			fmt.Fprintf(&b, "  <url><loc>%s%s</loc><lastmod>%s</lastmod><priority>%s</priority></url>\n", base, p, built.UTC().Format("2006-01-02"), prio)
		}
	}
	b.WriteString("</urlset>\n")
	body := b.String()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte(body))
	}
}
