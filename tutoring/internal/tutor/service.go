// Package tutor runs tutoring sessions (D-59): creating and joining them,
// the waiting room, starting and ending, what each student may publish,
// raised hands, chat and attendance. Media goes through LiveKit; this
// package decides who may do what and tells LiveKit (TS-NFR-22).
package tutor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/platform"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Platform answers who may take part in a classroom.
type Platform interface {
	Access(ctx context.Context, classroomID, userID string) (platform.Access, error)
}

// Media is the media server's part (LiveKit).
type Media interface {
	Token(g media.Grant) (string, error)
	SetPermission(ctx context.Context, room, identity string, subscribe bool, sources []string) error
	Mute(ctx context.Context, room, identity string, sources ...string) error
	Remove(ctx context.Context, room, identity string) error
	EndRoom(ctx context.Context, room string) error
}

type Service struct {
	pool     *pgxpool.Pool
	platform Platform
	media    Media
	mediaURL string // what browsers connect to (wss://…)
	log      *slog.Logger
	now      func() time.Time
	hub      *hub
}

func New(pool *pgxpool.Pool, p Platform, m Media, mediaURL string, log *slog.Logger) *Service {
	s := &Service{pool: pool, platform: p, media: m, mediaURL: mediaURL, log: log, now: time.Now}
	s.hub = newHub(s)
	return s
}

// ---------- model ----------

type Session struct {
	ID            string     `json:"id"`
	TeacherID     string     `json:"teacher_id"`
	TeacherName   string     `json:"teacher_name"`
	ClassroomID   string     `json:"classroom_id"`
	ClassroomName string     `json:"classroom_name"`
	Title         string     `json:"title"`
	JoinCode      string     `json:"join_code"`
	Status        string     `json:"status"` // open, live, ended
	AdmitMode     string     `json:"admit_mode"`
	Locked        bool       `json:"locked"`
	ChatMode      string     `json:"chat_mode"`
	SlowSeconds   int        `json:"slow_seconds"`
	PinnedID      *int64     `json:"pinned_id,omitempty"`
	ScheduledAt   *time.Time `json:"scheduled_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
}

const sessionCols = `id, teacher_id, teacher_name, classroom_id, classroom_name, title, join_code, status, admit_mode, locked, chat_mode,
	slow_seconds, pinned_id, scheduled_at, created_at, started_at, ended_at`

func scanSession(row pgx.Row) (Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.TeacherID, &s.TeacherName, &s.ClassroomID, &s.ClassroomName, &s.Title, &s.JoinCode, &s.Status, &s.AdmitMode,
		&s.Locked, &s.ChatMode, &s.SlowSeconds, &s.PinnedID, &s.ScheduledAt, &s.CreatedAt, &s.StartedAt, &s.EndedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, web.ErrNotFound
	}
	return s, err
}

// Participant is one person in a session and what they may do.
type Participant struct {
	UserID      string     `json:"user_id"`
	Name        string     `json:"name"`
	Role        string     `json:"role"`  // teacher or student
	State       string     `json:"state"` // waiting, admitted, refused, removed
	AllowMic    bool       `json:"allow_mic"`
	AllowCamera bool       `json:"allow_camera"`
	AllowScreen bool       `json:"allow_screen"`
	ChatMuted   bool       `json:"chat_muted"`
	HandAt      *time.Time `json:"hand_at,omitempty"`
	FirstAt     time.Time  `json:"first_at"`
	Online      bool       `json:"online"`
}

const participantCols = `user_id, name, role, state, allow_mic, allow_camera, allow_screen, chat_muted, hand_at, first_at`

func scanParticipant(row pgx.Row) (Participant, error) {
	var p Participant
	err := row.Scan(&p.UserID, &p.Name, &p.Role, &p.State, &p.AllowMic, &p.AllowCamera, &p.AllowScreen, &p.ChatMuted, &p.HandAt, &p.FirstAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, web.ErrNotFound
	}
	return p, err
}

// Sources are what p may publish: everything for teachers, only what the
// teacher allowed for students (TS-FR-20, TS-FR-21).
func (p Participant) Sources() []string {
	if p.Role == "teacher" {
		return []string{media.Camera, media.Microphone, media.Screen, media.ScreenAudio}
	}
	out := []string{}
	if p.AllowMic {
		out = append(out, media.Microphone)
	}
	if p.AllowCamera {
		out = append(out, media.Camera)
	}
	if p.AllowScreen {
		out = append(out, media.Screen, media.ScreenAudio)
	}
	return out
}

var (
	errEnded     = web.NewError(http.StatusGone, "session_ended", "this session has ended")
	errLocked    = web.NewError(http.StatusForbidden, "session_locked", "this session is locked: nobody new can join")
	errRemoved   = web.NewError(http.StatusForbidden, "removed", "the teacher removed you from this session")
	errRefused   = web.NewError(http.StatusForbidden, "refused", "the teacher didn't admit you to this session")
	errNotLive   = web.NewError(http.StatusConflict, "not_live", "the session hasn't started yet")
	errTeachOnly = web.NewError(http.StatusForbidden, "forbidden", "only the session's teacher can do this")
)

// ---------- sessions ----------

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O or 1/I

func newCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

type CreateInput struct {
	ClassroomID string     `json:"classroom_id"`
	Title       string     `json:"title"`
	AdmitMode   string     `json:"admit_mode"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
}

