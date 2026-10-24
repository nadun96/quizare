package presentation

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// The live hub (D-77): who is in each live presentation, and which slide
// each is on. Students report their state (the slide they're on, following
// the teacher or looking back, the tab visible or not) when it changes and
// every 15 s; the server counts their time from those reports, so live time
// needs no separate time reports. Presenters get a summary at most once a
// second (PR-FR-12). Everything is in memory; the slide is in the database,
// so a restart only makes browsers reconnect.

const (
	readTimeout  = 45 * time.Second // clients ping every ~10 s
	writeTimeout = 10 * time.Second
	sendQueue    = 32
	maxInterval  = 60 * time.Second // one report counts at most this much time
)

type member struct {
	userID    string
	name      string
	viewerID  string // "" for presenters
	presenter bool
	send      chan []byte
	cancel    context.CancelFunc

	n         int // the slide they're on
	following bool
	visible   bool
	since     time.Time // when that state began
}

func (m *member) push(b []byte) {
	select {
	case m.send <- b:
	default:
		m.cancel() // too slow to keep up: it reconnects and gets the full state
	}
}

type liveRoom struct {
	live    Live
	members map[*member]bool
	dirty   bool // the audience changed since presenters last heard
}

type liveHub struct {
	s     *Service
	log   *slog.Logger
	mu    sync.Mutex
	rooms map[string]*liveRoom // by presentation id
	now   func() time.Time
}

func newLiveHub(s *Service) *liveHub {
	h := &liveHub{s: s, rooms: map[string]*liveRoom{}, now: time.Now, log: slog.Default()}
	go h.tick()
	return h
}

func msg(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// spent is time on one slide to add to the record.
type spent struct {
	viewerID string
	n        int
	d        time.Duration
}

// close ends a member's current interval, returning the time to record.
func (h *liveHub) close(m *member, now time.Time) []spent {
	if m.presenter || m.n < 1 || !m.visible {
		m.since = now
		return nil
	}
	d := min(now.Sub(m.since), maxInterval)
	m.since = now
	if d < time.Second {
		return nil
	}
	return []spent{{m.viewerID, m.n, d}}
}

// record adds live time to the students' records.
func (h *liveHub) record(ctx context.Context, xs []spent) {
	for _, x := range xs {
		secs := int(x.d.Round(time.Second) / time.Second)
		if _, err := h.s.pool.Exec(ctx, `INSERT INTO presentation.views(viewer_id, n, mode, seconds) VALUES ($1,$2,'live',$3)
			ON CONFLICT (viewer_id, n, mode) DO UPDATE SET seconds = presentation.views.seconds + $3, last_at = now()`, x.viewerID, x.n, secs); err != nil {
			h.log.Warn("recording live time", "err", err)
		}
	}
}

func (h *liveHub) join(l Live, m *member) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[l.PresentationID]
	if r == nil {
		r = &liveRoom{live: l, members: map[*member]bool{}}
		h.rooms[l.PresentationID] = r
	}
	r.live = l
	m.since = h.now()
	r.members[m] = true
	r.dirty = true
	m.push(msg(map[string]any{"type": "live", "live": r.live}))
}

func (h *liveHub) leave(presentationID string, m *member) {
	h.mu.Lock()
	var xs []spent
	if r := h.rooms[presentationID]; r != nil && r.members[m] {
		xs = h.close(m, h.now())
		delete(r.members, m)
		r.dirty = true
		if len(r.members) == 0 {
			delete(h.rooms, presentationID)
		}
	}
	h.mu.Unlock()
	h.record(context.Background(), xs)
}

// update tells everyone in the room about a change; followers move with
// the teacher's slide.
func (h *liveHub) update(l Live) {
	h.mu.Lock()
	var xs []spent
	r := h.rooms[l.PresentationID]
	if r != nil {
		r.live = l
		now := h.now()
		b := msg(map[string]any{"type": "live", "live": l})
		for m := range r.members {
			if m.following && m.n != l.Slide {
				xs = append(xs, h.close(m, now)...)
				m.n = l.Slide
			}
			m.push(b)
		}
		r.dirty = true
	}
	h.mu.Unlock()
	h.record(context.Background(), xs)
}

