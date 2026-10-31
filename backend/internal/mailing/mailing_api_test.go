package mailing_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/mailing"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func emails(t *testing.T, e *apptest.Env) map[string]mail.Message {
	t.Helper()
	out := map[string]mail.Message{}
	for _, raw := range e.TakeJobs("email") {
		var m mail.Message
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		out[m.To] = m
	}
	return out
}

func TestRender(t *testing.T) {
	r := mailing.Recipient{Name: "Ms Ann Perera", Email: "ann@example.com", Role: "teacher", Classroom: "Grade 8"}
	got := mailing.Render("Hi {{first_name}} ({{ name }}, {{EMAIL}}), {{classroom}} at {{site_url}}", r, "https://x.test")
	if got != "Hi Ann (Ms Ann Perera, ann@example.com), Grade 8 at https://x.test" {
		t.Fatalf("got %q", got)
	}
}

// Templates with placeholders, sent to a role, a classroom or a list of
// addresses, each filled in for its recipient and recorded (D-102).
func TestTemplatedEmail(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	teacher := e.NewUser(auth.RoleTeacher)
	s1, s2 := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 8"}, 201, &c)
	s1.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)
	emails(t, e) // registration emails

	// A typo in a placeholder is refused, by name.
	code, body := admin.Do("POST", "/api/admin/email/templates", map[string]string{"name": "Welcome", "subject": "Hi {{frist_name}}", "body": "x"})
	if code != 422 || !strings.Contains(string(body), "frist_name") {
		t.Fatalf("typo = %d %s", code, body)
	}
	var tpl mailing.Template
	admin.Call("POST", "/api/admin/email/templates", map[string]string{"name": "Welcome", "subject": "Welcome, {{first_name}}", "body": "Hello {{name}} of {{classroom}}.\nSee {{site_url}}"}, 201, &tpl)
	admin.Call("POST", "/api/admin/email/templates", map[string]string{"name": "welcome", "subject": "x", "body": "y"}, 422, nil) // name taken
	tpl.Body += "\nBye"
	admin.Call("PUT", "/api/admin/email/templates/"+tpl.ID, map[string]string{"name": tpl.Name, "subject": tpl.Subject, "body": tpl.Body}, 200, nil)

	// A classroom's students: preview, a test to oneself, then the send.
	aud := map[string]any{"kind": "classroom", "classroom_id": c.ID}
	var p mailing.Preview
	admin.Call("POST", "/api/admin/email/preview", map[string]any{"subject": tpl.Subject, "body": tpl.Body, "audience": aud}, 200, &p)
	if p.Recipients != 1 || p.Sample == nil || p.Sample.To != s1.User.Email || !strings.Contains(p.Sample.Body, "of Grade 8.") {
		t.Fatalf("preview = %+v", p)
	}
	admin.Call("POST", "/api/admin/email/test", map[string]any{"subject": tpl.Subject, "body": tpl.Body, "audience": aud}, 204, nil)
	if m := emails(t, e)[admin.User.Email]; !strings.HasPrefix(m.Subject, "[Test] Welcome, Admin") {
		t.Fatalf("test = %+v", m)
	}
	var sent mailing.Send
	admin.Call("POST", "/api/admin/email/sends", map[string]any{"subject": tpl.Subject, "body": tpl.Body, "audience": aud, "template_id": tpl.ID}, 201, &sent)
	got := emails(t, e)
	if len(got) != 1 || got[s1.User.Email].Subject != "Welcome, "+strings.Fields(s1.User.Name)[0] || sent.Recipients != 1 || sent.AudienceLabel != "Students of Grade 8" {
		t.Fatalf("sent %+v: %+v", sent, got)
	}

	// All students; a list with an unknown address.
	admin.Call("POST", "/api/admin/email/sends", map[string]any{"subject": "News", "body": "Hi {{name}}", "audience": map[string]any{"kind": "students"}}, 201, nil)
	if got := emails(t, e); len(got) != 2 || got[s2.User.Email].Body == "" || got[teacher.User.Email].To != "" {
		t.Fatalf("students = %+v", got)
	}
	admin.Call("POST", "/api/admin/email/preview", map[string]any{"subject": "x", "body": "y", "audience": map[string]any{"kind": "emails", "emails": []string{teacher.User.Email + ", nobody@example.com"}}}, 200, &p)
	if p.Recipients != 1 || len(p.Unknown) != 1 || p.Unknown[0] != "nobody@example.com" {
		t.Fatalf("list = %+v", p)
	}
	admin.Call("POST", "/api/admin/email/sends", map[string]any{"subject": "x", "body": "y", "audience": map[string]any{"kind": "emails", "emails": []string{"nobody@example.com"}}}, 422, nil)

	// The history, newest first.
	var h struct {
		Sends []mailing.Send
		Total int
	}
	admin.Call("GET", "/api/admin/email/sends", nil, 200, &h)
	if h.Total != 2 || h.Sends[0].Subject != "News" || h.Sends[1].TemplateID == nil || h.Sends[1].SentBy != "Admin" {
		t.Fatalf("history = %+v", h)
	}
	admin.Call("DELETE", "/api/admin/email/templates/"+tpl.ID, nil, 204, nil)
	admin.Call("GET", "/api/admin/email/sends", nil, 200, &h)
	if h.Total != 2 {
		t.Fatalf("history after deleting the template = %+v", h)
	}

	// Managers need "Email"; nobody else gets in.
	mgr := e.NewUser(auth.RoleTeacher)
	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": mgr.User.ID, "features": []string{"usage"}}, 201, nil)
	mgr.Call("GET", "/api/admin/email/templates", nil, 404, nil)
	admin.Call("PUT", "/api/admin/managers/"+mgr.User.ID+"/features", map[string]any{"features": []string{"send_email"}}, 200, nil)
	mgr.Call("GET", "/api/admin/email/templates", nil, 200, nil)
	teacher.Call("GET", "/api/admin/email/templates", nil, 403, nil)
}
