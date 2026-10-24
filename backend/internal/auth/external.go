package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Sign-in with an outside identity provider (OpenID Connect, D-70). The oidc
// module proves who someone is; this file decides which account that is. An
// account keeps its own password, if it has one: an outside sign-in is one
// more way in, not a replacement. Roles never follow the provider after the
// account exists.

// ExternalUser is someone an identity provider has just vouched for.
type ExternalUser struct {
	ProviderID, Subject string
	Email, Name         string
	EmailVerified       bool
	// NewRole is the role of an account created on first sign-in; "" creates none.
	NewRole Role
	// LinkByEmail: a first sign-in may join the student or teacher account
	// with the same, verified, email.
	LinkByEmail bool
}

// Why an outside sign-in found no account. Each has a code the login page explains.
var (
	ErrExternalNoAccount  = httpx.NewError(http.StatusForbidden, "sso_no_account", "there is no account for this sign-in; ask your teacher or administrator to create one")
	ErrExternalUnverified = httpx.NewError(http.StatusForbidden, "sso_email_unverified", "the provider didn't confirm this email address")
	ErrExternalConflict   = httpx.NewError(http.StatusConflict, "sso_conflict", "an account with this email already exists; sign in with its password, then connect this sign-in from your account page")
	ErrExternalTaken      = httpx.NewError(http.StatusConflict, "sso_taken", "this sign-in already belongs to another account")
	errExternalLastWay    = httpx.BadRequest("this is the only way into your account: set a password first (Forgot password on the login page)")
)

// SignInExternal returns the account for an outside sign-in, linking or
// creating it as the provider's settings allow. The caller checks the status.
func (s *Service) SignInExternal(ctx context.Context, eu ExternalUser) (User, error) {
	eu.Email = strings.ToLower(strings.TrimSpace(eu.Email))
	var id string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT i.user_id FROM auth.identities i JOIN auth.users u ON u.id = i.user_id
			WHERE i.provider_id=$1 AND i.subject=$2 AND u.status <> 'deleted'`, eu.ProviderID, eu.Subject).Scan(&id)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE auth.identities SET last_used_at=$3, email=$4 WHERE provider_id=$1 AND subject=$2`, eu.ProviderID, eu.Subject, s.now(), eu.Email)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// A sign-in left over from a deleted account starts again.
		if _, err := tx.Exec(ctx, `DELETE FROM auth.identities WHERE provider_id=$1 AND subject=$2`, eu.ProviderID, eu.Subject); err != nil {
			return err
		}
		if eu.Email == "" || !eu.EmailVerified {
			return ErrExternalUnverified
		}
		var role Role
		var source string
		var privileged bool
		err = tx.QueryRow(ctx, `SELECT u.id, u.role, u.auth_source, m.user_id IS NOT NULL FROM auth.users u LEFT JOIN auth.managers m ON m.user_id = u.id
			WHERE u.email=$1 AND u.status <> 'deleted' FOR UPDATE OF u`, eu.Email).Scan(&id, &role, &source, &privileged)
		switch {
		case err == nil:
			// Admins and managers, and school directory accounts, are never
			// joined by email alone: they connect from their account page.
			if !eu.LinkByEmail || privileged || role == RoleAdmin || role == RoleManager || source == "directory" {
				return ErrExternalConflict
			}
			if err := s.addIdentity(ctx, tx, id, eu); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE auth.users SET email_verified_at=coalesce(email_verified_at,$2) WHERE id=$1`, id, s.now()); err != nil {
				return err
			}
			return audit.Log(ctx, tx, id, "sso_linked", "user", id, map[string]string{"provider": eu.ProviderID, "by": "email"})
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if eu.NewRole != RoleStudent && eu.NewRole != RoleTeacher {
			return ErrExternalNoAccount
		}
		status := StatusActive
		if eu.NewRole == RoleTeacher {
			var approval bool
			if err := tx.QueryRow(ctx, `SELECT require_teacher_approval FROM auth.policy`).Scan(&approval); err != nil {
				return err
			}
			if approval {
				status = StatusPendingApproval
			}
		}
		name := strings.TrimSpace(eu.Name)
		if name == "" {
			name, _, _ = strings.Cut(eu.Email, "@")
		}
		if r := []rune(name); len(r) > 100 {
			name = string(r[:100])
		}
		// '!' is no password: the account signs in through the provider until
		// its owner sets one with "Forgot password".
		if err := tx.QueryRow(ctx, `INSERT INTO auth.users(email, name, role, status, password_hash, email_verified_at)
			VALUES ($1,$2,$3,$4,'!',$5) RETURNING id`, eu.Email, name, eu.NewRole, status, s.now()).Scan(&id); err != nil {
			return err
		}
		if err := s.addIdentity(ctx, tx, id, eu); err != nil {
			return err
		}
		return audit.Log(ctx, tx, id, "sso_account_created", "user", id, map[string]string{"provider": eu.ProviderID, "role": string(eu.NewRole)})
	})
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, id)
}

func (s *Service) addIdentity(ctx context.Context, tx pgx.Tx, userID string, eu ExternalUser) error {
	tag, err := tx.Exec(ctx, `INSERT INTO auth.identities(provider_id, subject, user_id, email, created_at, last_used_at) VALUES ($1,$2,$3,$4,$5,$5)
		ON CONFLICT (provider_id, user_id) DO NOTHING`, eu.ProviderID, eu.Subject, userID, eu.Email, s.now())
	if err != nil {
		if isUniqueViolation(err) {
			return ErrExternalTaken
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		// The account already has another sign-in with this provider.
		return ErrExternalTaken
	}
	return nil
}

// LinkExternal connects an outside sign-in to a signed-in account, from its
// account page. Any account may, admins included: they chose to.
func (s *Service) LinkExternal(ctx context.Context, userID string, eu ExternalUser) error {
	eu.Email = strings.ToLower(strings.TrimSpace(eu.Email))
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var owner string
		err := tx.QueryRow(ctx, `SELECT user_id FROM auth.identities WHERE provider_id=$1 AND subject=$2`, eu.ProviderID, eu.Subject).Scan(&owner)
		switch {
		case err == nil && owner == userID:
			return nil
		case err == nil:
			return ErrExternalTaken
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM auth.identities WHERE provider_id=$1 AND user_id=$2`, eu.ProviderID, userID); err != nil {
			return err
		}
		if err := s.addIdentity(ctx, tx, userID, eu); err != nil {
			return err
		}
		return audit.Log(ctx, tx, userID, "sso_linked", "user", userID, map[string]string{"provider": eu.ProviderID, "by": "account_page"})
	})
}

