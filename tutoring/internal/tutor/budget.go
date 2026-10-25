package tutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// The bandwidth budget (D-81, TS-FR-08, TS-FR-30 to TS-FR-32, PO-2, PO-9).
// Live video costs the server upload: each track is sent once to every
// person who receives it. A session's use is estimated from what it sends,
// at the typical rates the requirements give per receiver; LiveKit doesn't
// report bandwidth per room, so the figures say "estimated". Teachers plan a
// session against what's free, and changes are checked against it too.

// Typical Mbit/s per receiver ("Capacity: what 200 students needs").
const (
	rateScreen       = 1.2  // a readable screen, 1080p at a low frame rate
	rateScreenSmooth = 2.5  // a screen at a higher frame rate (small classes)
	rateCamera       = 0.4  // a small camera
	rateExtra        = 0.2  // a second screen or camera, as a thumbnail
	rateAudio        = 0.04 // a voice
	// At 5 frames a second (TS-FR-38, step 5) a screen needs about half.
	slowScreen = 0.5
)

// Budget is what the admin allows tutoring (TS-FR-31).
type Budget struct {
	TotalMbps        float64 `json:"total_mbps"`         // all sessions together (PO-9: 240 of a 300 Mbit/s port)
	HardCapMbps      float64 `json:"hard_cap_mbps"`      // nothing goes above this, even when a teacher insists
	SessionMbps      float64 `json:"session_mbps"`       // one session's share; 0: any of what's free
	MaxTeacherTracks int     `json:"max_teacher_tracks"` // screens and cameras the broadcaster sends (TS-NFR-02)
	MaxStudentCams   int     `json:"max_student_cameras"`
	MaxStudentScreen int     `json:"max_student_screens"`
}

// DefaultBudget is the requirements' (PO-9, TS-FR-25, TS-NFR-02).
var DefaultBudget = Budget{TotalMbps: 240, HardCapMbps: 270, MaxTeacherTracks: 4, MaxStudentCams: 2, MaxStudentScreen: 2}

func (b Budget) check() error {
	f := map[string]string{}
	if b.TotalMbps < 1 || b.TotalMbps > 100000 {
		f["total_mbps"] = "1 to 100,000 Mbit/s"
	}
	if b.HardCapMbps < b.TotalMbps || b.HardCapMbps > 100000 {
		f["hard_cap_mbps"] = "at least the budget"
	}
	if b.SessionMbps < 0 || b.SessionMbps > b.TotalMbps {
		f["session_mbps"] = "0 (no share of its own) up to the budget"
	}
	if b.MaxTeacherTracks < 1 || b.MaxTeacherTracks > 8 {
		f["max_teacher_tracks"] = "1 to 8"
	}
	// At least two students share at once (TS-FR-25).
	if b.MaxStudentCams < 2 || b.MaxStudentCams > 50 {
		f["max_student_cameras"] = "2 to 50"
	}
	if b.MaxStudentScreen < 2 || b.MaxStudentScreen > 50 {
		f["max_student_screens"] = "2 to 50"
	}
	if len(f) > 0 {
		return web.Invalid(f)
	}
	return nil
}

// Plan is what a session will use (TS-FR-08).
type Plan struct {
	Students       int    `json:"students"`        // expected
	Screens        int    `json:"screens"`         // the broadcaster's
	Cameras        int    `json:"cameras"`         // the broadcaster's
	Quality        string `json:"quality"`         // readable or smooth
	StudentMics    int    `json:"student_mics"`    // speaking at once, to the teachers
	StudentCameras int    `json:"student_cameras"` // at once, to the teachers
	StudentScreens int    `json:"student_screens"` // at once, to the teachers
	GroupVideo     bool   `json:"group_video"`     // cameras in breakout groups
}

// DefaultPlan is a lecture: one screen and a camera, 30 students, two may share.
var DefaultPlan = Plan{Students: 30, Screens: 1, Cameras: 1, Quality: "readable", StudentMics: 2, StudentCameras: 2, StudentScreens: 2}

