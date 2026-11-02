// Package loadtest is the admin's capacity test (D-125): virtual students
// sign in, join a real quiz session on this server, answer and submit,
// in growing steps, while the server's response times, CPU, memory,
// database and traffic are measured. The accounts and everything they make
// are deleted afterwards, and on the next start if the server stopped mid-test.
package loadtest

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Limits of a plan.
const (
	MaxStudents = 2000
	MaxSteps    = 6
	keepRuns    = 20
)

// Plan is what the admin asks for.
type Plan struct {
	Steps        []int   `json:"steps"`         // students in each step, growing; the test stops at the first step that fails
	Questions    int     `json:"questions"`     // 3-30, one answer each
	ThinkSeconds int     `json:"think_seconds"` // a student's time on a question, on average
	RampSeconds  int     `json:"ramp_seconds"`  // students arrive over this time
	OneNetwork   bool    `json:"one_network"`   // all from one internet address, as from one school's Wi-Fi
	LinkMbps     float64 `json:"link_mbps"`     // the server's internet upload, for the bandwidth estimate
}

// DefaultPlan finds the limit up to 2,000 students.
var DefaultPlan = Plan{Steps: []int{100, 250, 500, 1000, 2000}, Questions: 10, ThinkSeconds: 3, RampSeconds: 30, LinkMbps: 300}

func (p *Plan) check() error {
	f := map[string]string{}
	if len(p.Steps) == 0 || len(p.Steps) > MaxSteps {
		f["steps"] = fmt.Sprintf("1 to %d steps", MaxSteps)
	}
	for i, n := range p.Steps {
		if n < 1 || n > MaxStudents || (i > 0 && n <= p.Steps[i-1]) {
			f["steps"] = fmt.Sprintf("growing numbers of students, 1 to %d", MaxStudents)
		}
	}
	if p.Questions < 3 || p.Questions > 30 {
		f["questions"] = "3 to 30"
	}
	if p.ThinkSeconds < 1 || p.ThinkSeconds > 30 {
		f["think_seconds"] = "1 to 30 seconds"
	}
	if p.RampSeconds < 0 || p.RampSeconds > 300 {
		f["ramp_seconds"] = "0 to 300 seconds"
	}
	if p.LinkMbps == 0 {
		p.LinkMbps = 300
	}
	if p.LinkMbps < 1 || p.LinkMbps > 100000 {
		f["link_mbps"] = "1 to 100,000 Mbit/s"
	}
	if len(f) > 0 {
		return httpx.Invalid(f)
	}
	return nil
}

// Thresholds a step must meet (architecture §2.3; D-125).
var Thresholds = struct {
	SaveP95, SaveP99, LoginP95, JoinP95 float64
	ErrorRate, Completion               float64
}{SaveP95: 150, SaveP99: 400, LoginP95: 3000, JoinP95: 1000, ErrorRate: 0.01, Completion: 0.99}

// Step is one size of class and how the server coped.
type Step struct {
	Students   int                `json:"students"`
	Passed     bool               `json:"passed"`
	Reasons    []string           `json:"reasons,omitempty"` // why it failed
	Seconds    float64            `json:"seconds"`
	Joined     int                `json:"joined"`
	Completed  int                `json:"completed"` // submitted every answer
	Ops        map[string]Latency `json:"ops"`
	Errors     map[string]int     `json:"errors"`
	FirstError map[string]string  `json:"first_error,omitempty"`
	Drops      int                `json:"drops"` // live connections lost
	// The traffic of this step's students (not the first download of the app).
	DownMB       float64 `json:"down_mb"`
	UpMB         float64 `json:"up_mb"`
	PeakDownMbps float64 `json:"peak_down_mbps"`
	KBPerStudent float64 `json:"kb_per_student"`
	// The machine at its busiest; -1 where unknown.
	CPUPeak     float64  `json:"cpu_peak"`
	CPUAvg      float64  `json:"cpu_avg"`
	MemFreeMin  float64  `json:"mem_free_min_mb"`
	Load1Peak   float64  `json:"load1_peak"`
	HeapPeakMB  float64  `json:"heap_peak_mb"`
	DBBusyPeak  int32    `json:"db_busy_peak"`
	DBWaits     int64    `json:"db_waits"`
	SavesPerSec float64  `json:"saves_per_sec_peak"`
	Samples     []Sample `json:"samples"`
}

// Report is a whole test.
type Report struct {
	Steps     []Step  `json:"steps"`
	Max       int     `json:"max"`        // the largest step that passed; 0 none
	PageKB    float64 `json:"page_kb"`    // the app's first download, compressed; 0 not served by this process
	PageFiles int     `json:"page_files"` //
	Machine   Machine `json:"machine"`
	Error     string  `json:"error,omitempty"`
	DBMax     int32   `json:"db_max"`
	// The sign-in limits in force during the test (D-126).
	SignIn *SignIn `json:"sign_in,omitempty"`
}

