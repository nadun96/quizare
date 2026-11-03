package grading_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/grading"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/quiz"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

const csvQuiz = `question_code,type,question_text,options,correct_answer,marks
Q1,SINGLE,Capital of France?,Paris|Rome|Madrid,Paris,1
Q2,BLANK_TEXT,Water boils at [[1]] C,,1=100,1
`

type fx struct {
	e       *apptest.Env
	teacher *apptest.Client
	class   string
	topic   string
}

func (f *fx) quiz(t *testing.T, title string) string {
	var q quiz.Quiz
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic+"/quizzes", map[string]any{"title": title}, 201, &q)
	f.teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(csvQuiz))
	f.teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	return q.ID
}

// take: each student by number answers Q1 (always right) and Q2 right or not: 100 % or 50 %.
func (f *fx) take(t *testing.T, quizID string, students map[string]bool) live.Session {
	var sess live.Session
	f.teacher.Call("POST", "/api/teacher/quizzes/"+quizID+"/sessions", map[string]any{}, 201, &sess)
	for num, right := range students {
		s := f.e.Client()
		code, _ := s.Do("POST", "/api/auth/login", map[string]string{"email": num + "@example.com", "password": "password123"})
		if code != 200 {
			s.Call("POST", "/api/auth/register", map[string]string{"email": num + "@example.com", "name": "Student " + num, "password": "password123", "role": "student"}, 201, nil)
		}
		var st live.StudentState
		s.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{"student_number": num}, 200, &st)
		f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
		s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{"o1"}}}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
		blank := "99"
		if right {
			blank = "100"
		}
		s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Blanks: map[string]string{"1": blank}}}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
	}
	for _, raw := range f.e.TakeJobs("evaluate_attempt") {
		var a eval.EvaluateAttemptArgs
		json.Unmarshal(raw, &a)
		if err := f.e.App.Eval.EvaluateAttempt(context.Background(), a.AttemptID); err != nil {
			t.Fatal(err)
		}
	}
	return sess
}

func student(g grading.Gradebook, name string) grading.Student {
	for _, s := range g.Students {
		if s.Name == name {
			return s
		}
	}
	return grading.Student{}
}

func pts(m grading.Mark) float64 {
	if m.Points == nil {
		return -1
	}
	return *m.Points
}

