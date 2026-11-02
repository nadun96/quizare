package insight_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/insight"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/quiz"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

const csvQuiz = `question_code,type,question_text,options,correct_answer,marks
Q1,SINGLE,Capital of France?,Paris|Rome|Madrid,Paris,1
Q2,BLANK_TEXT,Water boils at [[1]] C,,1=100,1
`

type class struct {
	teacher *apptest.Client
	id      string
	quizID  string
}

func newClass(t *testing.T, e *apptest.Env, name string) class {
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": name, "settings": map[string]any{"student_id_required": true}}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "Term 1"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "Heat"}, 201, &topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": name + " quiz"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(csvQuiz))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	return class{teacher: teacher, id: c.ID, quizID: q.ID}
}

// run has n students take the quiz: the first `right` get both questions right, the rest Q1 only.
func run(t *testing.T, e *apptest.Env, c class, prefix string, n, right int) {
	var sess live.Session
	c.teacher.Call("POST", "/api/teacher/quizzes/"+c.quizID+"/sessions", map[string]any{}, 201, &sess)
	for i := 0; i < n; i++ {
		s := e.Client()
		num := fmt.Sprintf("%s%d", prefix, i)
		s.Call("POST", "/api/auth/register", map[string]string{"email": num + "@example.com", "name": "Student " + num, "password": "password123", "role": "student"}, 201, nil)
		var st live.StudentState
		s.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{"student_number": num}, 200, &st)
		c.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
		s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{"o1"}}}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
		blank := "99"
		if i < right {
			blank = "100"
		}
		s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Blanks: map[string]string{"1": blank}}}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
	}
	for _, raw := range e.TakeJobs("evaluate_attempt") {
		var a eval.EvaluateAttemptArgs
		json.Unmarshal(raw, &a)
		if err := e.App.Eval.EvaluateAttempt(context.Background(), a.AttemptID); err != nil {
			t.Fatal(err)
		}
	}
}

func query(c *apptest.Client, q insight.Query, want int) insight.Result {
	var r insight.Result
	c.Call("POST", "/api/insight/query", q, want, &r)
	return r
}

func val(r insight.Row, k string) float64 {
	if r.Values[k] == nil {
		return -1
	}
	return *r.Values[k]
}

