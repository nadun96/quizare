// Package apptest runs the full HTTP app against a real PostgreSQL for
// end-to-end API tests.
package apptest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/app"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/imageurl"
	"github.com/nadun96/quizplatform/internal/llm"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/config"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

type Env struct {
	T      testing.TB
	App    *app.App
	Server *httptest.Server
	Pool   *pgxpool.Pool
	seq    atomic.Int64
}

// Option lets a test tweak configuration before the app is built.
type Option func(*config.Config, *app.Options)

// WithProviders replaces the LLM provider adapters.
func WithProviders(p map[string]llm.Provider) Option {
	return func(_ *config.Config, o *app.Options) { o.Providers = p }
}

func New(t testing.TB, opts ...Option) *Env {
	t.Helper()
	pool := dbtest.New(t)
	e := &Env{T: t, Pool: pool}
	// Start the server first so BaseURL (used for Origin checks) is known.
	var handler http.Handler = http.NotFoundHandler()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	cfg := config.Config{BaseURL: srv.URL, Argon2Workers: 2, DBMaxConns: 8, APIDocs: true}
	options := app.Options{Mailer: mail.LogSender{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		Checker: imageurl.NewChecker(true), // tests check URLs on local httptest servers
		KEK:     bytes.Repeat([]byte{7}, 32)}
	for _, o := range opts {
		o(&cfg, &options)
	}
	a, err := app.Build(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), pool, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	handler = a.Handler()
	e.App, e.Server = a, srv
	return e
}

// Client is a browser-like client with its own cookie jar.
type Client struct {
	e    *Env
	http *http.Client
	User auth.User
	// Headers are added to every request (e.g. a poll's anonymous token).
	Headers map[string]string
}

func (e *Env) Client() *Client {
	jar, _ := cookiejar.New(nil)
	hc := *e.Server.Client() // copy: Server.Client() is shared
	hc.Jar = jar
	return &Client{e: e, http: &hc}
}

// Do sends a request with the SPA's CSRF header and same-origin Origin.
func (c *Client) Do(method, path string, body any) (int, []byte) {
	c.e.T.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			c.e.T.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.e.Server.URL+path, rd)
	if err != nil {
		c.e.T.Fatal(err)
	}
	req.Header.Set("Origin", c.e.Server.URL)
	req.Header.Set("X-Requested-With", "fetch")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.T.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// Raw sends a non-JSON body (e.g. text/csv) and returns status and body.
func (c *Client) Raw(method, path, contentType string, body []byte) (int, []byte) {
	c.e.T.Helper()
	req, err := http.NewRequest(method, c.e.Server.URL+path, bytes.NewReader(body))
	if err != nil {
		c.e.T.Fatal(err)
	}
	req.Header.Set("Origin", c.e.Server.URL)
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", contentType)
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.T.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// Dial opens a WebSocket to path with this client's cookies and our Origin.
func (c *Client) Dial(path string) *websocket.Conn {
	c.e.T.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(c.e.Server.URL, "https")+path, &websocket.DialOptions{
		HTTPClient: c.http, HTTPHeader: http.Header{"Origin": {c.e.Server.URL}},
	})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		c.e.T.Fatalf("dial %s: %v (status %d)", path, err, status)
	}
	c.e.T.Cleanup(func() { conn.CloseNow() })
	return conn
}

// ReadUntil reads JSON messages until one has the given "type" or the timeout passes.
func ReadUntil(t testing.TB, conn *websocket.Conn, typ string, timeout time.Duration) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v", typ, err)
		}
		var m map[string]any
		if json.Unmarshal(data, &m) == nil && m["type"] == typ {
			return m
		}
	}
}

// Call sends a request, asserts the status and decodes the response into out.
func (c *Client) Call(method, path string, body any, want int, out any) {
	c.e.T.Helper()
	status, raw := c.Do(method, path, body)
	if status != want {
		c.e.T.Fatalf("%s %s: status %d, want %d: %s", method, path, status, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			c.e.T.Fatalf("%s %s: decode %s: %v", method, path, raw, err)
		}
	}
}

// NewUser registers a user with the given role and returns a logged-in client.
func (e *Env) NewUser(role auth.Role) *Client {
	e.T.Helper()
	n := e.seq.Add(1)
	c := e.Client()
	email := fmt.Sprintf("%s%d@example.com", role, n)
	switch role {
	case auth.RoleAdmin:
		u, err := e.App.Auth.CreateAdmin(context.Background(), email, "Admin", "password123")
		if err != nil {
			e.T.Fatal(err)
		}
		c.Call("POST", "/api/auth/login", map[string]string{"email": email, "password": "password123"}, 200, &c.User)
		_ = u
	default:
		c.Call("POST", "/api/auth/register", map[string]any{
			"email": email, "name": fmt.Sprintf("%s %d", role, n), "password": "password123", "role": role,
		}, 201, &c.User)
	}
	return c
}

// TakeJobs returns the args of queued River jobs of a kind and deletes them,
// so tests can run workers' logic deterministically.
func (e *Env) TakeJobs(kind string) []json.RawMessage {
	e.T.Helper()
	rows, err := e.Pool.Query(context.Background(), `DELETE FROM river_job WHERE kind=$1 AND state='available' RETURNING args`, kind)
	if err != nil {
		e.T.Fatal(err)
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			e.T.Fatal(err)
		}
		out = append(out, raw)
	}
	return out
}

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

// LastEmailToken returns the token from the newest queued email to addr
// with the given subject.
func (e *Env) LastEmailToken(addr, subject string) string {
	e.T.Helper()
	var body string
	err := e.Pool.QueryRow(context.Background(), `SELECT args->>'body' FROM river_job
		WHERE kind='email' AND args->>'to'=$1 AND args->>'subject'=$2 ORDER BY id DESC LIMIT 1`, addr, subject).Scan(&body)
	if err != nil {
		e.T.Fatalf("no email %q to %s: %v", subject, addr, err)
	}
	m := tokenRe.FindStringSubmatch(body)
	if m == nil {
		e.T.Fatalf("no token in email body: %s", body)
	}
	return m[1]
}
