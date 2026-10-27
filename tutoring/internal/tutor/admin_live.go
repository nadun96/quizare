package tutor

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/platform"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// LiveSession is a tutoring session open or live now, for the admin's Live
// activity tab (D-90): it lists them beside the platform's activities.
type LiveSession struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	TeacherID     string    `json:"teacher_id"`
	TeacherName   string    `json:"teacher_name"`
	ClassroomName string    `json:"classroom_name"`
	StartedAt     time.Time `json:"started_at"`
	People        int       `json:"people"` // in it now
}

// AdminLive lists the sessions open or live now, newest first.
func (s *Service) AdminLive(ctx context.Context, p authn.Person) ([]LiveSession, error) {
	if err := requireAdmin(p); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT s.id, s.title, s.status, s.teacher_id, s.teacher_name, s.classroom_name, coalesce(s.started_at, s.created_at),
		(SELECT count(DISTINCT v.user_id) FROM tutoring.visits v WHERE v.session_id = s.id AND v.left_at IS NULL)::int
		FROM tutoring.sessions s WHERE s.status IN ('open', 'live') AND (s.status = 'live' OR s.scheduled_at IS NULL OR s.scheduled_at <= now() + interval '2 hours')
		ORDER BY 7 DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LiveSession, error) {
		var l LiveSession
		err := r.Scan(&l.ID, &l.Title, &l.Status, &l.TeacherID, &l.TeacherName, &l.ClassroomName, &l.StartedAt, &l.People)
		return l, err
	})
}

// AdminEnd ends a session for the admin, whoever leads it, with the reason
// in its log and emailed to its teacher (D-90).
func (s *Service) AdminEnd(ctx context.Context, p authn.Person, id, reason string) error {
	if err := requireAdmin(p); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 1 || n > 500 {
		return web.Invalid(map[string]string{"reason": "say why, in at most 500 characters: the teacher is told"})
	}
	sess, err := s.session(ctx, id)
	if err != nil {
		return err
	}
	if sess.Status == "ended" {
		return web.ErrNotFound
	}
	if err := s.endSession(ctx, sess); err != nil {
		return err
	}
	s.logAction(ctx, id, p.ID, "ended_by_admin", nil, map[string]string{"reason": reason})
	body := fmt.Sprintf("Hello %s,\n\nAn administrator ended your tutoring session \"%s\" (%s) at %s.\n\nWhy: %s\n\nIts attendance and chat are kept.\n",
		sess.TeacherName, sess.Title, sess.ClassroomName, s.now().UTC().Format("2 Jan 2006 15:04 UTC"), reason)
	if err := s.platform.Notify(ctx, platform.Notice{ClassroomID: sess.ClassroomID, Subject: fmt.Sprintf("Your tutoring session \"%s\" was ended", sess.Title), Body: body}); err != nil {
		s.log.Warn("telling the teacher", "session", id, "err", err)
	}
	return nil
}

func (s *Service) hAdminLive(w http.ResponseWriter, r *http.Request) error {
	list, err := s.AdminLive(r.Context(), me(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"sessions": list})
	return nil
}

func (s *Service) hAdminEnd(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.AdminEnd(r.Context(), me(r), id(r), in.Reason))
}