// SignIn is the server's sign-in limits when the test ran.
type SignIn struct {
	NetworkBurst  int     `json:"network_burst"`
	NetworkPerSec float64 `json:"network_per_sec"`
	Workers       int     `json:"workers"`
}

// Machine is what the test ran on.
type Machine struct {
	CPUs       int     `json:"cpus"`
	MemTotalMB float64 `json:"mem_total_mb"` // -1 unknown
	OS         string  `json:"os"`
}

// Run is a test and its report.
type Run struct {
	ID         string     `json:"id"`
	AdminID    *string    `json:"admin_id"`
	AdminName  string     `json:"admin_name"`
	Status     string     `json:"status"`
	Plan       Plan       `json:"plan"`
	Report     *Report    `json:"report,omitempty"`
	Max        int        `json:"max"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// Progress is the running test, for the admin's page.
type Progress struct {
	RunID    string  `json:"run_id"`
	Plan     Plan    `json:"plan"`
	Step     int     `json:"step"` // index into Plan.Steps
	Students int     `json:"students"`
	Phase    string  `json:"phase"`
	Joined   int64   `json:"joined"`
	Finished int64   `json:"finished"`
	Errors   int64   `json:"errors"`
	Elapsed  float64 `json:"elapsed"`
	Sample   *Sample `json:"sample"`
	Done     []Step  `json:"done"` // steps finished so far, without samples
	Stopping bool    `json:"stopping"`
}

type Service struct {
	pool    *pgxpool.Pool
	auth    *auth.Service
	target  string // where virtual students connect: this server, bypassing the proxy
	origin  string // the site's address, which the server checks requests against
	log     *slog.Logger
	now     func() time.Time
	tls     *tls.Config // tests: trust the test server
	mu      sync.Mutex
	current *runner
}

// NewService: target is this server's own address (http://127.0.0.1:8080),
// origin its public one.
func NewService(pool *pgxpool.Pool, a *auth.Service, target, origin string, log *slog.Logger) *Service {
	s := &Service{pool: pool, auth: a, target: target, origin: origin, log: log, now: time.Now}
	a.SetLoadGate(s.gate)
	return s
}

// SetTLSConfig is for tests, whose server has its own certificate.
func (s *Service) SetTLSConfig(c *tls.Config) { s.tls = c }

// gate lets the running test's sign-ins through, and nothing else.
func (s *Service) gate(token string) bool {
	s.mu.Lock()
	r := s.current
	s.mu.Unlock()
	return r != nil && subtle.ConstantTimeCompare([]byte(token), []byte(r.token)) == 1
}

var (
	errRunning  = httpx.NewError(http.StatusConflict, "test_running", "a capacity test is already running")
	errBusy     = httpx.NewError(http.StatusConflict, "server_in_use", "students are taking a quiz now; run the test when no session is live")
	errNotFound = httpx.NewError(http.StatusNotFound, "not_found", "no such test")
)

// Start begins a test in the background and returns its id.
func (s *Service) Start(ctx context.Context, adminID string, p Plan) (string, error) {
	if err := p.check(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		return "", errRunning
	}
	// Real students come first: no test while anyone is in a live quiz.
	var busy bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM live.sessions ss JOIN auth.users u ON u.id = ss.teacher_id
		JOIN live.attempts a ON a.session_id = ss.id
		WHERE ss.status <> 'ended' AND NOT u.synthetic AND a.state IN ('waiting','admitted','in_progress','paused')
		  AND a.updated_at > now() - interval '30 minutes')`).Scan(&busy); err != nil {
		return "", err
	}
	if busy {
		return "", errBusy
	}
	if err := s.Cleanup(ctx); err != nil {
		return "", err
	}
	plan, _ := json.Marshal(p)
	var id string
	if err := s.pool.QueryRow(ctx, `INSERT INTO loadtest.runs(admin_id, status, plan) VALUES ($1,'running',$2) RETURNING id`, adminID, plan).Scan(&id); err != nil {
		return "", err
	}
	tok := make([]byte, 32)
	_, _ = rand.Read(tok)
	r := &runner{s: s, id: id, plan: p, token: hex.EncodeToString(tok), started: s.now(), stop: make(chan struct{})}
	s.current = r
	go r.main()
	return id, nil
}

// Stop asks the running test to end now; it still cleans up and keeps its report.
func (s *Service) Stop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return false
	}
	s.current.halt()
	return true
}

// Running is the test in progress, or nil.
func (s *Service) Running() *Progress {
	s.mu.Lock()
	r := s.current
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.progress()
}