func (p *Plan) clean(b Budget) error {
	f := map[string]string{}
	if p.Students < 1 || p.Students > 500 {
		f["students"] = "1 to 500 students"
	}
	if p.Screens < 0 || p.Cameras < 0 || p.Screens+p.Cameras > b.MaxTeacherTracks {
		f["screens"] = fmt.Sprintf("screens and cameras together: at most %d", b.MaxTeacherTracks)
	}
	if p.Quality == "" {
		p.Quality = "readable"
	}
	if p.Quality != "readable" && p.Quality != "smooth" {
		f["quality"] = "readable or smooth"
	}
	if p.StudentMics < 0 || p.StudentMics > 50 {
		f["student_mics"] = "0 to 50"
	}
	if p.StudentCameras < 0 || p.StudentCameras > b.MaxStudentCams {
		f["student_cameras"] = fmt.Sprintf("0 to %d", b.MaxStudentCams)
	}
	if p.StudentScreens < 0 || p.StudentScreens > b.MaxStudentScreen {
		f["student_screens"] = fmt.Sprintf("0 to %d", b.MaxStudentScreen)
	}
	if len(f) > 0 {
		return web.Invalid(f)
	}
	return nil
}

// Item is one part of an estimate.
type Item struct {
	Name string  `json:"name"`
	Mbps float64 `json:"mbps"`
}

// Estimate is upload in Mbit/s, part by part.
type Estimate struct {
	Mbps  float64 `json:"mbps"`
	Items []Item  `json:"items"`
}

func (e *Estimate) add(name string, mbps float64) {
	if mbps <= 0 {
		return
	}
	e.Items = append(e.Items, Item{Name: name, Mbps: round2(mbps)})
	e.Mbps = round2(e.Mbps + mbps)
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

// staffShare: what students share goes to the teachers only; plan for two.
const staffShare = 2

// Forecast is a plan's upload when everyone expected is there.
func (p Plan) Forecast() Estimate {
	e := Estimate{Items: []Item{}}
	v := float64(p.Students)
	screen := rateScreen
	if p.Quality == "smooth" {
		screen = rateScreenSmooth
	}
	if p.Screens > 0 {
		e.add("The main screen, to every student", screen*v)
		e.add(fmt.Sprintf("%d more screen(s), as thumbnails", p.Screens-1), float64(p.Screens-1)*rateExtra*v)
	}
	if p.Cameras > 0 {
		e.add("The camera, to every student", rateCamera*v)
		e.add(fmt.Sprintf("%d more camera(s), as thumbnails", p.Cameras-1), float64(p.Cameras-1)*rateExtra*v)
	}
	e.add("The teacher's voice", rateAudio*v)
	e.add("Students' cameras and screens, to the teachers", (float64(p.StudentCameras)*rateCamera+float64(p.StudentScreens)*rateScreen)*staffShare)
	e.add("Students' microphones, to the teachers", float64(p.StudentMics)*rateAudio*staffShare)
	if p.GroupVideo {
		// Groups of about five: each camera reaches four others.
		e.add("Cameras in groups", v*rateCamera*4)
	}
	return e
}

// ---------- the admin's budget ----------

func (s *Service) budget(ctx context.Context) (Budget, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM tutoring.settings WHERE key='budget'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultBudget, nil
	}
	if err != nil {
		return Budget{}, err
	}
	b := DefaultBudget
	err = json.Unmarshal(raw, &b)
	return b, err
}

