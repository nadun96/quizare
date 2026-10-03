package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app"
	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/config"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/quiz"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

// clock is a controllable time source shared with the service.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type fixture struct {
	e       *apptest.Env
	teacher *apptest.Client
	quiz    quiz.Quiz
	sess    live.Session
	clk     *clock
}

const threeQuestions = `question_code,type,question_text,options,correct_answer,marks,time_limit_sec
Q1,SINGLE,Capital of France?,Paris|Rome|Madrid,Paris,1,30
Q2,MULTI,Primes,2|3|4,2|3,2,
Q3,ESSAY,Explain,,,5,
`

func setup(t *testing.T, classroomSettings, quizSettings, sessionSettings map[string]any) *fixture {
	t.Helper()
	e := apptest.New(t)
	clk := &clock{t: time.Now().Truncate(time.Second)}
	e.App.Live.SetClock(clk.now)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C", "settings": classroomSettings}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Quiz", "settings": quizSettings}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(threeQuestions))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess live.Session
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{"settings": sessionSettings}, 201, &sess)
	return &fixture{e: e, teacher: teacher, quiz: q, sess: sess, clk: clk}
}

func (f *fixture) join(t *testing.T) (*apptest.Client, live.StudentState) {
	t.Helper()
	s := f.e.NewUser(auth.RoleStudent)
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}, 200, &st)
	return s, st
}

func (f *fixture) admit(t *testing.T, ids ...string) {
	t.Helper()
	body := map[string]any{"all": len(ids) == 0, "attempt_ids": ids}
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/admit", body, 200, nil)
}

func state(t *testing.T, s *apptest.Client, attemptID string) live.StudentState {
	t.Helper()
	var st live.StudentState
	s.Call("GET", "/api/attempts/"+attemptID, nil, 200, &st)
	return st
}

func (f *fixture) tick(t *testing.T) {
	t.Helper()
	if err := f.e.App.Live.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionHasJoinLink(t *testing.T) {
	f := setup(t, nil, nil, nil)
	if len(f.sess.JoinCode) != 6 || f.sess.JoinURL != f.e.Server.URL+"/j/"+f.sess.JoinCode || f.sess.Status != "open" {
		t.Fatalf("session = %+v", f.sess)
	}
	// Only Ready quizzes can run.
	f.teacher.Call("POST", "/api/teacher/quizzes/"+f.quiz.ID+"/status", map[string]any{"status": "draft"}, 200, nil)
	f.teacher.Call("POST", "/api/teacher/quizzes/"+f.quiz.ID+"/sessions", map[string]any{}, 409, nil)
}

// AC-01, AC-02: join → waiting room → admit → 60 s countdown → first question appears automatically.
func TestJoinAdmitCountdownAutoStart(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, st := f.join(t)
	if st.State != live.StateWaiting || st.Question != nil || st.Total != 3 {
		t.Fatalf("after join: %+v", st)
	}
	var again live.StudentState // BR-01: rejoining returns the same attempt
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}, 200, &again)
	if again.AttemptID != st.AttemptID {
		t.Fatal("second attempt created")
	}
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 409, nil) // BR-05: not admitted yet

	var d live.Dashboard
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 200, &d)
	if d.Counts["waiting"] != 1 || d.Rows[0].Name == "" {
		t.Fatalf("dashboard = %+v", d)
	}

	f.admit(t)
	st = state(t, s, st.AttemptID)
	if st.State != live.StateAdmitted || *st.CountdownDeadline != f.clk.now().Add(60*time.Second).UnixMilli() {
		t.Fatalf("after admit: %+v", st)
	}
	f.clk.add(59 * time.Second)
	f.tick(t)
	if state(t, s, st.AttemptID).State != live.StateAdmitted {
		t.Fatal("started before the countdown ended")
	}
	f.clk.add(time.Second)
	f.tick(t)
	st = state(t, s, st.AttemptID)
	if st.State != live.StateInProgress || st.Question == nil || st.Question.Code != "Q1" || st.Index != 0 {
		t.Fatalf("after countdown: %+v", st)
	}
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 200, &d)
	if d.Session.Status != "live" || d.Counts["in_progress"] != 1 {
		t.Fatalf("dashboard after start = %+v", d.Counts)
	}
}

