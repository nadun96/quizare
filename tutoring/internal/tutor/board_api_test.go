package tutor_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// The session's whiteboard (D-85, TS-FR-66): teachers draw; students watch,
// unless the lead teacher lets everyone or chosen students draw.

func pen(g string) tutor.Stroke {
	return tutor.Stroke{Gesture: g, Tool: "pen", Color: "#111827", Size: 3, Points: []float64{10, 10, 20, 20}}
}

func TestBoard(t *testing.T) {
	f := setup(t, "auto")
	ann, bob := f.student("Ann"), f.student("Bob")
	for _, p := range []*testenv.Person{f.teacher, ann, bob} {
		f.join(p, 200)
	}
	co := f.e.NewPerson("teacher", "Mr Silva")
	f.teacher.Call("POST", f.path("/coteachers"), map[string]string{"email": co.Email}, 201, nil)
	f.join(co, 200)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	as, bs := ann.Dial(f.sess.ID), bob.Dial(f.sess.ID)
	as.Must("state")
	bs.Must("state")
	type out struct{ Strokes []tutor.Stroke }
	var v tutor.BoardView

	// Closed: students see nothing and can't draw; teachers prepare it.
	ann.Call("GET", f.path("/board"), nil, 200, &v)
	if v.Open || v.CanDraw || len(v.Strokes) != 0 || v.Access != nil || v.Me != ann.ID {
		t.Fatalf("Ann's closed board = %+v", v)
	}
	var got out
	co.Call("POST", f.path("/board/strokes"), map[string]any{"strokes": []tutor.Stroke{pen("c1")}}, 200, &got)
	if len(got.Strokes) != 1 || got.Strokes[0].ID == 0 || got.Strokes[0].By != co.ID {
		t.Fatalf("co-teacher's stroke = %+v", got)
	}
	as.Quiet("board", 300*time.Millisecond) // not open: students don't get the marks
	ann.Call("POST", f.path("/board/strokes"), map[string]any{"strokes": []tutor.Stroke{pen("a0")}}, 403, nil)

	// Shown: everyone sees it; still only teachers draw.
	co.Call("PUT", f.path("/board/access"), tutor.BoardAccessInput{Open: true}, 403, nil) // the lead's
	f.teacher.Call("PUT", f.path("/board/access"), tutor.BoardAccessInput{Open: true}, 204, nil)
	if m := as.Must("board")["data"].(map[string]any); m["op"] != "access" || m["open"] != true {
		t.Fatalf("access event = %v", m)
	}
	v = tutor.BoardView{}
	ann.Call("GET", f.path("/board"), nil, 200, &v)
	if !v.Open || v.CanDraw || len(v.Strokes) != 1 {
		t.Fatalf("Ann's open board = %+v", v)
	}
	f.teacher.Call("POST", f.path("/board/strokes"), map[string]any{"strokes": []tutor.Stroke{pen("t1"), {Gesture: "t2", Tool: "text", Color: "#111827", Size: 3, Points: []float64{100, 100}, Text: " Hello\u0007 "}}}, 200, &got)
	if got.Strokes[1].Text != "Hello" {
		t.Fatalf("text kept as %q", got.Strokes[1].Text)
	}
	if m := as.Must("board")["data"].(map[string]any); m["op"] != "add" || len(m["strokes"].([]any)) != 2 {
		t.Fatalf("add event = %v", m)
	}

	// Chosen students: Ann draws, Bob doesn't.
	f.teacher.Call("PUT", f.path("/board/access"), tutor.BoardAccessInput{Open: true, Mode: "selected", Participants: []string{ann.ID, co.ID}}, 204, nil)
	var tv tutor.View
	f.teacher.Call("GET", f.path(""), nil, 200, &tv)
	if tv.Session.BoardMode != "selected" || len(tv.Session.BoardUsers) != 1 || tv.Session.BoardUsers[0] != ann.ID {
		t.Fatalf("selected = %+v (only students are kept)", tv.Session.BoardUsers)
	}
	var av tutor.View
	ann.Call("GET", f.path(""), nil, 200, &av)
	if len(av.Session.BoardUsers) != 0 {
		t.Fatal("students see who else may draw")
	}
	v = tutor.BoardView{}
	ann.Call("GET", f.path("/board"), nil, 200, &v)
	if !v.CanDraw {
		t.Fatal("Ann can't draw when chosen")
	}
	ann.Call("POST", f.path("/board/strokes"), map[string]any{"strokes": []tutor.Stroke{pen("a1"), pen("a1")}}, 200, &got)
	annIDs := []int64{got.Strokes[0].ID, got.Strokes[1].ID}
	bob.Call("POST", f.path("/board/strokes"), map[string]any{"strokes": []tutor.Stroke{pen("b1")}}, 403, nil)

	// Checked marks; at most 20 at once.
	bad := pen("x")
	bad.Color = "red"
	ann.Call("POST", f.path("/board/strokes"), map[string]any{"strokes": []tutor.Stroke{bad}}, 422, nil)
	off := pen("x")
	off.Points = []float64{5000, 10}
	ann.Call("POST", f.path("/board/strokes"), map[string]any{"strokes": []tutor.Stroke{off}}, 422, nil)
	many := make([]tutor.Stroke, 21)
	for i := range many {
		many[i] = pen("m")
	}
	ann.Call("POST", f.path("/board/strokes"), map[string]any{"strokes": many}, 422, nil)

	// Erasing: students their own only (a gesture takes the whole line); teachers anything.
	var rm struct{ Removed []int64 }
	ann.Call("POST", f.path("/board/erase"), map[string]any{"ids": []int64{v.Strokes[0].ID}}, 200, &rm)
	if len(rm.Removed) != 0 {
		t.Fatalf("Ann erased a teacher's mark: %v", rm.Removed)
	}
	ann.Call("POST", f.path("/board/erase"), map[string]any{"gesture": "a1"}, 200, &rm)
	if len(rm.Removed) != 2 || rm.Removed[0] != annIDs[0] {
		t.Fatalf("undo = %v", rm.Removed)
	}
	f.teacher.Call("POST", f.path("/board/erase"), map[string]any{"ids": []int64{v.Strokes[0].ID}}, 200, &rm)
	if len(rm.Removed) != 1 {
		t.Fatalf("teacher erase = %v", rm.Removed)
	}
	for {
		m, err := bs.Next("board", time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if d := m["data"].(map[string]any); d["op"] == "remove" && len(d["ids"].([]any)) == 1 {
			break
		}
	}

	// Clearing is the teachers'; marks before it are gone for good.
	ann.Call("POST", f.path("/board/clear"), nil, 403, nil)
	var cl struct{ Upto int64 }
	co.Call("POST", f.path("/board/clear"), nil, 200, &cl)
	if cl.Upto == 0 {
		t.Fatal("clear upto 0")
	}
	v = tutor.BoardView{}
	f.teacher.Call("GET", f.path("/board"), nil, 200, &v)
	if len(v.Strokes) != 0 || v.Access == nil || v.Access.Mode != "selected" {
		t.Fatalf("after clearing = %+v", v)
	}

	// Hidden again: students stop getting marks; strangers never see it.
	f.teacher.Call("PUT", f.path("/board/access"), tutor.BoardAccessInput{Open: false, Mode: "everyone"}, 204, nil)
	ann.Call("POST", f.path("/board/strokes"), map[string]any{"strokes": []tutor.Stroke{pen("a2")}}, 403, nil)
	f.teacher.Call("PUT", f.path("/board/access"), tutor.BoardAccessInput{Open: true, Mode: "scribble"}, 422, nil)
	stranger := f.e.NewPerson("student", "Zed")
	stranger.Call("GET", f.path("/board"), nil, 404, nil)
	f.teacher.Call("POST", f.path("/end"), nil, 204, nil)
	code, raw := f.teacher.Do("POST", f.path("/board/strokes"), map[string]any{"strokes": []tutor.Stroke{pen("t9")}})
	if code != 410 || !strings.Contains(string(raw), "session_ended") {
		t.Fatalf("drawing after the end: %d %s", code, raw)
	}
}
