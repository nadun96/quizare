package grading

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

func uid(r *http.Request) string { return auth.MustUser(r.Context()).ID }

// TeacherRoutes mounts the gradebook and attendance under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/classrooms/{id}/gradebook", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		g, err := s.Gradebook(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, g)
		return nil
	}))
	r.Method("PUT", "/classrooms/{id}/weights", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Weights []WeightInput `json:"weights"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		g, err := s.SetWeights(r.Context(), uid(r), chi.URLParam(r, "id"), in.Weights)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, g)
		return nil
	}))
	r.Method("GET", "/classrooms/{id}/class-report", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		rep, err := s.ClassReport(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, rep)
		return nil
	}))
	r.Method("PUT", "/classrooms/{id}/class-report", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in ReportSetup
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		rep, err := s.SetClassReport(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, rep)
		return nil
	}))
	r.Method("GET", "/classrooms/{id}/attendance", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		day := func(k string, def time.Time) (time.Time, bool) {
			v := r.URL.Query().Get(k)
			if v == "" {
				return def, true
			}
			t, err := time.Parse("2006-01-02", v)
			return t, err == nil
		}
		today := time.Now().UTC().Truncate(24 * time.Hour)
		from, ok1 := day("from", today.AddDate(0, 0, -90))
		to, ok2 := day("to", today)
		if !ok1 || !ok2 || to.Before(from) {
			return httpx.Invalid(map[string]string{"from": "dates like 2026-01-31, the start before the end"})
		}
		a, err := s.Attendance(r.Context(), uid(r), chi.URLParam(r, "id"), from, to)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, a)
		return nil
	}))
}