// AC-03 early start; FR-SS-06 countdown changed per session.
func TestEarlyStartAndCountdownSetting(t *testing.T) {
	f := setup(t, nil, nil, map[string]any{"countdown_seconds": 10})
	s, st := f.join(t)
	f.admit(t)
	if got := *state(t, s, st.AttemptID).CountdownDeadline; got != f.clk.now().Add(10*time.Second).UnixMilli() {
		t.Fatalf("countdown deadline %d", got)
	}
	var started live.StudentState
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &started)
	if started.State != live.StateInProgress || started.Question == nil {
		t.Fatalf("early start: %+v", started)
	}
	// UC-02 5b: change the countdown before admitting the next group.
	f.teacher.Call("PUT", "/api/teacher/sessions/"+f.sess.ID+"/settings", map[string]any{"countdown_seconds": 30}, 200, nil)
	s2, st2 := f.join(t)
	f.admit(t)
	if got := *state(t, s2, st2.AttemptID).CountdownDeadline; got != f.clk.now().Add(30*time.Second).UnixMilli() {
		t.Fatalf("new countdown not applied: %d", got)
	}
	f.teacher.Call("PUT", "/api/teacher/sessions/"+f.sess.ID+"/settings", map[string]any{"pass_mark_pct": 10}, 422, nil)
}

func running(t *testing.T, f *fixture) (*apptest.Client, live.StudentState) {
	t.Helper()
	s, st := f.join(t)
	f.admit(t, st.AttemptID)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	return s, st
}

func save(s *apptest.Client, st live.StudentState, r quiz.Response, seq int64) (int, []byte) {
	return s.Do("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": r, "seq": seq})
}

// AC-04: when a question's limit passes, the answer so far is kept and the next question appears.
func TestQuestionTimerAdvancesAndKeepsAnswer(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, st := running(t, f)
	if st.QuestionDeadline == nil || *st.QuestionDeadline != f.clk.now().Add(30*time.Second).UnixMilli() {
		t.Fatalf("question deadline: %v", st.QuestionDeadline)
	}
	if code, body := save(s, st, quiz.Response{Selected: []string{"o2"}}, 1); code != 200 {
		t.Fatalf("save: %d %s", code, body)
	}
	if code, _ := save(s, st, quiz.Response{Selected: []string{"o3"}}, 0); code != 200 {
		t.Fatal("stale save should be accepted but ignored")
	}
	f.clk.add(31 * time.Second) // inside grace: still accepted
	if code, _ := save(s, st, quiz.Response{Selected: []string{"o1"}}, 2); code != 200 {
		t.Fatal("save within grace rejected")
	}
	f.clk.add(2 * time.Second)
	f.tick(t)
	next := state(t, s, st.AttemptID)
	if next.Index != 1 || next.Question.Code != "Q2" || next.QuestionDeadline != nil {
		t.Fatalf("not advanced: %+v", next)
	}
	if code, _ := save(s, st, quiz.Response{Selected: []string{"o2"}}, 3); code != 409 {
		t.Fatal("one-way navigation: saving a past question must fail (BR-09)")
	}
	var raw []byte
	var sn *string
	f.e.Pool.QueryRow(context.Background(), `SELECT response, student_number FROM live.answers WHERE attempt_id=$1 AND question_id=$2`, st.AttemptID, st.Question.ID).Scan(&raw, &sn)
	if !strings.Contains(string(raw), "o1") {
		t.Fatalf("saved answer = %s", raw)
	}
}

// AC-05: a quiz limit submits the attempt whatever question is open.
func TestQuizTimerSubmits(t *testing.T) {
	f := setup(t, nil, map[string]any{"quiz_time_limit_sec": 600}, nil)
	s, st := running(t, f)
	f.clk.add(31 * time.Second)
	f.tick(t) // question 1 times out
	f.clk.add(569 * time.Second)
	f.tick(t)
	if state(t, s, st.AttemptID).State != live.StateInProgress {
		t.Fatal("submitted before the limit + grace")
	}
	f.clk.add(live.Grace)
	f.tick(t)
	if got := state(t, s, st.AttemptID); got.State != live.StateSubmitted || got.Question != nil {
		t.Fatalf("after limit: %+v", got)
	}
}

