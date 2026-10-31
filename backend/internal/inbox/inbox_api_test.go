package inbox_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/inbox"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func emails(t *testing.T, e *apptest.Env) []mail.Message {
	t.Helper()
	var out []mail.Message
	for _, raw := range e.TakeJobs("email") {
		var m mail.Message
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func summary(c *apptest.Client) inbox.Summary {
	var s inbox.Summary
	c.Call("GET", "/api/my/inbox", nil, 200, &s)
	return s
}

type announcements struct {
	Announcements []inbox.Announcement
	Total         int
}

// A teacher announces to a classroom: its students read it, the teacher
// sees how many have, and it's emailed when asked to anyone who didn't
// turn email off (D-103).
func TestAnnouncements(t *testing.T) {
	e := apptest.New(t)
	teacher, other := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleTeacher)
	s1, s2, outsider := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 8"}, 201, &c)
	s1.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)
	s2.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)
	s2.Call("PUT", "/api/my/inbox/prefs", map[string]bool{"email": false}, 204, nil)
	emails(t, e)

	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/announcements", map[string]any{"title": "", "body": "x"}, 422, nil)
	other.Call("POST", "/api/teacher/classrooms/"+c.ID+"/announcements", map[string]any{"title": "Mine?", "body": "x"}, 404, nil)
	var a inbox.Announcement
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/announcements", map[string]any{"title": "Test on Friday", "body": "Chapters 1 to 3.", "email": true}, 201, &a)
	got := emails(t, e)
	if len(got) != 1 || got[0].To != s1.User.Email || got[0].Subject != "Grade 8: Test on Friday" || !strings.Contains(got[0].Body, "Chapters 1 to 3.") {
		t.Fatalf("emails = %+v", got)
	}
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/announcements", map[string]any{"title": "Bring a calculator", "body": "Please."}, 201, nil)
	if len(emails(t, e)) != 0 {
		t.Fatal("emailed without being asked")
	}

	// Students see their classrooms' announcements, unread until read.
	if s := summary(s1); s.Announcements != 2 || !s.Email {
		t.Fatalf("s1 summary = %+v", s)
	}
	if s := summary(outsider); s.Announcements != 0 {
		t.Fatalf("outsider summary = %+v", s)
	}
	var l announcements
	s1.Call("GET", "/api/my/announcements", nil, 200, &l)
	if l.Total != 2 || l.Announcements[0].Title != "Bring a calculator" || l.Announcements[0].Read || l.Announcements[1].Classroom != "Grade 8" {
		t.Fatalf("s1 list = %+v", l)
	}
	s1.Call("POST", "/api/my/announcements/read", map[string]any{"ids": []string{a.ID}}, 204, nil)
	if s := summary(s1); s.Announcements != 1 {
		t.Fatalf("after one read = %+v", s)
	}
	s1.Call("POST", "/api/my/announcements/read", map[string]any{}, 204, nil)
	if s := summary(s1); s.Announcements != 0 {
		t.Fatalf("after all read = %+v", s)
	}

	// The teacher sees who read it.
	teacher.Call("GET", "/api/my/announcements", nil, 200, &l)
	if l.Total != 2 || l.Announcements[1].Reads != 1 || l.Announcements[1].Students != 2 {
		t.Fatalf("teacher list = %+v", l)
	}
	var readers struct{ Readers []inbox.Reader }
	teacher.Call("GET", "/api/teacher/announcements/"+a.ID+"/readers", nil, 200, &readers)
	if len(readers.Readers) != 2 || readers.Readers[0].ID != s2.User.ID || readers.Readers[0].ReadAt != nil || readers.Readers[1].ReadAt == nil {
		t.Fatalf("readers = %+v", readers)
	}

	// Editing and deleting are the teacher's.
	other.Call("PATCH", "/api/teacher/announcements/"+a.ID, map[string]any{"title": "x"}, 404, nil)
	teacher.Call("PATCH", "/api/teacher/announcements/"+a.ID, map[string]any{"title": "Test moved to Monday"}, 200, &a)
	if a.Title != "Test moved to Monday" || a.Body != "Chapters 1 to 3." {
		t.Fatalf("edited = %+v", a)
	}
	teacher.Call("DELETE", "/api/teacher/announcements/"+a.ID, nil, 204, nil)
	s1.Call("GET", "/api/my/announcements", nil, 200, &l)
	if l.Total != 1 {
		t.Fatalf("after delete = %+v", l)
	}
	s1.Call("PATCH", "/api/teacher/announcements/"+l.Announcements[0].ID, map[string]any{"title": "x"}, 403, nil)
}

