package grading

import (
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// offlineRoutes are a teacher's offline assignments (D-159).
func (s *Service) offlineRoutes(r chi.Router) {
	r.Method("GET", "/topics/{id}/offline-assignments", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.ListOffline(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"assignments": list})
		return nil
	}))
	r.Method("POST", "/topics/{id}/offline-assignments", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in OfflineInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		o, err := s.CreateOffline(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, o)
		return nil
	}))
	r.Method("GET", "/offline-assignments/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		sh, err := s.Sheet(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, sh)
		return nil
	}))
	r.Method("PUT", "/offline-assignments/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in OfflineInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		o, err := s.UpdateOffline(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, o)
		return nil
	}))
	r.Method("DELETE", "/offline-assignments/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.DeleteOffline(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("PUT", "/offline-assignments/{id}/release", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Released bool `json:"released"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		o, err := s.ReleaseOffline(r.Context(), uid(r), chi.URLParam(r, "id"), in.Released)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, o)
		return nil
	}))
	r.Method("PUT", "/offline-assignments/{id}/marks", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in MarksInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		sh, err := s.SetMarks(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, sh)
		return nil
	}))
	r.Method("GET", "/offline-assignments/{id}/sheet", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		q := r.URL.Query()
		f, err := s.Download(r.Context(), uid(r), chi.URLParam(r, "id"), q.Get("format"), q.Get("sheet"))
		if err != nil {
			return err
		}
		h := w.Header()
		h.Set("Content-Type", f.ContentType)
		h.Set("Content-Length", strconv.Itoa(len(f.Data)))
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
		h.Set("Cache-Control", "private, no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		_, err = w.Write(f.Data)
		return err
	}))
	// A filled-in sheet: what it would change. Saving is PUT .../marks.
	r.Method("POST", "/offline-assignments/{id}/sheet", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxUpload))
		if err != nil {
			return httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", "a mark sheet can be at most 5 MB")
		}
		if len(data) == 0 {
			return httpx.Invalid(map[string]string{"file": "choose a file"})
		}
		up, err := s.ReadUpload(r.Context(), uid(r), chi.URLParam(r, "id"), data)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, up)
		return nil
	}))
}

// StudentRoutes are what students see of offline assignments.
func (s *Service) StudentRoutes(r chi.Router) {
	s.presStudentRoutes(r) // group presentations (D-166)
	r.Method("GET", "/my/offline-assignments/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.MyOfflineView(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, v)
		return nil
	}))
}
