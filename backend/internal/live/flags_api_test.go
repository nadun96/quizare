package live_test

import (
	"bytes"
	"testing"

	"github.com/nadun96/quizplatform/internal/live"
)

// D-61: with free navigation a student flags questions to come back to.
func TestFlagQuestionsToComeBackTo(t *testing.T) {
	f := setup(t, nil, map[string]any{"one_way_navigation": false}, nil)
	s, st := running(t, f)
	if len(st.Flagged) != 3 || st.Flagged[0] {
		t.Fatalf("flagged = %v", st.Flagged)
	}
	flag := func(qid string, on bool, want int) live.StudentState {
		t.Helper()
		var out live.StudentState
		code, body := s.Do("PUT", "/api/attempts/"+st.AttemptID+"/flags/"+qid, map[string]bool{"flagged": on})
		if code != want {
			t.Fatalf("flag %s %v: %d %s", qid, on, code, body)
		}
		if code == 200 {
			out = state(t, s, st.AttemptID)
		}
		return out
	}
	first := st.Question.ID
	flag(first, true, 200)
	flag(first, true, 200) // twice is still one flag
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/goto", map[string]int{"index": 2}, 200, &st)
	third := st.Question.ID
	got := flag(third, true, 200)
	if want := []bool{true, false, true}; !equal(got.Flagged, want) {
		t.Fatalf("flagged = %v, want %v", got.Flagged, want)
	}
	// Going back from the list keeps the flags; clearing one leaves the other.
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/goto", map[string]int{"index": 0}, 200, &st)
	if got := flag(first, false, 200); !equal(got.Flagged, []bool{false, false, true}) {
		t.Fatalf("flagged after clearing = %v", got.Flagged)
	}

	// Not a question of this attempt; someone else's attempt.
	flag("00000000-0000-0000-0000-000000000000", true, 400)
	other, _ := f.join(t)
	if code, _ := other.Do("PUT", "/api/attempts/"+st.AttemptID+"/flags/"+first, map[string]bool{"flagged": true}); code != 404 {
		t.Fatalf("another student flagged: %d", code)
	}
	// Flags are the student's own: the teacher's dashboard doesn't carry them.
	code, dash := f.teacher.Do("GET", "/api/teacher/sessions/"+f.sess.ID, nil)
	if code != 200 || bytes.Contains(dash, []byte("flagged")) {
		t.Fatalf("dashboard %d mentions flags", code)
	}
	// Not once the attempt is over.
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/submit", nil, 200, &st)
	flag(third, false, 409)
}

func TestNoFlagsInOneWayQuiz(t *testing.T) {
	f := setup(t, nil, nil, nil) // one-way by default (BR-09)
	s, st := running(t, f)
	if st.Flagged != nil {
		t.Fatalf("one-way state has flags: %v", st.Flagged)
	}
	if code, _ := s.Do("PUT", "/api/attempts/"+st.AttemptID+"/flags/"+st.Question.ID, map[string]bool{"flagged": true}); code != 409 {
		t.Fatalf("flag in a one-way quiz: %d", code)
	}
}

func equal(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
