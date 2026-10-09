package live

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
)

// Hub is the in-process connection registry and broadcaster (ADR-05). It
// holds only ephemeral data: sockets and "dashboard needs refresh" flags.
// All state that affects grading is in Postgres before it is broadcast.
type Hub struct {
	svc      *Service
	mu       sync.Mutex
	students map[string]map[*client]struct{} // attempt id → sockets
	teachers map[string]map[*client]struct{} // session id → sockets
	dirty    map[string]bool                 // session ids whose dashboard changed
}

// client is one socket with a bounded outbound queue; a client that cannot
// keep up is dropped rather than slowing everyone else down.
type client struct {
	send   chan []byte
	cancel context.CancelFunc
}

func newClient(cancel context.CancelFunc) *client {
	return &client{send: make(chan []byte, 32), cancel: cancel}
}

func (c *client) push(msg []byte) {
	select {
	case c.send <- msg:
	default:
		c.cancel() // slow consumer
	}
}

func newHub(s *Service) *Hub {
	return &Hub{svc: s, students: map[string]map[*client]struct{}{}, teachers: map[string]map[*client]struct{}{}, dirty: map[string]bool{}}
}

func register(m map[string]map[*client]struct{}, key string, c *client) {
	if m[key] == nil {
		m[key] = map[*client]struct{}{}
	}
	m[key][c] = struct{}{}
}

// unregister removes c and reports how many sockets remain for key.
func unregister(m map[string]map[*client]struct{}, key string, c *client) int {
	delete(m[key], c)
	n := len(m[key])
	if n == 0 {
		delete(m, key)
	}
	return n
}

func (h *Hub) addStudent(attemptID string, c *client) {
	h.mu.Lock()
	register(h.students, attemptID, c)
	h.mu.Unlock()
}

func (h *Hub) removeStudent(attemptID string, c *client) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return unregister(h.students, attemptID, c)
}

func (h *Hub) addTeacher(sessionID string, c *client) {
	h.mu.Lock()
	register(h.teachers, sessionID, c)
	h.mu.Unlock()
}

func (h *Hub) removeTeacher(sessionID string, c *client) {
	h.mu.Lock()
	unregister(h.teachers, sessionID, c)
	h.mu.Unlock()
}

func (h *Hub) isConnected(attemptID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.students[attemptID]) > 0
}

func (h *Hub) toStudent(attemptID string, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.students[attemptID] {
		c.push(msg)
	}
}

func (h *Hub) toTeachers(sessionID string, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.teachers[sessionID] {
		c.push(msg)
	}
}

func (h *Hub) markDirty(sessionID string) {
	h.mu.Lock()
	h.dirty[sessionID] = true
	h.mu.Unlock()
}

// publishAttempt sends the student their new state and flags the dashboard.
func (h *Hub) publishAttempt(sess *Session, a *Attempt) {
	h.markDirty(a.SessionID)
	if !h.isConnected(a.ID) {
		return
	}
	st, err := h.svc.studentState(context.Background(), sess, a)
	if err != nil {
		h.svc.log.Warn("build student state", "err", err)
		return
	}
	msg, _ := json.Marshal(st)
	h.toStudent(a.ID, msg)
}

// alert pushes a violation to the teacher immediately (FR-PR-03, AC-06: within 2 s).
func (h *Hub) alert(sess *Session, a *Attempt, kind, action string) {
	name := ""
	if users, err := h.svc.users.UsersByID(context.Background(), []string{a.UserID}); err == nil {
		name = users[a.UserID].Name
	}
	msg, _ := json.Marshal(map[string]any{
		"type": "alert", "server_time": h.svc.now().UnixMilli(), "attempt_id": a.ID, "student_name": name,
		"student_number": a.StudentNumber, "kind": kind, "action": action, "state": a.State,
	})
	h.toTeachers(sess.ID, msg)
}

