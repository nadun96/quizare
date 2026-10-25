package drive

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

func me(r *http.Request) string { return auth.MustUser(r.Context()).ID }

type fileIn struct {
	FileID string `json:"file_id"`
}

func decodeFile(w http.ResponseWriter, r *http.Request) (string, error) {
	var in fileIn
	if err := httpx.Decode(w, r, &in); err != nil {
		return "", err
	}
	if in.FileID == "" || len(in.FileID) > 200 {
		return "", httpx.Invalid(map[string]string{"file_id": "choose a file"})
	}
	return in.FileID, nil
}

// TeacherRoutes mounts the teacher's Drive under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/drive", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		st, err := s.Status(r.Context(), me(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, st)
		return nil
	}))
	r.Method("PUT", "/drive", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			AutoBackup bool `json:"auto_backup"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		if err := s.SetAutoBackup(r.Context(), me(r), in.AutoBackup); err != nil {
			return err
		}
		st, err := s.Status(r.Context(), me(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, st)
		return nil
	}))
	r.Method("DELETE", "/drive", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Disconnect(r.Context(), me(r)); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	// Browser navigations: to Google and back.
	r.Method("GET", "/drive/connect", httpx.Handler(s.connect))
	r.Get("/drive/callback", s.callback)
	r.Method("POST", "/drive/picker", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		t, err := s.Picker(r.Context(), me(r))
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		httpx.JSON(w, http.StatusOK, t)
		return nil
	}))
	r.Method("POST", "/drive/share", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		id, err := decodeFile(w, r)
		if err != nil {
			return err
		}
		f, err := s.ShareByLink(r.Context(), me(r), id)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, f)
		return nil
	}))
	// Presentations (D-78): a PowerPoint file as the body, or a file picked
	// in Drive, back as a PDF for the browser to turn into slides.
	r.Method("POST", "/drive/slides-pdf", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, MaxDeckBytes+1024)
		name, _ := url.QueryUnescape(r.Header.Get("X-File-Name"))
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return httpx.Invalid(map[string]string{"file": "a presentation of at most 100 MB"})
		}
		pdf, err := s.SlidesToPDF(r.Context(), me(r), name, data)
		if err != nil {
			return err
		}
		sendPDF(w, pdf, strings.TrimSuffix(path.Base(name), path.Ext(name))+".pdf")
		return nil
	}))
	r.Method("POST", "/drive/files/{file}/slides-pdf", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		pdf, name, err := s.DrivePDF(r.Context(), me(r), chi.URLParam(r, "file"))
		if err != nil {
			return err
		}
		sendPDF(w, pdf, name)
		return nil
	}))
	r.Method("GET", "/drive/exports", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.Exports(r.Context(), me(r), r.URL.Query().Get("target_id"), 20)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"exports": list})
		return nil
	}))
	r.Method("POST", "/quizzes/{id}/questions/import-drive", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		id, err := decodeFile(w, r)
		if err != nil {
			return err
		}
		rep, err := s.ImportQuestions(r.Context(), me(r), chi.URLParam(r, "id"), id)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, rep)
		return nil
	}))
	r.Method("GET", "/classrooms/{id}/drive-files", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.ClassroomFiles(r.Context(), me(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"files": list})
		return nil
	}))
	r.Method("POST", "/classrooms/{id}/drive-files", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in AttachInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		list, err := s.Attach(r.Context(), me(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"files": list})
		return nil
	}))
	r.Method("PATCH", "/classrooms/{id}/drive-files/{file}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Sharing string `json:"sharing"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		list, err := s.SetSharing(r.Context(), me(r), chi.URLParam(r, "id"), chi.URLParam(r, "file"), in.Sharing)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"files": list})
		return nil
	}))
	r.Method("DELETE", "/classrooms/{id}/drive-files/{file}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Detach(r.Context(), me(r), chi.URLParam(r, "id"), chi.URLParam(r, "file")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	export := func(kind string) http.Handler {
		return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			e, err := s.Save(r.Context(), me(r), kind, chi.URLParam(r, "id"), false)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusCreated, e)
			return nil
		})
	}
	r.Method("POST", "/sessions/{id}/drive-export", export("session"))
	r.Method("POST", "/polls/{id}/drive-export", export("poll"))
	r.Method("POST", "/presentations/{id}/drive-export", export("presentation"))
}

// UserRoutes: a classroom's files for its students, under /api.
func (s *Service) UserRoutes(r chi.Router) {
	r.Method("GET", "/my/classrooms/{id}/drive-files", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.StudentFiles(r.Context(), me(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"files": list})
		return nil
	}))
}

// AdminRoutes: the platform's Google app; admins only.
func (s *Service) AdminRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.auth.AdminOnly())
		r.Method("GET", "/drive", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			c, _, err := s.config(r.Context())
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, map[string]any{"config": c, "redirect_url": s.RedirectURL(), "origin": httpx.Origin(s.baseURL)})
			return nil
		}))
		r.Method("PUT", "/drive", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var c Config
			if err := httpx.Decode(w, r, &c); err != nil {
				return err
			}
			saved, err := s.saveConfig(r.Context(), me(r), c)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, saved)
			return nil
		}))
	})
}

// sendPDF answers with a converted PDF, for the browser to read, never to open as a page.
func sendPDF(w http.ResponseWriter, data []byte, name string) {
	h := w.Header()
	h.Set("Content-Type", MimePDF)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("X-File-Name", url.QueryEscape(name))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
