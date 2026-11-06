package meet_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/meet"
)

// D-147: a meeting sits under a topic, for its classroom's students.
func TestMeetingUnderATopic(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	made := func(name string) (content.Classroom, content.Topic) {
		var c content.Classroom
		teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": name}, 201, &c)
		var m content.Module
		teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
		var tp content.Topic
		teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &tp)
		return c, tp
	}
	c, tp := made("Science")
	_, otherTopic := made("Maths")

	m := create(t, teacher, map[string]any{"title": "In the topic", "topic_id": tp.ID})
	if m.TopicID == nil || *m.TopicID != tp.ID || m.ClassroomID == nil || *m.ClassroomID != c.ID {
		t.Fatalf("bound to %v, %v", m.TopicID, m.ClassroomID)
	}
	create(t, teacher, map[string]any{"title": "Only the classroom", "classroom_id": c.ID})
	create(t, teacher, map[string]any{"title": "Anyone"})

	count := func(query string) int {
		var out struct{ Meetings []meet.Meeting }
		teacher.Call("GET", "/api/teacher/meetings?"+query, nil, 200, &out)
		return len(out.Meetings)
	}
	if got := count("topic_id=" + tp.ID); got != 1 {
		t.Fatalf("topic lists %d", got)
	}
	if got := count("classroom_id=" + c.ID); got != 2 {
		t.Fatalf("classroom lists %d", got)
	}
	if got := count(""); got != 3 {
		t.Fatalf("all: %d", got)
	}

	// Only the classroom's students may join.
	outsider := e.NewUser(auth.RoleStudent)
	outsider.Call("GET", "/api/meetings/"+m.Code, nil, 403, nil)

	teacher.Call("POST", "/api/teacher/meetings", map[string]any{"title": "X", "topic_id": otherTopic.ID, "classroom_id": c.ID}, 422, nil)
	stranger := e.NewUser(auth.RoleTeacher)
	stranger.Call("POST", "/api/teacher/meetings", map[string]any{"title": "X", "topic_id": tp.ID}, 422, nil)
}
