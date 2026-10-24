package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

const (
	// The browser holds the state of a sign-in in progress; only this path sees it.
	stateCookie = "qp_oidc"
	cookiePath  = "/api/auth/oidc"
	// How long someone has at the provider before the sign-in must start over.
	loginTTL = 10 * time.Minute
	// How long a provider's discovery document and keys are reused.
	discoveryTTL = time.Hour
)

// Service runs sign-ins with the providers.
type Service struct {
	store   *Store
	pool    *pgxpool.Pool
	auth    *auth.Service
	baseURL string
	client  *http.Client
	log     *slog.Logger
	now     func() time.Time

	mu        sync.Mutex
	discovery map[string]cached
}

type cached struct {
	p  *gooidc.Provider
	at time.Time
}

// NewService: client is the HTTP client for talking to providers (nil: a
// default with a timeout; tests pass one that trusts their fake provider).
func NewService(pool *pgxpool.Pool, vault Sealer, a *auth.Service, baseURL string, client *http.Client, log *slog.Logger) *Service {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	s := &Service{store: &Store{pool: pool, vault: vault}, pool: pool, auth: a, baseURL: strings.TrimRight(baseURL, "/"),
		client: client, log: log, now: time.Now, discovery: map[string]cached{}}
	a.SetSSO(s.Options)
	return s
}

// RedirectURL is the one callback address every provider is given.
func (s *Service) RedirectURL() string { return s.baseURL + cookiePath + "/callback" }

// Options lists the enabled providers for the login page.
func (s *Service) Options(ctx context.Context) []auth.SSOOption {
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM oidc.providers WHERE enabled ORDER BY position, lower(name)`)
	if err != nil {
		s.log.Warn("listing sign-in providers", "err", err)
		return []auth.SSOOption{}
	}
	out, _ := pgx.CollectRows(rows, func(r pgx.CollectableRow) (auth.SSOOption, error) {
		var o auth.SSOOption
		return o, r.Scan(&o.ID, &o.Name)
	})
	if out == nil {
		out = []auth.SSOOption{}
	}
	return out
}

func (s *Service) ctx(ctx context.Context) context.Context {
	return gooidc.ClientContext(ctx, s.client)
}

// provider fetches (or reuses) the issuer's discovery document.
func (s *Service) provider(ctx context.Context, issuer string) (*gooidc.Provider, error) {
	s.mu.Lock()
	c, ok := s.discovery[issuer]
	s.mu.Unlock()
	if ok && s.now().Sub(c.at) < discoveryTTL {
		return c.p, nil
	}
	p, err := gooidc.NewProvider(s.ctx(ctx), issuer)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.discovery[issuer] = cached{p: p, at: s.now()}
	s.mu.Unlock()
	return p, nil
}

func (s *Service) forget(issuer string) {
	s.mu.Lock()
	delete(s.discovery, issuer)
	s.mu.Unlock()
}

func random(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// localPath keeps "next" on this site: a path, never another origin.
func localPath(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") || strings.ContainsAny(next, "\r\n") {
		return "/"
	}
	return next
}

// start sends the browser to the provider.
func (s *Service) start(w http.ResponseWriter, r *http.Request, linkUser string) error {
	ctx := r.Context()
	p, secret, err := s.store.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	if !p.Enabled {
		return httpx.ErrNotFound
	}
	op, err := s.provider(ctx, p.Issuer)
	if err != nil {
		s.log.Warn("sign-in provider unreachable", "provider", p.Name, "err", err)
		return httpx.NewError(http.StatusBadGateway, "sso_unavailable", p.Name+" can't be reached right now; try again shortly")
	}
	state, nonce, verifier := random(32), random(24), oauth2.GenerateVerifier()
	var link *string
	if linkUser != "" {
		link = &linkUser
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM oidc.logins WHERE created_at < $1`, s.now().Add(-loginTTL)); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO oidc.logins(state_hash, provider_id, nonce, verifier, next, link_user, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		hash(state), p.ID, nonce, verifier, localPath(r.URL.Query().Get("next")), link, s.now()); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: state, Path: cookiePath, MaxAge: int(loginTTL.Seconds()),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}) // Lax: it must come back on the provider's redirect
	cfg := s.oauth(p, secret, op)
	http.Redirect(w, r, cfg.AuthCodeURL(state, gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
	return nil
}

func (s *Service) oauth(p Provider, secret string, op *gooidc.Provider) *oauth2.Config {
	return &oauth2.Config{ClientID: p.ClientID, ClientSecret: secret, Endpoint: op.Endpoint(), RedirectURL: s.RedirectURL(), Scopes: p.Scopes}
}

// ssoError is a reason the browser is sent back with; the page explains the code.
type ssoError struct{ code string }

func (e ssoError) Error() string { return e.code }

func fail(code string) error { return ssoError{code} }

// callback finishes a sign-in. It always ends in a redirect: to where the
// person was going, or back to the login or account page with a reason.
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	back := "/login"
	next, err := s.finish(w, r, &back)
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: cookiePath, MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	if err != nil {
		code := "sso_failed"
		var se ssoError
		var he *httpx.Error
		switch {
		case errors.As(err, &se):
			code = se.code
		case errors.As(err, &he):
			code = he.Code
		default:
			s.log.Warn("sign-in with a provider failed", "err", err, "provider_error", q.Get("error"))
		}
		http.Redirect(w, r, back+"?sso_error="+url.QueryEscape(code), http.StatusFound)
		return
	}
	http.Redirect(w, r, next, http.StatusFound)
}

