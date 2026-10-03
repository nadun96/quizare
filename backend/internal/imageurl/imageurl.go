// Package imageurl normalises and validates teacher-supplied resource URLs
// (ADR-10). The platform stores URLs only, never files (BR-15).
package imageurl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const MaxBytes = 5 << 20 // ADR-10: size limit of 5 MB

var driveIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{10,}$`)

// Normalise validates the URL and converts Google Drive share links into the
// thumbnail form, because the legacy uc?export=view embedding has returned
// 403 since January 2024 (ADR-10, BA §10.4).
func Normalise(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("URL is required")
	}
	if len(raw) > 2048 {
		return "", errors.New("URL is too long")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("not a valid URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("URL must start with https:// or http://")
	}
	if u.User != nil {
		return "", errors.New("URL must not contain credentials")
	}
	if id := DriveFileID(u); id != "" {
		return "https://drive.google.com/thumbnail?id=" + id + "&sz=w1000", nil
	}
	return u.String(), nil
}

// DriveFileID extracts a Google Drive file id from common share-link forms.
func DriveFileID(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if host != "drive.google.com" && host != "docs.google.com" {
		return ""
	}
	var id string
	switch {
	case strings.HasPrefix(u.Path, "/file/d/"):
		// /file/d/{ID}/view, /file/d/{ID}/preview, /file/d/{ID}
		id = strings.SplitN(strings.TrimPrefix(u.Path, "/file/d/"), "/", 2)[0]
	case u.Path == "/open", u.Path == "/uc", u.Path == "/thumbnail":
		id = u.Query().Get("id")
	}
	if !driveIDRe.MatchString(id) {
		return ""
	}
	return id
}

// Result of checking a URL.
type Result struct {
	OK          bool   `json:"ok"`
	ContentType string `json:"content_type,omitempty"`
	Bytes       int64  `json:"bytes,omitempty"`
	Message     string `json:"message,omitempty"`
}

// Checker fetches URLs with SSRF safeguards (ADR-10): private, loopback and
// link-local addresses are refused after DNS resolution, responses are size-
// and type-limited, there is a timeout, and cookies are never sent.
type Checker struct {
	client *http.Client
}

// NewChecker builds a checker. allowPrivate is for tests against local servers only.
func NewChecker(allowPrivate bool) *Checker {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || !IsPublic(ip) {
				return fmt.Errorf("address %s is not public", host)
			}
			return nil
		}
	}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		MaxIdleConns:          10,
		Proxy:                 nil, // never route through an environment proxy
	}
	return &Checker{client: &http.Client{
		Transport: tr,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return errors.New("redirect to a non-HTTP URL")
			}
			return nil
		},
	}}
}

// IsPublic reports whether ip is a globally routable unicast address.
func IsPublic(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() ||
		// 100.64.0.0/10 carrier-grade NAT and 0.0.0.0/8 are not public either.
		(ip.To4() != nil && (ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64 || ip.To4()[0] == 0)))
}

// Check fetches url and verifies it is a publicly reachable image (FR-QZ-11).
func (c *Checker) Check(ctx context.Context, rawURL string) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{Message: "not a valid URL"}
	}
	req.Header.Set("User-Agent", "QuizPlatform-LinkCheck/1.0")
	req.Header.Set("Accept", "image/*")
	resp, err := c.client.Do(req)
	if err != nil {
		return Result{Message: "could not reach the link: " + shortErr(err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return Result{Message: "the link is not public (sharing must be 'Anyone with the link')"}
	}
	if resp.StatusCode >= 400 {
		return Result{Message: fmt.Sprintf("the link returned HTTP %d", resp.StatusCode)}
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if !strings.HasPrefix(ct, "image/") {
		return Result{ContentType: ct, Message: "the link does not point to an image (got " + orUnknown(ct) + ")"}
	}
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return Result{ContentType: ct, Message: "download failed: " + shortErr(err)}
	}
	if n > MaxBytes {
		return Result{ContentType: ct, Bytes: n, Message: "image is larger than 5 MB"}
	}
	return Result{OK: true, ContentType: ct, Bytes: n}
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown type"
	}
	return s
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