// SetBudget changes it, at once, without a restart (TS-FR-31).
func (s *Service) SetBudget(ctx context.Context, p authn.Person, b Budget) (Budget, error) {
	if err := requireAdmin(p); err != nil {
		return Budget{}, err
	}
	if err := b.check(); err != nil {
		return Budget{}, err
	}
	raw, _ := json.Marshal(b)
	if _, err := s.pool.Exec(ctx, `INSERT INTO tutoring.settings (key, value) VALUES ('budget', $1) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, raw); err != nil {
		return Budget{}, err
	}
	return b, nil
}

// ---------- plans ----------

// PlanView is a plan, what it would use, and what's free for it.
type PlanView struct {
	Plan     Plan     `json:"plan"`
	Forecast Estimate `json:"forecast"`
	// Free is the budget less what the other live sessions use now;
	// HardFree the same against the hard cap. Limit is this session's share.
	Free     float64 `json:"free_mbps"`
	HardFree float64 `json:"hard_free_mbps"`
	Limit    float64 `json:"limit_mbps"`
	Budget   Budget  `json:"budget"`
	Fits     bool    `json:"fits"`
	// Advice says what to drop or lower when it doesn't fit.
	Advice []string `json:"advice,omitempty"`
}

// others is what the live sessions other than id use now (estimated).
func (s *Service) others(id string) float64 {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	var total float64
	for sid, l := range s.load {
		if sid != id {
			total += l.Mbps
		}
	}
	return total
}

// limits are a session's share of the budget and of the hard cap now.
func (s *Service) limits(ctx context.Context, sessionID string) (b Budget, limit, hard float64, err error) {
	if b, err = s.budget(ctx); err != nil {
		return b, 0, 0, err
	}
	other := s.others(sessionID)
	limit, hard = math.Max(0, b.TotalMbps-other), math.Max(0, b.HardCapMbps-other)
	if b.SessionMbps > 0 {
		limit = math.Min(limit, b.SessionMbps)
	}
	return b, round2(limit), round2(hard), nil
}

func (s *Service) planView(ctx context.Context, sess Session, p Plan) (PlanView, error) {
	b, limit, hard, err := s.limits(ctx, sess.ID)
	if err != nil {
		return PlanView{}, err
	}
	v := PlanView{Plan: p, Forecast: p.Forecast(), Budget: b, Free: round2(math.Max(0, b.TotalMbps-s.others(sess.ID))), HardFree: hard, Limit: limit}
	v.Fits = v.Forecast.Mbps <= v.Limit
	if !v.Fits {
		v.Advice = advise(p, v.Limit)
	}
	return v, nil
}

// advise suggests changes that bring a plan within limit, cheapest loss first.
func advise(p Plan, limit float64) []string {
	var out []string
	try := func(text string, change func(*Plan)) {
		q := p
		change(&q)
		if q != p && q.Forecast().Mbps <= limit {
			out = append(out, text)
		}
	}
	try("turn off cameras in groups", func(q *Plan) { q.GroupVideo = false })
	try("use readable quality for the screen", func(q *Plan) { q.Quality = "readable" })
	try("send one screen only", func(q *Plan) { q.Screens = min(q.Screens, 1) })
	try("send no camera", func(q *Plan) { q.Cameras = 0 })
	if len(out) == 0 {
		try("send one screen and no camera, at readable quality, without cameras in groups", func(q *Plan) {
			q.Screens, q.Cameras, q.Quality, q.GroupVideo = min(q.Screens, 1), 0, "readable", false
		})
	}
	if len(out) == 0 {
		// Fewer students: as many as fit with one readable screen and a voice.
		per := rateScreen + rateAudio
		fixed := (float64(p.StudentCameras)*rateCamera + float64(p.StudentScreens)*rateScreen + float64(p.StudentMics)*rateAudio) * staffShare
		out = append(out, fmt.Sprintf("plan for at most %d students", max(0, int((limit-fixed)/per))))
	}
	return out
}

// PlanFor returns a session's plan and what it would use.
func (s *Service) PlanFor(ctx context.Context, p authn.Person, id string) (PlanView, error) {
	sess, err := s.staffed(ctx, p, id)
	if err != nil {
		return PlanView{}, err
	}
	return s.planView(ctx, sess, sess.Plan)
}

// EstimatePlan says what a plan would use, before a session has it.
func (s *Service) EstimatePlan(ctx context.Context, p authn.Person, pl Plan) (PlanView, error) {
	if p.Role != "teacher" && p.Role != "admin" {
		return PlanView{}, web.ErrForbidden
	}
	b, err := s.budget(ctx)
	if err != nil {
		return PlanView{}, err
	}
	if err := pl.clean(b); err != nil {
		return PlanView{}, err
	}
	return s.planView(ctx, Session{}, pl)
}

// SetPlan changes a session's plan (the lead teacher's), before or during it.
func (s *Service) SetPlan(ctx context.Context, p authn.Person, id string, pl Plan) (PlanView, error) {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return PlanView{}, err
	}
	if sess.Status == "ended" {
		return PlanView{}, errEnded
	}
	b, err := s.budget(ctx)
	if err != nil {
		return PlanView{}, err
	}
	if err := pl.clean(b); err != nil {
		return PlanView{}, err
	}
	raw, _ := json.Marshal(pl)
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET plan=$2 WHERE id=$1`, id, raw); err != nil {
		return PlanView{}, err
	}
	s.logAction(ctx, id, p.ID, "plan_changed", nil, pl)
	s.hub.changed(id)
	return s.planView(ctx, sess, pl)
}

