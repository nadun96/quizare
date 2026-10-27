package tutor

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
			r.Method("POST", "/start", h(s.hStart))
			r.Method("POST", "/end", h(s.simple(s.End)))
			r.Method("PUT", "/settings", h(s.hSettings))
			r.Method("POST", "/admit", h(s.hAdmit))
			r.Method("PUT", "/permissions", h(s.hPermissions))
			r.Method("POST", "/mute", h(s.hMute))
			r.Method("PUT", "/hand", h(s.hHand))
			r.Method("POST", "/react", h(s.hReact))
			r.Method("GET", "/reactions", h(s.hReactions))
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
			// Recording the presenter (D-80).
			r.Method("GET", "/recordings", h(s.hSessionRecordings))
			r.Method("POST", "/recordings", h(s.hStartRecording))
			r.Method("PUT", "/recordings/{rec}/chunks/{n}", h(s.hChunk))
			r.Method("POST", "/recordings/{rec}/pause", h(s.hPauseRecording))
			r.Method("POST", "/recordings/{rec}/stop", h(s.hStopRecording))
			r.Method("PUT", "/recordings/{rec}/viewers", h(s.hRecordingViewers))
			// The bandwidth budget (D-81).
			r.Method("GET", "/plan", h(s.hPlan))
			r.Method("PUT", "/plan", h(s.hSetPlan))
			r.Method("GET", "/resources", h(s.hResources))
			r.Method("POST", "/budget/track", h(s.hCheckTrack))
			r.Method("POST", "/budget/restore", h(s.hRestore))
			r.Method("POST", "/media-reconnected", h(s.simple(s.MediaReconnected)))
			// Scheduling (D-84).
			r.Method("PUT", "/timing", h(s.hSetTiming))
			r.Method("POST", "/cancel", h(s.hCancel))
			r.Method("GET", "/calendar.ics", h(s.hSessionICS))
			// The whiteboard (D-85).
			r.Method("GET", "/board", h(s.hBoard))
			r.Method("POST", "/board/strokes", h(s.hAddStrokes))
			r.Method("POST", "/board/erase", h(s.hErase))
			r.Method("POST", "/board/clear", h(s.hClearBoard))
			r.Method("PUT", "/board/access", h(s.hBoardAccess))
			// A poll in the session (D-86).
			r.Method("PUT", "/poll", h(s.hShowPoll))
		})
		// Attendance reports (D-87).
		r.Method("GET", "/classrooms/{classroom}/attendance", h(s.hClassroomAttendance))
		r.Method("PUT", "/classrooms/{classroom}/attendance/settings", h(s.hAttendanceSettings))
		r.Method("GET", "/my/attendance", h(s.hMyAttendance))
		r.Method("POST", "/schedule", h(s.hSchedule))
		r.Method("GET", "/calendar", h(s.hCalendar))
		r.Method("GET", "/my/feed", h(s.hFeed))
		r.Method("POST", "/my/feed", h(s.hNewFeed))
		r.Method("DELETE", "/my/feed", h(s.hRevokeFeed))
		r.Method("POST", "/plans/estimate", h(s.hEstimatePlan))
		r.Method("GET", "/plan-presets", h(s.hPresets))
		r.Method("POST", "/plan-presets", h(s.hSavePreset))
		r.Method("DELETE", "/plan-presets/{preset}", h(s.hDeletePreset))
		r.Method("GET", "/admin/budget", h(s.hAdminBudget))
		r.Method("PUT", "/admin/budget", h(s.hSetBudget))
		r.Method("GET", "/admin/monitor", h(s.hMonitor))
		r.Method("GET", "/admin/live", h(s.hAdminLive))
		r.Method("POST", "/admin/sessions/{id}/end", h(s.hAdminEnd))
		r.Method("GET", "/my/recordings", h(s.hMyRecordings))
		r.Method("POST", "/recordings/{rec}/link", h(s.hRecordingLink))
		r.Method("DELETE", "/recordings/{rec}", h(s.hDeleteRecording))
		r.Method("GET", "/admin/recordings", h(s.hAdminRecordings))
		r.Method("PUT", "/admin/recordings/limit", h(s.hRecordingLimit))
		r.Method("DELETE", "/admin/recordings/{rec}", h(s.hAdminDeleteRecording))
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

// ---------- recording (D-80) ----------

func rec(r *http.Request) string { return chi.URLParam(r, "rec") }

func (s *Service) hSessionRecordings(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Recordings(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"recordings": list})
	return nil
}

