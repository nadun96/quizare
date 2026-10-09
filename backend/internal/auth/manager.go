package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	qmail "github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// Managers (PO-27 to PO-29, PL-FR-10 to PL-FR-17, D-56): the admin gives
// chosen people some admin features. A manager is a teacher who keeps
// teaching, or a manager-only account; the admin decides when assigning.

// Feature is one admin feature a manager can be given (PL-FR-10).
type Feature string

const (
	FeatViewUsers      Feature = "view_users"
	FeatManageUsers    Feature = "manage_users"
	FeatApprovalPolicy Feature = "approval_policy"
	FeatSettings       Feature = "settings"
	FeatUsage          Feature = "usage"
	FeatAudit          Feature = "audit"

	// featAdmin is never given: routes behind it are for admins only
	// (making and changing managers, PL-FR-13).
	featAdmin Feature = "admin"
)

// Features lists what can be given, in the order the admin sees them.
// Storage, clean-up, recordings, tutoring resources and backups join the
// list with their features.
var Features = []Feature{FeatViewUsers, FeatManageUsers, FeatApprovalPolicy, FeatSettings, FeatUsage, FeatAudit}

var featureLabels = map[Feature]string{
	FeatViewUsers: "View users", FeatManageUsers: "Manage users", FeatApprovalPolicy: "Teacher approval policy",
	FeatSettings: "Platform settings", FeatUsage: "Usage", FeatAudit: "Audit log",
}

// Manager is what a manager may do in the admin console.
type Manager struct {
	Features []Feature `json:"features"`
}

// Can says whether u may use an admin feature: admins always, managers
// only with that feature (PL-NFR-06).
func (u User) Can(f Feature) bool {
	if u.Role == RoleAdmin {
		return true
	}
	return u.Manager != nil && f != featAdmin && slices.Contains(u.Manager.Features, f)
}

// IsStaff says whether u may open the admin console at all.
func (u User) IsStaff() bool { return u.Role == RoleAdmin || u.Manager != nil }

// The manager columns selected beside a user (u) joined to auth.managers (m).
const (
	managerJoin   = `LEFT JOIN auth.managers m ON m.user_id = u.id`
	managerSelect = `m.user_id IS NOT NULL, coalesce(m.features, '{}')`
)

type managerCols struct {
	is       bool
	features []string
}

func (c managerCols) manager() *Manager {
	if !c.is {
		return nil
	}
	f := make([]Feature, 0, len(c.features))
	for _, x := range c.features {
		f = append(f, Feature(x))
	}
	return &Manager{Features: f}
}

// normaliseFeatures checks the features and puts them in the list's order.
// Managing users needs the user list, so it brings "View users" with it.
func normaliseFeatures(in []Feature) ([]string, error) {
	want := map[Feature]bool{}
	for _, f := range in {
		if !slices.Contains(Features, f) {
			return nil, httpx.Invalid(map[string]string{"features": fmt.Sprintf("unknown feature %q", f)})
		}
		want[f] = true
	}
	if want[FeatManageUsers] {
		want[FeatViewUsers] = true
	}
	out := []string{}
	for _, f := range Features {
		if want[f] {
			out = append(out, string(f))
		}
	}
	return out, nil
}

// ---------- server checks (PL-NFR-06) ----------

// RequireStaff lets in admins and managers; everyone else is refused.
func RequireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := CurrentUser(r.Context())
		if !ok {
			httpx.WriteError(w, r, httpx.ErrUnauthorized)
			return
		}
		if !u.IsStaff() {
			httpx.WriteError(w, r, httpx.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireFeature checks, on every request, that the caller is an admin or a
// manager with feature f. A manager without it gets 404, as for other
// people's resources, and the attempt is logged. Actions that pass are
// logged with the role the caller acted in (PL-FR-15).
func (s *Service) RequireFeature(f Feature) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := CurrentUser(r.Context())
			switch {
			case !ok:
				httpx.WriteError(w, r, httpx.ErrUnauthorized)
			case u.Role == RoleAdmin:
				next.ServeHTTP(w, r.WithContext(audit.WithActorRole(r.Context(), "admin")))
			case u.Manager == nil:
				httpx.WriteError(w, r, httpx.ErrForbidden)
			case !u.Can(f):
				ctx := audit.WithActorRole(r.Context(), "manager")
				_ = audit.Log(ctx, s.pool, u.ID, "access_refused", "feature", string(f), map[string]string{"method": r.Method, "path": r.URL.Path})
				httpx.WriteError(w, r, httpx.ErrNotFound)
			default:
				next.ServeHTTP(w, r.WithContext(audit.WithActorRole(r.Context(), "manager")))
			}
		})
	}
}

