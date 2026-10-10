// Package auth owns accounts, passwords and server-side sessions (FR-ACC, ADR-13).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	qmail "github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
)

type Role string

const (
	RoleStudent Role = "student"
	RoleTeacher Role = "teacher"
	RoleAdmin   Role = "admin"
	// RoleManager is a manager-only account (PO-29): it has the admin
	// features it was given and nothing else. A teacher who is also a
	// manager keeps RoleTeacher and has Manager set.
	RoleManager Role = "manager"
)

const (
	StatusActive          = "active"
	StatusPendingApproval = "pending_approval"
	StatusSuspended       = "suspended"
	StatusDeleted         = "deleted"
)

type User struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Role          Role   `json:"role"`
	Status        string `json:"status"`
	EmailVerified bool   `json:"email_verified"`
	// Avatar is the profile picture's version ("" = none); the image is at
	// /api/avatars/{id}?v={avatar} (D-49).
	Avatar string `json:"avatar,omitempty"`
	// CreatedAt is set only in the admin's user list.
	CreatedAt *time.Time `json:"created_at,omitempty"`
	// Manager is set for managers (PL-FR-10): the admin features they have.
	Manager *Manager `json:"manager,omitempty"`
}

// Timeouts per role (ADR-13): students get long-lived sessions so most are
// already logged in when the QR goes up, which flattens the login burst (R1).
type timeouts struct{ idle, absolute time.Duration }

var sessionTimeouts = map[Role]timeouts{
	RoleStudent: {7 * 24 * time.Hour, 30 * 24 * time.Hour},
	RoleTeacher: {24 * time.Hour, 14 * 24 * time.Hour},
	RoleAdmin:   {30 * time.Minute, 12 * time.Hour},
	RoleManager: {30 * time.Minute, 12 * time.Hour},
}

// timeoutsFor gives managers, teacher-managers included, admin limits
// because they act on other people's accounts (PL-NFR-08).
func timeoutsFor(u User) timeouts {
	if u.Manager != nil {
		return sessionTimeouts[RoleAdmin]
	}
	return sessionTimeouts[u.Role]
}

var (
	errInvalidCredentials = httpx.NewError(http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
	errSuspended          = httpx.NewError(http.StatusForbidden, "account_suspended", "this account is suspended")
	errPendingApproval    = httpx.NewError(http.StatusForbidden, "pending_approval", "this teacher account is awaiting admin approval")
	errRateLimited        = httpx.NewError(http.StatusTooManyRequests, "rate_limited", "too many attempts, try again shortly")
	errBadToken           = httpx.NewError(http.StatusBadRequest, "invalid_token", "link is invalid or has expired")
)

type Service struct {
	pool    *pgxpool.Pool
	hasher  *Hasher
	jobs    jobs.Inserter
	baseURL string
	now     func() time.Time

	ipLimiter    *Limiter
	emailLimiter *Limiter
	relations    Relations // who may see whose profile picture (D-49)

	cacheMu sync.Mutex
	cache   map[string]cachedSession // key: token hash
	dummy   string                   // hash used to equalise timing for unknown emails
}

type cachedSession struct {
	user      User
	expiresAt time.Time
	created   time.Time
	lastSeen  time.Time
	fetched   time.Time
	// managerSeen is when last_active_at was last written for a manager.
	managerSeen time.Time
}

const sessionCacheTTL = 30 * time.Second

func NewService(pool *pgxpool.Pool, hasher *Hasher, inserter jobs.Inserter, baseURL string) *Service {
	return &Service{
		pool: pool, hasher: hasher, jobs: inserter, baseURL: strings.TrimRight(baseURL, "/"), now: time.Now,
		// A whole class shares one NAT address, so the per-IP bucket is generous;
		// the per-account bucket is what stops password guessing.
		ipLimiter:    NewLimiter(300, 200*time.Millisecond),
		emailLimiter: NewLimiter(10, 30*time.Second),
		cache:        map[string]cachedSession{},
	}
}

// ---------- registration ----------

type RegisterInput struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     Role   `json:"role"` // "student" or "teacher"; admins are created by CLI
}

func (in *RegisterInput) normalise() error {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	f := map[string]string{}
	if a, err := mail.ParseAddress(in.Email); err != nil || a.Address != in.Email || len(in.Email) > 254 {
		f["email"] = "enter a valid email address"
	}
	if n := len([]rune(in.Name)); n < 1 || n > 100 {
		f["name"] = "name must be 1-100 characters"
	}
	if err := checkPassword(in.Password); err != "" {
		f["password"] = err
	}
	if in.Role != RoleStudent && in.Role != RoleTeacher {
		f["role"] = "role must be student or teacher"
	}
	if len(f) > 0 {
		return httpx.Invalid(f)
	}
	return nil
}

