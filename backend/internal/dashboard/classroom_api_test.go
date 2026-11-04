package dashboard_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/dashboard"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// D-138: a student's one classroom: what's on, due and coming, and what
// they've completed, with marks only once they may see them.
func TestStudentClassroomView(t *testing.T) {
	e := apptest.New(t)
	ctx := context.Background()
	teacher, student := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleStudent)
	room := func(name string, settings map[string]any) (content.Classroom, content.Topic) {
		var c content.Classroom
		teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": name, "settings": settings}, 201, &c)
		var m content.Module
		teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
		var tp content.Topic
		teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &tp)
		return c, tp
	}
	c, topic := room("Science", nil)
	other, otherTopic := room("History", nil)
	student.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)
	student.Call("POST", "/api/enrolments", map[string]string{"join_code": other.JoinCode}, 200, nil)
	if _, err := e.Pool.Exec(ctx, `UPDATE content.enrolments SET created_at = now() - interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	tutorial := func(tp content.Topic, title, opens, due, feedback string) string {
		var tu struct {
			ID     string `json:"id"`
			QuizID string `json:"quiz_id"`
		}
		teacher.Call("POST", "/api/teacher/topics/"+tp.ID+"/tutorials", map[string]any{"title": title}, 201, &tu)
		teacher.Raw("POST", "/api/teacher/quizzes/"+tu.QuizID+"/questions/import", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,A?,A|B,A,2\n"))
		if _, err := e.Pool.Exec(ctx, `UPDATE tutorial.tutorials SET status='published', published_at=now(), feedback=$4,
			opens_at = CASE WHEN $2 = '' THEN NULL ELSE now() + $2::interval END, due_at = CASE WHEN $3 = '' THEN NULL ELSE now() + $3::interval END WHERE id=$1`, tu.ID, opens, due, feedback); err != nil {
			t.Fatal(err)
		}
		return tu.ID
	}
	tutorial(topic, "No date", "", "", "page")
	tutorial(topic, "Due tomorrow", "", "1 day", "page")
	tutorial(topic, "Overdue", "", "-2 days", "page")
	tutorial(topic, "Opens next week", "7 days", "10 days", "page")
	tutorial(otherTopic, "History homework", "", "1 day", "page")
	finish := func(id string, score int) {
		if _, err := e.Pool.Exec(ctx, `WITH r AS (INSERT INTO tutorial.runs (tutorial_id, user_id, finished_at)
				SELECT $1, u.id, now() FROM auth.users u WHERE u.id = $3 RETURNING id)
			INSERT INTO tutorial.answers (run_id, question_id, response, score, max_score)
			SELECT r.id, x.id, '{}', $2, 2 FROM r, quiz.questions x JOIN tutorial.tutorials t ON t.quiz_id = x.quiz_id WHERE t.id = $1`, id, score, student.User.ID); err != nil {
			t.Fatal(err)
		}
	}
	done := tutorial(topic, "Finished, marks shown", "", "", "page")
	finish(done, 1)
	held := tutorial(topic, "Finished, marks held", "", "", "release")
	finish(held, 2)

	var q struct{ ID string }
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Cells"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,A?,A|B,A,4\n"))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess live.Session
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{"title": "Cells test"}, 201, &sess)

	path := "/api/my/classrooms/" + c.ID + "/work"
	view := func() dashboard.ClassroomWork {
		t.Helper()
		var w dashboard.ClassroomWork
		student.Call("GET", path, nil, 200, &w)
		return w
	}
	w := view()
	if w.Name != "Science" || w.Status != "active" || w.Teacher == "" {
		t.Fatalf("classroom = %+v", w)
	}
	titles := func(items []dashboard.Item) []string {
		out := []string{}
		for _, it := range items {
			out = append(out, it.Title)
		}
		return out
	}
	if g := titles(w.Due); len(g) != 3 || g[0] != "Overdue" || g[1] != "Due tomorrow" || g[2] != "No date" {
		t.Fatalf("due = %v", g)
	}
	if g := titles(w.Upcoming); len(g) != 1 || g[0] != "Opens next week" {
		t.Fatalf("upcoming = %v", g)
	}
	if len(w.Live) != 1 || w.Live[0].Kind != "quiz" || w.Live[0].Href != "/j/"+sess.JoinCode {
		t.Fatalf("live = %+v", w.Live)
	}
	byTitle := func(w dashboard.ClassroomWork, title string) *dashboard.Done {
		for i := range w.Completed {
			if w.Completed[i].Title == title {
				return &w.Completed[i]
			}
		}
		return nil
	}
	if d := byTitle(w, "Finished, marks shown"); d == nil || d.Score == nil || *d.Score != 1 || *d.Max != 2 || *d.Pct != 50 {
		t.Fatalf("finished tutorial = %+v", d)
	}
	if d := byTitle(w, "Finished, marks held"); d == nil || d.Released || d.Score != nil {
		t.Fatalf("held marks leaked = %+v", d)
	}
	// Nothing from the other classroom.
	var o dashboard.ClassroomWork
	student.Call("GET", "/api/my/classrooms/"+other.ID+"/work", nil, 200, &o)
	if g := titles(o.Due); len(g) != 1 || g[0] != "History homework" || len(o.Live) != 0 || len(o.Completed) != 0 {
		t.Fatalf("other classroom = %+v", o)
	}

	// Take the quiz, right; its mark waits for the session to end.
	take := func(attempt, choice string) {
		var st live.StudentState
		student.Call("POST", "/api/attempts/"+attempt+"/start", nil, 200, &st)
		student.Call("PUT", "/api/attempts/"+attempt+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{choice}}, "seq": 1}, 200, nil)
		student.Call("POST", "/api/attempts/"+attempt+"/submit", nil, 200, nil)
		for _, raw := range e.TakeJobs("evaluate_attempt") {
			var a eval.EvaluateAttemptArgs
			_ = json.Unmarshal(raw, &a)
			if err := e.App.Eval.EvaluateAttempt(ctx, a.AttemptID); err != nil {
				t.Fatal(err)
			}
		}
	}
	var st live.StudentState
	student.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{}, 200, &st)
	teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"all": true}, 200, nil)
	take(st.AttemptID, "o1")
	w = view()
	d := byTitle(w, "Cells test")
	if d == nil || d.Kind != "quiz" || d.Status != "submitted" || d.Released || d.Score != nil || d.Href != "" {
		t.Fatalf("quiz before release = %+v", d)
	}
	if len(w.Live) != 0 {
		t.Fatalf("a finished quiz is still live: %+v", w.Live)
	}

	// A worse retry: one row, the best try's mark, once the session has ended.
	teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/retry", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	var again live.StudentState
	student.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{}, 200, &again)
	take(again.AttemptID, "o2")
	teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/end", map[string]any{}, 204, nil)
	w = view()
	d = byTitle(w, "Cells test")
	if d == nil || !d.Released || d.Score == nil || *d.Score != 4 || *d.Pct != 100 || d.Tries != 2 || d.Href != "/my/results/"+st.AttemptID {
		t.Fatalf("quiz after release = %+v", d)
	}
	n := 0
	for _, x := range w.Completed {
		if x.Title == "Cells test" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d rows for one quiz", n)
	}

	// Only their own classrooms; a pending student sees it's pending, and nothing else.
	e.NewUser(auth.RoleStudent).Call("GET", path, nil, 404, nil)
	teacher.Call("GET", path, nil, 404, nil)
	closed, _ := room("Closed", map[string]any{"enrolment_approval": true})
	student.Call("POST", "/api/enrolments", map[string]string{"join_code": closed.JoinCode}, 200, nil)
	var p dashboard.ClassroomWork
	student.Call("GET", "/api/my/classrooms/"+closed.ID+"/work", nil, 200, &p)
	if p.Status != "pending" || len(p.Due) != 0 {
		t.Fatalf("pending = %+v", p)
	}
}
