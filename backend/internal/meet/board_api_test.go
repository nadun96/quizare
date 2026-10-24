package meet_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/meet"
)

func pen(gesture string, pts ...float64) map[string]any {
	return map[string]any{"gesture": gesture, "tool": "pen", "color": "#1d4ed8", "size": 4, "points": pts}
}

// ack sends a whiteboard request and waits for its answer.
func ack(t *testing.T, c *websocket.Conn, req string, m map[string]any) map[string]any {
	t.Helper()
	m["req"] = req
	send(t, c, m)
	for {
		a := apptest.ReadUntil(t, c, "board_ack", 3*time.Second)
		if a["req"] == req {
			return a
		}
	}
}

func TestMeetingWhiteboard(t *testing.T) {
	e := apptest.New(t)
	host := e.NewUser(auth.RoleTeacher)
	ann, ben := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	m := create(t, host, map[string]any{"title": "Board", "max_people": 4})

	hc := host.Dial("/ws/meet/" + m.Code)
	hw := apptest.ReadUntil(t, hc, "welcome", 3*time.Second)
	if b := hw["board"].(map[string]any); b["open"] != false || b["mode"] != "host" || b["can_draw"] != true || b["me"] != host.User.ID {
		t.Fatalf("host board = %v", b)
	}
	ac := ann.Dial("/ws/meet/" + m.Code)
	if b := apptest.ReadUntil(t, ac, "welcome", 3*time.Second)["board"].(map[string]any); b["can_draw"] != false {
		t.Fatalf("ann board = %v", b)
	}
	bc := ben.Dial("/ws/meet/" + m.Code)
	apptest.ReadUntil(t, bc, "welcome", 3*time.Second)

	// Hidden and host-only: Ann can't draw.
	if a := ack(t, ac, "a1", map[string]any{"type": "board_add", "strokes": []any{pen("g1", 10, 10, 20, 20)}}); a["error"] == nil {
		t.Fatalf("ann drew: %v", a)
	}
	// The host draws and shows it; everyone may draw now.
	if a := ack(t, hc, "h1", map[string]any{"type": "board_add", "strokes": []any{pen("hg", 100, 100, 200, 200)}}); a["error"] != nil {
		t.Fatalf("host: %v", a)
	}
	send(t, ac, map[string]any{"type": "board_access", "open": true, "mode": "everyone"}) // only the host decides
	send(t, hc, map[string]any{"type": "board_access", "open": true, "mode": "everyone"})
	for _, c := range []*websocket.Conn{hc, ac, bc} {
		if a := apptest.ReadUntil(t, c, "board_access", 2*time.Second); a["open"] != true || a["can_draw"] != true {
			t.Fatalf("access = %v", a)
		}
	}

	// Ann's mark: numbered, hers, and sent to the others.
	a := ack(t, ac, "a2", map[string]any{"type": "board_add", "strokes": []any{pen("g2", 10, 10, 20, 20), pen("g2", 20, 20, 30, 30)}})
	saved := a["strokes"].([]any)
	first := saved[0].(map[string]any)
	if len(saved) != 2 || first["by"] != ann.User.ID || first["id"].(float64) != 2 {
		t.Fatalf("saved = %v", a)
	}
	if ev := apptest.ReadUntil(t, bc, "board", 2*time.Second); ev["op"] != "add" || len(ev["strokes"].([]any)) != 2 {
		t.Fatalf("ben saw %v", ev)
	}
	// Checked like poll strokes.
	if a := ack(t, ac, "a3", map[string]any{"type": "board_add", "strokes": []any{map[string]any{"gesture": "x", "tool": "pen", "color": "red", "size": 4, "points": []float64{1, 1}}}}); a["error"] == nil {
		t.Fatal("a bad colour was kept")
	}
	// Ben can't erase Ann's mark; Ann undoes her whole line; the host erases anything.
	if a := ack(t, bc, "b1", map[string]any{"type": "board_erase", "ids": []int64{2}}); len(a["removed"].([]any)) != 0 {
		t.Fatalf("ben erased: %v", a)
	}
	if a := ack(t, ac, "a4", map[string]any{"type": "board_erase", "gesture": "g2"}); len(a["removed"].([]any)) != 2 {
		t.Fatalf("undo: %v", a)
	}
	for ev := apptest.ReadUntil(t, hc, "board", 2*time.Second); ev["op"] != "remove"; ev = apptest.ReadUntil(t, hc, "board", 2*time.Second) {
	}
	ack(t, bc, "b2", map[string]any{"type": "board_add", "strokes": []any{map[string]any{"gesture": "t1", "tool": "text", "color": "#111827", "size": 24, "points": []float64{400, 300}, "text": "Mitochondria"}}})
	if a := ack(t, hc, "h2", map[string]any{"type": "board_erase", "ids": []int64{4}}); len(a["removed"].([]any)) != 1 {
		t.Fatalf("host erase: %v", a)
	}

	// Late joiners and people who come back see the board.
	ack(t, bc, "b3", map[string]any{"type": "board_add", "strokes": []any{pen("g3", 5, 5, 6, 6)}})
	for _, c := range []*websocket.Conn{hc, ac, bc} {
		c.Close(websocket.StatusNormalClosure, "")
	}
	time.Sleep(200 * time.Millisecond)
	cat := e.NewUser(auth.RoleStudent)
	cc := cat.Dial("/ws/meet/" + m.Code)
	b := apptest.ReadUntil(t, cc, "welcome", 3*time.Second)["board"].(map[string]any)
	if b["open"] != true || len(b["strokes"].([]any)) != 2 {
		t.Fatalf("late joiner sees %v", b)
	}

	// Only the host clears; ending the meeting drops the board.
	if a := ack(t, cc, "c1", map[string]any{"type": "board_clear"}); a["error"] == nil {
		t.Fatal("cat cleared")
	}
	hc = host.Dial("/ws/meet/" + m.Code)
	apptest.ReadUntil(t, hc, "welcome", 3*time.Second)
	if a := ack(t, hc, "h3", map[string]any{"type": "board_clear"}); a["upto"].(float64) != 5 {
		t.Fatalf("clear: %v", a)
	}
	if ev := apptest.ReadUntil(t, cc, "board", 2*time.Second); ev["op"] != "clear" {
		t.Fatalf("cat saw %v", ev)
	}
}