func checkPassword(p string) string {
	if n := len([]rune(p)); n < 8 || n > 128 {
		return "password must be 8-128 characters"
	}
	return ""
}

// Register creates the account and queues a verification email in the same
// transaction. Students may log in before verifying so the QR join flow stays
// under 90 seconds (BO-2); see DECISIONS.md D-02.
func (s *Service) Register(ctx context.Context, in RegisterInput) (User, error) {
	if err := in.normalise(); err != nil {
		return User{}, err
	}
	hash, err := s.hasher.Hash(ctx, in.Password)
	if err != nil {
		return User{}, err
	}
	var u User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		status := StatusActive
		if in.Role == RoleTeacher {
			var approval bool
			if err := tx.QueryRow(ctx, `SELECT require_teacher_approval FROM auth.policy`).Scan(&approval); err != nil {
				return err
			}
			if approval {
				status = StatusPendingApproval
			}
		}
		err := tx.QueryRow(ctx, `INSERT INTO auth.users(email, name, role, status, password_hash)
			VALUES ($1,$2,$3,$4,$5) RETURNING id`, in.Email, in.Name, in.Role, status, hash).Scan(&u.ID)
		if isUniqueViolation(err) {
			return httpx.Invalid(map[string]string{"email": "an account with this email already exists"})
		}
		if err != nil {
			return err
		}
		u.Email, u.Name, u.Role, u.Status = in.Email, in.Name, in.Role, status
		return s.issueTokenTx(ctx, tx, u, "verify_email", 48*time.Hour)
	})
	return u, err
}

// newTokenTx stores a one-time token and returns it.
func (s *Service) newTokenTx(ctx context.Context, tx pgx.Tx, userID, purpose string, ttl time.Duration) (string, error) {
	token, hash := newToken()
	_, err := tx.Exec(ctx, `INSERT INTO auth.tokens(token_hash, user_id, purpose, expires_at) VALUES ($1,$2,$3,$4)`,
		hash, userID, purpose, s.now().Add(ttl))
	return token, err
}

// issueTokenTx stores a one-time token and enqueues the email that carries it.
func (s *Service) issueTokenTx(ctx context.Context, tx pgx.Tx, u User, purpose string, ttl time.Duration) error {
	token, err := s.newTokenTx(ctx, tx, u.ID, purpose, ttl)
	if err != nil {
		return err
	}
	var msg qmail.Message
	msg.To = u.Email
	switch purpose {
	case "verify_email":
		msg.Subject = "Verify your email"
		msg.Body = fmt.Sprintf("Hi %s,\n\nConfirm your email address:\n%s/verify-email?token=%s\n\nThe link expires in 48 hours.", u.Name, s.baseURL, token)
	case "reset_password":
		msg.Subject = "Reset your password"
		msg.Body = fmt.Sprintf("Hi %s,\n\nReset your password:\n%s/reset-password?token=%s\n\nThe link expires in 1 hour. If you did not ask for this, ignore this email.", u.Name, s.baseURL, token)
	}
	_, err = s.jobs.InsertTx(ctx, tx, qmail.Args{Message: msg}, nil)
	return err
}