// Create makes a session for one of the teacher's classrooms (TS-FR-01).
func (s *Service) Create(ctx context.Context, p authn.Person, in CreateInput) (Session, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.AdmitMode == "" {
		in.AdmitMode = "auto"
	}
	f := map[string]string{}
	if n := len([]rune(in.Title)); n < 1 || n > 200 {
		f["title"] = "give the session a title of up to 200 characters"
	}
	if in.AdmitMode != "auto" && in.AdmitMode != "manual" {
		f["admit_mode"] = "admit automatically or manually"
	}
	if len(f) > 0 {
		return Session{}, web.Invalid(f)
	}
	a, err := s.platform.Access(ctx, in.ClassroomID, p.ID)
	if err != nil {
		return Session{}, err
	}
	if a.Role != "teacher" {
		return Session{}, web.NewError(http.StatusForbidden, "forbidden", "only the classroom's teacher can create its sessions")
	}
	if a.Archived {
		return Session{}, web.Conflict("this classroom is archived")
	}
	for range 5 {
		sess, err := scanSession(s.pool.QueryRow(ctx, `INSERT INTO tutoring.sessions (teacher_id, teacher_name, classroom_id, classroom_name, title, join_code, admit_mode, scheduled_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+sessionCols, p.ID, p.Name, a.ClassroomID, a.ClassroomName, in.Title, newCode(), in.AdmitMode, in.ScheduledAt))
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			continue // the join code was taken
		}
		return sess, err
	}
	return Session{}, errors.New("could not find a free join code")
}

// SessionSorts are the columns the teacher's list sorts by.
var sessionSorts = map[string]string{"created": "created_at", "title": "lower(title)", "scheduled": "scheduled_at"}

// List returns one page of a teacher's sessions (TS-FR-90, TS-FR-91).
func (s *Service) List(ctx context.Context, p authn.Person, classroomID, status string, pg Page) ([]Session, int, error) {
	where := ` FROM tutoring.sessions WHERE teacher_id=$1 AND ($2 = '' OR classroom_id::text = $2) AND ($3 = '' OR status = $3) AND ($4 = '' OR title ILIKE $4)`
	args := []any{p.ID, classroomID, status, pg.Like()}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT `+sessionCols+where+pg.OrderBy(sessionSorts, "id")+pg.Limit(), args...)
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) { return scanSession(r) })
	return list, total, err
}

func (s *Service) session(ctx context.Context, id string) (Session, error) {
	return scanSession(s.pool.QueryRow(ctx, `SELECT `+sessionCols+` FROM tutoring.sessions WHERE id=$1`, id))
}

// owned returns a session the caller teaches.
func (s *Service) owned(ctx context.Context, p authn.Person, id string) (Session, error) {
	sess, err := s.session(ctx, id)
	if err != nil {
		return sess, err
	}
	if sess.TeacherID != p.ID {
		return sess, web.ErrNotFound // like other people's resources on the platform
	}
	return sess, nil
}

func (s *Service) participant(ctx context.Context, sessionID, userID string) (Participant, error) {
	return scanParticipant(s.pool.QueryRow(ctx, `SELECT `+participantCols+` FROM tutoring.participants WHERE session_id=$1 AND user_id=$2`, sessionID, userID))
}

