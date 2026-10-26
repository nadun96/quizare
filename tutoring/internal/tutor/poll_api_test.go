package tutor_test

import (
	"testing"

	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// A poll in the session (D-86, TS-FR-68): the teachers show one of their
// polls; everyone's view says which; students are told.

func TestSessionPoll(t *testing.T) {
	f := setup(t, "auto")
	ann := f.student("Ann")
	f.join(f.teacher, 200)
	f.join(ann, 200)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	as := ann.Dial(f.sess.ID)
	as.Must("state")
	co := f.e.NewPerson("teacher", "Mr Silva")
	f.teacher.Call("POST", f.path("/coteachers"), map[string]string{"email": co.Email}, 201, nil)
	f.join(co, 200)

	in := tutor.PollInput{PollID: "6f1c9a2e-0b7d-4c55-9a3e-2d1f0c8b7a61", Code: " ab12cd ", Title: "Quick check"}
	ann.Call("PUT", f.path("/poll"), in, 404, nil) // teachers only
	for name, bad := range map[string]tutor.PollInput{
		"code":  {PollID: in.PollID, Code: "no!", Title: "x"},
		"id":    {PollID: "x", Code: "AB12CD", Title: "x"},
		"title": {PollID: in.PollID, Code: "AB12CD"},
	} {
		if code, _ := f.teacher.Do("PUT", f.path("/poll"), bad); code != 422 {
			t.Fatalf("bad %s: %d", name, code)
		}
	}
	co.Call("PUT", f.path("/poll"), in, 204, nil) // co-teachers run polls too
	if m := as.Must("poll_shown")["data"].(map[string]any); m["title"] != "Quick check" {
		t.Fatalf("poll_shown = %v", m)
	}
	var v tutor.View
	ann.Call("GET", f.path(""), nil, 200, &v)
	if v.Session.PollCode == nil || *v.Session.PollCode != "AB12CD" || *v.Session.PollTitle != "Quick check" || *v.Session.PollID != in.PollID {
		t.Fatalf("Ann's view = %+v", v.Session)
	}
	// Hidden.
	f.teacher.Call("PUT", f.path("/poll"), tutor.PollInput{}, 204, nil)
	v = tutor.View{}
	ann.Call("GET", f.path(""), nil, 200, &v)
	if v.Session.PollCode != nil {
		t.Fatalf("still shown: %+v", v.Session.PollCode)
	}
	f.teacher.Call("POST", f.path("/end"), nil, 204, nil)
	f.teacher.Call("PUT", f.path("/poll"), in, 409, nil)
}
