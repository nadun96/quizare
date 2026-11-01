package media

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// TeacherRoutes mounts POST /api/teacher/images: the request body is the
// picture itself (as the avatar upload takes it).
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("POST", "/images", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		img, err := s.Upload(r.Context(), auth.MustUser(r.Context()).ID, http.MaxBytesReader(w, r.Body, MaxUpload+1))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, img)
		return nil
	}))
}

// PublicRoutes mounts GET /api/images/{id}: anyone with the text has its
// pictures, students and anonymous poll participants alike. Ids are random,
// so a picture can't be found without the text it's in.
func (s *Service) PublicRoutes(r chi.Router) {
	r.Method("GET", "/images/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		data, mime, err := s.Get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		h := w.Header()
		h.Set("Content-Type", mime)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		// A picture never changes: a new one gets a new id.
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		_, err = w.Write(data)
		return err
	}))
}
