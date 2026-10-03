package eval_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/quiz"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

const csvQuiz = `question_code,type,question_text,options,correct_answer,marks,evaluation,feedback_correct,feedback_incorrect
Q1,SINGLE,Capital of France?,Paris|Rome,Paris,2,KEY,Well done,It is Paris
Q2,BLANK_TEXT,Water boils at [[1]] C,,1=100,1,KEY,,
Q3,ESSAY,Explain photosynthesis,,,5,LLM,,
`

type fx struct {
	e       *apptest.Env
	teacher *apptest.Client
	sess    live.Session
}

func setup(t *testing.T, sessionSettings map[string]any) *fx {
	t.Helper()
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Q"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(csvQuiz))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess live.Session
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{"settings": sessionSettings}, 201, &sess)
	return &fx{e: e, teacher: teacher, sess: sess}
}

// take answers the quiz: answers[i] is the response for question i (nil = skip).
func (f *fx) take(t *testing.T, answers ...*quiz.Response) (*apptest.Client, string) {
	t.Helper()
	s := f.e.NewUser(auth.RoleStudent)
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}, 200, &st)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	for _, a := range answers {
		if a != nil {
			s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": a, "seq": 1}, 200, nil)
		}
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
	}
	if st.State != live.StateSubmitted {
		t.Fatalf("not submitted: %s", st.State)
	}
	return s, st.AttemptID
}

func (f *fx) evaluate(t *testing.T) int {
	t.Helper()
	jobs := f.e.TakeJobs("evaluate_attempt")
	for _, raw := range jobs {
		var args eval.EvaluateAttemptArgs
		json.Unmarshal(raw, &args)
		if err := f.e.App.Eval.EvaluateAttempt(context.Background(), args.AttemptID); err != nil {
			t.Fatal(err)
		}
	}
	return len(jobs)
}

func (f *fx) results(t *testing.T) map[string]eval.AttemptResult {
	t.Helper()
	var out struct{ Results []eval.AttemptResult }
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/results", nil, 200, &out)
	m := map[string]eval.AttemptResult{}
	for _, r := range out.Results {
		m[r.AttemptID] = r
	}
	return m
}

