package tutor

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// The resource monitor (D-81, TS-FR-33, TS-FR-38, TS-FR-39). Every 2
// seconds it estimates what each live session sends and shows the teachers.
// When all of tutoring stays over the budget for more than one check (over
// 3 seconds), it switches off one thing in the busiest session, in this
// order, and tells the teachers what and why:
//
//  1. cameras and screens in breakout groups;
//  2. students sharing a camera or screen, beyond two;
//  3. the broadcaster's extra screens and cameras;
//  4. the broadcaster's camera;
//  5. the screen's frame rate, down to 5 a second.
//
// Nothing comes back by itself: once there's room again the teachers are
// told, and they turn things back on (TS-FR-39).

// The steps, as kept in a session's auto_off.
const (
	stepGroupCamera = "group_camera"
	stepGroupScreen = "group_screen"
	stepShares      = "student_shares"
	stepExtra       = "extra_tracks"
	stepCamera      = "camera"
	stepFPS         = "fps"
)

var stepText = map[string]string{
	stepGroupCamera: "Cameras in groups were turned off",
	stepGroupScreen: "Sharing screens in groups was turned off",
	stepShares:      "Only two students may share their camera or screen now",
	stepExtra:       "The extra screens and cameras were stopped",
	stepCamera:      "The broadcaster's camera was turned off",
	stepFPS:         "The screen is sent at 5 frames a second",
}

// MonitorInterval is how often the monitor measures.
const MonitorInterval = 2 * time.Second

type monitorState struct {
	mu      sync.Mutex
	over    int             // checks in a row over the budget
	offered map[string]bool // sessions told there's room again
	met     metricsState    // monitoring (D-82)
}

// Resources is what a session uses, for its teachers (TS-FR-33).
type Resources struct {
	Load    Load     `json:"load"`
	Total   float64  `json:"total_mbps"`  // all of tutoring
	Limit   float64  `json:"limit_mbps"`  // this session's share now
	Budget  float64  `json:"budget_mbps"` // the admin's budget
	HardCap float64  `json:"hard_cap_mbps"`
	Plan    Estimate `json:"plan"` // the plan's forecast
	AutoOff []string `json:"auto_off"`
}

func (s *Service) resources(sess Session, l Load, total float64, b Budget) Resources {
	if l.Items == nil {
		l.Items = []Item{}
	}
	limit := b.TotalMbps - (total - l.Mbps)
	if b.SessionMbps > 0 {
		limit = min(limit, b.SessionMbps)
	}
	off := sess.AutoOff
	if off == nil {
		off = []string{}
	}
	return Resources{Load: l, Total: round2(total), Limit: round2(max(0, limit)), Budget: b.TotalMbps, HardCap: b.HardCapMbps, Plan: sess.Plan.Forecast(), AutoOff: off}
}

// SessionResources is the Resources panel's first look; the socket keeps it current.
func (s *Service) SessionResources(ctx context.Context, p authn.Person, id string) (Resources, error) {
	sess, err := s.staffed(ctx, p, id)
	if err != nil {
		return Resources{}, err
	}
	b, err := s.budget(ctx)
	if err != nil {
		return Resources{}, err
	}
	return s.resources(sess, s.loadOf(id), s.others(""), b), nil
}

// Monitor runs CheckLoad until ctx ends.
func (s *Service) Monitor(ctx context.Context) {
	t := time.NewTicker(MonitorInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.CheckLoad(ctx); err != nil && ctx.Err() == nil {
				s.log.Warn("resource monitor", "err", err)
			}
		}
	}
}

