package presentation

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

// ForceEndLive stops a presentation being presented, for the admin (D-90).
func (s *Service) ForceEndLive(ctx context.Context, actorID, id string) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE presentation.lives SET ended_at=now() WHERE presentation_id=$1 AND ended_at IS NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(ctx, tx, actorID, "presentation_live_ended", "presentation", id, nil)
	})
	if err == nil {
		s.hub.end(id)
	}
	return err
}

// Online is how many are in a live presentation now, presenters included.
func (s *Service) Online(id string) int {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	if r := s.hub.rooms[id]; r != nil {
		return len(r.members)
	}
	return 0
}
