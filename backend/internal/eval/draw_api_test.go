package eval_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
)

// D-65: with question_draw each student gets their own random subset, and
// every total counts only the questions that student got.
func TestRandomSubsetPerStudent(t *testing.T) {
	f := setup(t, map[string]any{"question_draw": 2, "team_mode": "manual"})
	var team live.TeamInfo
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams", map[string]any{"name": "Blue"}, 201, &team)

	marks := map[string]float64{"Q1": 2, "Q2": 1, "Q3": 5}
	seen := map[string]bool{}
	var ids []string
	for i := 0; i < 12; i++ {
		_, id := f.take(t, nil, nil) // two questions each, both skipped
		ids = append(ids, id)
	}
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams/members", map[string]any{"attempt_ids": ids[:2], "team_id": team.ID}, 204, nil)
	f.evaluate(t)

	res := f.results(t)
	var teamMax float64
	for i, id := range ids {
		r := res[id]
		if len(r.Marks) != 2 {
			t.Fatalf("attempt %d has %d marks, want the 2 questions it drew", i, len(r.Marks))
		}
		var want float64
		codes := ""
		for _, m := range r.Marks {
			want += marks[m.Code]
			codes += m.Code
		}
		if r.MaxScore != want {
			t.Fatalf("attempt %d (%s): max %v, want %v", i, codes, r.MaxScore, want)
		}
		seen[codes] = true
		if i < 2 {
			teamMax += want
		}
	}
	if len(seen) < 2 {
		t.Fatalf("12 students all drew the same questions: %v", seen)
	}

	// The team's maximum is its members' own maximums added up ("every member's marks, totalled").
	var standings struct{ Teams []eval.TeamStanding }
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/teams/standings", nil, 200, &standings)
	if len(standings.Teams) != 1 || standings.Teams[0].MaxScore != teamMax {
		t.Fatalf("standings %+v, want max %v", standings.Teams, teamMax)
	}

	// Analytics count, per question, the students who got it.
	var stats struct {
		Questions []struct {
			Code     string
			Shown    int
			Answered int
		}
	}
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/analytics", nil, 200, &stats)
	total := 0
	for _, q := range stats.Questions {
		total += q.Shown
		if q.Shown == 12 {
			t.Fatalf("%s shown to everyone although each student drew 2 of 3", q.Code)
		}
	}
	if total != 24 {
		t.Fatalf("questions shown %d times, want 12 students × 2", total)
	}
}
