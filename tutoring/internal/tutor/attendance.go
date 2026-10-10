package tutor

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
)

// Attendance (TS-FR-07): who joined while the session was live, when they
// first came and last left, how often they rejoined, and their total time.

type Attendee struct {
	UserID   string     `json:"user_id"`
	Name     string     `json:"name"`
	Role     string     `json:"role"`
	FirstAt  time.Time  `json:"first_at"`
	LastLeft *time.Time `json:"last_left_at,omitempty"` // nil while still connected
	Visits   int        `json:"visits"`
	Seconds  int64      `json:"seconds"`
	Online   bool       `json:"online"`
}

// Attendance returns everyone who was in the live session, by name.
func (s *Service) Attendance(ctx context.Context, p authn.Person, id string) ([]Attendee, error) {
	if _, err := s.owned(ctx, p, id); err != nil {
		return nil, err
	}
	now := s.now()
	rows, _ := s.pool.Query(ctx, `SELECT v.user_id, p.name, p.role, min(v.joined_at),
			CASE WHEN bool_or(v.left_at IS NULL) THEN NULL ELSE max(v.left_at) END, count(*),
			coalesce(sum(extract(epoch FROM coalesce(v.left_at, $2) - v.joined_at)), 0)::bigint
		FROM tutoring.visits v JOIN tutoring.participants p ON p.session_id = v.session_id AND p.user_id = v.user_id
		WHERE v.session_id = $1 GROUP BY v.user_id, p.name, p.role ORDER BY p.role DESC, lower(p.name)`, id, now)
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Attendee, error) {
		var a Attendee
		err := r.Scan(&a.UserID, &a.Name, &a.Role, &a.FirstAt, &a.LastLeft, &a.Visits, &a.Seconds)
		return a, err
	})
	online := s.hub.online(id)
	for i := range list {
		list[i].Online = online[list[i].UserID]
	}
	return list, err
}

// AttendanceCSV writes the attendance as CSV, for during or after the session.
func (s *Service) AttendanceCSV(ctx context.Context, p authn.Person, id string, w io.Writer) error {
	list, err := s.Attendance(ctx, p, id)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"name", "role", "first_joined", "last_left", "visits", "minutes"})
	for _, a := range list {
		left := ""
		if a.LastLeft != nil {
			left = a.LastLeft.UTC().Format(time.RFC3339)
		}
		// A leading = + - @ would be a formula in a spreadsheet.
		name := a.Name
		if name != "" && (name[0] == '=' || name[0] == '+' || name[0] == '-' || name[0] == '@') {
			name = "'" + name
		}
		_ = cw.Write([]string{name, a.Role, a.FirstAt.UTC().Format(time.RFC3339), left, fmt.Sprint(a.Visits), fmt.Sprintf("%.1f", float64(a.Seconds)/60)})
	}
	cw.Flush()
	return cw.Error()
}