type claims map[string]any

func (c claims) str(k string) string {
	v, _ := c[k].(string)
	return strings.TrimSpace(v)
}

// values reads a claim that may be one string or a list of them.
func (c claims) values(k string) []string {
	switch v := c[k].(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func (c claims) verified() bool {
	switch v := c["email_verified"].(type) {
	case bool:
		return v
	case string:
		return v == "true" // some providers send it as text
	}
	return false
}

func (s *Service) finish(w http.ResponseWriter, r *http.Request, back *string) (string, error) {
	ctx := r.Context()
	q := r.URL.Query()
	cookie, err := r.Cookie(stateCookie)
	if err != nil || q.Get("state") == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(q.Get("state"))) != 1 {
		// Another browser, a second tab, or a link forged to sign someone in as someone else.
		return "", fail("sso_expired")
	}
	var providerID, nonce, verifier, next string
	var linkUser *string
	var created time.Time
	err = s.pool.QueryRow(ctx, `DELETE FROM oidc.logins WHERE state_hash=$1 RETURNING provider_id, nonce, verifier, next, link_user, created_at`, hash(cookie.Value)).
		Scan(&providerID, &nonce, &verifier, &next, &linkUser, &created)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && s.now().Sub(created) > loginTTL) {
		return "", fail("sso_expired")
	}
	if err != nil {
		return "", err
	}
	if linkUser != nil {
		*back = "/account"
		// Linking needs the same person still signed in.
		if u, ok := auth.CurrentUser(ctx); !ok || u.ID != *linkUser {
			return "", fail("sso_expired")
		}
	}
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			return "", fail("sso_cancelled")
		}
		return "", fmt.Errorf("provider said %s: %s", e, q.Get("error_description"))
	}
	p, secret, err := s.store.Get(ctx, providerID)
	if err != nil {
		return "", err
	}
	if !p.Enabled {
		return "", fail("sso_unavailable")
	}
	op, err := s.provider(ctx, p.Issuer)
	if err != nil {
		return "", err
	}
	tok, err := s.oauth(p, secret, op).Exchange(s.ctx(ctx), q.Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("exchanging the code: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return "", errors.New("the provider sent no ID token")
	}
	idt, err := op.VerifierContext(s.ctx(ctx), &gooidc.Config{ClientID: p.ClientID}).Verify(s.ctx(ctx), raw)
	if err != nil {
		s.forget(p.Issuer) // new signing keys, perhaps: look again next time
		return "", fmt.Errorf("checking the ID token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(nonce)) != 1 {
		return "", errors.New("the ID token's nonce doesn't match")
	}
	var c claims
	if err := idt.Claims(&c); err != nil {
		return "", err
	}
	eu := s.external(p, idt.Subject, c)
	if eu.Email != "" && len(p.AllowedDomains) > 0 {
		_, domain, _ := strings.Cut(eu.Email, "@")
		if !slices.Contains(p.AllowedDomains, domain) {
			return "", fail("sso_domain")
		}
	} else if eu.Email == "" && len(p.AllowedDomains) > 0 {
		return "", fail("sso_domain")
	}

	if linkUser != nil {
		if err := s.auth.LinkExternal(ctx, *linkUser, eu); err != nil {
			return "", err
		}
		return "/account?sso=linked", nil
	}
	u, err := s.auth.SignInExternal(ctx, eu)
	if err != nil {
		return "", err
	}
	token, exp, err := s.auth.StartSession(ctx, u, r.UserAgent())
	if err != nil {
		return "", err
	}
	// Rotate: drop the session the browser held before, as a password login does.
	if old, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.auth.Logout(ctx, old.Value)
	}
	auth.SetSessionCookie(w, token, exp)
	return next, nil
}

// external turns the provider's claims into what auth needs.
func (s *Service) external(p Provider, subject string, c claims) auth.ExternalUser {
	email := strings.ToLower(c.str(p.EmailClaim))
	if !strings.Contains(email, "@") {
		email = ""
	}
	name := c.str("name")
	if name == "" {
		name = strings.TrimSpace(c.str("given_name") + " " + c.str("family_name"))
	}
	eu := auth.ExternalUser{ProviderID: p.ID, Subject: subject, Email: email, Name: name,
		EmailVerified: c.verified() || p.TrustEmail, LinkByEmail: p.LinkByEmail}
	switch p.NewAccounts {
	case "teacher":
		eu.NewRole = auth.RoleTeacher
	case "student":
		eu.NewRole = auth.RoleStudent
		if p.TeacherClaim != "" {
			for _, v := range c.values(p.TeacherClaim) {
				if slices.Contains(p.TeacherValues, v) {
					eu.NewRole = auth.RoleTeacher
				}
			}
		}
	}
	return eu
}