// Preset is a saved plan.
type Preset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Plan Plan   `json:"plan"`
}

func (s *Service) Presets(ctx context.Context, p authn.Person) ([]Preset, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, name, plan FROM tutoring.plan_presets WHERE teacher_id=$1 ORDER BY lower(name)`, p.ID)
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Preset, error) {
		var pr Preset
		var raw []byte
		err := r.Scan(&pr.ID, &pr.Name, &raw)
		if err == nil {
			pr.Plan = DefaultPlan
			err = json.Unmarshal(raw, &pr.Plan)
		}
		return pr, err
	})
	if out == nil {
		out = []Preset{}
	}
	return out, err
}

// SavePreset saves a plan under a name, replacing one of the same name.
func (s *Service) SavePreset(ctx context.Context, p authn.Person, name string, pl Plan) (Preset, error) {
	if p.Role != "teacher" && p.Role != "admin" {
		return Preset{}, web.ErrForbidden
	}
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 1 || n > 60 {
		return Preset{}, web.Invalid(map[string]string{"name": "1 to 60 characters"})
	}
	b, err := s.budget(ctx)
	if err != nil {
		return Preset{}, err
	}
	if err := pl.clean(b); err != nil {
		return Preset{}, err
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tutoring.plan_presets WHERE teacher_id=$1 AND name<>$2`, p.ID, name).Scan(&count); err != nil {
		return Preset{}, err
	}
	if count >= 30 {
		return Preset{}, web.Conflict("you have 30 saved plans: delete one first")
	}
	raw, _ := json.Marshal(pl)
	pr := Preset{Name: name, Plan: pl}
	err = s.pool.QueryRow(ctx, `INSERT INTO tutoring.plan_presets (teacher_id, name, plan) VALUES ($1,$2,$3)
		ON CONFLICT (teacher_id, name) DO UPDATE SET plan=EXCLUDED.plan RETURNING id`, p.ID, name, raw).Scan(&pr.ID)
	return pr, err
}

func (s *Service) DeletePreset(ctx context.Context, p authn.Person, presetID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tutoring.plan_presets WHERE id=$1 AND teacher_id=$2`, presetID, p.ID)
	if err == nil && tag.RowsAffected() == 0 {
		return web.ErrNotFound
	}
	return err
}

// ---------- admission control (TS-FR-32) ----------

// overBudget is a refusal with what to do instead.
func overBudget(msg string, advice ...string) error {
	if len(advice) > 0 {
		msg += ": " + strings.Join(advice, ", or ")
	}
	return web.NewError(http.StatusConflict, "over_budget", msg)
}

// checkStudentShares refuses letting more students share their camera or
// screen at once than the plan and the admin's caps allow; two may always
// (TS-FR-25), and only two while the service has cut it back (TS-FR-38).
func (s *Service) checkStudentShares(ctx context.Context, sess Session, in PermissionsInput) error {
	b, err := s.budget(ctx)
	if err != nil {
		return err
	}
	type share struct {
		col, name string
		on        *bool
		limit     int
	}
	for _, sh := range []share{
		{"allow_camera", "cameras", in.Camera, max(2, min(sess.Plan.StudentCameras, b.MaxStudentCams))},
		{"allow_screen", "screens", in.Screen, max(2, min(sess.Plan.StudentScreens, b.MaxStudentScreen))},
	} {
		if sh.on == nil || !*sh.on {
			continue
		}
		if sess.off(stepShares) {
			sh.limit = 2
		}
		var after int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tutoring.participants WHERE session_id=$1 AND role='student' AND state IN ('admitted', 'waiting')
			AND (`+sh.col+` OR $3 OR user_id = ANY($2::uuid[]))`, sess.ID, in.UserIDs, in.All).Scan(&after); err != nil {
			return err
		}
		if after > sh.limit {
			return overBudget(fmt.Sprintf("this session lets %d students share their %s at once", sh.limit, sh.name),
				"take it from someone first", "raise it in the session's plan")
		}
	}
	return nil
}

// TrackCheck asks before the broadcaster adds a screen or camera (TS-FR-32).
type TrackCheck struct {
	Kind  string `json:"kind"`  // screen or camera
	Force bool   `json:"force"` // over the budget, up to the hard cap
}