// AdminOnly is for what is never given to a manager (PL-FR-13).
func (s *Service) AdminOnly() func(http.Handler) http.Handler { return s.RequireFeature(featAdmin) }

// ---------- acting on accounts (PL-FR-13, PL-FR-17) ----------

var (
	errManagerTarget = httpx.NewError(http.StatusForbidden, "forbidden", "managers can't change admin or manager accounts")
	errLastAdmin     = httpx.NewError(http.StatusConflict, "last_admin", "At least one admin is needed")
)

type targetUser struct {
	role    Role
	status  string
	manager bool
}

// actOnUser locks the target account and runs fn on it, after checking that
// a manager acts only on teachers and students. A refused attempt is logged.
func (s *Service) actOnUser(ctx context.Context, actor User, userID, action string, fn func(pgx.Tx, targetUser) error) error {
	refused := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var t targetUser
		err := tx.QueryRow(ctx, `SELECT u.role, u.status, EXISTS (SELECT 1 FROM auth.managers m WHERE m.user_id = u.id)
			FROM auth.users u WHERE u.id=$1 AND u.status <> 'deleted' FOR UPDATE OF u`, userID).Scan(&t.role, &t.status, &t.manager)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if actor.Role != RoleAdmin && (t.role == RoleAdmin || t.role == RoleManager || t.manager) {
			refused = true
			return errManagerTarget
		}
		return fn(tx, t)
	})
	if refused {
		_ = audit.Log(ctx, s.pool, actor.ID, "access_refused", "user", userID, map[string]string{"action": action})
	}
	return err
}

// lastAdminCheck refuses to suspend, delete or demote userID when no other
// active admin would remain. The advisory lock serialises two admins acting
// on each other at once, so both can't succeed.
func lastAdminCheck(ctx context.Context, tx pgx.Tx, userID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('auth.admins'))`); err != nil {
		return err
	}
	var others int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM auth.users WHERE role='admin' AND status='active' AND id<>$1`, userID).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return errLastAdmin
	}
	return nil
}

// ---------- the managers page (PL-FR-16) ----------

// ManagerRow is one manager on the admin's managers page.
type ManagerRow struct {
	User
	GrantedAt    time.Time  `json:"granted_at"`
	GrantedBy    string     `json:"granted_by,omitempty"` // the admin's name
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
}

// Managers who were never active sort as the oldest activity.
var ManagerSorts = page.Sorts{"granted": "m.granted_at", "name": "lower(u.name)", "email": "lower(u.email)", "active": "coalesce(m.last_active_at, '-infinity')"}

