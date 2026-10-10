// Package tutorlink connects the platform to the separate tutoring service
// (ADR-18, ADR-23, D-59). The platform signs short-lived tokens naming the
// logged-in person, so tutoring needs no second login (TS-NFR-52), and
// answers the tutoring service's questions about classrooms through an
// internal API, so tutoring never reads the platform's tables (TS-NFR-51).
//
// Both directions use HS256 tokens with one shared secret file (like the
// master key, never an environment variable). Tokens for people last 5
// minutes: the browser asks for a new one as it goes, so logging out of the
// platform ends tutoring access within minutes.
package tutorlink

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

const (
	// Issuer and audiences: a person's token is for tutoring; the tutoring
	// service's own calls are for the platform. Neither is accepted the other way.
	Issuer           = "quiz-platform"
	AudienceTutoring = "tutoring"
	ServiceIssuer    = "tutoring"
	AudiencePlatform = "quiz-platform"

	UserTokenTTL = 5 * time.Minute
	// Service tokens are minted per call; a minute covers clock drift.
	maxServiceTTL = 2 * time.Minute
)

// UserClaims is what tutoring learns about the person.
type UserClaims struct {
	jwt.RegisteredClaims
	Name   string `json:"name"`
	Role   string `json:"role"`
	Avatar string `json:"avatar,omitempty"`
}

// Classrooms answers whether someone teaches or studies in a classroom.
type Classrooms interface {
	ClassroomAccess(ctx context.Context, classroomID, userID string) (content.Classroom, string, error)
}

type Service struct {
	secret     []byte
	url        string // the tutoring service's public address
	classrooms Classrooms
	now        func() time.Time
}

// New returns nil when tutoring isn't configured: the platform then shows no
// tutoring features (TS-NFR-55).
func New(secret []byte, url string, classrooms Classrooms) *Service {
	if len(secret) == 0 || url == "" {
		return nil
	}
	return &Service{secret: secret, url: strings.TrimRight(url, "/"), classrooms: classrooms, now: time.Now}
}

func jti() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// UserToken signs a token for u, valid for UserTokenTTL.
func (s *Service) UserToken(u auth.User) (string, time.Time, error) {
	now := s.now()
	exp := now.Add(UserTokenTTL)
	role := string(u.Role)
	c := UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Subject: u.ID, Audience: jwt.ClaimStrings{AudienceTutoring},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp), ID: jti()},
		Name: u.Name, Role: role, Avatar: u.Avatar,
	}
	t, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
	return t, exp, err
}

// verifyService checks a token the tutoring service sent with its call.
func (s *Service) verifyService(raw string) error {
	var c jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(ServiceIssuer), jwt.WithAudience(AudiencePlatform),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now), jwt.WithLeeway(30*time.Second))
	if err != nil {
		return err
	}
	if c.ExpiresAt.Sub(c.IssuedAt.Time) > maxServiceTTL {
		return errors.New("service token lives too long")
	}
	return nil
}

// ---------- HTTP ----------

// UserRoutes mounts /api/tutoring for logged-in people.
func UserRoutes(s *Service) func(chi.Router) {
	return func(r chi.Router) {
		r.Method("GET", "/config", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			if s == nil {
				httpx.JSON(w, 200, map[string]any{"enabled": false})
				return nil
			}
			httpx.JSON(w, 200, map[string]any{"enabled": true, "url": s.url})
			return nil
		}))
		r.Method("POST", "/token", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			if s == nil {
				return httpx.ErrNotFound
			}
			u, ok := auth.CurrentUser(r.Context())
			if !ok {
				return httpx.ErrUnauthorized
			}
			t, exp, err := s.UserToken(u)
			if err != nil {
				return err
			}
			w.Header().Set("Cache-Control", "no-store")
			httpx.JSON(w, 200, map[string]any{"token": t, "expires_at": exp, "url": s.url})
			return nil
		}))
	}
}

// InternalRoutes mounts /internal/tutoring, outside /api: Caddy forwards
// only /api, /ws, /beacon and /healthz, so this is reachable only from the
// server's own network, and every call must carry a service token too.
func InternalRoutes(s *Service) func(chi.Router) {
	return func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
				if s == nil || !ok || s.verifyService(raw) != nil {
					httpx.WriteError(w, r, httpx.ErrNotFound)
					return
				}
				next.ServeHTTP(w, r)
			})
		})
		r.Method("POST", "/access", httpx.Handler(s.handleAccess))
	}
}

// AccessResult tells tutoring how a person may take part in a classroom's
// sessions; Role is "teacher" or "student".
type AccessResult struct {
	ClassroomID   string `json:"classroom_id"`
	ClassroomName string `json:"classroom_name"`
	TeacherID     string `json:"teacher_id"`
	Archived      bool   `json:"archived"`
	Role          string `json:"role"`
}

func (s *Service) handleAccess(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		ClassroomID string `json:"classroom_id"`
		UserID      string `json:"user_id"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	c, role, err := s.classrooms.ClassroomAccess(r.Context(), in.ClassroomID, in.UserID)
	if err != nil {
		return err // "not enrolled", "pending" and the like keep their codes for tutoring to show
	}
	httpx.JSON(w, 200, AccessResult{ClassroomID: c.ID, ClassroomName: c.Name, TeacherID: c.TeacherID, Archived: c.Archived, Role: role})
	return nil
}