// The gradebook (D-122): each item's weight as a percent of the final
// result, marks turned into points (55 % on a 10 % quiz is 5.5), weights
// that needn't add up to 100, and attendance at quiz sessions.
func TestGradebook(t *testing.T) {
	e := apptest.New(t)
	ctx := context.Background()
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Physics", "settings": map[string]any{"student_id_required": true}}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "Term 1"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "Heat"}, 201, &topic)
	f := &fx{e: e, teacher: teacher, class: c.ID, topic: topic.ID}

	q1, q2 := f.quiz(t, "Heat check"), f.quiz(t, "Light check")
	s1 := f.take(t, q1, map[string]bool{"s1": true, "s2": false}) // Student s1 100 %, s2 50 %
	f.take(t, q2, map[string]bool{"s1": false})                   // s1 50 %; s2 doesn't take it
	// A third student enrols and takes nothing.
	s3 := e.Client()
	s3.Call("POST", "/api/auth/register", map[string]string{"email": "s3@example.com", "name": "Student s3", "password": "password123", "role": "student"}, 201, nil)
	s3.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "s3"}, 200, nil)

	// A tutorial with questions: s2 finished it with 1 of 2 marks.
	var tu struct {
		ID     string `json:"id"`
		QuizID string `json:"quiz_id"`
	}
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/tutorials", map[string]any{"title": "Cells"}, 201, &tu)
	teacher.Raw("POST", "/api/teacher/quizzes/"+tu.QuizID+"/questions/import", "text/csv", []byte(csvQuiz))
	if _, err := e.Pool.Exec(ctx, `UPDATE tutorial.tutorials SET status = 'published', published_at = now() WHERE id = $1`, tu.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pool.Exec(ctx, `WITH r AS (INSERT INTO tutorial.runs (tutorial_id, user_id, finished_at)
			SELECT $1, id, now() FROM auth.users WHERE email = 's2@example.com' RETURNING id)
		INSERT INTO tutorial.answers (run_id, question_id, response, score, max_score)
		SELECT r.id, x.id, '{}', CASE WHEN x.code = 'Q1' THEN 1 ELSE 0 END, 1 FROM r, quiz.questions x WHERE x.quiz_id = $2`, tu.ID, tu.QuizID); err != nil {
		t.Fatal(err)
	}

	path := "/api/teacher/classrooms/" + c.ID
	var g grading.Gradebook
	teacher.Call("GET", path+"/gradebook", nil, 200, &g)
	if len(g.Items) != 3 || g.Items[0].Title != "Heat check" || g.Items[2].Kind != "tutorial" || g.WeightTotal != 0 || len(g.Students) != 3 {
		t.Fatalf("gradebook = %+v", g)
	}
	// D-130: where each item sits, and the sessions run here, each with its numbers.
	for _, it := range g.Items {
		if it.ModuleID == "" || it.TopicID == "" || it.Module == "" {
			t.Fatalf("item without its module or topic: %+v", it)
		}
	}
	if len(g.Sessions) != 2 || g.Sessions[0].QuizID == "" || g.Sessions[0].Attempts < 1 || g.Sessions[0].Results < 1 || g.Sessions[0].Avg == nil {
		t.Fatalf("sessions = %+v", g.Sessions)
	}
	s2 := student(g, "Student s2")
	if s2.Marks[grading.Key("tutorial", tu.ID)].Pct != 50 || s2.Final != nil || *s2.Average != 50 {
		t.Fatalf("before weights, s2 = %+v", s2)
	}

	// 10 % for the first quiz, 25 % for the second, 5 % for the tutorial: 40 % in all.
	w := []grading.WeightInput{{Kind: "quiz", ID: q1, Weight: 10}, {Kind: "quiz", ID: q2, Weight: 25}, {Kind: "tutorial", ID: tu.ID, Weight: 5}}
	teacher.Call("PUT", path+"/weights", map[string]any{"weights": w}, 200, &g)
	if g.WeightTotal != 40 || *g.Items[0].Weight != 10 {
		t.Fatalf("weights = %+v", g.Items)
	}
	one := student(g, "Student s1") // 100 % × 10 + 50 % × 25 = 10 + 12.5; no tutorial: 0
	if pts(one.Marks[grading.Key("quiz", q1)]) != 10 || pts(one.Marks[grading.Key("quiz", q2)]) != 12.5 || one.Total != 22.5 || one.Missing != 1 || *one.Final != 56.3 {
		t.Fatalf("s1 = %+v", one)
	}
	s2 = student(g, "Student s2") // 50 % × 10 = 5; tutorial 50 % × 5 = 2.5
	if s2.Total != 7.5 || s2.Missing != 1 {
		t.Fatalf("s2 = %+v", s2)
	}
	three := student(g, "Student s3")
	if three.Total != 0 || three.Missing != 3 || *three.Final != 0 || three.Average != nil {
		t.Fatalf("s3 = %+v", three)
	}

	// The example: 55 % on a quiz worth 10 % gives 5.5.
	if _, err := e.Pool.Exec(ctx, `UPDATE eval.results SET pct = 55 WHERE user_id = (SELECT id FROM auth.users WHERE email = 's1@example.com') AND session_id = $1`, s1.ID); err != nil {
		t.Fatal(err)
	}
	teacher.Call("GET", path+"/gradebook", nil, 200, &g)
	if pts(student(g, "Student s1").Marks[grading.Key("quiz", q1)]) != 5.5 {
		t.Fatalf("55 %% of 10 = %+v", student(g, "Student s1").Marks)
	}

	// A weight of 0 takes an item out; what can't be set.
	teacher.Call("PUT", path+"/weights", map[string]any{"weights": []grading.WeightInput{{Kind: "quiz", ID: q1, Weight: 10}, {Kind: "quiz", ID: q2, Weight: 0}}}, 200, &g)
	if g.WeightTotal != 10 || g.Items[1].Weight != nil || student(g, "Student s1").Marks[grading.Key("quiz", q2)].Points != nil {
		t.Fatalf("q2 at 0 = %+v", g.Items)
	}
	teacher.Call("PUT", path+"/weights", map[string]any{"weights": []grading.WeightInput{{Kind: "quiz", ID: q1, Weight: 60}, {Kind: "quiz", ID: q2, Weight: 50}}}, 422, nil)
	teacher.Call("PUT", path+"/weights", map[string]any{"weights": []grading.WeightInput{{Kind: "quiz", ID: q1, Weight: -1}}}, 422, nil)
	teacher.Call("PUT", path+"/weights", map[string]any{"weights": []grading.WeightInput{{Kind: "quiz", ID: c.ID, Weight: 5}}}, 422, nil)

	// Attendance: two quiz sessions; s3 came to neither.
	var a grading.Attendance
	teacher.Call("GET", path+"/attendance", nil, 200, &a)
	if len(a.Events) != 2 || len(a.Students) != 3 {
		t.Fatalf("attendance = %+v", a)
	}
	if e := a.Events[0]; e.QuizID == "" || e.TopicID == "" || e.ModuleID == "" || e.Module == "" {
		t.Fatalf("a quiz session without where it sits: %+v", e)
	}
	byName := map[string]int{}
	for _, s := range a.Students {
		byName[s.Name] = len(s.Attended)
	}
	if byName["Student s1"] != 2 || byName["Student s2"] != 1 || byName["Student s3"] != 0 {
		t.Fatalf("attended = %v", byName)
	}
	teacher.Call("GET", path+"/attendance?from=2001-01-01&to=2001-01-31", nil, 200, &a)
	if len(a.Events) != 0 {
		t.Fatalf("an empty month = %+v", a.Events)
	}
	teacher.Call("GET", path+"/attendance?from=nope", nil, 422, nil)

	// Others can't; a co-teacher can.
	other := e.NewUser(auth.RoleTeacher)
	other.Call("GET", path+"/gradebook", nil, 404, nil)
	other.Call("PUT", path+"/weights", map[string]any{"weights": w}, 404, nil)
	teacher.Call("POST", path+"/teachers", map[string]string{"email": other.User.Email}, 201, nil)
	other.Call("POST", "/api/teacher/invitations/"+c.ID+"/accept", nil, 204, nil)
	other.Call("GET", path+"/gradebook", nil, 200, nil)
}
