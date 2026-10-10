// Package authn checks the platform's tokens for people and signs this
// service's own tokens for the platform's internal API (ADR-23, D-59). The
// claims match backend/internal/tutorlink.
package authn

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Person is who a request comes from, as the platform vouched.
type Person struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"` // the platform role: student, teacher, manager, admin
	Avatar string `json:"avatar,omitempty"`
	// Expires is when the token runs out; sockets close then unless renewed.
	Expires time.Time `json:"-"`
}

type claims struct {
	jwt.RegisteredClaims
	Name   string `json:"name"`
	Role   string `json:"role"`
	Avatar string `json:"avatar,omitempty"`
}

// Keys holds the secret shared with the platform.
type Keys struct {
	Secret []byte
	Now    func() time.Time
}

func (k Keys) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

var errToken = web.NewError(http.StatusUnauthorized, "unauthorized", "your sign-in has expired; reload the page")

// Verify checks a person's token: signed with the shared secret by the
// platform, for tutoring, and not expired (they last 5 minutes).
func (k Keys) Verify(raw string) (Person, error) {
	var c claims
	_, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return k.Secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("quiz-platform"), jwt.WithAudience("tutoring"),
		jwt.WithExpirationRequired(), jwt.WithTimeFunc(k.now), jwt.WithLeeway(15*time.Second))
	if err != nil || c.Subject == "" {
		return Person{}, errToken
	}
	return Person{ID: c.Subject, Name: c.Name, Role: c.Role, Avatar: c.Avatar, Expires: c.ExpiresAt.Time}, nil
}

// ServiceToken signs a one-minute token for a call to the platform.
func (k Keys) ServiceToken() (string, error) {
	now := k.now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: "tutoring", Audience: jwt.ClaimStrings{"quiz-platform"},
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}).SignedString(k.Secret)
}

type ctxKey struct{}

// Middleware requires a person's token on every request.
func (k Keys) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			web.WriteError(w, r, web.ErrUnauthorized)
			return
		}
		p, err := k.Verify(raw)
		if err != nil {
			web.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

// From returns the person a request is from.
func From(ctx context.Context) Person {
	p, _ := ctx.Value(ctxKey{}).(Person)
	return p
}

// ErrNoSecret: the shared secret file is required.
var ErrNoSecret = errors.New("the shared secret is missing")
