package grading_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/grading"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/quiz"

	"github.com/nadun96/quizplatform/internal/app/apptest"
)

const reportCSV = "question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,A?,A|B,A,2\nQ2,SINGLE,B?,C|D,C,2\n"

type reportFx struct {
	e       *apptest.Env
	teacher *apptest.Client
	class   content.Classroom
	topic   content.Topic
}

func newReportFx(t *testing.T) *reportFx {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Science"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var tp content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &tp)
	return &reportFx{e: e, teacher: teacher, class: c, topic: tp}
}

func (f *reportFx) quiz(t *testing.T, title string) string {
	return f.quizOf(t, title, 2, 2)
}

// quizOf makes a quiz of that many questions, each worth that many marks.
func (f *reportFx) quizOf(t *testing.T, title string, questions, marks int) string {
	csv := "question_code,type,question_text,options,correct_answer,marks\n"
	for i := 1; i <= questions; i++ {
		csv += fmt.Sprintf("Q%d,SINGLE,Q%d?,A|B,A,%d\n", i, i, marks)
	}
	var q quiz.Quiz
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/quizzes", map[string]any{"title": title, "settings": map[string]any{"results_release": "immediate", "question_order": "fixed"}}, 201, &q)
	f.teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(csv))
	f.teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	return q.ID
}

func (f *reportFx) student(t *testing.T) *apptest.Client {
	s := f.e.NewUser(auth.RoleStudent)
	s.Call("POST", "/api/enrolments", map[string]string{"join_code": f.class.JoinCode}, 200, nil)
	return s
}

func (f *reportFx) evaluate(t *testing.T) {
	t.Helper()
	for _, raw := range f.e.TakeJobs("evaluate_attempt") {
		var a eval.EvaluateAttemptArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			t.Fatal(err)
		}
		if err := f.e.App.Eval.EvaluateAttempt(context.Background(), a.AttemptID); err != nil {
			t.Fatal(err)
		}
	}
}

// take runs a session of the quiz for the student, who gets right answers
// right of the two; it returns the attempt.
func (f *reportFx) take(t *testing.T, s *apptest.Client, quizID string, right int) string {
	t.Helper()
	return f.takeOf(t, s, quizID, right, 2)
}

// takeOf is take for a quiz of any number of questions.
func (f *reportFx) takeOf(t *testing.T, s *apptest.Client, quizID string, right, questions int) string {
	t.Helper()
	var sess live.Session
	f.teacher.Call("POST", "/api/teacher/quizzes/"+quizID+"/sessions", map[string]any{}, 201, &sess)
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{}, 200, &st)
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/start", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	for i := 0; i < questions; i++ {
		choice := "o2"
		if i < right {
			choice = "o1"
		}
		s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{choice}}, "seq": i + 1}, 200, nil)
		if i < questions-1 {
			s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
		}
	}
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/submit", nil, 200, nil)
	f.evaluate(t)
	return st.AttemptID
}

func setupFor(r grading.ClassReport, change func(*grading.ReportItemInput)) grading.ReportSetup {
	in := grading.ReportSetup{Items: []grading.ReportItemInput{}, Exceptions: []grading.ResultException{}}
	for _, it := range r.Items {
		x := grading.ReportItemInput{Kind: it.Kind, ID: it.ID, Included: it.Included, Weight: it.Weight, Pick: it.Pick, Detail: it.Detail, ReviewMinutes: it.ReviewMinutes}
		change(&x)
		in.Items = append(in.Items, x)
	}
	return in
}