// CheckTrack says whether one more of the broadcaster's tracks fits.
func (s *Service) CheckTrack(ctx context.Context, p authn.Person, id string, in TrackCheck) (Estimate, error) {
	sess, err := s.session(ctx, id)
	if err != nil {
		return Estimate{}, err
	}
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return Estimate{}, err
	}
	if me.State != "admitted" || !broadcasting(sess, me) {
		return Estimate{}, web.ErrForbidden
	}
	if in.Kind != "screen" && in.Kind != "camera" {
		return Estimate{}, web.Invalid(map[string]string{"kind": "screen or camera"})
	}
	b, limit, hard, err := s.limits(ctx, id)
	if err != nil {
		return Estimate{}, err
	}
	cur := s.loadOf(id)
	if cur.Screens+cur.Cameras >= b.MaxTeacherTracks {
		return Estimate{}, overBudget(fmt.Sprintf("the broadcaster sends at most %d screens and cameras", b.MaxTeacherTracks), "stop one first")
	}
	first := (in.Kind == "screen" && cur.Screens == 0) || (in.Kind == "camera" && cur.Cameras == 0)
	switch {
	case !first && sess.off(stepExtra):
		return Estimate{}, overBudget("extra screens and cameras were stopped to stay within the server's budget", "turn them back on under Resources")
	case in.Kind == "camera" && sess.off(stepCamera):
		return Estimate{}, overBudget("the camera was turned off to stay within the server's budget", "turn it back on under Resources")
	}
	viewers := float64(max(cur.Viewers, 1))
	add := rateExtra * viewers
	switch {
	case first && in.Kind == "screen":
		add = rateScreen * viewers
	case first:
		add = rateCamera * viewers
	}
	e := Estimate{Items: []Item{}}
	e.add("This "+in.Kind, add)
	switch after := cur.Mbps + add; {
	case after <= limit:
		return e, nil
	case after > hard:
		return e, overBudget(fmt.Sprintf("that would take the server above its hard limit (%.0f Mbit/s)", hard), "stop another screen or camera")
	case in.Force:
		s.logAction(ctx, id, p.ID, "budget_overridden", nil, map[string]any{"kind": in.Kind, "mbps": round2(after)})
		return e, nil
	}
	return e, overBudget(fmt.Sprintf("it would use about %.1f Mbit/s more, and %.1f are left", add, math.Max(0, limit-cur.Mbps)), "stop another screen or camera", "send it anyway")
}

