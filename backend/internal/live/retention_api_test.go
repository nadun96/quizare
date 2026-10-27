package live_test

import (
	"context"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app"
	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/config"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// ended makes a session and ends it, then dates its end daysAgo.
func ended(t *testing.T, e *apptest.Env, teacher *apptest.Client, quizID string, daysAgo int) string {
	t.Helper()
	var s live.Session
	teacher.Call("POST", "/api/teacher/quizzes/"+quizID+"/sessions", map[string]any{}, 201, &s)
	teacher.Call("POST", "/api/teacher/sessions/"+s.ID+"/end", map[string]any{}, 204, nil)
	if _, err := e.Pool.Exec(context.Background(), `UPDATE live.sessions SET ended_at = now() - make_interval(days => $2) WHERE id=$1`, s.ID, daysAgo); err != nil {
		t.Fatal(err)
	}
	return s.ID
}

func exists(t *testing.T, e *apptest.Env, id string) bool {
	t.Helper()
	var ok bool
	if err := e.Pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM live.sessions WHERE id=$1)`, id).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// Old quiz results go through the admin's clean-up with a preview, and by
// themselves when a retention period is set (D-94); open sessions never.
func TestOldQuizResults(t *testing.T) {
	e := apptest.New(t, func(c *config.Config, _ *app.Options) { c.ResultsRetentionDays = 30 })
	admin := e.NewUser(auth.RoleAdmin)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Quiz"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(threeQuestions))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)

	veryOld := ended(t, e, teacher, q.ID, 400)
	monthOld := ended(t, e, teacher, q.ID, 40)
	recent := ended(t, e, teacher, q.ID, 5)
	var open live.Session
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &open)
	if _, err := e.Pool.Exec(context.Background(), `UPDATE live.sessions SET created_at = now() - interval '500 days' WHERE id=$1`, open.ID); err != nil {
		t.Fatal(err)
	}

	// The admin's clean-up: a preview, then the same.
	before := time.Now().AddDate(0, 0, -100).UTC().Format(time.DateOnly)
	var f struct{ Items int }
	admin.Call("POST", "/api/admin/storage/cleanup/preview", map[string]string{"area": "quiz_sessions", "before": before}, 200, &f)
	if f.Items != 1 {
		t.Fatalf("preview = %+v", f)
	}
	admin.Call("POST", "/api/admin/storage/cleanup", map[string]string{"area": "quiz_sessions", "before": before}, 200, &f)
	if f.Items != 1 || exists(t, e, veryOld) || !exists(t, e, monthOld) {
		t.Fatalf("clean-up = %+v", f)
	}

	// The retention rule: 30 days.
	e.App.Storage.RunRetention(context.Background())
	if exists(t, e, monthOld) || !exists(t, e, recent) || !exists(t, e, open.ID) {
		t.Fatalf("retention: month old %v, recent %v, open %v", exists(t, e, monthOld), exists(t, e, recent), exists(t, e, open.ID))
	}
	var n int
	if err := e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit.events WHERE action='retention_run' AND target_id='quiz_sessions' AND actor_id IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit = %d, %v", n, err)
	}
}
