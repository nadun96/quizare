// Package testenv runs the tutoring service against a real PostgreSQL, a
// fake platform and a fake LiveKit, for API tests.
//
// It uses QP_TEST_DATABASE_URL when set (as the platform's tests do, e.g.
// `go run ./cmd/testdb` in backend/), and otherwise starts an embedded
// PostgreSQL for the run.
package testenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/db"
	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/platform"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
	"github.com/nadun96/quizplatform/tutoring/migrations"
)

var (
	once     sync.Once
	baseURL  string
	setupErr error
	stop     func()
	counter  atomic.Int64
)

// Main stops the embedded database after the run: func TestMain(m *testing.M) { testenv.Main(m) }.
func Main(m *testing.M) {
	code := m.Run()
	if stop != nil {
		stop()
	}
	os.Exit(code)
}

func setup() {
	if u := os.Getenv("QP_TEST_DATABASE_URL"); u != "" {
		baseURL = u
		return
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		setupErr = err
		return
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cache, _ := os.UserCacheDir()
	rt, err := os.MkdirTemp("", "tutor-pg-")
	if err != nil {
		setupErr = err
		return
	}
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().Version(embeddedpostgres.V16).Port(uint32(port)).
		CachePath(filepath.Join(cache, "quizplatform-pg")).RuntimePath(rt).Logger(io.Discard).
		StartParameters(map[string]string{"fsync": "off", "max_connections": "200"}))
	if err := pg.Start(); err != nil {
		setupErr = err
		return
	}
	stop = func() { _ = pg.Stop(); _ = os.RemoveAll(rt) }
	baseURL = fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", port)
}

func newDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	once.Do(setup)
	if setupErr != nil {
		t.Fatalf("test database: %v", setupErr)
	}
	ctx := context.Background()
	name := fmt.Sprintf("tutor_test_%d_%d", os.Getpid(), counter.Add(1))
	conn, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	u, _ := url.Parse(baseURL)
	u.Path = "/" + name
	pool, err := db.Open(ctx, u.String(), 8, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if c, err := pgx.Connect(ctx, baseURL); err == nil {
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			c.Close(ctx)
		}
	})
	return pool
}

// Secret is the key the fake platform and the service share.
var Secret = bytes.Repeat([]byte{5}, 32)

// Platform is a fake of the platform's internal API: classrooms with their
// teacher and enrolled students.
type Platform struct {
	mu        sync.Mutex
	Teachers  map[string]string          // classroom → teacher
	Enrolled  map[string]map[string]bool // classroom → students
	Archived  map[string]bool
	Calls     int
	BadTokens int
}

func (p *Platform) Enrol(classroom, student string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Enrolled[classroom] == nil {
		p.Enrolled[classroom] = map[string]bool{}
	}
	p.Enrolled[classroom][student] = true
}

func (p *Platform) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Calls++
	raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if _, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return Secret, nil }, jwt.WithIssuer("tutoring"), jwt.WithAudience("quiz-platform"),
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired()); err != nil || r.URL.Path != "/internal/tutoring/access" {
		p.BadTokens++
		w.WriteHeader(404)
		return
	}
	var in struct{ ClassroomID, UserID string }
	_ = json.NewDecoder(r.Body).Decode(&struct {
		ClassroomID *string `json:"classroom_id"`
		UserID      *string `json:"user_id"`
	}{&in.ClassroomID, &in.UserID})
	teacher, ok := p.Teachers[in.ClassroomID]
	w.Header().Set("Content-Type", "application/json")
	switch {
	case !ok:
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"resource not found"}`))
	case teacher == in.UserID:
		_ = json.NewEncoder(w).Encode(platform.Access{ClassroomID: in.ClassroomID, ClassroomName: "Room " + in.ClassroomID[:4], TeacherID: teacher, Archived: p.Archived[in.ClassroomID], Role: "teacher"})
	case p.Enrolled[in.ClassroomID][in.UserID]:
		_ = json.NewEncoder(w).Encode(platform.Access{ClassroomID: in.ClassroomID, ClassroomName: "Room " + in.ClassroomID[:4], TeacherID: teacher, Role: "student"})
	default:
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"code":"not_enrolled","message":"you are not enrolled in this classroom"}`))
	}
}

// Call is one request the fake LiveKit received.
type Call struct {
	Method string
	Body   map[string]any
}

// LiveKit is a fake of LiveKit's server API (Twirp JSON).
type LiveKit struct {
	mu     sync.Mutex
	calls  []Call
	Tracks map[string][]media.Track // identity → published tracks
}

func (l *LiveKit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/twirp/livekit.RoomService/")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	l.mu.Lock()
	l.calls = append(l.calls, Call{method, body})
	tracks := l.Tracks
	l.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if method == "ListParticipants" {
		var ps []media.Participant
		for id, ts := range tracks {
			ps = append(ps, media.Participant{Identity: id, Tracks: ts})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"participants": ps})
		return
	}
	_, _ = w.Write([]byte(`{}`))
}

// Calls returns and forgets the calls of one method.
func (l *LiveKit) Calls(method string) []Call {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out, rest []Call
	for _, c := range l.calls {
		if c.Method == method {
			out = append(out, c)
		} else {
			rest = append(rest, c)
		}
	}
	l.calls = rest
	return out
}

type Env struct {
	T        testing.TB
	Pool     *pgxpool.Pool
	Service  *tutor.Service
	Server   *httptest.Server
	Platform *Platform
	LiveKit  *LiveKit
	Origin   string
	LKSecret []byte
	seq      atomic.Int64
}

