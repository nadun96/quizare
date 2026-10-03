package settings

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Store owns the platform and teacher layers. Lower layers (classroom … student)
// are stored by the module that owns that entity.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Platform(ctx context.Context) (Overrides, error) {
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT overrides FROM settings.platform`).Scan(&raw); err != nil {
		return Overrides{}, err
	}
	return Parse(raw)
}

func (s *Store) Teacher(ctx context.Context, teacherID string) (Overrides, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT overrides FROM settings.teacher WHERE teacher_id=$1`, teacherID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Overrides{}, nil
	}
	if err != nil {
		return Overrides{}, err
	}
	return Parse(raw)
}

// Base resolves defaults → platform → teacher: the starting point for every
// lower level.
func (s *Store) Base(ctx context.Context, teacherID string) (Effective, error) {
	p, err := s.Platform(ctx)
	if err != nil {
		return Effective{}, err
	}
	t, err := s.Teacher(ctx, teacherID)
	if err != nil {
		return Effective{}, err
	}
	return Resolve(Defaults(), p, t), nil
}

func (s *Store) SetPlatform(ctx context.Context, actorID string, o Overrides) error {
	if err := o.Validate(LevelPlatform); err != nil {
		return ToHTTP(err)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE settings.platform SET overrides=$1`, o.JSON()); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "platform_settings_changed", "settings", "platform", o)
	})
}

func (s *Store) SetTeacher(ctx context.Context, teacherID string, o Overrides) error {
	if err := o.Validate(LevelTeacher); err != nil {
		return ToHTTP(err)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO settings.teacher(teacher_id, overrides) VALUES ($1,$2)
		ON CONFLICT (teacher_id) DO UPDATE SET overrides=EXCLUDED.overrides`, teacherID, o.JSON())
	return err
}

// ToHTTP converts a ValidationError into a 422 response error.
func ToHTTP(err error) error {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return httpx.Invalid(ve.Fields)
	}
	return err
}

// ---------- HTTP ----------

type layerResponse struct {
	Overrides Overrides `json:"overrides"`
	Effective Effective `json:"effective"`
}

// TeacherRoutes mounts /api/teacher/settings (teacher role enforced by caller).
func (s *Store) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u := auth.MustUser(r.Context())
		o, err := s.Teacher(r.Context(), u.ID)
		if err != nil {
			return err
		}
		eff, err := s.Base(r.Context(), u.ID)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, layerResponse{o, eff})
		return nil
	}))
	r.Method("PUT", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var o Overrides
		if err := httpx.Decode(w, r, &o); err != nil {
			return err
		}
		u := auth.MustUser(r.Context())
		if err := s.SetTeacher(r.Context(), u.ID, o); err != nil {
			return err
		}
		eff, err := s.Base(r.Context(), u.ID)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, layerResponse{o, eff})
		return nil
	}))
}

// AdminRoutes mounts /api/admin/settings (admin role enforced by caller).
func (s *Store) AdminRoutes(r chi.Router) {
	r.Method("GET", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		o, err := s.Platform(r.Context())
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, layerResponse{o, ApplyPlatform(o)})
		return nil
	}))
	r.Method("PUT", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var o Overrides
		if err := httpx.Decode(w, r, &o); err != nil {
			return err
		}
		if err := s.SetPlatform(r.Context(), auth.MustUser(r.Context()).ID, o); err != nil {
			return err
		}
		httpx.JSON(w, 200, layerResponse{o, ApplyPlatform(o)})
		return nil
	}))
}
