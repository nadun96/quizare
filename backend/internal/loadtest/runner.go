package loadtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/auth"
)

const cookieName = "__Host-sid"

// runner is one test in progress.
type runner struct {
	s       *Service
	id      string
	plan    Plan
	token   string // the sign-in secret of this test's accounts
	started time.Time

	stop     chan struct{}
	stopOnce sync.Once

	client   *http.Client // REST, with a timeout
	wsClient *http.Client // live connections: the same connections, timed by their context
	m        *meter
	password string
	students []string // emails
	teacher  string   // the teacher's session cookie
	quizID   string

	mu    sync.Mutex
	step  int
	n     int
	phase string
	done  []Step
	smp   *sampler

	joined, finished, errs, active, saves atomic.Int64
}

func (r *runner) halt() { r.stopOnce.Do(func() { close(r.stop) }) }

func (r *runner) stopped() bool {
	select {
	case <-r.stop:
		return true
	default:
		return false
	}
}

func (r *runner) setPhase(p string) {
	r.mu.Lock()
	r.phase = p
	r.mu.Unlock()
}

func (r *runner) progress() *Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := &Progress{RunID: r.id, Plan: r.plan, Step: r.step, Students: r.n, Phase: r.phase,
		Joined: r.joined.Load(), Finished: r.finished.Load(), Errors: r.errs.Load(),
		Elapsed: round1(time.Since(r.started).Seconds()), Stopping: r.stopped(), Done: []Step{}}
	if r.smp != nil {
		p.Sample = r.smp.latest()
	}
	for _, st := range r.done {
		st.Samples = nil
		p.Done = append(p.Done, st)
	}
	return p
}

func (r *runner) main() {
	status, rep := "error", Report{Machine: machine(), Steps: []Step{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		if p := recover(); p != nil {
			rep.Error = fmt.Sprint("the test stopped unexpectedly: ", p)
			status = "error"
		}
		cancel()
		r.setPhase("cleaning up")
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer ccancel()
		if err := r.s.Cleanup(cctx); err != nil {
			r.s.log.Error("capacity test: cleanup", "err", err)
			rep.Error = strings.TrimSpace(rep.Error + " Cleanup failed and runs again on the next start: " + err.Error())
		}
		r.client.CloseIdleConnections()
		r.s.finish(r, status, rep)
	}()
	go func() {
		select {
		case <-r.stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	r.m = &meter{}
	tr := &http.Transport{DialContext: r.m.dial, TLSClientConfig: r.s.tls, MaxIdleConns: 2 * MaxStudents, MaxIdleConnsPerHost: 2 * MaxStudents, IdleConnTimeout: 90 * time.Second}
	r.client = &http.Client{Timeout: 60 * time.Second, Transport: tr}
	r.wsClient = &http.Client{Transport: tr}
	rep.DBMax = r.s.pool.Stat().MaxConns()
	if l, err := r.s.auth.SignInLimits(ctx); err == nil {
		rep.SignIn = &SignIn{NetworkBurst: l.NetworkBurst, NetworkPerSec: l.NetworkPerSec, Workers: l.Workers}
	}
	r.setPhase("measuring the app's download")
	rep.PageKB, rep.PageFiles = r.pageWeight(ctx)
	r.setPhase("creating the virtual class")
	if err := r.prepare(ctx); err != nil {
		rep.Error = "Preparing the test: " + err.Error()
		if r.stopped() {
			status = "stopped"
		}
		return
	}
	for i, n := range r.plan.Steps {
		if ctx.Err() != nil {
			break
		}
		st, err := r.runStep(ctx, i, n)
		if err != nil {
			if !r.stopped() {
				rep.Error = fmt.Sprintf("Step %d (%d students): %v", i+1, n, err)
			}
			break
		}
		rep.Steps = append(rep.Steps, st)
		r.mu.Lock()
		r.done = append(r.done, st)
		r.mu.Unlock()
		if !st.Passed {
			break
		}
		rep.Max = n
	}
	switch {
	case r.stopped():
		status = "stopped"
	case rep.Error != "":
		status = "error"
	case len(rep.Steps) == len(r.plan.Steps) && rep.Max == r.plan.Steps[len(r.plan.Steps)-1]:
		status = "passed"
	default:
		status = "failed"
	}
}

// ---------- requests ----------

type httpError struct {
	status int
	body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.status, e.body) }

func statusOf(err error) int {
	var he *httpError
	if errors.As(err, &he) {
		return he.status
	}
	return 0
}

// call sends one request as a browser on this site would, from the address ip.
func (r *runner) call(ctx context.Context, cookie, ip, method, path string, body, out any) (http.Header, error) {
	var rd io.Reader
	ctype := "application/json"
	switch b := body.(type) {
	case nil:
	case []byte:
		rd, ctype = bytes.NewReader(b), "text/csv"
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.s.target+path, rd)
	if err != nil {
		return nil, err
	}
	if rd != nil {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Origin", r.s.origin)
	req.Header.Set("X-Forwarded-For", ip)
	req.Header.Set("User-Agent", "capacity-test")
	if cookie != "" {
		req.Header.Set("Cookie", cookieName+"="+cookie)
	}
	if strings.HasSuffix(path, "/auth/login") {
		req.Header.Set(auth.LoadTestHeader, r.token)
	}
	res, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		msg := string(data)
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return res.Header, &httpError{status: res.StatusCode, body: msg}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return res.Header, err
		}
	}
	return res.Header, nil
}

// login signs in as a student would: a busy server (503) or the per-network
// limit (429) means wait a moment and try again, for up to two minutes.
func (r *runner) login(ctx context.Context, rec *recorder, email, ip string) (string, error) {
	t0 := time.Now()
	deadline := t0.Add(2 * time.Minute)
	for {
		h, err := r.call(ctx, "", ip, "POST", "/api/auth/login", map[string]string{"email": email, "password": r.password}, nil)
		switch st := statusOf(err); {
		case err == nil:
			for _, c := range (&http.Response{Header: h}).Cookies() {
				if c.Name == cookieName {
					if rec != nil {
						rec.ok("login", time.Since(t0))
					}
					return c.Value, nil
				}
			}
			return "", errors.New("signed in without a session cookie")
		case (st == 503 || st == 429) && time.Now().Before(deadline):
			if rec != nil {
				rec.retry(map[int]string{503: "login_busy", 429: "login_limited"}[st])
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(500+mrand.IntN(1500)) * time.Millisecond):
			}
		default:
			return "", err
		}
	}
}