func TestSessionDurationOverridesQuiz(t *testing.T) {
	f := setup(t, nil, map[string]any{"quiz_time_limit_sec": 600}, map[string]any{"quiz_time_limit_sec": 120})
	_, st := running(t, f)
	if *st.QuizDeadline != f.clk.now().Add(120*time.Second).UnixMilli() {
		t.Fatal("session-level duration must override the quiz's (BA §7)")
	}
}

func TestAdvanceThroughAllQuestionsSubmits(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, st := running(t, f)
	for i := 0; i < 3; i++ {
		qid := st.Question.ID
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": qid}, 200, &st)
		// A retried advance for the same question is a no-op.
		var again live.StudentState
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": qid}, 200, &again)
		if again.Index != st.Index {
			t.Fatal("advance retry skipped a question")
		}
	}
	if st.State != live.StateSubmitted {
		t.Fatalf("state = %s", st.State)
	}
}

func TestStateNeverContainsAnswerKey(t *testing.T) {
	f := setup(t, nil, map[string]any{"option_order": "shuffled", "question_order": "shuffled"}, nil)
	s, st := running(t, f)
	for i := 0; i < 3; i++ {
		_, raw := s.Do("GET", "/api/attempts/"+st.AttemptID, nil)
		for _, leak := range []string{`"key"`, `"correct"`, `"rubric"`, `"feedback"`} {
			if strings.Contains(string(raw), leak) {
				t.Fatalf("state leaks %s: %s", leak, raw)
			}
		}
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
	}
}

// AC-06: tab switch under "invalidate" → invalidated, logged, teacher alerted within 2 s.
func TestTabSwitchInvalidatesAndAlertsTeacher(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, st := running(t, f)
	tws := f.teacher.Dial("/ws/sessions/" + f.sess.ID)
	apptest.ReadUntil(t, tws, "dashboard", 2*time.Second)
	sws := s.Dial("/ws/attempts/" + st.AttemptID)
	apptest.ReadUntil(t, sws, "state", 2*time.Second)

	s.Call("POST", "/api/attempts/"+st.AttemptID+"/violations", map[string]any{"kind": "tab_hidden", "client_ts": time.Now().UnixMilli()}, 204, nil)
	alert := apptest.ReadUntil(t, tws, "alert", 2*time.Second)
	if alert["kind"] != "tab_hidden" || alert["action"] != "invalidated" || alert["student_name"] == "" {
		t.Fatalf("alert = %v", alert)
	}
	msg := apptest.ReadUntil(t, sws, "state", 2*time.Second)
	if msg["state"] != live.StateInvalidated || msg["question"] != nil {
		t.Fatalf("student state = %v", msg)
	}
	var n int
	var device string
	f.e.Pool.QueryRow(context.Background(), `SELECT count(*), max(device) FROM live.violations WHERE attempt_id=$1`, st.AttemptID).Scan(&n, &device)
	if n != 1 || device == "" {
		t.Fatalf("violations logged = %d device=%q", n, device)
	}
	if code, _ := save(s, st, quiz.Response{Selected: []string{"o1"}}, 1); code != 409 {
		t.Fatal("invalidated attempt accepted an answer")
	}
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/violations", map[string]any{"kind": "made_up"}, 422, nil)
}

func TestWarnThenInvalidateAndLogOnly(t *testing.T) {
	f := setup(t, nil, map[string]any{"violation_policy": "warn_then_invalidate", "allowed_warnings": 1}, nil)
	s, st := running(t, f)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/violations", map[string]any{"kind": "window_blur"}, 204, nil)
	got := state(t, s, st.AttemptID)
	if got.State != live.StateInProgress || got.Warnings != 1 {
		t.Fatalf("first violation should warn: %+v", got)
	}
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/violations", map[string]any{"kind": "window_blur"}, 204, nil)
	if got := state(t, s, st.AttemptID); got.State != live.StateInvalidated {
		t.Fatalf("second violation should invalidate: %+v", got)
	}

	// Session-level policy overrides the quiz's.
	f2 := setup(t, nil, map[string]any{"violation_policy": "invalidate"}, map[string]any{"violation_policy": "log_only"})
	s2, st2 := running(t, f2)
	for i := 0; i < 3; i++ {
		s2.Call("POST", "/api/attempts/"+st2.AttemptID+"/violations", map[string]any{"kind": "tab_hidden"}, 204, nil)
	}
	if got := state(t, s2, st2.AttemptID); got.State != live.StateInProgress {
		t.Fatalf("log_only invalidated: %+v", got)
	}
}