func (s *Service) logAction(ctx context.Context, sessionID, actorID, action string, target *string, details any) {
	b, _ := json.Marshal(details)
	if details == nil {
		b = []byte("{}")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO tutoring.log (session_id, actor_id, action, target_id, details) VALUES ($1, $2, $3, $4, $5)`, sessionID, actorID, action, target, b); err != nil {
		s.log.Warn("tutoring log", "err", err)
	}
}

// ---------- joining (TS-FR-02, TS-FR-03) ----------

// View is a session as one person sees it.
type View struct {
	Session      Session       `json:"session"`
	Me           Participant   `json:"me"`
	Participants []Participant `json:"participants,omitempty"` // teachers only
	Online       int           `json:"online"`
	Waiting      int           `json:"waiting"`
	Pinned       *Message      `json:"pinned,omitempty"`
	MediaURL     string        `json:"media_url"`
}

// Join enters a session by its code. The platform checks the classroom:
// its teacher, or an enrolled student (joining may enrol, BR-02). There
// are no guests (PO-11).
func (s *Service) Join(ctx context.Context, p authn.Person, code string) (View, error) {
	sess, err := scanSession(s.pool.QueryRow(ctx, `SELECT `+sessionCols+` FROM tutoring.sessions WHERE join_code=$1`, strings.ToUpper(strings.TrimSpace(code))))
	if err != nil {
		return View{}, err
	}
	if sess.Status == "ended" {
		return View{}, errEnded
	}
	role := "student"
	if sess.TeacherID == p.ID {
		role = "teacher"
	} else {
		a, err := s.platform.Access(ctx, sess.ClassroomID, p.ID)
		if err != nil {
			return View{}, err
		}
		if a.Role != "student" {
			return View{}, web.ErrForbidden
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		existing, err := scanParticipant(tx.QueryRow(ctx, `SELECT `+participantCols+` FROM tutoring.participants WHERE session_id=$1 AND user_id=$2 FOR UPDATE`, sess.ID, p.ID))
		switch {
		case err == nil:
			switch existing.State {
			case "removed":
				return errRemoved
			case "refused":
				return errRefused
			}
			_, err = tx.Exec(ctx, `UPDATE tutoring.participants SET name=$3 WHERE session_id=$1 AND user_id=$2`, sess.ID, p.ID, p.Name)
			return err
		case !errors.Is(err, web.ErrNotFound):
			return err
		}
		if sess.Locked && role == "student" {
			return errLocked
		}
		state := "admitted"
		if role == "student" && sess.AdmitMode == "manual" {
			state = "waiting"
		}
		_, err = tx.Exec(ctx, `INSERT INTO tutoring.participants (session_id, user_id, name, role, state, first_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			sess.ID, p.ID, p.Name, role, state, s.now())
		return err
	})
	if err != nil {
		return View{}, err
	}
	s.hub.changed(sess.ID)
	return s.View(ctx, p, sess.ID)
}

// View returns the session as p sees it.
func (s *Service) View(ctx context.Context, p authn.Person, sessionID string) (View, error) {
	sn, err := s.snapshot(ctx, sessionID)
	if err != nil {
		return View{}, err
	}
	v, ok := sn.viewFor(p.ID, s.mediaURL)
	if !ok {
		return View{}, web.ErrNotFound
	}
	return v, nil
}

// MediaToken lets an admitted person join the session's media room, with
// exactly the sources they may publish (TS-NFR-21, TS-NFR-22). Teachers
// can join before the start to check their devices; students once it is live.
func (s *Service) MediaToken(ctx context.Context, p authn.Person, sessionID string) (string, error) {
	sess, err := s.session(ctx, sessionID)
	if err != nil {
		return "", err
	}
	me, err := s.participant(ctx, sessionID, p.ID)
	if err != nil {
		return "", err
	}
	switch {
	case sess.Status == "ended":
		return "", errEnded
	case me.State != "admitted":
		return "", web.NewError(http.StatusForbidden, "not_admitted", "you haven't been admitted yet")
	case me.Role == "student" && sess.Status != "live":
		return "", errNotLive
	}
	meta, _ := json.Marshal(map[string]string{"role": me.Role})
	return s.media.Token(media.Grant{Room: sess.ID, Identity: p.ID, Name: me.Name, Metadata: string(meta), Subscribe: true, Sources: me.Sources()})
}

// ---------- the teacher's controls ----------

