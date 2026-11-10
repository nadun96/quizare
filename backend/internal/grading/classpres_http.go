package grading

import (
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Group presentations in a classroom (D-166): the teachers' routes under
// /api/teacher, the students' under /api.

const maxDeckBytes = 100 << 20

func slideN(r *http.Request) int {
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil {
		return -1
	}
	return n
}

func sendSlide(w http.ResponseWriter, img []byte) {
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Length", strconv.Itoa(len(img)))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(img)
}

func done(w http.ResponseWriter, err error) error {
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func jsonOr(w http.ResponseWriter, status int, v any, err error) error {
	if err != nil {
		return err
	}
	httpx.JSON(w, status, v)
	return nil
}

type turnInput struct {
	N int `json:"n"`
}

func (s *Service) presTeacherRoutes(r chi.Router) {
	r.Method("GET", "/classrooms/{id}/presentation-tasks", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.PresTasks(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		st, err := s.TeacherStage(r.Context(), uid(r), chi.URLParam(r, "id"))
		return jsonOr(w, http.StatusOK, map[string]any{"tasks": list, "stage": st}, err)
	}))
	r.Method("POST", "/classrooms/{id}/presentation-tasks", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in PresTaskInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		t, err := s.CreatePresTask(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		return jsonOr(w, http.StatusCreated, t, err)
	}))
	r.Method("GET", "/presentation-tasks/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.PresTaskView(r.Context(), uid(r), chi.URLParam(r, "id"))
		return jsonOr(w, http.StatusOK, v, err)
	}))
	r.Method("PUT", "/presentation-tasks/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in PresTaskInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		t, err := s.UpdatePresTask(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		return jsonOr(w, http.StatusOK, t, err)
	}))
	r.Method("DELETE", "/presentation-tasks/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return done(w, s.DeletePresTask(r.Context(), uid(r), chi.URLParam(r, "id")))
	}))
	// Every group's slides of a task go; marks and ratings stay.
	r.Method("DELETE", "/presentation-tasks/{id}/slides", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		n, err := s.RemoveSlides(r.Context(), uid(r), chi.URLParam(r, "id"), "")
		return jsonOr(w, http.StatusOK, map[string]int{"removed": n}, err)
	}))
	r.Method("PUT", "/group-presentations/{id}/allowed", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Allowed bool `json:"allowed"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return done(w, s.AllowPresenting(r.Context(), uid(r), chi.URLParam(r, "id"), in.Allowed))
	}))
	r.Method("PUT", "/group-presentations/{id}/mark", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Mark    *float64 `json:"mark"`
			Comment string   `json:"comment"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return done(w, s.MarkPres(r.Context(), uid(r), chi.URLParam(r, "id"), in.Mark, in.Comment))
	}))
	r.Method("DELETE", "/group-presentations/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return done(w, s.DeletePres(r.Context(), uid(r), chi.URLParam(r, "id")))
	}))
	// One group's slides go; its mark and ratings stay.
	r.Method("DELETE", "/group-presentations/{id}/slides", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		_, err := s.RemoveSlides(r.Context(), uid(r), "", chi.URLParam(r, "id"))
		return done(w, err)
	}))
	r.Method("GET", "/group-presentations/{id}/slides/{n}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		img, err := s.TeacherPresSlide(r.Context(), uid(r), chi.URLParam(r, "id"), slideN(r))
		if err != nil {
			return err
		}
		sendSlide(w, img)
		return nil
	}))
	// The teacher presents it (or takes over from the student presenting it).
	r.Method("POST", "/group-presentations/{id}/show", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return done(w, s.ShowPres(r.Context(), uid(r), chi.URLParam(r, "id")))
	}))
	r.Method("GET", "/classrooms/{id}/stage", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		st, err := s.TeacherStage(r.Context(), uid(r), chi.URLParam(r, "id"))
		return jsonOr(w, http.StatusOK, map[string]any{"stage": st}, err)
	}))
	r.Method("DELETE", "/classrooms/{id}/stage", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return done(w, s.StopStage(r.Context(), uid(r), chi.URLParam(r, "id")))
	}))
	r.Method("PUT", "/classrooms/{id}/stage/slide", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in turnInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return done(w, s.TeacherTurn(r.Context(), uid(r), chi.URLParam(r, "id"), in.N))
	}))
}

