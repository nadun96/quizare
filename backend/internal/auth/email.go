package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	qmail "github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// A person's own email address (D-98): sending the verification link again,
// and changing the address, which takes effect only once the new one is
// confirmed from its own inbox.

var (
	errAlreadyVerified = httpx.NewError(http.StatusConflict, "already_verified", "your email address is already verified")
	errDirectoryEmail  = httpx.BadRequest("your email address comes from your school account; your school changes it")
)

// EmailStatus is what the account page shows about the address.
type EmailStatus struct {
	Email        string `json:"email"`
	Verified     bool   `json:"verified"`
	PendingEmail string `json:"pending_email,omitempty"`
	CanChange    bool   `json:"can_change"`
}

func (s *Service) EmailStatus(ctx context.Context, userID string) (EmailStatus, error) {
	var st EmailStatus
	var pending *string
	var source string
	err := s.pool.QueryRow(ctx, `SELECT email, email_verified_at IS NOT NULL, pending_email, auth_source FROM auth.users WHERE id=$1`, userID).
		Scan(&st.Email, &st.Verified, &pending, &source)
	if pending != nil {
		st.PendingEmail = *pending
	}
	st.CanChange = source != "directory"
	return st, err
}

// ResendVerification sends the verification link again, a few times at most.
func (s *Service) ResendVerification(ctx context.Context, u User) error {
	st, err := s.EmailStatus(ctx, u.ID)
	if err != nil {
		return err
	}
	if st.Verified {
		return errAlreadyVerified
	}
	if !s.mailLimiter.Allow("mail:" + u.ID) {
		return errRateLimited
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Earlier links stop working: only the newest one counts.
		if _, err := tx.Exec(ctx, `DELETE FROM auth.tokens WHERE user_id=$1 AND purpose='verify_email' AND used_at IS NULL`, u.ID); err != nil {
			return err
		}
		u.Email = st.Email
		return s.issueTokenTx(ctx, tx, u, "verify_email", 48*time.Hour)
	})
}

