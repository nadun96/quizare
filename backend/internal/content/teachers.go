package content

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Co-teachers (D-121). A classroom's owner invites another teacher by
// email; once they accept, they teach the classroom as the owner does: its
// students, modules and topics, quizzes, sessions and marking, tutorials,
// presentations, results and trends. The classroom itself stays its
// owner's: its name, settings, join code, archiving and deleting, its
// teachers, and removing students.

// Teacher is one of a classroom's teachers.
type Teacher struct {
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Email      string     `json:"email"`
	Avatar     string     `json:"avatar,omitempty"`
	Role       string     `json:"role"`   // owner or coteacher
	Status     string     `json:"status"` // active, or invited for a co-teacher who hasn't answered
	InvitedAt  *time.Time `json:"invited_at,omitempty"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
}

// Invitation is a classroom a teacher has been asked to co-teach.
type Invitation struct {
	ClassroomID string    `json:"classroom_id"`
	Classroom   string    `json:"classroom"`
	OwnerName   string    `json:"owner_name"`
	InvitedAt   time.Time `json:"invited_at"`
}

// Mailer sends an email inside the caller's transaction.
type Mailer func(ctx context.Context, tx pgx.Tx, m mail.Message) error

// SetMailer lets invitations go out by email.
func (s *Service) SetMailer(m Mailer, baseURL string) {
	s.mailer, s.baseURL = m, strings.TrimRight(baseURL, "/")
}

// Teaches reports whether userID owns or co-teaches the classroom.
func (s *Service) Teaches(ctx context.Context, classroomID, userID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT content.teaches($1, $2)`, classroomID, userID).Scan(&ok)
	return ok, err
}

// Teachers lists a classroom's owner and co-teachers, for anyone who teaches it.
func (s *Service) Teachers(ctx context.Context, teacherID, classroomID string) ([]Teacher, error) {
	c, err := s.GetClassroom(ctx, teacherID, classroomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.name, u.email, coalesce(u.avatar_version, ''), 'owner', 'active', NULL::timestamptz, NULL::timestamptz
		FROM auth.users u WHERE u.id = $2
		UNION ALL
		SELECT u.id, u.name, u.email, coalesce(u.avatar_version, ''), 'coteacher', t.status, t.created_at, t.accepted_at
		FROM content.classroom_teachers t JOIN auth.users u ON u.id = t.user_id
		WHERE t.classroom_id = $1`, classroomID, c.TeacherID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Teacher, error) {
		var t Teacher
		err := r.Scan(&t.UserID, &t.Name, &t.Email, &t.Avatar, &t.Role, &t.Status, &t.InvitedAt, &t.AcceptedAt)
		return t, err
	})
}

// Invite asks another teacher, by their account's email, to co-teach. Only the owner may.
func (s *Service) Invite(ctx context.Context, ownerID, classroomID, email string) (Teacher, error) {
	c, err := s.GetClassroom(ctx, ownerID, classroomID)
	if err != nil {
		return Teacher{}, err
	}
	if c.Role != "owner" {
		return Teacher{}, errOwnerOnly
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return Teacher{}, httpx.Invalid(map[string]string{"email": "enter the teacher's email"})
	}
	var t Teacher
	var role, status string
	err = s.pool.QueryRow(ctx, `SELECT id, name, email, coalesce(avatar_version, ''), role, status FROM auth.users WHERE lower(email) = lower($1)`, email).
		Scan(&t.UserID, &t.Name, &t.Email, &t.Avatar, &role, &status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return t, httpx.Invalid(map[string]string{"email": "no account has this email; they need to sign up as a teacher first"})
	case err != nil:
		return t, err
	case role != "teacher" || status != "active":
		return t, httpx.Invalid(map[string]string{"email": "this account isn't an active teacher"})
	case t.UserID == ownerID:
		return t, httpx.Invalid(map[string]string{"email": "you already own this classroom"})
	}
	t.Role, t.Status = "coteacher", "invited"
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var inserted bool
		if err := tx.QueryRow(ctx, `INSERT INTO content.classroom_teachers (classroom_id, user_id, invited_by) VALUES ($1, $2, $3)
			ON CONFLICT (classroom_id, user_id) DO UPDATE SET invited_by = EXCLUDED.invited_by
			RETURNING xmax = 0, status, created_at`, classroomID, t.UserID, ownerID).Scan(&inserted, &t.Status, &t.InvitedAt); err != nil {
			return err
		}
		if t.Status == "active" {
			return httpx.Invalid(map[string]string{"email": t.Name + " already teaches this classroom"})
		}
		if s.mailer != nil {
			msg := mail.Message{To: t.Email, Subject: "Teach " + c.Name + " with " + c.OwnerName,
				Body: fmt.Sprintf("Hi %s,\n\n%s has invited you to co-teach the classroom \"%s\" on Classroom Quiz.\n\nAccept or decline on your Classrooms page:\n%s/t\n\nAs a co-teacher you'll see its students, run and mark its quizzes, and add lessons. The classroom itself stays %s's.",
					t.Name, c.OwnerName, c.Name, s.baseURL, c.OwnerName)}
			if err := s.mailer(ctx, tx, msg); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, ownerID, "coteacher_invited", "classroom", classroomID, map[string]any{"teacher": t.UserID})
	})
	return t, err
}

// RemoveTeacher takes a co-teacher (or an invitation) off a classroom: the
// owner removes anyone, a co-teacher removes themself (leaves).
func (s *Service) RemoveTeacher(ctx context.Context, actorID, classroomID, userID string) error {
	c, err := s.GetClassroom(ctx, actorID, classroomID)
	if errors.Is(err, httpx.ErrNotFound) && actorID == userID {
		// Declining an invitation: they don't teach it yet.
		return s.answer(ctx, actorID, classroomID, false)
	}
	if err != nil {
		return err
	}
	if c.Role != "owner" && actorID != userID {
		return errOwnerOnly
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM content.classroom_teachers WHERE classroom_id = $1 AND user_id = $2`, classroomID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		action := "coteacher_removed"
		if actorID == userID {
			action = "coteacher_left"
		}
		return audit.Log(ctx, tx, actorID, action, "classroom", classroomID, map[string]any{"teacher": userID})
	})
}

