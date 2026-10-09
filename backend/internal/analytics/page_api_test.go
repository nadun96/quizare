package analytics_test

import (
	"testing"
)

// A quiz's sessions, a session's results and integrity log, and result links
// are paginated on the server (PL-FR-02).
func TestSessionListsArePaginated(t *testing.T) {
	f := setup(t)
	sessions := make([]string, 0, 5)
	for range 5 {
		sessions = append(sessions, f.session(t).ID)
	}
	all := f.teacher.CheckPages("/api/teacher/quizzes/"+f.quizID+"/sessions", "sessions", "id", 5, 2)
	if all[0]["id"] != sessions[4] {
		t.Fatal("newest session first")
	}

	sess := f.session(t)
	f.take(t, sess, "S1", "Ann", "", "100")
	f.take(t, sess, "S2", "Ben", "", "50")
	f.take(t, sess, "S3", "Cat", "", "100")
	f.process(t) // mark the attempts
	res := f.teacher.CheckPages("/api/teacher/sessions/"+sess.ID+"/results", "results", "attempt_id", 3, 2)
	if res[0]["student_number"] != "S1" {
		t.Fatalf("results in join order: %v", res[0]["student_number"])
	}
	low := f.teacher.Page("/api/teacher/sessions/"+sess.ID+"/results?sort=score", "results", 1, 1)
	if low.Total != 3 || low.Rows[0]["student_number"] != "S2" {
		t.Fatalf("lowest score first: %+v", low.Rows)
	}
	if p := f.teacher.Page("/api/teacher/sessions/"+sess.ID+"/results?q=s3", "results", 1, 25); p.Total != 1 {
		t.Fatalf("search by student ID: %d", p.Total)
	}

	// The integrity log: join, admit and start events for 3 students.
	first := f.teacher.Page("/api/teacher/sessions/"+sess.ID+"/events", "events", 1, 4)
	if first.Total < 6 {
		t.Fatalf("events total %d", first.Total)
	}
	f.teacher.CheckPages("/api/teacher/sessions/"+sess.ID+"/events", "events", "id", first.Total, 4)
	if p := f.teacher.Page("/api/teacher/sessions/"+sess.ID+"/events?kind=admitted", "events", 1, 25); p.Total != 3 {
		t.Fatalf("admitted events: %d", p.Total)
	}

	for range 4 {
		f.teacher.Call("POST", "/api/teacher/share-links", map[string]any{"scope": "session", "target_id": sess.ID, "views": []string{"question_pct"}}, 201, nil)
	}
	f.teacher.CheckPages("/api/teacher/share-links?target_id="+sess.ID, "links", "id", 4, 3)
	f.teacher.Call("POST", "/api/teacher/share-links", map[string]any{"scope": "live_session", "target_id": sess.ID, "views": []string{"leaderboard"}}, 201, nil)
	if p := f.teacher.Page("/api/teacher/share-links?target_id="+sess.ID+"&scope=session,quiz", "links", 1, 25); p.Total != 4 {
		t.Fatalf("scope filter: %d", p.Total)
	}
}
