package content_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// Co-teachers (D-121): the owner invites a teacher, who teaches the
// classroom once they accept; the classroom itself stays the owner's.
func TestCoteachers(t *testing.T) {
	e := apptest.New(t)
	owner := e.NewUser(auth.RoleTeacher)
	co := e.NewUser(auth.RoleTeacher)
	other := e.NewUser(auth.RoleTeacher)
	student := e.NewUser(auth.RoleStudent)

	var c content.Classroom
	owner.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Physics 10A"}, 201, &c)
	var m content.Module
	owner.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "Term 1"}, 201, &m)
	var topic content.Topic
	owner.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "Heat"}, 201, &topic)
	var q quiz.Quiz
	owner.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Heat check"}, 201, &q)
	owner.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,Hot?,Yes|No,Yes,1\n"))
	owner.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	base := "/api/teacher/classrooms/" + c.ID

	// Who can be invited.
	owner.Call("POST", base+"/teachers", map[string]string{"email": "nobody@example.com"}, 422, nil)
	owner.Call("POST", base+"/teachers", map[string]string{"email": student.User.Email}, 422, nil)
	owner.Call("POST", base+"/teachers", map[string]string{"email": owner.User.Email}, 422, nil)
	e.TakeJobs("email")
	var inv content.Teacher
	owner.Call("POST", base+"/teachers", map[string]string{"email": strings.ToUpper(co.User.Email)}, 201, &inv)
	if inv.Status != "invited" || inv.UserID != co.User.ID {
		t.Fatalf("invite = %+v", inv)
	}
	jobs := e.TakeJobs("email")
	var msg mail.Args
	if len(jobs) != 1 || json.Unmarshal(jobs[0], &msg) != nil || msg.Message.To != co.User.Email || !strings.Contains(msg.Message.Body, "Physics 10A") {
		t.Fatalf("invitation email = %s", jobs)
	}

	// Invited isn't yet teaching.
	co.Call("GET", base, nil, 404, nil)
	var invs struct{ Invitations []content.Invitation }
	co.Call("GET", "/api/teacher/invitations", nil, 200, &invs)
	if len(invs.Invitations) != 1 || invs.Invitations[0].Classroom != "Physics 10A" || invs.Invitations[0].OwnerName != owner.User.Name {
		t.Fatalf("invitations = %+v", invs)
	}
	other.Call("POST", "/api/teacher/invitations/"+c.ID+"/accept", nil, 404, nil)
	co.Call("POST", "/api/teacher/invitations/"+c.ID+"/accept", nil, 204, nil)
	co.Call("POST", "/api/teacher/invitations/"+c.ID+"/accept", nil, 404, nil)                // once
	owner.Call("POST", base+"/teachers", map[string]string{"email": co.User.Email}, 422, nil) // already teaches

	// Their classrooms list shows it as co-taught.
	var list struct{ Classrooms []content.Classroom }
	co.Call("GET", "/api/teacher/classrooms", nil, 200, &list)
	if len(list.Classrooms) != 1 || list.Classrooms[0].Role != "coteacher" || list.Classrooms[0].OwnerName != owner.User.Name {
		t.Fatalf("co-teacher's classrooms = %+v", list.Classrooms)
	}
	var got content.Classroom
	owner.Call("GET", base, nil, 200, &got)
	if got.Role != "owner" {
		t.Fatalf("owner's role = %q", got.Role)
	}

	// They teach: modules, the owner's quiz, a quiz of their own, a session.
	co.Call("GET", base+"/modules", nil, 200, nil)
	co.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "Light"}, 201, nil)
	co.Call("PATCH", "/api/teacher/quizzes/"+q.ID, map[string]any{"title": "Heat check 2"}, 200, nil)
	var mine quiz.Quiz
	co.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Co's quiz"}, 201, &mine)
	var quizzes struct{ Quizzes []quiz.Quiz }
	owner.Call("GET", "/api/teacher/topics/"+topic.ID+"/quizzes", nil, 200, &quizzes)
	if len(quizzes.Quizzes) != 2 {
		t.Fatalf("owner sees %d quizzes", len(quizzes.Quizzes))
	}
	var sess live.Session
	co.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)
	owner.Call("GET", "/api/teacher/sessions/"+sess.ID, nil, 200, nil)
	co.Call("GET", "/api/teacher/sessions/"+sess.ID, nil, 200, nil)
	other.Call("GET", "/api/teacher/sessions/"+sess.ID, nil, 404, nil)
	other.Call("GET", "/api/teacher/quizzes/"+q.ID, nil, 404, nil)
	var teachers struct{ Teachers []content.Teacher }
	co.Call("GET", base+"/teachers", nil, 200, &teachers)
	if len(teachers.Teachers) != 2 || teachers.Teachers[0].Role != "owner" || teachers.Teachers[1].Status != "active" {
		t.Fatalf("teachers = %+v", teachers.Teachers)
	}

	// The classroom itself is the owner's.
	for _, x := range []struct{ method, path string }{
		{"PATCH", base}, {"POST", base + "/archive"}, {"POST", base + "/join-code"},
	} {
		var body any
		if x.method == "PATCH" {
			body = map[string]any{"name": "Mine now"}
		}
		if code, raw := co.Do(x.method, x.path, body); code != 403 || !strings.Contains(string(raw), "owner") {
			t.Fatalf("%s %s by a co-teacher = %d %s", x.method, x.path, code, raw)
		}
	}
	co.Call("POST", base+"/teachers", map[string]string{"email": other.User.Email}, 403, nil)
	co.Call("DELETE", base+"/teachers/"+owner.User.ID, nil, 403, nil)
	co.Call("DELETE", base, nil, 404, nil)

	// Declining an invitation.
	owner.Call("POST", base+"/teachers", map[string]string{"email": other.User.Email}, 201, nil)
	other.Call("POST", "/api/teacher/invitations/"+c.ID+"/decline", nil, 204, nil)
	other.Call("GET", "/api/teacher/invitations", nil, 200, &invs)
	if len(invs.Invitations) != 0 {
		t.Fatalf("after declining = %+v", invs)
	}

	// Leaving, and being removed.
	co.Call("DELETE", base+"/teachers/"+co.User.ID, nil, 204, nil)
	co.Call("GET", base, nil, 404, nil)
	co.Call("GET", "/api/teacher/sessions/"+sess.ID, nil, 404, nil)
	owner.Call("POST", base+"/teachers", map[string]string{"email": co.User.Email}, 201, nil)
	co.Call("POST", "/api/teacher/invitations/"+c.ID+"/accept", nil, 204, nil)
	owner.Call("DELETE", base+"/teachers/"+co.User.ID, nil, 204, nil)
	co.Call("GET", "/api/teacher/quizzes/"+q.ID, nil, 404, nil)
	// What they made stays in the classroom.
	owner.Call("GET", "/api/teacher/quizzes/"+mine.ID, nil, 200, nil)
}