// New starts the service with fakes around it.
func New(t testing.TB) *Env {
	t.Helper()
	e := &Env{T: t, Pool: newDB(t), Platform: &Platform{Teachers: map[string]string{}, Enrolled: map[string]map[string]bool{}, Archived: map[string]bool{}},
		LiveKit: &LiveKit{Tracks: map[string][]media.Track{}}, Origin: "https://quiz.example.edu", LKSecret: []byte("livekit-secret-of-at-least-32-chars!!")}
	plat := httptest.NewServer(e.Platform)
	lk := httptest.NewServer(e.LiveKit)
	t.Cleanup(plat.Close)
	t.Cleanup(lk.Close)
	keys := authn.Keys{Secret: Secret}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.Service = tutor.New(e.Pool, platform.New(plat.URL, keys), media.New(lk.URL, "devkey", e.LKSecret), "wss://tutor.example.edu", log)
	e.Server = httptest.NewServer(e.Service.Handler(keys, e.Origin, e.Pool))
	t.Cleanup(e.Server.Close)
	return e
}

// Person is someone with a platform token.
type Person struct {
	e    *Env
	ID   string
	Name string
	Role string
}

func uuid(n int64, kind byte) string {
	return fmt.Sprintf("%08x-0000-4000-8%03x-%012x", n, kind, n)
}

// NewPerson makes someone the platform would vouch for.
func (e *Env) NewPerson(role, name string) *Person {
	n := e.seq.Add(1)
	return &Person{e: e, ID: uuid(n, 1), Name: name, Role: role}
}

// NewClassroom makes a classroom taught by teacher.
func (e *Env) NewClassroom(teacher *Person) string {
	id := uuid(e.seq.Add(1), 2)
	e.Platform.mu.Lock()
	e.Platform.Teachers[id] = teacher.ID
	e.Platform.mu.Unlock()
	return id
}

// Token is the platform's 5-minute token for p.
func (p *Person) Token() string {
	now := time.Now()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": "quiz-platform", "aud": "tutoring", "sub": p.ID, "name": p.Name, "role": p.Role,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()}).SignedString(Secret)
	if err != nil {
		p.e.T.Fatal(err)
	}
	return s
}

// Do sends a request as p and returns the status and body.
func (p *Person) Do(method, path string, body any) (int, []byte) {
	p.e.T.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, p.e.Server.URL+path, r)
	req.Header.Set("Authorization", "Bearer "+p.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.e.T.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// Call is Do, failing the test unless the status is want, decoding into out.
func (p *Person) Call(method, path string, body any, want int, out any) {
	p.e.T.Helper()
	code, raw := p.Do(method, path, body)
	if code != want {
		p.e.T.Fatalf("%s %s %s: %d, want %d: %s", p.Name, method, path, code, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			p.e.T.Fatalf("%s %s: decode %s: %v", method, path, raw, err)
		}
	}
}

// Socket is p's live connection to a session. A goroutine reads it, since
// a read that times out would close the connection.
type Socket struct {
	t      testing.TB
	Conn   *websocket.Conn
	msgs   chan map[string]any
	closed chan struct{}
	// CloseReason is the close frame's reason once the server closed it.
	CloseReason string
}

// Dial opens p's socket and sends the token.
func (p *Person) Dial(sessionID string) *Socket {
	p.e.T.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(p.e.Server.URL, "http") + "/ws/sessions/" + sessionID
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {p.e.Origin}}})
	if err != nil {
		p.e.T.Fatalf("dial: %v", err)
	}
	p.e.T.Cleanup(func() { c.CloseNow() })
	b, _ := json.Marshal(map[string]string{"token": p.Token()})
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		p.e.T.Fatal(err)
	}
	s := &Socket{t: p.e.T, Conn: c, msgs: make(chan map[string]any, 256), closed: make(chan struct{})}
	go func() {
		defer close(s.closed)
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				s.CloseReason = websocket.CloseStatus(err).String() + " " + closeText(err)
				return
			}
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				s.msgs <- m
			}
		}
	}()
	return s
}

func closeText(err error) string {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return ""
}

// Next waits for the next message of a type, skipping others.
func (s *Socket) Next(typ string, timeout time.Duration) (map[string]any, error) {
	deadline := time.After(timeout)
	for {
		select {
		case m := <-s.msgs:
			if m["type"] == typ {
				return m, nil
			}
		case <-s.closed:
			select { // what arrived before the close still counts
			case m := <-s.msgs:
				if m["type"] == typ {
					return m, nil
				}
				continue
			default:
			}
			return nil, fmt.Errorf("closed: %s", s.CloseReason)
		case <-deadline:
			return nil, fmt.Errorf("timed out")
		}
	}
}

// Must is Next, failing the test on a timeout.
func (s *Socket) Must(typ string) map[string]any {
	s.t.Helper()
	m, err := s.Next(typ, 3*time.Second)
	if err != nil {
		s.t.Fatalf("waiting for %q: %v", typ, err)
	}
	return m
}

// Quiet checks no message of a type arrives within d.
func (s *Socket) Quiet(typ string, d time.Duration) {
	s.t.Helper()
	if m, err := s.Next(typ, d); err == nil {
		s.t.Fatalf("unexpected %q: %v", typ, m)
	}
}

// WaitClosed waits for the server to close the socket.
func (s *Socket) WaitClosed() string {
	s.t.Helper()
	select {
	case <-s.closed:
		return s.CloseReason
	case <-time.After(3 * time.Second):
		s.t.Fatal("socket still open")
		return ""
	}
}