func TestBeaconViolation(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, st := running(t, f)
	// sendBeacon: text/plain body, no custom header, but same Origin.
	status, _ := s.Raw("POST", "/beacon/attempts/"+st.AttemptID+"/violations", "text/plain", []byte(`{"kind":"page_close"}`))
	if status != 204 {
		t.Fatalf("beacon status %d", status)
	}
	if got := state(t, s, st.AttemptID); got.State != live.StateInvalidated || got.InvalidReason != "page_close" {
		t.Fatalf("after beacon: %+v", got)
	}
}

// AC-07: pausing a group hides questions and stops their timers; others continue.
func TestPauseGroupAC07(t *testing.T) {
	f := setup(t, nil, map[string]any{"quiz_time_limit_sec": 600}, nil)
	s1, st1 := running(t, f)
	s2, st2 := running(t, f)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/pause", map[string]any{"attempt_ids": []string{st1.AttemptID}}, 200, nil)
	p := state(t, s1, st1.AttemptID)
	if p.State != live.StatePaused || p.Question != nil || *p.QuizRemainingMs != 600_000 {
		t.Fatalf("paused: %+v", p)
	}
	if code, _ := save(s1, st1, quiz.Response{Selected: []string{"o1"}}, 1); code != 409 {
		t.Fatal("paused attempt accepted an answer")
	}
	if state(t, s2, st2.AttemptID).State != live.StateInProgress {
		t.Fatal("other students must continue")
	}
	f.clk.add(300 * time.Second)
	f.tick(t)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/resume", map[string]any{"all": true}, 200, nil)
	r := state(t, s1, st1.AttemptID)
	if r.State != live.StateInProgress || *r.QuizDeadline != f.clk.now().Add(600*time.Second).UnixMilli() {
		t.Fatalf("paused time counted: %+v", r)
	}
}

// AC-08: a student with 2 minutes left extended by 5 has 7; others unchanged.
func TestExtendAC08AndSessionWide(t *testing.T) {
	f := setup(t, nil, map[string]any{"quiz_time_limit_sec": 600}, nil)
	s1, st1 := running(t, f)
	s2, st2 := running(t, f)
	f.clk.add(480 * time.Second)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/extend", map[string]any{"attempt_ids": []string{st1.AttemptID}, "seconds": 300}, 200, nil)
	if got := *state(t, s1, st1.AttemptID).QuizDeadline - f.clk.now().UnixMilli(); got != 7*60*1000 {
		t.Fatalf("remaining = %d ms", got)
	}
	if got := *state(t, s2, st2.AttemptID).QuizDeadline - f.clk.now().UnixMilli(); got != 2*60*1000 {
		t.Fatalf("other student changed: %d ms", got)
	}
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/extend", map[string]any{"all": true, "seconds": 60}, 200, nil)
	if got := *state(t, s2, st2.AttemptID).QuizDeadline - f.clk.now().UnixMilli(); got != 3*60*1000 {
		t.Fatalf("session-wide extension: %d ms", got)
	}
	// A student who starts later also gets the session-wide extension.
	_, st3 := running(t, f)
	if *st3.QuizDeadline != f.clk.now().Add(660*time.Second).UnixMilli() {
		t.Fatal("late starter missed the session-wide extension")
	}
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/extend", map[string]any{"all": true, "seconds": 0}, 422, nil)
}

// FR-SS-11, BR-04: ending submits running attempts and stops the QR working.
func TestEndSession(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s1, st1 := running(t, f)
	s2, st2 := f.join(t)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/end", nil, 204, nil)
	if got := state(t, s1, st1.AttemptID).State; got != live.StateSubmitted {
		t.Fatalf("running attempt = %s", got)
	}
	if got := state(t, s2, st2.AttemptID).State; got != live.StateNotStarted {
		t.Fatalf("waiting attempt = %s", got)
	}
	f.e.NewUser(auth.RoleStudent).Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}, 410, nil)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/admit", map[string]any{"all": true}, 410, nil)

	var ev struct{ Events []live.Event }
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/events", nil, 200, &ev)
	kinds := map[string]int{}
	for _, e := range ev.Events {
		kinds[e.Kind]++
	}
	if kinds["joined"] != 2 || kinds["admitted"] != 1 || kinds["started"] != 1 || kinds["session_ended"] != 1 {
		t.Fatalf("integrity log = %v", kinds)
	}
}

