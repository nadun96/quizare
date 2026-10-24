package meet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/whiteboard"
)

// The signalling hub: who is in each meeting, and the set-up messages passed
// between them. Media never comes here.

const (
	readTimeout  = 45 * time.Second // clients ping every ~15 s
	writeTimeout = 10 * time.Second
	maxMessage   = 1 << 20 // an SDP offer fits in 64 KB; a batch of whiteboard strokes needs more
	sendQueue    = 64
)

type peer struct {
	ID     string `json:"peer_id"`
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Host   bool   `json:"host"`
	Mic    bool   `json:"mic"`
	Cam    bool   `json:"cam"`
	Screen bool   `json:"screen"`
	Hand   bool   `json:"hand"`

	send   chan []byte
	cancel context.CancelFunc
}

type room struct {
	max   int
	peers map[string]*peer
}

type hub struct {
	s     *Service
	mu    sync.Mutex
	rooms map[string]*room
	// Whiteboards outlive an empty room: people step out and come back.
	// They go when the meeting ends (or the server restarts).
	boards map[string]*board
}

func newHub(s *Service) *hub {
	return &hub{s: s, rooms: map[string]*room{}, boards: map[string]*board{}}
}

func (h *hub) count(meetingID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[meetingID]; r != nil {
		return len(r.peers)
	}
	return 0
}

func (h *hub) online(meetingID string) map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]bool{}
	if r := h.rooms[meetingID]; r != nil {
		for _, p := range r.peers {
			out[p.UserID] = true
		}
	}
	return out
}

