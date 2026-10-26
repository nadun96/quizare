package tutor_test

import (
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// While the admin has paused new activities for a teacher, they neither
// make nor start sessions; what's scheduled stays (D-90).
func TestPausedTeachersStartNoSessions(t *testing.T) {
	e := testenv.New(t)
	teacher := e.NewPerson("teacher", "Ms Perera")
	classroom := e.NewClassroom(teacher)
	var s tutor.Session
	teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": classroom, "title": "Before"}, 201, &s)

	e.Platform.Paused[teacher.ID] = "the admin has paused new activities for now: exams"
	var bad struct{ Code, Message string }
	teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": classroom, "title": "During"}, 409, &bad)
	if bad.Code != "activities_paused" || !strings.Contains(bad.Message, "exams") {
		t.Fatalf("create = %+v", bad)
	}
	teacher.Call("POST", "/api/sessions/"+s.ID+"/start", nil, 409, &bad)
	if bad.Code != "activities_paused" {
		t.Fatalf("start = %+v", bad)
	}

	delete(e.Platform.Paused, teacher.ID)
	teacher.Call("POST", "/api/sessions/"+s.ID+"/start", nil, 204, nil)
}
