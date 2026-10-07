package poll_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/poll"
)

func pen(gesture string, pts ...float64) map[string]any {
	return map[string]any{"gesture": gesture, "tool": "pen", "color": "#1d4ed8", "size": 4, "points": pts}
}

func (p *player) board() poll.BoardView {
	var v poll.BoardView
	p.c.Call("GET", "/api/polls/"+p.f.poll.JoinCode+"/board", nil, 200, &v)
	return v
}

func (p *player) draw(want int, strokes ...map[string]any) []poll.Stroke {
	var out struct{ Strokes []poll.Stroke }
	code, body := p.c.Do("POST", "/api/polls/"+p.f.poll.JoinCode+"/board/strokes", map[string]any{"strokes": strokes})
	if code != want {
		p.f.e.T.Fatalf("draw: %d %s", code, body)
	}
	_ = json.Unmarshal(body, &out)
	return out.Strokes
}

func (f *fixture) access(t *testing.T, a map[string]any) poll.BoardAccess {
	t.Helper()
	var out poll.BoardAccess
	f.teacher.Call("PUT", "/api/teacher/polls/"+f.poll.ID+"/board/access", a, 200, &out)
	return out
}

func TestWhiteboard(t *testing.T) {
	st := settings("anonymous", "self", "live")
	st["groups"] = "manual"
	f := setup(t, st, singleQ)
	ann, ben := f.join(t, "Ann"), f.join(t, "Ben")
	audience := ann.c.Dial("/ws/polls/" + f.poll.JoinCode)
	apptest.ReadUntil(t, audience, "update", 3*time.Second)

	// The teacher always draws; the board starts hidden from participants.
	var tv poll.BoardView
	f.teacher.Call("GET", "/api/teacher/polls/"+f.poll.ID+"/board", nil, 200, &tv)
	if tv.Open || !tv.CanDraw || tv.Access == nil || tv.Access.Mode != "teacher" {
		t.Fatalf("teacher board: %+v", tv)
	}
	var drawn struct{ Strokes []poll.Stroke }
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/board/strokes", map[string]any{"strokes": []map[string]any{
		pen("t1", 100, 100, 200, 150, 300, 120),
		{"gesture": "t2", "tool": "text", "color": "#000000", "size": 24, "points": []float64{400, 300}, "text": "  Photosynthesis\u0007 "},
	}}, 201, &drawn)
	if len(drawn.Strokes) != 2 || drawn.Strokes[1].Text != "Photosynthesis" || drawn.Strokes[0].By != "t" {
		t.Fatalf("teacher strokes: %+v", drawn.Strokes)
	}
	// Pushed to open screens at once, without waiting for a flush.
	msg := apptest.ReadUntil(t, audience, "board", 2*time.Second)
	if msg["op"] != "add" || len(msg["strokes"].([]any)) != 2 {
		t.Fatalf("board push: %v", msg)
	}
	if v := ann.board(); v.Open || len(v.Strokes) != 0 {
		t.Fatalf("hidden board leaked: %+v", v)
	}

	// Shown, teacher only: everyone sees it, nobody else draws.
	f.access(t, map[string]any{"open": true, "mode": "teacher"})
	v := ann.board()
	if !v.Open || v.CanDraw || len(v.Strokes) != 2 || v.Me == "" || v.Me == "t" {
		t.Fatalf("view-only board: %+v", v)
	}
	if up := f.view(ann.c); !up.BoardOpen {
		t.Fatal("poll state says the board is open")
	}
	ann.draw(403, pen("a1", 1, 1, 2, 2))

	// Everyone may draw; strokes are checked.
	f.access(t, map[string]any{"open": true, "mode": "everyone"})
	if !ann.board().CanDraw {
		t.Fatal("everyone may draw")
	}
	mine := ann.draw(201, pen("a1", 10, 10, 20, 20), pen("a1", 20, 20, 30, 30))
	if len(mine) != 2 || mine[0].By != v.Me {
		t.Fatalf("own strokes: %+v (me %s)", mine, v.Me)
	}
	for _, bad := range []map[string]any{
		{"gesture": "x", "tool": "pen", "color": "red", "size": 4, "points": []float64{1, 1}},
		{"gesture": "x", "tool": "rect", "color": "#ff0000", "size": 4, "points": []float64{1, 1}},
		{"gesture": "x", "tool": "text", "color": "#ff0000", "size": 4, "points": []float64{1, 1}, "text": strings.Repeat("a", 201)},
		{"gesture": "x", "tool": "spray", "color": "#ff0000", "size": 4, "points": []float64{1, 1}},
		{"gesture": "x", "tool": "pen", "color": "#ff0000", "size": 4, "points": []float64{1, 99999}},
		{"gesture": "bad gesture!", "tool": "pen", "color": "#ff0000", "size": 4, "points": []float64{1, 1}},
	} {
		ann.draw(422, bad)
	}
	// Participants erase and undo only their own strokes.
	var removed struct{ Removed []int64 }
	ann.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/board/erase", map[string]any{"ids": []int64{drawn.Strokes[0].ID}}, 200, &removed)
	if len(removed.Removed) != 0 {
		t.Fatal("a participant erased the teacher's stroke")
	}
	ben.draw(201, pen("b1", 50, 50, 60, 60))
	ann.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/board/erase", map[string]any{"gesture": "a1"}, 200, &removed)
	if len(removed.Removed) != 2 {
		t.Fatalf("undo removes the whole gesture: %v", removed.Removed)
	}
	// Late joiners see the whole board.
	cat := f.join(t, "Cat")
	if v := cat.board(); len(v.Strokes) != 3 {
		t.Fatalf("late joiner sees %d strokes", len(v.Strokes))
	}

	// Selected: chosen participants and groups.
	var gv poll.GroupsView
	f.teacher.Call("GET", "/api/teacher/polls/"+f.poll.ID+"/groups", nil, 200, &gv)
	ids := map[string]string{}
	for _, m := range gv.Ungrouped {
		ids[m.Nickname] = m.ParticipantID
	}
	var g poll.Group
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups", map[string]any{"name": "Artists"}, 201, &g)
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups/members", map[string]any{"participant_ids": []string{ids["Cat"]}, "group_id": g.ID}, 204, nil)
	a := f.access(t, map[string]any{"open": true, "mode": "selected", "participants": []string{ids["Ann"], f.poll.ID}, "groups": []string{g.ID}})
	if len(a.Participants) != 1 || len(a.Groups) != 1 {
		t.Fatalf("foreign ids are dropped: %+v", a)
	}
	if !ann.board().CanDraw || ben.board().CanDraw || !cat.board().CanDraw {
		t.Fatal("selected participants and groups")
	}
	ben.draw(403, pen("b2", 1, 1, 2, 2))

	// The teacher clears the board for everyone.
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/board/clear", nil, 204, nil)
	for {
		m := apptest.ReadUntil(t, audience, "board", 2*time.Second)
		if m["op"] == "clear" {
			break
		}
	}
	if v := cat.board(); len(v.Strokes) != 0 {
		t.Fatalf("after clear: %d", len(v.Strokes))
	}
	// Closed polls stop drawing; hidden boards show nothing.
	f.access(t, map[string]any{"open": false, "mode": "everyone"})
	ann.draw(403, pen("a3", 1, 1, 2, 2))
}
