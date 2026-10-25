package tutor

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Routes mounts the API under /api; every route needs the platform's token.
func (s *Service) Routes(keys authn.Keys) func(chi.Router) {
	return func(r chi.Router) {
		r.Use(keys.Middleware)
		h := func(f web.Handler) http.Handler { return f }
		r.Method("POST", "/sessions", h(s.hCreate))
		r.Method("GET", "/sessions", h(s.hList))
		r.Method("POST", "/join/{code}", h(s.hJoin))
		r.Route("/sessions/{id}", func(r chi.Router) {
			r.Method("GET", "/", h(s.hView))
			r.Method("POST", "/media-token", h(s.hMediaToken))
			r.Method("POST", "/start", h(s.simple(s.Start)))
			r.Method("POST", "/end", h(s.simple(s.End)))
			r.Method("PUT", "/settings", h(s.hSettings))
			r.Method("POST", "/admit", h(s.hAdmit))
			r.Method("PUT", "/permissions", h(s.hPermissions))
			r.Method("POST", "/mute", h(s.hMute))
			r.Method("PUT", "/hand", h(s.hHand))
			r.Method("DELETE", "/participants/{user}/hand", h(s.hLowerHand))
			r.Method("DELETE", "/participants/{user}", h(s.hRemove))
			r.Method("PUT", "/participants/{user}/chat-muted", h(s.hMuteChat))
			r.Method("GET", "/chat", h(s.hHistory))
			r.Method("POST", "/chat", h(s.hPost))
			r.Method("DELETE", "/chat/{msg}", h(s.hDeleteMessage))
			r.Method("PUT", "/pin", h(s.hPin))
			r.Method("GET", "/attendance", h(s.hAttendance))
			r.Method("GET", "/coteachers", h(s.hCoteachers))
			r.Method("POST", "/coteachers", h(s.hAddCoteacher))
			r.Method("DELETE", "/coteachers/{user}", h(s.hRemoveCoteacher))
			r.Method("PUT", "/broadcaster", h(s.hBroadcaster))
			r.Method("POST", "/broadcast-answer", h(s.hBroadcastAnswer))
			r.Method("POST", "/ask-answer", h(s.hAskAnswer))
			// Breakout groups (D-79).
			r.Method("PUT", "/groups", h(s.hForm))
			r.Method("POST", "/groups/open", h(s.simple(s.OpenGroups)))
			r.Method("POST", "/groups/close", h(s.hCloseGroups))
			r.Method("PUT", "/groups/settings", h(s.hGroupSettings))
			r.Method("POST", "/groups/announce", h(s.hAnnounce))
			r.Method("PATCH", "/groups/{group}", h(s.hRenameGroup))
			r.Method("DELETE", "/groups/{group}/help", h(s.hDismissHelp))
			r.Method("PUT", "/participants/{user}/group", h(s.hMove))
			r.Method("PUT", "/my-group", h(s.hChoose))
			r.Method("PUT", "/visit", h(s.hVisit))
			r.Method("PUT", "/help", h(s.hHelp))
		})
	}
}

func me(r *http.Request) authn.Person { return authn.From(r.Context()) }
func id(r *http.Request) string       { return chi.URLParam(r, "id") }

// simple adapts an action on the session with no body and no reply.
func (s *Service) simple(f func(ctx context.Context, p authn.Person, id string) error) web.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		if err := f(r.Context(), me(r), id(r)); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

