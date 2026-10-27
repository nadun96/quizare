package progress

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, now: time.Now} }

// Of works out a student's progress. Only results the teacher released
// count, marked in full and not invalidated, so it never gives a mark away.
func (s *Service) Of(ctx context.Context, userID string) (Progress, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.title, coalesce(a.submitted_at, s.ended_at, a.created_at), r.pct::float8, r.passed
		FROM eval.results r JOIN live.attempts a ON a.id = r.attempt_id JOIN live.sessions s ON s.id = r.session_id
		WHERE r.user_id = $1 AND s.released_at IS NOT NULL AND r.complete AND NOT r.invalidated`, userID)
	if err != nil {
		return Progress{}, err
	}
	quizzes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Activity, error) {
		var a Activity
		err := r.Scan(&a.Title, &a.At, &a.Pct, &a.Passed)
		return a, err
	})
	if err != nil {
		return Progress{}, err
	}
	rows, err = s.pool.Query(ctx, `SELECT t.title, r.finished_at FROM tutorial.runs r JOIN tutorial.tutorials t ON t.id = r.tutorial_id
		WHERE r.user_id = $1 AND r.finished_at IS NOT NULL`, userID)
	if err != nil {
		return Progress{}, err
	}
	tutorials, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Activity, error) {
		var a Activity
		err := r.Scan(&a.Title, &a.At)
		return a, err
	})
	if err != nil {
		return Progress{}, err
	}
	return Compute(quizzes, tutorials, s.now()), nil
}

// StudentRoutes mounts GET /api/my/progress for students.
func (s *Service) StudentRoutes(r chi.Router) {
	r.Method("GET", "/my/progress", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u := auth.MustUser(r.Context())
		if u.Role != auth.RoleStudent {
			return httpx.ErrNotFound
		}
		p, err := s.Of(r.Context(), u.ID)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, p)
		return nil
	}))
}
