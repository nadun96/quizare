package poll

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

// Presentations in polls (D-165): the teacher's routes under /api/teacher,
// the participants' under /api/polls.

const maxDeckBytes = 100 << 20

func slideNumber(r *http.Request) int {
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

func ok(w http.ResponseWriter, err error) error {
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) presTeacherRoutes(r chi.Router) {
	r.Method("GET", "/polls/{id}/presentations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		st, err := s.PresSettingsOf(r.Context(), teacherID(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		list, err := s.Submissions(r.Context(), teacherID(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		p, err := s.pollByID(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		stage, err := s.stage(r.Context(), p)
		if err != nil {
			return err
		}
		var presenter *string
		_ = s.pool.QueryRow(r.Context(), `SELECT presenter::text FROM poll.stage WHERE poll_id = $1`, p.ID).Scan(&presenter)
		httpx.JSON(w, http.StatusOK, map[string]any{"settings": st, "submissions": list, "stage": stage, "presenter_id": presenter})
		return nil
	}))
	r.Method("PUT", "/polls/{id}/presentations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in PresSettings
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		st, err := s.SetPresSettings(r.Context(), teacherID(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, st)
		return nil
	}))
	r.Method("PUT", "/poll-submissions/{id}/mark", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Mark    *float64 `json:"mark"`
			Comment string   `json:"comment"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return ok(w, s.MarkSubmission(r.Context(), teacherID(r), chi.URLParam(r, "id"), in.Mark, in.Comment))
	}))
	r.Method("DELETE", "/poll-submissions/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, s.DeleteSubmission(r.Context(), teacherID(r), chi.URLParam(r, "id")))
	}))
	r.Method("GET", "/poll-submissions/{id}/slides/{n}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		img, err := s.TeacherSlideImage(r.Context(), teacherID(r), chi.URLParam(r, "id"), slideNumber(r))
		if err != nil {
			return err
		}
		sendSlide(w, img)
		return nil
	}))
	r.Method("PUT", "/polls/{id}/stage", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in StageInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return ok(w, s.SetStage(r.Context(), teacherID(r), chi.URLParam(r, "id"), in))
	}))
	r.Method("DELETE", "/polls/{id}/stage", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, s.ClearStage(r.Context(), teacherID(r), chi.URLParam(r, "id")))
	}))
	r.Method("PUT", "/polls/{id}/stage/slide", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			N int `json:"n"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return ok(w, s.TeacherSlide(r.Context(), teacherID(r), chi.URLParam(r, "id"), in.N))
	}))
}

func (s *Service) presPublicRoutes(r chi.Router) {
	r.Method("GET", "/{code}/presentations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.MyPres(r.Context(), chi.URLParam(r, "code"), caller(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, v)
		return nil
	}))
	r.Method("POST", "/{code}/presentations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in SubmitInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		x, err := s.StartSubmission(r.Context(), chi.URLParam(r, "code"), caller(r), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, x)
		return nil
	}))
	r.Method("PUT", "/{code}/presentations/{id}/slides/{n}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, maxSlideBytes+64<<10)
		if err := r.ParseMultipartForm(maxSlideBytes + 64<<10); err != nil {
			return httpx.Invalid(map[string]string{"image": "a JPEG picture of at most 1 MB"})
		}
		f, _, err := r.FormFile("image")
		if err != nil {
			return httpx.Invalid(map[string]string{"image": "send the slide as image"})
		}
		defer f.Close()
		img, err := io.ReadAll(io.LimitReader(f, maxSlideBytes+1))
		if err != nil {
			return err
		}
		return ok(w, s.PutSlide(r.Context(), chi.URLParam(r, "code"), caller(r), chi.URLParam(r, "id"), slideNumber(r), img, r.FormValue("text")))
	}))
	r.Method("POST", "/{code}/presentations/{id}/finish", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		x, err := s.FinishSubmission(r.Context(), chi.URLParam(r, "code"), caller(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, x)
		return nil
	}))
	r.Method("GET", "/{code}/presentations/{id}/slides/{n}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		img, err := s.PublicSlide(r.Context(), chi.URLParam(r, "code"), caller(r), chi.URLParam(r, "id"), slideNumber(r))
		if err != nil {
			return err
		}
		sendSlide(w, img)
		return nil
	}))
	r.Method("PUT", "/{code}/presentations/{id}/rating", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Stars int `json:"stars"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return ok(w, s.Rate(r.Context(), chi.URLParam(r, "code"), caller(r), chi.URLParam(r, "id"), in.Stars))
	}))
	r.Method("PUT", "/{code}/stage/slide", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			N int `json:"n"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		return ok(w, s.PresenterSlide(r.Context(), chi.URLParam(r, "code"), caller(r), in.N))
	}))
	// PowerPoint in, PDF out, through the teacher's Google Drive.
	r.Method("POST", "/{code}/presentations/convert", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, maxDeckBytes+1024)
		name, _ := url.QueryUnescape(r.Header.Get("X-File-Name"))
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return httpx.Invalid(map[string]string{"file": "a presentation of at most 100 MB"})
		}
		pdf, err := s.Convert(r.Context(), chi.URLParam(r, "code"), caller(r), name, data)
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
}
