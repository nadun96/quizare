package tutor_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// Phase 2 (D-60): co-teachers, the broadcaster, answers to requests, layout.

var all4 = []string{"camera", "microphone", "screen_share", "screen_share_audio"}

func TestCoteachers(t *testing.T) {
	f := setup(t, "manual")
	co := f.e.NewPerson("teacher", "Mr Silva")
	ann := f.student("Ann")
	stranger := f.e.NewPerson("student", "Zed")

	// Only the lead teacher adds co-teachers, by an active teacher's email (TS-FR-70).
	f.teacher.Call("POST", f.path("/coteachers"), map[string]string{"email": "nobody@example.edu"}, 422, nil)
	f.teacher.Call("POST", f.path("/coteachers"), map[string]string{"email": f.teacher.Email}, 422, nil)
	var added tutor.Person
	f.teacher.Call("POST", f.path("/coteachers"), map[string]string{"email": strings.ToUpper(co.Email)}, 201, &added)
	if added.ID != co.ID || added.Name != "Mr Silva" {
		t.Fatalf("added %+v", added)
	}
	// They join without being in the classroom, admitted at once, and see their co-taught sessions.
	v := f.join(co, 200)
	if v.Me.Role != "coteacher" || v.Me.State != "admitted" || v.Participants == nil || v.Session.JoinCode == "" {
		t.Fatalf("co-teacher view = %+v", v)
	}
	var list struct{ Total int }
	co.Call("GET", "/api/sessions", nil, 200, &list)
	if list.Total != 1 {
		t.Fatalf("co-teacher's sessions = %d", list.Total)
	}
	co.Call("POST", f.path("/coteachers"), map[string]string{"email": stranger.Email}, 403, nil) // lead only

	// Co-teachers admit, allow and moderate (TS-FR-71)…
	f.join(ann, 200)
	co.Call("POST", f.path("/admit"), map[string]any{"user_ids": []string{ann.ID}, "admit": true}, 200, nil)
	yes := true
	co.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{UserIDs: []string{ann.ID}, Camera: &yes}, 200, nil)
	co.Call("PUT", f.path("/settings"), map[string]any{"chat_mode": "everyone"}, 204, nil)
	co.Call("GET", f.path("/attendance"), nil, 200, nil)
	// …but don't start, end, lock, remove students, change the layout or choose the broadcaster.
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/start", nil}, {"POST", "/end", nil}, {"PUT", "/settings", map[string]any{"locked": true}},
		{"PUT", "/settings", map[string]any{"layout": "grid"}}, {"DELETE", "/participants/" + ann.ID, nil},
		{"PUT", "/broadcaster", map[string]string{"user_id": ann.ID}}, {"DELETE", "/coteachers/" + co.ID, nil},
	} {
		code, body := co.Do(c.method, f.path(c.path), c.body)
		if code != 403 || !strings.Contains(string(body), "lead_only") {
			t.Errorf("co-teacher %s %s: %d %s", c.method, c.path, code, body)
		}
	}

	// A co-teacher shares backstage, where teachers see it (TS-FR-72), and watches it.
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	if got := sources(t, f, co); len(got) != 0 {
		t.Fatalf("co-teacher broadcasts %v", got)
	}
	if _, st := grants(t, f, co); st == nil || !st.Subscribe || !slices.Equal(st.Sources, all4) {
		t.Fatalf("co-teacher backstage = %+v", st)
	}
	// Students see who teaches (TS-FR-73).
	var sv tutor.View
	ann.Call("GET", f.path(""), nil, 200, &sv)
	if len(sv.Teachers) != 2 || sv.Teachers[0].Role != "teacher" || sv.Teachers[1].Name != "Mr Silva" || sv.Participants != nil {
		t.Fatalf("student's teachers = %+v", sv.Teachers)
	}

	// Removing a co-teacher ends their part.
	sock := co.Dial(f.sess.ID)
	sock.Must("state")
	f.teacher.Call("DELETE", f.path("/coteachers/"+co.ID), nil, 204, nil)
	if got := sock.Must("closed"); got["data"].(map[string]any)["reason"] != "removed" {
		t.Fatalf("closed = %v", got)
	}
	co.Call("GET", f.path("/attendance"), nil, 404, nil)
	// Rejoining now needs the classroom, which they aren't in.
	code, body := co.Do("POST", "/api/join/"+f.sess.JoinCode, nil)
	if code != 403 {
		t.Fatalf("removed co-teacher rejoins: %d %s", code, body)
	}
}

