package insight

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Catalog is what a person may ask for, and the classrooms they may filter by.
type Catalog struct {
	Measures   []CatalogMeasure `json:"measures"`
	Dimensions []CatalogDim     `json:"dimensions"`
	Grains     []string         `json:"grains"`
	Classrooms []Cell           `json:"classrooms"`
	Staff      bool             `json:"staff"`
	MinGroup   int              `json:"min_group,omitempty"`
}

type CatalogMeasure struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit,omitempty"`
	Fact  Fact   `json:"fact"`
	Help  string `json:"help,omitempty"`
}

type CatalogDim struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Facts []Fact `json:"facts"`
}

func (s *Service) Catalog(ctx context.Context, sc Scope) (Catalog, error) {
	out := Catalog{Grains: []string{"day", "week", "month", "quarter", "year"}, Staff: sc.Staff, Classrooms: []Cell{}}
	if sc.Staff {
		out.MinGroup = MinGroup
	}
	for _, m := range measures {
		out.Measures = append(out.Measures, CatalogMeasure{Key: m.Key, Label: m.Label, Unit: m.Unit, Fact: m.Fact, Help: m.Help})
	}
	for _, d := range dimensions {
		if sc.Staff && !d.staffOK {
			continue
		}
		if !sc.Staff && d.Key == "teacher" {
			continue // a teacher's own classrooms all have one teacher
		}
		out.Dimensions = append(out.Dimensions, CatalogDim{Key: d.Key, Label: d.Label, Facts: d.Facts})
	}
	q := `SELECT id::text, name FROM olap.classrooms WHERE NOT archived ORDER BY name`
	args := []any{}
	if !sc.Staff {
		q = `SELECT id::text, name FROM olap.classrooms c WHERE c.teacher_id = $1
			OR EXISTS (SELECT 1 FROM olap.coteachers ct WHERE ct.classroom_id = c.id AND ct.user_id = $1) ORDER BY archived, name`
		args = append(args, sc.UserID)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Cell
		if err := rows.Scan(&c.ID, &c.Label); err != nil {
			return out, err
		}
		out.Classrooms = append(out.Classrooms, c)
	}
	return out, rows.Err()
}

// scope: admins see everything as staff; teachers (managers too) their own classrooms.
func scope(r *http.Request) (Scope, error) {
	u := auth.MustUser(r.Context())
	switch u.Role {
	case auth.RoleAdmin:
		return Scope{UserID: u.ID, Staff: true}, nil
	case auth.RoleTeacher:
		return Scope{UserID: u.ID}, nil
	}
	return Scope{}, httpx.ErrForbidden
}

// Routes mounts /api/insight for teachers and admins.
func (s *Service) Routes(r chi.Router) {
	r.Method("GET", "/insight/catalog", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		sc, err := scope(r)
		if err != nil {
			return err
		}
		c, err := s.Catalog(r.Context(), sc)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, c)
		return nil
	}))
	r.Method("POST", "/insight/query", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		sc, err := scope(r)
		if err != nil {
			return err
		}
		var q Query
		if err := httpx.Decode(w, r, &q); err != nil {
			return err
		}
		res, err := s.Run(r.Context(), sc, q)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, res)
		return nil
	}))
}