func (s *Service) hCreate(w http.ResponseWriter, r *http.Request) error {
	var in CreateInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	sess, err := s.Create(r.Context(), me(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, sess)
	return nil
}

func (s *Service) hList(w http.ResponseWriter, r *http.Request) error {
	pg := ParsePage(r, sessionSorts, "created", true)
	q := r.URL.Query()
	list, total, err := s.List(r.Context(), me(r), q.Get("classroom_id"), q.Get("status"), pg)
	if err != nil {
		return err
	}
	WritePage(w, "sessions", list, total, pg)
	return nil
}

func (s *Service) hJoin(w http.ResponseWriter, r *http.Request) error {
	v, err := s.Join(r.Context(), me(r), chi.URLParam(r, "code"))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) hView(w http.ResponseWriter, r *http.Request) error {
	v, err := s.View(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) hMediaToken(w http.ResponseWriter, r *http.Request) error {
	t, err := s.MediaToken(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	web.JSON(w, http.StatusOK, t)
	return nil
}

func (s *Service) hSettings(w http.ResponseWriter, r *http.Request) error {
	var in SettingsInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.Settings(r.Context(), me(r), id(r), in); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hAdmit(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		UserIDs []string `json:"user_ids"`
		Admit   bool     `json:"admit"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	n, err := s.Admit(r.Context(), me(r), id(r), in.UserIDs, in.Admit)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]int{"changed": n})
	return nil
}

func (s *Service) hPermissions(w http.ResponseWriter, r *http.Request) error {
	var in PermissionsInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	n, err := s.SetPermissions(r.Context(), me(r), id(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]int{"changed": n})
	return nil
}

func (s *Service) hMute(w http.ResponseWriter, r *http.Request) error {
	var in MuteInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.Mute(r.Context(), me(r), id(r), in); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hHand(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Raised bool `json:"raised"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.Hand(r.Context(), me(r), id(r), in.Raised); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hLowerHand(w http.ResponseWriter, r *http.Request) error {
	if err := s.LowerHand(r.Context(), me(r), id(r), chi.URLParam(r, "user")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hRemove(w http.ResponseWriter, r *http.Request) error {
	if err := s.Remove(r.Context(), me(r), id(r), chi.URLParam(r, "user")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hMuteChat(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Muted bool `json:"muted"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.MuteChat(r.Context(), me(r), id(r), chi.URLParam(r, "user"), in.Muted); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hHistory(w http.ResponseWriter, r *http.Request) error {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.History(r.Context(), me(r), id(r), before, limit)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"messages": list})
	return nil
}

func (s *Service) hPost(w http.ResponseWriter, r *http.Request) error {
	var in PostInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	m, err := s.Post(r.Context(), me(r), id(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, m)
	return nil
}

func (s *Service) hDeleteMessage(w http.ResponseWriter, r *http.Request) error {
	msg, err := strconv.ParseInt(chi.URLParam(r, "msg"), 10, 64)
	if err != nil {
		return web.ErrNotFound
	}
	if err := s.DeleteMessage(r.Context(), me(r), id(r), msg); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hPin(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.Pin(r.Context(), me(r), id(r), in.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hAttendance(w http.ResponseWriter, r *http.Request) error {
	if strings.Contains(r.Header.Get("Accept"), "text/csv") || r.URL.Query().Get("format") == "csv" {
		sess, err := s.staffed(r.Context(), me(r), id(r))
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "attendance-"+sess.JoinCode+".csv"))
		return s.AttendanceCSV(r.Context(), me(r), id(r), w)
	}
	list, err := s.Attendance(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"attendance": list})
	return nil
}

// ---------- breakout groups (D-79) ----------

func groupParam(r *http.Request) (int64, error) {
	n, err := strconv.ParseInt(chi.URLParam(r, "group"), 10, 64)
	if err != nil {
		return 0, web.ErrNotFound
	}
	return n, nil
}

func noContent(w http.ResponseWriter, err error) error {
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hForm(w http.ResponseWriter, r *http.Request) error {
	var in FormInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	gs, err := s.Form(r.Context(), me(r), id(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"groups": gs})
	return nil
}

func (s *Service) hCloseGroups(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Countdown int `json:"countdown"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.CloseGroups(r.Context(), me(r), id(r), in.Countdown))
}

func (s *Service) hGroupSettings(w http.ResponseWriter, r *http.Request) error {
	var in GroupSettingsInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.GroupSettings(r.Context(), me(r), id(r), in))
}

func (s *Service) hAnnounce(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Text string `json:"text"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	m, err := s.Announce(r.Context(), me(r), id(r), in.Text)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, m)
	return nil
}

func (s *Service) hRenameGroup(w http.ResponseWriter, r *http.Request) error {
	g, err := groupParam(r)
	if err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.RenameGroup(r.Context(), me(r), id(r), g, in.Name))
}

func (s *Service) hDismissHelp(w http.ResponseWriter, r *http.Request) error {
	g, err := groupParam(r)
	if err != nil {
		return err
	}
	return noContent(w, s.DismissHelp(r.Context(), me(r), id(r), g))
}

type groupIn struct {
	GroupID int64 `json:"group_id"` // 0: none
}

func (s *Service) hMove(w http.ResponseWriter, r *http.Request) error {
	var in groupIn
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.Move(r.Context(), me(r), id(r), chi.URLParam(r, "user"), in.GroupID))
}

func (s *Service) hChoose(w http.ResponseWriter, r *http.Request) error {
	var in groupIn
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.Choose(r.Context(), me(r), id(r), in.GroupID))
}

func (s *Service) hVisit(w http.ResponseWriter, r *http.Request) error {
	var in groupIn
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.Visit(r.Context(), me(r), id(r), in.GroupID))
}

func (s *Service) hHelp(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		On bool `json:"on"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.Help(r.Context(), me(r), id(r), in.On))
}
