package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// trusted holds the proxies whose X-Forwarded-For is believed. Loopback is
// always trusted (Caddy on the same host, ADR-12); TrustProxies adds more,
// e.g. Caddy's container address in compose (D-51).
var trusted []*net.IPNet

// TrustProxies sets the extra proxy addresses (IPs or CIDRs) whose
// X-Forwarded-For header is trusted. Call it once at startup.
func TrustProxies(list []string) error {
	nets, err := ParseProxies(list)
	if err != nil {
		return err
	}
	trusted = nets
	return nil
}

// ParseProxies parses IPs ("172.30.0.10") and CIDRs ("10.0.0.0/8").
func ParseProxies(list []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("trusted proxy %q is not an IP or CIDR", s)
			}
			bits := 128
			if ip.To4() != nil {
				ip, bits = ip.To4(), 32
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is not an IP or CIDR", s)
		}
		nets = append(nets, n)
	}
	return nets, nil
}

func isTrusted(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the caller's address. X-Forwarded-For is believed only
// when the direct peer is a trusted proxy; the client is then the rightmost
// address in it that isn't one of our proxies, so a client can't pick its own
// address by sending the header itself.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !isTrusted(host) {
		return host
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p == "" {
			continue
		}
		if !isTrusted(p) {
			return p
		}
		host = p
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