// pageWeight downloads the app as a first visit would, compressed, and
// counts its size: the page and the scripts and styles it names.
func (r *runner) pageWeight(ctx context.Context) (float64, int) {
	c := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DisableCompression: true, TLSClientConfig: r.s.tls}}
	get := func(path string) ([]byte, int64) {
		req, err := http.NewRequestWithContext(ctx, "GET", r.s.target+path, nil)
		if err != nil {
			return nil, 0
		}
		req.Header.Set("Accept-Encoding", "br, gzip")
		res, err := c.Do(req)
		if err != nil {
			return nil, 0
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, 0
		}
		b, _ := io.ReadAll(res.Body)
		return b, int64(len(b))
	}
	html, size := get("/")
	if size == 0 || !bytes.Contains(html, []byte("_app/")) {
		return 0, 0
	}
	files, total := 1, size
	seen := map[string]bool{}
	for _, m := range assetRE.FindAllSubmatch(html, -1) {
		p := "/" + strings.TrimPrefix(string(m[1]), "/")
		if seen[p] {
			continue
		}
		seen[p] = true
		if _, n := get(p); n > 0 {
			files++
			total += n
		}
	}
	return round1(float64(total) / 1024), files
}

var assetRE = regexp.MustCompile(`(?:href|src|import\()\s*=?\s*"(/?_app/[^"]+\.(?:js|css))"`)