func (s *Service) hMyRecordings(w http.ResponseWriter, r *http.Request) error {
	list, err := s.MyRecordings(r.Context(), me(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"recordings": list})
	return nil
}

func (s *Service) hStartRecording(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Mime string `json:"mime"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	rc, err := s.StartRecording(r.Context(), me(r), id(r), in.Mime)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, rc)
	return nil
}

// hChunk takes one part of the recording as the raw body.
func (s *Service) hChunk(w http.ResponseWriter, r *http.Request) error {
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil || n < 0 {
		return web.ErrNotFound
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChunk+1024)
	return noContent(w, s.Chunk(r.Context(), me(r), id(r), rec(r), n, r.Body))
}

func (s *Service) hPauseRecording(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Paused bool `json:"paused"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.PauseRecording(r.Context(), me(r), id(r), rec(r), in.Paused))
}

func (s *Service) hStopRecording(w http.ResponseWriter, r *http.Request) error {
	rc, err := s.StopRecording(r.Context(), me(r), id(r), rec(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, rc)
	return nil
}

func (s *Service) hRecordingViewers(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Viewers string `json:"viewers"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.SetViewers(r.Context(), me(r), id(r), rec(r), in.Viewers))
}

func (s *Service) hRecordingLink(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Download bool `json:"download"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	u, err := s.PlayLink(r.Context(), me(r), rec(r), in.Download)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]string{"url": u})
	return nil
}

func (s *Service) hDeleteRecording(w http.ResponseWriter, r *http.Request) error {
	return noContent(w, s.DeleteRecording(r.Context(), me(r), rec(r)))
}

var recordingSorts = map[string]string{"started": "r.started_at"}

func (s *Service) hAdminRecordings(w http.ResponseWriter, r *http.Request) error {
	pg := ParsePage(r, recordingSorts, "started", true)
	list, total, space, err := s.AdminRecordings(r.Context(), me(r), pg)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"recordings": list, "total": total, "page": pg.Page, "size": pg.Size, "space": space})
	return nil
}

func (s *Service) hRecordingLimit(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Limit int64 `json:"limit"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	sp, err := s.SetRecordingLimit(r.Context(), me(r), in.Limit)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, sp)
	return nil
}

func (s *Service) hAdminDeleteRecording(w http.ResponseWriter, r *http.Request) error {
	return noContent(w, s.AdminDeleteRecording(r.Context(), me(r), rec(r)))
}

// ---------- the bandwidth budget (D-81) ----------

// hStart takes an optional {"force": true}: start over the budget.
func (s *Service) hStart(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Force bool `json:"force"`
	}
	if r.ContentLength != 0 {
		if err := web.Decode(w, r, &in); err != nil {
			return err
		}
	}
	return noContent(w, s.Start(r.Context(), me(r), id(r), in.Force))
}

func (s *Service) hPlan(w http.ResponseWriter, r *http.Request) error {
	v, err := s.PlanFor(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) hSetPlan(w http.ResponseWriter, r *http.Request) error {
	var in Plan
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	v, err := s.SetPlan(r.Context(), me(r), id(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) hEstimatePlan(w http.ResponseWriter, r *http.Request) error {
	var in Plan
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	v, err := s.EstimatePlan(r.Context(), me(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) hResources(w http.ResponseWriter, r *http.Request) error {
	v, err := s.SessionResources(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) hCheckTrack(w http.ResponseWriter, r *http.Request) error {
	var in TrackCheck
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	e, err := s.CheckTrack(r.Context(), me(r), id(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, e)
	return nil
}

func (s *Service) hRestore(w http.ResponseWriter, r *http.Request) error {
	var in RestoreInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.Restore(r.Context(), me(r), id(r), in))
}

func (s *Service) hPresets(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Presets(r.Context(), me(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"presets": list})
	return nil
}

func (s *Service) hSavePreset(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name string `json:"name"`
		Plan Plan   `json:"plan"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	pr, err := s.SavePreset(r.Context(), me(r), in.Name, in.Plan)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, pr)
	return nil
}

func (s *Service) hDeletePreset(w http.ResponseWriter, r *http.Request) error {
	return noContent(w, s.DeletePreset(r.Context(), me(r), chi.URLParam(r, "preset")))
}

