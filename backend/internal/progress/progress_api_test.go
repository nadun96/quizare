package progress_test

import (
	"context"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/progress"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

// A student's progress counts only released, fully marked results, and
// only the student sees it (D-97).
func TestProgressCountsReleasedResults(t *testing.T) {
	e := apptest.New(t)
	teacher, student := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleStudent)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q struct{ ID string }
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Cells"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,A?,A|B,A,1\n"))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess struct{ ID, Title string }
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)

	ctx := context.Background()
	var attempt string
	if err := e.Pool.QueryRow(ctx, `INSERT INTO live.attempts (session_id, user_id, state, submitted_at) VALUES ($1, $2, 'submitted', now()) RETURNING id`, sess.ID, student.User.ID).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pool.Exec(ctx, `INSERT INTO eval.results (attempt_id, session_id, user_id, score, max_score, pct, pass_mark_pct, passed, complete, invalidated)
		VALUES ($1, $2, $3, 1, 1, 100, 50, true, true, false)`, attempt, sess.ID, student.User.ID); err != nil {
		t.Fatal(err)
	}

	var p progress.Progress
	student.Call("GET", "/api/my/progress", nil, 200, &p)
	if p.XP != 0 || p.Quizzes != 0 || p.Level != 1 {
		t.Fatalf("before release = %+v", p)
	}
	teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/end", map[string]any{}, 204, nil)
	teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/release", nil, 204, nil)
	student.Call("GET", "/api/my/progress", nil, 200, &p)
	if p.XP != 130 || p.Quizzes != 1 || p.Level != 2 || !p.ThisWeek || p.Streak != 1 || len(p.Recent) != 1 || p.Recent[0].XP != 130 {
		t.Fatalf("after release = %+v", p)
	}
	earned := map[string]bool{}
	for _, b := range p.Badges {
		earned[b.ID] = b.Earned
	}
	if !earned["first_quiz"] || !earned["full_marks"] || earned["ten_quizzes"] {
		t.Fatalf("badges = %+v", p.Badges)
	}

	// Invalidated results don't count; teachers have no progress page.
	if _, err := e.Pool.Exec(ctx, `UPDATE eval.results SET invalidated = true WHERE attempt_id = $1`, attempt); err != nil {
		t.Fatal(err)
	}
	student.Call("GET", "/api/my/progress", nil, 200, &p)
	if p.XP != 0 {
		t.Fatalf("invalidated = %+v", p)
	}
	teacher.Call("GET", "/api/my/progress", nil, 404, nil)
	e.Client().Call("GET", "/api/my/progress", nil, 401, nil)
}