// The warehouse and the query engine (D-120): any measure by any allowed
// dimensions, a teacher within their classrooms, an admin over everything
// but never one student nor a group under five.
func TestInsight(t *testing.T) {
	e := apptest.New(t)
	ctx := context.Background()
	big := newClass(t, e, "Physics")
	small := newClass(t, e, "Chemistry")
	run(t, e, big, "P", 6, 3)   // three 100 %, three 50 %
	run(t, e, small, "C", 2, 2) // two 100 %

	st, err := e.App.Insight.Load(ctx)
	if err != nil || st.Results != 8 || st.Answers != 16 || !st.Reconciled {
		t.Fatalf("load = %+v, %v", st, err)
	}

	// A teacher: their classroom only, with their students.
	r := query(big.teacher, insight.Query{Measures: []string{"results", "avg_score", "pass_rate", "students"}, Dimensions: []string{"classroom"}}, 200)
	if len(r.Rows) != 1 || r.Rows[0].Dims["classroom"].Label != "Physics" || val(r.Rows[0], "results") != 6 || val(r.Rows[0], "avg_score") != 75 || val(r.Rows[0], "students") != 6 {
		t.Fatalf("teacher by classroom = %+v", r.Rows)
	}
	r = query(big.teacher, insight.Query{Measures: []string{"avg_score"}, Dimensions: []string{"student"}}, 200)
	if len(r.Rows) != 6 || r.Rows[0].Dims["student"].Label != "Student P0" {
		t.Fatalf("by student = %+v", r.Rows)
	}
	// Someone else's classroom, asked for by id: nothing.
	r = query(big.teacher, insight.Query{Measures: []string{"results"}, Filters: map[string][]string{"classroom": {small.id}}}, 200)
	if len(r.Rows) != 1 || val(r.Rows[0], "results") != 0 {
		t.Fatalf("another's classroom = %+v", r.Rows)
	}

	// Questions by type.
	r = query(big.teacher, insight.Query{Measures: []string{"answers", "pct_correct"}, Dimensions: []string{"qtype"}}, 200)
	if len(r.Rows) != 2 || r.Rows[0].Dims["qtype"].Label != "Fill in (typed)" || val(r.Rows[0], "pct_correct") != 50 || val(r.Rows[1], "pct_correct") != 100 {
		t.Fatalf("by question type = %+v", r.Rows)
	}

	// Three dimensions with totals: subtotals for the first, and a grand total.
	r = query(big.teacher, insight.Query{Measures: []string{"results"}, Dimensions: []string{"date", "topic", "item"}, Grain: "year", Totals: true}, 200)
	last := r.Rows[len(r.Rows)-1]
	if last.Level != 2 || val(last, "results") != 6 || r.Rows[len(r.Rows)-2].Level != 1 || r.Rows[0].Dims["item"].Label != "Physics quiz" {
		t.Fatalf("with totals = %+v", r.Rows)
	}

	// An admin: every classroom, the small one hidden; no students.
	admin := e.NewUser(auth.RoleAdmin)
	r = query(admin, insight.Query{Measures: []string{"results", "avg_score"}, Dimensions: []string{"teacher", "classroom"}}, 200)
	if len(r.Rows) != 2 || r.MinGroup != 5 {
		t.Fatalf("admin = %+v", r)
	}
	for _, row := range r.Rows {
		hidden := row.Dims["classroom"].Label == "Chemistry"
		if row.Hidden != hidden || (hidden && row.Values["results"] != nil) || (!hidden && val(row, "results") != 6) {
			t.Fatalf("admin row %+v", row)
		}
	}
	query(admin, insight.Query{Measures: []string{"results"}, Dimensions: []string{"student"}}, 422)
	query(admin, insight.Query{Measures: []string{"results"}, Filters: map[string][]string{"student": {"x"}}}, 422)
	var cat insight.Catalog
	admin.Call("GET", "/api/insight/catalog", nil, 200, &cat)
	if !cat.Staff || len(cat.Classrooms) != 2 {
		t.Fatalf("admin catalog = %+v", cat)
	}
	for _, d := range cat.Dimensions {
		if d.Key == "student" {
			t.Fatal("admins are offered students")
		}
	}

	// What can't be asked.
	query(big.teacher, insight.Query{Measures: []string{"results", "pct_correct"}}, 422)               // two facts
	query(big.teacher, insight.Query{Measures: []string{"results"}, Dimensions: []string{"qtype"}}, 422) // not with activity
	query(big.teacher, insight.Query{Measures: []string{"drop table"}}, 422)
	query(big.teacher, insight.Query{Measures: []string{"results"}, Dimensions: []string{"date", "topic", "item", "kind"}}, 422)
	query(big.teacher, insight.Query{Measures: []string{"results"}, From: "2026-13-40"}, 422)
	student := e.NewUser(auth.RoleStudent)
	student.Call("POST", "/api/insight/query", insight.Query{Measures: []string{"results"}}, 403, nil)

	// A result invalidated leaves the warehouse at the next load.
	if _, err := e.Pool.Exec(ctx, `UPDATE eval.results SET invalidated = true, updated_at = now()
		WHERE user_id = (SELECT id FROM auth.users WHERE email = 'p0@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.App.Insight.Load(ctx); err != nil {
		t.Fatal(err)
	}
	r = query(big.teacher, insight.Query{Measures: []string{"results"}}, 200)
	if val(r.Rows[0], "results") != 5 || r.Loaded == nil {
		t.Fatalf("after invalidating = %+v", r)
	}

	// A co-teacher sees the classroom too, once the loader knows (D-121).
	co := e.NewUser(auth.RoleTeacher)
	big.teacher.Call("POST", "/api/teacher/classrooms/"+big.id+"/teachers", map[string]string{"email": co.User.Email}, 201, nil)
	co.Call("POST", "/api/teacher/invitations/"+big.id+"/accept", nil, 204, nil)
	if _, err := e.App.Insight.Load(ctx); err != nil {
		t.Fatal(err)
	}
	r = query(co, insight.Query{Measures: []string{"results"}, Dimensions: []string{"classroom"}}, 200)
	if len(r.Rows) != 1 || r.Rows[0].Dims["classroom"].Label != "Physics" || val(r.Rows[0], "results") != 5 {
		t.Fatalf("co-teacher = %+v", r.Rows)
	}
}
