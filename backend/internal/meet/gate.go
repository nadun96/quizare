package meet

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

// ForceEnd ends an open meeting for the admin, whoever hosts it (D-90).
func (s *Service) ForceEnd(ctx context.Context, actorID, id string) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE meet.meetings SET status='ended', ended_at=$2 WHERE id=$1 AND status='open'`, id, s.now())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(ctx, tx, actorID, "meeting_ended", "meeting", id, nil)
	})
	if err == nil {
		s.hub.end(id)
	}
	return err
}

// Online is how many are in a meeting now.
func (s *Service) Online(id string) int { return s.hub.count(id) }