// D-140: which items, their weights, highest or lowest, and the total from them.
func TestClassReport(t *testing.T) {
	f := newReportFx(t)
	q1, q2, q3 := f.quiz(t, "One"), f.quiz(t, "Two"), f.quiz(t, "Three")
	amaya, ben := f.student(t), f.student(t)
	// Amaya: 50 % then 100 % on One; 100 % on Two; 100 % on Three.
	f.take(t, amaya, q1, 1)
	f.take(t, amaya, q1, 2)
	f.take(t, amaya, q2, 2)
	f.take(t, amaya, q3, 2)
	// Ben: 50 % on One, nothing on Two.
	f.take(t, ben, q1, 1)

	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	var r grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &r)
	if len(r.Items) != 3 || r.WeightTotal != 0 || len(r.Students) != 2 {
		t.Fatalf("before setup: %+v", r)
	}
	if r.Students[0].Final != nil {
		t.Fatalf("no weights, yet a final: %+v", r.Students[0])
	}

	// One: weight 20, lowest. Two: weight 30. Three: left out.
	in := setupFor(r, func(x *grading.ReportItemInput) {
		switch x.ID {
		case q1:
			x.Weight, x.Pick = 0.2, grading.PickLow
		case q2:
			x.Weight = 0.3
		case q3:
			x.Included = false
		}
	})
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)
	if r.WeightTotal != 0.5 || r.OutOf != 50 {
		t.Fatalf("weights total %v, out of %v", r.WeightTotal, r.OutOf)
	}
	var a, b grading.ReportStudent
	for _, st := range r.Students {
		if st.UserID == amaya.User.ID {
			a = st
		} else {
			b = st
		}
	}
	m := a.Marks["quiz:"+q1]
	if m.Pct != 50 || m.Submissions != 2 || m.Points == nil || *m.Points != 10 {
		t.Fatalf("lowest of two: %+v", m)
	}
	if _, ok := a.Marks["quiz:"+q3]; ok {
		t.Fatal("an item left out is in the report")
	}
	if a.Total != 40 || a.Final == nil || *a.Final != 80 || a.Missing != 0 {
		t.Fatalf("amaya: %+v", a)
	}
	if b.Total != 10 || *b.Final != 20 || b.Missing != 1 {
		t.Fatalf("ben: %+v", b)
	}

	// Highest instead; and weight 0 stays in the report, out of the total.
	in = setupFor(r, func(x *grading.ReportItemInput) {
		switch x.ID {
		case q1:
			x.Pick = grading.PickHigh
		case q2:
			x.Weight = 0
		}
	})
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)
	for _, st := range r.Students {
		if st.UserID != amaya.User.ID {
			if st.Missing != 0 {
				t.Fatalf("a weight 0 item counted as missing: %+v", st)
			}
			continue
		}
		two, ok := st.Marks["quiz:"+q2]
		if !ok || two.Points != nil {
			t.Fatalf("weight 0 mark = %+v, %v", two, ok)
		}
		if st.Total != 20 || *st.Final != 100 || r.OutOf != 20 {
			t.Fatalf("highest: %+v of %v", st, r.WeightTotal)
		}
	}

	// Checked, audited, and the classroom's teachers' only.
	bad := setupFor(r, func(x *grading.ReportItemInput) { x.Weight = 1.5 })
	f.teacher.Call("PUT", url, bad, 422, nil)
	bad = setupFor(r, func(x *grading.ReportItemInput) { x.Pick = "middle" })
	f.teacher.Call("PUT", url, bad, 422, nil)
	bad = setupFor(r, func(*grading.ReportItemInput) {})
	bad.Exceptions = []grading.ResultException{{Kind: "quiz", ID: q1, UserID: f.e.NewUser(auth.RoleStudent).User.ID, Detail: grading.DetailMarks}}
	f.teacher.Call("PUT", url, bad, 422, nil)
	f.e.NewUser(auth.RoleTeacher).Call("GET", url, nil, 404, nil)
	amaya.Call("GET", url, nil, 403, nil)
	var n int
	f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit.events WHERE action='class_report_set' AND target_id=$1`, f.class.ID).Scan(&n)
	if n != 2 {
		t.Fatalf("audited %d times", n)
	}
}

// D-140: what a student sees: marks only, correctness or answers, for a review
// time after which the score stays and the detail closes; one student may differ.
func TestResultVisibility(t *testing.T) {
	f := newReportFx(t)
	ctx := context.Background()
	q := f.quiz(t, "One")
	amaya, ben := f.student(t), f.student(t)
	aa, ab := f.take(t, amaya, q, 1), f.take(t, ben, q, 1)
	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	result := func(s *apptest.Client, attempt string) eval.StudentResult {
		t.Helper()
		var r eval.StudentResult
		s.Call("GET", "/api/my/attempts/"+attempt+"/result", nil, 200, &r)
		return r
	}
	var rep grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &rep)
	set := func(detail string, minutes int, ex ...grading.ResultException) {
		t.Helper()
		in := setupFor(rep, func(x *grading.ReportItemInput) { x.Detail, x.ReviewMinutes = detail, minutes })
		in.Exceptions = ex
		f.teacher.Call("PUT", url, in, 200, &rep)
	}

	// Its own settings, as before: questions with correct answers.
	r := result(amaya, aa)
	if len(r.Questions) != 2 || r.Questions[0].Key == nil || r.Review.Detail != "" {
		t.Fatalf("default: %+v", r)
	}

	set(grading.DetailMarks, 0)
	r = result(amaya, aa)
	if len(r.Questions) != 0 || r.Score != 2 || r.MaxScore != 4 {
		t.Fatalf("marks only: %+v", r)
	}

	set(grading.DetailCorrectness, 0)
	r = result(amaya, aa)
	if len(r.Questions) != 2 || r.Questions[0].Key != nil || r.Questions[0].Correct == nil {
		t.Fatalf("correctness: %+v", r.Questions)
	}

	set(grading.DetailAnswers, 60)
	r = result(amaya, aa)
	if len(r.Questions) != 2 || r.Questions[0].Key == nil || r.Review.Until == nil || r.Review.Closed {
		t.Fatalf("answers within review: %+v", r)
	}

	// Time passes for both; Ben has more of it, and a different level.
	if _, err := f.e.Pool.Exec(ctx, `UPDATE live.attempts SET submitted_at = submitted_at - interval '2 hours' WHERE id = ANY($1::uuid[])`, []string{aa, ab}); err != nil {
		t.Fatal(err)
	}
	longer, level := 600, grading.DetailCorrectness
	set(grading.DetailAnswers, 60, grading.ResultException{Kind: "quiz", ID: q, UserID: ben.User.ID, Detail: level, ReviewMinutes: &longer})
	r = result(amaya, aa)
	if !r.Review.Closed || len(r.Questions) != 0 || r.Score != 2 {
		t.Fatalf("after the review: %+v", r)
	}
	r = result(ben, ab)
	if r.Review.Closed || len(r.Questions) != 2 || r.Questions[0].Key != nil {
		t.Fatalf("ben's exception: %+v", r)
	}
	// An exception of 0 is no limit.
	none := 0
	set(grading.DetailAnswers, 60, grading.ResultException{Kind: "quiz", ID: q, UserID: amaya.User.ID, ReviewMinutes: &none})
	if r = result(amaya, aa); r.Review.Closed || len(r.Questions) != 2 {
		t.Fatalf("no limit: %+v", r)
	}
}

// D-140: a finished tutorial follows the same level and review time.
func TestTutorialVisibility(t *testing.T) {
	f := newReportFx(t)
	ctx := context.Background()
	var tu struct {
		ID     string `json:"id"`
		QuizID string `json:"quiz_id"`
	}
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/tutorials", map[string]any{"title": "Atoms"}, 201, &tu)
	f.teacher.Raw("POST", "/api/teacher/quizzes/"+tu.QuizID+"/questions/import", "text/csv", []byte(reportCSV))
	if _, err := f.e.Pool.Exec(ctx, `UPDATE tutorial.tutorials SET status='published', published_at=now(), feedback='page' WHERE id=$1`, tu.ID); err != nil {
		t.Fatal(err)
	}
	var qid string
	if err := f.e.Pool.QueryRow(ctx, `SELECT id::text FROM quiz.questions WHERE quiz_id=$1 ORDER BY position LIMIT 1`, tu.QuizID).Scan(&qid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Pool.Exec(ctx, `INSERT INTO tutorial.pages (tutorial_id, position, title, question_ids) VALUES ($1, 0, 'P', ARRAY[$2]::uuid[])`, tu.ID, qid); err != nil {
		t.Fatal(err)
	}
	s := f.student(t)
	if _, err := f.e.Pool.Exec(ctx, `WITH r AS (INSERT INTO tutorial.runs (tutorial_id, user_id, finished_at) VALUES ($1, $2, now()) RETURNING id)
		INSERT INTO tutorial.answers (run_id, question_id, response, score, max_score, correct) SELECT r.id, $3, '{"selected":["o1"]}', 2, 2, true FROM r`, tu.ID, s.User.ID, qid); err != nil {
		t.Fatal(err)
	}
	type view struct {
		Finished bool `json:"finished"`
		Score    *struct {
			Score float64 `json:"score"`
		} `json:"score"`
		Review *grading.Review `json:"review"`
		Page   *struct {
			Questions []struct {
				Mark *struct {
					Correct bool      `json:"correct"`
					Key     *quiz.Key `json:"key"`
				} `json:"mark"`
			} `json:"questions"`
		} `json:"page"`
	}
	open := func() view {
		t.Helper()
		var v view
		s.Call("POST", "/api/tutorials/"+tu.ID+"/open", nil, 200, &v)
		return v
	}
	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	var rep grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &rep)
	set := func(detail string, minutes int) {
		t.Helper()
		f.teacher.Call("PUT", url, setupFor(rep, func(x *grading.ReportItemInput) { x.Detail, x.ReviewMinutes = detail, minutes }), 200, &rep)
	}

	v := open()
	if !v.Finished || v.Page.Questions[0].Mark == nil || v.Page.Questions[0].Mark.Key == nil {
		t.Fatalf("own settings: %+v", v.Page)
	}
	set(grading.DetailCorrectness, 0)
	v = open()
	if m := v.Page.Questions[0].Mark; m == nil || !m.Correct || m.Key != nil {
		t.Fatalf("correctness: %+v", m)
	}
	set(grading.DetailMarks, 0)
	v = open()
	if v.Page.Questions[0].Mark != nil || v.Score == nil || v.Score.Score != 2 {
		t.Fatalf("marks only: %+v", v)
	}
	set(grading.DetailAnswers, 60)
	if v = open(); v.Page.Questions[0].Mark == nil || v.Review == nil || v.Review.Closed {
		t.Fatalf("within the review: %+v", v)
	}
	if _, err := f.e.Pool.Exec(ctx, `UPDATE tutorial.runs SET finished_at = now() - interval '2 hours' WHERE tutorial_id = $1`, tu.ID); err != nil {
		t.Fatal(err)
	}
	if v = open(); v.Page.Questions[0].Mark != nil || !v.Review.Closed || v.Score == nil {
		t.Fatalf("after the review: %+v", v)
	}
}

// D-142: a weight is a fraction, and the gradebook's percent starts it off.
func TestReportWeightIsAFraction(t *testing.T) {
	f := newReportFx(t)
	q := f.quiz(t, "One")
	s := f.student(t)
	f.take(t, s, q, 1) // 50 %
	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	f.teacher.Call("PUT", "/api/teacher/classrooms/"+f.class.ID+"/weights", map[string]any{"weights": []map[string]any{{"kind": "quiz", "id": q, "weight": 40}}}, 200, nil)
	var r grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &r)
	if r.Items[0].Weight != 0.4 {
		t.Fatalf("started from %v", r.Items[0].Weight)
	}
	// 50 % × 0.125 keeps the precision of the weight.
	in := setupFor(r, func(x *grading.ReportItemInput) { x.Weight = 0.125 })
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)
	st := r.Students[0]
	if m := st.Marks["quiz:"+q]; m.Points == nil || *m.Points != 6.25 {
		t.Fatalf("final mark: %+v", m)
	}
	if st.Total != 6.25 || r.OutOf != 12.5 || *st.Final != 50 {
		t.Fatalf("total %v of %v, final %v", st.Total, r.OutOf, *st.Final)
	}
}

// D-145, the product owner's example: 30/40 at 10%, 20/20 at 20% and 75/100 at
// 10% add 7.5, 20 and 7.5, a total of 35 out of 40.
func TestReportProductOwnersExample(t *testing.T) {
	f := newReportFx(t)
	a1, a2, a3 := f.quizOf(t, "Assignment 1", 4, 10), f.quizOf(t, "Assignment 2", 2, 10), f.quizOf(t, "Assignment 3", 4, 25)
	s := f.student(t)
	f.takeOf(t, s, a1, 3, 4)
	f.takeOf(t, s, a2, 2, 2)
	f.takeOf(t, s, a3, 3, 4)

	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	var r grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &r)
	weights := map[string]float64{a1: 0.1, a2: 0.2, a3: 0.1}
	in := setupFor(r, func(x *grading.ReportItemInput) { x.Included, x.Weight = true, weights[x.ID] })
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)

	st := r.Students[0]
	for id, want := range map[string]struct{ score, max, points float64 }{a1: {30, 40, 7.5}, a2: {20, 20, 20}, a3: {75, 100, 7.5}} {
		m := st.Marks["quiz:"+id]
		if m.Score != want.score || m.Max != want.max || m.Points == nil || *m.Points != want.points {
			t.Fatalf("want %+v, got %+v, points %v", want, m, m.Points)
		}
	}
	if st.Total != 35 || r.OutOf != 40 || *st.Final != 87.5 {
		t.Fatalf("total %v of %v, final %v", st.Total, r.OutOf, *st.Final)
	}
}

// D-145: a final mark comes from the marks themselves, not the percentage
// rounded for showing: 1/3 at weight 1 is 33.33, not 33.3; the same for groups.
func TestReportFinalMarkIsExact(t *testing.T) {
	f := newReportFx(t)
	q := f.quizOf(t, "Thirds", 3, 1)
	s := f.student(t)
	f.takeOf(t, s, q, 1, 3)
	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	var r grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &r)
	f.classGroup(t, "G", s)
	in := setupFor(r, func(x *grading.ReportItemInput) { x.Weight = 1 })
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)
	m := r.Students[0].Marks["quiz:"+q]
	if m.Pct != 33.3 || m.Points == nil || *m.Points != 33.33 || r.Students[0].Total != 33.33 {
		t.Fatalf("mark %+v, points %v", m, m.Points)
	}
	if len(r.ClassGroups) != 1 {
		t.Fatalf("groups: %+v", r.ClassGroups)
	}
	if g := r.ClassGroups[0].Marks["quiz:"+q]; g.Points == nil || *g.Points != 33.33 {
		t.Fatalf("group mark %+v", g)
	}
}

// D-156: a report can also show violations, attendance and dates.
func TestReportExtras(t *testing.T) {
	f := newReportFx(t)
	q := f.quiz(t, "One")
	other := f.quiz(t, "Two")
	s := f.student(t)
	attempt := f.take(t, s, q, 2)
	f.take(t, s, other, 2)
	if _, err := f.e.Pool.Exec(context.Background(), `UPDATE live.attempts SET violations = 3 WHERE id = $1`, attempt); err != nil {
		t.Fatal(err)
	}
	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	var r grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &r)
	if r.Options != (grading.ReportOptions{}) {
		t.Fatalf("on by default: %+v", r.Options)
	}
	if r.Items[0].Date.IsZero() {
		t.Fatalf("no date: %+v", r.Items[0])
	}
	st := r.Students[0]
	if st.ViolationsTotal != 3 || st.Violations["quiz:"+q] != 3 {
		t.Fatalf("violations: %+v", st)
	}

	// Saved, and left alone by a setup that doesn't mention them.
	in := setupFor(r, func(*grading.ReportItemInput) {})
	in.Options = &grading.ReportOptions{Violations: true, Dates: true}
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)
	if !r.Options.Violations || !r.Options.Dates || r.Options.Attendance {
		t.Fatalf("saved: %+v", r.Options)
	}
	in.Options = nil
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)
	if !r.Options.Violations {
		t.Fatalf("lost: %+v", r.Options)
	}

	// A quiz left out of the report takes its violations with it.
	in = setupFor(r, func(x *grading.ReportItemInput) { x.Included = x.ID != q })
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)
	if r.Students[0].ViolationsTotal != 0 {
		t.Fatalf("left out, still counted: %+v", r.Students[0])
	}

	// An attempt thrown out still shows its violations.
	if _, err := f.e.Pool.Exec(context.Background(), `UPDATE eval.results SET invalidated = true WHERE attempt_id = $1`, attempt); err != nil {
		t.Fatal(err)
	}
	in = setupFor(r, func(x *grading.ReportItemInput) { x.Included = true })
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)
	if m := r.Students[0]; m.Violations["quiz:"+q] != 3 {
		t.Fatalf("thrown out: %+v", m)
	}
}
