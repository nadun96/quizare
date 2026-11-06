package live_test

import (
	"context"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// D-139: a student who handed in by mistake carries on in the same attempt:
// answers and place kept, the time they had left, marks given again.
func TestReopenSubmittedAttempt(t *testing.T) {
	f := setup(t, nil, map[string]any{"quiz_time_limit_sec": 600}, nil)
	ctx := context.Background()
	s, st := running(t, f)
	reopenURL := "/api/teacher/attempts/" + st.AttemptID + "/reopen"
	count := func(table string) int {
		t.Helper()
		var n int
		if err := f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE attempt_id=$1`, st.AttemptID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Only once it's handed in.
	f.teacher.Call("POST", reopenURL, map[string]string{"reason": "too soon"}, 409, nil)

	// Answer the first question wrongly, then hand in 100 s into the 600.
	first := st.Question.ID
	wrong := st.Question.Body.Options[1].ID
	right := st.Question.Body.Options[0].ID
	if code, body := save(s, st, quiz.Response{Selected: []string{wrong}}, 1); code != 200 {
		t.Fatalf("save: %d %s", code, body)
	}
	f.clk.add(100 * time.Second)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/submit", nil, 200, nil)
	evaluate(t, f.e)
	if count("eval.results") != 1 || count("eval.marks") == 0 {
		t.Fatal("not marked")
	}
	var d live.Dashboard
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 200, &d)
	if len(d.Rows) != 1 || d.Rows[0].RemainingMs != nil {
		t.Fatalf("a handed-in row shows time left: %+v", d.Rows)
	}

	// The teacher's, with a reason.
	f.teacher.Call("POST", reopenURL, map[string]string{"reason": " "}, 422, nil)
	f.e.NewUser(auth.RoleTeacher).Call("POST", reopenURL, map[string]string{"reason": "x"}, 404, nil)
	s.Call("POST", reopenURL, map[string]string{"reason": "x"}, 403, nil)
	f.clk.add(30 * time.Second) // waiting for the teacher doesn't use up their time
	f.teacher.Call("POST", reopenURL, map[string]string{"reason": "Clicked Finish by mistake"}, 200, nil)

	got := state(t, s, st.AttemptID)
	if got.State != live.StateInProgress || got.Question == nil || got.Question.ID != first {
		t.Fatalf("after reopening: %+v", got)
	}
	if got.Answer == nil || len(got.Answer.Selected) != 1 || got.Answer.Selected[0] != wrong {
		t.Fatalf("answer lost: %+v", got.Answer)
	}
	if want := f.clk.now().Add(500 * time.Second).UnixMilli(); got.QuizDeadline == nil || *got.QuizDeadline != want {
		t.Fatalf("deadline = %v, want %d", got.QuizDeadline, want)
	}
	if count("eval.results") != 0 || count("eval.marks") != 0 {
		t.Fatal("old marks kept")
	}
	var n int
	f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM audit.events WHERE action='attempt_reopened' AND target_id=$1`, st.AttemptID).Scan(&n)
	if n != 1 {
		t.Fatal("reopening not audited")
	}
	f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM live.events WHERE attempt_id=$1 AND kind='reopened'`, st.AttemptID).Scan(&n)
	if n != 1 {
		t.Fatal("reopening not in the session's events")
	}

	// They change their answer and hand in: marked afresh, one result.
	if code, body := save(s, got, quiz.Response{Selected: []string{right}}, 2); code != 200 {
		t.Fatalf("save after reopening: %d %s", code, body)
	}
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/submit", nil, 200, nil)
	evaluate(t, f.e)
	if count("eval.results") != 1 {
		t.Fatal("not marked again")
	}
	var score float64
	if err := f.e.Pool.QueryRow(ctx, `SELECT score::float8 FROM eval.results WHERE attempt_id=$1`, st.AttemptID).Scan(&score); err != nil {
		t.Fatal(err)
	}
	if score != 1 {
		t.Fatalf("score = %v, want the new answer's 1", score)
	}

	// Handed in as time ran out: they still get a minute.
	f.teacher.Call("POST", reopenURL, map[string]string{"reason": "again"}, 200, nil)
	f.clk.add(600 * time.Second)
	f.tick(t)
	if g := state(t, s, st.AttemptID); g.State != live.StateSubmitted {
		t.Fatalf("time up: %s", g.State)
	}
	f.teacher.Call("POST", reopenURL, map[string]string{"reason": "out of time"}, 200, nil)
	if g := state(t, s, st.AttemptID); g.QuizDeadline == nil || *g.QuizDeadline != f.clk.now().Add(live.ReopenMinTime).UnixMilli() {
		t.Fatalf("minimum time: %+v", g.QuizDeadline)
	}

	// A try replaced by a newer one stays closed; so does everything once the session ends.
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/submit", nil, 200, nil)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/retry", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	f.teacher.Call("POST", reopenURL, map[string]string{"reason": "old try"}, 409, nil)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/end", map[string]any{}, 204, nil)
	f.teacher.Call("POST", reopenURL, map[string]string{"reason": "too late"}, 410, nil)
}

// D-139: an attempt handed in without a time limit stays without one.
func TestReopenWithoutTimeLimit(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, st := running(t, f)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/submit", nil, 200, nil)
	f.teacher.Call("POST", "/api/teacher/attempts/"+st.AttemptID+"/reopen", map[string]string{"reason": "by mistake"}, 200, nil)
	if g := state(t, s, st.AttemptID); g.State != live.StateInProgress || g.QuizDeadline != nil {
		t.Fatalf("reopened: %+v", g)
	}
}

// D-139: not in a team quiz.
func TestReopenNotInTeamQuiz(t *testing.T) {
	f := setup(t, nil, nil, map[string]any{"team_mode": "manual"})
	s, st := running(t, f)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/submit", nil, 200, nil)
	f.teacher.Call("POST", "/api/teacher/attempts/"+st.AttemptID+"/reopen", map[string]string{"reason": "by mistake"}, 409, nil)
}
