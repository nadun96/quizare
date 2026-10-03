package live

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
)

// MarkingItem is one question of an attempt with its effective settings
// (question, session and student levels applied) and the saved response.
type MarkingItem struct {
	Question  quiz.Question
	Effective settings.Effective
	Response  *quiz.Response
	SavedAt   *time.Time
}

// MarkingData is everything evaluation and results need about one attempt.
type MarkingData struct {
	AttemptID     string
	SessionID     string
	SessionTitle  string
	QuizTitle     string
	TeacherID     string
	UserID        string
	StudentNumber *string
	State         string
	SubmittedAt   *time.Time
	Session       settings.Effective // session-level effective settings for this student
	Released      bool               // results visible to the student (BR-12)
	Items         []MarkingItem      // in the order the student saw them
}

// released applies results_release (BA §7): immediate, on_session_end or manual.
func released(sess Session, a *Attempt, eff settings.Effective) bool {
	if !Finished(a.State) {
		return false
	}
	if sess.ReleasedAt != nil {
		return true
	}
	switch eff.ResultsRelease {
	case "immediate":
		return true
	case "on_session_end":
		return sess.Status == SessionEnded
	default:
		return false
	}
}

func (s *Service) markingData(ctx context.Context, sess Session, a *Attempt) (*MarkingData, error) {
	rows, err := s.pool.Query(ctx, `SELECT question_id, response, saved_at FROM live.answers WHERE attempt_id=$1`, a.ID)
	if err != nil {
		return nil, err
	}
	type saved struct {
		r  quiz.Response
		at time.Time
	}
	answers := map[string]saved{}
	for rows.Next() {
		var qid string
		var raw []byte
		var sv saved
		if err := rows.Scan(&qid, &raw, &sv.at); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(raw, &sv.r); err != nil {
			rows.Close()
			return nil, err
		}
		answers[qid] = sv
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	eff := sess.Effective(nil, a.Overrides)
	d := &MarkingData{
		AttemptID: a.ID, SessionID: sess.ID, SessionTitle: sess.Title, QuizTitle: sess.Snapshot.QuizTitle,
		TeacherID: sess.TeacherID, UserID: a.UserID, StudentNumber: a.StudentNumber, State: a.State,
		SubmittedAt: a.SubmittedAt, Session: eff, Released: released(sess, a, eff),
	}
	for i := range a.Order {
		q := sess.question(a, i)
		item := MarkingItem{Question: *q, Effective: sess.Effective(q, a.Overrides)}
		if sv, ok := answers[q.ID]; ok {
			r, at := sv.r, sv.at
			item.Response, item.SavedAt = &r, &at
		}
		d.Items = append(d.Items, item)
	}
	return d, nil
}

// MarkingData loads one attempt for evaluation.
func (s *Service) MarkingData(ctx context.Context, attemptID string) (*MarkingData, error) {
	a, err := scanAttempt(s.pool.QueryRow(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.id=$1`, attemptID))
	if err != nil {
		return nil, err
	}
	sess, err := s.session(ctx, a.SessionID)
	if err != nil {
		return nil, err
	}
	return s.markingData(ctx, s.sessionView(sess), a)
}

// SessionMarkingData loads every finished attempt of a session for its teacher.
func (s *Service) SessionMarkingData(ctx context.Context, teacherID, sessionID string) ([]*MarkingData, error) {
	sess, err := s.ownedSession(ctx, teacherID, sessionID)
	if err != nil {
		return nil, err
	}
	v := s.sessionView(sess)
	rows, err := s.pool.Query(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.session_id=$1
		AND a.state IN ('submitted','invalidated') ORDER BY a.created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Attempt, error) { return scanAttempt(r) })
	if err != nil {
		return nil, err
	}
	out := make([]*MarkingData, 0, len(list))
	for _, a := range list {
		d, err := s.markingData(ctx, v, a)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// AttemptIDs lists a session's finished attempts in the given states.
func (s *Service) AttemptIDs(ctx context.Context, sessionID string, states ...string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM live.attempts WHERE session_id=$1 AND state = ANY($2) ORDER BY created_at`, sessionID, states)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Release makes a session's results visible now (results_release=manual, Q-10).
func (s *Service) Release(ctx context.Context, teacherID, sessionID string) error {
	if _, err := s.ownedSession(ctx, teacherID, sessionID); err != nil {
		return err
	}
	now := s.now()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE live.sessions SET released_at=coalesce(released_at,$2) WHERE id=$1`, sessionID, now); err != nil {
			return err
		}
		if err := s.event(ctx, tx, sessionID, "", teacherID, "results_released", nil); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "results_released", "session", sessionID, nil)
	})
	if err != nil {
		return err
	}
	s.updateCached(sessionID, func(x *Session) {
		if x.ReleasedAt == nil {
			x.ReleasedAt = &now
		}
	})
	return nil
}

// Unrelease hides results again (e.g. after spotting a marking error).
func (s *Service) Unrelease(ctx context.Context, teacherID, sessionID string) error {
	if _, err := s.ownedSession(ctx, teacherID, sessionID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE live.sessions SET released_at=NULL WHERE id=$1`, sessionID); err != nil {
		return err
	}
	s.updateCached(sessionID, func(x *Session) { x.ReleasedAt = nil })
	return nil
}

// StudentAttempt is one row of a student's own history.
type StudentAttempt struct {
	AttemptID    string     `json:"attempt_id"`
	SessionID    string     `json:"session_id"`
	SessionTitle string     `json:"session_title"`
	QuizTitle    string     `json:"quiz_title"`
	State        string     `json:"state"`
	Released     bool       `json:"released"`
	SubmittedAt  *time.Time `json:"submitted_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Service) MyAttempts(ctx context.Context, userID string) ([]StudentAttempt, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.user_id=$1 ORDER BY a.created_at DESC LIMIT 200`, userID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Attempt, error) { return scanAttempt(r) })
	if err != nil {
		return nil, err
	}
	out := make([]StudentAttempt, 0, len(list))
	for _, a := range list {
		sess, err := s.session(ctx, a.SessionID)
		if err != nil {
			return nil, err
		}
		v := s.sessionView(sess)
		out = append(out, StudentAttempt{AttemptID: a.ID, SessionID: v.ID, SessionTitle: v.Title, QuizTitle: v.Snapshot.QuizTitle,
			State: a.State, Released: released(v, a, v.Effective(nil, a.Overrides)), SubmittedAt: a.SubmittedAt, CreatedAt: a.CreatedAt})
	}
	return out, nil
}

// OwnMarkingData returns an attempt's data for the student who owns it.
func (s *Service) OwnMarkingData(ctx context.Context, userID, attemptID string) (*MarkingData, error) {
	d, err := s.MarkingData(ctx, attemptID)
	if errors.Is(err, httpx.ErrNotFound) || (err == nil && d.UserID != userID) {
		return nil, httpx.ErrNotFound
	}
	return d, err
}

var errNotReleased = httpx.NewError(http.StatusForbidden, "results_not_released", "results for this quiz have not been released yet")

// ErrNotReleased is returned when a student asks for results too early.
func ErrNotReleased() error { return errNotReleased }
