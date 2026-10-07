package poll_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/poll"
)

// addLive adds a question with placement options; it returns the question
// and its index in the poll.
func (f *fixture) addLive(t *testing.T, q map[string]any, opts map[string]any) (poll.Question, string) {
	t.Helper()
	body := map[string]any{}
	for k, v := range q {
		body[k] = v
	}
	for k, v := range opts {
		body[k] = v
	}
	var out poll.Question
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/questions", body, 201, &out)
	var p poll.Poll
	f.teacher.Call("GET", "/api/teacher/polls/"+f.poll.ID, nil, 200, &p)
	for i, x := range p.Questions {
		if x.ID == out.ID {
			return out, fmt.Sprint(i)
		}
	}
	t.Fatal("added question not in the poll")
	return out, ""
}

func TestAddQuestionWhilePresenting(t *testing.T) {
	f := setup(t, settings("anonymous", "presenter", "presenter"), singleQ, cloudQ, essayQ)
	a := f.anon(t)
	audience := a.Dial("/ws/polls/" + f.poll.JoinCode)
	apptest.ReadUntil(t, audience, "update", 3*time.Second)
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/present", map[string]any{"index": 1}, 200, nil)

	quick := map[string]any{"type": "RATING", "text": "How sure are you?", "body": map[string]any{"points": 5}}
	q, index := f.addLive(t, quick, map[string]any{"after_current": true, "present": true})
	if index != "2" {
		t.Fatalf("inserted after the current question: index %q", index)
	}
	var p poll.Poll
	f.teacher.Call("GET", "/api/teacher/polls/"+f.poll.ID, nil, 200, &p)
	order := []string{p.Questions[0].ID, p.Questions[1].ID, p.Questions[2].ID, p.Questions[3].ID}
	if order[0] != f.qs[0].ID || order[1] != f.qs[1].ID || order[2] != q.ID || order[3] != f.qs[2].ID || p.CurrentIndex != 2 {
		t.Fatalf("order %v, current %d", order, p.CurrentIndex)
	}
	// Participants are moved to it without reloading.
	f.e.App.Poll.Flush(context.Background())
	for {
		up := apptest.ReadUntil(t, audience, "update", 3*time.Second)
		raw, _ := json.Marshal(up)
		var pub poll.PublicPoll
		_ = json.Unmarshal(raw, &pub)
		if len(pub.Questions) == 1 && pub.Questions[0].ID == q.ID {
			if pub.Total != 4 || pub.Index != 2 {
				t.Fatalf("update: %s", raw)
			}
			break
		}
	}
	f.answer(a, q, map[string]any{"number": 4}, 200)

	// Without "present", the room stays where it is.
	q2, index := f.addLive(t, quick, map[string]any{"after_current": true})
	if index != "3" {
		t.Fatalf("second insert index %q", index)
	}
	if v := f.view(a); v.Questions[0].ID != q.ID || v.Total != 5 {
		t.Fatalf("not presented yet: %+v", v.Questions)
	}
	_ = q2
}

func TestAddQuestionSelfPaced(t *testing.T) {
	f := setup(t, settings("anonymous", "self", "live"), singleQ)
	a := f.anon(t)
	audience := a.Dial("/ws/polls/" + f.poll.JoinCode)
	apptest.ReadUntil(t, audience, "update", 3*time.Second)
	q, index := f.addLive(t, cloudQ, nil)
	if index != "1" {
		t.Fatalf("appended: %q", index)
	}
	f.e.App.Poll.Flush(context.Background())
	for {
		up := apptest.ReadUntil(t, audience, "update", 3*time.Second)
		raw, _ := json.Marshal(up)
		var pub poll.PublicPoll
		_ = json.Unmarshal(raw, &pub)
		if len(pub.Questions) == 2 {
			if pub.Questions[1].ID != q.ID {
				t.Fatalf("new question last: %s", raw)
			}
			break
		}
	}
	// "present" means nothing for self-paced polls; after_id places it.
	_, index = f.addLive(t, essayQ, map[string]any{"present": true, "after_id": f.qs[0].ID})
	if index != "1" {
		t.Fatalf("after_id: %q", index)
	}
	var p poll.Poll
	f.teacher.Call("GET", "/api/teacher/polls/"+f.poll.ID, nil, 200, &p)
	if p.CurrentIndex != 0 || len(p.Questions) != 3 {
		t.Fatalf("self-paced: %+v", p)
	}
}