// Start makes the session live (TS-FR-04).
func (s *Service) Start(ctx context.Context, p authn.Person, id string) error {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return err
	}
	if sess.Status != "open" {
		return web.Conflict("the session has already started or ended")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET status='live', started_at=$2 WHERE id=$1`, id, s.now()); err != nil {
		return err
	}
	s.hub.startVisits(ctx, id)
	s.hub.changed(id)
	return nil
}

// End stops the session: everyone is disconnected and no media is sent
// any more (TS-FR-04).
func (s *Service) End(ctx context.Context, p authn.Person, id string) error {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return err
	}
	if sess.Status == "ended" {
		return nil
	}
	now := s.now()
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET status='ended', ended_at=$2 WHERE id=$1`, id, now); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.visits SET left_at=$2 WHERE session_id=$1 AND left_at IS NULL`, id, now); err != nil {
		return err
	}
	if err := s.media.EndRoom(ctx, id); err != nil && !errors.Is(err, media.ErrNotInRoom) {
		s.log.Warn("ending media room", "session", id, "err", err)
	}
	s.hub.end(id)
	return nil
}

type SettingsInput struct {
	Locked      *bool   `json:"locked,omitempty"`
	AdmitMode   *string `json:"admit_mode,omitempty"`
	ChatMode    *string `json:"chat_mode,omitempty"`
	SlowSeconds *int    `json:"slow_seconds,omitempty"`
}

var chatModes = map[string]bool{"off": true, "to_teacher": true, "everyone": true, "announcements": true}

// Settings changes the lock (TS-FR-61), admission and chat (TS-FR-40, TS-FR-42); it applies at once.
func (s *Service) Settings(ctx context.Context, p authn.Person, id string, in SettingsInput) error {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return err
	}
	f := map[string]string{}
	if in.AdmitMode != nil && *in.AdmitMode != "auto" && *in.AdmitMode != "manual" {
		f["admit_mode"] = "admit automatically or manually"
	}
	if in.ChatMode != nil && !chatModes[*in.ChatMode] {
		f["chat_mode"] = "off, to_teacher, everyone or announcements"
	}
	if in.SlowSeconds != nil && (*in.SlowSeconds < 0 || *in.SlowSeconds > 600) {
		f["slow_seconds"] = "0 to 600 seconds"
	}
	if len(f) > 0 {
		return web.Invalid(f)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET locked=coalesce($2, locked), admit_mode=coalesce($3, admit_mode),
		chat_mode=coalesce($4, chat_mode), slow_seconds=coalesce($5, slow_seconds) WHERE id=$1`, sess.ID, in.Locked, in.AdmitMode, in.ChatMode, in.SlowSeconds); err != nil {
		return err
	}
	s.logAction(ctx, id, p.ID, "settings_changed", nil, in)
	s.hub.changed(id)
	return nil
}

// Admit lets waiting students in, or refuses them (TS-FR-03).
func (s *Service) Admit(ctx context.Context, p authn.Person, id string, userIDs []string, admit bool) (int, error) {
	if _, err := s.owned(ctx, p, id); err != nil {
		return 0, err
	}
	state := "admitted"
	if !admit {
		state = "refused"
	}
	tag, err := s.pool.Exec(ctx, `UPDATE tutoring.participants SET state=$3 WHERE session_id=$1 AND user_id = ANY($2::uuid[]) AND state='waiting'`, id, userIDs, state)
	if err != nil {
		return 0, err
	}
	if !admit {
		for _, u := range userIDs {
			s.hub.kick(id, u, "refused")
		}
	}
	s.hub.changed(id)
	return int(tag.RowsAffected()), nil
}

