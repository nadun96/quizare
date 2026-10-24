package meet

import (
	"fmt"
	"slices"

	"github.com/nadun96/quizplatform/internal/platform/whiteboard"
)

// The meeting whiteboard (D-72): the poll whiteboard's strokes and tools,
// over the meeting's own WebSocket. It lives in memory with the meeting: a
// call is short and small, and the strokes are sent to everyone anyway.
// The host shows or hides it for everyone and decides who draws.

const (
	maxBoardStrokes = 20000
	maxPerRequest   = 20
	ModeHost        = "host"
	ModeEveryone    = "everyone"
)

type board struct {
	open    bool
	mode    string
	strokes []whiteboard.Stroke
	next    int64
}

// boardOf returns a meeting's board, making it on first use (h.mu held).
func (h *hub) boardOf(meetingID string) *board {
	b := h.boards[meetingID]
	if b == nil {
		b = &board{mode: ModeHost}
		h.boards[meetingID] = b
	}
	return b
}

func (b *board) mayDraw(p *peer) bool { return p.Host || (b.open && b.mode == ModeEveryone) }

// boardView is what a person joining sees.
func (h *hub) boardView(meetingID string, p *peer) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	b := h.boardOf(meetingID)
	return map[string]any{"type": "board_state", "open": b.open, "mode": b.mode, "can_draw": b.mayDraw(p), "me": p.UserID,
		"strokes": slices.Clone(b.strokes)}
}

// boardMessage handles a whiteboard request: it answers the sender with
// board_ack (the saved strokes, the removed ids, or an error) and tells
// everyone else.
func (h *hub) boardMessage(meetingID string, me *peer, in inbound) {
	ack := map[string]any{"type": "board_ack", "req": in.Req}
	fail := func(why string) {
		ack["error"] = why
		me.push(msg(ack))
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[meetingID]
	if r == nil || r.peers[me.ID] == nil {
		return
	}
	b := h.boardOf(meetingID)
	switch in.Type {
	case "board_access":
		if !me.Host {
			return
		}
		if in.Mode != ModeHost && in.Mode != ModeEveryone {
			in.Mode = b.mode
		}
		b.open, b.mode = in.Open, in.Mode
		// Each person's drawing rights depend on who they are.
		for _, p := range r.peers {
			p.push(msg(map[string]any{"type": "board_access", "open": b.open, "mode": b.mode, "can_draw": b.mayDraw(p)}))
		}
		return
	case "board_add":
		if !b.mayDraw(me) {
			fail("the host hasn't let everyone draw")
			return
		}
		if len(in.Strokes) == 0 || len(in.Strokes) > maxPerRequest {
			fail(fmt.Sprintf("send 1-%d strokes", maxPerRequest))
			return
		}
		if len(b.strokes)+len(in.Strokes) > maxBoardStrokes {
			fail("the board is full; the host can clear it")
			return
		}
		for i := range in.Strokes {
			if f := in.Strokes[i].Clean(); len(f) > 0 {
				fail("that mark can't be drawn")
				return
			}
		}
		for i := range in.Strokes {
			b.next++
			in.Strokes[i].ID, in.Strokes[i].By = b.next, me.UserID
		}
		b.strokes = append(b.strokes, in.Strokes...)
		ack["strokes"] = in.Strokes
		h.broadcast(r, me.ID, msg(map[string]any{"type": "board", "op": "add", "strokes": in.Strokes}))
	case "board_erase":
		// The host erases anything; others only their own marks. A gesture
		// (undo) takes every piece of one of their own lines.
		if !b.mayDraw(me) {
			fail("the host hasn't let everyone draw")
			return
		}
		if in.Gesture != "" && !whiteboard.GestureOK(in.Gesture) {
			fail("invalid gesture id")
			return
		}
		want := map[int64]bool{}
		for _, id := range in.IDs {
			want[id] = true
		}
		removed := []int64{}
		b.strokes = slices.DeleteFunc(b.strokes, func(s whiteboard.Stroke) bool {
			own := s.By == me.UserID
			gone := (want[s.ID] && (me.Host || own)) || (in.Gesture != "" && own && s.Gesture == in.Gesture)
			if gone {
				removed = append(removed, s.ID)
			}
			return gone
		})
		ack["removed"] = removed
		if len(removed) > 0 {
			h.broadcast(r, me.ID, msg(map[string]any{"type": "board", "op": "remove", "ids": removed}))
		}
	case "board_clear":
		if !me.Host {
			fail("only the host clears the board")
			return
		}
		b.strokes = nil
		ack["upto"] = b.next
		h.broadcast(r, me.ID, msg(map[string]any{"type": "board", "op": "clear", "upto": b.next}))
	}
	me.push(msg(ack))
}
