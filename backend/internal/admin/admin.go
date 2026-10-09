// Package admin provides platform oversight (BA §4 admin role) and the
// student's own data export (NFR-04). Admins see counts and the audit log,
// never quiz content, answers or API keys.
package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

type Service struct {
	pool *pgxpool.Pool
	auth *auth.Service
}

func NewService(pool *pgxpool.Pool, a *auth.Service) *Service { return &Service{pool: pool, auth: a} }

// Usage is a read-only reporting query; it only counts rows.
type Usage struct {
	UsersByRole      map[string]int `json:"users_by_role"`
	UsersByStatus    map[string]int `json:"users_by_status"`
	Classrooms       int            `json:"classrooms"`
	Quizzes          int            `json:"quizzes"`
	Sessions         int            `json:"sessions"`
	Sessions30d      int            `json:"sessions_30d"`
	LiveSessions     int            `json:"live_sessions"`
	Attempts30d      int            `json:"attempts_30d"`
	Violations30d    int            `json:"violations_30d"`
	TeachersWithKeys int            `json:"teachers_with_llm_keys"`
	JobsByQueueState map[string]int `json:"jobs_by_queue_state"`
}

func (s *Service) Usage(ctx context.Context) (Usage, error) {
	u := Usage{UsersByRole: map[string]int{}, UsersByStatus: map[string]int{}, JobsByQueueState: map[string]int{}}
	groups := []struct {
		sql string
		dst map[string]int
	}{
		{`SELECT role, count(*) FROM auth.users WHERE status <> 'deleted' GROUP BY role`, u.UsersByRole},
		{`SELECT status, count(*) FROM auth.users GROUP BY status`, u.UsersByStatus},
		{`SELECT queue || ':' || state, count(*) FROM river_job GROUP BY queue, state`, u.JobsByQueueState},
	}
	for _, g := range groups {
		rows, err := s.pool.Query(ctx, g.sql)
		if err != nil {
			return u, err
		}
		for rows.Next() {
			var k string
			var n int
			if err := rows.Scan(&k, &n); err != nil {
				rows.Close()
				return u, err
			}
			g.dst[k] = n
		}
		rows.Close()
	}
	since := time.Now().Add(-30 * 24 * time.Hour)
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM content.classrooms),
		(SELECT count(*) FROM quiz.quizzes),
		(SELECT count(*) FROM live.sessions),
		(SELECT count(*) FROM live.sessions WHERE created_at >= $1),
		(SELECT count(*) FROM live.sessions WHERE status <> 'ended'),
		(SELECT count(*) FROM live.attempts WHERE created_at >= $1),
		(SELECT count(*) FROM live.violations WHERE server_ts >= $1),
		(SELECT count(DISTINCT teacher_id) FROM llm.keys)`, since).
		Scan(&u.Classrooms, &u.Quizzes, &u.Sessions, &u.Sessions30d, &u.LiveSessions, &u.Attempts30d, &u.Violations30d, &u.TeachersWithKeys)
	return u, err
}

type AuditEvent struct {
	ID         int64           `json:"id"`
	ActorID    *string         `json:"actor_id"`
	ActorName  string          `json:"actor_name,omitempty"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Details    json.RawMessage `json:"details"`
	CreatedAt  time.Time       `json:"created_at"`
}

// AuditSorts: the audit log is read newest first, or oldest first.
var AuditSorts = page.Sorts{"time": "e.id"}

