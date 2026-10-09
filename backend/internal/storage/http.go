package storage

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// BackupSorts: newest first by default.
var BackupSorts = page.Sorts{"started": "b.started_at", "size": "b.size_bytes"}

// AdminRoutes mounts under /api/admin. Storage, clean-up and backups are
// separate features, each checked on every request (PL-NFR-02).
func (s *Service) AdminRoutes(r chi.Router, a *auth.Service) {
	r.Group(func(r chi.Router) {
		r.Use(a.RequireFeature(auth.FeatStorage))
		r.Method("GET", "/storage", httpx.Handler(s.handleOverview))
		r.Method("POST", "/storage/refresh", httpx.Handler(s.handleOverview))
		r.Method("PUT", "/storage/limits", httpx.Handler(s.handleSetLimits))
	})
	r.Group(func(r chi.Router) {
		r.Use(a.RequireFeature(auth.FeatCleanup))
		r.Method("GET", "/storage/cleanup", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			httpx.JSON(w, 200, map[string]any{"areas": s.areas})
			return nil
		}))
		r.Method("POST", "/storage/cleanup/preview", httpx.Handler(s.handleCleanup(true)))
		r.Method("POST", "/storage/cleanup", httpx.Handler(s.handleCleanup(false)))
	})
	r.Group(func(r chi.Router) {
		r.Use(a.RequireFeature(auth.FeatBackups))
		r.Method("GET", "/backups", httpx.Handler(s.handleListBackups))
		r.Method("POST", "/backups", httpx.Handler(s.handleStartBackup))
		r.Method("DELETE", "/backups/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			if err := s.DeleteBackup(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "id")); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
		r.Method("POST", "/backups/{id}/link", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			token, exp, err := s.DownloadLink(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "id"))
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusCreated, map[string]any{"url": "/api/admin/backups/download/" + token, "expires_at": exp})
			return nil
		}))
		r.Method("GET", "/backups/download/{token}", httpx.Handler(s.handleDownload))
	})
}

func (s *Service) handleOverview(w http.ResponseWriter, r *http.Request) error {
	o, err := s.Overview(r.Context(), r.Method == http.MethodPost)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, o)
	return nil
}

func (s *Service) handleSetLimits(w http.ResponseWriter, r *http.Request) error {
	var l Limits
	if err := httpx.Decode(w, r, &l); err != nil {
		return err
	}
	l, err := s.SetLimits(r.Context(), auth.MustUser(r.Context()).ID, l)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, l)
	return nil
}

func (s *Service) handleCleanup(preview bool) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Area   string `json:"area"`
			Before string `json:"before"` // YYYY-MM-DD: data from before this day
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		before, err := time.Parse(time.DateOnly, in.Before)
		if err != nil {
			return httpx.Invalid(map[string]string{"before": "choose a date"})
		}
		var f Freed
		if preview {
			f, err = s.PreviewCleanup(r.Context(), in.Area, before)
		} else {
			f, err = s.Cleanup(r.Context(), auth.MustUser(r.Context()).ID, in.Area, before)
		}
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, f)
		return nil
	}
}

func (s *Service) handleListBackups(w http.ResponseWriter, r *http.Request) error {
	p := page.Parse(r, BackupSorts, "started", true)
	where := `WHERE ($1 = '' OR b.kind = $1)`
	kind := r.URL.Query().Get("kind")
	var total int
	if err := s.pool.QueryRow(r.Context(), `SELECT count(*) FROM storage.backups b `+where, kind).Scan(&total); err != nil {
		return err
	}
	order := p.OrderBy(BackupSorts, "b.id")
	list, err := s.listBackups(r.Context(), where, p.Size, p.Offset(), order[len(" ORDER BY "):], kind)
	if err != nil {
		return err
	}
	// The nightly job's state goes with the history, for managers who have
	// "Backups" but not "Storage".
	httpx.JSON(w, http.StatusOK, map[string]any{"backups": list, "total": total, "page": p.Page, "size": p.Size, "nightly": s.nightlyStatus()})
	return nil
}

func (s *Service) handleStartBackup(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Passphrase string `json:"passphrase"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	b, err := s.StartBackup(r.Context(), auth.MustUser(r.Context()), in.Passphrase)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusAccepted, b)
	return nil
}

func (s *Service) handleDownload(w http.ResponseWriter, r *http.Request) error {
	f, b, err := s.OpenDownload(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "token"))
	if err != nil {
		return err
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", b.Name))
	w.Header().Set("Content-Length", strconv.FormatInt(b.SizeBytes, 10))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, f)
	return nil
}