// Wait blocks until no test runs (tests and shutdown).
func (s *Service) Wait(ctx context.Context) error {
	for {
		s.mu.Lock()
		r := s.current
		s.mu.Unlock()
		if r == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *Service) finish(r *runner, status string, rep Report) {
	b, _ := json.Marshal(rep)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.pool.Exec(ctx, `UPDATE loadtest.runs SET status=$2, report=$3, finished_at=now() WHERE id=$1`, r.id, status, b); err != nil {
		s.log.Error("capacity test: saving the report", "err", err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM loadtest.runs WHERE id NOT IN (SELECT id FROM loadtest.runs ORDER BY started_at DESC LIMIT $1)`, keepRuns); err != nil {
		s.log.Error("capacity test: trimming reports", "err", err)
	}
	s.mu.Lock()
	s.current = nil
	s.mu.Unlock()
}

const runCols = `r.id, r.admin_id, coalesce(u.name, ''), r.status, r.plan, r.started_at, r.finished_at, coalesce((r.report->>'max')::int, 0)`

func scanRun(row pgx.Row, withReport bool) (Run, error) {
	var x Run
	var plan, rep []byte
	dst := []any{&x.ID, &x.AdminID, &x.AdminName, &x.Status, &plan, &x.StartedAt, &x.FinishedAt, &x.Max}
	if withReport {
		dst = append(dst, &rep)
	}
	if err := row.Scan(dst...); err != nil {
		return x, err
	}
	_ = json.Unmarshal(plan, &x.Plan)
	if withReport {
		x.Report = &Report{}
		_ = json.Unmarshal(rep, x.Report)
	}
	return x, nil
}

// Runs are the recent tests, newest first, without their reports.
func (s *Service) Runs(ctx context.Context) ([]Run, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+runCols+` FROM loadtest.runs r LEFT JOIN auth.users u ON u.id = r.admin_id ORDER BY r.started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		x, err := scanRun(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Get is one test with its report.
func (s *Service) Get(ctx context.Context, id string) (Run, error) {
	x, err := scanRun(s.pool.QueryRow(ctx, `SELECT `+runCols+`, r.report FROM loadtest.runs r LEFT JOIN auth.users u ON u.id = r.admin_id WHERE r.id::text = $1`, id), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, errNotFound
	}
	return x, err
}

// Delete removes a finished test's report.
func (s *Service) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM loadtest.runs WHERE id::text = $1 AND status <> 'running'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return nil
}

// Recover runs when the server starts: a test that was running was cut
// short, and its accounts are removed.
func (s *Service) Recover(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `UPDATE loadtest.runs SET status='interrupted', finished_at=now() WHERE status='running'`); err != nil {
		return err
	}
	return s.Cleanup(ctx)
}

// Cleanup deletes every virtual account and all they made: sessions,
// attempts, answers, results, the classroom and quiz, the warehouse's
// copies and the audit trail of their sign-ins.
func (s *Service) Cleanup(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM auth.users WHERE synthetic`).Scan(&n); err != nil || n == 0 {
			return err
		}
		const syn = `(SELECT id FROM auth.users WHERE synthetic)`
		const rooms = `(SELECT id FROM content.classrooms WHERE teacher_id IN ` + syn + `)`
		for _, q := range []string{
			`DELETE FROM olap.answers WHERE user_id IN ` + syn + ` OR classroom_id IN ` + rooms,
			`DELETE FROM olap.activity WHERE user_id IN ` + syn + ` OR classroom_id IN ` + rooms,
			`DELETE FROM olap.items WHERE topic_id IN (SELECT id FROM olap.topics WHERE classroom_id IN ` + rooms + `)`,
			`DELETE FROM olap.topics WHERE classroom_id IN ` + rooms,
			`DELETE FROM olap.coteachers WHERE classroom_id IN ` + rooms,
			`DELETE FROM olap.classrooms WHERE id IN ` + rooms,
			`DELETE FROM olap.people WHERE id IN ` + syn,
			`DELETE FROM analytics.session_stats WHERE teacher_id IN ` + syn,
			`DELETE FROM live.sessions WHERE teacher_id IN ` + syn,
			`DELETE FROM live.attempts WHERE user_id IN ` + syn,
			`DELETE FROM eval.results WHERE user_id IN ` + syn,
			`DELETE FROM audit.events WHERE actor_id IN ` + syn,
			`DELETE FROM quiz.quizzes WHERE teacher_id IN ` + syn,
			`DELETE FROM content.enrolments WHERE user_id IN ` + syn,
			`DELETE FROM content.classrooms WHERE teacher_id IN ` + syn,
			`DELETE FROM auth.users WHERE synthetic`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return fmt.Errorf("capacity test cleanup: %w", err)
			}
		}
		return nil
	})
}

func machine() Machine {
	return Machine{CPUs: runtime.NumCPU(), MemTotalMB: memTotalMB(), OS: runtime.GOOS + "/" + runtime.GOARCH}
}
