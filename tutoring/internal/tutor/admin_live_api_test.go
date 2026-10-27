package tutor_test

import (
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// The admin sees the tutoring sessions open or live, and ends one with a
// reason its teacher is emailed (D-90).
func TestAdminEndsATutoringSession(t *testing.T) {
	e := testenv.New(t)
	teacher := e.NewPerson("teacher", "Ms Perera")
	admin := e.NewPerson("admin", "The Admin")
	classroom := e.NewClassroom(teacher)
	var s tutor.Session
	teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": classroom, "title": "Algebra"}, 201, &s)
	teacher.Call("POST", "/api/sessions/"+s.ID+"/start", nil, 204, nil)

	var list struct{ Sessions []tutor.LiveSession }
	teacher.Call("GET", "/api/admin/live", nil, 403, nil)
	admin.Call("GET", "/api/admin/live", nil, 200, &list)
	if len(list.Sessions) != 1 || list.Sessions[0].ID != s.ID || list.Sessions[0].Status != "live" || list.Sessions[0].TeacherName != "Ms Perera" {
		t.Fatalf("live = %+v", list.Sessions)
	}

	teacher.Call("POST", "/api/admin/sessions/"+s.ID+"/end", map[string]string{"reason": "x"}, 403, nil)
	admin.Call("POST", "/api/admin/sessions/"+s.ID+"/end", map[string]string{"reason": " "}, 422, nil)
	e.Platform.TakeNotices()
	admin.Call("POST", "/api/admin/sessions/"+s.ID+"/end", map[string]string{"reason": "Server overloaded"}, 204, nil)
	admin.Call("POST", "/api/admin/sessions/"+s.ID+"/end", map[string]string{"reason": "again"}, 404, nil)

	var mine struct{ Sessions []tutor.Session }
	teacher.Call("GET", "/api/sessions?classroom_id="+classroom, nil, 200, &mine)
	if len(mine.Sessions) != 1 || mine.Sessions[0].Status != "ended" {
		t.Fatalf("sessions = %+v", mine.Sessions)
	}
	n := e.Platform.TakeNotices()
	if len(n) != 1 || n[0].ClassroomID != classroom || n[0].Students || !strings.Contains(n[0].Body, "Server overloaded") {
		t.Fatalf("notices = %+v", n)
	}
	admin.Call("GET", "/api/admin/live", nil, 200, &list)
	if len(list.Sessions) != 0 {
		t.Fatalf("still live: %+v", list.Sessions)
	}
}
