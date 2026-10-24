package presentation

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

func uid(r *http.Request) string { return auth.MustUser(r.Context()).ID }

func reply[T any](status int, f func(w http.ResponseWriter, r *http.Request) (T, error)) http.Handler {
	return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, err := f(w, r)
		if err != nil {
			return err
		}
		httpx.JSON(w, status, v)
		return nil
	})
}

func intParam(r *http.Request, name string) int {
	n, err := strconv.Atoi(chi.URLParam(r, name))
	if err != nil {
		return -1
	}
	return n
}

// sendJPEG writes a slide. A slide of a given version never changes, so it
// may be kept for a day, by this browser only.
func sendJPEG(w http.ResponseWriter, data []byte, versioned bool) {
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if versioned {
		h.Set("Cache-Control", "private, max-age=86400, immutable")
	} else {
		h.Set("Cache-Control", "private, no-cache")
	}
	_, _ = w.Write(data)
}

func sendPDF(w http.ResponseWriter, f PDF) {
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Length", strconv.Itoa(len(f.Data)))
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, no-store")
	_, _ = w.Write(f.Data)
}

// TeacherRoutes mounts building presentations and their reports under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/topics/{id}/presentations", reply(200, func(w http.ResponseWriter, r *http.Request) (any, error) {
		list, err := s.List(r.Context(), uid(r), chi.URLParam(r, "id"))
		return map[string]any{"presentations": list}, err
	}))
	r.Method("POST", "/topics/{id}/presentations", reply(201, func(w http.ResponseWriter, r *http.Request) (Presentation, error) {
		var in Input
		if err := httpx.Decode(w, r, &in); err != nil {
			return Presentation{}, err
		}
		return s.Create(r.Context(), uid(r), chi.URLParam(r, "id"), in)
	}))
	r.Method("GET", "/presentations/{id}", reply(200, func(w http.ResponseWriter, r *http.Request) (Presentation, error) {
		return s.Get(r.Context(), uid(r), chi.URLParam(r, "id"))
	}))
	r.Method("PUT", "/presentations/{id}", reply(200, func(w http.ResponseWriter, r *http.Request) (Presentation, error) {
		var in Input
		if err := httpx.Decode(w, r, &in); err != nil {
			return Presentation{}, err
		}
		return s.Update(r.Context(), uid(r), chi.URLParam(r, "id"), in)
	}))
	r.Method("DELETE", "/presentations/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Delete(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("POST", "/presentations/{id}/status", reply(200, func(w http.ResponseWriter, r *http.Request) (Presentation, error) {
		var in struct {
			Status string `json:"status"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return Presentation{}, err
		}
		return s.SetStatus(r.Context(), uid(r), chi.URLParam(r, "id"), in.Status)
	}))
	r.Method("POST", "/presentations/{id}/uploads", reply(201, func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in BeginInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return nil, err
		}
		v, err := s.Begin(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		return map[string]int{"version": v}, err
	}))
	r.Method("PUT", "/presentations/{id}/uploads/{version}/pdf", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, MaxPDFBytes+1024)
		if err := s.PutPDF(r.Context(), uid(r), chi.URLParam(r, "id"), intParam(r, "version"), r.Body); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	// A slide: multipart, with the JPEG as "image" and the slide's text as "text".
	r.Method("PUT", "/presentations/{id}/uploads/{version}/slides/{n}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, MaxSlideBytes+maxSlideText*4+16<<10)
		if err := r.ParseMultipartForm(MaxSlideBytes + 64<<10); err != nil {
			return httpx.Invalid(map[string]string{"image": "send the slide as a JPEG of at most 3 MB"})
		}
		defer r.MultipartForm.RemoveAll()
		f, _, err := r.FormFile("image")
		if err != nil {
			return httpx.Invalid(map[string]string{"image": "send the slide's image"})
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		if err := s.PutSlide(r.Context(), uid(r), chi.URLParam(r, "id"), intParam(r, "version"), intParam(r, "n"), r.FormValue("text"), bytes.NewReader(data)); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("POST", "/presentations/{id}/uploads/{version}/finish", reply(200, func(w http.ResponseWriter, r *http.Request) (Presentation, error) {
		return s.Finish(r.Context(), uid(r), chi.URLParam(r, "id"), intParam(r, "version"))
	}))
	r.Method("GET", "/presentations/{id}/slides", reply(200, func(w http.ResponseWriter, r *http.Request) (any, error) {
		sl, err := s.Slides(r.Context(), uid(r), chi.URLParam(r, "id"))
		return map[string]any{"slides": sl}, err
	}))
	r.Method("GET", "/presentations/{id}/slides/{n}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, _ := strconv.Atoi(r.URL.Query().Get("v"))
		data, err := s.TeacherImage(r.Context(), uid(r), chi.URLParam(r, "id"), v, intParam(r, "n"))
		if err != nil {
			return err
		}
		sendJPEG(w, data, v > 0)
		return nil
	}))
	r.Method("GET", "/presentations/{id}/pdf", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		f, err := s.TeacherPDF(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		sendPDF(w, f)
		return nil
	}))
	r.Method("GET", "/presentations/{id}/report", reply(200, func(w http.ResponseWriter, r *http.Request) (Report, error) {
		return s.Report(r.Context(), uid(r), chi.URLParam(r, "id"))
	}))
	r.Method("GET", "/presentations/{id}/viewers/{viewer}", reply(200, func(w http.ResponseWriter, r *http.Request) (ViewerDetail, error) {
		return s.Viewer(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "viewer"))
	}))
}

// StudentRoutes mounts viewing presentations under /api (students).
func (s *Service) StudentRoutes(r chi.Router) {
	r.Method("GET", "/my/presentations", reply(200, func(w http.ResponseWriter, r *http.Request) (any, error) {
		list, err := s.MyPresentations(r.Context(), uid(r))
		return map[string]any{"presentations": list}, err
	}))
	r.Method("POST", "/presentations/{id}/open", reply(200, func(w http.ResponseWriter, r *http.Request) (View, error) {
		return s.Open(r.Context(), uid(r), chi.URLParam(r, "id"))
	}))
	r.Method("POST", "/presentations/{id}/go", reply(200, func(w http.ResponseWriter, r *http.Request) (View, error) {
		var in struct {
			To int `json:"to"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return View{}, err
		}
		return s.Go(r.Context(), uid(r), chi.URLParam(r, "id"), in.To)
	}))
	r.Method("POST", "/presentations/{id}/time", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Slide   int `json:"slide"`
			Seconds int `json:"seconds"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		if err := s.Time(r.Context(), uid(r), chi.URLParam(r, "id"), in.Slide, in.Seconds); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("GET", "/presentations/{id}/slides/{n}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, _ := strconv.Atoi(r.URL.Query().Get("v"))
		data, err := s.Image(r.Context(), uid(r), chi.URLParam(r, "id"), v, intParam(r, "n"))
		if err != nil {
			return err
		}
		sendJPEG(w, data, true)
		return nil
	}))
	r.Method("GET", "/presentations/{id}/pdf", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		f, err := s.Download(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		sendPDF(w, f)
		return nil
	}))
}
