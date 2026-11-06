package tutorlink_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
)

// D-147: the tutoring service asks which classroom a topic is in.
func TestInternalTopic(t *testing.T) {
	e := apptest.New(t, apptest.WithTutoring("http://localhost:8090", secret))
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Science"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var tp content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &tp)

	cl := e.Client()
	ask := func(token, topic string) (int, string) {
		cl.Headers = map[string]string{"Authorization": "Bearer " + token}
		body, _ := json.Marshal(map[string]string{"topic_id": topic})
		code, out := cl.Raw("POST", "/internal/tutoring/topic", "application/json", body)
		cl.Headers = nil
		return code, string(out)
	}
	good := serviceToken(t, secret, time.Minute, "tutoring", "quiz-platform")
	code, out := ask(good, tp.ID)
	var got struct {
		ClassroomID string `json:"classroom_id"`
	}
	json.Unmarshal([]byte(out), &got)
	if code != 200 || got.ClassroomID != c.ID {
		t.Fatalf("%d %s", code, out)
	}
	if code, out := ask(good, "00000000-0000-0000-0000-000000000000"); code != 404 {
		t.Fatalf("no such topic: %d %s", code, out)
	}
	if code, _ := ask(serviceToken(t, []byte("another secret of the right length!"), time.Minute, "tutoring", "quiz-platform"), tp.ID); code != 404 {
		t.Fatalf("a bad token got %d", code)
	}
}