func TestMeetingLimits(t *testing.T) {
	e := apptest.New(t)
	admin, host := e.NewUser(auth.RoleAdmin), e.NewUser(auth.RoleTeacher)
	var lim meet.Limits
	host.Call("GET", "/api/teacher/meeting-limits", nil, 200, &lim)
	if lim.MaxPeople != 8 || lim.MaxOpen != 0 {
		t.Fatalf("defaults = %+v", lim)
	}
	host.Call("PUT", "/api/admin/meeting-limits", map[string]int{"max_people": 0}, 403, nil)
	admin.Call("PUT", "/api/admin/meeting-limits", map[string]int{"max_people": 1, "max_open": -1}, 422, nil)

	// Small meetings, one each.
	admin.Call("PUT", "/api/admin/meeting-limits", map[string]int{"max_people": 3, "max_open": 1}, 200, nil)
	host.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Big", "max_people": 4}, 422, nil)
	host.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Unlimited", "max_people": 0}, 422, nil)
	first := create(t, host, map[string]any{"title": "Default size"})
	if first.MaxPeople != 3 {
		t.Fatalf("the default fits the limit: %d", first.MaxPeople)
	}
	code, raw := host.Do("POST", "/api/teacher/meetings", map[string]any{"title": "Second"})
	if code != 409 || !strings.Contains(string(raw), "too_many_meetings") {
		t.Fatalf("second: %d %s", code, raw)
	}
	host.Call("POST", "/api/teacher/meetings/"+first.ID+"/end", nil, 204, nil)

	// No limits: any size, or none at all.
	admin.Call("PUT", "/api/admin/meeting-limits", map[string]int{"max_people": 0, "max_open": 0}, 200, nil)
	open := create(t, host, map[string]any{"title": "Open to all", "max_people": 0})
	big := create(t, host, map[string]any{"title": "Forty", "max_people": 40})
	if open.MaxPeople != 0 || big.MaxPeople != 40 {
		t.Fatalf("sizes %d %d", open.MaxPeople, big.MaxPeople)
	}
	var acc meet.Access
	host.Call("GET", "/api/meetings/"+open.Code, nil, 200, &acc)
	if acc.Meeting.MaxPeople != 0 {
		t.Fatalf("unlimited = %d", acc.Meeting.MaxPeople)
	}
	for range 9 { // more than the old cap of 8
		c := e.NewUser(auth.RoleStudent).Dial("/ws/meet/" + open.Code)
		apptest.ReadUntil(t, c, "welcome", 3*time.Second)
	}

	// Lowering the limit applies to open meetings at once.
	admin.Call("PUT", "/api/admin/meeting-limits", map[string]int{"max_people": 2}, 200, nil)
	host.Call("GET", "/api/meetings/"+big.Code, nil, 200, &acc)
	if acc.Meeting.MaxPeople != 2 {
		t.Fatalf("capped = %d", acc.Meeting.MaxPeople)
	}
	apptest.ReadUntil(t, host.Dial("/ws/meet/"+big.Code), "welcome", 3*time.Second)
	apptest.ReadUntil(t, e.NewUser(auth.RoleStudent).Dial("/ws/meet/"+big.Code), "welcome", 3*time.Second)
	third := e.NewUser(auth.RoleStudent).Dial("/ws/meet/" + big.Code)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := third.Read(ctx); websocket.CloseStatus(err) != 4009 {
		t.Fatalf("third: %v", err)
	}
}
