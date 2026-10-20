package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Directory sign-in (D-64): students and teachers sign in with their school
// account (Windows Active Directory or another LDAP directory). Their first
// sign-in creates the account, with the role their directory groups give.
// Admin and manager accounts always keep their own password, so the platform
// can still be reached when the directory can't.

// DirectoryUser is what the directory says about someone who just proved
// their password.
type DirectoryUser struct {
	ID, DN, Email, Name string
	Role                Role
	// LinkByEmail: an existing local account with the same email may move to
	// directory sign-in (the directory's setting).
	LinkByEmail bool
}

// Directory checks a sign-in name and password against the directory.
type Directory interface {
	Enabled(ctx context.Context) bool
	Authenticate(ctx context.Context, login, password string) (DirectoryUser, error)
}

// Errors a Directory returns, beyond a wrong name or password.
var (
	ErrDirectoryCredentials = errors.New("directory: invalid credentials")
	ErrDirectoryDown        = httpx.NewError(http.StatusServiceUnavailable, "directory_unavailable", "the school directory can't be reached right now; try again shortly")
	ErrDirectoryNotAllowed  = httpx.NewError(http.StatusForbidden, "directory_not_allowed", "your school account isn't allowed to use this platform; ask your administrator")
	ErrDirectoryNoEmail     = httpx.NewError(http.StatusForbidden, "directory_no_email", "your school account has no email address; ask your administrator")
	errDirectoryConflict    = httpx.NewError(http.StatusConflict, "directory_conflict", "an account with this email already exists; ask an administrator to move it to school sign-in")
	errDirectoryAccount     = httpx.BadRequest("your password belongs to your school account; change it the way your school does")
)

// SetDirectory turns directory sign-in on (D-64).
func (s *Service) SetDirectory(d Directory) { s.dir = d }

// DirectoryEnabled tells the login page to ask for a school account too.
func (s *Service) DirectoryEnabled(ctx context.Context) bool {
	return s.dir != nil && s.dir.Enabled(ctx)
}

func (s *Service) directoryLogin(ctx context.Context, login, password string) (User, error) {
	du, err := s.dir.Authenticate(ctx, login, password)
	if errors.Is(err, ErrDirectoryCredentials) {
		return User{}, errInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	return s.provisionDirectory(ctx, du)
}

// provisionDirectory finds or creates the account for a directory user and
// brings its email, name, role and place in the directory up to date. The
// directory is the source of truth for all four.
func (s *Service) provisionDirectory(ctx context.Context, du DirectoryUser) (User, error) {
	var u User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var source string
		var role Role
		err := tx.QueryRow(ctx, `SELECT id, role, auth_source FROM auth.users WHERE directory_id=$1 AND status <> 'deleted' FOR UPDATE`, du.ID).Scan(&u.ID, &role, &source)
		if errors.Is(err, pgx.ErrNoRows) {
			// Not seen before: an existing account with the same email, or a new one.
			var dirID *string
			var isManager bool
			err = tx.QueryRow(ctx, `SELECT u.id, u.role, u.auth_source, u.directory_id, m.user_id IS NOT NULL FROM auth.users u LEFT JOIN auth.managers m ON m.user_id = u.id
				WHERE u.email=$1 AND u.status <> 'deleted' FOR UPDATE OF u`, du.Email).Scan(&u.ID, &role, &source, &dirID, &isManager)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				err = tx.QueryRow(ctx, `INSERT INTO auth.users(email, name, role, status, password_hash, email_verified_at, auth_source, directory_id, directory_dn)
					VALUES ($1,$2,$3,'active','!',$4,'directory',$5,$6) RETURNING id`,
					du.Email, du.Name, du.Role, s.now(), du.ID, du.DN).Scan(&u.ID)
				if err != nil {
					return err
				}
				return audit.Log(ctx, tx, u.ID, "directory_account_created", "user", u.ID, map[string]string{"role": string(du.Role)})
			case err != nil:
				return err
			}
			// Admins and managers keep their own password (break-glass), an
			// account another directory entry owns stays with it, and a
			// student account doesn't become a teacher's by email alone.
			if role == RoleAdmin || role == RoleManager || isManager || source == "directory" || !du.LinkByEmail || role != du.Role {
				return errDirectoryConflict
			}
			if _, err := tx.Exec(ctx, `UPDATE auth.users SET auth_source='directory', password_hash='!', directory_id=$2, directory_dn=$3,
				email_verified_at=coalesce(email_verified_at,$4), updated_at=$4 WHERE id=$1`, u.ID, du.ID, du.DN, s.now()); err != nil {
				return err
			}
			if err := audit.Log(ctx, tx, u.ID, "directory_account_linked", "user", u.ID, nil); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		// Seen before: follow the directory. An email taken by another account stays as it was.
		if _, err := tx.Exec(ctx, `UPDATE auth.users SET name=$2, directory_dn=$3, role=$4, updated_at=$5 WHERE id=$1`, u.ID, du.Name, du.DN, du.Role, s.now()); err != nil {
			return err
		}
		if role != du.Role {
			if err := audit.Log(ctx, tx, u.ID, "directory_role_changed", "user", u.ID, map[string]string{"from": string(role), "to": string(du.Role)}); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE auth.users SET email=$2 WHERE id=$1 AND email <> $2
			AND NOT EXISTS (SELECT 1 FROM auth.users o WHERE o.email=$2 AND o.id <> $1 AND o.status <> 'deleted')`, u.ID, du.Email)
		return err
	})
	if err != nil {
		return User{}, err
	}
	s.dropUserCache(u.ID)
	return s.GetUser(ctx, u.ID)
}

// DirectoryAccount is a directory account that is signed in somewhere.
type DirectoryAccount struct {
	ID, DN string
	Role   Role
}

// SignedInDirectoryAccounts lists directory accounts with live sessions, for
// the hourly check against the directory.
func (s *Service) SignedInDirectoryAccounts(ctx context.Context) ([]DirectoryAccount, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT u.id, coalesce(u.directory_dn, ''), u.role FROM auth.users u JOIN auth.sessions s ON s.user_id = u.id
		WHERE u.auth_source='directory' AND u.status='active' AND s.expires_at > $1`, s.now())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DirectoryAccount, error) {
		var a DirectoryAccount
		return a, r.Scan(&a.ID, &a.DN, &a.Role)
	})
}

// SignOutDirectoryAccount ends every session of an account the directory
// no longer lets in (disabled, removed, or out of the groups).
func (s *Service) SignOutDirectoryAccount(ctx context.Context, userID, reason string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM auth.sessions WHERE user_id=$1`, userID); err != nil {
			return err
		}
		s.dropUserCache(userID)
		return audit.Log(ctx, tx, userID, "directory_signed_out", "user", userID, map[string]string{"reason": reason})
	})
}

// verifyDirectoryPassword re-checks a directory account's password, for
// deleting one's own account.
func (s *Service) verifyDirectoryPassword(ctx context.Context, u User, password string) error {
	if !s.DirectoryEnabled(ctx) {
		return httpx.BadRequest("school sign-in is off; ask an administrator to remove your account")
	}
	du, err := s.dir.Authenticate(ctx, u.Email, password)
	if errors.Is(err, ErrDirectoryCredentials) {
		return errInvalidCredentials
	}
	if err != nil {
		return err
	}
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(directory_id, '') FROM auth.users WHERE id=$1`, u.ID).Scan(&id); err != nil {
		return err
	}
	if id != du.ID {
		return errInvalidCredentials
	}
	return nil
}
