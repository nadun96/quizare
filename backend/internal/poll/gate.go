package poll

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Gate refuses new activities while the admin has paused them, for
// everyone or for this teacher (D-90).
type Gate func(ctx context.Context, teacherID string) error

// Check passes when there is no gate.
func (g Gate) Check(ctx context.Context, teacherID string) error {
	if g == nil {
		return nil
	}
	return g(ctx, teacherID)
}

// SetGate installs the check made before an activity starts.
func (s *Service) SetGate(g Gate) { s.gate = g }

// ForceClose closes an open poll for the admin, whoever runs it (D-90).
func (s *Service) ForceClose(ctx context.Context, actorID, id string) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE poll.polls SET status='closed', closed_at=$2, updated_at=now() WHERE id=$1 AND status='open'`, id, s.now())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(ctx, tx, actorID, "poll_closed", "poll", id, nil)
	})
	if err == nil {
		s.hub.stateChanged(id)
	}
	return err
}
