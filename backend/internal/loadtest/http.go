package loadtest

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// AdminRoutes mounts the capacity test under /api/admin, for admins only.
func (s *Service) AdminRoutes(r chi.Router) {
	r.Route("/loadtest", func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdmin))
		r.Method("GET", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			runs, err := s.Runs(r.Context())
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, map[string]any{"running": s.Running(), "runs": runs, "defaults": DefaultPlan,
				"thresholds": Thresholds, "max_students": MaxStudents, "max_steps": MaxSteps})
			return nil
		}))
		r.Method("POST", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var p Plan
			if err := httpx.Decode(w, r, &p); err != nil {
				return err
			}
			id, err := s.Start(r.Context(), auth.MustUser(r.Context()).ID, p)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusAccepted, map[string]string{"id": id})
			return nil
		}))
		r.Method("POST", "/stop", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			httpx.JSON(w, http.StatusOK, map[string]bool{"stopping": s.Stop()})
			return nil
		}))
		r.Method("GET", "/runs/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			x, err := s.Get(r.Context(), chi.URLParam(r, "id"))
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, x)
			return nil
		}))
		r.Method("DELETE", "/runs/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			if err := s.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
	})
}
