package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Gender (D-162) is used only to balance groups. A student sets their own;
// their teachers can correct it. "" is "prefer not to say".

// Genders a person may give.
const (
	GenderFemale = "female"
	GenderMale   = "male"
)

// ValidGender says whether g is "", female or male.
func ValidGender(g string) bool { return g == "" || g == GenderFemale || g == GenderMale }

// Gender is the person's gender, or "".
func (s *Service) Gender(ctx context.Context, userID string) (string, error) {
	var g *string
	err := s.pool.QueryRow(ctx, `SELECT gender FROM auth.users WHERE id = $1`, userID).Scan(&g)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound
	}
	if err != nil || g == nil {
		return "", err
	}
	return *g, nil
}

// SetGender records a person's gender; actor is who did it.
func (s *Service) SetGender(ctx context.Context, actorID, userID, gender string) error {
	if !ValidGender(gender) {
		return httpx.Invalid(map[string]string{"gender": "female, male, or empty"})
	}
	var value any
	if gender != "" {
		value = gender
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE auth.users SET gender = $2, updated_at = now() WHERE id = $1`, userID, value)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(ctx, tx, actorID, "gender_set", "user", userID, nil)
	})
}

func (s *Service) genderRoutes(r chi.Router) {
	r.Method("GET", "/me/gender", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, ok := CurrentUser(r.Context())
		if !ok {
			return httpx.ErrUnauthorized
		}
		g, err := s.Gender(r.Context(), u.ID)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"gender": g})
		return nil
	}))
	r.Method("PUT", "/me/gender", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, ok := CurrentUser(r.Context())
		if !ok {
			return httpx.ErrUnauthorized
		}
		var in struct {
			Gender string `json:"gender"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		if err := s.SetGender(r.Context(), u.ID, u.ID, in.Gender); err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"gender": in.Gender})
		return nil
	}))
}
