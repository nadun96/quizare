package live

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
)

func uid(r *http.Request) string { return auth.MustUser(r.Context()).ID }
func pid(r *http.Request) string { return chi.URLParam(r, "id") }

// TeacherRoutes mounts session control under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("POST", "/quizzes/{id}/sessions", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in SessionInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		sess, err := s.CreateSession(r.Context(), uid(r), pid(r), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, 201, s.sessionView(sess))
		return nil
	}))
	r.Method("GET", "/quizzes/{id}/sessions", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.ListSessions(r.Context(), uid(r), pid(r))
		if err != nil {
			return err
		}
		if list == nil {
			list = []Session{}
		}
		httpx.JSON(w, 200, map[string]any{"sessions": list})
		return nil
	}))
	r.Method("GET", "/sessions/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		d, err := s.Dashboard(r.Context(), uid(r), pid(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, d)
		return nil
	}))
	r.Method("PUT", "/sessions/{id}/settings", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var o settings.Overrides
		if err := httpx.Decode(w, r, &o); err != nil {
			return err
		}
		sess, err := s.UpdateSettings(r.Context(), uid(r), pid(r), o)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, sess)
		return nil
	}))
	bulk := func(fn func(r *http.Request, t Target) (int, error)) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			var t Target
			if err := httpx.Decode(w, r, &t); err != nil {
				return err
			}
			n, err := fn(r, t)
			if err != nil {
				return err
			}
			httpx.JSON(w, 200, map[string]int{"affected": n})
			return nil
		}
	}
	r.Method("POST", "/sessions/{id}/admit", bulk(func(r *http.Request, t Target) (int, error) { return s.Admit(r.Context(), uid(r), pid(r), t) }))
	r.Method("POST", "/sessions/{id}/pause", bulk(func(r *http.Request, t Target) (int, error) { return s.Pause(r.Context(), uid(r), pid(r), t) }))
	r.Method("POST", "/sessions/{id}/resume", bulk(func(r *http.Request, t Target) (int, error) { return s.Resume(r.Context(), uid(r), pid(r), t) }))
	r.Method("POST", "/sessions/{id}/extend", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Target
			Seconds int `json:"seconds"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		n, err := s.Extend(r.Context(), uid(r), pid(r), in.Target, in.Seconds)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]int{"affected": n})
		return nil
	}))
	r.Method("POST", "/sessions/{id}/end", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.End(r.Context(), uid(r), pid(r)); err != nil {
			return err
		}
		w.WriteHeader(204)
		return nil
	}))
	r.Method("GET", "/sessions/{id}/events", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		ev, err := s.Events(r.Context(), uid(r), pid(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]any{"events": ev})
		return nil
	}))
	r.Method("POST", "/sessions/{id}/release", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Release(r.Context(), uid(r), pid(r)); err != nil {
			return err
		}
		w.WriteHeader(204)
		return nil
	}))
	r.Method("POST", "/sessions/{id}/unrelease", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Unrelease(r.Context(), uid(r), pid(r)); err != nil {
			return err
		}
		w.WriteHeader(204)
		return nil
	}))
	r.Method("POST", "/attempts/{id}/reinstate", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		a, err := s.Reinstate(r.Context(), uid(r), pid(r), in.Reason)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, a)
		return nil
	}))
}

// StudentRoutes mounts joining and answering under /api (student role).
func (s *Service) StudentRoutes(r chi.Router) {
	r.Method("GET", "/join/sessions/{code}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p, err := s.Preview(r.Context(), uid(r), chi.URLParam(r, "code"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, p)
		return nil
	}))
	r.Method("POST", "/join/sessions/{code}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			StudentNumber string `json:"student_number"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		a, err := s.Join(r.Context(), uid(r), chi.URLParam(r, "code"), in.StudentNumber)
		if err != nil {
			return err
		}
		st, err := s.StudentState(r.Context(), uid(r), a.ID)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, st)
		return nil
	}))
	state := func(w http.ResponseWriter, r *http.Request, err error) error {
		if err != nil {
			return err
		}
		st, err := s.StudentState(r.Context(), uid(r), pid(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, st)
		return nil
	}
	r.Method("GET", "/attempts/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return state(w, r, nil)
	}))
	r.Method("POST", "/attempts/{id}/start", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		_, err := s.Start(r.Context(), uid(r), pid(r))
		return state(w, r, err)
	}))
	// Answer saves stay on REST: idempotent, retryable, standard auth (ADR-04).
	r.Method("PUT", "/attempts/{id}/answers/{qid}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Response quiz.Response `json:"response"`
			Seq      int64         `json:"seq"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		if err := s.SaveAnswer(r.Context(), uid(r), pid(r), chi.URLParam(r, "qid"), in.Response, in.Seq); err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]any{"saved": true, "server_time": s.now().UnixMilli()})
		return nil
	}))
	r.Method("POST", "/attempts/{id}/advance", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			QuestionID string `json:"question_id"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		_, err := s.Advance(r.Context(), uid(r), pid(r), in.QuestionID)
		return state(w, r, err)
	}))
	r.Method("POST", "/attempts/{id}/goto", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Index int `json:"index"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		_, err := s.Goto(r.Context(), uid(r), pid(r), in.Index)
		return state(w, r, err)
	}))
	r.Method("POST", "/attempts/{id}/submit", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		_, err := s.Submit(r.Context(), uid(r), pid(r))
		return state(w, r, err)
	}))
	r.Method("POST", "/attempts/{id}/violations", httpx.Handler(s.handleViolation))
	r.Method("GET", "/my/attempts", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.MyAttempts(r.Context(), uid(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]any{"attempts": list})
		return nil
	}))
}

func (s *Service) handleViolation(w http.ResponseWriter, r *http.Request) error {
	var in ViolationInput
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return httpx.BadRequest("unreadable body")
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return httpx.BadRequest("invalid JSON body")
	}
	in.Device = r.UserAgent()
	if _, err := s.ReportViolation(r.Context(), uid(r), pid(r), in); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

// BeaconRoutes accepts navigator.sendBeacon violation reports, sent when the
// page is being hidden or closed and the socket may already be gone (ADR-08).
// sendBeacon cannot set custom headers, so this route checks Origin only;
// the body is JSON sent as text/plain to avoid a preflight.
func (s *Service) BeaconRoutes(r chi.Router) {
	origin := httpx.Origin(s.baseURL)
	r.Method("POST", "/attempts/{id}/violations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if !httpx.OriginAllowed(r, origin) {
			return httpx.ErrForbidden
		}
		if _, ok := auth.CurrentUser(r.Context()); !ok {
			return httpx.ErrUnauthorized
		}
		return s.handleViolation(w, r)
	}))
}