func (s *Service) presStudentRoutes(r chi.Router) {
	r.Method("GET", "/my/classrooms/{id}/presentations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.MyPresentations(r.Context(), uid(r), chi.URLParam(r, "id"))
		return jsonOr(w, http.StatusOK, v, err)
	}))
	r.Method("GET", "/my/classrooms/{id}/stage", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		st, err := s.MyStage(r.Context(), uid(r), chi.URLParam(r, "id"))
		return jsonOr(w, http.StatusOK, map[string]any{"stage": st}, err)
	}))
	r.Method("PUT", "/my/classrooms/{id}/stage/slide", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in turnInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return done(w, s.StudentTurn(r.Context(), uid(r), chi.URLParam(r, "id"), in.N))
	}))
	r.Method("DELETE", "/my/classrooms/{id}/stage", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return done(w, s.EndMyPresenting(r.Context(), uid(r), chi.URLParam(r, "id")))
	}))
	// PowerPoint in, PDF out, through the classroom owner's Google Drive.
	r.Method("POST", "/my/classrooms/{id}/presentations/convert", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, maxDeckBytes+1024)
		name, _ := url.QueryUnescape(r.Header.Get("X-File-Name"))
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return httpx.Invalid(map[string]string{"file": "a presentation of at most 100 MB"})
		}
		pdf, err := s.ConvertForClass(r.Context(), uid(r), chi.URLParam(r, "id"), name, data)
		if err != nil {
			return err
		}
		h := w.Header()
		h.Set("Content-Type", "application/pdf")
		h.Set("Content-Disposition", `attachment; filename="`+strings.TrimSuffix(path.Base(name), path.Ext(name))+`.pdf"`)
		h.Set("Cache-Control", "private, no-store")
		_, err = w.Write(pdf)
		return err
	}))
	r.Method("POST", "/my/presentation-tasks/{id}/presentations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in GroupPresInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		x, err := s.StartGroupPres(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		return jsonOr(w, http.StatusCreated, x, err)
	}))
	r.Method("PUT", "/my/group-presentations/{id}/slides/{n}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, maxPresSlideBytes+64<<10)
		if err := r.ParseMultipartForm(maxPresSlideBytes + 64<<10); err != nil {
			return httpx.Invalid(map[string]string{"image": "a JPEG picture of at most 1 MB"})
		}
		defer r.MultipartForm.RemoveAll()
		f, _, err := r.FormFile("image")
		if err != nil {
			return httpx.Invalid(map[string]string{"image": "send the slide as image"})
		}
		defer f.Close()
		img, err := io.ReadAll(io.LimitReader(f, maxPresSlideBytes+1))
		if err != nil {
			return err
		}
		return done(w, s.PutPresSlide(r.Context(), uid(r), chi.URLParam(r, "id"), slideN(r), img, r.FormValue("text")))
	}))
	r.Method("POST", "/my/group-presentations/{id}/finish", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		x, err := s.FinishGroupPres(r.Context(), uid(r), chi.URLParam(r, "id"))
		return jsonOr(w, http.StatusOK, x, err)
	}))
	r.Method("GET", "/my/group-presentations/{id}/slides/{n}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		img, err := s.MyPresSlide(r.Context(), uid(r), chi.URLParam(r, "id"), slideN(r))
		if err != nil {
			return err
		}
		sendSlide(w, img)
		return nil
	}))
	r.Method("POST", "/my/group-presentations/{id}/present", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		st, err := s.Present(r.Context(), uid(r), chi.URLParam(r, "id"))
		return jsonOr(w, http.StatusOK, map[string]any{"stage": st}, err)
	}))
	r.Method("PUT", "/my/group-presentations/{id}/rating", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Stars int `json:"stars"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return done(w, s.RatePres(r.Context(), uid(r), chi.URLParam(r, "id"), in.Stars))
	}))
}
