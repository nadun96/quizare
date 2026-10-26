package tutor

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// A live poll inside the session (D-86, TS-FR-68). Polls are the
// platform's (D-40): teachers open, pace and reveal them there, and
// students answer them there, from a panel beside the broadcast. The
// session only records which poll is on, so everyone's page shows it, and
// tells the students when one starts.

var (
	pollCodeRe = regexp.MustCompile(`^[A-Z0-9]{4,12}$`)
	uuidRe     = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)
)

// PollInput is the poll to show (empty PollID: none).
type PollInput struct {
	PollID string `json:"poll_id"`
	Code   string `json:"code"`
	Title  string `json:"title"`
}

// ShowPoll shows one of the teacher's polls in the session, or hides it
// (the teachers'). The platform still decides who may answer it.
func (s *Service) ShowPoll(ctx context.Context, p authn.Person, id string, in PollInput) error {
	sess, err := s.staffed(ctx, p, id)
	if err != nil {
		return err
	}
	if sess.Status != "open" && sess.Status != "live" {
		return web.Conflict("the session has ended")
	}
	var pollID, code, title any
	if in.PollID != "" {
		in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
		in.Title = strings.TrimSpace(in.Title)
		f := map[string]string{}
		if !uuidRe.MatchString(in.PollID) {
			f["poll_id"] = "choose one of your polls"
		}
		if !pollCodeRe.MatchString(in.Code) {
			f["code"] = "the poll's join code"
		}
		if n := utf8.RuneCountInString(in.Title); n < 1 || n > 200 {
			f["title"] = "the poll's title"
		}
		if len(f) > 0 {
			return web.Invalid(f)
		}
		pollID, code, title = in.PollID, in.Code, in.Title
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET poll_id=$2, poll_code=$3, poll_title=$4 WHERE id=$1`, id, pollID, code, title); err != nil {
		return err
	}
	s.logAction(ctx, id, p.ID, "poll_shown", nil, in)
	if in.PollID != "" {
		s.hub.broadcast(id, event{Type: "poll_shown", Data: map[string]string{"title": in.Title}})
	}
	s.hub.changed(id)
	return nil
}
