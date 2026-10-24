// Package oidctest is an OpenID Connect provider for tests: discovery, keys,
// an authorization endpoint that signs in whoever the test says, and a token
// endpoint that checks the client, the redirect and PKCE before it issues a
// signed ID token.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Provider is a running fake identity provider.
type Provider struct {
	Server       *httptest.Server
	ClientID     string
	ClientSecret string

	key *rsa.PrivateKey
	mu  sync.Mutex
	// User holds the claims of whoever "signs in" next (sub, email, …).
	user   map[string]any
	deny   bool
	codes  map[string]grant
	Tokens int // ID tokens issued
}

type grant struct {
	claims           map[string]any
	nonce, challenge string
	redirect, client string
}

// New starts a provider over HTTPS; it stops when the test ends.
func New(t testing.TB) *Provider { return start(t, true) }

// NewPlain starts one over plain HTTP on 127.0.0.1, for trying sign-in in a
// browser against a local server (issuers on this machine may use http).
func NewPlain(t testing.TB) *Provider { return start(t, false) }

func start(t testing.TB, tls bool) *Provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{ClientID: "quiz-app", ClientSecret: "s3cret-for-tests", key: key, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("/authorize", p.authorize)
	mux.HandleFunc("/token", p.token)
	mux.HandleFunc("/jwks", p.jwks)
	if tls {
		p.Server = httptest.NewTLSServer(mux)
	} else {
		p.Server = httptest.NewServer(mux)
	}
	t.Cleanup(p.Server.Close)
	return p
}

// Issuer is the provider's issuer URL.
func (p *Provider) Issuer() string { return p.Server.URL }

// SignInAs sets who the next sign-in is.
func (p *Provider) SignInAs(claims map[string]any) {
	p.mu.Lock()
	p.user, p.deny = claims, false
	p.mu.Unlock()
}

// Deny makes the next sign-in end with the person saying no.
func (p *Provider) Deny() {
	p.mu.Lock()
	p.deny = true
	p.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer": p.Issuer(), "authorization_endpoint": p.Issuer() + "/authorize", "token_endpoint": p.Issuer() + "/token",
		"jwks_uri": p.Issuer() + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"}, "scopes_supported": []string{"openid", "email", "profile"},
		"code_challenge_methods_supported": []string{"S256"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	enc := base64.RawURLEncoding
	writeJSON(w, map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "k1",
		"n": enc.EncodeToString(p.key.N.Bytes()), "e": enc.EncodeToString(big.NewInt(int64(p.key.E)).Bytes()),
	}}})
}

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("nonce") == "" {
		http.Error(w, "PKCE and a nonce are required", http.StatusBadRequest)
		return
	}
	back := redirect.Query()
	back.Set("state", q.Get("state"))
	p.mu.Lock()
	if p.deny || p.user == nil {
		back.Set("error", "access_denied")
	} else {
		code := rand.Text()
		p.codes[code] = grant{claims: p.user, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), client: q.Get("client_id")}
		back.Set("code", code)
	}
	p.mu.Unlock()
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	// Basic auth values are form-encoded (RFC 6749 §2.3.1).
	id, _ = url.QueryUnescape(id)
	secret, _ = url.QueryUnescape(secret)
	p.mu.Lock()
	g, found := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code")) // one use
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	switch {
	case id != p.ClientID || secret != p.ClientSecret:
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "invalid_client"})
		return
	case !found || g.redirect != r.PostForm.Get("redirect_uri") || g.client != id:
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	case base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge:
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant", "error_description": "PKCE verifier mismatch"})
		return
	}
	now := time.Now()
	c := jwt.MapClaims{"iss": p.Issuer(), "aud": p.ClientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "nonce": g.nonce}
	for k, v := range g.claims {
		c[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	tok.Header["kid"] = "k1"
	signed, err := tok.SignedString(p.key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.mu.Lock()
	p.Tokens++
	p.mu.Unlock()
	writeJSON(w, map[string]any{"access_token": rand.Text(), "token_type": "Bearer", "expires_in": 3600, "id_token": signed})
}
