package mailing

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

type composed struct {
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
	Audience   Audience `json:"audience"`
	TemplateID string   `json:"template_id"`
}

type templateIn struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// AdminRoutes mounts /api/admin/email, for admins and managers given "Email".
func (s *Service) AdminRoutes(r chi.Router, a *auth.Service) {
	r.With(a.RequireFeature(auth.FeatEmail)).Route("/email", func(r chi.Router) {
		actor := func(r *http.Request) string { return auth.MustUser(r.Context()).ID }
		r.Method("GET", "/templates", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			p := page.Parse(r, TemplateSorts, "name", false)
			list, total, err := s.Templates(r.Context(), p)
			if err != nil {
				return err
			}
			page.Write(w, "templates", list, total, p)
			return nil
		}))
		r.Method("POST", "/templates", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in templateIn
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			t, err := s.SaveTemplate(r.Context(), actor(r), "", in.Name, in.Subject, in.Body)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusCreated, t)
			return nil
		}))
		r.Method("GET", "/templates/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			t, err := s.Template(r.Context(), chi.URLParam(r, "id"))
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, t)
			return nil
		}))
		r.Method("PUT", "/templates/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in templateIn
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			t, err := s.SaveTemplate(r.Context(), actor(r), chi.URLParam(r, "id"), in.Name, in.Subject, in.Body)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, t)
			return nil
		}))
		r.Method("DELETE", "/templates/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			if err := s.DeleteTemplate(r.Context(), chi.URLParam(r, "id")); err != nil {
				return err
			}
			_ = audit.Log(r.Context(), s.pool, actor(r), "email_template_deleted", "email_template", chi.URLParam(r, "id"), nil)
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
		r.Method("GET", "/classrooms", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			list, err := s.Classrooms(r.Context(), r.URL.Query().Get("q"))
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, map[string]any{"classrooms": list})
			return nil
		}))
		r.Method("POST", "/preview", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in composed
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			p, err := s.PreviewSend(r.Context(), in.Subject, in.Body, in.Audience)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, p)
			return nil
		}))
		r.Method("POST", "/test", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in composed
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			if err := s.SendTest(r.Context(), actor(r), in.Subject, in.Body, in.Audience); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
		r.Method("POST", "/sends", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in composed
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			out, err := s.SendAll(r.Context(), actor(r), in.Subject, in.Body, in.Audience, in.TemplateID)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusCreated, out)
			return nil
		}))
		r.Method("GET", "/sends", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			p := page.Parse(r, SendSorts, "sent", true)
			list, total, err := s.Sends(r.Context(), p)
			if err != nil {
				return err
			}
			page.Write(w, "sends", list, total, p)
			return nil
		}))
	})
}
