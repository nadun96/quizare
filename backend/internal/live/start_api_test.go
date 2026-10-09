package live_test

import (
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/live"
)

func (f *fixture) start(t *testing.T, body map[string]any) int {
	t.Helper()
	var out struct{ Affected int }
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/start", body, 200, &out)
	return out.Affected
}

// D-53: with start_mode=teacher, admitted students wait (no countdown, the
// ticker never starts them, they can't start themselves) until the teacher
// presses Start; the countdown runs from then. Starting for everyone lets
// later students count down by themselves.
func TestTeacherStartsTheQuiz(t *testing.T) {
	f := setup(t, nil, nil, map[string]any{"start_mode": "teacher", "countdown_seconds": 10})
	s1, a1 := f.join(t)
	s2, a2 := f.join(t)
	f.admit(t)
	st := state(t, s1, a1.AttemptID)
	if st.State != live.StateAdmitted || st.CountdownDeadline != nil {
		t.Fatalf("admitted student should wait for the teacher: %+v", st)
	}
	f.clk.add(time.Hour)
	f.tick(t)
	if state(t, s1, a1.AttemptID).State != live.StateAdmitted {
		t.Fatal("the ticker started a student who waits for the teacher")
	}
	s1.Call("POST", "/api/attempts/"+a1.AttemptID+"/start", nil, 409, nil)
	var d live.Dashboard
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 200, &d)
	if d.StartMode != "teacher" || d.Rows[0].CountdownDeadline != nil || d.Session.StartedAt != nil {
		t.Fatalf("dashboard before start: mode %q, row %+v", d.StartMode, d.Rows[0])
	}

	// Start for one student: their countdown runs; the other still waits.
	if n := f.start(t, map[string]any{"attempt_ids": []string{a1.AttemptID}}); n != 1 {
		t.Fatalf("start one: %d", n)
	}
	if got := state(t, s1, a1.AttemptID).CountdownDeadline; got == nil || *got != f.clk.now().Add(10*time.Second).UnixMilli() {
		t.Fatalf("countdown after teacher start: %v", got)
	}
	if state(t, s2, a2.AttemptID).CountdownDeadline != nil {
		t.Fatal("starting one student started another")
	}
	// Now that the countdown runs, the student may start early (AC-03), and the ticker starts at zero.
	f.clk.add(10 * time.Second)
	f.tick(t)
	if state(t, s1, a1.AttemptID).State != live.StateInProgress {
		t.Fatal("countdown didn't start the quiz")
	}

	// Start everyone now: no countdown at all.
	if n := f.start(t, map[string]any{"all": true, "now": true}); n != 1 {
		t.Fatalf("start all now: %d", n)
	}
	if st := state(t, s2, a2.AttemptID); st.State != live.StateInProgress || st.Question == nil {
		t.Fatalf("start now: %+v", st)
	}
	// After a start for everyone, a latecomer counts down by themselves.
	s3, a3 := f.join(t)
	f.admit(t)
	if got := state(t, s3, a3.AttemptID).CountdownDeadline; got == nil {
		t.Fatal("a latecomer after the start should count down")
	}
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 200, &d)
	if d.Session.StartedAt == nil {
		t.Fatal("session start time not recorded")
	}
}

// The default stays as before (FR-SS-05): the countdown starts on admission,
// and the teacher can still start everyone straight away.
func TestCountdownModeAndStartNow(t *testing.T) {
	f := setup(t, map[string]any{"admission_mode": "auto"}, nil, nil)
	s, a := f.join(t)
	if got := state(t, s, a.AttemptID).CountdownDeadline; got == nil || *got != f.clk.now().Add(60*time.Second).UnixMilli() {
		t.Fatalf("auto-admitted student should count down: %v", got)
	}
	// Start without now leaves a running countdown alone.
	if n := f.start(t, map[string]any{"all": true}); n != 0 {
		t.Fatalf("start (no now) changed %d running countdowns", n)
	}
	if n := f.start(t, map[string]any{"all": true, "now": true}); n != 1 {
		t.Fatalf("start now: %d", n)
	}
	if st := state(t, s, a.AttemptID); st.State != live.StateInProgress {
		t.Fatalf("after start now: %+v", st)
	}
	f.start(t, map[string]any{"all": true}) // nothing left to start: still fine
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/start", map[string]any{}, 400, nil)
}
