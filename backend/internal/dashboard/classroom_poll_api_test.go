package dashboard_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/dashboard"
	"github.com/nadun96/quizplatform/internal/poll"
)

// D-147: a student's classroom page shows a poll that's open for its students.
func TestStudentSeesAClassroomsOpenPoll(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Science"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var tp content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &tp)
	student := e.NewUser(auth.RoleStudent)
	student.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)

	make := func(title string, body map[string]any) poll.Poll {
		body["title"] = title
		var p poll.Poll
		teacher.Call("POST", "/api/teacher/polls", body, 201, &p)
		return p
	}
	underTopic := make("Under the topic", map[string]any{"topic_id": tp.ID})
	organising := make("Only filed here", map[string]any{"classroom_id": c.ID})
	draft := make("Still a draft", map[string]any{"topic_id": tp.ID})
	for _, id := range []string{underTopic.ID, organising.ID} {
		if _, err := e.Pool.Exec(t.Context(), `UPDATE poll.polls SET status = 'open' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	_ = draft

	var w dashboard.ClassroomWork
	student.Call("GET", "/api/my/classrooms/"+c.ID+"/work", nil, 200, &w)
	if len(w.Live) != 1 || w.Live[0].Kind != "poll" || w.Live[0].Title != "Under the topic" || w.Live[0].Href != "/p/"+underTopic.JoinCode {
		t.Fatalf("live = %+v", w.Live)
	}
}
