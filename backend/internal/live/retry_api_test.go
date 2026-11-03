package live_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// takeQuiz starts an admitted attempt, answers the first question with
// option choice (o1 is right), leaves the rest blank and hands it in.
func takeQuiz(t *testing.T, s *apptest.Client, attemptID, choice string) live.StudentState {
	t.Helper()
	var st live.StudentState
	s.Call("POST", "/api/attempts/"+attemptID+"/start", nil, 200, &st)
	s.Call("PUT", "/api/attempts/"+attemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{choice}}, "seq": 1}, 200, nil)
	s.Call("POST", "/api/attempts/"+attemptID+"/submit", nil, 200, &st)
	return st
}

func evaluate(t *testing.T, e *apptest.Env) {
	t.Helper()
	for _, raw := range e.TakeJobs("evaluate_attempt") {
		var a eval.EvaluateAttemptArgs
		_ = json.Unmarshal(raw, &a)
		if err := e.App.Eval.EvaluateAttempt(context.Background(), a.AttemptID); err != nil {
			t.Fatal(err)
		}
	}
}

func sessionResults(t *testing.T, f *fixture) []eval.AttemptResult {
	t.Helper()
	var page struct{ Results []eval.AttemptResult }
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/results", nil, 200, &page)
	return page.Results
}

// D-131: the teacher lets a student who finished take the quiz again from
// the start; with tries allowed the student starts again themselves; the
// best try counts.
func TestRetries(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, st := f.join(t)
	f.admit(t, st.AttemptID)
	first := takeQuiz(t, s, st.AttemptID, "o1") // right: 1 of 8 marks
	if first.State != live.StateSubmitted || first.Try != 1 || first.Tries != 1 || first.CanRetry {
		t.Fatalf("after the first try: %+v", first)
	}
	evaluate(t, f.e)
	// One try allowed: the student can't start again themselves.
	code, body := s.Do("POST", "/api/attempts/"+st.AttemptID+"/retry", nil)
	if code != 409 || !json.Valid(body) {
		t.Fatalf("self retry with no tries left: %d %s", code, body)
	}

	// The teacher lets them.
	var n struct{ Affected int }
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/retry", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, &n)
	if n.Affected != 1 {
		t.Fatalf("retried %d", n.Affected)
	}
	old := state(t, s, st.AttemptID)
	if old.NextAttemptID == "" || old.NextAttemptID == st.AttemptID {
		t.Fatalf("the old try doesn't point to the new one: %+v", old)
	}
	again := state(t, s, old.NextAttemptID)
	if again.Try != 2 || again.State != live.StateAdmitted {
		t.Fatalf("the new try: %+v", again)
	}
	// Joining again finds the current try, not the old one.
	var rejoin live.StudentState
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}, 200, &rejoin)
	if rejoin.AttemptID != again.AttemptID {
		t.Fatalf("join found %s, want %s", rejoin.AttemptID, again.AttemptID)
	}
	var d live.Dashboard
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 200, &d)
	if len(d.Rows) != 1 || d.Rows[0].Try != 2 || d.Rows[0].AttemptID != again.AttemptID {
		t.Fatalf("dashboard = %+v", d.Rows)
	}
	// A worse second try: the first still counts.
	takeQuiz(t, s, again.AttemptID, "o2")
	evaluate(t, f.e)
	rs := sessionResults(t, f)
	if len(rs) != 1 || rs[0].AttemptID != st.AttemptID || rs[0].Try != 1 || rs[0].Tries != 2 || rs[0].Score != 1 {
		t.Fatalf("results after a worse retry = %+v", rs)
	}

	// Three tries allowed for the session: the student starts again themselves.
	f.teacher.Call("PUT", "/api/teacher/sessions/"+f.sess.ID+"/settings", map[string]any{"tries": 3}, 200, nil)
	if st2 := state(t, s, again.AttemptID); !st2.CanRetry || st2.Tries != 3 {
		t.Fatalf("with 3 tries: %+v", st2)
	}
	var third live.StudentState
	s.Call("POST", "/api/attempts/"+again.AttemptID+"/retry", nil, 200, &third)
	if third.Try != 3 || third.AttemptID == again.AttemptID {
		t.Fatalf("third try = %+v", third)
	}
	// As good as the first: of equal tries the latest counts.
	takeQuiz(t, s, third.AttemptID, "o1")
	evaluate(t, f.e)
	rs = sessionResults(t, f)
	if len(rs) != 1 || rs[0].AttemptID != third.AttemptID || rs[0].Try != 3 || rs[0].Tries != 3 {
		t.Fatalf("results after an equal third try = %+v", rs)
	}
	if st3 := state(t, s, third.AttemptID); st3.CanRetry {
		t.Fatal("a fourth try offered with 3 allowed")
	}
	// Retrying is for finished students in a running session.
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/end", map[string]any{}, 204, nil)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/retry", map[string]any{"all": true}, 410, nil)
}
