package inbox

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// Routes mounts /api/my/inbox, announcements and conversations, for
// students and teachers.
func (s *Service) Routes(r chi.Router) {
	user := func(r *http.Request) auth.User { return auth.MustUser(r.Context()) }
	r.Method("GET", "/my/inbox", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		sum, err := s.Summary(r.Context(), user(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, sum)
		return nil
	}))
	r.Method("PUT", "/my/inbox/prefs", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Email bool `json:"email"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		if err := s.SetEmail(r.Context(), user(r).ID, in.Email); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("GET", "/my/announcements", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p := page.Parse(r, page.Sorts{"created": "a.created_at"}, "created", true)
		list, total, err := s.Announcements(r.Context(), user(r), r.URL.Query().Get("classroom"), p)
		if err != nil {
			return err
		}
		page.Write(w, "announcements", list, total, p)
		return nil
	}))
	r.Method("POST", "/my/announcements/read", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		if in.IDs == nil {
			in.IDs = []string{}
		}
		if u := user(r); u.Role == auth.RoleStudent {
			if err := s.MarkRead(r.Context(), u.ID, in.IDs); err != nil {
				return err
			}
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("GET", "/my/contacts", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p := page.Parse(r, page.Sorts{"name": "o.name"}, "name", false)
		list, total, err := s.Contacts(r.Context(), user(r), r.URL.Query().Get("classroom"), p)
		if err != nil {
			return err
		}
		page.Write(w, "contacts", list, total, p)
		return nil
	}))
	r.Method("GET", "/my/conversations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p := page.Parse(r, page.Sorts{"last": "c.last_at"}, "last", true)
		list, total, err := s.Conversations(r.Context(), user(r), p)
		if err != nil {
			return err
		}
		page.Write(w, "conversations", list, total, p)
		return nil
	}))
	r.Method("POST", "/my/conversations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			UserID string `json:"user_id"`
			Body   string `json:"body"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		c, m, err := s.Start(r.Context(), user(r), in.UserID, in.Body)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, map[string]any{"conversation": c, "message": m})
		return nil
	}))
	r.Method("GET", "/my/conversations/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		c, err := s.Conversation(r.Context(), user(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, c)
		return nil
	}))
	r.Method("GET", "/my/conversations/{id}/messages", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p := page.Parse(r, page.Sorts{"created": "m.created_at"}, "created", true)
		list, total, err := s.Messages(r.Context(), user(r), chi.URLParam(r, "id"), p)
		if err != nil {
			return err
		}
		page.Write(w, "messages", list, total, p)
		return nil
	}))
	r.Method("POST", "/my/conversations/{id}/messages", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Body string `json:"body"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		m, err := s.Send(r.Context(), user(r), chi.URLParam(r, "id"), in.Body)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, m)
		return nil
	}))
	r.Method("POST", "/my/conversations/{id}/read", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ReadConversation(r.Context(), user(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
}

// TeacherRoutes mounts posting, editing and deleting announcements.
func (s *Service) TeacherRoutes(r chi.Router) {
	teacher := func(r *http.Request) string { return auth.MustUser(r.Context()).ID }
	r.Method("POST", "/classrooms/{id}/announcements", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			Email bool   `json:"email"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		a, err := s.Announce(r.Context(), teacher(r), chi.URLParam(r, "id"), in.Title, in.Body, in.Email)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, a)
		return nil
	}))
	r.Method("PATCH", "/announcements/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Title *string `json:"title"`
			Body  *string `json:"body"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		a, err := s.EditAnnouncement(r.Context(), teacher(r), chi.URLParam(r, "id"), in.Title, in.Body)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, a)
		return nil
	}))
	r.Method("DELETE", "/announcements/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.DeleteAnnouncement(r.Context(), teacher(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("GET", "/announcements/{id}/readers", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p := page.Parse(r, page.Sorts{"name": "u.name"}, "name", false)
		list, total, err := s.Readers(r.Context(), teacher(r), chi.URLParam(r, "id"), p)
		if err != nil {
			return err
		}
		page.Write(w, "readers", list, total, p)
		return nil
	}))
}
