package analytics

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

func uid(r *http.Request) string { return auth.MustUser(r.Context()).ID }

// TeacherRoutes mounts analytics and sharing under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/sessions/{id}/analytics", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		st, err := s.SessionStats(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, st)
		return nil
	}))
	r.Method("GET", "/quizzes/{id}/analytics", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		st, err := s.QuizStats(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, st)
		return nil
	}))
	r.Method("GET", "/sessions/{id}/export.csv", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		data, err := s.ExportSessionCSV(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="session-%s.csv"`, chi.URLParam(r, "id")))
		_, err = w.Write(data)
		return err
	}))
	r.Method("GET", "/share-links", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.ListLinks(r.Context(), uid(r), r.URL.Query().Get("target_id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]any{"links": list})
		return nil
	}))
	r.Method("POST", "/share-links", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in ShareInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		l, err := s.CreateLink(r.Context(), uid(r), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, 201, l)
		return nil
	}))
	r.Method("DELETE", "/share-links/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.RevokeLink(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(204)
		return nil
	}))
	r.Method("POST", "/share-links/{id}/regenerate", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		l, err := s.RegenerateLink(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, l)
		return nil
	}))
}

// PublicRoutes mounts unauthenticated, read-only share pages under /api/public.
func (s *Service) PublicRoutes(r chi.Router) {
	r.Method("GET", "/results/{token}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow") // ADR-16
		w.Header().Set("Cache-Control", "no-store")
		v, err := s.Public(r.Context(), chi.URLParam(r, "token"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, v)
		return nil
	}))
	r.Method("GET", "/live/{token}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow") // ADR-16
		w.Header().Set("Cache-Control", "no-store")
		v, err := s.Live(r.Context(), chi.URLParam(r, "token"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, v)
		return nil
	}))
}
