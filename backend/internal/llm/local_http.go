package llm

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// LocalTest is how one marking request went on the school's server.
type LocalTest struct {
	OK      bool    `json:"ok"`
	Error   string  `json:"error,omitempty"`
	Seconds float64 `json:"seconds"`
	Score   float64 `json:"score"`
}

// TestLocal marks a tiny answer with a model, so the admin sees it works
// and how long it takes on this hardware.
func (s *Service) TestLocal(ctx context.Context, model string) (LocalTest, error) {
	l, err := s.loadLocal(ctx)
	if err != nil {
		return LocalTest{}, err
	}
	if l.BaseURL == "" {
		return LocalTest{}, httpx.Invalid(map[string]string{"base_url": "save the Ollama server's address first"})
	}
	if model == "" {
		model = l.DefaultModel
	}
	if model == "" {
		return LocalTest{}, httpx.Invalid(map[string]string{"model": "choose a model to test"})
	}
	p := Ollama{Base: func(context.Context) (string, error) { return l.BaseURL, nil }, Client: s.localHTTP()}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	start := time.Now()
	res, err := p.Grade(ctx, "", model, GradeRequest{Pseudonym: "test", QuestionType: quiz.Essay, Question: "What is 2 + 2?", ModelAnswer: "4", Answer: "4", MaxScore: 1})
	out := LocalTest{Seconds: time.Since(start).Round(100 * time.Millisecond).Seconds()}
	if err != nil {
		out.Error = truncate(err.Error(), 300)
		return out, nil
	}
	out.OK, out.Score = true, res.Score
	return out, nil
}

// LocalAdminRoutes mounts /api/admin/ai/local, for admins only (D-115).
func (s *Service) LocalAdminRoutes(r chi.Router, a *auth.Service) {
	r.With(a.AdminOnly()).Route("/ai/local", func(r chi.Router) {
		r.Method("GET", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			st, err := s.LocalState(r.Context())
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, st)
			return nil
		}))
		r.Method("PUT", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in Local
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			st, err := s.SaveLocal(r.Context(), uid(r), in)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, st)
			return nil
		}))
		r.Method("POST", "/test", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				Model string `json:"model"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			res, err := s.TestLocal(r.Context(), in.Model)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, res)
			return nil
		}))
	})
}

// LocalTeacherRoutes mounts GET /api/teacher/llm-local: the school's models a teacher can pick.
func (s *Service) LocalTeacherRoutes(r chi.Router) {
	r.Method("GET", "/llm-local", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.ForTeachers(r.Context())
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, out)
		return nil
	}))
}
