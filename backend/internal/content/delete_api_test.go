package content_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
)

const oneQuestion = `question_code,type,question_text,options,correct_answer,marks
Q1,SINGLE,Capital of France?,Paris|Rome|Madrid,Paris,1
`

type tree struct {
	classroom content.Classroom
	module    content.Module
	topic     content.Topic
	quizID    string
}

// build makes a classroom with a module, a topic, a ready quiz, a tutorial
// and a presentation in it.
func build(t *testing.T, e *apptest.Env, teacher *apptest.Client, name string) tree {
	t.Helper()
	var tr tree
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": name}, 201, &tr.classroom)
	teacher.Call("POST", "/api/teacher/classrooms/"+tr.classroom.ID+"/modules", map[string]any{"name": "Unit 1"}, 201, &tr.module)
	teacher.Call("POST", "/api/teacher/modules/"+tr.module.ID+"/topics", map[string]any{"name": "Cells"}, 201, &tr.topic)
	var q struct{ ID string }
	teacher.Call("POST", "/api/teacher/topics/"+tr.topic.ID+"/quizzes", map[string]any{"title": "Cells quiz"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(oneQuestion))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	tr.quizID = q.ID
	teacher.Call("POST", "/api/teacher/topics/"+tr.topic.ID+"/tutorials", map[string]any{"title": "Cells tutorial"}, 201, nil)
	if _, err := e.Pool.Exec(context.Background(), `INSERT INTO presentation.presentations (topic_id, teacher_id, title) VALUES ($1, $2, 'Cells slides')`, tr.topic.ID, teacher.User.ID); err != nil {
		t.Fatal(err)
	}
	return tr
}

func count(t *testing.T, e *apptest.Env, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Content without results goes with its classroom, module or topic at once:
// it used to fail with a 500, quizzes, tutorials and presentations refusing
// to lose their topic (D-92).
func TestDeletingTakesTheContentInside(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)

	tr := build(t, e, teacher, "Topic only")
	teacher.Call("DELETE", "/api/teacher/topics/"+tr.topic.ID, nil, 204, nil)
	tr2 := build(t, e, teacher, "Module only")
	teacher.Call("DELETE", "/api/teacher/modules/"+tr2.module.ID, nil, 204, nil)
	tr3 := build(t, e, teacher, "Whole classroom")
	var c content.Contents
	teacher.Call("GET", "/api/teacher/classrooms/"+tr3.classroom.ID+"/delete-preview", nil, 200, &c)
	if c.Name != "Whole classroom" || c.Modules != 1 || c.Topics != 1 || c.Quizzes != 1 || c.Tutorials != 1 || c.Presentations != 1 || c.NeedsConfirm {
		t.Fatalf("preview = %+v", c)
	}
	teacher.Call("DELETE", "/api/teacher/classrooms/"+tr3.classroom.ID, nil, 204, nil)

	for _, tr := range []tree{tr, tr2, tr3} {
		if n := count(t, e, `SELECT count(*) FROM quiz.quizzes WHERE id=$1`, tr.quizID); n != 0 {
			t.Fatalf("quiz kept")
		}
	}
	if n := count(t, e, `SELECT count(*) FROM tutorial.tutorials WHERE teacher_id=$1`, teacher.User.ID) +
		count(t, e, `SELECT count(*) FROM presentation.presentations WHERE teacher_id=$1`, teacher.User.ID); n != 0 {
		t.Fatalf("%d tutorials and presentations kept", n)
	}
	// Tutorials keep their questions in a quiz of their own: gone too.
	if n := count(t, e, `SELECT count(*) FROM quiz.quizzes WHERE teacher_id=$1 AND kind <> 'bank'`, teacher.User.ID); n != 0 {
		t.Fatalf("%d quizzes kept", n)
	}
	// Nobody else's: another teacher gets 404 for the preview and the delete.
	tr4 := build(t, e, teacher, "Mine")
	other := e.NewUser(auth.RoleTeacher)
	other.Call("GET", "/api/teacher/classrooms/"+tr4.classroom.ID+"/delete-preview", nil, 404, nil)
	other.Call("DELETE", "/api/teacher/topics/"+tr4.topic.ID, nil, 404, nil)
}

// With results, the name must be typed; nothing goes while a session is
// open; then the sessions, attempts and students' tutorial work go too.
func TestDeletingResultsNeedsTheName(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	tr := build(t, e, teacher, "Grade 7 / B")
	var sess struct{ ID string }
	teacher.Call("POST", "/api/teacher/quizzes/"+tr.quizID+"/sessions", map[string]any{}, 201, &sess)
	student := e.NewUser(auth.RoleStudent)
	if _, err := e.Pool.Exec(context.Background(), `INSERT INTO live.attempts (session_id, user_id, state) VALUES ($1, $2, 'submitted')`, sess.ID, student.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pool.Exec(context.Background(), `INSERT INTO tutorial.runs (tutorial_id, user_id) SELECT id, $1 FROM tutorial.tutorials WHERE topic_id=$2`, student.User.ID, tr.topic.ID); err != nil {
		t.Fatal(err)
	}

	var c content.Contents
	teacher.Call("GET", "/api/teacher/classrooms/"+tr.classroom.ID+"/delete-preview", nil, 200, &c)
	if c.Sessions != 1 || c.Attempts != 1 || c.TutorialRuns != 1 || c.LiveSessions != 1 || !c.NeedsConfirm {
		t.Fatalf("preview = %+v", c)
	}
	path := "/api/teacher/classrooms/" + tr.classroom.ID + "?confirm=" + url.QueryEscape("Grade 7 / B")
	var bad struct{ Code string }
	teacher.Call("DELETE", path, nil, 409, &bad)
	if bad.Code != "session_live" {
		t.Fatalf("live = %+v", bad)
	}
	teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/end", map[string]any{}, 204, nil)
	teacher.Call("DELETE", "/api/teacher/topics/"+tr.topic.ID, nil, 409, &bad)
	if bad.Code != "confirm_needed" {
		t.Fatalf("no name = %+v", bad)
	}
	teacher.Call("DELETE", "/api/teacher/classrooms/"+tr.classroom.ID+"?confirm=Grade+7", nil, 409, nil)
	teacher.Call("DELETE", path, nil, 204, nil)

	for what, sql := range map[string]string{
		"sessions": `SELECT count(*) FROM live.sessions WHERE id=$1`,
		"attempts": `SELECT count(*) FROM live.attempts WHERE session_id=$1`,
	} {
		if n := count(t, e, sql, sess.ID); n != 0 {
			t.Fatalf("%s kept: %d", what, n)
		}
	}
	if n := count(t, e, `SELECT count(*) FROM tutorial.runs WHERE user_id=$1`, student.User.ID); n != 0 {
		t.Fatalf("tutorial work kept")
	}
	if n := count(t, e, `SELECT count(*) FROM audit.events WHERE action='classroom_deleted' AND target_id=$1 AND (details->>'sessions')::int = 1`, tr.classroom.ID); n != 1 {
		t.Fatalf("audit = %d", n)
	}
}

// A quiz or tutorial with results: archived (quiz) or refused (tutorial)
// as before, unless its title is typed.
func TestDeletingAQuizOrTutorialWithResults(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	tr := build(t, e, teacher, "Quizzes")
	var sess struct{ ID string }
	teacher.Call("POST", "/api/teacher/quizzes/"+tr.quizID+"/sessions", map[string]any{}, 201, &sess)

	var out struct{ Archived, Deleted bool }
	teacher.Call("DELETE", "/api/teacher/quizzes/"+tr.quizID+"?confirm=Cells+quiz", nil, 409, nil) // still open
	teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/end", map[string]any{}, 204, nil)
	teacher.Call("DELETE", "/api/teacher/quizzes/"+tr.quizID, nil, 200, &out)
	if !out.Archived {
		t.Fatalf("without the title: %+v", out)
	}
	teacher.Call("DELETE", "/api/teacher/quizzes/"+tr.quizID+"?confirm=Wrong", nil, 409, nil)
	teacher.Call("DELETE", "/api/teacher/quizzes/"+tr.quizID+"?confirm=Cells+quiz", nil, 200, &out)
	if !out.Deleted || count(t, e, `SELECT count(*) FROM live.sessions WHERE id=$1`, sess.ID) != 0 || count(t, e, `SELECT count(*) FROM quiz.quizzes WHERE id=$1`, tr.quizID) != 0 {
		t.Fatalf("with the title: %+v", out)
	}

	var tu struct{ ID string }
	teacher.Call("POST", "/api/teacher/topics/"+tr.topic.ID+"/tutorials", map[string]any{"title": "Worked"}, 201, &tu)
	student := e.NewUser(auth.RoleStudent)
	if _, err := e.Pool.Exec(context.Background(), `INSERT INTO tutorial.runs (tutorial_id, user_id) VALUES ($1, $2)`, tu.ID, student.User.ID); err != nil {
		t.Fatal(err)
	}
	teacher.Call("DELETE", "/api/teacher/tutorials/"+tu.ID, nil, 409, nil)
	teacher.Call("DELETE", "/api/teacher/tutorials/"+tu.ID+"?confirm=Worked", nil, 204, nil)
	if count(t, e, `SELECT count(*) FROM tutorial.tutorials WHERE id=$1`, tu.ID) != 0 {
		t.Fatal("tutorial kept")
	}
}
