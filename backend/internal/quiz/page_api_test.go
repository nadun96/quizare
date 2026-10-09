package quiz_test

import (
	"fmt"
	"testing"
)

// A topic's quizzes are paginated, searched, filtered by status and sorted (PL-FR-02).
func TestTopicQuizzesArePaginated(t *testing.T) {
	_, teacher, _, topic := setup(t) // setup makes "Quiz 1"
	for i := range 6 {
		teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": fmt.Sprintf("Extra %c", 'F'-i)}, 201, nil)
	}
	path := "/api/teacher/topics/" + topic.ID + "/quizzes"
	all := teacher.CheckPages(path, "quizzes", "id", 7, 3)
	if all[0]["title"] != "Quiz 1" {
		t.Fatalf("oldest first by default: %v", all[0]["title"])
	}
	if byTitle := teacher.CheckPages(path+"?sort=title", "quizzes", "id", 7, 3); byTitle[0]["title"] != "Extra A" {
		t.Fatalf("by title: %v", byTitle[0]["title"])
	}
	if p := teacher.Page(path+"?q=extra", "quizzes", 1, 25); p.Total != 6 {
		t.Fatalf("search: %d", p.Total)
	}
	if p := teacher.Page(path+"?status=ready", "quizzes", 1, 25); p.Total != 0 {
		t.Fatalf("status filter: %d", p.Total)
	}
}
