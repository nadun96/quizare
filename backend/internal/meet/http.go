package meet

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// TeacherRoutes mounts hosting under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	s.TeacherLimitsRoute(r)
	r.Method("POST", "/meetings", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in MeetingInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		m, err := s.Create(r.Context(), auth.MustUser(r.Context()), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, m)
		return nil
	}))
	r.Method("GET", "/meetings", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p := page.Parse(r, Sorts, "created", true)
		list, total, err := s.List(r.Context(), auth.MustUser(r.Context()).ID, p)
		if err != nil {
			return err
		}
		page.Write(w, "meetings", list, total, p)
		return nil
	}))
	r.Method("POST", "/meetings/{id}/end", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.End(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("GET", "/meetings/{id}/attendance", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		rows, err := s.Attendance(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"attendance": rows})
		return nil
	}))
}

// TeacherLimitsRoute tells teachers what they may open (under /api/teacher).
func (s *Service) TeacherLimitsRoute(r chi.Router) {
	r.Method("GET", "/meeting-limits", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		l, err := s.Limits(r.Context())
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, l)
		return nil
	}))
}

// AdminRoutes lets admins, and managers with the settings feature, set the limits.
func (s *Service) AdminRoutes(r chi.Router, a *auth.Service) {
	r.With(a.RequireFeature(auth.FeatSettings)).Method("GET", "/meeting-limits", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		l, err := s.Limits(r.Context())
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, l)
		return nil
	}))
	r.With(a.RequireFeature(auth.FeatSettings)).Method("PUT", "/meeting-limits", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in Limits
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		l, err := s.SetLimits(r.Context(), auth.MustUser(r.Context()).ID, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, l)
		return nil
	}))
}

// UserRoutes mounts joining under /api for anyone signed in.
func (s *Service) UserRoutes(r chi.Router) {
	r.Method("GET", "/meetings/{code}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		acc, err := s.Join(r.Context(), auth.MustUser(r.Context()), chi.URLParam(r, "code"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, acc)
		return nil
	}))
}
