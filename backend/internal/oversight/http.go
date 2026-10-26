package oversight

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// AdminRoutes mounts under /api/admin, for admins and managers with the
// live activity feature (D-90).
func (s *Service) AdminRoutes(r chi.Router, a *auth.Service) {
	r.With(a.RequireFeature(auth.FeatLive)).Route("/live-activity", func(r chi.Router) {
		r.Method("GET", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			list, err := s.Live(r.Context())
			if err != nil {
				return err
			}
			pauses, err := s.Pauses(r.Context())
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, map[string]any{"activities": list, "pauses": pauses})
			return nil
		}))
		r.Method("POST", "/{kind}/{id}/end", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				Reason string `json:"reason"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			if err := s.End(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "kind"), chi.URLParam(r, "id"), in.Reason); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
		r.Method("POST", "/pauses", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in PauseInput
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			p, err := s.SetPause(r.Context(), auth.MustUser(r.Context()).ID, in)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, p)
			return nil
		}))
		r.Method("DELETE", "/pauses/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			if err := s.Resume(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "id")); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
	})
}