// ListManagers returns one page of managers, searched by name or email.
func (s *Service) ListManagers(ctx context.Context, p page.Request) ([]ManagerRow, int, error) {
	where := ` FROM auth.managers m JOIN auth.users u ON u.id = m.user_id LEFT JOIN auth.users g ON g.id = m.granted_by
		WHERE u.status <> 'deleted' AND ($1 = '' OR u.email ILIKE $1 OR u.name ILIKE $1)`
	args := []any{p.Like()}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.email, u.name, u.role, u.status, u.email_verified_at IS NOT NULL, u.created_at,
		m.features, m.granted_at, coalesce(g.name, ''), m.last_active_at`+where+p.OrderBy(ManagerSorts, "u.id")+p.Limit(), args...)
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ManagerRow, error) {
		var m ManagerRow
		var features []string
		err := r.Scan(&m.ID, &m.Email, &m.Name, &m.Role, &m.Status, &m.EmailVerified, &m.CreatedAt, &features, &m.GrantedAt, &m.GrantedBy, &m.LastActiveAt)
		m.Manager = managerCols{true, features}.manager()
		return m, err
	})
	return out, total, err
}

// MakeManagerInput makes a manager (PL-FR-10, PL-FR-11): either an existing
// teacher (UserID), who keeps teaching, or a new manager-only account (Email
// and Name), who is emailed a link to set a password.
type MakeManagerInput struct {
	UserID   string    `json:"user_id,omitempty"`
	Email    string    `json:"email,omitempty"`
	Name     string    `json:"name,omitempty"`
	Features []Feature `json:"features"`
}

var errAlreadyManager = httpx.NewError(http.StatusConflict, "already_manager", "this person is already a manager")

// MakeManager makes a manager with the given features (none is allowed).
func (s *Service) MakeManager(ctx context.Context, actor User, in MakeManagerInput) (User, error) {
	features, err := normaliseFeatures(in.Features)
	if err != nil {
		return User{}, err
	}
	var u User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		account := "teacher"
		if in.UserID != "" {
			var mgr bool
			err := tx.QueryRow(ctx, `SELECT u.id, u.email, u.name, u.role, u.status, EXISTS (SELECT 1 FROM auth.managers m WHERE m.user_id = u.id)
				FROM auth.users u WHERE u.id=$1 AND u.status <> 'deleted' FOR UPDATE OF u`, in.UserID).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &mgr)
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			if err != nil {
				return err
			}
			if mgr {
				return errAlreadyManager
			}
			// Admins stay admins; students aren't managers (PO-29).
			if u.Role != RoleTeacher || u.Status != StatusActive {
				return httpx.BadRequest("only an active teacher account can be made a manager; create a manager account instead")
			}
		} else {
			account = "manager"
			ri := RegisterInput{Email: in.Email, Name: in.Name, Password: "unused-password", Role: RoleStudent}
			if err := ri.normalise(); err != nil {
				return err
			}
			// No password until they set one from the email ('!' never matches).
			u = User{Email: ri.Email, Name: ri.Name, Role: RoleManager, Status: StatusActive, EmailVerified: true}
			err := tx.QueryRow(ctx, `INSERT INTO auth.users(email, name, role, status, password_hash, email_verified_at)
				VALUES ($1, $2, 'manager', 'active', '!', now()) RETURNING id`, u.Email, u.Name).Scan(&u.ID)
			if isUniqueViolation(err) {
				return httpx.Invalid(map[string]string{"email": "an account with this email already exists; make that teacher a manager instead"})
			}
			if err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO auth.managers(user_id, features, granted_by, granted_at, updated_at) VALUES ($1, $2, $3, $4, $4)`,
			u.ID, features, actor.ID, s.now()); err != nil {
			return err
		}
		u.Manager = managerCols{true, features}.manager()
		msg := qmail.Message{To: u.Email, Subject: "You're now a manager",
			Body: fmt.Sprintf("Hi %s,\n\nAn admin made you a manager on Classroom Quiz. You can use: %s.\n\nOpen the admin console:\n%s/admin", u.Name, featureList(features), s.baseURL)}
		if account == "manager" {
			token, err := s.newTokenTx(ctx, tx, u.ID, "reset_password", 72*time.Hour)
			if err != nil {
				return err
			}
			msg.Subject = "Your manager account"
			msg.Body = fmt.Sprintf("Hi %s,\n\nAn admin created a manager account for you on Classroom Quiz. You can use: %s.\n\nSet your password to sign in:\n%s/reset-password?token=%s\n\nThe link expires in 72 hours; after that, use \"Forgot password\".", u.Name, featureList(features), s.baseURL, token)
		}
		if _, err := s.jobs.InsertTx(ctx, tx, qmail.Args{Message: msg}, nil); err != nil {
			return err
		}
		s.dropUserCache(u.ID) // a logged-in teacher gets the Manage area at once
		return audit.Log(ctx, tx, actor.ID, "manager_made", "user", u.ID, map[string]any{"account": account, "features": features})
	})
	return u, err
}