// ---------- login / sessions ----------

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login verifies credentials and creates a new session (rotating any token
// the client had). It returns the raw token for the cookie.
func (s *Service) Login(ctx context.Context, in LoginInput, ip, userAgent string) (User, string, time.Time, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !s.ipLimiter.Allow("ip:"+ip) || !s.emailLimiter.Allow("email:"+email) {
		return User{}, "", time.Time{}, errRateLimited
	}
	var u User
	var hash string
	var verified *time.Time
	var mgr managerCols
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.email, u.name, u.role, u.status, u.password_hash, u.email_verified_at, coalesce(u.avatar_version, ''), `+managerSelect+`
		FROM auth.users u `+managerJoin+` WHERE u.email=$1 AND u.status <> 'deleted'`, email).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &hash, &verified, &u.Avatar, &mgr.is, &mgr.features)
	if errors.Is(err, pgx.ErrNoRows) || hash == "!" { // '!': a manager account with no password yet
		// Spend the same work as a real check so timing does not reveal accounts.
		_, _ = s.hasher.Verify(ctx, in.Password, s.dummyHash(ctx))
		return User{}, "", time.Time{}, errInvalidCredentials
	}
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	ok, err := s.hasher.Verify(ctx, in.Password, hash)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	if !ok {
		return User{}, "", time.Time{}, errInvalidCredentials
	}
	switch u.Status {
	case StatusSuspended:
		return User{}, "", time.Time{}, errSuspended
	case StatusPendingApproval:
		return User{}, "", time.Time{}, errPendingApproval
	}
	u.EmailVerified = verified != nil
	u.Manager = mgr.manager()
	token, expires, err := s.CreateSession(ctx, u, userAgent)
	return u, token, expires, err
}

// CreateSession issues a fresh session token for an already-authenticated user.
func (s *Service) CreateSession(ctx context.Context, u User, userAgent string) (string, time.Time, error) {
	token, tokenHash := newToken()
	expires := s.now().Add(timeoutsFor(u).absolute)
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO auth.sessions(token_hash, user_id, expires_at, user_agent, created_at, last_seen_at)
		VALUES ($1,$2,$3,$4,$5,$5)`, tokenHash, u.ID, expires, userAgent, s.now()); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func (s *Service) dummyHash(ctx context.Context) string {
	s.cacheMu.Lock()
	d := s.dummy
	s.cacheMu.Unlock()
	if d == "" {
		d, _ = s.hasher.Hash(ctx, "not-a-real-password")
		s.cacheMu.Lock()
		s.dummy = d
		s.cacheMu.Unlock()
	}
	return d
}

// Authenticate resolves a raw session token to its user, enforcing idle and
// absolute timeouts server-side. Results are cached briefly in memory.
func (s *Service) Authenticate(ctx context.Context, token string) (User, bool, error) {
	if token == "" || len(token) > 100 {
		return User{}, false, nil
	}
	key := string(hashToken(token))
	now := s.now()

	s.cacheMu.Lock()
	c, ok := s.cache[key]
	s.cacheMu.Unlock()
	if !ok || now.Sub(c.fetched) > sessionCacheTTL {
		var verified *time.Time
		var mgr managerCols
		err := s.pool.QueryRow(ctx, `SELECT u.id, u.email, u.name, u.role, u.status, u.email_verified_at, s.expires_at, s.created_at, s.last_seen_at, coalesce(u.avatar_version, ''), `+managerSelect+`
			FROM auth.sessions s JOIN auth.users u ON u.id = s.user_id `+managerJoin+` WHERE s.token_hash=$1`, []byte(key)).
			Scan(&c.user.ID, &c.user.Email, &c.user.Name, &c.user.Role, &c.user.Status, &verified, &c.expiresAt, &c.created, &c.lastSeen, &c.user.Avatar, &mgr.is, &mgr.features)
		if errors.Is(err, pgx.ErrNoRows) {
			s.dropCache(key)
			return User{}, false, nil
		}
		if err != nil {
			return User{}, false, err
		}
		c.user.EmailVerified = verified != nil
		c.user.Manager = mgr.manager()
		c.fetched = now
	}
	t := timeoutsFor(c.user)
	// A teacher made a manager mid-session gets the manager limits at once,
	// although the session was issued with a teacher's expiry.
	if c.user.Status != StatusActive || now.After(c.expiresAt) || now.Sub(c.created) > t.absolute || now.Sub(c.lastSeen) > t.idle {
		_, _ = s.pool.Exec(ctx, `DELETE FROM auth.sessions WHERE token_hash=$1`, []byte(key))
		s.dropCache(key)
		return User{}, false, nil
	}
	// Refresh last_seen at most once a minute to keep writes low.
	if now.Sub(c.lastSeen) > time.Minute {
		if _, err := s.pool.Exec(ctx, `UPDATE auth.sessions SET last_seen_at=$2 WHERE token_hash=$1`, []byte(key), now); err != nil {
			return User{}, false, err
		}
		c.lastSeen = now
	}
	// The managers page shows each manager's last activity (PL-FR-16).
	if c.user.Manager != nil && now.Sub(c.managerSeen) > time.Minute {
		if _, err := s.pool.Exec(ctx, `UPDATE auth.managers SET last_active_at=$2 WHERE user_id=$1`, c.user.ID, now); err != nil {
			return User{}, false, err
		}
		c.managerSeen = now
	}
	s.cacheMu.Lock()
	s.cache[key] = c
	s.cacheMu.Unlock()
	return c.user, true, nil
}

func (s *Service) dropCache(key string) {
	s.cacheMu.Lock()
	delete(s.cache, key)
	s.cacheMu.Unlock()
}

// dropUserCache evicts every cached session of a user (logout-all, suspend, delete).
func (s *Service) dropUserCache(userID string) {
	s.cacheMu.Lock()
	for k, c := range s.cache {
		if c.user.ID == userID {
			delete(s.cache, k)
		}
	}
	s.cacheMu.Unlock()
}

func (s *Service) Logout(ctx context.Context, token string) error {
	key := hashToken(token)
	s.dropCache(string(key))
	_, err := s.pool.Exec(ctx, `DELETE FROM auth.sessions WHERE token_hash=$1`, key)
	return err
}

// ---------- email verification & password reset ----------

// consumeToken marks a token used and returns its user id.
func consumeToken(ctx context.Context, tx pgx.Tx, token, purpose string, now time.Time) (string, error) {
	var userID string
	err := tx.QueryRow(ctx, `UPDATE auth.tokens SET used_at=$3
		WHERE token_hash=$1 AND purpose=$2 AND used_at IS NULL AND expires_at > $3 RETURNING user_id`,
		hashToken(token), purpose, now).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errBadToken
	}
	return userID, err
}