// ---------- the virtual class ----------

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// prepare makes the accounts, signs the teacher in and builds the quiz.
func (r *runner) prepare(ctx context.Context) error {
	r.password = randomHex(16)
	hash, err := r.s.auth.HashPassword(ctx, r.password)
	if err != nil {
		return err
	}
	tag := r.id[:8]
	most := r.plan.Steps[len(r.plan.Steps)-1]
	teacherEmail := "lt-" + tag + "-teacher@loadtest.invalid"
	rows := [][]any{{teacherEmail, "Capacity test teacher", "teacher", hash}}
	for i := 1; i <= most; i++ {
		e := fmt.Sprintf("lt-%s-%d@loadtest.invalid", tag, i)
		r.students = append(r.students, e)
		rows = append(rows, []any{e, fmt.Sprintf("Capacity test student %d", i), "student", hash})
	}
	err = pgx.BeginFunc(ctx, r.s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE lt_users (email text, name text, role text, password_hash text) ON COMMIT DROP`); err != nil {
			return err
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"lt_users"}, []string{"email", "name", "role", "password_hash"}, pgx.CopyFromRows(rows)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO auth.users(email, name, role, status, password_hash, email_verified_at, synthetic)
			SELECT email, name, role, 'active', password_hash, now(), true FROM lt_users`)
		return err
	})
	if err != nil {
		return err
	}
	if r.teacher, err = r.login(ctx, nil, teacherEmail, "198.19.0.1"); err != nil {
		return fmt.Errorf("the teacher couldn't sign in: %w", err)
	}
	var obj struct {
		ID string `json:"id"`
	}
	t := func(method, path string, body any) (string, error) {
		obj.ID = ""
		_, err := r.call(ctx, r.teacher, "198.19.0.1", method, path, body, &obj)
		if err != nil {
			return "", fmt.Errorf("%s %s: %w", method, path, err)
		}
		return obj.ID, nil
	}
	room, err := t("POST", "/api/teacher/classrooms", map[string]any{"name": "Capacity test " + tag, "settings": map[string]any{
		"admission_mode": "manual", "auto_enrol_on_join": true, "enrolment_approval": false, "student_id_required": false,
		"violation_policy": "log_only", "countdown_seconds": 5,
	}})
	if err != nil {
		return err
	}
	mod, err := t("POST", "/api/teacher/classrooms/"+room+"/modules", map[string]any{"name": "Capacity test"})
	if err != nil {
		return err
	}
	topic, err := t("POST", "/api/teacher/modules/"+mod+"/topics", map[string]any{"name": "Capacity test"})
	if err != nil {
		return err
	}
	if r.quizID, err = t("POST", "/api/teacher/topics/"+topic+"/quizzes", map[string]any{"title": "Capacity test"}); err != nil {
		return err
	}
	var csv strings.Builder
	csv.WriteString("question_code,type,question_text,options,correct_answer,marks,time_limit_sec\n")
	limit := max(60, r.plan.ThinkSeconds*6)
	for i := 1; i <= r.plan.Questions; i++ {
		fmt.Fprintf(&csv, "Q%d,SINGLE,Capacity test question %d: which is the largest?,%d|%d|%d|%d,%d,1,%d\n", i, i, i, i+1, i+2, i+3, i+3, limit)
	}
	if _, err := t("POST", "/api/teacher/quizzes/"+r.quizID+"/questions/import", []byte(csv.String())); err != nil {
		return err
	}
	_, err = t("POST", "/api/teacher/quizzes/"+r.quizID+"/status", map[string]any{"status": "ready"})
	return err
}

