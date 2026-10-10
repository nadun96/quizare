package tutor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
)

// The hub keeps one live connection per person and session (a second tab
// replaces the first, TS-FR-02), pushes each person their view of the
// session when it changes, delivers chat, and records attendance visits
// while the session is live (TS-FR-07). Actions go through the REST API;
// the socket only receives. Chat keeps working when the media server is
// down, because it doesn't go through it (TS-NFR-13).

type event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type client struct {
	userID string
	role   string

	mu      sync.Mutex // guards state, visit and expires
	state   string
	visit   int64 // open attendance row, 0 = none
	expires time.Time

	out    chan []byte
	closed chan struct{}
	once   sync.Once
	reason string
}

func (c *client) close(reason string) {
	c.once.Do(func() {
		c.reason = reason
		close(c.closed)
	})
}

// push queues a message; a client that can't keep up is disconnected and
// reconnects, rather than slowing everyone down.
func (c *client) push(b []byte) {
	select {
	case c.out <- b:
	default:
		c.close("slow")
	}
}

type room struct {
	clients map[string]*client
	dirty   bool
	timer   *time.Timer
}

type hub struct {
	s     *Service
	mu    sync.Mutex
	rooms map[string]*room
}

func newHub(s *Service) *hub { return &hub{s: s, rooms: map[string]*room{}} }

const flushDelay = 200 * time.Millisecond

func (h *hub) room(id string) *room {
	r := h.rooms[id]
	if r == nil {
		r = &room{clients: map[string]*client{}}
		h.rooms[id] = r
	}
	return r
}

func (h *hub) online(id string) map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]bool{}
	if r := h.rooms[id]; r != nil {
		for u := range r.clients {
			out[u] = true
		}
	}
	return out
}

// changed schedules a state push to everyone in the session, at most every 200 ms.
func (h *hub) changed(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[id]
	if r == nil || r.dirty {
		return
	}
	r.dirty = true
	r.timer = time.AfterFunc(flushDelay, func() { h.flush(id) })
}

// Flush pushes the current state now (tests).
func (s *Service) Flush(id string) { s.hub.flush(id) }

func (h *hub) flush(id string) {
	h.mu.Lock()
	r := h.rooms[id]
	if r == nil {
		h.mu.Unlock()
		return
	}
	r.dirty = false
	clients := make([]*client, 0, len(r.clients))
	for _, c := range r.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := h.s.snapshot(ctx, id)
	if err != nil {
		h.s.log.Warn("tutoring state", "session", id, "err", err)
		return
	}
	for _, c := range clients {
		v, ok := snap.viewFor(c.userID, h.s.mediaURL)
		if !ok {
			continue
		}
		c.mu.Lock()
		open := c.state != "admitted" && v.Me.State == "admitted" && snap.sess.Status == "live" && c.visit == 0
		c.state = v.Me.State
		c.mu.Unlock()
		if open {
			visit := h.s.openVisit(ctx, id, c.userID)
			c.mu.Lock()
			c.visit = visit
			c.mu.Unlock()
		}
		b, _ := json.Marshal(event{Type: "state", Data: v})
		c.push(b)
	}
}

func (h *hub) each(id string, f func(*client)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[id]; r != nil {
		for _, c := range r.clients {
			f(c)
		}
	}
}

func (c *client) admitted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state == "admitted"
}

func (h *hub) chat(id string, m Message) {
	b, _ := json.Marshal(event{Type: "chat", Data: m})
	h.each(id, func(c *client) {
		if c.admitted() && m.visibleTo(c.userID, c.role) {
			c.push(b)
		}
	})
}

func (h *hub) broadcast(id string, e event) {
	b, _ := json.Marshal(e)
	h.each(id, func(c *client) {
		if c.admitted() {
			c.push(b)
		}
	})
}

func (h *hub) send(id, userID string, e event) {
	b, _ := json.Marshal(e)
	h.each(id, func(c *client) {
		if c.userID == userID {
			c.push(b)
		}
	})
}