// CheckLoad measures every live session once, tells their teachers, and
// switches something off when tutoring is over the budget.
func (s *Service) CheckLoad(ctx context.Context) error {
	s.mon.mu.Lock()
	defer s.mon.mu.Unlock()
	rows, _ := s.pool.Query(ctx, `SELECT `+sessionCols+` FROM tutoring.sessions WHERE status='live'`)
	live, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) { return scanSession(r) })
	if err != nil {
		return err
	}
	b, err := s.budget(ctx)
	if err != nil {
		return err
	}
	loads := map[string]Load{}
	var total float64
	for _, sess := range live {
		l, err := s.measure(ctx, sess)
		if err != nil {
			// The media server didn't answer: keep the last figure.
			s.log.Warn("measuring a session", "session", sess.ID, "err", err)
			l = s.loadOf(sess.ID)
		}
		loads[sess.ID] = l
		total += l.Mbps
	}
	s.observe(ctx, live, loads)
	s.loadMu.Lock()
	s.load = loads
	s.loadMu.Unlock()
	for _, sess := range live {
		s.toStaff(sess.ID, event{Type: "resources", Data: s.resources(sess, loads[sess.ID], total, b)})
	}
	if total > b.TotalMbps {
		s.mon.over++
	} else {
		s.mon.over = 0
	}
	if s.mon.over >= 2 {
		s.mon.over = 0
		// The busiest session gives something up first.
		slices.SortFunc(live, func(a, c Session) int { return cmp.Compare(loads[c.ID].Mbps, loads[a.ID].Mbps) })
		for _, sess := range live {
			done, err := s.reduce(ctx, sess, loads[sess.ID])
			if err != nil {
				return err
			}
			if done {
				break
			}
		}
		return nil
	}
	// Room again (under 80 %): the teachers choose what comes back.
	if total < 0.8*b.TotalMbps {
		for _, sess := range live {
			if len(sess.AutoOff) > 0 && !s.mon.offered[sess.ID] {
				s.mon.offered[sess.ID] = true
				s.toStaff(sess.ID, event{Type: "resources_available", Data: map[string]any{"auto_off": sess.AutoOff}})
			}
		}
	}
	for id := range s.mon.offered {
		if _, ok := loads[id]; !ok {
			delete(s.mon.offered, id)
		}
	}
	return nil
}

// reduce switches off the next step in sess, if anything is left to.
func (s *Service) reduce(ctx context.Context, sess Session, l Load) (bool, error) {
	var steps []string
	switch {
	case sess.GroupsOpen && (sess.GroupCamera || sess.GroupScreen):
		if sess.GroupCamera {
			steps = append(steps, stepGroupCamera)
		}
		if sess.GroupScreen {
			steps = append(steps, stepGroupScreen)
		}
		if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET group_camera=false, group_screen=false WHERE id=$1`, sess.ID); err != nil {
			return false, err
		}
		sess.GroupCamera, sess.GroupScreen = false, false
		if err := s.applyGroups(ctx, sess); err != nil {
			return false, err
		}
	default:
		// Students beyond the first two who were allowed keep their microphone only.
		rows, _ := s.pool.Query(ctx, `UPDATE tutoring.participants SET allow_camera=false, allow_screen=false
			WHERE session_id=$1 AND user_id IN (SELECT user_id FROM tutoring.participants WHERE session_id=$1 AND role='student' AND (allow_camera OR allow_screen)
				ORDER BY first_at, user_id OFFSET 2)
			RETURNING `+participantCols, sess.ID)
		cut, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Participant, error) { return scanParticipant(r) })
		if err != nil {
			return false, err
		}
		switch {
		case len(cut) > 0:
			steps = append(steps, stepShares)
			for _, p := range cut {
				s.applyMedia(ctx, sess, p)
				s.hub.send(sess.ID, p.UserID, event{Type: "auto_disabled", Data: map[string]string{"step": stepShares, "message": "Your teacher's session ran short of bandwidth: your camera and screen were turned off."}})
			}
		case len(l.extra) > 0:
			steps = append(steps, stepExtra)
			for _, sid := range l.extra {
				if err := s.media.MuteTrack(ctx, sess.ID, l.broadcaster, sid); err != nil && !errors.Is(err, media.ErrNotInRoom) {
					return false, err
				}
			}
		case l.Cameras > 0:
			steps = append(steps, stepCamera)
			if err := s.media.Mute(ctx, sess.ID, l.broadcaster, media.Camera); err != nil && !errors.Is(err, media.ErrNotInRoom) {
				return false, err
			}
		case l.Screens > 0 && !sess.off(stepFPS):
			steps = append(steps, stepFPS)
		default:
			return false, nil
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET auto_off = ARRAY(SELECT DISTINCT unnest(auto_off || $2::text[]) ORDER BY 1) WHERE id=$1`, sess.ID, steps); err != nil {
		return false, err
	}
	details, _ := json.Marshal(map[string]any{"steps": steps, "mbps": l.Mbps})
	if _, err := s.pool.Exec(ctx, `INSERT INTO tutoring.log (session_id, action, details) VALUES ($1, 'auto_disabled', $2)`, sess.ID, details); err != nil {
		s.log.Warn("tutoring log", "err", err)
	}
	s.mon.offered[sess.ID] = false
	for _, st := range steps {
		e := event{Type: "auto_disabled", Data: map[string]string{"step": st, "message": stepText[st] + " to keep the session within the server's bandwidth."}}
		if st == stepExtra || st == stepCamera || st == stepFPS {
			s.toStaff(sess.ID, e, l.broadcaster) // their browser stops sending it
		} else {
			s.toStaff(sess.ID, e)
		}
	}
	s.hub.changed(sess.ID)
	return true, nil
}