// ChangeEmail asks for a new address: the password confirms it's the
// owner, a link goes to the new address, and the old one is told.
func (s *Service) ChangeEmail(ctx context.Context, u User, newEmail, password string) error {
	st, err := s.EmailStatus(ctx, u.ID)
	if err != nil {
		return err
	}
	if !st.CanChange {
		return errDirectoryEmail
	}
	newEmail = strings.ToLower(strings.TrimSpace(newEmail))
	if a, err := mail.ParseAddress(newEmail); err != nil || a.Address != newEmail || len(newEmail) > 254 {
		return httpx.Invalid(map[string]string{"email": "enter a valid email address"})
	}
	if newEmail == st.Email {
		return httpx.Invalid(map[string]string{"email": "that is already your email address"})
	}
	if !s.emailLimiter.Allow("pwchange:" + u.ID) {
		return errRateLimited // guessing the password is throttled like logins
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
		return httpx.Invalid(map[string]string{"password": "that isn't your password"})
	}
	var taken bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM auth.users WHERE email=$1)`, newEmail).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return httpx.Invalid(map[string]string{"email": "another account uses this email address"})
	}
	if !s.mailLimiter.Allow("mail:" + u.ID) {
		return errRateLimited
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE auth.users SET pending_email=$2, updated_at=$3 WHERE id=$1`, u.ID, newEmail, s.now()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM auth.tokens WHERE user_id=$1 AND purpose='change_email' AND used_at IS NULL`, u.ID); err != nil {
			return err
		}
		to := u
		to.Email = newEmail
		if err := s.issueTokenTx(ctx, tx, to, "change_email", 48*time.Hour); err != nil {
			return err
		}
		// The old address hears about it, in case it wasn't its owner asking.
		notice := qmail.Message{To: st.Email, Subject: "Your email address is being changed",
			Body: fmt.Sprintf("Hi %s,\n\nSomeone signed in to your account at %s asked to change its email address to %s. It changes only once that address confirms.\n\nIf this wasn't you, change your password now and cancel the change on your account page.", u.Name, s.baseURL, newEmail)}
		if _, err := s.jobs.InsertTx(ctx, tx, qmail.Args{Message: notice}, nil); err != nil {
			return err
		}
		return audit.Log(ctx, tx, u.ID, "email_change_requested", "user", u.ID, map[string]string{"to": newEmail})
	})
}

// CancelEmailChange drops a change not yet confirmed.
func (s *Service) CancelEmailChange(ctx context.Context, u User) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE auth.users SET pending_email=NULL WHERE id=$1`, u.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM auth.tokens WHERE user_id=$1 AND purpose='change_email' AND used_at IS NULL`, u.ID)
		return err
	})
}

// confirmEmailChange follows the link sent to a new address: it becomes
// the account's address, verified.
func (s *Service) confirmEmailChange(ctx context.Context, tx pgx.Tx, token string) error {
	id, err := consumeToken(ctx, tx, token, "change_email", s.now())
	if err != nil {
		return err
	}
	var email string
	err = tx.QueryRow(ctx, `UPDATE auth.users SET email=pending_email, pending_email=NULL, email_verified_at=$2, updated_at=$2
		WHERE id=$1 AND pending_email IS NOT NULL RETURNING email`, id, s.now()).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return errBadToken // cancelled meanwhile
	}
	if isUniqueViolation(err) {
		return httpx.Conflict("another account has taken this email address meanwhile")
	}
	if err != nil {
		return err
	}
	s.dropUserCache(id)
	return audit.Log(ctx, tx, id, "email_changed", "user", id, map[string]string{"to": email})
}

// UpdateProfile changes a person's own name (their picture has its own routes).
func (s *Service) UpdateProfile(ctx context.Context, u User, name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if n := len([]rune(name)); n < 1 || n > 100 {
		return "", httpx.Invalid(map[string]string{"name": "name must be 1-100 characters"})
	}
	var source string
	if err := s.pool.QueryRow(ctx, `SELECT auth_source FROM auth.users WHERE id=$1`, u.ID).Scan(&source); err != nil {
		return "", err
	}
	if source == "directory" {
		return "", httpx.BadRequest("your name comes from your school account; your school changes it")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE auth.users SET name=$2, updated_at=$3 WHERE id=$1`, u.ID, name, s.now()); err != nil {
		return "", err
	}
	s.dropUserCache(u.ID)
	return name, nil
}

func (s *Service) emailRoutes(r chi.Router) {
	me := func(r *http.Request) (User, error) {
		u, ok := CurrentUser(r.Context())
		if !ok {
			return User{}, httpx.ErrUnauthorized
		}
		return u, nil
	}
	done := func(w http.ResponseWriter, err error) error {
		if writeBusy(w, err) {
			return nil
		}
		if err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	r.Method("GET", "/me/email", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, err := me(r)
		if err != nil {
			return err
		}
		st, err := s.EmailStatus(r.Context(), u.ID)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, st)
		return nil
	}))
	r.Method("POST", "/me/email/verify", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, err := me(r)
		if err != nil {
			return err
		}
		return done(w, s.ResendVerification(r.Context(), u))
	}))
	r.Method("PUT", "/me/email", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, err := me(r)
		if err != nil {
			return err
		}
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return done(w, s.ChangeEmail(r.Context(), u, in.Email, in.Password))
	}))
	r.Method("DELETE", "/me/email/pending", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, err := me(r)
		if err != nil {
			return err
		}
		return done(w, s.CancelEmailChange(r.Context(), u))
	}))
	r.Method("PUT", "/me/profile", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, err := me(r)
		if err != nil {
			return err
		}
		var in struct {
			Name string `json:"name"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		name, err := s.UpdateProfile(r.Context(), u, in.Name)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"name": name})
		return nil
	}))
}
