package tutor_test

import (
	"testing"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// D-149: a finished session goes to the archive, back, and away for good.
func TestSessionArchive(t *testing.T) {
	f := setup(t, "auto")
	url := "/api/sessions/" + f.sess.ID

	titles := func(query string) []string {
		var out struct{ Sessions []tutor.Session }
		f.teacher.Call("GET", "/api/sessions?"+query, nil, 200, &out)
		list := []string{}
		for _, s := range out.Sessions {
			list = append(list, s.Title)
		}
		return list
	}
	// Still to come: it can't be archived, and isn't past.
	f.teacher.Call("POST", url+"/archive", nil, 409, nil)
	if got := titles("view=past"); len(got) != 0 {
		t.Fatalf("past before it ended: %v", got)
	}
	if _, err := f.e.Pool.Exec(t.Context(), `UPDATE tutoring.sessions SET status = 'ended', ended_at = now(), started_at = now() WHERE id = $1`, f.sess.ID); err != nil {
		t.Fatal(err)
	}
	if got := titles("view=past"); len(got) != 1 {
		t.Fatalf("past: %v", got)
	}
	if got := titles(""); len(got) != 1 {
		t.Fatalf("everyday: %v", got)
	}
	f.teacher.Call("DELETE", url, nil, 409, nil) // archive first

	// A student can't.
	st := f.student("Amaya")
	st.Call("POST", url+"/archive", nil, 404, nil)

	f.teacher.Call("POST", url+"/archive", nil, 204, nil)
	if got := titles(""); len(got) != 0 {
		t.Fatalf("still in the everyday list: %v", got)
	}
	if got := titles("view=past"); len(got) != 0 {
		t.Fatalf("still past: %v", got)
	}
	if got := titles("view=archived&q=algebra"); len(got) != 1 {
		t.Fatalf("archived search: %v", got)
	}
	if got := titles("view=archived&q=nothing"); len(got) != 0 {
		t.Fatalf("archived search for nothing: %v", got)
	}
	today := time.Now().UTC().Format("2006-01-02")
	if got := titles("view=archived&from=" + today + "&to=" + today); len(got) != 1 {
		t.Fatalf("today: %v", got)
	}
	if got := titles("view=archived&to=2001-01-01"); len(got) != 0 {
		t.Fatalf("long ago: %v", got)
	}
	f.teacher.Call("GET", "/api/sessions?view=archived&from=soon", nil, 422, nil)

	f.teacher.Call("POST", url+"/restore", nil, 204, nil)
	if got := titles("view=past"); len(got) != 1 {
		t.Fatalf("restored: %v", got)
	}
	f.teacher.Call("POST", url+"/archive", nil, 204, nil)
	f.teacher.Call("DELETE", url, nil, 204, nil)
	if got := titles("view=archived"); len(got) != 0 {
		t.Fatalf("left: %v", got)
	}
	f.teacher.Call("POST", url+"/restore", nil, 404, nil)
}