func (s *Service) hAdminBudget(w http.ResponseWriter, r *http.Request) error {
	v, err := s.AdminBudget(r.Context(), me(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) hMonitor(w http.ResponseWriter, r *http.Request) error {
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "24h"
	}
	v, err := s.Monitoring(r.Context(), me(r), rng)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) hSetBudget(w http.ResponseWriter, r *http.Request) error {
	var in Budget
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	b, err := s.SetBudget(r.Context(), me(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, b)
	return nil
}

// ---------- scheduling (D-84) ----------

func (s *Service) hSchedule(w http.ResponseWriter, r *http.Request) error {
	var in ScheduleInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	out, err := s.Schedule(r.Context(), me(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) hSetTiming(w http.ResponseWriter, r *http.Request) error {
	var in TimingInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	sess, err := s.SetTiming(r.Context(), me(r), id(r), in)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, sess)
	return nil
}

func (s *Service) hCancel(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Series bool `json:"series"`
	}
	if r.ContentLength != 0 {
		if err := web.Decode(w, r, &in); err != nil {
			return err
		}
	}
	n, err := s.Cancel(r.Context(), me(r), id(r), in.Series)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]int{"cancelled": n})
	return nil
}

func (s *Service) hCalendar(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	from, err1 := time.Parse(time.RFC3339, q.Get("from"))
	to, err2 := time.Parse(time.RFC3339, q.Get("to"))
	if err1 != nil || err2 != nil {
		return web.Invalid(map[string]string{"from": "from and to, as RFC 3339 times"})
	}
	list, err := s.Calendar(r.Context(), me(r), from, to)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"sessions": list})
	return nil
}

func (s *Service) hSessionICS(w http.ResponseWriter, r *http.Request) error {
	body, code, err := s.SessionICS(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "tutoring-"+code+".ics"))
	_, err = w.Write([]byte(body))
	return err
}

func (s *Service) hFeed(w http.ResponseWriter, r *http.Request) error {
	at, err := s.Feed(r.Context(), me(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"active": at != nil, "created_at": at})
	return nil
}

func (s *Service) hNewFeed(w http.ResponseWriter, r *http.Request) error {
	token, err := s.NewFeed(r.Context(), me(r))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	web.JSON(w, http.StatusCreated, map[string]string{"path": "/feeds/" + token + ".ics"})
	return nil
}

func (s *Service) hRevokeFeed(w http.ResponseWriter, r *http.Request) error {
	return noContent(w, s.RevokeFeed(r.Context(), me(r)))
}

// ---------- the whiteboard (D-85) ----------

func (s *Service) hBoard(w http.ResponseWriter, r *http.Request) error {
	v, err := s.Board(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) hAddStrokes(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Strokes []Stroke `json:"strokes"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	out, err := s.AddStrokes(r.Context(), me(r), id(r), in.Strokes)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"strokes": out})
	return nil
}

func (s *Service) hErase(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		IDs     []int64 `json:"ids"`
		Gesture string  `json:"gesture"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	removed, err := s.EraseStrokes(r.Context(), me(r), id(r), in.IDs, in.Gesture)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"removed": removed})
	return nil
}

func (s *Service) hClearBoard(w http.ResponseWriter, r *http.Request) error {
	upto, err := s.ClearBoard(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]int64{"upto": upto})
	return nil
}

func (s *Service) hBoardAccess(w http.ResponseWriter, r *http.Request) error {
	var in BoardAccessInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.SetBoardAccess(r.Context(), me(r), id(r), in))
}

func (s *Service) hShowPoll(w http.ResponseWriter, r *http.Request) error {
	var in PollInput
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.ShowPoll(r.Context(), me(r), id(r), in))
}

// ---------- attendance reports (D-87) ----------

func (s *Service) hClassroomAttendance(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	from, err1 := time.Parse(time.RFC3339, q.Get("from"))
	to, err2 := time.Parse(time.RFC3339, q.Get("to"))
	if err1 != nil || err2 != nil {
		return web.Invalid(map[string]string{"from": "from and to, as RFC 3339 times"})
	}
	rep, err := s.ClassroomAttendance(r.Context(), me(r), chi.URLParam(r, "classroom"), from, to)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, rep)
	return nil
}

func (s *Service) hAttendanceSettings(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		CountsInResults bool `json:"counts_in_results"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	return noContent(w, s.SetCountsInResults(r.Context(), me(r), chi.URLParam(r, "classroom"), in.CountsInResults))
}

func (s *Service) hMyAttendance(w http.ResponseWriter, r *http.Request) error {
	list, err := s.MyAttendance(r.Context(), me(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"classrooms": list})
	return nil
}
