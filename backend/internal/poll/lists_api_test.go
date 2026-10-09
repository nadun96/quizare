package poll_test

import (
	"fmt"
	"testing"
	"time"
)

// Polls, a poll's participants and every answer to a question are paginated
// on the server; the answers list reaches past live results' newest 300 (PL-FR-02).
func TestPollListsArePaginated(t *testing.T) {
	f := setup(t, settings("anonymous", "self", "live"), essayQ)
	for i := range 4 {
		f.teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": fmt.Sprintf("Extra poll %d", i)}, 201, nil)
	}
	polls := f.teacher.CheckPages("/api/teacher/polls", "polls", "id", 5, 2)
	if polls[0]["title"] != "Extra poll 3" {
		t.Fatalf("newest poll first: %v", polls[0]["title"])
	}
	if p := f.teacher.Page("/api/teacher/polls?status=open", "polls", 1, 25); p.Total != 1 {
		t.Fatalf("status filter: %d", p.Total)
	}

	names := []string{"Mia", "Leo", "Ava", "Zoe", "Ben", "Kai"}
	for i, n := range names {
		p := f.join(t, n)
		if i%2 == 0 {
			p.answer(f.qs[0], map[string]any{"text": "Answer from " + n})
			time.Sleep(5 * time.Millisecond) // distinct answer times
		}
	}
	base := "/api/teacher/polls/" + f.poll.ID
	people := f.teacher.CheckPages(base+"/participants", "participants", "participant_id", 6, 4)
	if people[0]["name"] != "Mia" || people[0]["number"].(float64) != 1 {
		t.Fatalf("join order: %v", people[0])
	}
	if byName := f.teacher.Page(base+"/participants?sort=name", "participants", 1, 2); byName.Rows[0]["name"] != "Ava" {
		t.Fatalf("by name: %v", byName.Rows[0]["name"])
	}
	if busy := f.teacher.Page(base+"/participants?sort=answered&dir=desc", "participants", 1, 3); busy.Rows[2]["answered"].(float64) != 1 {
		t.Fatalf("most answers first: %v", busy.Rows)
	}

	answers := f.teacher.CheckPages(base+"/questions/"+f.qs[0].ID+"/answers", "answers", "participant_id", 3, 2)
	if answers[0]["text"] != "Answer from Ben" || answers[0]["name"] != "Ben" {
		t.Fatalf("newest answer first: %v", answers[0])
	}
	f.teacher.Call("POST", base+"/moderation", map[string]any{"question_id": f.qs[0].ID, "participant_id": answers[1]["participant_id"], "hidden": true}, 204, nil)
	if p := f.teacher.Page(base+"/questions/"+f.qs[0].ID+"/answers?hidden=true", "answers", 1, 25); p.Total != 1 || p.Rows[0]["text"] != "Answer from Ava" {
		t.Fatalf("hidden answers: %+v", p.Rows)
	}
	if p := f.teacher.Page(base+"/questions/"+f.qs[0].ID+"/answers?q=mia", "answers", 1, 25); p.Total != 1 {
		t.Fatalf("search answers: %d", p.Total)
	}
	// Another teacher's poll is not found.
	other := f.e.NewUser("teacher")
	other.Call("GET", base+"/participants", nil, 404, nil)
	other.Call("GET", base+"/questions/"+f.qs[0].ID+"/answers", nil, 404, nil)
}