func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := consumeToken(ctx, tx, token, "verify_email", s.now())
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE auth.users SET email_verified_at=$2, updated_at=$2 WHERE id=$1`, id, s.now())
		s.dropUserCache(id)
		return err
	})
}

// RequestPasswordReset always succeeds from the caller's view so it cannot be
// used to discover which emails have accounts.
func (s *Service) RequestPasswordReset(ctx context.Context, email, ip string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !s.ipLimiter.Allow("ip:"+ip) || !s.emailLimiter.Allow("reset:"+email) {
		return errRateLimited
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var u User
		err := tx.QueryRow(ctx, `SELECT id, email, name FROM auth.users WHERE email=$1 AND status IN ('active','pending_approval')`, email).
			Scan(&u.ID, &u.Email, &u.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return s.issueTokenTx(ctx, tx, u, "reset_password", time.Hour)
	})
}

// ResetPassword sets a new password and revokes every existing session.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if msg := checkPassword(password); msg != "" {
		return httpx.Invalid(map[string]string{"password": msg})
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := consumeToken(ctx, tx, token, "reset_password", s.now())
		if err != nil {
			return err
		}
		// Following the emailed link proves control of the address.
		if _, err := tx.Exec(ctx, `UPDATE auth.users SET password_hash=$2, email_verified_at=coalesce(email_verified_at,$3), updated_at=$3 WHERE id=$1`,
			id, hash, s.now()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM auth.sessions WHERE user_id=$1`, id); err != nil {
			return err
		}
		s.dropUserCache(id)
		return audit.Log(ctx, tx, id, "password_reset", "user", id, nil)
	})
}

// ---------- lookups for other modules ----------

// GetUser returns a user by id (used by other modules through the Users interface).
func (s *Service) GetUser(ctx context.Context, id string) (User, error) {
	var u User
	var verified *time.Time
	var mgr managerCols
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.email, u.name, u.role, u.status, u.email_verified_at, coalesce(u.avatar_version, ''), `+managerSelect+`
		FROM auth.users u `+managerJoin+` WHERE u.id=$1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &verified, &u.Avatar, &mgr.is, &mgr.features)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, httpx.ErrNotFound
	}
	u.EmailVerified = verified != nil
	u.Manager = mgr.manager()
	return u, err
}

// ActiveTeacher finds an active teacher account by email, for adding a
// co-teacher to a tutoring session (TS-FR-70). Others are "not found".
func (s *Service) ActiveTeacher(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `SELECT id, email, name, role, status FROM auth.users WHERE email=$1 AND role='teacher' AND status='active'`,
		strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, httpx.ErrNotFound
	}
	return u, err
}

// UsersByID returns users keyed by id; unknown ids are omitted.
func (s *Service) UsersByID(ctx context.Context, ids []string) (map[string]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, email, name, role, status, email_verified_at IS NOT NULL, coalesce(avatar_version, '')
		FROM auth.users WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]User, len(ids))
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &u.EmailVerified, &u.Avatar); err != nil {
			return nil, err
		}
		out[u.ID] = u
	}
	return out, rows.Err()
}

// CreateAdmin is used by the CLI bootstrap command; there is no HTTP path to it.
func (s *Service) CreateAdmin(ctx context.Context, email, name, password string) (User, error) {
	in := RegisterInput{Email: email, Name: name, Password: password, Role: RoleStudent}
	if err := in.normalise(); err != nil {
		return User{}, err
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return User{}, err
	}
	u := User{Email: in.Email, Name: in.Name, Role: RoleAdmin, Status: StatusActive, EmailVerified: true}
	err = s.pool.QueryRow(ctx, `INSERT INTO auth.users(email,name,role,status,password_hash,email_verified_at)
		VALUES ($1,$2,'admin','active',$3,now()) RETURNING id`, u.Email, u.Name, hash).Scan(&u.ID)
	if isUniqueViolation(err) {
		return User{}, httpx.Conflict("an account with this email already exists")
	}
	return u, err
}

// ---------- helpers ----------

func newToken() (string, []byte) {
	b := make([]byte, 32) // 256-bit
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	t := base64.RawURLEncoding.EncodeToString(b)
	return t, hashToken(t)
}

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}
