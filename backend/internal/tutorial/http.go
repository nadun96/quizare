package tutorial

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

func uid(r *http.Request) string { return auth.MustUser(r.Context()).ID }

// reply wraps the common shape: run the action, then write its result.
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

// TeacherRoutes mounts building and following tutorials under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/topics/{id}/tutorials", reply(200, func(w http.ResponseWriter, r *http.Request) (any, error) {
		list, err := s.List(r.Context(), uid(r), chi.URLParam(r, "id"))
		return map[string]any{"tutorials": list}, err
	}))
	r.Method("POST", "/topics/{id}/tutorials", reply(201, func(w http.ResponseWriter, r *http.Request) (Tutorial, error) {
		var in TutorialInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return Tutorial{}, err
		}
		return s.Create(r.Context(), uid(r), chi.URLParam(r, "id"), in)
	}))
	r.Method("GET", "/tutorials/{id}", reply(200, func(w http.ResponseWriter, r *http.Request) (Editor, error) {
		return s.Get(r.Context(), uid(r), chi.URLParam(r, "id"))
	}))
	r.Method("PUT", "/tutorials/{id}", reply(200, func(w http.ResponseWriter, r *http.Request) (Tutorial, error) {
		var in TutorialInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return Tutorial{}, err
		}
		return s.Update(r.Context(), uid(r), chi.URLParam(r, "id"), in)
	}))
	r.Method("DELETE", "/tutorials/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.DeleteConfirmed(r.Context(), uid(r), chi.URLParam(r, "id"), r.URL.Query().Get("confirm")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("POST", "/tutorials/{id}/status", reply(200, func(w http.ResponseWriter, r *http.Request) (Tutorial, error) {
		var in struct {
			Status string `json:"status"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return Tutorial{}, err
		}
		return s.SetStatus(r.Context(), uid(r), chi.URLParam(r, "id"), in.Status)
	}))
	r.Method("POST", "/tutorials/{id}/release", reply(200, func(w http.ResponseWriter, r *http.Request) (Tutorial, error) {
		var in struct {
			Released bool `json:"released"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return Tutorial{}, err
		}
		return s.Release(r.Context(), uid(r), chi.URLParam(r, "id"), in.Released)
	}))
	r.Method("POST", "/tutorials/{id}/pages", reply(201, func(w http.ResponseWriter, r *http.Request) (Page, error) {
		var in PageInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return Page{}, err
		}
		return s.AddPage(r.Context(), uid(r), chi.URLParam(r, "id"), in)
	}))
	r.Method("PUT", "/tutorials/{id}/pages/{page}", reply(200, func(w http.ResponseWriter, r *http.Request) (Page, error) {
		var in PageInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return Page{}, err
		}
		return s.UpdatePage(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "page"), in)
	}))
	r.Method("DELETE", "/tutorials/{id}/pages/{page}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.DeletePage(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "page")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("POST", "/tutorials/{id}/pages/{page}/duplicate", reply(201, func(w http.ResponseWriter, r *http.Request) (Page, error) {
		return s.DuplicatePage(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "page"))
	}))
	r.Method("POST", "/tutorials/{id}/questions/copy", reply(201, func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return nil, err
		}
		qs, err := s.CopyQuestions(r.Context(), uid(r), chi.URLParam(r, "id"), in.IDs)
		return map[string]any{"questions": qs}, err
	}))
	r.Method("PUT", "/tutorials/{id}/page-order", reply(200, func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return nil, err
		}
		pages, err := s.Reorder(r.Context(), uid(r), chi.URLParam(r, "id"), in.IDs)
		return map[string]any{"pages": pages}, err
	}))
	r.Method("GET", "/tutorials/{id}/progress", reply(200, func(w http.ResponseWriter, r *http.Request) (Progress, error) {
		return s.Progress(r.Context(), uid(r), chi.URLParam(r, "id"))
	}))
	r.Method("GET", "/tutorials/{id}/analytics", reply(200, func(w http.ResponseWriter, r *http.Request) (Analytics, error) {
		return s.Analytics(r.Context(), uid(r), chi.URLParam(r, "id"))
	}))
	r.Method("POST", "/tutorials/{id}/runs/{run}/unlock", reply(200, func(w http.ResponseWriter, r *http.Request) (RunDetail, error) {
		var in struct {
			PageID string `json:"page_id"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return RunDetail{}, err
		}
		return s.Unlock(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "run"), in.PageID)
	}))
	r.Method("GET", "/tutorials/{id}/runs/{run}", reply(200, func(w http.ResponseWriter, r *http.Request) (RunDetail, error) {
		return s.Run(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "run"))
	}))
	r.Method("PUT", "/tutorials/{id}/runs/{run}/answers/{question}", reply(200, func(w http.ResponseWriter, r *http.Request) (RunDetail, error) {
		var in MarkInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return RunDetail{}, err
		}
		return s.Mark(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "run"), chi.URLParam(r, "question"), in)
	}))
}

// StudentRoutes mounts working through tutorials under /api (students).
func (s *Service) StudentRoutes(r chi.Router) {
	r.Method("GET", "/my/tutorials", reply(200, func(w http.ResponseWriter, r *http.Request) (any, error) {
		list, err := s.Mine(r.Context(), uid(r))
		return map[string]any{"tutorials": list}, err
	}))
	r.Method("POST", "/tutorials/{id}/open", reply(200, func(w http.ResponseWriter, r *http.Request) (RunView, error) {
		return s.withByline(r.Context())(s.Open(r.Context(), uid(r), chi.URLParam(r, "id")))
	}))
	r.Method("GET", "/tutorials/{id}/pages/{n}", reply(200, func(w http.ResponseWriter, r *http.Request) (RunView, error) {
		n, err := strconv.Atoi(chi.URLParam(r, "n"))
		if err != nil {
			return RunView{}, httpx.ErrNotFound
		}
		return s.withByline(r.Context())(s.Page(r.Context(), uid(r), chi.URLParam(r, "id"), n))
	}))
	r.Method("POST", "/tutorials/{id}/go", reply(200, func(w http.ResponseWriter, r *http.Request) (RunView, error) {
		var in struct {
			To int `json:"to"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return RunView{}, err
		}
		return s.withByline(r.Context())(s.Go(r.Context(), uid(r), chi.URLParam(r, "id"), in.To))
	}))
	r.Method("POST", "/tutorials/{id}/answers", reply(200, func(w http.ResponseWriter, r *http.Request) (RunView, error) {
		var in AnswerInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return RunView{}, err
		}
		return s.withByline(r.Context())(s.Answer(r.Context(), uid(r), chi.URLParam(r, "id"), in))
	}))
	r.Method("POST", "/tutorials/{id}/time", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Page    int `json:"page"`
			Seconds int `json:"seconds"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		if err := s.Time(r.Context(), uid(r), chi.URLParam(r, "id"), in.Page, in.Seconds); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("POST", "/tutorials/{id}/leave", reply(200, func(w http.ResponseWriter, r *http.Request) (RunView, error) {
		var in LeaveInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return RunView{}, err
		}
		return s.withByline(r.Context())(s.Leave(r.Context(), uid(r), chi.URLParam(r, "id"), in))
	}))
	r.Method("POST", "/tutorials/{id}/finish", reply(200, func(w http.ResponseWriter, r *http.Request) (RunView, error) {
		return s.withByline(r.Context())(s.Finish(r.Context(), uid(r), chi.URLParam(r, "id")))
	}))
}