// kick tells one person why and disconnects them.
func (h *hub) kick(id, userID, reason string) {
	b, _ := json.Marshal(event{Type: "closed", Data: map[string]string{"reason": reason}})
	h.each(id, func(c *client) {
		if c.userID == userID {
			c.push(b)
			c.close(reason)
		}
	})
}

// end tells everyone the session ended and disconnects them.
func (h *hub) end(id string) {
	b, _ := json.Marshal(event{Type: "closed", Data: map[string]string{"reason": "ended"}})
	h.each(id, func(c *client) {
		c.mu.Lock()
		c.visit = 0 // End closed every visit already
		c.mu.Unlock()
		c.push(b)
		c.close("ended")
	})
}

// startVisits opens attendance for everyone admitted and connected when the session goes live.
func (h *hub) startVisits(ctx context.Context, id string) {
	var todo []*client
	h.each(id, func(c *client) {
		c.mu.Lock()
		if c.state == "admitted" && c.visit == 0 {
			todo = append(todo, c)
		}
		c.mu.Unlock()
	})
	for _, c := range todo {
		v := h.s.openVisit(ctx, id, c.userID)
		c.mu.Lock()
		c.visit = v
		c.mu.Unlock()
	}
}

func (s *Service) openVisit(ctx context.Context, sessionID, userID string) int64 {
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO tutoring.visits (session_id, user_id, joined_at) VALUES ($1, $2, $3) RETURNING id`, sessionID, userID, s.now()).Scan(&id); err != nil {
		s.log.Warn("attendance", "err", err)
	}
	return id
}

func (s *Service) closeVisit(sessionID string, id int64) {
	if id == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.visits SET left_at=$2 WHERE id=$1 AND left_at IS NULL`, id, s.now()); err != nil {
		s.log.Warn("attendance", "session", sessionID, "err", err)
	}
}

// snap is a session's state, loaded once per push for everyone.
type snap struct {
	sess   Session
	all    []Participant
	pinned *Message
}

func (s *Service) snapshot(ctx context.Context, id string) (snap, error) {
	var sn snap
	var err error
	if sn.sess, err = s.session(ctx, id); err != nil {
		return sn, err
	}
	online := s.hub.online(id)
	rows, _ := s.pool.Query(ctx, `SELECT `+participantCols+` FROM tutoring.participants WHERE session_id=$1
		ORDER BY CASE role WHEN 'teacher' THEN 0 WHEN 'coteacher' THEN 1 ELSE 2 END, hand_at NULLS LAST, lower(name)`, id)
	if sn.all, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Participant, error) { return scanParticipant(r) }); err != nil {
		return sn, err
	}
	for i := range sn.all {
		sn.all[i].Online = online[sn.all[i].UserID]
	}
	if sn.sess.PinnedID != nil {
		if m, err := s.message(ctx, *sn.sess.PinnedID); err == nil && m.DeletedAt == nil {
			sn.pinned = &m
		}
	}
	return sn, nil
}

func (sn snap) viewFor(userID, mediaURL string) (View, bool) {
	v := View{Session: sn.sess, Pinned: sn.pinned, MediaURL: mediaURL}
	found := false
	for _, p := range sn.all {
		if p.UserID == userID {
			v.Me, found = p, true
		}
		if p.Online && p.State == "admitted" {
			v.Online++
		}
		if p.State == "waiting" {
			v.Waiting++
		}
	}
	if !found {
		return v, false
	}
	// The lead teacher always, whether or not they have joined yet; then co-teachers who joined.
	v.Teachers = []Person{{ID: sn.sess.TeacherID, Name: sn.sess.TeacherName, Role: "teacher"}}
	for _, p := range sn.all {
		if p.Role == "coteacher" && p.State == "admitted" {
			v.Teachers = append(v.Teachers, Person{ID: p.UserID, Name: p.Name, Role: p.Role})
		}
	}
	if v.Me.Staff() {
		v.Participants = sn.all
	} else {
		v.Session.JoinCode = "" // teachers share the code
		if o := sn.sess.BroadcastOffer; o == nil || *o != userID {
			v.Session.BroadcastOffer = nil // only the person asked sees an offer
		}
	}
	if v.Me.State != "admitted" {
		v.Pinned = nil
	}
	return v, true
}

