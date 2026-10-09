package poll

import (
	"encoding/json"
	"testing"
	"time"
)

// Board events within boardWindow reach a screen together, with consecutive
// adds and removes merged, and in their original order (D-52).
func TestBoardEventsAreGroupedPerWindow(t *testing.T) {
	h := newHub(nil)
	c := newClient(func() { t.Error("a grouped burst must not overflow the socket") })
	h.audience["p1"] = map[*client]struct{}{c: {}}
	for i := range 40 { // more than the socket's 32-message buffer
		h.board("p1", map[string]any{"type": "board", "op": "add", "strokes": []Stroke{{ID: int64(i + 1)}}})
	}
	h.board("p1", map[string]any{"type": "board", "op": "remove", "ids": []int64{3}})
	h.board("p1", map[string]any{"type": "board", "op": "remove", "ids": []int64{4}})
	h.board("p1", map[string]any{"type": "board", "op": "add", "strokes": []Stroke{{ID: 41}}})
	h.board("p1", map[string]any{"type": "board", "op": "clear", "upto": 41})

	var got []map[string]any
	deadline := time.After(time.Second)
	for len(got) < 4 {
		select {
		case m := <-c.send:
			var ev map[string]any
			if err := json.Unmarshal(m, &ev); err != nil {
				t.Fatal(err)
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("got %d messages: %v", len(got), got)
		}
	}
	ops := []string{"add", "remove", "add", "clear"}
	for i, ev := range got {
		if ev["op"] != ops[i] {
			t.Fatalf("message %d is %v, want %s", i, ev["op"], ops[i])
		}
	}
	if n := len(got[0]["strokes"].([]any)); n != 40 {
		t.Fatalf("first add carries %d strokes, want 40", n)
	}
	if n := len(got[1]["ids"].([]any)); n != 2 {
		t.Fatalf("removes merged to %d ids, want 2", n)
	}
	select {
	case m := <-c.send:
		t.Fatalf("unexpected extra message %s", m)
	case <-time.After(2 * boardWindow):
	}
}
