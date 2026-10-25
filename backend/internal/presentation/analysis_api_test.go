package presentation_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/presentation"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/tutorial"
)

// take has a student sit the quiz's one question, answering choice, and marks it.
func (f *fx) take(t *testing.T, sess live.Session, s *apptest.Client, choice string) {
	t.Helper()
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{}, 200, &st)
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{choice}}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, nil)
	for _, raw := range f.e.TakeJobs("evaluate_attempt") {
		var a eval.EvaluateAttemptArgs
		json.Unmarshal(raw, &a)
		if err := f.e.App.Eval.EvaluateAttempt(context.Background(), a.AttemptID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnalysis(t *testing.T) {
	f := setup(t)
	// The lesson's quiz, and a tutorial, in the same topic.
	var q quiz.Quiz
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/quizzes", map[string]any{"title": "Cells quiz"}, 201, &q)
	f.teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv",
		[]byte("question_code,type,question_text,options,correct_answer,marks,evaluation\nQ1,SINGLE,The powerhouse?,Mitochondria|Nucleus,Mitochondria,1,KEY\n"))
	f.teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess live.Session
	f.teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)
	var tu tutorial.Tutorial
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/tutorials", map[string]any{"title": "Cells tutorial"}, 201, &tu)

	var p presentation.Presentation
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/presentations", map[string]any{"title": "Cells"}, 201, &p)
	p = f.upload(t, p.ID, 2)
	tpath := "/api/teacher/presentations/" + p.ID
	f.teacher.Call("POST", tpath+"/status", map[string]string{"status": "published"}, 200, nil)

	// Links: only the topic's quizzes and tutorials.
	var l presentation.Links
	f.teacher.Call("GET", tpath+"/links", nil, 200, &l)
	if len(l.Options) != 2 || len(l.Linked) != 0 {
		t.Fatalf("links = %+v", l)
	}
	f.teacher.Call("PUT", tpath+"/links", map[string]any{"linked": []map[string]string{{"kind": "quiz", "id": p.ID}}}, 422, nil)
	var an presentation.Analysis
	f.teacher.Call("GET", tpath+"/analysis", nil, 200, &an)
	if len(an.Points) != 0 || len(an.Linked) != 0 {
		t.Fatalf("nothing linked yet = %+v", an)
	}
	f.teacher.Call("PUT", tpath+"/links", map[string]any{"linked": []map[string]string{{"kind": "quiz", "id": q.ID}, {"kind": "tutorial", "id": tu.ID}}}, 200, &l)
	if len(l.Linked) != 2 || l.Linked[0].Title == "" {
		t.Fatalf("linked = %+v", l.Linked)
	}

	// Four students: three read (for long, little, middling), one never opens it.
	long, short, mid, never := f.student(), f.student(), f.student(), f.student()
	for _, s := range []*apptest.Client{long, short, mid} {
		s.Call("POST", "/api/presentations/"+p.ID+"/open", nil, 200, nil)
		s.Call("POST", "/api/presentations/"+p.ID+"/go", map[string]int{"to": 2}, 200, nil)
	}
	for who, secs := range map[*apptest.Client]int{long: 100, short: 10, mid: 50} {
		if _, err := f.e.Pool.Exec(context.Background(), `UPDATE presentation.views w SET seconds=$2 FROM presentation.viewers v
			WHERE v.id=w.viewer_id AND v.user_id=$1 AND w.n=1`, who.User.ID, secs); err != nil {
			t.Fatal(err)
		}
	}
	f.take(t, sess, long, "o1")
	f.take(t, sess, short, "o2")
	f.take(t, sess, mid, "o1")
	f.take(t, sess, never, "o1")

	f.teacher.Call("GET", tpath+"/analysis", nil, 200, &an)
	if len(an.Points) != 3 || an.NotViewed != 1 || an.NoScore != 0 {
		t.Fatalf("points = %+v not viewed %d", an.Points, an.NotViewed)
	}
	if an.Trend == nil || an.Trend.R <= 0 || an.Trend.R > 1 || an.Trend.PerTenMinutes <= 0 {
		t.Fatalf("trend = %+v", an.Trend)
	}
	s1 := an.Slides[0]
	if s1.MedianSeconds != 50 || s1.LongerN != 2 || *s1.Longer != 100 || s1.ShorterN != 1 || *s1.Shorter != 0 || s1.Skipped != 0 {
		t.Fatalf("slide 1 = %+v", s1)
	}

	// The CSV, with the linked score.
	code, h, body := f.teacher.Get(tpath+"/report.csv", nil)
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if code != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/csv") || !strings.Contains(lines[0], "linked_score_pct") ||
		!strings.Contains(lines[0], "slide_2_seconds") || len(lines) != 5 {
		t.Fatalf("csv %d: %s", code, body)
	}
	if code, _, _ := f.e.NewUser(auth.RoleTeacher).Get(tpath+"/report.csv", nil); code != 404 {
		t.Fatalf("another teacher's csv: %d", code)
	}
}
