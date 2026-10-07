package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// CookieName uses the __Host- prefix: Secure, no Domain, Path=/ (ADR-13).
const CookieName = "__Host-sid"

type ctxKey struct{}

// WithUser stores the authenticated user in ctx (also used by tests).
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// CurrentUser returns the authenticated user, if any.
func CurrentUser(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

// MustUser returns the authenticated user; use only behind RequireUser.
func MustUser(ctx context.Context) User {
	u, _ := CurrentUser(ctx)
	return u
}

// Middleware resolves the session cookie, if present, into a user on the context.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err == nil {
			u, ok, err := s.Authenticate(r.Context(), c.Value)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			if ok {
				r = r.WithContext(WithUser(r.Context(), u))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole rejects requests without a logged-in user of one of roles.
// With no roles, any logged-in user passes.
func RequireRole(roles ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := CurrentUser(r.Context())
			if !ok {
				httpx.WriteError(w, r, httpx.ErrUnauthorized)
				return
			}
			if len(roles) > 0 {
				allowed := false
				for _, role := range roles {
					allowed = allowed || u.Role == role
				}
				if !allowed {
					httpx.WriteError(w, r, httpx.ErrForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", Expires: expires,
		Secure: true, HttpOnly: true,
		// Lax, not Strict, so the session survives opening the quiz from a
		// camera app or QR scanner link (ADR-13); CSRF is covered by SameOrigin.
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// Routes mounts the public auth API under /api/auth.
func (s *Service) Routes(r chi.Router) {
	r.Method("POST", "/register", httpx.Handler(s.handleRegister))
	r.Method("POST", "/login", httpx.Handler(s.handleLogin))
	r.Method("POST", "/logout", httpx.Handler(s.handleLogout))
	r.Method("GET", "/me", httpx.Handler(s.handleMe))
	r.Method("DELETE", "/me", httpx.Handler(s.handleDeleteMe))
	s.accountRoutes(r)
	r.Method("POST", "/verify-email", httpx.Handler(s.handleVerifyEmail))
	r.Method("POST", "/password-reset/request", httpx.Handler(s.handleResetRequest))
	r.Method("POST", "/password-reset/confirm", httpx.Handler(s.handleResetConfirm))
}

// AdminRoutes mounts account management under /api/admin (caller enforces admin role).
func (s *Service) AdminRoutes(r chi.Router) {
	r.Method("GET", "/users", httpx.Handler(s.handleListUsers))
	r.Method("POST", "/users/{id}/status", httpx.Handler(s.handleSetStatus))
	r.Method("DELETE", "/users/{id}", httpx.Handler(s.handleDeleteUser))
	r.Method("GET", "/auth-policy", httpx.Handler(s.handleGetPolicy))
	r.Method("PUT", "/auth-policy", httpx.Handler(s.handleSetPolicy))
}

func writeBusy(w http.ResponseWriter, err error) bool {
	if errors.Is(err, ErrBusy) {
		w.Header().Set("Retry-After", "2")
		httpx.JSON(w, http.StatusServiceUnavailable, httpx.NewError(503, "busy", "many people are logging in at once, retrying shortly"))
		return true
	}
	return false
}

func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) error {
	var in RegisterInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if !s.ipLimiter.Allow("ip:" + httpx.ClientIP(r)) {
		return errRateLimited
	}
	u, err := s.Register(r.Context(), in)
	if writeBusy(w, err) {
		return nil
	}
	if err != nil {
		return err
	}
	if u.Status == StatusActive {
		// Log straight in so a student who registers from the QR page
		// returns to the session without a second form (FR-ACC-06).
		token, exp, err := s.CreateSession(r.Context(), u, r.UserAgent())
		if err != nil {
			return err
		}
		setSessionCookie(w, token, exp)
	}
	httpx.JSON(w, http.StatusCreated, u)
	return nil
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var in LoginInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	u, token, exp, err := s.Login(r.Context(), in, httpx.ClientIP(r), r.UserAgent())
	if writeBusy(w, err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Rotate: drop the session the browser held before this login.
	if old, err := r.Cookie(CookieName); err == nil {
		_ = s.Logout(r.Context(), old.Value)
	}
	setSessionCookie(w, token, exp)
	httpx.JSON(w, http.StatusOK, u)
	return nil
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(CookieName); err == nil {
		if err := s.Logout(r.Context(), c.Value); err != nil {
			return err
		}
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) error {
	u, ok := CurrentUser(r.Context())
	if !ok {
		return httpx.ErrUnauthorized
	}
	httpx.JSON(w, http.StatusOK, u)
	return nil
}

func (s *Service) handleVerifyEmail(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token string `json:"token"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.VerifyEmail(r.Context(), in.Token); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handleResetRequest(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.RequestPasswordReset(r.Context(), in.Email, httpx.ClientIP(r)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func (s *Service) handleResetConfirm(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	err := s.ResetPassword(r.Context(), in.Token, in.Password)
	if writeBusy(w, err) {
		return nil
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handleListUsers(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	users, err := s.ListUsers(r.Context(), UserFilter{Role: q.Get("role"), Status: q.Get("status"), Query: q.Get("q"), Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"users": users})
	return nil
}

func (s *Service) handleSetStatus(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Status string `json:"status"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.SetStatus(r.Context(), MustUser(r.Context()).ID, chi.URLParam(r, "id"), in.Status); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handleDeleteUser(w http.ResponseWriter, r *http.Request) error {
	if err := s.DeleteUser(r.Context(), MustUser(r.Context()).ID, chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handleGetPolicy(w http.ResponseWriter, r *http.Request) error {
	p, err := s.GetPolicy(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, p)
	return nil
}

func (s *Service) handleSetPolicy(w http.ResponseWriter, r *http.Request) error {
	var p Policy
	if err := httpx.Decode(w, r, &p); err != nil {
		return err
	}
	if err := s.SetPolicy(r.Context(), MustUser(r.Context()).ID, p); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, p)
	return nil
}

func (s *Service) handleDeleteMe(w http.ResponseWriter, r *http.Request) error {
	u, ok := CurrentUser(r.Context())
	if !ok {
		return httpx.ErrUnauthorized
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	err := s.DeleteOwnAccount(r.Context(), u, in.Password)
	if writeBusy(w, err) {
		return nil
	}
	if err != nil {
		return err
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
