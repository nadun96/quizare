package tutor_test

import (
	"testing"

	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

const (
	topicHere  = "11111111-1111-1111-1111-111111111111"
	topicThere = "22222222-2222-2222-2222-222222222222"
)

// D-147: a session sits under a topic of its classroom.
func TestSessionUnderATopic(t *testing.T) {
	f := setup(t, "auto")
	f.e.Platform.Topics = map[string]string{topicHere: f.classroom, topicThere: "elsewhere"}

	var s tutor.Session
	f.teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": f.classroom, "topic_id": topicHere, "title": "Cells"}, 201, &s)
	if s.TopicID == nil || *s.TopicID != topicHere {
		t.Fatalf("session topic %v", s.TopicID)
	}
	if f.sess.TopicID != nil {
		t.Fatalf("a session with no topic has %v", *f.sess.TopicID)
	}

	var out struct{ Sessions []tutor.Session }
	f.teacher.Call("GET", "/api/sessions?topic_id="+topicHere, nil, 200, &out)
	if len(out.Sessions) != 1 || out.Sessions[0].ID != s.ID {
		t.Fatalf("listed %+v", out.Sessions)
	}
	out.Sessions = nil
	f.teacher.Call("GET", "/api/sessions?classroom_id="+f.classroom, nil, 200, &out)
	if len(out.Sessions) != 2 {
		t.Fatalf("the classroom lists %d", len(out.Sessions))
	}

	// A topic of another classroom, or none at all, is refused.
	f.teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": f.classroom, "topic_id": topicThere, "title": "X"}, 422, nil)
	f.teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": f.classroom, "topic_id": "33333333-3333-3333-3333-333333333333", "title": "X"}, 422, nil)

	// Every session of a schedule goes under it.
	var sch tutor.Scheduled
	body := map[string]any{"classroom_id": f.classroom, "topic_id": topicHere, "title": "Weekly", "start": "2030-01-07T10:00", "time_zone": "UTC", "duration_min": 60,
		"repeat": map[string]any{"freq": "weekly", "interval": 1, "count": 2}}
	f.teacher.Call("POST", "/api/schedule", body, 201, &sch)
	if len(sch.Sessions) != 2 || sch.Sessions[1].TopicID == nil || *sch.Sessions[1].TopicID != topicHere {
		t.Fatalf("scheduled %+v", sch.Sessions)
	}
	body["topic_id"] = topicThere
	f.teacher.Call("POST", "/api/schedule", body, 422, nil)
}
