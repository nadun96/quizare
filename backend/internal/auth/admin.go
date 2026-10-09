package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// Admin account management (FR-ACC-04, FR-ACC-05). Admins manage accounts but
// never see password hashes, API keys or quiz content (BA §4).

type UserFilter struct {
	Role   string
	Status string
}

// UserSorts are the columns the admin's user list sorts by (PL-FR-01).
var UserSorts = page.Sorts{"created": "created_at", "name": "lower(name)", "email": "lower(email)", "role": "role", "status": "status"}

// ListUsers returns one page of users matching the filter and search, and
// how many match in all.
func (s *Service) ListUsers(ctx context.Context, f UserFilter, p page.Request) ([]User, int, error) {
	where := ` FROM auth.users WHERE ($1 = '' OR role = $1) AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR email ILIKE $3 OR name ILIKE $3)`
	args := []any{f.Role, f.Status, p.Like()}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, email, name, role, status, email_verified_at IS NOT NULL, created_at`+where+
		p.OrderBy(UserSorts, "id")+p.Limit(), args...)
	if err != nil {
		return nil, 0, err
	}
	users, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (User, error) {
		var u User
		err := r.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &u.EmailVerified, &u.CreatedAt)
		return u, err
	})
	return users, total, err
}

// SetStatus activates (or approves), or suspends an account. Suspending
// revokes all of the user's sessions immediately.
func (s *Service) SetStatus(ctx context.Context, actorID, userID, status string) error {
	if status != StatusActive && status != StatusSuspended {
		return httpx.BadRequest("status must be active or suspended")
	}
	if actorID == userID {
		return httpx.BadRequest("you cannot change your own status")
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var prev string
		err := tx.QueryRow(ctx, `SELECT status FROM auth.users WHERE id=$1 AND status <> 'deleted' FOR UPDATE`, userID).Scan(&prev)
		if err == pgx.ErrNoRows {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE auth.users SET status=$2, updated_at=now() WHERE id=$1`, userID, status); err != nil {
			return err
		}
		if status == StatusSuspended {
			if _, err := tx.Exec(ctx, `DELETE FROM auth.sessions WHERE user_id=$1`, userID); err != nil {
				return err
			}
		}
		s.dropUserCache(userID)
		return audit.Log(ctx, tx, actorID, "user_status_changed", "user", userID, map[string]string{"from": prev, "to": status})
	})
}

// DeleteUser anonymises the account: personal data is scrubbed and login is
// impossible, but attempts and answers remain for the teacher's records
// (NFR-04 deletion on request; BR-16 keeps results).
func (s *Service) DeleteUser(ctx context.Context, actorID, userID string) error {
	if actorID == userID {
		return httpx.BadRequest("you cannot delete your own account here")
	}
	return s.anonymise(ctx, actorID, userID)
}

// DeleteOwnAccount lets a student or teacher delete their account after
// re-entering their password (NFR-04). Admin accounts are removed by another admin.
func (s *Service) DeleteOwnAccount(ctx context.Context, u User, password string) error {
	if u.Role == RoleAdmin {
		return httpx.BadRequest("ask another admin to remove an admin account")
	}
	var hash string
	if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM auth.users WHERE id=$1`, u.ID).Scan(&hash); err != nil {
		return err
	}
	ok, err := s.hasher.Verify(ctx, password, hash)
	if err != nil {
		return err
	}
	if !ok {
		return errInvalidCredentials
	}
	return s.anonymise(ctx, u.ID, u.ID)
}

func (s *Service) anonymise(ctx context.Context, actorID, userID string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE auth.users
			SET status='deleted', email=$2, name='Deleted user', password_hash='!', email_verified_at=NULL, avatar_version=NULL, updated_at=now()
			WHERE id=$1 AND status <> 'deleted'`, userID, fmt.Sprintf("deleted+%s@invalid", userID))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM auth.sessions WHERE user_id=$1`, userID); err != nil {
			return err
		}
		// The profile picture is personal data too (D-49).
		if _, err := tx.Exec(ctx, `DELETE FROM auth.avatars WHERE user_id=$1`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM auth.tokens WHERE user_id=$1`, userID); err != nil {
			return err
		}
		s.dropUserCache(userID)
		return audit.Log(ctx, tx, actorID, "user_deleted", "user", userID, nil)
	})
}

type Policy struct {
	RequireTeacherApproval bool `json:"require_teacher_approval"`
}

func (s *Service) GetPolicy(ctx context.Context) (Policy, error) {
	var p Policy
	err := s.pool.QueryRow(ctx, `SELECT require_teacher_approval FROM auth.policy`).Scan(&p.RequireTeacherApproval)
	return p, err
}

func (s *Service) SetPolicy(ctx context.Context, actorID string, p Policy) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE auth.policy SET require_teacher_approval=$1`, p.RequireTeacherApproval); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "auth_policy_changed", "policy", "auth", p)
	})
}
