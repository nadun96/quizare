package live

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// AutoEndQuiet is how long a live session must stay idle with every attempt
// finished before it ends by itself (UC-02 step 6). The pause gives the
// teacher time to reinstate a last-minute invalidation (D-18).
const AutoEndQuiet = 2 * time.Minute

// Run starts the 1 Hz deadline ticker and the 2 Hz dashboard flusher. One
// shared ticker replaces a timer per student (ADR-07). Deadlines are
// timestamps in Postgres, so a restart loses nothing: the next tick picks up
// whatever came due.
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	dash := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	defer dash.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
				s.log.Error("live tick", "err", err)
			}
		case <-dash.C:
			s.hub.flushDashboards(ctx)
		}
	}
}

// Tick enforces every deadline that has come due. Exported for tests.
func (s *Service) Tick(ctx context.Context) error {
	now := s.now()
	ids := func(sql string, args ...any) ([]string, error) {
		rows, err := s.pool.Query(ctx, sql, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[string])
	}

	// Countdown expired → the quiz starts automatically (FR-SS-05, AC-02).
	due, err := ids(`SELECT id FROM live.attempts WHERE state='admitted' AND countdown_deadline <= $1`, now)
	if err != nil {
		return err
	}
	for _, id := range due {
		if _, err := s.change(ctx, id, func(tx pgx.Tx, sess *Session, a *Attempt) error {
			if a.State != StateAdmitted || a.CountdownDeadline == nil || a.CountdownDeadline.After(s.now()) {
				return errNoop
			}
			sess.startAttempt(a, s.now())
			return s.event(ctx, tx, sess.ID, a.ID, "", "started", map[string]string{"by": "countdown"})
		}); err != nil {
			return err
		}
	}

	// Question or quiz time up, or a socket that never came back.
	due, err = ids(`SELECT id FROM live.attempts WHERE state='in_progress'
		AND (quiz_deadline <= $1 OR question_deadline <= $1 OR disconnected_at IS NOT NULL)`, now.Add(-Grace))
	if err != nil {
		return err
	}
	for _, id := range due {
		if err := s.expire(ctx, id); err != nil {
			return err
		}
	}

	// Auto-end idle sessions whose attempts are all finished.
	due, err = ids(`SELECT s.id FROM live.sessions s WHERE s.status='live'
		AND EXISTS (SELECT 1 FROM live.attempts a WHERE a.session_id=s.id)
		AND NOT EXISTS (SELECT 1 FROM live.attempts a WHERE a.session_id=s.id AND a.state IN ('waiting','admitted','in_progress','paused'))
		AND (SELECT max(updated_at) FROM live.attempts a WHERE a.session_id=s.id) <= $1`, now.Add(-AutoEndQuiet))
	if err != nil {
		return err
	}
	for _, id := range due {
		sess, err := s.session(ctx, id)
		if err != nil {
			return err
		}
		if err := s.endSession(ctx, sess, ""); err != nil && err != errSessionEnded {
			return err
		}
	}
	return nil
}

func (s *Service) expire(ctx context.Context, attemptID string) error {
	disconnectedTooLong := false
	_, err := s.change(ctx, attemptID, func(tx pgx.Tx, sess *Session, a *Attempt) error {
		if a.State != StateInProgress {
			return errNoop
		}
		now := s.now()
		late := func(t *time.Time) bool { return t != nil && !now.Before(t.Add(Grace)) }
		switch {
		case late(a.QuizDeadline):
			// The quiz limit ends the attempt whatever question is open (AC-05).
			return s.finish(ctx, tx, sess, a, "", "time_up")
		case late(a.QuestionDeadline):
			// The answer so far is already saved; move on (AC-04).
			if !advance(a, now, len(a.Order), func(i int) int { return sess.questionLimit(a, i) }) {
				return s.finish(ctx, tx, sess, a, "", "time_up")
			}
			return s.event(ctx, tx, sess.ID, a.ID, "", "question_timed_out", map[string]int{"index": a.Current - 1})
		case a.DisconnectedAt != nil:
			grace := time.Duration(sess.Effective(nil, a.Overrides).DisconnectGraceSec) * time.Second
			disconnectedTooLong = !now.Before(a.DisconnectedAt.Add(grace)) && !s.hub.isConnected(a.ID)
		}
		return errNoop
	})
	if err != nil || !disconnectedTooLong {
		return err
	}
	_, err = s.violation(ctx, "", attemptID, ViolationInput{Kind: "disconnected"})
	return err
}