// Audit returns one page of the audit log (NFR-15, PL-FR-01), newest first by
// default, filtered by action and target type and searched by action, target
// or actor name, with the number of matching events.
func (s *Service) Audit(ctx context.Context, action, targetType string, p page.Request) ([]AuditEvent, int, error) {
	where := ` FROM audit.events e LEFT JOIN auth.users u ON u.id = e.actor_id
		WHERE ($1 = '' OR e.action = $1) AND ($2 = '' OR e.target_type = $2)
		  AND ($3 = '' OR e.action ILIKE $3 OR e.target_type ILIKE $3 OR u.name ILIKE $3)`
	args := []any{action, targetType, p.Like()}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.actor_id, coalesce(u.name,''), e.action, e.target_type, e.target_id, e.details, e.created_at`+
		where+p.OrderBy(AuditSorts, "e.id")+p.Limit(), args...)
	if err != nil {
		return nil, 0, err
	}
	ev, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AuditEvent, error) {
		var e AuditEvent
		err := r.Scan(&e.ID, &e.ActorID, &e.ActorName, &e.Action, &e.TargetType, &e.TargetID, &e.Details, &e.CreatedAt)
		return e, err
	})
	return ev, total, err
}

// MyData exports everything stored about the calling user (NFR-04, ADR-16
// "export per student").
func (s *Service) MyData(ctx context.Context, userID string) (map[string]any, error) {
	out := map[string]any{}
	u, err := s.auth.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out["profile"] = u
	queries := map[string]string{
		"enrolments": `SELECT json_agg(x) FROM (SELECT c.name AS classroom, e.student_number, e.status, e.created_at
			FROM content.enrolments e JOIN content.classrooms c ON c.id=e.classroom_id WHERE e.user_id=$1) x`,
		"attempts": `SELECT json_agg(x) FROM (SELECT s.title AS session, a.state, a.student_number, a.started_at, a.submitted_at,
			a.invalidated_at, a.invalid_reason, r.score, r.max_score, r.pct, r.passed
			FROM live.attempts a JOIN live.sessions s ON s.id=a.session_id LEFT JOIN eval.results r ON r.attempt_id=a.id
			WHERE a.user_id=$1 ORDER BY a.created_at) x`,
		"answers": `SELECT json_agg(x) FROM (SELECT ans.attempt_id, ans.question_id, ans.response, ans.saved_at, m.score, m.feedback, m.ai_feedback
			FROM live.answers ans JOIN live.attempts a ON a.id=ans.attempt_id
			LEFT JOIN eval.marks m ON m.attempt_id=ans.attempt_id AND m.question_id=ans.question_id WHERE a.user_id=$1) x`,
		"violations": `SELECT json_agg(x) FROM (SELECT v.kind, v.action, v.server_ts FROM live.violations v
			JOIN live.attempts a ON a.id=v.attempt_id WHERE a.user_id=$1) x`,
		// The stored profile picture, as a data URL (D-49).
		"avatar": `SELECT (SELECT to_json('data:image/jpeg;base64,' || translate(encode(image, 'base64'), E'
', '')) FROM auth.avatars WHERE user_id=$1)`,
	}
	for key, q := range queries {
		var raw []byte
		if err := s.pool.QueryRow(ctx, q, userID).Scan(&raw); err != nil {
			return nil, err
		}
		if raw == nil {
			raw = []byte("[]")
		}
		out[key] = json.RawMessage(raw)
	}
	return out, nil
}

// AdminRoutes mounts under /api/admin (admin role enforced by caller).
func (s *Service) AdminRoutes(r chi.Router) {
	r.Method("GET", "/usage", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, err := s.Usage(r.Context())
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, u)
		return nil
	}))
	r.Method("GET", "/audit", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		q := r.URL.Query()
		p := page.Parse(r, AuditSorts, "time", true)
		ev, total, err := s.Audit(r.Context(), q.Get("action"), q.Get("target_type"), p)
		if err != nil {
			return err
		}
		page.Write(w, "events", ev, total, p)
		return nil
	}))
}

// UserRoutes mounts self-service privacy endpoints for any logged-in user.
func (s *Service) UserRoutes(r chi.Router) {
	r.Method("GET", "/my/data", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		d, err := s.MyData(r.Context(), auth.MustUser(r.Context()).ID)
		if err != nil {
			return err
		}
		w.Header().Set("Content-Disposition", `attachment; filename="my-data.json"`)
		httpx.JSON(w, 200, d)
		return nil
	}))
}
