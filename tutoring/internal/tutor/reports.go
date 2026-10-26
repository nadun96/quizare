package tutor

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Attendance reports (D-87, TS-FR-81 to TS-FR-83): across a classroom's
// sessions, for its teacher, and across a student's classrooms, for them.
// A session counts once it has started (live or ended); a student attended
// it when they were connected while it was live (the visits, TS-FR-07).
// The tutoring service knows who came, not the whole class: the platform's
// page sets the report beside the classroom's roster, so students who never
// came show as such.

// ReportSession is one session in a report.
type ReportSession struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Seconds   int64      `json:"seconds"` // how long it ran
}

// ReportStudent is one student who came to any of the sessions.
type ReportStudent struct {
	UserID   string   `json:"user_id"`
	Name     string   `json:"name"`
	Attended []string `json:"attended"` // session ids
	Seconds  int64    `json:"seconds"`  // time present, all sessions
}

// ClassroomReport is a classroom's attendance between two dates.
type ClassroomReport struct {
	Sessions        []ReportSession `json:"sessions"`
	Students        []ReportStudent `json:"students"`
	CountsInResults bool            `json:"counts_in_results"`
}

// teaches checks, through the platform, that p teaches the classroom. It
// asks for p's classrooms rather than their access to this one: asking for
// access may enrol a student, as joining does (BR-02), and a report is no
// reason to enrol anyone.
func (s *Service) teaches(ctx context.Context, p authn.Person, classroomID string) error {
	cs, err := s.platform.Classrooms(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if c.ID == classroomID && c.Role == "teacher" {
			return nil
		}
	}
	return web.ErrNotFound
}

func (s *Service) reportSessions(ctx context.Context, classroomID string, from, to time.Time) ([]ReportSession, error) {
	now := s.now()
	rows, _ := s.pool.Query(ctx, `SELECT id, title, started_at, ended_at, extract(epoch FROM coalesce(ended_at, $4) - started_at)::bigint
		FROM tutoring.sessions WHERE classroom_id=$1 AND status IN ('live', 'ended') AND started_at >= $2 AND started_at < $3
		ORDER BY started_at, id`, classroomID, from, to, now)
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReportSession, error) {
		var x ReportSession
		err := r.Scan(&x.ID, &x.Title, &x.StartedAt, &x.EndedAt, &x.Seconds)
		return x, err
	})
	if list == nil {
		list = []ReportSession{}
	}
	return list, err
}

// ClassroomAttendance is a classroom's attendance from from to to (at most
// a year), for its teacher (TS-FR-81, TS-FR-83).
func (s *Service) ClassroomAttendance(ctx context.Context, p authn.Person, classroomID string, from, to time.Time) (ClassroomReport, error) {
	if !to.After(from) || to.Sub(from) > 367*24*time.Hour {
		return ClassroomReport{}, web.Invalid(map[string]string{"to": "after from, and at most a year later"})
	}
	if err := s.teaches(ctx, p, classroomID); err != nil {
		return ClassroomReport{}, err
	}
	out := ClassroomReport{Students: []ReportStudent{}}
	var err error
	if out.Sessions, err = s.reportSessions(ctx, classroomID, from, to); err != nil {
		return out, err
	}
	if len(out.Sessions) > 0 {
		ids := make([]string, len(out.Sessions))
		for i, x := range out.Sessions {
			ids[i] = x.ID
		}
		rows, _ := s.pool.Query(ctx, `SELECT v.user_id::text, max(p.name), array_agg(DISTINCT v.session_id::text),
				coalesce(sum(extract(epoch FROM coalesce(v.left_at, $2) - v.joined_at)), 0)::bigint
			FROM tutoring.visits v JOIN tutoring.participants p ON p.session_id = v.session_id AND p.user_id = v.user_id AND p.role = 'student'
			WHERE v.session_id::text = ANY($1) GROUP BY v.user_id ORDER BY lower(max(p.name))`, ids, s.now())
		if out.Students, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReportStudent, error) {
			var x ReportStudent
			err := r.Scan(&x.UserID, &x.Name, &x.Attended, &x.Seconds)
			return x, err
		}); err != nil {
			return out, err
		}
		if out.Students == nil {
			out.Students = []ReportStudent{}
		}
	}
	err = s.pool.QueryRow(ctx, `SELECT coalesce((SELECT attendance_in_results FROM tutoring.classroom_settings WHERE classroom_id=$1), false)`, classroomID).Scan(&out.CountsInResults)
	return out, err
}