// Remove takes a student out; they can't rejoin this session (TS-FR-60).
func (s *Service) Remove(ctx context.Context, p authn.Person, id, userID string) error {
	if _, err := s.owned(ctx, p, id); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE tutoring.participants SET state='removed', hand_at=NULL WHERE session_id=$1 AND user_id=$2 AND role='student'`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return web.ErrNotFound
	}
	if err := s.media.Remove(ctx, id, userID); err != nil && !errors.Is(err, media.ErrNotInRoom) {
		return err
	}
	s.logAction(ctx, id, p.ID, "removed", &userID, nil)
	s.hub.kick(id, userID, "removed")
	s.hub.changed(id)
	return nil
}

// PermissionsInput allows or forbids devices (nil leaves one as it is),
// for some students or for everyone, and optionally asks them to turn the
// allowed devices on (TS-FR-21, TS-FR-22, TS-FR-24).
type PermissionsInput struct {
	UserIDs []string `json:"user_ids,omitempty"`
	All     bool     `json:"all,omitempty"`
	Mic     *bool    `json:"mic,omitempty"`
	Camera  *bool    `json:"camera,omitempty"`
	Screen  *bool    `json:"screen,omitempty"`
	Ask     bool     `json:"ask,omitempty"`
	// LowerHand clears the students' raised hands, as allowing from the queue does.
	LowerHand bool `json:"lower_hand,omitempty"`
}

func (s *Service) SetPermissions(ctx context.Context, p authn.Person, id string, in PermissionsInput) (int, error) {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return 0, err
	}
	if !in.All && len(in.UserIDs) == 0 {
		return 0, web.Invalid(map[string]string{"user_ids": "choose students, or everyone"})
	}
	rows, _ := s.pool.Query(ctx, `UPDATE tutoring.participants SET allow_mic=coalesce($3, allow_mic), allow_camera=coalesce($4, allow_camera),
		allow_screen=coalesce($5, allow_screen), hand_at=CASE WHEN $6 THEN NULL ELSE hand_at END
		WHERE session_id=$1 AND role='student' AND state IN ('admitted', 'waiting') AND ($7 OR user_id = ANY($2::uuid[]))
		RETURNING `+participantCols, id, in.UserIDs, in.Mic, in.Camera, in.Screen, in.LowerHand, in.All)
	changed, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Participant, error) { return scanParticipant(r) })
	if err != nil {
		return 0, err
	}
	for _, c := range changed {
		// Live at once for anyone connected; the next token carries it otherwise.
		if err := s.media.SetPermission(ctx, sess.ID, c.UserID, true, c.Sources()); err != nil && !errors.Is(err, media.ErrNotInRoom) {
			s.log.Warn("media permission", "session", id, "user", c.UserID, "err", err)
		}
		if in.Ask {
			s.hub.send(id, c.UserID, event{Type: "ask", Data: map[string]bool{"mic": in.Mic != nil && *in.Mic, "camera": in.Camera != nil && *in.Camera, "screen": in.Screen != nil && *in.Screen}})
		}
	}
	s.logAction(ctx, id, p.ID, "permissions_changed", nil, in)
	s.hub.changed(id)
	return len(changed), nil
}

// MuteInput mutes or stops published devices, for one student or everyone (TS-FR-23).
type MuteInput struct {
	UserID string `json:"user_id,omitempty"`
	All    bool   `json:"all,omitempty"`
	Mic    bool   `json:"mic,omitempty"`
	Camera bool   `json:"camera,omitempty"`
	Screen bool   `json:"screen,omitempty"`
}

func (s *Service) Mute(ctx context.Context, p authn.Person, id string, in MuteInput) error {
	if _, err := s.owned(ctx, p, id); err != nil {
		return err
	}
	var sources []string
	if in.Mic {
		sources = append(sources, media.Microphone)
	}
	if in.Camera {
		sources = append(sources, media.Camera)
	}
	if in.Screen {
		sources = append(sources, media.Screen, media.ScreenAudio)
	}
	if len(sources) == 0 {
		return web.Invalid(map[string]string{"mic": "choose what to mute"})
	}
	var targets []string
	if in.All {
		rows, _ := s.pool.Query(ctx, `SELECT user_id FROM tutoring.participants WHERE session_id=$1 AND role='student' AND state='admitted'`, id)
		var err error
		if targets, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
	} else {
		if _, err := s.participant(ctx, id, in.UserID); err != nil {
			return err
		}
		targets = []string{in.UserID}
	}
	for _, u := range targets {
		if err := s.media.Mute(ctx, id, u, sources...); err != nil && !errors.Is(err, media.ErrNotInRoom) {
			return err
		}
	}
	s.logAction(ctx, id, p.ID, "muted", nil, in)
	return nil
}

// Hand raises or lowers the caller's own hand (TS-FR-24).
func (s *Service) Hand(ctx context.Context, p authn.Person, id string, raised bool) error {
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return err
	}
	if me.Role != "student" || me.State != "admitted" {
		return web.ErrForbidden
	}
	var at *time.Time
	if raised {
		if me.HandAt != nil {
			return nil // keeps its place in the queue
		}
		now := s.now()
		at = &now
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.participants SET hand_at=$3 WHERE session_id=$1 AND user_id=$2`, id, p.ID, at); err != nil {
		return err
	}
	s.hub.changed(id)
	return nil
}

// LowerHand lowers a student's hand for them.
func (s *Service) LowerHand(ctx context.Context, p authn.Person, id, userID string) error {
	if _, err := s.owned(ctx, p, id); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.participants SET hand_at=NULL WHERE session_id=$1 AND user_id=$2`, id, userID); err != nil {
		return err
	}
	s.hub.changed(id)
	return nil
}

// CloseOpenVisits ends visits left open by a restart; people reconnect and
// open new ones (TS-NFR-11).
func (s *Service) CloseOpenVisits(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE tutoring.visits SET left_at=$1 WHERE left_at IS NULL`, s.now())
	return err
}