// state is what a student sees of their attempt.
type state struct {
	AttemptID string `json:"attempt_id"`
	State     string `json:"state"`
	Question  *struct {
		ID   string `json:"id"`
		Body struct {
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"body"`
	} `json:"question"`
}

// fakeIP is a student's internet address, from the range kept for
// benchmarks (198.18.0.0/15): one each, or one for all as behind a school's router.
func (r *runner) fakeIP(k int) string {
	if r.plan.OneNetwork {
		return "198.18.0.1"
	}
	return fmt.Sprintf("198.18.%d.%d", k/250, k%250+1)
}

func (r *runner) wsURL(path string) string {
	return "ws" + strings.TrimPrefix(r.s.target, "http") + path
}

func (r *runner) dial(ctx context.Context, cookie, ip, path string) (*websocket.Conn, error) {
	h := http.Header{}
	h.Set("Origin", r.s.origin)
	h.Set("Cookie", cookieName+"="+cookie)
	h.Set("X-Forwarded-For", ip)
	h.Set("User-Agent", "capacity-test")
	c, _, err := websocket.Dial(ctx, r.wsURL(path), &websocket.DialOptions{HTTPClient: r.wsClient, HTTPHeader: h})
	if err == nil {
		c.SetReadLimit(4 << 20)
	}
	return c, err
}

// runStep runs one class of n students through the quiz.
func (r *runner) runStep(parent context.Context, i, n int) (Step, error) {
	r.mu.Lock()
	r.step, r.n = i, n
	r.mu.Unlock()
	r.joined.Store(0)
	r.finished.Store(0)
	r.errs.Store(0)
	r.active.Store(0)
	r.saves.Store(0)
	r.setPhase("starting a session")

	var sess struct {
		ID       string `json:"id"`
		JoinCode string `json:"join_code"`
	}
	if _, err := r.call(parent, r.teacher, "198.19.0.1", "POST", "/api/teacher/quizzes/"+r.quizID+"/sessions", map[string]any{}, &sess); err != nil {
		return Step{}, fmt.Errorf("the session didn't start: %w", err)
	}
	limit := time.Duration(r.plan.RampSeconds+120+r.plan.Questions*r.plan.ThinkSeconds*3+120) * time.Second
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()

	rec := newRecorder()
	down0, up0 := r.m.down.Load(), r.m.up.Load()
	smp := &sampler{pool: r.s.pool, m: r.m, active: &r.active, saves: &r.saves, start: time.Now()}
	r.mu.Lock()
	r.smp = smp
	r.mu.Unlock()
	sctx, stopSampling := context.WithCancel(context.Background())
	go smp.run(sctx)
	t0 := time.Now()

	// The teacher watches the dashboard live.
	var drops atomic.Int64
	ending := make(chan struct{})
	if tc, err := r.dial(ctx, r.teacher, "198.19.0.1", "/ws/sessions/"+sess.ID); err == nil {
		go keepAlive(ctx, tc, ending)
		go func() {
			defer tc.CloseNow()
			for {
				if _, _, err := tc.Read(ctx); err != nil {
					select {
					case <-ending:
					default:
						if ctx.Err() == nil {
							drops.Add(1)
							rec.fail("connect", "the teacher's dashboard connection closed: "+err.Error())
						}
					}
					return
				}
			}
		}()
	} else {
		rec.fail("connect", "the teacher's dashboard didn't connect: "+err.Error())
	}

	r.setPhase("students joining and answering")
	var wg sync.WaitGroup
	var completed atomic.Int64
	for k := 0; k < n; k++ {
		wg.Add(1)
		delay := time.Duration(0)
		if n > 1 {
			delay = time.Duration(float64(r.plan.RampSeconds) * float64(time.Second) * float64(k) / float64(n-1))
		}
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			if r.student(ctx, rec, k, sess.JoinCode, ending, &drops) {
				completed.Add(1)
			}
		}()
	}
	// The teacher admits whoever is waiting, every two seconds.
	allDone := make(chan struct{})
	go func() { wg.Wait(); close(allDone) }()
	tick := time.NewTicker(2 * time.Second)
admitting:
	for {
		select {
		case <-allDone:
			break admitting
		case <-ctx.Done():
			break admitting
		case <-tick.C:
			if r.joined.Load() == 0 {
				continue
			}
			a0 := time.Now()
			if _, err := r.call(ctx, r.teacher, "198.19.0.1", "POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"all": true}, nil); err != nil {
				if ctx.Err() == nil {
					rec.fail("admit", err.Error())
				}
			} else {
				rec.ok("admit", time.Since(a0))
			}
		}
	}
	tick.Stop()
	timedOut := ctx.Err() != nil && parent.Err() == nil
	cancel()
	<-allDone
	close(ending)
	r.setPhase("ending the session")
	ectx, ecancel := context.WithTimeout(context.Background(), 30*time.Second)
	_, _ = r.call(ectx, r.teacher, "198.19.0.1", "POST", "/api/teacher/sessions/"+sess.ID+"/end", map[string]any{}, nil)
	ecancel()
	stopSampling()
	if parent.Err() != nil {
		return Step{}, parent.Err()
	}

	st := Step{Students: n, Seconds: round1(time.Since(t0).Seconds()), Joined: int(r.joined.Load()), Completed: int(completed.Load()),
		Ops: rec.latencies(), Drops: int(drops.Load()), Samples: smp.all()}
	st.Errors, st.FirstError = rec.failures()
	down, up := float64(r.m.down.Load()-down0), float64(r.m.up.Load()-up0)
	st.DownMB, st.UpMB = round1(down/1e6), round1(up/1e6)
	st.KBPerStudent = round1(down / 1024 / float64(n))
	summariseSamples(&st)
	judge(&st, timedOut)
	return st, nil
}

// student is one virtual student: sign in, join, wait to be let in, answer
// every question after thinking a moment, submit. It reports whether they finished.
func (r *runner) student(ctx context.Context, rec *recorder, k int, code string, ending chan struct{}, drops *atomic.Int64) bool {
	ip := r.fakeIP(k)
	fail := func(op string, err error) bool {
		if ctx.Err() == nil {
			r.errs.Add(1)
			rec.fail(op, err.Error())
		}
		return false
	}
	cookie, err := r.login(ctx, rec, r.students[k], ip)
	if err != nil {
		return fail("login", err)
	}
	var st state
	t0 := time.Now()
	if _, err := r.call(ctx, cookie, ip, "POST", "/api/join/sessions/"+code, map[string]any{}, &st); err != nil {
		return fail("join", err)
	}
	rec.ok("join", time.Since(t0))
	r.joined.Add(1)

	t0 = time.Now()
	c, err := r.dial(ctx, cookie, ip, "/ws/attempts/"+st.AttemptID)
	if err != nil {
		return fail("connect", err)
	}
	rec.ok("connect", time.Since(t0))
	r.active.Add(1)
	finished := make(chan struct{})
	defer func() {
		close(finished)
		r.active.Add(-1)
		c.Close(websocket.StatusNormalClosure, "")
	}()
	admitted := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				select {
				case <-finished:
				case <-ending:
				default:
					if ctx.Err() == nil {
						drops.Add(1)
						rec.fail("connect", "a student's live connection closed: "+err.Error())
					}
				}
				return
			}
			var m state
			if json.Unmarshal(data, &m) == nil && (m.State == "admitted" || m.State == "in_progress") {
				once.Do(func() { close(admitted) })
			}
		}
	}()
	go keepAlive(ctx, c, finished)
	if st.State == "waiting" {
		// Admitted arrives on the live connection; look now and then too, as the app does after a reconnect.
		look := time.NewTicker(10 * time.Second)
	waiting:
		for {
			select {
			case <-admitted:
				break waiting
			case <-ctx.Done():
				look.Stop()
				return false
			case <-look.C:
				var now state
				if _, err := r.call(ctx, cookie, ip, "GET", "/api/attempts/"+st.AttemptID, nil, &now); err == nil && now.State != "waiting" {
					break waiting
				}
			}
		}
		look.Stop()
	}
	t0 = time.Now()
	if _, err := r.call(ctx, cookie, ip, "POST", "/api/attempts/"+st.AttemptID+"/start", nil, &st); err != nil && statusOf(err) != 409 {
		return fail("start", err)
	}
	rec.ok("start", time.Since(t0))
	think := float64(r.plan.ThinkSeconds) * float64(time.Second)
	for st.State == "in_progress" && st.Question != nil {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Duration(think * (0.5 + mrand.Float64()))):
		}
		q := st.Question
		answer := []string{}
		if len(q.Body.Options) > 0 {
			answer = []string{q.Body.Options[mrand.IntN(len(q.Body.Options))].ID}
		}
		t0 = time.Now()
		if _, err := r.call(ctx, cookie, ip, "PUT", "/api/attempts/"+st.AttemptID+"/answers/"+q.ID, map[string]any{"response": map[string]any{"selected": answer}, "seq": time.Now().UnixMilli()}, nil); err != nil {
			return fail("save", err)
		}
		rec.ok("save", time.Since(t0))
		r.saves.Add(1)
		t0 = time.Now()
		if _, err := r.call(ctx, cookie, ip, "POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]any{"question_id": q.ID}, &st); err != nil {
			return fail("advance", err)
		}
		rec.ok("advance", time.Since(t0))
	}
	if st.State != "submitted" {
		t0 = time.Now()
		if _, err := r.call(ctx, cookie, ip, "POST", "/api/attempts/"+st.AttemptID+"/submit", nil, &st); err != nil {
			return fail("submit", err)
		}
		rec.ok("submit", time.Since(t0))
	}
	r.finished.Add(1)
	return true
}

// keepAlive pings every 10 s, as the app does; the server closes a quiet
// connection after 45 s.
func keepAlive(ctx context.Context, c *websocket.Conn, done chan struct{}) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`))
		}
	}
}