// SSOOption is a sign-in button on the login page.
type SSOOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SetSSO gives the login page its outside sign-in buttons (D-70).
func (s *Service) SetSSO(list func(context.Context) []SSOOption) { s.sso = list }

// Identity is one outside sign-in of an account.
type Identity struct {
	ProviderID string    `json:"provider_id"`
	Email      string    `json:"email"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

func (s *Service) Identities(ctx context.Context, userID string) ([]Identity, error) {
	rows, err := s.pool.Query(ctx, `SELECT provider_id, email, created_at, last_used_at FROM auth.identities WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Identity, error) {
		var i Identity
		return i, r.Scan(&i.ProviderID, &i.Email, &i.CreatedAt, &i.LastUsedAt)
	})
	if out == nil {
		out = []Identity{}
	}
	return out, err
}

// UnlinkExternal removes an outside sign-in, unless it's the only way in.
func (s *Service) UnlinkExternal(ctx context.Context, userID, providerID string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var hash, source string
		var others int
		if err := tx.QueryRow(ctx, `SELECT password_hash, auth_source, (SELECT count(*) FROM auth.identities WHERE user_id=$1 AND provider_id <> $2)
			FROM auth.users WHERE id=$1 FOR UPDATE`, userID, providerID).Scan(&hash, &source, &others); err != nil {
			return err
		}
		if hash == "!" && source != "directory" && others == 0 {
			return errExternalLastWay
		}
		tag, err := tx.Exec(ctx, `DELETE FROM auth.identities WHERE user_id=$1 AND provider_id=$2`, userID, providerID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(ctx, tx, userID, "sso_unlinked", "user", userID, map[string]string{"provider": providerID})
	})
}

// StartSession checks an account may sign in now and opens a session, as a
// password login does.
func (s *Service) StartSession(ctx context.Context, u User, userAgent string) (string, time.Time, error) {
	switch u.Status {
	case StatusSuspended:
		return "", time.Time{}, errSuspended
	case StatusPendingApproval:
		return "", time.Time{}, errPendingApproval
	case StatusActive:
		return s.CreateSession(ctx, u, userAgent)
	}
	return "", time.Time{}, ErrExternalNoAccount
}

// SetSessionCookie lets a sign-in that finishes outside this package (the
// provider's redirect) set the same cookie a login does.
func SetSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	setSessionCookie(w, token, expires)
}