// FR-SS-12, BR-08: only the owning teacher reinstates, with a logged reason.
func TestReinstate(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, st := running(t, f)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/violations", map[string]any{"kind": "tab_hidden"}, 204, nil)
	f.teacher.Call("POST", "/api/teacher/attempts/"+st.AttemptID+"/reinstate", map[string]string{"reason": ""}, 422, nil)
	other := f.e.NewUser(auth.RoleTeacher)
	other.Call("POST", "/api/teacher/attempts/"+st.AttemptID+"/reinstate", map[string]string{"reason": "x"}, 404, nil)
	f.teacher.Call("POST", "/api/teacher/attempts/"+st.AttemptID+"/reinstate", map[string]string{"reason": "Phone call from parent"}, 200, nil)
	if got := state(t, s, st.AttemptID); got.State != live.StateInProgress || got.Question == nil {
		t.Fatalf("after reinstate: %+v", got)
	}
	var n int
	f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit.events WHERE action='attempt_reinstated' AND target_id=$1`, st.AttemptID).Scan(&n)
	if n != 1 {
		t.Fatal("reinstatement not audited")
	}
	f.teacher.Call("POST", "/api/teacher/attempts/"+st.AttemptID+"/reinstate", map[string]string{"reason": "again"}, 409, nil)
}

// Heartbeat rule: a socket that closes and does not return within the grace counts as a violation.
func TestDisconnectWithoutReconnectIsViolation(t *testing.T) {
	f := setup(t, nil, map[string]any{"disconnect_grace_sec": 10}, nil)
	s, st := running(t, f)
	conn := s.Dial("/ws/attempts/" + st.AttemptID)
	apptest.ReadUntil(t, conn, "state", 2*time.Second)
	conn.CloseNow()
	waitFor(t, func() bool {
		var n int
		f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM live.attempts WHERE id=$1 AND disconnected_at IS NOT NULL`, st.AttemptID).Scan(&n)
		return n == 1
	})
	f.clk.add(9 * time.Second)
	f.tick(t)
	if state(t, s, st.AttemptID).State != live.StateInProgress {
		t.Fatal("invalidated inside the grace period")
	}
	f.clk.add(2 * time.Second)
	f.tick(t)
	if got := state(t, s, st.AttemptID); got.State != live.StateInvalidated || got.InvalidReason != "disconnected" {
		t.Fatalf("after grace: %+v", got)
	}

	// Reconnecting inside the grace clears it.
	s2, st2 := running(t, f)
	c2 := s2.Dial("/ws/attempts/" + st2.AttemptID)
	apptest.ReadUntil(t, c2, "state", 2*time.Second)
	c2.CloseNow()
	time.Sleep(200 * time.Millisecond)
	c3 := s2.Dial("/ws/attempts/" + st2.AttemptID)
	apptest.ReadUntil(t, c3, "state", 2*time.Second)
	f.clk.add(30 * time.Second)
	f.tick(t)
	if got := state(t, s2, st2.AttemptID); got.State != live.StateInProgress {
		t.Fatalf("reconnected student invalidated: %+v", got)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestAutoAdmitAndStudentIDOnAnswers(t *testing.T) {
	f := setup(t, map[string]any{"student_id_required": true}, nil, map[string]any{"admission_mode": "auto"})
	s := f.e.NewUser(auth.RoleStudent)
	var p live.JoinPreview
	s.Call("GET", "/api/join/sessions/"+f.sess.JoinCode, nil, 200, &p)
	if !p.StudentIDRequired || p.Attempt != nil {
		t.Fatalf("preview = %+v", p)
	}
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}, 422, nil)
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{"student_number": "S-42"}, 200, &st)
	if st.State != live.StateAdmitted || *st.StudentNumber != "S-42" {
		t.Fatalf("auto-admit: %+v", st)
	}
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	save(s, st, quiz.Response{Selected: []string{"o1"}}, 1)
	var sn string
	f.e.Pool.QueryRow(context.Background(), `SELECT student_number FROM live.answers WHERE attempt_id=$1`, st.AttemptID).Scan(&sn)
	if sn != "S-42" {
		t.Fatalf("BR-03: answer student number = %q", sn)
	}
}