func summariseSamples(st *Step) {
	st.CPUPeak, st.CPUAvg, st.MemFreeMin, st.Load1Peak = -1, -1, -1, -1
	var cpuSum float64
	var cpuN int
	for _, x := range st.Samples {
		if x.CPU >= 0 {
			st.CPUPeak = max(st.CPUPeak, x.CPU)
			cpuSum += x.CPU
			cpuN++
		}
		if x.MemFreeMB >= 0 && (st.MemFreeMin < 0 || x.MemFreeMB < st.MemFreeMin) {
			st.MemFreeMin = x.MemFreeMB
		}
		st.Load1Peak = max(st.Load1Peak, x.Load1)
		st.HeapPeakMB = max(st.HeapPeakMB, x.HeapMB)
		st.DBBusyPeak = max(st.DBBusyPeak, x.DBBusy)
		st.DBWaits += x.DBWaits
		st.PeakDownMbps = max(st.PeakDownMbps, x.DownMbps)
		st.SavesPerSec = max(st.SavesPerSec, float64(x.Saves))
	}
	if cpuN > 0 {
		st.CPUAvg = round1(cpuSum / float64(cpuN))
	}
	// A long step keeps one point in every few, enough for a chart.
	if len(st.Samples) > 300 {
		every := (len(st.Samples) + 299) / 300
		kept := st.Samples[:0]
		for i, x := range st.Samples {
			if i%every == 0 {
				kept = append(kept, x)
			}
		}
		st.Samples = kept
	}
}

