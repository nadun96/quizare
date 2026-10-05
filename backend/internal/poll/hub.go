package poll

import (
	"context"
	"encoding/json"
	"sync"
)

// Hub pushes poll state and live results (D-40), like the live-session hub
// (ADR-05): sockets and caches live in memory, answers in Postgres. Results
// are recomputed only for questions that changed, at most twice a second.
type Hub struct {
	svc        *Service
	mu         sync.Mutex
	presenters map[string]map[*client]struct{} // poll id → teacher sockets
	audience   map[string]map[*client]struct{} // poll id → participant sockets
	dirty      map[string]map[string]bool      // poll id → question ids ("*" = all)
	cache      map[string]Result               // "pollID/questionID/t|p" → last result
}

type client struct {
	send   chan []byte
	cancel context.CancelFunc
}

func newClient(cancel context.CancelFunc) *client {
	return &client{send: make(chan []byte, 32), cancel: cancel}
}

func (c *client) push(msg []byte) {
	select {
	case c.send <- msg:
	default:
		c.cancel() // slow consumer
	}
}

func newHub(s *Service) *Hub {
	return &Hub{svc: s, presenters: map[string]map[*client]struct{}{}, audience: map[string]map[*client]struct{}{},
		dirty: map[string]map[string]bool{}, cache: map[string]Result{}}
}

func add(m map[string]map[*client]struct{}, key string, c *client) {
	if m[key] == nil {
		m[key] = map[*client]struct{}{}
	}
	m[key][c] = struct{}{}
}

func remove(m map[string]map[*client]struct{}, key string, c *client) {
	delete(m[key], c)
	if len(m[key]) == 0 {
		delete(m, key)
	}
}

func (h *Hub) addPresenter(pollID string, c *client) {
	h.mu.Lock()
	add(h.presenters, pollID, c)
	h.mu.Unlock()
}

func (h *Hub) removePresenter(pollID string, c *client) {
	h.mu.Lock()
	remove(h.presenters, pollID, c)
	h.mu.Unlock()
}

func (h *Hub) addAudience(pollID string, c *client) {
	h.mu.Lock()
	add(h.audience, pollID, c)
	h.mu.Unlock()
}

func (h *Hub) removeAudience(pollID string, c *client) {
	h.mu.Lock()
	remove(h.audience, pollID, c)
	h.mu.Unlock()
}

func cacheKey(pollID, questionID string, teacher bool) string {
	if teacher {
		return pollID + "/" + questionID + "/t"
	}
	return pollID + "/" + questionID + "/p"
}

// markDirty records that answers to a question changed.
func (h *Hub) markDirty(pollID, questionID string) {
	h.mu.Lock()
	if h.dirty[pollID] == nil {
		h.dirty[pollID] = map[string]bool{}
	}
	h.dirty[pollID][questionID] = true
	delete(h.cache, cacheKey(pollID, questionID, true))
	delete(h.cache, cacheKey(pollID, questionID, false))
	h.mu.Unlock()
}

// markAll forces every question (and the participant count) to refresh.
func (h *Hub) markAll(pollID string) {
	h.mu.Lock()
	if h.dirty[pollID] == nil {
		h.dirty[pollID] = map[string]bool{}
	}
	h.dirty[pollID]["*"] = true
	for k := range h.cache {
		if len(k) > len(pollID) && k[:len(pollID)] == pollID {
			delete(h.cache, k)
		}
	}
	h.mu.Unlock()
}

// stateChanged means settings, status or the current question changed.
func (h *Hub) stateChanged(pollID string) { h.markAll(pollID) }

func (h *Hub) closed(pollID string) {
	msg, _ := json.Marshal(map[string]any{"type": "deleted"})
	h.mu.Lock()
	for c := range h.audience[pollID] {
		c.push(msg)
	}
	for c := range h.presenters[pollID] {
		c.push(msg)
	}
	h.mu.Unlock()
}

// result returns a cached or freshly computed result.
func (h *Hub) result(ctx context.Context, pollID string, q Question, teacher bool) (Result, error) {
	key := cacheKey(pollID, q.ID, teacher)
	h.mu.Lock()
	r, ok := h.cache[key]
	h.mu.Unlock()
	if ok {
		return r, nil
	}
	r, err := h.svc.compute(ctx, q, teacher)
	if err != nil {
		return r, err
	}
	h.mu.Lock()
	h.cache[key] = r
	h.mu.Unlock()
	return r, nil
}

// flush pushes fresh results to presenters and a shared update to
// participants for polls that changed since the last tick.
func (h *Hub) flush(ctx context.Context) {
	h.mu.Lock()
	type job struct {
		id         string
		presenters []*client
		audience   []*client
	}
	var jobs []job
	for id := range h.dirty {
		j := job{id: id}
		for c := range h.presenters[id] {
			j.presenters = append(j.presenters, c)
		}
		for c := range h.audience[id] {
			j.audience = append(j.audience, c)
		}
		if len(j.presenters)+len(j.audience) > 0 {
			jobs = append(jobs, j)
		}
		delete(h.dirty, id)
	}
	h.mu.Unlock()
	for _, j := range jobs {
		if len(j.presenters) > 0 {
			p, err := h.svc.pollByID(ctx, j.id)
			if err != nil {
				continue
			}
			res, err := h.svc.teacherResults(ctx, p)
			if err != nil {
				h.svc.log.Warn("poll results", "poll", j.id, "err", err)
				continue
			}
			msg, _ := json.Marshal(res)
			for _, c := range j.presenters {
				c.push(msg)
			}
		}
		// One shared update for every participant socket; each browser keeps
		// its own answers and applies "after answering" to what it shows.
		if len(j.audience) > 0 {
			p, err := h.svc.pollByID(ctx, j.id)
			if err != nil {
				continue
			}
			u, err := h.svc.publicUpdate(ctx, p)
			if err != nil {
				h.svc.log.Warn("poll update", "poll", j.id, "err", err)
				continue
			}
			msg, _ := json.Marshal(u)
			for _, c := range j.audience {
				c.push(msg)
			}
		}
	}
}
