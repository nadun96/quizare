package live_test

import (
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/live"
)

// A student's "My results" list is paginated, newest first (PL-FR-02).
func TestMyAttemptsArePaginated(t *testing.T) {
	f := setup(t, nil, nil, nil)
	s, _ := f.join(t)
	for range 4 {
		f.clk.add(time.Second) // attempts are stamped with the service clock
		var sess live.Session
		f.teacher.Call("POST", "/api/teacher/quizzes/"+f.quiz.ID+"/sessions", map[string]any{}, 201, &sess)
		s.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{}, 200, nil)
	}
	all := s.CheckPages("/api/my/attempts", "attempts", "attempt_id", 5, 2)
	if all[4]["session_id"] != f.sess.ID {
		t.Fatalf("the first session joined is last: %v", all[4]["session_id"])
	}
	if p := s.Page("/api/my/attempts?q=no%20such%20quiz", "attempts", 1, 25); p.Total != 0 {
		t.Fatalf("search: %d", p.Total)
	}
}