func (h *Hub) sessionEnded(sessionID string) {
	msg, _ := json.Marshal(map[string]any{"type": "session_ended", "server_time": h.svc.now().UnixMilli()})
	h.toTeachers(sessionID, msg)
	h.markDirty(sessionID)
}

// flushDashboards pushes refreshed dashboards for changed sessions; run at
// 2 Hz so a burst of 100 answers costs one dashboard query (architecture §2.2).
func (h *Hub) flushDashboards(ctx context.Context) {
	h.mu.Lock()
	var ids []string
	for id := range h.dirty {
		if len(h.teachers[id]) > 0 {
			ids = append(ids, id)
		}
		delete(h.dirty, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		d, err := h.svc.dashboard(ctx, id)
		if err != nil {
			h.svc.log.Warn("dashboard", "session", id, "err", err)
			continue
		}
		msg, _ := json.Marshal(d)
		h.toTeachers(id, msg)
	}
}

// ---------- message shapes ----------

func ms(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.UnixMilli()
	return &v
}

// StudentState is the full state pushed to a student on every change and on
// (re)connect, so the client can always resume from server state (ADR-04).
// Deadlines are server epoch milliseconds; the client renders
// deadline - (Date.now() + offset) using server_time (ADR-07).
type StudentState struct {
	Type                string                `json:"type"`
	ServerTime          int64                 `json:"server_time"`
	AttemptID           string                `json:"attempt_id"`
	SessionID           string                `json:"session_id"`
	SessionTitle        string                `json:"session_title"`
	QuizTitle           string                `json:"quiz_title"`
	SessionStatus       string                `json:"session_status"`
	State               string                `json:"state"`
	StudentNumber       *string               `json:"student_number"`
	CountdownDeadline   *int64                `json:"countdown_deadline"`
	QuizDeadline        *int64                `json:"quiz_deadline"`
	QuestionDeadline    *int64                `json:"question_deadline"`
	QuizRemainingMs     *int64                `json:"quiz_remaining_ms"`
	QuestionRemainingMs *int64                `json:"question_remaining_ms"`
	Index               int                   `json:"index"`
	Total               int                   `json:"total"`
	Answered            []bool                `json:"answered"`
	OneWay              bool                  `json:"one_way"`
	Question            *quiz.StudentQuestion `json:"question"`
	Answer              *quiz.Response        `json:"answer"`
	Policy              string                `json:"violation_policy"`
	Warnings            int                   `json:"warnings"`
	AllowedWarnings     int                   `json:"allowed_warnings"`
	BlurGraceMs         int                   `json:"blur_grace_ms"`
	InvalidReason       string                `json:"invalid_reason,omitempty"`
	Team                *TeamInfo             `json:"team,omitempty"` // D-44
	Captain             bool                  `json:"captain,omitempty"`
}

func (s *Service) studentState(ctx context.Context, sess *Session, a *Attempt) (StudentState, error) {
	v := s.sessionView(sess)
	cur := v.question(a, a.Current)
	eff := v.Effective(cur, a.Overrides)
	st := StudentState{
		Type: "state", ServerTime: s.now().UnixMilli(), AttemptID: a.ID, SessionID: v.ID, SessionTitle: v.Title,
		QuizTitle: v.Snapshot.QuizTitle, SessionStatus: v.Status, State: a.State, StudentNumber: a.StudentNumber,
		CountdownDeadline: ms(a.CountdownDeadline), QuizDeadline: ms(a.QuizDeadline),
		QuestionDeadline: ms(EffectiveQuestionDeadline(a)), QuizRemainingMs: a.QuizRemainingMs,
		QuestionRemainingMs: a.QuestionRemainingMs, Index: a.Current, Total: len(a.Order), OneWay: eff.OneWayNavigation,
		Policy: eff.ViolationPolicy, Warnings: a.Warnings, AllowedWarnings: eff.AllowedWarnings,
		BlurGraceMs: eff.BlurGraceMs, InvalidReason: a.InvalidReason, Captain: a.Captain,
	}
	if a.TeamID != nil {
		var t TeamInfo
		err := s.pool.QueryRow(ctx, `SELECT t.id, t.name, t.color, t.position, (SELECT count(*) FROM live.attempts x WHERE x.team_id=t.id)
			FROM live.teams t WHERE t.id=$1`, *a.TeamID).Scan(&t.ID, &t.Name, &t.Color, &t.Position, &t.Members)
		if err == nil {
			st.Team = &t
		}
	}
	// Questions are only visible while running: hidden when paused (UC-03 7b)
	// and never before the start (BR-05).
	if a.State != StateInProgress || cur == nil {
		return st, nil
	}
	view := applyOrder(*cur, a.OptionOrders[cur.ID])
	st.Question = &view
	rows, err := s.pool.Query(ctx, `SELECT question_id, response FROM live.answers WHERE attempt_id=$1`, a.ID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	saved := map[string][]byte{}
	for rows.Next() {
		var qid string
		var raw []byte
		if err := rows.Scan(&qid, &raw); err != nil {
			return st, err
		}
		saved[qid] = raw
	}
	if raw, ok := saved[cur.ID]; ok {
		var r quiz.Response
		if json.Unmarshal(raw, &r) == nil {
			st.Answer = &r
		}
	}
	st.Answered = make([]bool, len(a.Order))
	for i := range a.Order {
		_, st.Answered[i] = saved[v.question(a, i).ID]
	}
	return st, rows.Err()
}

// StudentState returns the current state for the REST fallback.
func (s *Service) StudentState(ctx context.Context, userID, attemptID string) (StudentState, error) {
	a, err := scanAttempt(s.pool.QueryRow(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.id=$1`, attemptID))
	if err != nil {
		return StudentState{}, err
	}
	if err := s.ownAttempt(userID, a); err != nil {
		return StudentState{}, err
	}
	sess, err := s.session(ctx, a.SessionID)
	if err != nil {
		return StudentState{}, err
	}
	return s.studentState(ctx, sess, a)
}

// DashboardRow is one student on the teacher's live dashboard (FR-SS-10).
type DashboardRow struct {
	AttemptID     string  `json:"attempt_id"`
	UserID        string  `json:"user_id"`
	Name          string  `json:"name"`
	Avatar        string  `json:"avatar,omitempty"` // picture version (D-49)
	StudentNumber *string `json:"student_number"`
	State         string  `json:"state"`
	Index         int     `json:"index"`
	Total         int     `json:"total"`
	Answered      int     `json:"answered"`
	Warnings      int     `json:"warnings"`
	Violations    int     `json:"violations"`
	ExtensionSec  int     `json:"extension_sec"`
	QuizDeadline  *int64  `json:"quiz_deadline"`
	RemainingMs   *int64  `json:"remaining_ms"` // while paused
	// CountdownDeadline is set while an admitted student counts down; an
	// admitted student without one waits for the teacher (D-53).
	CountdownDeadline *int64  `json:"countdown_deadline"`
	Connected         bool    `json:"connected"`
	InvalidReason     string  `json:"invalid_reason,omitempty"`
	TeamID            *string `json:"team_id,omitempty"`
	Captain           bool    `json:"captain,omitempty"`
}

type Dashboard struct {
	Type       string         `json:"type"`
	ServerTime int64          `json:"server_time"`
	Session    Session        `json:"session"`
	Counts     map[string]int `json:"counts"`
	Rows       []DashboardRow `json:"rows"`
	TeamMode   string         `json:"team_mode"`  // D-44
	StartMode  string         `json:"start_mode"` // D-53
	Teams      []TeamInfo     `json:"teams,omitempty"`
}

func (s *Service) dashboard(ctx context.Context, sessionID string) (Dashboard, error) {
	sess, err := s.session(ctx, sessionID)
	if err != nil {
		return Dashboard{}, err
	}
	v := s.sessionView(sess)
	rows, err := s.pool.Query(ctx, `SELECT `+attemptCols+`, (SELECT count(*) FROM live.answers x WHERE x.attempt_id=a.id)
		FROM live.attempts a WHERE a.session_id=$1 ORDER BY a.created_at`, sessionID)
	if err != nil {
		return Dashboard{}, err
	}
	type pair struct {
		a        *Attempt
		answered int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pair, error) {
		var p pair
		var a Attempt
		var ov, oo []byte
		var order []int32
		err := r.Scan(&a.ID, &a.SessionID, &a.UserID, &a.StudentNumber, &a.State, &ov, &a.ExtensionSec,
			&a.CountdownDeadline, &a.StartedAt, &a.QuizDeadline, &a.QuizRemainingMs, &a.Current, &a.QuestionDeadline,
			&a.QuestionRemainingMs, &a.PausedAt, &order, &oo, &a.Warnings, &a.Violations, &a.DisconnectedAt,
			&a.SubmittedAt, &a.InvalidatedAt, &a.InvalidReason, &a.CreatedAt, &a.TeamID, &a.Captain, &p.answered)
		a.Order = make([]int, len(order))
		p.a = &a
		return p, err
	})
	if err != nil {
		return Dashboard{}, err
	}
	ids := make([]string, len(list))
	for i, p := range list {
		ids[i] = p.a.UserID
	}
	users, err := s.users.UsersByID(ctx, ids)
	if err != nil {
		return Dashboard{}, err
	}
	d := Dashboard{Type: "dashboard", ServerTime: s.now().UnixMilli(), Session: v, Counts: map[string]int{}, Rows: make([]DashboardRow, 0, len(list))}
	for _, p := range list {
		a := p.a
		d.Counts[a.State]++
		d.Rows = append(d.Rows, DashboardRow{
			AttemptID: a.ID, UserID: a.UserID, Name: users[a.UserID].Name, Avatar: users[a.UserID].Avatar, StudentNumber: a.StudentNumber, State: a.State,
			Index: a.Current, Total: len(a.Order), Answered: p.answered, Warnings: a.Warnings, Violations: a.Violations,
			ExtensionSec: a.ExtensionSec, QuizDeadline: ms(a.QuizDeadline), RemainingMs: a.QuizRemainingMs, CountdownDeadline: ms(a.CountdownDeadline),
			Connected: s.hub.isConnected(a.ID), InvalidReason: a.InvalidReason, TeamID: a.TeamID, Captain: a.Captain,
		})
	}
	d.StartMode = v.Effective(nil, settings.Overrides{}).StartMode
	if d.TeamMode = v.Effective(nil, settings.Overrides{}).TeamMode; d.TeamMode != "off" {
		if d.Teams, err = s.teamInfos(ctx, sessionID); err != nil {
			return d, err
		}
	}
	return d, nil
}

// Dashboard returns the live dashboard for the owning teacher.
func (s *Service) Dashboard(ctx context.Context, teacherID, sessionID string) (Dashboard, error) {
	if _, err := s.ownedSession(ctx, teacherID, sessionID); err != nil {
		return Dashboard{}, err
	}
	return s.dashboard(ctx, sessionID)
}

// Event is one entry in the integrity log (BA §11).
type Event struct {
	ID        int64           `json:"id"`
	AttemptID *string         `json:"attempt_id"`
	ActorID   *string         `json:"actor_id"`
	Kind      string          `json:"kind"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Service) Events(ctx context.Context, teacherID, sessionID string) ([]Event, error) {
	if _, err := s.ownedSession(ctx, teacherID, sessionID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, attempt_id, actor_id, kind, details, created_at FROM live.events WHERE session_id=$1 ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		err := r.Scan(&e.ID, &e.AttemptID, &e.ActorID, &e.Kind, &e.Details, &e.CreatedAt)
		return e, err
	})
}
