package tutor

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// The rules of the platform's archive, for tutoring sessions (D-149): a finished
// session can be archived, which takes it out of the everyday list, brought
// back, or deleted from the archive for good with all that's kept with it.

// ViewPast and ViewArchived are the session lists besides the everyday one.
const (
	ViewPast     = "past"
	ViewArchived = "archived"
)

func finished(sess Session) bool { return sess.Status == "ended" || sess.Status == "cancelled" }

// ArchiveSession puts the lead teacher's finished session in the archive.
func (s *Service) ArchiveSession(ctx context.Context, p authn.Person, id string) error {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return err
	}
	if !finished(sess) {
		return web.NewError(http.StatusConflict, "still_running", "it's still running: end it first")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET archived_at = coalesce(archived_at, now()) WHERE id = $1`, id); err != nil {
		return err
	}
	s.logAction(ctx, id, p.ID, "session_archived", nil, nil)
	return nil
}

// RestoreSession brings one back from the archive.
func (s *Service) RestoreSession(ctx context.Context, p authn.Person, id string) error {
	if _, err := s.owned(ctx, p, id); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET archived_at = NULL WHERE id = $1`, id); err != nil {
		return err
	}
	s.logAction(ctx, id, p.ID, "session_restored", nil, nil)
	return nil
}

// DeleteSession removes an archived session for good: its chat, attendance and
// recordings, files included.
func (s *Service) DeleteSession(ctx context.Context, p authn.Person, id string) error {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return err
	}
	if sess.ArchivedAt == nil {
		return web.NewError(http.StatusConflict, "not_archived", "archive it first: only what's in the archive can be deleted")
	}
	var recordings []string
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM tutoring.recordings WHERE session_id = $1`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			rows.Close()
			return err
		}
		recordings = append(recordings, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM tutoring.sessions WHERE id = $1`, id); err != nil {
		return err
	}
	if s.rec != nil {
		for _, r := range recordings {
			if err := os.Remove(s.rec.path(r)); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.log.Warn("deleting a recording's file", "recording", r, "err", err)
			}
		}
	}
	s.log.Info("session deleted from the archive", "session", id, "by", p.ID, "title", sess.Title)
	return nil
}