// TestResult is what the admin's "Test" finds at an issuer.
type TestResult struct {
	OK                    bool     `json:"ok"`
	Message               string   `json:"message"`
	AuthorizationEndpoint string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint         string   `json:"token_endpoint,omitempty"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
	PKCE                  bool     `json:"pkce"`
}

// Test reads an issuer's discovery document, as a sign-in would.
func (s *Service) Test(ctx context.Context, issuer string) TestResult {
	p := Provider{Name: "x", ClientID: "x", Issuer: issuer}
	p.normalise()
	if err := p.validate(true); err != nil {
		var he *httpx.Error
		if errors.As(err, &he) && he.Fields["issuer"] != "" {
			return TestResult{Message: he.Fields["issuer"]}
		}
	}
	op, err := gooidc.NewProvider(s.ctx(ctx), p.Issuer)
	if err != nil {
		return TestResult{Message: "couldn't read the issuer's discovery document: " + err.Error()}
	}
	var meta struct {
		Scopes []string `json:"scopes_supported"`
		PKCE   []string `json:"code_challenge_methods_supported"`
	}
	_ = op.Claims(&meta)
	return TestResult{OK: true, Message: "found the provider", AuthorizationEndpoint: op.Endpoint().AuthURL, TokenEndpoint: op.Endpoint().TokenURL,
		ScopesSupported: meta.Scopes, PKCE: slices.Contains(meta.PKCE, "S256")}
}

// ---------- HTTP ----------

// Routes mounts the sign-in under /api/auth.
func (s *Service) Routes(r chi.Router) {
	r.Method("GET", "/oidc/{id}/start", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error { return s.start(w, r, "") }))
	r.Get("/oidc/callback", s.callback)
	// From the account page: connect a provider to the signed-in account.
	r.Method("GET", "/oidc/{id}/link", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, ok := auth.CurrentUser(r.Context())
		if !ok {
			return httpx.ErrUnauthorized
		}
		return s.start(w, r, u.ID)
	}))
	r.Method("GET", "/oidc/identities", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, ok := auth.CurrentUser(r.Context())
		if !ok {
			return httpx.ErrUnauthorized
		}
		ids, err := s.auth.Identities(r.Context(), u.ID)
		if err != nil {
			return err
		}
		type row struct {
			auth.Identity
			Name string `json:"name"`
		}
		names := map[string]string{}
		all, err := s.store.List(r.Context())
		if err != nil {
			return err
		}
		for _, p := range all {
			names[p.ID] = p.Name
		}
		out := make([]row, 0, len(ids))
		for _, i := range ids {
			out = append(out, row{Identity: i, Name: names[i.ProviderID]})
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"identities": out, "available": s.Options(r.Context())})
		return nil
	}))
	r.Method("DELETE", "/oidc/identities/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, ok := auth.CurrentUser(r.Context())
		if !ok {
			return httpx.ErrUnauthorized
		}
		if err := s.auth.UnlinkExternal(r.Context(), u.ID, chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
}

// AdminRoutes: admins only. Who may sign in, and as what, isn't a feature a
// manager can be given (PL-FR-13), as with school sign-in.
func (s *Service) AdminRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.auth.AdminOnly())
		r.Method("GET", "/sso", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			list, err := s.store.List(r.Context())
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, map[string]any{"providers": list, "redirect_url": s.RedirectURL()})
			return nil
		}))
		save := func(w http.ResponseWriter, r *http.Request, id string, status int) error {
			var in struct {
				Provider
				ClearSecret bool `json:"clear_secret"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			in.Provider.ID = id
			p, err := s.store.Save(r.Context(), auth.MustUser(r.Context()).ID, in.Provider, in.ClearSecret)
			if err != nil {
				return err
			}
			s.forget(p.Issuer)
			httpx.JSON(w, status, p)
			return nil
		}
		r.Method("POST", "/sso", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error { return save(w, r, "", http.StatusCreated) }))
		r.Method("PUT", "/sso/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			return save(w, r, chi.URLParam(r, "id"), http.StatusOK)
		}))
		r.Method("DELETE", "/sso/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			if err := s.store.Delete(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "id")); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
		r.Method("PUT", "/sso-order", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				IDs []string `json:"ids"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			if err := s.store.Reorder(r.Context(), in.IDs); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
		r.Method("POST", "/sso/test", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				Issuer string `json:"issuer"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, s.Test(r.Context(), in.Issuer))
			return nil
		}))
	})
}
