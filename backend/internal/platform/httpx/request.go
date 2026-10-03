package httpx

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ClientIP returns the caller's address. X-Forwarded-For is trusted only when
// the direct peer is loopback, i.e. Caddy on the same host (ADR-12).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return host
}

// CSRFHeader is the custom header the SPA sends on every state-changing
// request. Cross-site forms cannot set it, and cross-site fetch cannot set it
// without a CORS preflight that the server never grants (ADR-13).
const CSRFHeader = "X-Requested-With"

// SameOrigin rejects state-changing requests that do not come from our own
// origin. Safe methods pass through.
func SameOrigin(baseURL string) func(http.Handler) http.Handler {
	want := originOf(baseURL)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if !OriginAllowed(r, want) || r.Header.Get(CSRFHeader) == "" {
				JSON(w, http.StatusForbidden, NewError(http.StatusForbidden, "csrf", "cross-site request rejected"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// OriginAllowed checks the Origin header (or Sec-Fetch-Site when a browser
// omits Origin) against the expected origin.
func OriginAllowed(r *http.Request, want string) bool {
	if o := r.Header.Get("Origin"); o != "" {
		return o == want
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Scheme + "://" + u.Host
}

// Origin returns scheme://host of a base URL.
func Origin(raw string) string { return originOf(raw) }
