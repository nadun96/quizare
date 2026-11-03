package live

import (
	"context"
	"encoding/json"
	mrand "math/rand/v2"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
)

// Retries (D-131): a student takes the session's quiz again from the first
// question. The teacher may let anyone who finished retry; a student may
// start again themselves after submitting, up to the session's tries. Each
// try is its own attempt with a fresh question order (and draw from the
// pools); the old one is kept, superseded, and the best valid result counts.

var (
	errNoTriesLeft = httpx.NewError(http.StatusConflict, "no_tries_left", "you have used all your tries for this quiz")
	errNotFinished = httpx.NewError(http.StatusConflict, "not_finished", "a retry starts after the quiz is handed in")
	errTeamRetry   = httpx.NewError(http.StatusConflict, "team_retry", "retries aren't possible in a team quiz")
)

// canRetry is whether an attempt may be followed by a new try, by anyone.
func canRetry(v Session, eff settings.Effective, a *Attempt) error {
	switch {
	case a.SupersededAt != nil:
		return errNotFinished
	case v.Status == SessionEnded:
		return errSessionEnded
	case eff.TeamMode != "off":
		return errTeamRetry
	case a.State != StateSubmitted && a.State != StateInvalidated:
		return errNotFinished
	}
	return nil
}

// selfRetry is whether the student may start again themselves: after
// handing in (not after being removed), within the tries allowed.
func (s *Service) selfRetry(v Session, eff settings.Effective, a *Attempt) error {
	if err := canRetry(v, eff, a); err != nil {
		return err
	}
	if a.State != StateSubmitted {
		return errNotFinished
	}
	if a.Try >= eff.Tries {
		return errNoTriesLeft
	}
	return nil
}

// retry replaces a finished attempt with a new try, admitted at once (it
// counts down as on admission), and returns the new attempt.
func (s *Service) retry(ctx context.Context, sess *Session, attemptID, actor string, self bool) (*Attempt, error) {
	var old, next *Attempt
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if old, err = scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.id=$1 FOR UPDATE`, attemptID)); err != nil {
			return err
		}
		v := s.sessionView(sess)
		eff := v.Effective(nil, old.Overrides)
		if self {
			err = s.selfRetry(v, eff, old)
		} else {
			err = canRetry(v, eff, old)
		}
		if err != nil {
			return err
		}
		now := s.now()
		if _, err := tx.Exec(ctx, `UPDATE live.attempts SET superseded_at=$2, updated_at=$2 WHERE id=$1`, old.ID, now); err != nil {
			return err
		}
		old.SupersededAt = &now
		rng := mrand.New(mrand.NewPCG(mrand.Uint64(), mrand.Uint64()))
		order, opts := newOrders(v.Snapshot.Questions, eff.QuestionOrder == "shuffled", eff.QuestionDraw, v.Snapshot.Pools, func(q quiz.Question) bool {
			return v.Effective(&q, settings.Overrides{}).OptionOrder == "shuffled"
		}, rng)
		order32 := make([]int32, len(order))
		for i, x := range order {
			order32[i] = int32(x)
		}
		optsJSON, _ := json.Marshal(opts)
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO live.attempts(session_id, user_id, student_number, overrides, extension_sec, state, question_order, option_orders, try, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,'waiting',$6,$7,$8,$9,$9) RETURNING id`,
			old.SessionID, old.UserID, old.StudentNumber, old.Overrides.JSON(), old.ExtensionSec, order32, optsJSON, old.Try+1, now).Scan(&id); err != nil {
			return err
		}
		if next, err = scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.id=$1`, id)); err != nil {
			return err
		}
		if err := s.event(ctx, tx, sess.ID, next.ID, actor, "retry", map[string]any{"try": next.Try, "previous": old.ID, "by_student": self}); err != nil {
			return err
		}
		if err := s.admitTx(ctx, tx, sess, next, actor); err != nil {
			return err
		}
		return saveAttempt(ctx, tx, next, now)
	})
	if err != nil {
		return nil, err
	}
	s.hub.publishAttempt(sess, old) // their screen moves to the new try
	s.hub.publishAttempt(sess, next)
	return next, nil
}

// Retry lets the chosen students who finished take the quiz again (the teacher's
// "Let them retry"), whatever the tries setting, and says how many will.
func (s *Service) Retry(ctx context.Context, teacherID, sessionID string, t Target) (int, error) {
	sess, err := s.live(ctx, teacherID, sessionID)
	if err != nil {
		return 0, err
	}
	ids, err := s.targets(ctx, sessionID, t, StateSubmitted, StateInvalidated)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if _, err := s.retry(ctx, sess, id, teacherID, false); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// StudentRetry starts the student's next try, within the tries allowed.
func (s *Service) StudentRetry(ctx context.Context, userID, attemptID string) (*Attempt, error) {
	a, err := scanAttempt(s.pool.QueryRow(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.id=$1`, attemptID))
	if err != nil {
		return nil, err
	}
	if err := s.ownAttempt(userID, a); err != nil {
		return nil, err
	}
	sess, err := s.session(ctx, a.SessionID)
	if err != nil {
		return nil, err
	}
	return s.retry(ctx, sess, attemptID, userID, true)
}
