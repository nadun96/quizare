package llm

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

func uid(r *http.Request) string { return auth.MustUser(r.Context()).ID }

// TeacherRoutes mounts key management and LLM marking controls under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/llm-keys", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		keys, err := s.ListKeys(r.Context(), uid(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]any{"keys": keys})
		return nil
	}))
	r.Method("POST", "/llm-keys", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in KeyInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		k, err := s.AddKey(r.Context(), uid(r), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, 201, k)
		return nil
	}))
	r.Method("PATCH", "/llm-keys/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in KeyUpdate
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		k, err := s.UpdateKey(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, k)
		return nil
	}))
	r.Method("DELETE", "/llm-keys/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.DeleteKey(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(204)
		return nil
	}))
	r.Method("POST", "/llm-keys/{id}/test", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		k, err := s.TestKey(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, k)
		return nil
	}))
	r.Method("POST", "/marks/{attempt}/{question}/remark", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Remark(r.Context(), uid(r), chi.URLParam(r, "attempt"), chi.URLParam(r, "question")); err != nil {
			return err
		}
		w.WriteHeader(202)
		return nil
	}))
	r.Method("GET", "/sessions/{id}/llm-estimate", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		e, err := s.Estimate(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, e)
		return nil
	}))
}
