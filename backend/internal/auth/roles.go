package auth

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Changing someone's role (D-101). An admin sets anyone but themself to
// student, teacher or admin; a manager with "Manage users" switches only
// students and teachers (PL-FR-13), never making an admin. The last active
// admin stays one (PL-FR-17); a teacher who still owns classrooms isn't
// made a student until those move to another teacher; and a manager keeps
// their manager role only while they're a teacher.

var (
	errOwnsClassrooms = httpx.NewError(http.StatusConflict, "owns_classrooms", "this teacher still owns classrooms: move them to another teacher (Admin → Classes) first")
	errStillManager   = httpx.NewError(http.StatusConflict, "is_manager", "this teacher is a manager: remove their manager role first")
)

// SetRole changes a user's role.
func (s *Service) SetRole(ctx context.Context, actor User, userID string, role Role) error {
	if role != RoleStudent && role != RoleTeacher && role != RoleAdmin {
		return httpx.BadRequest("role must be student, teacher or admin")
	}
	if actor.ID == userID {
		return httpx.BadRequest("you cannot change your own role")
	}
	if role == RoleAdmin && actor.Role != RoleAdmin {
		return httpx.NewError(http.StatusForbidden, "forbidden", "only an admin can make someone an admin")
	}
	return s.actOnUser(ctx, actor, userID, "change_role", func(tx pgx.Tx, t targetUser) error {
		if t.role == role {
			return nil
		}
		if t.role == RoleAdmin {
			if err := lastAdminCheck(ctx, tx, userID); err != nil {
				return err
			}
		}
		if t.role == RoleTeacher && role == RoleStudent {
			var owned int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM content.classrooms WHERE teacher_id=$1`, userID).Scan(&owned); err != nil {
				return err
			}
			if owned > 0 {
				return errOwnsClassrooms
			}
		}
		if t.manager && role != RoleTeacher {
			return errStillManager
		}
		if _, err := tx.Exec(ctx, `UPDATE auth.users SET role=$2, updated_at=now() WHERE id=$1`, userID, role); err != nil {
			return err
		}
		s.dropUserCache(userID)
		return audit.Log(ctx, tx, actor.ID, "user_role_changed", "user", userID, map[string]string{"from": string(t.role), "to": string(role)})
	})
}

func (s *Service) handleSetRole(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Role Role `json:"role"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.SetRole(r.Context(), MustUser(r.Context()), chi.URLParam(r, "id"), in.Role); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ActiveTeacherByEmail finds an active teacher (or admin) to own classrooms.
func (s *Service) ActiveTeacherByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `SELECT id, email, name, role FROM auth.users WHERE email=lower(trim($1)) AND status='active' AND role IN ('teacher', 'admin')`, email).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role)
	if err == pgx.ErrNoRows {
		return u, httpx.Invalid(map[string]string{"teacher": "no active teacher with that email address"})
	}
	return u, err
}