type conversations struct {
	Conversations []inbox.Conversation
	Total         int
}

// Messages between a teacher and a student of theirs, either starting; one
// email until it's read, not one a message (D-103).
func TestMessages(t *testing.T) {
	e := apptest.New(t)
	teacher, other := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleTeacher)
	s1, outsider := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 8"}, 201, &c)
	s1.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)
	emails(t, e)

	// Who each may write to.
	var contacts struct {
		Contacts []inbox.Person
		Total    int
	}
	s1.Call("GET", "/api/my/contacts", nil, 200, &contacts)
	if contacts.Total != 1 || contacts.Contacts[0].ID != teacher.User.ID || contacts.Contacts[0].Classrooms != "Grade 8" {
		t.Fatalf("student contacts = %+v", contacts)
	}
	teacher.Call("GET", "/api/my/contacts?classroom="+c.ID, nil, 200, &contacts)
	if contacts.Total != 1 || contacts.Contacts[0].ID != s1.User.ID {
		t.Fatalf("teacher contacts = %+v", contacts)
	}
	teacher.Call("POST", "/api/my/conversations", map[string]string{"user_id": outsider.User.ID, "body": "Hi"}, 403, nil)
	outsider.Call("POST", "/api/my/conversations", map[string]string{"user_id": teacher.User.ID, "body": "Hi"}, 403, nil)

	// The teacher writes twice: one email.
	var started struct {
		Conversation inbox.Conversation
		Message      inbox.Message
	}
	teacher.Call("POST", "/api/my/conversations", map[string]string{"user_id": s1.User.ID, "body": "Well done on the quiz."}, 201, &started)
	cid := started.Conversation.ID
	teacher.Call("POST", "/api/my/conversations/"+cid+"/messages", map[string]string{"body": "Keep it up!"}, 201, nil)
	got := emails(t, e)
	if len(got) != 1 || got[0].To != s1.User.Email || !strings.Contains(got[0].Body, "Well done on the quiz.") || !strings.Contains(got[0].Body, "/inbox?c="+cid) {
		t.Fatalf("emails = %+v", got)
	}
	if s := summary(s1); s.Messages != 2 {
		t.Fatalf("s1 unread = %+v", s)
	}
	var l conversations
	s1.Call("GET", "/api/my/conversations", nil, 200, &l)
	if l.Total != 1 || l.Conversations[0].With.ID != teacher.User.ID || l.Conversations[0].Unread != 2 || l.Conversations[0].Last != "Keep it up!" || !l.Conversations[0].CanReply {
		t.Fatalf("s1 conversations = %+v", l)
	}
	var msgs struct {
		Messages []inbox.Message
		Total    int
	}
	s1.Call("GET", "/api/my/conversations/"+cid+"/messages", nil, 200, &msgs)
	if msgs.Total != 2 || msgs.Messages[0].Body != "Keep it up!" || msgs.Messages[0].Mine {
		t.Fatalf("messages = %+v", msgs)
	}
	s1.Call("POST", "/api/my/conversations/"+cid+"/read", nil, 204, nil)
	if s := summary(s1); s.Messages != 0 {
		t.Fatalf("after read = %+v", s)
	}

	// The student replies, starting from the teacher: the same conversation.
	s1.Call("POST", "/api/my/conversations", map[string]string{"user_id": teacher.User.ID, "body": "Thank you!"}, 201, &started)
	if started.Conversation.ID != cid {
		t.Fatalf("a second conversation: %+v", started)
	}
	if got := emails(t, e); len(got) != 1 || got[0].To != teacher.User.Email {
		t.Fatalf("teacher emails = %+v", got)
	}
	if s := summary(teacher); s.Messages != 1 {
		t.Fatalf("teacher unread = %+v", s)
	}
	other.Call("GET", "/api/my/conversations/"+cid+"/messages", nil, 404, nil)
	outsider.Call("POST", "/api/my/conversations/"+cid+"/messages", map[string]string{"body": "x"}, 404, nil)

	// Once the student leaves, the conversation stays but can't be added to.
	if _, err := e.Pool.Exec(t.Context(), `UPDATE content.enrolments SET status = 'removed' WHERE classroom_id = $1`, c.ID); err != nil {
		t.Fatal(err)
	}
	teacher.Call("GET", "/api/my/conversations", nil, 200, &l)
	if l.Total != 1 || l.Conversations[0].CanReply {
		t.Fatalf("after leaving = %+v", l)
	}
	teacher.Call("POST", "/api/my/conversations/"+cid+"/messages", map[string]string{"body": "Still there?"}, 403, nil)
}