func msg(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// push queues a message; a peer too slow to keep up is dropped, so one
// stalled browser can't hold the others back.
func (p *peer) push(b []byte) {
	select {
	case p.send <- b:
	default:
		p.cancel()
	}
}

func (h *hub) broadcast(r *room, except string, b []byte) {
	for id, p := range r.peers {
		if id != except {
			p.push(b)
		}
	}
}

func newPeerID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// join adds a peer. One connection per person: a second one (another tab)
// replaces the first. A full meeting refuses.
func (h *hub) join(meetingID string, max int, p *peer) ([]peer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[meetingID]
	if r == nil {
		r = &room{max: max, peers: map[string]*peer{}}
		h.rooms[meetingID] = r
	}
	var replaced *peer
	for _, o := range r.peers {
		if o.UserID == p.UserID {
			replaced = o
		}
	}
	if replaced == nil && r.max > 0 && len(r.peers) >= r.max { // 0: no limit
		return nil, errFull
	}
	if replaced != nil {
		replaced.push(msg(map[string]string{"type": "replaced"}))
		delete(r.peers, replaced.ID)
		h.broadcast(r, "", msg(map[string]string{"type": "left", "peer_id": replaced.ID}))
		go func(c context.CancelFunc) { time.Sleep(200 * time.Millisecond); c() }(replaced.cancel)
	}
	others := make([]peer, 0, len(r.peers))
	for _, o := range r.peers {
		others = append(others, *o)
	}
	h.broadcast(r, "", msg(map[string]any{"type": "joined", "peer": p}))
	r.peers[p.ID] = p
	return others, nil
}

func (h *hub) leave(meetingID, peerID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[meetingID]
	if r == nil || r.peers[peerID] == nil {
		return false
	}
	delete(r.peers, peerID)
	h.broadcast(r, "", msg(map[string]string{"type": "left", "peer_id": peerID}))
	if len(r.peers) == 0 {
		delete(h.rooms, meetingID)
	}
	return true
}

// end tells everyone the meeting is over and closes their connections.
func (h *hub) end(meetingID string) {
	h.mu.Lock()
	r := h.rooms[meetingID]
	delete(h.rooms, meetingID)
	delete(h.boards, meetingID)
	h.mu.Unlock()
	if r == nil {
		return
	}
	for _, p := range r.peers {
		p.push(msg(map[string]string{"type": "ended"}))
		go func(c context.CancelFunc) { time.Sleep(200 * time.Millisecond); c() }(p.cancel)
	}
}

type inbound struct {
	Type    string              `json:"type"`
	Req     string              `json:"req"` // a whiteboard request, answered with board_ack
	Strokes []whiteboard.Stroke `json:"strokes"`
	IDs     []int64             `json:"ids"`
	Gesture string              `json:"gesture"`
	Open    bool                `json:"open"`
	Mode    string              `json:"mode"`
	To      string              `json:"to"`
	PeerID  string              `json:"peer_id"`
	Data    json.RawMessage     `json:"data"`
	Mic     bool                `json:"mic"`
	Cam     bool                `json:"cam"`
	Screen  bool                `json:"screen"`
	Hand    bool                `json:"hand"`
}

func (h *hub) handle(ctx context.Context, meetingID string, me *peer, in inbound) {
	switch in.Type {
	case "ping":
		me.push(msg(map[string]any{"type": "pong", "server_time": time.Now().UnixMilli()}))
	case "signal":
		h.mu.Lock()
		if r := h.rooms[meetingID]; r != nil {
			if to := r.peers[in.To]; to != nil && in.To != me.ID {
				to.push(msg(map[string]any{"type": "signal", "from": me.ID, "data": in.Data}))
			}
		}
		h.mu.Unlock()
	case "state":
		h.mu.Lock()
		if r := h.rooms[meetingID]; r != nil && r.peers[me.ID] != nil {
			me.Mic, me.Cam, me.Screen, me.Hand = in.Mic, in.Cam, in.Screen, in.Hand
			h.broadcast(r, me.ID, msg(map[string]any{"type": "state", "peer_id": me.ID, "mic": me.Mic, "cam": me.Cam, "screen": me.Screen, "hand": me.Hand}))
		}
		h.mu.Unlock()
	case "remove":
		if !me.Host {
			return
		}
		h.mu.Lock()
		var target *peer
		if r := h.rooms[meetingID]; r != nil {
			target = r.peers[in.PeerID]
		}
		h.mu.Unlock()
		if target != nil && target.ID != me.ID {
			target.push(msg(map[string]string{"type": "removed"}))
			h.leave(meetingID, target.ID)
			go func(c context.CancelFunc) { time.Sleep(200 * time.Millisecond); c() }(target.cancel)
		}
	case "board_add", "board_erase", "board_clear", "board_access":
		h.boardMessage(meetingID, me, in)
	case "end":
		if me.Host {
			if err := h.s.End(ctx, me.UserID, meetingID); err != nil {
				h.s.log.Warn("ending a meeting", "err", err)
			}
		}
	}
}

// WSRoutes mounts the signalling socket under /ws (auth middleware first).
func (s *Service) WSRoutes(r chi.Router) {
	r.Get("/meet/{code}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.CurrentUser(r.Context())
		if !ok {
			httpx.WriteError(w, r, httpx.ErrUnauthorized)
			return
		}
		acc, err := s.Join(r.Context(), u, chi.URLParam(r, "code"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		base, _ := url.Parse(s.baseURL)
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{base.Host}})
		if err != nil {
			return
		}
		conn.SetReadLimit(maxMessage)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		me := &peer{ID: newPeerID(), UserID: u.ID, Name: u.Name, Host: acc.Host, send: make(chan []byte, sendQueue), cancel: cancel}
		others, err := s.hub.join(acc.Meeting.ID, acc.Meeting.MaxPeople, me)
		if err != nil {
			_ = conn.Close(4009, "the meeting is full")
			return
		}
		s.arrived(ctx, acc.Meeting.ID, u.ID)
		me.push(msg(map[string]any{"type": "welcome", "you": me, "peers": others, "ice_servers": acc.ICE, "meeting": acc.Meeting, "board": s.hub.boardView(acc.Meeting.ID, me)}))
		defer func() {
			if s.hub.leave(acc.Meeting.ID, me.ID) {
				s.left(context.Background(), acc.Meeting.ID, u.ID)
			}
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case b := <-me.send:
					wctx, c := context.WithTimeout(ctx, writeTimeout)
					err := conn.Write(wctx, websocket.MessageText, b)
					c()
					if err != nil {
						cancel()
						return
					}
				}
			}
		}()
		for {
			rctx, c := context.WithTimeout(ctx, readTimeout)
			_, data, err := conn.Read(rctx)
			c()
			if err != nil {
				return
			}
			var in inbound
			if json.Unmarshal(data, &in) == nil {
				s.hub.handle(ctx, acc.Meeting.ID, me, in)
			}
		}
	})
}