// SetManagerFeatures replaces a manager's features. It applies at once,
// also to a manager who is logged in (PL-FR-12).
func (s *Service) SetManagerFeatures(ctx context.Context, actor User, userID string, in []Feature) (User, error) {
	features, err := normaliseFeatures(in)
	if err != nil {
		return User{}, err
	}
	var u User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var prev []string
		err := tx.QueryRow(ctx, `SELECT u.id, u.email, u.name, u.role, u.status, m.features FROM auth.managers m JOIN auth.users u ON u.id = m.user_id
			WHERE m.user_id=$1 AND u.status <> 'deleted' FOR UPDATE OF m`, userID).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &prev)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		u.Manager = managerCols{true, features}.manager()
		if slices.Equal(prev, features) {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE auth.managers SET features=$2, updated_at=$3 WHERE user_id=$1`, userID, features, s.now()); err != nil {
			return err
		}
		s.dropUserCache(userID)
		msg := qmail.Message{To: u.Email, Subject: "Your manager features changed",
			Body: fmt.Sprintf("Hi %s,\n\nAn admin changed what you can do as a manager on Classroom Quiz. You can now use: %s.", u.Name, featureList(features))}
		if _, err := s.jobs.InsertTx(ctx, tx, qmail.Args{Message: msg}, nil); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actor.ID, "manager_features_changed", "user", userID, map[string]any{"from": prev, "to": features})
	})
	return u, err
}

// RemoveManager takes the manager role away (PL-FR-12). A teacher goes back
// to teaching; a manager-only account has nothing left, so it is suspended
// and signed out (an admin can activate it again or delete it).
func (s *Service) RemoveManager(ctx context.Context, actor User, userID string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var u User
		err := tx.QueryRow(ctx, `SELECT u.id, u.email, u.name, u.role FROM auth.managers m JOIN auth.users u ON u.id = m.user_id
			WHERE m.user_id=$1 AND u.status <> 'deleted' FOR UPDATE OF m`, userID).Scan(&u.ID, &u.Email, &u.Name, &u.Role)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM auth.managers WHERE user_id=$1`, userID); err != nil {
			return err
		}
		body := "Hi %s,\n\nAn admin removed your manager role on Classroom Quiz. Your teacher account is unchanged."
		if u.Role == RoleManager {
			if _, err := tx.Exec(ctx, `UPDATE auth.users SET status='suspended', updated_at=now() WHERE id=$1`, userID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM auth.sessions WHERE user_id=$1`, userID); err != nil {
				return err
			}
			body = "Hi %s,\n\nAn admin removed your manager role on Classroom Quiz, so your manager account can no longer sign in."
		}
		s.dropUserCache(userID)
		msg := qmail.Message{To: u.Email, Subject: "You're no longer a manager", Body: fmt.Sprintf(body, u.Name)}
		if _, err := s.jobs.InsertTx(ctx, tx, qmail.Args{Message: msg}, nil); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actor.ID, "manager_removed", "user", userID, nil)
	})
}

func featureList(features []string) string {
	if len(features) == 0 {
		return "nothing yet; an admin will give you features"
	}
	names := make([]string, len(features))
	for i, f := range features {
		names[i] = featureLabels[Feature(f)]
	}
	return strings.Join(names, ", ")
}

// ---------- HTTP ----------

// managerRoutes mounts the managers page under /api/admin, for admins only.
func (s *Service) managerRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.AdminOnly())
		r.Method("GET", "/managers", httpx.Handler(s.handleListManagers))
		r.Method("POST", "/managers", httpx.Handler(s.handleMakeManager))
		r.Method("PUT", "/managers/{id}/features", httpx.Handler(s.handleSetManagerFeatures))
		r.Method("DELETE", "/managers/{id}", httpx.Handler(s.handleRemoveManager))
	})
}

func (s *Service) handleListManagers(w http.ResponseWriter, r *http.Request) error {
	p := page.Parse(r, ManagerSorts, "granted", true)
	rows, total, err := s.ListManagers(r.Context(), p)
	if err != nil {
		return err
	}
	page.Write(w, "managers", rows, total, p)
	return nil
}

func (s *Service) handleMakeManager(w http.ResponseWriter, r *http.Request) error {
	var in MakeManagerInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	u, err := s.MakeManager(r.Context(), MustUser(r.Context()), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, u)
	return nil
}

func (s *Service) handleSetManagerFeatures(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Features []Feature `json:"features"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	u, err := s.SetManagerFeatures(r.Context(), MustUser(r.Context()), chi.URLParam(r, "id"), in.Features)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, u)
	return nil
}

func (s *Service) handleRemoveManager(w http.ResponseWriter, r *http.Request) error {
	if err := s.RemoveManager(r.Context(), MustUser(r.Context()), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
