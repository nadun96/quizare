package tutor

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Quick reactions (TS-FR-26): a student taps thumbs up, clap, "slower
// please" or "I'm confused", and the teachers see how many people did each
// in the last minute. No permission is needed and no media is used; they
// live in memory only, and a person counts once per kind however often they
// tap.

var reactionKinds = map[string]bool{"thumbs_up": true, "clap": true, "slower": true, "confused": true}

const (
	reactionWindow = time.Minute
	reactionRepeat = 2 * time.Second // taps closer together count once
)

// Reactions are the counts of the last minute, by kind.
type Reactions struct {
	Counts    map[string]int `json:"counts"`
	WindowSec int            `json:"window_sec"`
}

// React records a student's reaction and tells the teachers.
func (s *Service) React(ctx context.Context, p authn.Person, id, kind string) error {
	if !reactionKinds[kind] {
		return web.Invalid(map[string]string{"kind": "thumbs_up, clap, slower or confused"})
	}
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return err
	}
	if me.Role != "student" || me.State != "admitted" {
		return web.ErrForbidden
	}
	now := s.now()
	s.reactMu.Lock()
	byPerson := s.reacts[id]
	if byPerson == nil {
		byPerson = map[string]map[string]time.Time{}
		s.reacts[id] = byPerson
	}
	mine := byPerson[p.ID]
	if mine == nil {
		mine = map[string]time.Time{}
		byPerson[p.ID] = mine
	}
	repeat := now.Sub(mine[kind]) < reactionRepeat
	mine[kind] = now
	s.reactMu.Unlock()
	if repeat {
		return nil
	}
	s.sendReactions(id)
	// When this one leaves the minute, the counts drop.
	time.AfterFunc(reactionWindow+time.Second, func() { s.sendReactions(id) })
	return nil
}

// reactionCounts counts the last minute's reactions, forgetting older ones.
func (s *Service) reactionCounts(id string) Reactions {
	since := s.now().Add(-reactionWindow)
	out := Reactions{Counts: map[string]int{}, WindowSec: int(reactionWindow / time.Second)}
	for k := range reactionKinds {
		out.Counts[k] = 0
	}
	s.reactMu.Lock()
	defer s.reactMu.Unlock()
	for person, kinds := range s.reacts[id] {
		for k, at := range kinds {
			if at.Before(since) {
				delete(kinds, k)
				continue
			}
			out.Counts[k]++
		}
		if len(kinds) == 0 {
			delete(s.reacts[id], person)
		}
	}
	if len(s.reacts[id]) == 0 {
		delete(s.reacts, id)
	}
	return out
}

// sendReactions gives the teachers in the session the current counts.
func (s *Service) sendReactions(id string) {
	b, _ := json.Marshal(event{Type: "reactions", Data: s.reactionCounts(id)})
	s.hub.each(id, func(c *client) {
		if c.admitted() && (c.role == "teacher" || c.role == "coteacher") {
			c.push(b)
		}
	})
}

// SessionReactions gives a session's teachers the counts, for a page that opens.
func (s *Service) SessionReactions(ctx context.Context, p authn.Person, id string) (Reactions, error) {
	if _, err := s.staffed(ctx, p, id); err != nil {
		return Reactions{}, err
	}
	return s.reactionCounts(id), nil
}

func (s *Service) hReact(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Kind string `json:"kind"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.React(r.Context(), me(r), id(r), in.Kind))
}

func (s *Service) hReactions(w http.ResponseWriter, r *http.Request) error {
	v, err := s.SessionReactions(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}