func TestSnapshotIsolatesSessionFromEdits(t *testing.T) {
	f := setup(t, nil, nil, nil)
	var qs struct{ Questions []quiz.Question }
	f.teacher.Call("GET", "/api/teacher/quizzes/"+f.quiz.ID+"/questions", nil, 200, &qs)
	q1 := qs.Questions[0]
	body, _ := json.Marshal(q1)
	var in map[string]any
	json.Unmarshal(body, &in)
	in["text"] = "EDITED"
	delete(in, "id")
	delete(in, "quiz_id")
	delete(in, "position")
	delete(in, "resources")
	f.teacher.Call("PUT", "/api/teacher/questions/"+q1.ID, in, 200, nil)
	_, st := running(t, f)
	if st.Question.Text == "EDITED" {
		t.Fatal("running session picked up a quiz edit (ADR-14)")
	}
	// BR-16: a quiz with sessions is archived, not deleted.
	var del map[string]bool
	f.teacher.Call("DELETE", "/api/teacher/quizzes/"+f.quiz.ID, nil, 200, &del)
	if !del["archived"] {
		t.Fatalf("delete = %v", del)
	}
}

func TestTeacherIsolation(t *testing.T) {
	f := setup(t, nil, nil, nil)
	_, st := f.join(t)
	other := f.e.NewUser(auth.RoleTeacher)
	other.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 404, nil)
	other.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/admit", map[string]any{"all": true}, 404, nil)
	other.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/end", nil, 404, nil)
	// Another student cannot see or act on someone else's attempt.
	thief := f.e.NewUser(auth.RoleStudent)
	thief.Call("GET", "/api/attempts/"+st.AttemptID, nil, 404, nil)
	thief.Call("POST", "/api/attempts/"+st.AttemptID+"/violations", map[string]any{"kind": "tab_hidden"}, 404, nil)
	// Teachers cannot join as students.
	f.teacher.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}, 403, nil)
}

// Chaos: a fresh process (empty cache) rehydrates sessions from Postgres
// and keeps enforcing deadlines (architecture §2.3 step 6, R3).
func TestRestartRehydrates(t *testing.T) {
	f := setup(t, nil, map[string]any{"quiz_time_limit_sec": 100}, nil)
	s, st := running(t, f)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/pause", map[string]any{"all": true}, 200, nil)

	cfg := config.Config{BaseURL: f.e.Server.URL, Argon2Workers: 1}
	restarted, err := app.Build(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), f.e.Pool, app.Options{KEK: bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	restarted.Live.SetClock(f.clk.now)
	f.clk.add(time.Hour) // a restart during a pause must not burn time (ADR-05 rule)
	if err := restarted.Live.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := state(t, s, st.AttemptID); got.State != live.StatePaused || *got.QuizRemainingMs != 100_000 {
		t.Fatalf("after restart during pause: %+v", got)
	}
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/resume", map[string]any{"all": true}, 200, nil)
	f.clk.add(103 * time.Second)
	if err := restarted.Live.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := state(t, s, st.AttemptID); got.State != live.StateSubmitted {
		t.Fatalf("restarted process did not enforce the deadline: %s", got.State)
	}
}

// UC-02 step 6: the session ends by itself once every attempt is finished and it stays quiet.
func TestAutoEnd(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, st := running(t, f)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/submit", nil, 200, nil)
	f.tick(t)
	var d live.Dashboard
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 200, &d)
	if d.Session.Status != "live" {
		t.Fatal("ended immediately")
	}
	// updated_at is DB time; move the app clock past the quiet window.
	f.clk.add(live.AutoEndQuiet + time.Minute)
	f.tick(t)
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 200, &d)
	if d.Session.Status != "ended" {
		t.Fatalf("status = %s", d.Session.Status)
	}
}