// Invitations lists the classrooms a teacher has been invited to and not answered.
func (s *Service) Invitations(ctx context.Context, teacherID string) ([]Invitation, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.name, coalesce(u.name, ''), t.created_at
		FROM content.classroom_teachers t JOIN content.classrooms c ON c.id = t.classroom_id LEFT JOIN auth.users u ON u.id = c.teacher_id
		WHERE t.user_id = $1 AND t.status = 'invited' AND c.archived_at IS NULL ORDER BY t.created_at DESC`, teacherID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Invitation, error) {
		var i Invitation
		err := r.Scan(&i.ClassroomID, &i.Classroom, &i.OwnerName, &i.InvitedAt)
		return i, err
	})
	if out == nil {
		out = []Invitation{}
	}
	return out, err
}

// answer accepts or declines an invitation.
func (s *Service) answer(ctx context.Context, teacherID, classroomID string, accept bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var tag interface{ RowsAffected() int64 }
		var err error
		if accept {
			tag, err = tx.Exec(ctx, `UPDATE content.classroom_teachers SET status = 'active', accepted_at = now()
				WHERE classroom_id = $1 AND user_id = $2 AND status = 'invited'`, classroomID, teacherID)
		} else {
			tag, err = tx.Exec(ctx, `DELETE FROM content.classroom_teachers WHERE classroom_id = $1 AND user_id = $2 AND status = 'invited'`, classroomID, teacherID)
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		action := "coteacher_declined"
		if accept {
			action = "coteacher_joined"
		}
		return audit.Log(ctx, tx, teacherID, action, "classroom", classroomID, nil)
	})
}

// TeacherRoutesCo mounts the co-teacher endpoints under /api/teacher.
func (s *Service) teacherRoutesCo(r chi.Router) {
	r.Method("GET", "/classrooms/{id}/teachers", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.Teachers(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"teachers": list})
		return nil
	}))
	r.Method("POST", "/classrooms/{id}/teachers", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Email string `json:"email"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		t, err := s.Invite(r.Context(), uid(r), chi.URLParam(r, "id"), in.Email)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, t)
		return nil
	}))
	r.Method("DELETE", "/classrooms/{id}/teachers/{user}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.RemoveTeacher(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "user")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("GET", "/invitations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.Invitations(r.Context(), uid(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"invitations": list})
		return nil
	}))
	for path, accept := range map[string]bool{"accept": true, "decline": false} {
		r.Method("POST", "/invitations/{id}/"+path, httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			if err := s.answer(r.Context(), uid(r), chi.URLParam(r, "id"), accept); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
	}
}