func TestHandingTheBroadcastOver(t *testing.T) {
	f := setup(t, "auto")
	ann, bob := f.student("Ann"), f.student("Bob")
	f.join(ann, 200)
	f.join(bob, 200)
	f.join(f.teacher, 200)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	as, bs := ann.Dial(f.sess.ID), bob.Dial(f.sess.ID)
	as.Must("state")
	bs.Must("state")

	// The lead teacher asks Ann; nothing changes until she accepts (TS-FR-17, PO-3).
	f.teacher.Call("PUT", f.path("/broadcaster"), map[string]string{"user_id": ann.ID}, 204, nil)
	as.Must("broadcast_offer")
	if got := sources(t, f, ann); len(got) != 0 {
		t.Fatalf("broadcasting before accepting: %v", got)
	}
	bob.Call("POST", f.path("/broadcast-answer"), map[string]bool{"accept": true}, 409, nil) // not asked
	f.e.LiveKit.Calls("UpdateParticipant")
	ann.Call("POST", f.path("/broadcast-answer"), map[string]bool{"accept": true}, 204, nil)

	// Ann broadcasts; the lead teacher keeps only the microphone in the broadcast.
	if got := sources(t, f, ann); !slices.Equal(got, all4) {
		t.Fatalf("ann broadcasts %v", got)
	}
	if got := sources(t, f, f.teacher); !slices.Equal(got, []string{"microphone"}) {
		t.Fatalf("teacher while ann broadcasts: %v", got)
	}
	if got := sources(t, f, bob); len(got) != 0 {
		t.Fatalf("bob broadcasts %v", got)
	}
	calls := f.e.LiveKit.Calls("UpdateParticipant")
	byRoom := map[string]int{}
	for _, c := range calls {
		byRoom[c.Body["room"].(string)+" "+c.Body["identity"].(string)]++
	}
	if byRoom[f.sess.ID+" "+ann.ID] != 1 || byRoom[f.sess.ID+" "+f.teacher.ID] != 1 {
		t.Fatalf("live updates = %v", byRoom)
	}
	var bv tutor.View
	bob.Call("GET", f.path(""), nil, 200, &bv)
	if bv.Session.BroadcasterID == nil || *bv.Session.BroadcasterID != ann.ID {
		t.Fatalf("bob's view: broadcaster %v", bv.Session.BroadcasterID)
	}

	// Asking Bob, who declines: the teacher hears so, and Ann still broadcasts.
	ts := f.teacher.Dial(f.sess.ID)
	ts.Must("state")
	f.teacher.Call("PUT", f.path("/broadcaster"), map[string]string{"user_id": bob.ID}, 204, nil)
	bs.Must("broadcast_offer")
	bob.Call("POST", f.path("/broadcast-answer"), map[string]bool{"accept": false}, 204, nil)
	if got := ts.Must("broadcast_answer"); got["data"].(map[string]any)["accepted"] != false {
		t.Fatalf("answer = %v", got)
	}
	if got := sources(t, f, ann); !slices.Equal(got, all4) {
		t.Fatalf("ann after bob declined: %v", got)
	}

	// Taking it back, in one step.
	f.teacher.Call("PUT", f.path("/broadcaster"), map[string]string{"user_id": ""}, 204, nil)
	as.Must("broadcast_ended")
	if got := sources(t, f, ann); len(got) != 0 {
		t.Fatalf("ann after take-back: %v", got)
	}
	if got := sources(t, f, f.teacher); !slices.Equal(got, all4) {
		t.Fatalf("teacher after take-back: %v", got)
	}
	f.teacher.Call("PUT", f.path("/broadcaster"), map[string]string{"user_id": "00000000-0000-4000-8000-000000000000"}, 404, nil)
}

func TestAnswersAndLayout(t *testing.T) {
	f := setup(t, "auto")
	ann := f.student("Ann")
	f.join(ann, 200)
	f.join(f.teacher, 200)
	ts := f.teacher.Dial(f.sess.ID)
	ts.Must("state")
	// A student's answer to "turn on your camera" reaches the teachers (TS-FR-22).
	ann.Call("POST", f.path("/ask-answer"), map[string]bool{"accepted": false}, 204, nil)
	got := ts.Must("ask_answer")
	if d := got["data"].(map[string]any); d["accepted"] != false || d["name"] != "Ann" {
		t.Fatalf("ask answer = %v", got)
	}
	f.teacher.Call("POST", f.path("/ask-answer"), map[string]bool{"accepted": true}, 403, nil)

	// The teacher's layout reaches students (TS-FR-13).
	as := ann.Dial(f.sess.ID)
	as.Must("state")
	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"layout": "grid"}, 204, nil)
	for {
		st := as.Must("state")
		if st["data"].(map[string]any)["session"].(map[string]any)["layout"] == "grid" {
			break
		}
	}
	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"layout": "mosaic"}, 422, nil)
	_ = testenv.Secret
}