// groupVideoCost is what cameras in a session's groups would add.
func (s *Service) groupVideoCost(ctx context.Context, id string) (float64, error) {
	var members int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tutoring.group_members WHERE session_id=$1`, id).Scan(&members)
	return float64(members) * rateCamera * 4, err
}

// checkAdd refuses adding mbps to a session over its share of the budget,
// unless the teacher insists, and above the hard cap always.
func (s *Service) checkAdd(ctx context.Context, id, what string, add float64, force bool, instead string) error {
	_, limit, hard, err := s.limits(ctx, id)
	if err != nil {
		return err
	}
	cur := s.loadOf(id).Mbps
	switch after := cur + add; {
	case after <= limit:
		return nil
	case after > hard:
		return overBudget(fmt.Sprintf("%s would take the server above its hard limit (%.0f Mbit/s)", what, hard), instead)
	case force:
		return nil
	}
	return overBudget(fmt.Sprintf("%s would use about %.0f Mbit/s, and %.0f are left", what, add, math.Max(0, limit-cur)), instead, "turn it on anyway")
}

// ---------- what a session sends now ----------

// Load is a session's use now, estimated from its tracks and who receives them.
type Load struct {
	Mbps     float64 `json:"mbps"`
	Items    []Item  `json:"items"`
	Viewers  int     `json:"viewers"`
	Tracks   int     `json:"tracks"`  // published, not muted
	Screens  int     `json:"screens"` // the broadcaster's
	Cameras  int     `json:"cameras"` // the broadcaster's
	Students int     `json:"students_sharing"`
	// For monitoring (D-82): what reaches the server (each published track
	// once), who is connected, and how often people came back today.
	DownMbps     float64 `json:"down_mbps"`
	Participants int     `json:"participants"`
	Reconnects   int     `json:"reconnects"`
	// The broadcaster's tracks beyond the first screen and camera (step 3).
	extra       []string
	broadcaster string
}

func (s *Service) loadOf(id string) Load {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	return s.load[id]
}

func rateOf(source string) float64 {
	switch source {
	case "SCREEN_SHARE":
		return rateScreen
	case "MICROPHONE", "SCREEN_SHARE_AUDIO":
		return rateAudio
	}
	return rateCamera
}

// measure estimates a live session's upload from LiveKit's rooms: the
// broadcast to everyone online, backstage to the teachers, each group to its
// members.
func (s *Service) measure(ctx context.Context, sess Session) (Load, error) {
	l := Load{Items: []Item{}}
	var e Estimate
	online := s.hub.online(sess.ID)
	rows, _ := s.pool.Query(ctx, `SELECT user_id::text FROM tutoring.participants WHERE session_id=$1 AND role IN ('teacher','coteacher')`, sess.ID)
	staffIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return l, err
	}
	staff := 0
	for _, u := range staffIDs {
		if online[u] {
			staff++
		}
	}
	l.Viewers = max(len(online)-1, 0)
	l.Participants = len(online)
	l.broadcaster = sess.TeacherID
	if sess.BroadcasterID != nil {
		l.broadcaster = *sess.BroadcasterID
	}
	list := func(room string) ([]media.Participant, error) {
		ps, err := s.media.Participants(ctx, room)
		if errors.Is(err, media.ErrNotInRoom) {
			return nil, nil
		}
		return ps, err
	}
	main, err := list(sess.ID)
	if err != nil {
		return l, err
	}
	v := float64(l.Viewers)
	screen := rateScreen
	if sess.Plan.Quality == "smooth" {
		screen = rateScreenSmooth
	}
	if sess.off(stepFPS) {
		screen *= slowScreen
	}
	for _, p := range main {
		for _, t := range p.Tracks {
			if t.Muted {
				continue
			}
			l.Tracks++
			l.DownMbps += rateOf(t.Source)
			switch {
			case t.Source == "SCREEN_SHARE" && p.Identity == l.broadcaster:
				if l.Screens == 0 {
					e.add("The main screen", screen*v)
				} else {
					e.add("More screens", rateExtra*v)
					l.extra = append(l.extra, t.Sid)
				}
				l.Screens++
			case t.Source == "CAMERA" && p.Identity == l.broadcaster:
				if l.Cameras == 0 {
					e.add("The camera", rateCamera*v)
				} else {
					e.add("More cameras", rateExtra*v)
					l.extra = append(l.extra, t.Sid)
				}
				l.Cameras++
			case t.Source == "MICROPHONE" || t.Source == "SCREEN_SHARE_AUDIO":
				e.add("Voices and sound", rateAudio*v)
			}
		}
	}
	stage, err := list(stageRoom(sess.ID))
	if err != nil {
		return l, err
	}
	for _, p := range stage {
		shared := false
		for _, t := range p.Tracks {
			if t.Muted {
				continue
			}
			l.Tracks++
			l.DownMbps += rateOf(t.Source)
			shared = true
			e.add("Shared with the teachers", rateOf(t.Source)*float64(max(staff, 1)))
		}
		if shared {
			l.Students++
		}
	}
	if sess.GroupsOpen {
		gs, err := s.groups(ctx, s.pool, sess.ID)
		if err != nil {
			return l, err
		}
		for _, g := range gs {
			ps, err := list(groupRoom(sess.ID, g.ID))
			if err != nil {
				return l, err
			}
			others := float64(max(len(ps)-1, 0))
			for _, p := range ps {
				for _, t := range p.Tracks {
					if !t.Muted {
						l.Tracks++
						l.DownMbps += rateOf(t.Source)
						e.add("In groups", rateOf(t.Source)*others)
					}
				}
			}
		}
	}
	// One line per kind of item, adding up repeats.
	sum := map[string]float64{}
	var order []string
	for _, it := range e.Items {
		if _, ok := sum[it.Name]; !ok {
			order = append(order, it.Name)
		}
		sum[it.Name] += it.Mbps
	}
	for _, n := range order {
		l.Items = append(l.Items, Item{Name: n, Mbps: round2(sum[n])})
	}
	l.Mbps = e.Mbps
	l.DownMbps = round2(l.DownMbps)
	return l, nil
}