// SetCountsInResults decides whether attendance shows in the classroom's
// results and their export (TS-FR-82).
func (s *Service) SetCountsInResults(ctx context.Context, p authn.Person, classroomID string, on bool) error {
	if err := s.teaches(ctx, p, classroomID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO tutoring.classroom_settings (classroom_id, attendance_in_results, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (classroom_id) DO UPDATE SET attendance_in_results=EXCLUDED.attendance_in_results, updated_at=EXCLUDED.updated_at`, classroomID, on, s.now())
	return err
}

// MyClassroomAttendance is a student's own attendance in one classroom.
type MyClassroomAttendance struct {
	ClassroomID   string          `json:"classroom_id"`
	ClassroomName string          `json:"classroom_name"`
	Sessions      int             `json:"sessions"`
	Attended      int             `json:"attended"`
	Seconds       int64           `json:"seconds"`
	Recent        []MyRecentVisit `json:"recent"` // the last 10 sessions
}

// MyRecentVisit is one session and whether the student was there.
type MyRecentVisit struct {
	Title     string    `json:"title"`
	StartedAt time.Time `json:"started_at"`
	Attended  bool      `json:"attended"`
	Seconds   int64     `json:"seconds"`
}

// MyAttendance is the caller's attendance in each classroom they study in,
// over the last year (TS-FR-81: on the student's own page).
func (s *Service) MyAttendance(ctx context.Context, p authn.Person) ([]MyClassroomAttendance, error) {
	cs, err := s.platform.Classrooms(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := []MyClassroomAttendance{}
	now := s.now()
	for _, c := range cs {
		if c.Role != "student" {
			continue
		}
		rows, _ := s.pool.Query(ctx, `SELECT se.title, se.started_at, count(v.id) > 0,
				coalesce(sum(extract(epoch FROM coalesce(v.left_at, $3) - v.joined_at)), 0)::bigint
			FROM tutoring.sessions se LEFT JOIN tutoring.visits v ON v.session_id = se.id AND v.user_id = $2
			WHERE se.classroom_id = $1 AND se.status IN ('live', 'ended') AND se.started_at >= $4
			GROUP BY se.id ORDER BY se.started_at DESC`, c.ID, p.ID, now, now.AddDate(-1, 0, 0))
		visits, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MyRecentVisit, error) {
			var v MyRecentVisit
			err := r.Scan(&v.Title, &v.StartedAt, &v.Attended, &v.Seconds)
			return v, err
		})
		if err != nil {
			return nil, err
		}
		if len(visits) == 0 {
			continue
		}
		a := MyClassroomAttendance{ClassroomID: c.ID, ClassroomName: c.Name, Sessions: len(visits), Recent: visits[:min(10, len(visits))]}
		for _, v := range visits {
			if v.Attended {
				a.Attended++
			}
			a.Seconds += v.Seconds
		}
		out = append(out, a)
	}
	return out, nil
}

// DefaultRetention is how long sessions, with their chat and attendance,
// are kept after they end (PO-16, TS-NFR-25).
const DefaultRetention = 365 * 24 * time.Hour

// SweepRetention deletes sessions that ended (or were cancelled) longer ago
// than keep, with everything kept with them (TS-FR-83).
func (s *Service) SweepRetention(ctx context.Context, keep time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tutoring.sessions WHERE status IN ('ended', 'cancelled')
		AND coalesce(ended_at, cancelled_at, created_at) < $1`, s.now().Add(-keep))
	return tag.RowsAffected(), err
}
