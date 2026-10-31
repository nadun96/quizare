// Package inbox is what teachers tell their students (D-103): announcements
// to a classroom, read by its students and counted for the teacher, and
// messages between a teacher and a student of one of their classrooms,
// either of whom may start. New ones also come by email unless the person
// turned that off. It owns the "inbox" schema.
package inbox

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
)

const (
	maxTitle   = 200
	maxBody    = 10000
	maxMessage = 5000
)

type Service struct {
	pool    *pgxpool.Pool
	jobs    jobs.Inserter
	baseURL string
}

func NewService(pool *pgxpool.Pool, inserter jobs.Inserter, baseURL string) *Service {
	return &Service{pool: pool, jobs: inserter, baseURL: baseURL}
}

// Person is someone on the other side: a teacher for a student, a student
// for a teacher. Classrooms names what they share.
type Person struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Avatar     string `json:"avatar,omitempty"`
	Classrooms string `json:"classrooms,omitempty"`
}

// Summary is what the bar's Inbox badge shows.
type Summary struct {
	Announcements int  `json:"announcements"` // unread, for students
	Messages      int  `json:"messages"`      // unread messages
	Email         bool `json:"email"`         // new ones also come by email
}

// Summary counts what is unread.
func (s *Service) Summary(ctx context.Context, u auth.User) (Summary, error) {
	out := Summary{Email: true}
	err := s.pool.QueryRow(ctx, `SELECT coalesce((SELECT email FROM inbox.prefs WHERE user_id = $1), true)`, u.ID).Scan(&out.Email)
	if err != nil {
		return out, err
	}
	if u.Role == auth.RoleStudent {
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM inbox.announcements a`+studentAnnouncements+
			` AND NOT EXISTS (SELECT 1 FROM inbox.announcement_reads r WHERE r.announcement_id = a.id AND r.user_id = $1)`, u.ID).Scan(&out.Announcements); err != nil {
			return out, err
		}
	}
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM inbox.messages m JOIN inbox.conversations c ON c.id = m.conversation_id
		WHERE (c.teacher_id = $1 AND m.created_at > coalesce(c.teacher_read_at, '-infinity') OR c.student_id = $1 AND m.created_at > coalesce(c.student_read_at, '-infinity'))
		AND m.sender_id IS DISTINCT FROM $1`, u.ID).Scan(&out.Messages)
	return out, err
}

// SetEmail turns emails of new announcements and messages on or off.
func (s *Service) SetEmail(ctx context.Context, userID string, on bool) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO inbox.prefs (user_id, email) VALUES ($1, $2) ON CONFLICT (user_id) DO UPDATE SET email = $2`, userID, on)
	return err
}

// wantsEmail reports, for each user, whether they still get email: active
// accounts that didn't turn it off.
func wantsEmail(ctx context.Context, tx pgx.Tx, ids []string) ([]mailTo, error) {
	rows, err := tx.Query(ctx, `SELECT u.name, u.email FROM auth.users u LEFT JOIN inbox.prefs p ON p.user_id = u.id
		WHERE u.id = ANY($1::uuid[]) AND u.status = 'active' AND coalesce(p.email, true)`, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (mailTo, error) {
		var m mailTo
		return m, r.Scan(&m.name, &m.email)
	})
}

type mailTo struct{ name, email string }

func (s *Service) queue(ctx context.Context, tx pgx.Tx, m mail.Message) error {
	_, err := s.jobs.InsertTx(ctx, tx, mail.Args{Message: m}, nil)
	return err
}

// teaches says whether the teacher teaches the student now: an active
// enrolment in one of their classrooms that isn't archived.
func (s *Service) teaches(ctx context.Context, teacherID, studentID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM content.classrooms c JOIN content.enrolments e ON e.classroom_id = c.id
		WHERE c.teacher_id = $1 AND e.user_id::text = $2 AND e.status = 'active' AND c.archived_at IS NULL)`, teacherID, studentID).Scan(&ok)
	return ok, err
}

func text(field, v string, limit int) (string, error) {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return v, httpx.Invalid(map[string]string{field: "required"})
	case len(v) > limit:
		return v, httpx.Invalid(map[string]string{field: "too long"})
	}
	return v, nil
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	return err
}

// excerpt shortens a message for a list or a subject line.
func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
