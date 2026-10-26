package eval_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nadun96/quizplatform/internal/app"
	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// A classroom's results (D-87, TS-FR-82): each active student's best mark
// on each quiz run in the classroom.

func TestClassroomResults(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 9"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "Biology"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "Cells"}, 201, &topic)
	newQuiz := func(title string) live.Session {
		var q quiz.Quiz
		teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": title}, 201, &q)
		teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv",
			[]byte("question_code,type,question_text,options,correct_answer,marks,evaluation\nQ1,SINGLE,The powerhouse?,Mitochondria|Nucleus,Mitochondria,1,KEY\n"))
		teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
		var sess live.Session
		teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)
		return sess
	}
	take := func(sess live.Session, s *apptest.Client, choice string) {
		t.Helper()
		var st live.StudentState
		s.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{}, 200, &st)
		teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
		s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{choice}}}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, nil)
		for _, raw := range e.TakeJobs("evaluate_attempt") {
			var a eval.EvaluateAttemptArgs
			json.Unmarshal(raw, &a)
			if err := e.App.Eval.EvaluateAttempt(context.Background(), a.AttemptID); err != nil {
				t.Fatal(err)
			}
		}
	}
	enrol := func() *apptest.Client {
		s := e.NewUser(auth.RoleStudent)
		s.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)
		return s
	}
	ann, bob, cy := enrol(), enrol(), enrol()

	// No quizzes yet: the roster with no marks.
	type results struct {
		Quizzes  []eval.ClassroomQuiz
		Students []app.ClassroomStudentResult
	}
	var r results
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/results", nil, 200, &r)
	if len(r.Quizzes) != 0 || len(r.Students) != 3 || r.Students[0].Average != nil || len(r.Students[0].Scores) != 0 {
		t.Fatalf("before any quiz = %+v", r)
	}

	first, second := newQuiz("Cells quiz"), newQuiz("Organs quiz")
	take(first, ann, "o1")
	take(first, bob, "o2")
	take(second, ann, "o2")
	r = results{}
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/results", nil, 200, &r)
	if len(r.Quizzes) != 2 || r.Quizzes[0].Title != "Cells quiz" || r.Quizzes[1].Title != "Organs quiz" {
		t.Fatalf("quizzes = %+v", r.Quizzes)
	}
	by := map[string]app.ClassroomStudentResult{}
	for _, s := range r.Students {
		by[s.UserID] = s
	}
	a, b, cc := by[ann.User.ID], by[bob.User.ID], by[cy.User.ID]
	if a.Scores[first.QuizID] != 100 || a.Scores[second.QuizID] != 0 || a.Average == nil || *a.Average != 50 {
		t.Fatalf("Ann = %+v", a)
	}
	if b.Scores[first.QuizID] != 0 || len(b.Scores) != 1 || *b.Average != 0 {
		t.Fatalf("Bob = %+v", b)
	}
	if len(cc.Scores) != 0 || cc.Average != nil {
		t.Fatalf("Cy, who took none = %+v", cc)
	}

	// Another teacher's classroom: not found.
	other := e.NewUser(auth.RoleTeacher)
	other.Call("GET", "/api/teacher/classrooms/"+c.ID+"/results", nil, 404, nil)
	ann.Call("GET", "/api/teacher/classrooms/"+c.ID+"/results", nil, 403, nil)
}
