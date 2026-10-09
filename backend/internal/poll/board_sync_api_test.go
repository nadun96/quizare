package poll_test

import (
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/poll"
)

// Erasing, batching and clearing keep every screen in step (D-52).
func TestWhiteboardEraseWholeLinesAndClearWatermark(t *testing.T) {
	f := setup(t, settings("anonymous", "self", "live"), singleQ)
	ann, ben := f.join(t, "Ann"), f.join(t, "Ben")
	f.access(t, map[string]any{"open": true, "mode": "everyone"})
	erase := func(p *player, ids ...int64) []int64 {
		var out struct{ Removed []int64 }
		p.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/board/erase", map[string]any{"ids": ids}, 200, &out)
		sort.Slice(out.Removed, func(i, j int) bool { return out.Removed[i] < out.Removed[j] })
		return out.Removed
	}
	ids := func(ss []poll.Stroke) (out []int64) {
		for _, s := range ss {
			out = append(out, s.ID)
		}
		return out
	}

	// A pen line arrives in pieces, each its own request, all one gesture.
	var line []poll.Stroke
	for i := range 3 {
		x := float64(10 + 10*i)
		line = append(line, ann.draw(201, pen("g1", x, 10, x+10, 10))...)
	}
	// Ben's browser happens to pick the same gesture id.
	bens := ben.draw(201, pen("g1", 500, 500, 510, 510))
	other := ann.draw(201, pen("g2", 300, 300, 310, 310))

	// Erasing the middle piece takes the whole line, and nothing of anyone else's.
	if got, want := erase(ann, line[1].ID), ids(line); !slices.Equal(got, want) {
		t.Fatalf("erase one piece removed %v, want the whole line %v", got, want)
	}
	if v := ann.board(); len(v.Strokes) != 2 {
		t.Fatalf("left on the board: %+v", v.Strokes)
	}
	// Participants still can't erase other people's strokes.
	if got := erase(ann, bens[0].ID); len(got) != 0 {
		t.Fatalf("Ann erased Ben's stroke: %v", got)
	}
	// The teacher can, and also takes the whole gesture.
	var out struct{ Removed []int64 }
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/board/erase", map[string]any{"ids": []int64{bens[0].ID}}, 200, &out)
	if !slices.Equal(out.Removed, ids(bens)) {
		t.Fatalf("teacher erase: %v", out.Removed)
	}

	// A batch is saved in order, in one go.
	batch := ann.draw(201, pen("g3", 1, 1, 2, 2), pen("g3", 2, 2, 3, 3), pen("g3", 3, 3, 4, 4))
	if len(batch) != 3 || batch[0].ID >= batch[1].ID || batch[1].ID >= batch[2].ID || batch[2].Points[0] != 3 {
		t.Fatalf("batch: %+v", batch)
	}

	// Clearing tells screens the highest id it removed, so a late add event
	// for a cleared stroke can be ignored.
	audience := ben.c.Dial("/ws/polls/" + f.poll.JoinCode)
	apptest.ReadUntil(t, audience, "update", 3*time.Second)
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/board/clear", nil, 204, nil)
	for {
		m := apptest.ReadUntil(t, audience, "board", 2*time.Second)
		if m["op"] != "clear" {
			continue
		}
		if upto, _ := m["upto"].(float64); int64(upto) != batch[2].ID {
			t.Fatalf("clear upto = %v, want %d (other: %d)", m["upto"], batch[2].ID, other[0].ID)
		}
		break
	}
	// After a clear the board takes new strokes again.
	if got := ann.draw(201, pen("g4", 5, 5, 6, 6)); got[0].ID <= batch[2].ID {
		t.Fatalf("new stroke id %d", got[0].ID)
	}
}

// Screens loading together share one read, but a load never gets a read that
// started before it, so a stroke saved before the load is always in it (D-52).
func TestWhiteboardSharedLoadsNeverMissEarlierStrokes(t *testing.T) {
	f := setup(t, settings("anonymous", "self", "live"), singleQ)
	ann := f.join(t, "Ann")
	f.access(t, map[string]any{"open": true, "mode": "everyone"})
	for range 2 {
		var batch []map[string]any
		for i := range 20 {
			batch = append(batch, pen("bg", float64(i), 1, float64(i)+1, 2))
		}
		ann.draw(201, batch...)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var viewers []*player
	for i := range 8 {
		viewers = append(viewers, f.join(t, "V"+string(rune('A'+i))))
	}
	for _, v := range viewers {
		go func() {
			defer func() { done <- struct{}{} }()
			for {
				select {
				case <-stop:
					return
				default:
					v.c.Do("GET", "/api/polls/"+f.poll.JoinCode+"/board", nil)
				}
			}
		}()
	}
	for i := range 15 {
		s := ann.draw(201, pen("fg", 100, float64(10+i), 110, float64(10+i)))
		found := false
		for _, x := range ann.board().Strokes {
			found = found || x.ID == s[0].ID
		}
		if !found {
			close(stop)
			t.Fatalf("load after saving stroke %d didn't include it", s[0].ID)
		}
	}
	close(stop)
	for range viewers {
		<-done
	}
	if v := ann.board(); len(v.Strokes) != 55 || v.Me == "" || !v.CanDraw {
		t.Fatalf("final view: %d strokes, me %q, can draw %v", len(v.Strokes), v.Me, v.CanDraw)
	}
}
