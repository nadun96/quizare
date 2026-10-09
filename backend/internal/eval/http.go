package eval

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

func uid(r *http.Request) string { return auth.MustUser(r.Context()).ID }

// TeacherRoutes mounts marking review under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/sessions/{id}/results", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p := page.Parse(r, ResultSorts, "joined", false)
		list, total, err := s.SessionResultsPage(r.Context(), uid(r), chi.URLParam(r, "id"), r.URL.Query().Get("review") == "1", p)
		if err != nil {
			return err
		}
		page.Write(w, "results", list, total, p)
		return nil
	}))
	r.Method("GET", "/sessions/{id}/teams/standings", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.TeamStandings(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]any{"teams": list})
		return nil
	}))
	r.Method("PUT", "/marks/{attempt}/{question}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in OverrideInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		m, err := s.Override(r.Context(), uid(r), chi.URLParam(r, "attempt"), chi.URLParam(r, "question"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, m)
		return nil
	}))
}

// StudentRoutes mounts the student's own results under /api.
func (s *Service) StudentRoutes(r chi.Router) {
	r.Method("GET", "/my/attempts/{id}/result", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		res, err := s.StudentResult(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, res)
		return nil
	}))
}
