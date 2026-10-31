package dashboard_test

import (
	"context"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/dashboard"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

type page struct {
	Items []dashboard.Item
	Total int
}

func list(t *testing.T, c *apptest.Client, name string) page {
	t.Helper()
	var p page
	c.Call("GET", "/api/my/dashboard/"+name+"?size=50", nil, 200, &p)
	return p
}

func find(p page, kind, title string) *dashboard.Item {
	for _, it := range p.Items {
		if it.Kind == kind && it.Title == title {
			return &it
		}
	}
	return nil
}

// A student's and a teacher's dashboards: what's live, coming, overdue or
// missed, and waiting, one page at a time (D-99).
func TestDashboards(t *testing.T) {
	e := apptest.New(t)
	teacher, student := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleStudent)
	ctx := context.Background()
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 8"}, 201, &c)
	student.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)
	// Enrolled a day ago, so quizzes after that count as missed.
	if _, err := e.Pool.Exec(ctx, `UPDATE content.enrolments SET created_at = now() - interval '1 day' WHERE classroom_id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q struct{ ID string }
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Cells"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,A?,A|B,A,1\n"))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)

	// A quiz that ended without the student, and one open now.
	type session struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		JoinCode string `json:"join_code"`
	}
	var missed, open session
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &missed)
	teacher.Call("POST", "/api/teacher/sessions/"+missed.ID+"/end", map[string]any{}, 204, nil)
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &open)

	// Tutorials: due tomorrow, overdue, not opened yet.
	tutorial := func(title, opens, due string) {
		var tu struct{ ID string }
		teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/tutorials", map[string]any{"title": title}, 201, &tu)
		if _, err := e.Pool.Exec(ctx, `UPDATE tutorial.tutorials SET status='published', published_at=now(),
			opens_at = CASE WHEN $2 = '' THEN NULL ELSE now() + $2::interval END, due_at = CASE WHEN $3 = '' THEN NULL ELSE now() + $3::interval END WHERE id=$1`, tu.ID, opens, due); err != nil {
			t.Fatal(err)
		}
	}
	tutorial("Due tomorrow", "", "1 day")
	tutorial("Overdue", "", "-2 days")
	tutorial("Opens next week", "7 days", "10 days")
	tutorial("No date", "", "")

	// The student.
	var sum dashboard.Summary
	student.Call("GET", "/api/my/dashboard", nil, 200, &sum)
	if sum.Role != "student" || sum.Counts["live"] != 1 || sum.Counts["upcoming"] != 2 || sum.Counts["overdue"] != 2 || sum.Counts["pending"] != 1 {
		t.Fatalf("student summary = %+v", sum)
	}
	if it := find(list(t, student, "live"), "quiz", open.Title); it == nil || it.Href != "/j/"+open.JoinCode {
		t.Fatalf("live quiz = %+v", it)
	}
	up := list(t, student, "upcoming")
	if up.Items[0].Title != "Due tomorrow" || up.Items[0].Status != "due_soon" || up.Items[1].Status != "opens" || up.Items[1].Href != "" {
		t.Fatalf("upcoming = %+v", up.Items)
	}
	over := list(t, student, "overdue")
	if it := find(over, "quiz", missed.Title); it == nil || it.Status != "missed" {
		t.Fatalf("missed = %+v", over.Items)
	}
	if it := find(over, "tutorial", "Overdue"); it == nil || it.Status != "overdue" || it.Href == "" {
		t.Fatalf("overdue = %+v", over.Items)
	}
	if it := find(list(t, student, "pending"), "tutorial", "No date"); it == nil || it.Status != "not_started" {
		t.Fatal("no-date tutorial not pending")
	}
	// One page at a time.
	var p page
	student.Call("GET", "/api/my/dashboard/upcoming?size=1&page=2", nil, 200, &p)
	if p.Total != 2 || len(p.Items) != 1 || p.Items[0].Title != "Opens next week" {
		t.Fatalf("page 2 = %+v", p)
	}
	student.Call("GET", "/api/my/dashboard/nope", nil, 404, nil)

	// The teacher: their live quiz, upcoming and overdue tutorials, and a
	// student asking to join another classroom.
	var closed content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Closed", "settings": map[string]any{"enrolment_approval": true}}, 201, &closed)
	other := e.NewUser(auth.RoleStudent)
	other.Call("POST", "/api/enrolments", map[string]string{"join_code": closed.JoinCode}, 200, nil)
	teacher.Call("GET", "/api/my/dashboard", nil, 200, &sum)
	if sum.Role != "teacher" || sum.Counts["live"] != 1 || sum.Counts["upcoming"] != 2 || sum.Counts["overdue"] != 1 {
		t.Fatalf("teacher summary = %+v", sum)
	}
	if it := find(list(t, teacher, "overdue"), "tutorial", "Overdue"); it == nil || it.Detail != "0 of 1 student finished" {
		t.Fatalf("teacher overdue = %+v", it)
	}
	if it := find(list(t, teacher, "pending"), "enrolment", "Closed"); it == nil || it.Status != "to_approve" || it.Href != "/t/classrooms/"+closed.ID {
		t.Fatalf("teacher pending = %+v", it)
	}
	// Nothing of another teacher's.
	stranger := e.NewUser(auth.RoleTeacher)
	stranger.Call("GET", "/api/my/dashboard", nil, 200, &sum)
	for _, n := range sum.Counts {
		if n != 0 {
			t.Fatalf("stranger sees %+v", sum.Counts)
		}
	}
}