// UC-04 + BR-11: key questions are marked at once; an LLM essay without a
// teacher key falls back to manual marking; the teacher's override completes it.
func TestEvaluateOverrideAndBR11(t *testing.T) {
	f := setup(t, nil)
	_, id := f.take(t,
		&quiz.Response{Selected: []string{"o1"}},             // Q1 right: 2
		&quiz.Response{Blanks: map[string]string{"1": "99"}}, // Q2 wrong: 0
		&quiz.Response{Text: "Plants use light to make glucose."})
	if n := f.evaluate(t); n != 1 {
		t.Fatalf("evaluation jobs = %d (queued in the submit transaction)", n)
	}
	r := f.results(t)[id]
	if r.Score != 2 || r.MaxScore != 8 || r.Complete || r.Passed {
		t.Fatalf("result = %+v", r)
	}
	byCode := map[string]eval.MarkView{}
	for _, m := range r.Marks {
		byCode[m.Code] = m
	}
	if m := byCode["Q1"]; m.Status != "marked" || *m.Score != 2 || m.Feedback != "Well done" {
		t.Errorf("Q1 = %+v", m)
	}
	if m := byCode["Q2"]; *m.Score != 0 || *m.Correct {
		t.Errorf("Q2 = %+v", m)
	}
	if m := byCode["Q3"]; m.Status != "needs_manual" || m.Score != nil {
		t.Errorf("Q3 should need manual marking without an LLM key: %+v", m)
	}

	essay := byCode["Q3"].QuestionID
	f.teacher.Call("PUT", "/api/teacher/marks/"+id+"/"+essay, map[string]any{"score": 9}, 422, nil)
	f.teacher.Call("PUT", "/api/teacher/marks/"+id+"/"+essay, map[string]any{"score": 4, "feedback": "Mention chlorophyll."}, 200, nil)
	r = f.results(t)[id]
	if r.Score != 6 || !r.Complete || r.Pct != 75 || !r.Passed {
		t.Fatalf("after override: %+v", r)
	}
	// Re-running evaluation keeps the override.
	if err := f.e.App.Eval.EvaluateAttempt(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if got := f.results(t)[id]; got.Score != 6 {
		t.Fatalf("re-evaluation lost the override: %+v", got)
	}
	var n int
	f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit.events WHERE action='mark_overridden'`).Scan(&n)
	if n != 1 {
		t.Fatalf("override audit rows = %d", n)
	}
	// Teachers can change only their own marks.
	f.e.NewUser(auth.RoleTeacher).Call("PUT", "/api/teacher/marks/"+id+"/"+essay, map[string]any{"score": 5}, 404, nil)
	f.e.NewUser(auth.RoleTeacher).Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/results", nil, 404, nil)
}

func TestUnansweredScoresZero(t *testing.T) {
	f := setup(t, nil)
	_, id := f.take(t, nil, nil, nil)
	f.evaluate(t)
	r := f.results(t)[id]
	if r.Score != 0 || !r.Complete {
		t.Fatalf("blank attempt: %+v", r)
	}
	for _, m := range r.Marks {
		if m.Status != "marked" || *m.Score != 0 {
			t.Errorf("%s = %+v", m.Code, m)
		}
	}
}

// BR-12 + results_release=on_session_end (default).
func TestReleaseOnSessionEnd(t *testing.T) {
	f := setup(t, nil)
	s, id := f.take(t, &quiz.Response{Selected: []string{"o1"}}, nil, nil)
	f.evaluate(t)
	status, body := s.Do("GET", "/api/my/attempts/"+id+"/result", nil)
	if status != 403 || !strings.Contains(string(body), "results_not_released") {
		t.Fatalf("before end: %d %s", status, body)
	}
	var mine struct{ Attempts []live.StudentAttempt }
	s.Call("GET", "/api/my/attempts", nil, 200, &mine)
	if len(mine.Attempts) != 1 || mine.Attempts[0].Released {
		t.Fatalf("my attempts = %+v", mine)
	}
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/end", nil, 204, nil)
	var res eval.StudentResult
	s.Call("GET", "/api/my/attempts/"+id+"/result", nil, 200, &res)
	if res.Score != 2 || len(res.Questions) != 3 || res.Questions[0].Key == nil || res.Questions[0].Response == nil {
		t.Fatalf("result = %+v", res)
	}
	if res.Questions[0].Feedback != "Well done" || !res.Complete { // unanswered essay scores 0 at once
		t.Fatalf("feedback/complete: %+v", res.Questions[0])
	}
	// Another student cannot read it.
	f.e.NewUser(auth.RoleStudent).Call("GET", "/api/my/attempts/"+id+"/result", nil, 404, nil)
}

func TestReleaseImmediateAndManual(t *testing.T) {
	f := setup(t, map[string]any{"results_release": "immediate", "results_show_correct": false, "results_show_feedback": false})
	s, id := f.take(t, &quiz.Response{Selected: []string{"o2"}}, nil, nil)
	f.evaluate(t)
	_, raw := s.Do("GET", "/api/my/attempts/"+id+"/result", nil)
	if strings.Contains(string(raw), "correct_answer") || strings.Contains(string(raw), "It is Paris") {
		t.Fatalf("hidden parts shown: %s", raw)
	}
	var res eval.StudentResult
	s.Call("GET", "/api/my/attempts/"+id+"/result", nil, 200, &res)
	if res.Questions[0].Response == nil {
		t.Fatal("answers should still be shown")
	}

	g := setup(t, map[string]any{"results_release": "manual"})
	s2, id2 := g.take(t, nil, nil, nil)
	g.teacher.Call("POST", "/api/teacher/sessions/"+g.sess.ID+"/end", nil, 204, nil)
	s2.Call("GET", "/api/my/attempts/"+id2+"/result", nil, 403, nil)
	g.teacher.Call("POST", "/api/teacher/sessions/"+g.sess.ID+"/release", nil, 204, nil)
	s2.Call("GET", "/api/my/attempts/"+id2+"/result", nil, 200, nil)
	g.teacher.Call("POST", "/api/teacher/sessions/"+g.sess.ID+"/unrelease", nil, 204, nil)
	s2.Call("GET", "/api/my/attempts/"+id2+"/result", nil, 403, nil)
}

func TestInvalidatedAttemptsMarkedAtSessionEnd(t *testing.T) {
	f := setup(t, nil)
	s := f.e.NewUser(auth.RoleStudent)
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}, 200, &st)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/admit", map[string]any{"all": true}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{"o1"}}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/violations", map[string]any{"kind": "tab_hidden"}, 204, nil)
	if n := f.evaluate(t); n != 0 {
		t.Fatal("invalidated attempt evaluated before the session ended")
	}
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/end", nil, 204, nil)
	if n := f.evaluate(t); n != 1 {
		t.Fatalf("jobs at session end = %d", n)
	}
	r := f.results(t)[st.AttemptID]
	if !r.Invalidated || r.Passed || r.Score != 2 {
		t.Fatalf("invalidated result = %+v", r)
	}
}