// end tells everyone it's over, records their time and closes their connections.
func (h *liveHub) end(presentationID string) {
	h.mu.Lock()
	r := h.rooms[presentationID]
	delete(h.rooms, presentationID)
	var xs []spent
	if r != nil {
		now := h.now()
		for m := range r.members {
			xs = append(xs, h.close(m, now)...)
			m.n = 0 // nothing more to record when it leaves
			m.push(msg(map[string]string{"type": "ended"}))
			go func(c context.CancelFunc) { time.Sleep(200 * time.Millisecond); c() }(m.cancel)
		}
	}
	h.mu.Unlock()
	h.record(context.Background(), xs)
}

// state is a student's report: the slide they're on, and whether they're
// following the teacher and can see the page.
func (h *liveHub) state(presentationID string, m *member, n int, following, visible bool) {
	h.mu.Lock()
	var xs []spent
	if r := h.rooms[presentationID]; r != nil && r.members[m] && !m.presenter {
		if following {
			n = r.live.Slide
		}
		if n >= 1 && n <= r.live.MaxShown && (following || r.live.AllowBrowse || n == r.live.Slide) {
			xs = h.close(m, h.now())
			if m.n != n || m.following != following || m.visible != visible {
				r.dirty = true
			}
			m.n, m.following, m.visible = n, following, visible
		}
	}
	h.mu.Unlock()
	h.record(context.Background(), xs)
}

// Person is one student in the audience, for the presenter.
type Person struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Slide     int    `json:"slide"`
	Following bool   `json:"following"`
	Visible   bool   `json:"visible"`
}

// tick sends presenters the audience when it changed, at most once a second.
func (h *liveHub) tick() {
	for range time.Tick(time.Second) {
		h.mu.Lock()
		for _, r := range h.rooms {
			if !r.dirty {
				continue
			}
			r.dirty = false
			people := []Person{}
			counts := map[string]int{"joined": 0, "following": 0, "looking_back": 0, "away": 0}
			seen := map[string]bool{}
			for m := range r.members {
				if m.presenter || seen[m.userID] {
					continue
				}
				seen[m.userID] = true
				people = append(people, Person{UserID: m.userID, Name: m.name, Slide: m.n, Following: m.following, Visible: m.visible})
				counts["joined"]++
				switch {
				case !m.visible:
					counts["away"]++
				case m.following:
					counts["following"]++
				default:
					counts["looking_back"]++
				}
			}
			sort.Slice(people, func(i, j int) bool { return people[i].Name < people[j].Name })
			b := msg(map[string]any{"type": "audience", "people": people, "counts": counts})
			for m := range r.members {
				if m.presenter {
					m.push(b)
				}
			}
		}
		h.mu.Unlock()
	}
}

type inbound struct {
	Type      string `json:"type"`
	T         int64  `json:"t"`
	Slide     int    `json:"slide"`
	Following bool   `json:"following"`
	Visible   bool   `json:"visible"`
}

// WSRoutes mounts the live socket under /ws (auth middleware first). The
// teacher who owns the presentation joins as a presenter; students in its
// classroom join while it's live.
func (s *Service) WSRoutes(r chi.Router) {
	r.Get("/presentations/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.CurrentUser(r.Context())
		if !ok {
			httpx.WriteError(w, r, httpx.ErrUnauthorized)
			return
		}
		id := chi.URLParam(r, "id")
		me := &member{userID: u.ID, name: u.Name, send: make(chan []byte, sendQueue)}
		var l Live
		if _, err := s.owned(r.Context(), u.ID, id); err == nil {
			cur, err := s.openLive(r.Context(), id)
			if err != nil || cur == nil {
				httpx.WriteError(w, r, errNoLive)
				return
			}
			l, me.presenter = *cur, true
		} else {
			p, cur, err := s.access(r.Context(), u.ID, id)
			if err == nil && cur == nil {
				err = errNoLive
			}
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			v, err := s.viewer(r.Context(), p, u.ID, true)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			l, me.viewerID = *cur, v.id
			me.n, me.following, me.visible = cur.Slide, true, true
		}
		base, _ := url.Parse(s.baseURL)
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{base.Host}})
		if err != nil {
			return
		}
		conn.SetReadLimit(4096)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		me.cancel = cancel
		s.hub.join(l, me)
		defer func() {
			s.hub.leave(id, me)
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
			if json.Unmarshal(data, &in) != nil {
				continue
			}
			switch in.Type {
			case "ping":
				me.push(msg(map[string]any{"type": "pong", "t": in.T, "server_time": time.Now().UnixMilli()}))
			case "state":
				s.hub.state(id, me, in.Slide, in.Following, in.Visible)
			}
		}
	})
}