// judge decides whether a step passed, and says why not.
func judge(st *Step, timedOut bool) {
	th := Thresholds
	var why []string
	if timedOut {
		why = append(why, "the class didn't finish in time")
	}
	if c := float64(st.Completed) / float64(st.Students); c < th.Completion {
		why = append(why, fmt.Sprintf("only %d of %d students finished", st.Completed, st.Students))
	}
	if l := st.Ops["save"]; l.N > 0 && l.P95 > th.SaveP95 {
		why = append(why, fmt.Sprintf("saving an answer took %.0f ms for 1 in 20 (limit %.0f ms)", l.P95, th.SaveP95))
	}
	if l := st.Ops["save"]; l.N > 0 && l.P99 > th.SaveP99 {
		why = append(why, fmt.Sprintf("saving an answer took %.0f ms for 1 in 100 (limit %.0f ms)", l.P99, th.SaveP99))
	}
	if l := st.Ops["login"]; l.N > 0 && l.P95 > th.LoginP95 {
		why = append(why, fmt.Sprintf("signing in took %.1f s for 1 in 20 (limit %.0f s)", l.P95/1000, th.LoginP95/1000))
	}
	if l := st.Ops["join"]; l.N > 0 && l.P95 > th.JoinP95 {
		why = append(why, fmt.Sprintf("joining took %.1f s for 1 in 20 (limit %.0f s)", l.P95/1000, th.JoinP95/1000))
	}
	var ops, errs int
	for _, l := range st.Ops {
		ops += l.N
	}
	for k, n := range st.Errors {
		if k != "login_busy" && k != "login_limited" {
			errs += n
		}
	}
	if ops+errs > 0 && float64(errs)/float64(ops+errs) > th.ErrorRate {
		why = append(why, fmt.Sprintf("%d of %d requests failed", errs, ops+errs))
	}
	if st.Drops > 0 {
		why = append(why, fmt.Sprintf("%d live connections dropped", st.Drops))
	}
	st.Passed, st.Reasons = len(why) == 0, why
}