// ---------- the socket ----------

// ServeWS is GET /ws/sessions/{id}. The first message carries the
// platform's token ({"token": "…"}), since browsers can't set headers on a
// WebSocket; the page sends a fresh one every few minutes, and a socket
// whose token runs out is closed, so logging out of the platform ends
// tutoring too (TS-NFR-52).
func (s *Service) ServeWS(keys authn.Keys, origins []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: origins})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(16 << 10)
		ctx := r.Context()
		p, err := readToken(ctx, conn, keys)
		if err != nil {
			conn.Close(websocket.StatusPolicyViolation, "sign in again")
			return
		}
		me, err := s.participant(ctx, id, p.ID)
		if err != nil || me.State == "removed" || me.State == "refused" {
			conn.Close(websocket.StatusPolicyViolation, "not in this session")
			return
		}
		c := &client{userID: p.ID, role: me.Role, expires: p.Expires, out: make(chan []byte, 64), closed: make(chan struct{})}
		s.hub.register(id, c)
		defer s.hub.unregister(id, c)
		s.hub.changed(id)
		s.hub.flush(id) // this person's view at once; the others' within 200 ms

		go func() { // receive token refreshes; anything else is ignored
			for {
				p, err := readToken(ctx, conn, keys)
				if err != nil {
					if errors.Is(err, errBadToken) {
						c.close("expired")
					} else {
						c.close("gone")
					}
					return
				}
				if p.ID == c.userID {
					c.mu.Lock()
					c.expires = p.Expires
					c.mu.Unlock()
				}
			}
		}()
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case b := <-c.out:
				wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(wctx, websocket.MessageText, b)
				cancel()
				if err != nil {
					return
				}
			case <-tick.C:
				c.mu.Lock()
				expired := time.Now().After(c.expires.Add(30 * time.Second))
				c.mu.Unlock()
				if expired {
					c.close("expired")
				}
			case <-c.closed:
				// Send what was queued for them (why they were closed), then go.
				for {
					select {
					case b := <-c.out:
						wctx, cancel := context.WithTimeout(ctx, time.Second)
						_ = conn.Write(wctx, websocket.MessageText, b)
						cancel()
						continue
					default:
					}
					break
				}
				conn.Close(websocket.StatusNormalClosure, c.reason)
				return
			case <-ctx.Done():
				return
			}
		}
	}
}

var errBadToken = errors.New("bad token")

func readToken(ctx context.Context, conn *websocket.Conn, keys authn.Keys) (authn.Person, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return authn.Person{}, err
	}
	var m struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(data, &m) != nil || m.Token == "" {
		return authn.Person{}, errBadToken
	}
	p, err := keys.Verify(m.Token)
	if err != nil {
		return p, errBadToken
	}
	return p, nil
}

func (h *hub) register(id string, c *client) {
	h.mu.Lock()
	r := h.room(id)
	old := r.clients[c.userID]
	r.clients[c.userID] = c
	h.mu.Unlock()
	if old != nil { // one connection per person: the newest wins
		b, _ := json.Marshal(event{Type: "closed", Data: map[string]string{"reason": "replaced"}})
		old.push(b)
		old.close("replaced")
		old.mu.Lock()
		visit := old.visit
		old.visit = 0
		old.mu.Unlock()
		h.s.closeVisit(id, visit)
	}
}

func (h *hub) unregister(id string, c *client) {
	h.mu.Lock()
	r := h.rooms[id]
	if r != nil && r.clients[c.userID] == c {
		delete(r.clients, c.userID)
		if len(r.clients) == 0 && !r.dirty {
			delete(h.rooms, id)
		}
	}
	h.mu.Unlock()
	c.close("gone")
	c.mu.Lock()
	visit := c.visit
	c.visit = 0
	c.mu.Unlock()
	h.s.closeVisit(id, visit)
	h.changed(id)
}