// applyGroups re-applies what students may publish in their groups.
func (s *Service) applyGroups(ctx context.Context, sess Session) error {
	if !sess.GroupsOpen {
		return nil
	}
	rows, _ := s.pool.Query(ctx, `SELECT `+participantCols+` FROM tutoring.participants WHERE session_id=$1 AND role='student' AND state='admitted'`, sess.ID)
	ps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Participant, error) { return scanParticipant(r) })
	if err != nil {
		return err
	}
	for _, sp := range ps {
		s.applyGroup(ctx, sess, sp)
	}
	return nil
}

// RestoreInput turns back on something the service switched off.
type RestoreInput struct {
	Step  string `json:"step"`
	Force bool   `json:"force"` // over the budget, up to the hard cap
}

// Restore turns a step back on, when it fits (TS-FR-39).
func (s *Service) Restore(ctx context.Context, p authn.Person, id string, in RestoreInput) error {
	sess, err := s.staffed(ctx, p, id)
	if err != nil {
		return err
	}
	if !sess.off(in.Step) {
		return web.Invalid(map[string]string{"step": "that is on already"})
	}
	l := s.loadOf(id)
	v := float64(max(l.Viewers, 1))
	var add float64
	switch in.Step {
	case stepGroupCamera:
		if add, err = s.groupVideoCost(ctx, id); err != nil {
			return err
		}
	case stepGroupScreen:
		var groups int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tutoring.groups WHERE session_id=$1`, id).Scan(&groups); err != nil {
			return err
		}
		add = float64(groups) * rateScreen * 4
	case stepCamera:
		add = rateCamera * v
	case stepFPS:
		add = rateScreen * (1 - slowScreen) * v
	}
	if err := s.checkAdd(ctx, id, "turning it back on", add, in.Force, "wait until there's more room"); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET auto_off = array_remove(auto_off, $2),
		group_camera = group_camera OR $2 = 'group_camera', group_screen = group_screen OR $2 = 'group_screen' WHERE id=$1`, id, in.Step); err != nil {
		return err
	}
	s.logAction(ctx, id, p.ID, "auto_restored", nil, in)
	switch in.Step {
	case stepGroupCamera, stepGroupScreen:
		if sess, err = s.session(ctx, id); err != nil {
			return err
		}
		if err := s.applyGroups(ctx, sess); err != nil {
			return err
		}
	case stepExtra, stepCamera, stepFPS:
		b := sess.TeacherID
		if sess.BroadcasterID != nil {
			b = *sess.BroadcasterID
		}
		s.hub.send(id, b, event{Type: "restored", Data: map[string]string{"step": in.Step}})
	}
	s.hub.changed(id)
	return nil
}

// ---------- the admin's view (TS-FR-31, TS-FR-33) ----------

// LiveUse is one live session's use, for the admin.
type LiveUse struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	TeacherName string   `json:"teacher_name"`
	Mbps        float64  `json:"mbps"`
	Viewers     int      `json:"viewers"`
	AutoOff     []string `json:"auto_off"`
	// For monitoring (D-82); CPU is the measured CPU shared out by upload.
	DownMbps     float64  `json:"down_mbps"`
	Tracks       int      `json:"tracks"`
	Participants int      `json:"participants"`
	Reconnects   int      `json:"reconnects"`
	CPU          *float64 `json:"cpu,omitempty"`
}

// BudgetOverview is the budget and what each live session uses now.
type BudgetOverview struct {
	Budget   Budget    `json:"budget"`
	Total    float64   `json:"total_mbps"`
	Sessions []LiveUse `json:"sessions"`
}

func (s *Service) AdminBudget(ctx context.Context, p authn.Person) (BudgetOverview, error) {
	if err := requireAdmin(p); err != nil {
		return BudgetOverview{}, err
	}
	b, err := s.budget(ctx)
	if err != nil {
		return BudgetOverview{}, err
	}
	sessions, err := s.liveUses(ctx)
	if err != nil {
		return BudgetOverview{}, err
	}
	out := BudgetOverview{Budget: b, Sessions: sessions}
	for _, u := range sessions {
		out.Total += u.Mbps
	}
	out.Total = round2(out.Total)
	return out, nil
}
