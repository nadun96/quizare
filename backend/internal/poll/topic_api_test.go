package poll_test

import (
	"fmt"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/poll"
)

func topicIn(t *testing.T, teacher *apptest.Client, name string) (content.Classroom, content.Topic) {
	t.Helper()
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": name}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var tp content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &tp)
	return c, tp
}

// D-147: a poll sits under a topic like a quiz does.
func TestPollUnderATopic(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	c, tp := topicIn(t, teacher, "Science")
	other, otherTopic := topicIn(t, teacher, "Maths")

	// The topic brings its classroom.
	var p poll.Poll
	teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "In the topic", "topic_id": tp.ID}, 201, &p)
	if p.TopicID == nil || *p.TopicID != tp.ID || p.ClassroomID == nil || *p.ClassroomID != c.ID {
		t.Fatalf("bound to %v, %v", p.TopicID, p.ClassroomID)
	}
	var loose, inClass poll.Poll
	teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "Loose"}, 201, &loose)
	teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "Only the classroom", "classroom_id": c.ID}, 201, &inClass)

	titles := func(query string) string {
		var out struct{ Polls []poll.Poll }
		teacher.Call("GET", "/api/teacher/polls?sort=title&dir=asc&"+query, nil, 200, &out)
		s := ""
		for _, x := range out.Polls {
			s += x.Title + ";"
		}
		return s
	}
	if got := titles("topic_id=" + tp.ID); got != "In the topic;" {
		t.Fatalf("topic lists %q", got)
	}
	if got := titles("classroom_id=" + c.ID); got != "In the topic;Only the classroom;" {
		t.Fatalf("classroom lists %q", got)
	}

	// An existing poll can be bound, moved and freed.
	var got poll.Poll
	teacher.Call("PUT", "/api/teacher/polls/"+loose.ID, map[string]any{"topic_id": tp.ID}, 200, &got)
	if got.TopicID == nil || *got.ClassroomID != c.ID {
		t.Fatalf("bound: %+v", got)
	}
	teacher.Call("PUT", "/api/teacher/polls/"+loose.ID, map[string]any{"classroom_id": other.ID}, 200, &got)
	if got.TopicID != nil || *got.ClassroomID != other.ID {
		t.Fatalf("moved classroom keeps the old topic: %+v", got)
	}
	teacher.Call("PUT", "/api/teacher/polls/"+loose.ID, map[string]any{"topic_id": otherTopic.ID}, 200, &got)
	teacher.Call("PUT", "/api/teacher/polls/"+loose.ID, map[string]any{"topic_id": ""}, 200, &got)
	if got.TopicID != nil || *got.ClassroomID != other.ID {
		t.Fatalf("out of the topic, still in the classroom: %+v", got)
	}

	// A topic of another classroom, or someone else's, is refused.
	teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "X", "topic_id": otherTopic.ID, "classroom_id": c.ID}, 422, nil)
	stranger := e.NewUser(auth.RoleTeacher)
	stranger.Call("POST", "/api/teacher/polls", map[string]any{"title": "X", "topic_id": tp.ID}, 422, nil)
	stranger.Call("PUT", "/api/teacher/polls/"+p.ID, map[string]any{"topic_id": ""}, 404, nil)
	teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "X", "topic_id": "00000000-0000-0000-0000-000000000000"}, 422, nil)

	// Losing the topic leaves the poll in its classroom.
	if _, err := e.Pool.Exec(t.Context(), `DELETE FROM content.topics WHERE id = $1`, tp.ID); err != nil {
		t.Fatal(fmt.Errorf("deleting the topic: %w", err))
	}
	teacher.Call("GET", "/api/teacher/polls/"+p.ID, nil, 200, &got)
	if got.TopicID != nil || *got.ClassroomID != c.ID {
		t.Fatalf("after the topic went: %+v", got)
	}
}
