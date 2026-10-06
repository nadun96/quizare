package live

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	mrand "math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
)

// Quizzes is the slice of the quiz module this module needs.
type Quizzes interface {
	GetQuiz(ctx context.Context, teacherID, id string) (quiz.Quiz, error)
	QuizLayers(ctx context.Context, q quiz.Quiz) (settings.Effective, []settings.Overrides, error)
	ListQuestions(ctx context.Context, teacherID, quizID string) ([]quiz.Question, error)
}

// Classrooms is the slice of the content module this module needs.
type Classrooms interface {
	TopicContext(ctx context.Context, teacherID, topicID string) (content.TopicContext, error)
	EnsureEnrolled(ctx context.Context, classroomID, userID, studentNumber string) (content.Enrolment, error)
	ListCategories(ctx context.Context, teacherID, classroomID string) ([]content.Category, error)
	CategoryMembers(ctx context.Context, teacherID, classroomID string) ([]content.CategoryMember, error)
}

type Users interface {
	UsersByID(ctx context.Context, ids []string) (map[string]auth.User, error)
}

// Hooks let the evaluation module react inside the same transaction
// (architecture: cross-module side effects via River jobs inserted in-tx).
type Hooks struct {
	AttemptFinished func(ctx context.Context, tx pgx.Tx, sessionID, attemptID string) error
	SessionEnded    func(ctx context.Context, tx pgx.Tx, sessionID string) error
}

type Service struct {
	pool       *pgxpool.Pool
	quizzes    Quizzes
	classrooms Classrooms
	users      Users
	baseURL    string
	log        *slog.Logger
	hooks      Hooks
	hub        *Hub
	now        func() time.Time

	mu       sync.Mutex
	sessions map[string]*Session // in-process cache of live sessions (ADR-05)
}

func NewService(pool *pgxpool.Pool, q Quizzes, c Classrooms, u Users, baseURL string, log *slog.Logger) *Service {
	s := &Service{pool: pool, quizzes: q, classrooms: c, users: u, baseURL: strings.TrimRight(baseURL, "/"),
		log: log, now: time.Now, sessions: map[string]*Session{}}
	s.hub = newHub(s)
	return s
}

func (s *Service) SetHooks(h Hooks) { s.hooks = h }

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

var (
	errSessionEnded = httpx.NewError(http.StatusGone, "session_ended", "this session has ended")
	errWrongState   = func(msg string) error { return httpx.NewError(http.StatusConflict, "wrong_state", msg) }
)

// ---------- sessions ----------

type SessionInput struct {
	Title    string              `json:"title"`
	Settings *settings.Overrides `json:"settings"`
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

func validateSessionSettings(o *settings.Overrides) error {
	if o == nil {
		return nil
	}
	if err := o.Validate(settings.LevelSession); err != nil {
		return settings.ToHTTP(err)
	}
	return nil
}

// CreateSession generates a session with its own QR/short link (FR-SS-01, FR-SS-02).
func (s *Service) CreateSession(ctx context.Context, teacherID, quizID string, in SessionInput) (*Session, error) {
	if err := validateSessionSettings(in.Settings); err != nil {
		return nil, err
	}
	qz, err := s.quizzes.GetQuiz(ctx, teacherID, quizID)
	if err != nil {
		return nil, err
	}
	if qz.Status != quiz.StatusReady {
		return nil, httpx.NewError(http.StatusConflict, "quiz_not_ready", "mark the quiz as Ready before starting a session")
	}
	tc, err := s.classrooms.TopicContext(ctx, teacherID, qz.TopicID)
	if err != nil {
		return nil, err
	}
	if tc.ClassroomArchived {
		return nil, httpx.Conflict("the classroom is archived")
	}
	base, layers, err := s.quizzes.QuizLayers(ctx, qz)
	if err != nil {
		return nil, err
	}
	qs, err := s.quizzes.ListQuestions(ctx, teacherID, quizID)
	if err != nil {
		return nil, err
	}
	if len(qs) == 0 {
		return nil, httpx.NewError(http.StatusConflict, "quiz_not_ready", "the quiz has no questions")
	}
	snap := &Snapshot{QuizTitle: qz.Title, Base: base, Layers: layers, Questions: qs}
	sess := &Session{QuizID: quizID, TeacherID: teacherID, ClassroomID: tc.ClassroomID, Status: SessionOpen, Snapshot: snap}
	sess.Title = strings.TrimSpace(in.Title)
	if sess.Title == "" {
		sess.Title = qz.Title
	}
	if in.Settings != nil {
		sess.Settings = *in.Settings
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	for range 5 {
		sess.JoinCode = newCode(6)
		err = s.pool.QueryRow(ctx, `INSERT INTO live.sessions(quiz_id, teacher_id, classroom_id, title, join_code, settings, snapshot, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at`,
			quizID, teacherID, tc.ClassroomID, sess.Title, sess.JoinCode, sess.Settings.JSON(), raw, s.now()).Scan(&sess.ID, &sess.CreatedAt)
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			continue
		}
		break
	}
	if err != nil {
		return nil, err
	}
	sess.JoinURL = s.joinURL(sess.JoinCode)
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	return sess, nil
}

func (s *Service) joinURL(code string) string { return s.baseURL + "/j/" + code }

const sessionCols = `id, quiz_id, teacher_id, classroom_id, title, join_code, status, settings, extension_sec, created_at, ended_at, released_at`

func scanSession(r pgx.Row, withSnapshot bool) (*Session, error) {
	var sess Session
	var st, snap []byte
	dst := []any{&sess.ID, &sess.QuizID, &sess.TeacherID, &sess.ClassroomID, &sess.Title, &sess.JoinCode, &sess.Status, &st,
		&sess.ExtensionSec, &sess.CreatedAt, &sess.EndedAt, &sess.ReleasedAt}
	if withSnapshot {
		dst = append(dst, &snap)
	}
	if err := r.Scan(dst...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, err
	}
	var err error
	if sess.Settings, err = settings.Parse(st); err != nil {
		return nil, err
	}
	if withSnapshot {
		sess.Snapshot = &Snapshot{}
		if err := json.Unmarshal(snap, sess.Snapshot); err != nil {
			return nil, err
		}
	}
	return &sess, nil
}

// session returns the cached session, loading it on first use (and after a
// restart, which is how state rehydrates: ADR-05).
func (s *Service) session(ctx context.Context, id string) (*Session, error) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if ok {
		return sess, nil
	}
	sess, err := scanSession(s.pool.QueryRow(ctx, `SELECT `+sessionCols+`, snapshot FROM live.sessions WHERE id=$1`, id), true)
	if err != nil {
		return nil, err
	}
	sess.JoinURL = s.joinURL(sess.JoinCode)
	s.mu.Lock()
	if cached, ok := s.sessions[id]; ok {
		sess = cached
	} else {
		s.sessions[id] = sess
	}
	s.mu.Unlock()
	return sess, nil
}

// updateCached applies a committed change to the cached session.
func (s *Service) updateCached(id string, fn func(*Session)) {
	s.mu.Lock()
	if sess, ok := s.sessions[id]; ok {
		fn(sess)
	}
	s.mu.Unlock()
}

// sessionView copies a session for safe use outside the lock.
func (s *Service) sessionView(sess *Session) Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *sess
}

func (s *Service) ownedSession(ctx context.Context, teacherID, id string) (*Session, error) {
	sess, err := s.session(ctx, id)
	if err != nil {
		return nil, err
	}
	if sess.TeacherID != teacherID {
		return nil, httpx.ErrNotFound
	}
	return sess, nil
}

func (s *Service) ListSessions(ctx context.Context, teacherID, quizID string) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+sessionCols+` FROM live.sessions WHERE quiz_id=$1 AND teacher_id=$2 ORDER BY created_at DESC`, quizID, teacherID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows, false)
		if err != nil {
			return nil, err
		}
		sess.JoinURL = s.joinURL(sess.JoinCode)
		out = append(out, *sess)
	}
	return out, rows.Err()
}

// UpdateSettings changes session-level overrides, e.g. the countdown before
// admitting the next group (FR-SS-06, UC-02 5b). It affects later events only.
func (s *Service) UpdateSettings(ctx context.Context, teacherID, id string, o settings.Overrides) (Session, error) {
	if err := validateSessionSettings(&o); err != nil {
		return Session{}, err
	}
	sess, err := s.ownedSession(ctx, teacherID, id)
	if err != nil {
		return Session{}, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE live.sessions SET settings=$2 WHERE id=$1 AND status <> 'ended'`, id, o.JSON())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errSessionEnded
		}
		return s.event(ctx, tx, id, "", teacherID, "settings_changed", o)
	})
	if err != nil {
		return Session{}, err
	}
	s.updateCached(id, func(x *Session) { x.Settings = o })
	return s.sessionView(sess), nil
}

// HasSessions reports whether a quiz has been run (used for BR-16).
func (s *Service) HasSessions(ctx context.Context, quizID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM live.sessions WHERE quiz_id=$1)`, quizID).Scan(&ok)
	return ok, err
}

// ClassroomHasSessions reports whether a classroom has session results.
func (s *Service) ClassroomHasSessions(ctx context.Context, classroomID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM live.sessions WHERE classroom_id=$1)`, classroomID).Scan(&ok)
	return ok, err
}

func (s *Service) event(ctx context.Context, tx pgx.Tx, sessionID, attemptID, actorID, kind string, details any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if details == nil {
		b = []byte("{}")
	}
	nullable := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	_, err = tx.Exec(ctx, `INSERT INTO live.events(session_id, attempt_id, actor_id, kind, details, created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		sessionID, nullable(attemptID), nullable(actorID), kind, b, s.now())
	return err
}

// ---------- attempts: persistence ----------

const attemptCols = `a.id, a.session_id, a.user_id, a.student_number, a.state, a.overrides, a.extension_sec,
	a.countdown_deadline, a.started_at, a.quiz_deadline, a.quiz_remaining_ms, a.current_index, a.question_deadline,
	a.question_remaining_ms, a.paused_at, a.question_order, a.option_orders, a.warnings, a.violations, a.disconnected_at,
	a.submitted_at, a.invalidated_at, a.invalid_reason, a.created_at, a.team_id, a.captain`

func scanAttempt(r pgx.Row) (*Attempt, error) {
	var a Attempt
	var ov, oo []byte
	var order []int32
	err := r.Scan(&a.ID, &a.SessionID, &a.UserID, &a.StudentNumber, &a.State, &ov, &a.ExtensionSec,
		&a.CountdownDeadline, &a.StartedAt, &a.QuizDeadline, &a.QuizRemainingMs, &a.Current, &a.QuestionDeadline,
		&a.QuestionRemainingMs, &a.PausedAt, &order, &oo, &a.Warnings, &a.Violations, &a.DisconnectedAt,
		&a.SubmittedAt, &a.InvalidatedAt, &a.InvalidReason, &a.CreatedAt, &a.TeamID, &a.Captain)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if a.Overrides, err = settings.Parse(ov); err != nil {
		return nil, err
	}
	a.Order = make([]int, len(order))
	for i, v := range order {
		a.Order[i] = int(v)
	}
	if err := json.Unmarshal(oo, &a.OptionOrders); err != nil {
		return nil, err
	}
	return &a, nil
}

func saveAttempt(ctx context.Context, tx pgx.Tx, a *Attempt, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE live.attempts SET state=$2, overrides=$3, extension_sec=$4, countdown_deadline=$5, started_at=$6,
		quiz_deadline=$7, quiz_remaining_ms=$8, current_index=$9, question_deadline=$10, question_remaining_ms=$11, paused_at=$12,
		warnings=$13, violations=$14, disconnected_at=$15, submitted_at=$16, invalidated_at=$17, invalid_reason=$18, updated_at=$19
		WHERE id=$1`, a.ID, a.State, a.Overrides.JSON(), a.ExtensionSec, a.CountdownDeadline, a.StartedAt, a.QuizDeadline,
		a.QuizRemainingMs, a.Current, a.QuestionDeadline, a.QuestionRemainingMs, a.PausedAt, a.Warnings, a.Violations,
		a.DisconnectedAt, a.SubmittedAt, a.InvalidatedAt, a.InvalidReason, now)
	return err
}

// errNoop lets a change function bail out without writing or broadcasting.
var errNoop = errors.New("no change")

// change runs fn on a locked attempt and persists it; the attempt is
// broadcast only after commit (write-through, ADR-05).
func (s *Service) change(ctx context.Context, attemptID string, fn func(tx pgx.Tx, sess *Session, a *Attempt) error) (*Attempt, error) {
	var a *Attempt
	var sess *Session
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		a, err = scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.id=$1 FOR UPDATE`, attemptID))
		if err != nil {
			return err
		}
		cached, err := s.session(ctx, a.SessionID)
		if err != nil {
			return err
		}
		v := s.sessionView(cached) // a consistent copy; the cache may change concurrently
		sess = &v
		if err := fn(tx, sess, a); err != nil {
			return err
		}
		return saveAttempt(ctx, tx, a, s.now())
	})
	if errors.Is(err, errNoop) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	s.hub.publishAttempt(sess, a)
	return a, nil
}

// ---------- helpers on the snapshot ----------

func (sess *Session) question(a *Attempt, index int) *quiz.Question {
	if index < 0 || index >= len(a.Order) {
		return nil
	}
	return &sess.Snapshot.Questions[a.Order[index]]
}

func (sess *Session) questionLimit(a *Attempt, index int) int {
	q := sess.question(a, index)
	if q == nil {
		return 0
	}
	eff := sess.Effective(q, a.Overrides)
	if !eff.OneWayNavigation {
		return 0 // D-17: per-question timers apply only with one-way navigation
	}
	return eff.QuestionTimeLimitSec
}

func (sess *Session) startAttempt(a *Attempt, now time.Time) {
	eff := sess.Effective(nil, a.Overrides)
	start(a, now, eff.QuizTimeLimitSec, sess.ExtensionSec+a.ExtensionSec, sess.questionLimit(a, 0))
}

func (s *Service) finish(ctx context.Context, tx pgx.Tx, sess *Session, a *Attempt, actor, why string) error {
	submit(a, s.now())
	if err := s.event(ctx, tx, sess.ID, a.ID, actor, "submitted", map[string]string{"reason": why}); err != nil {
		return err
	}
	if s.hooks.AttemptFinished != nil {
		return s.hooks.AttemptFinished(ctx, tx, sess.ID, a.ID)
	}
	return nil
}

// ---------- joining (FR-SS-03, BR-01, BR-02, BR-04, FR-ACC-06) ----------

// JoinPreview is what the join page shows before the student commits.
type JoinPreview struct {
	SessionID         string   `json:"session_id"`
	Title             string   `json:"title"`
	QuizTitle         string   `json:"quiz_title"`
	Status            string   `json:"status"`
	StudentIDRequired bool     `json:"student_id_required"`
	Attempt           *Attempt `json:"attempt"`
	// Teams (D-44): the list to choose from when team_mode is self.
	TeamMode string     `json:"team_mode"`
	Teams    []TeamInfo `json:"teams,omitempty"`
}

func (s *Service) sessionByCode(ctx context.Context, code string) (*Session, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM live.sessions WHERE join_code=$1`, strings.ToUpper(strings.TrimSpace(code))).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.session(ctx, id)
}

func (s *Service) Preview(ctx context.Context, userID, code string) (JoinPreview, error) {
	sess, err := s.sessionByCode(ctx, code)
	if err != nil {
		return JoinPreview{}, err
	}
	v := s.sessionView(sess)
	if v.Status == SessionEnded {
		return JoinPreview{}, errSessionEnded
	}
	eff := v.Effective(nil, settings.Overrides{})
	p := JoinPreview{SessionID: v.ID, Title: v.Title, QuizTitle: v.Snapshot.QuizTitle, Status: v.Status,
		StudentIDRequired: eff.StudentIDRequired, TeamMode: eff.TeamMode}
	if eff.TeamMode != "off" {
		if p.Teams, err = s.teamInfos(ctx, v.ID); err != nil {
			return p, err
		}
	}
	a, err := scanAttempt(s.pool.QueryRow(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.session_id=$1 AND a.user_id=$2`, v.ID, userID))
	if err == nil {
		p.Attempt = a
	} else if !errors.Is(err, httpx.ErrNotFound) {
		return p, err
	}
	return p, nil
}

// Join enrols the student if allowed and places them in the waiting room,
// or admits them straight away under auto-admit (FR-SS-04).
func (s *Service) Join(ctx context.Context, userID, code, studentNumber, teamID string) (*Attempt, error) {
	sess, err := s.sessionByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	v := s.sessionView(sess)
	if v.Status == SessionEnded {
		return nil, errSessionEnded // BR-04
	}
	en, err := s.classrooms.EnsureEnrolled(ctx, v.ClassroomID, userID, studentNumber)
	if err != nil {
		return nil, err
	}
	if en.Status != "active" {
		return nil, httpx.NewError(http.StatusForbidden, "enrolment_pending", "your teacher has not approved your enrolment yet")
	}
	eff := v.Effective(nil, settings.Overrides{})
	if err := s.checkTeamChoice(ctx, v.ID, eff, teamID); err != nil {
		return nil, err
	}
	rng := mrand.New(mrand.NewPCG(mrand.Uint64(), mrand.Uint64()))
	order, opts := newOrders(v.Snapshot.Questions, eff.QuestionOrder == "shuffled", func(q quiz.Question) bool {
		return v.Effective(&q, settings.Overrides{}).OptionOrder == "shuffled"
	}, rng)
	order32 := make([]int32, len(order))
	for i, x := range order {
		order32[i] = int32(x)
	}
	optsJSON, _ := json.Marshal(opts)

	var a *Attempt
	created := false
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO live.attempts(session_id, user_id, student_number, state, question_order, option_orders, created_at, updated_at)
			VALUES ($1,$2,$3,'waiting',$4,$5,$6,$6) ON CONFLICT (session_id, user_id) DO NOTHING RETURNING id`,
			v.ID, userID, en.StudentNumber, order32, optsJSON, s.now()).Scan(&id)
		created = err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		a, err = scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.session_id=$1 AND a.user_id=$2 FOR UPDATE`, v.ID, userID))
		if err != nil || !created {
			return err
		}
		if err := s.event(ctx, tx, v.ID, a.ID, userID, "joined", nil); err != nil {
			return err
		}
		if err := s.placeTeam(ctx, tx, sess, eff, a.ID, userID, teamID); err != nil {
			return err
		}
		if a, err = scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.id=$1`, a.ID)); err != nil {
			return err
		}
		if eff.AdmissionMode == "auto" {
			if err := s.admitTx(ctx, tx, sess, a, ""); err != nil {
				return err
			}
			return saveAttempt(ctx, tx, a, s.now())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.hub.publishAttempt(sess, a)
	return a, nil
}

func (s *Service) admitTx(ctx context.Context, tx pgx.Tx, sess *Session, a *Attempt, actor string) error {
	v := s.sessionView(sess)
	countdown := v.Effective(nil, a.Overrides).CountdownSeconds
	admit(a, s.now(), countdown)
	if countdown == 0 {
		v.startAttempt(a, s.now())
	}
	if _, err := tx.Exec(ctx, `UPDATE live.sessions SET status='live' WHERE id=$1 AND status='open'`, sess.ID); err != nil {
		return err
	}
	s.updateCached(sess.ID, func(x *Session) {
		if x.Status == SessionOpen {
			x.Status = SessionLive
		}
	})
	return s.event(ctx, tx, sess.ID, a.ID, actor, "admitted", map[string]int{"countdown_seconds": countdown})
}

// ---------- teacher controls ----------

// Target selects attempts for a bulk command: explicit ids, or all.
type Target struct {
	AttemptIDs []string `json:"attempt_ids"`
	All        bool     `json:"all"`
}

func (s *Service) targets(ctx context.Context, sessionID string, t Target, states ...string) ([]string, error) {
	if !t.All && len(t.AttemptIDs) == 0 {
		return nil, httpx.BadRequest("choose students or all")
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM live.attempts WHERE session_id=$1 AND state = ANY($2)
		AND ($3 OR id::text = ANY($4)) ORDER BY created_at`, sessionID, states, t.All, t.AttemptIDs)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (s *Service) live(ctx context.Context, teacherID, sessionID string) (*Session, error) {
	sess, err := s.ownedSession(ctx, teacherID, sessionID)
	if err != nil {
		return nil, err
	}
	if s.sessionView(sess).Status == SessionEnded {
		return nil, errSessionEnded
	}
	return sess, nil
}

// Admit admits waiting students one by one, in groups or all (FR-SS-04).
func (s *Service) Admit(ctx context.Context, teacherID, sessionID string, t Target) (int, error) {
	sess, err := s.live(ctx, teacherID, sessionID)
	if err != nil {
		return 0, err
	}
	ids, err := s.targets(ctx, sessionID, t, StateWaiting)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		_, err := s.change(ctx, id, func(tx pgx.Tx, _ *Session, a *Attempt) error {
			if a.State != StateWaiting {
				return nil
			}
			n++
			return s.admitTx(ctx, tx, sess, a, teacherID)
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Pause pauses running attempts; their questions are hidden and timers stop (FR-SS-08, AC-07).
func (s *Service) Pause(ctx context.Context, teacherID, sessionID string, t Target) (int, error) {
	return s.bulk(ctx, teacherID, sessionID, t, []string{StateInProgress}, "paused", func(_ *Session, a *Attempt) {
		pause(a, s.now())
	})
}

func (s *Service) Resume(ctx context.Context, teacherID, sessionID string, t Target) (int, error) {
	return s.bulk(ctx, teacherID, sessionID, t, []string{StatePaused}, "resumed", func(_ *Session, a *Attempt) {
		resume(a, s.now())
	})
}

func (s *Service) bulk(ctx context.Context, teacherID, sessionID string, t Target, states []string, kind string, fn func(*Session, *Attempt)) (int, error) {
	if _, err := s.live(ctx, teacherID, sessionID); err != nil {
		return 0, err
	}
	ids, err := s.targets(ctx, sessionID, t, states...)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		_, err := s.change(ctx, id, func(tx pgx.Tx, sess *Session, a *Attempt) error {
			ok := false
			for _, st := range states {
				ok = ok || a.State == st
			}
			if !ok {
				return nil
			}
			fn(sess, a)
			n++
			return s.event(ctx, tx, sessionID, a.ID, teacherID, kind, nil)
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Extend adds time for selected students, or for the whole session
// (FR-SS-09). Session-wide extensions also apply to students who start later.
func (s *Service) Extend(ctx context.Context, teacherID, sessionID string, t Target, seconds int) (int, error) {
	if seconds <= 0 || seconds > 24*3600 {
		return 0, httpx.Invalid(map[string]string{"seconds": "must be between 1 and 86400"})
	}
	if _, err := s.live(ctx, teacherID, sessionID); err != nil {
		return 0, err
	}
	if t.All {
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE live.sessions SET extension_sec = extension_sec + $2 WHERE id=$1`, sessionID, seconds); err != nil {
				return err
			}
			return s.event(ctx, tx, sessionID, "", teacherID, "session_extended", map[string]int{"seconds": seconds})
		})
		if err != nil {
			return 0, err
		}
		s.updateCached(sessionID, func(x *Session) { x.ExtensionSec += seconds })
		return s.bulk(ctx, teacherID, sessionID, t, []string{StateInProgress, StatePaused}, "extended", func(_ *Session, a *Attempt) {
			extend(a, seconds)
		})
	}
	return s.bulk(ctx, teacherID, sessionID, t, []string{StateWaiting, StateAdmitted, StateInProgress, StatePaused, StateInvalidated}, "extended",
		func(_ *Session, a *Attempt) {
			a.ExtensionSec += seconds
			extend(a, seconds)
		})
}

// End ends the session for everyone (FR-SS-11): running attempts are
// submitted as they stand; students who never started are marked not started.
func (s *Service) End(ctx context.Context, teacherID, sessionID string) error {
	sess, err := s.ownedSession(ctx, teacherID, sessionID)
	if err != nil {
		return err
	}
	return s.endSession(ctx, sess, teacherID)
}

func (s *Service) endSession(ctx context.Context, sess *Session, actor string) error {
	var changed []*Attempt
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE live.sessions SET status='ended', ended_at=$2 WHERE id=$1 AND status <> 'ended'`, sess.ID, s.now())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errSessionEnded
		}
		rows, err := tx.Query(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.session_id=$1
			AND a.state IN ('waiting','admitted','in_progress','paused') FOR UPDATE`, sess.ID)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Attempt, error) { return scanAttempt(r) })
		if err != nil {
			return err
		}
		for _, a := range list {
			if a.State == StateInProgress || a.State == StatePaused {
				if err := s.finish(ctx, tx, sess, a, actor, "session_ended"); err != nil {
					return err
				}
			} else {
				a.State = StateNotStarted
				a.CountdownDeadline = nil
			}
			if err := saveAttempt(ctx, tx, a, s.now()); err != nil {
				return err
			}
			changed = append(changed, a)
		}
		if err := s.event(ctx, tx, sess.ID, "", actor, "session_ended", nil); err != nil {
			return err
		}
		if actor != "" {
			if err := audit.Log(ctx, tx, actor, "session_ended", "session", sess.ID, nil); err != nil {
				return err
			}
		}
		if s.hooks.SessionEnded != nil {
			return s.hooks.SessionEnded(ctx, tx, sess.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	now := s.now()
	s.updateCached(sess.ID, func(x *Session) { x.Status = SessionEnded; x.EndedAt = &now })
	for _, a := range changed {
		s.hub.publishAttempt(sess, a)
	}
	s.hub.sessionEnded(sess.ID)
	return nil
}

// Reinstate restores an invalidated attempt with a logged reason (FR-SS-12, BR-08).
func (s *Service) Reinstate(ctx context.Context, teacherID, attemptID, reason string) (*Attempt, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 500 {
		return nil, httpx.Invalid(map[string]string{"reason": "give a reason (at most 500 characters)"})
	}
	return s.change(ctx, attemptID, func(tx pgx.Tx, sess *Session, a *Attempt) error {
		v := s.sessionView(sess)
		if v.TeacherID != teacherID {
			return httpx.ErrNotFound // BR-08: only the owning teacher
		}
		if v.Status == SessionEnded {
			return errSessionEnded
		}
		if a.State != StateInvalidated {
			return errWrongState("only invalidated attempts can be reinstated")
		}
		a.State = StateInProgress
		if a.StartedAt == nil {
			sess.startAttempt(a, s.now())
		}
		a.InvalidatedAt, a.InvalidReason, a.DisconnectedAt = nil, "", nil
		if err := s.event(ctx, tx, sess.ID, a.ID, teacherID, "reinstated", map[string]string{"reason": reason}); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "attempt_reinstated", "attempt", a.ID, map[string]string{"reason": reason})
	})
}

// ---------- student actions ----------

func (s *Service) ownAttempt(userID string, a *Attempt) error {
	if a.UserID != userID {
		return httpx.ErrNotFound
	}
	return nil
}

// Start begins the quiz early during the countdown (FR-SS-05, AC-03).
func (s *Service) Start(ctx context.Context, userID, attemptID string) (*Attempt, error) {
	return s.change(ctx, attemptID, func(tx pgx.Tx, sess *Session, a *Attempt) error {
		if err := s.ownAttempt(userID, a); err != nil {
			return err
		}
		switch a.State {
		case StateInProgress, StatePaused:
			return nil // idempotent
		case StateAdmitted:
			sess.startAttempt(a, s.now())
			return s.event(ctx, tx, sess.ID, a.ID, userID, "started", map[string]string{"by": "student"})
		default:
			return errWrongState("you have not been admitted yet") // BR-05
		}
	})
}

// SaveAnswer upserts the answer to a question (NFR-11). seq is a client
// counter: a retried or reordered older save never overwrites a newer one.
func (s *Service) SaveAnswer(ctx context.Context, userID, attemptID, questionID string, r quiz.Response, seq int64) error {
	_, err := s.change(ctx, attemptID, func(tx pgx.Tx, sess *Session, a *Attempt) error {
		if err := s.ownAttempt(userID, a); err != nil {
			return err
		}
		now := s.now()
		if !acceptsAnswers(a, now) {
			if a.State == StatePaused {
				return errWrongState("the quiz is paused")
			}
			return errWrongState("time is up or the attempt is closed")
		}
		q, err := s.answerable(sess, a, questionID)
		if err != nil {
			return err
		}
		if err := quiz.ValidateResponse(*q, r); err != nil {
			return httpx.Invalid(map[string]string{"response": err.Error()})
		}
		raw, _ := json.Marshal(r)
		_, err = tx.Exec(ctx, `INSERT INTO live.answers(attempt_id, question_id, student_number, response, client_seq, saved_at)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (attempt_id, question_id) DO UPDATE SET response=EXCLUDED.response, client_seq=EXCLUDED.client_seq, saved_at=EXCLUDED.saved_at
			WHERE live.answers.client_seq <= EXCLUDED.client_seq`, a.ID, questionID, a.StudentNumber, raw, seq, now)
		return err
	})
	return err
}

// answerable returns the question if the student may answer it now:
// the current one under one-way navigation (BR-09), any one otherwise.
func (s *Service) answerable(sess *Session, a *Attempt, questionID string) (*quiz.Question, error) {
	cur := sess.question(a, a.Current)
	if cur != nil && cur.ID == questionID {
		return cur, nil
	}
	if sess.Effective(nil, a.Overrides).OneWayNavigation {
		return nil, errWrongState("you can only answer the current question")
	}
	for i := range a.Order {
		if q := sess.question(a, i); q.ID == questionID {
			return q, nil
		}
	}
	return nil, httpx.ErrNotFound
}

// Advance moves past the current question; after the last one the attempt is
// submitted. fromQuestionID makes retries idempotent.
func (s *Service) Advance(ctx context.Context, userID, attemptID, fromQuestionID string) (*Attempt, error) {
	return s.change(ctx, attemptID, func(tx pgx.Tx, sess *Session, a *Attempt) error {
		if err := s.ownAttempt(userID, a); err != nil {
			return err
		}
		if a.State != StateInProgress {
			if Finished(a.State) {
				return nil
			}
			return errWrongState("the quiz is not running")
		}
		if cur := sess.question(a, a.Current); cur == nil || cur.ID != fromQuestionID {
			return nil // already moved on (retry)
		}
		if !advance(a, s.now(), len(a.Order), func(i int) int { return sess.questionLimit(a, i) }) {
			return s.finish(ctx, tx, sess, a, userID, "completed")
		}
		return nil
	})
}

// Goto jumps to a question when navigation is free (BR-09 configurable).
func (s *Service) Goto(ctx context.Context, userID, attemptID string, index int) (*Attempt, error) {
	return s.change(ctx, attemptID, func(tx pgx.Tx, sess *Session, a *Attempt) error {
		if err := s.ownAttempt(userID, a); err != nil {
			return err
		}
		if a.State != StateInProgress {
			return errWrongState("the quiz is not running")
		}
		if sess.Effective(nil, a.Overrides).OneWayNavigation {
			return errWrongState("this quiz does not allow going back")
		}
		if index < 0 || index >= len(a.Order) {
			return httpx.BadRequest("no such question")
		}
		a.Current = index
		return nil
	})
}

// Submit ends the attempt at the student's request.
func (s *Service) Submit(ctx context.Context, userID, attemptID string) (*Attempt, error) {
	return s.change(ctx, attemptID, func(tx pgx.Tx, sess *Session, a *Attempt) error {
		if err := s.ownAttempt(userID, a); err != nil {
			return err
		}
		if Finished(a.State) {
			return nil
		}
		if a.State != StateInProgress {
			return errWrongState("the quiz is not running")
		}
		return s.finish(ctx, tx, sess, a, userID, "student_submitted")
	})
}

// ---------- proctoring (FR-PR-02…05, ADR-08) ----------

// Violation kinds the client may report. "disconnected" is server-detected.
var violationKinds = map[string]bool{
	"tab_hidden": true, "window_blur": true, "page_close": true, "fullscreen_exit": true, "disconnected": true,
}

type ViolationInput struct {
	Kind     string `json:"kind"`
	ClientTS int64  `json:"client_ts"` // ms since epoch, evidence only
	Device   string `json:"-"`
}

// ReportViolation applies the configured policy and alerts the teacher in
// real time. Signals are evidence, not proof; the raw log is kept (ADR-08).
func (s *Service) ReportViolation(ctx context.Context, userID, attemptID string, in ViolationInput) (*Attempt, error) {
	if !violationKinds[in.Kind] || in.Kind == "disconnected" {
		return nil, httpx.Invalid(map[string]string{"kind": "unknown violation kind"})
	}
	return s.violation(ctx, userID, attemptID, in)
}

func (s *Service) violation(ctx context.Context, userID, attemptID string, in ViolationInput) (*Attempt, error) {
	var action string
	var sessRef *Session
	a, err := s.change(ctx, attemptID, func(tx pgx.Tx, sess *Session, a *Attempt) error {
		sessRef = sess
		if userID != "" {
			if err := s.ownAttempt(userID, a); err != nil {
				return err
			}
		}
		if a.State != StateInProgress {
			return nil // BR-07 applies once started; paused or finished attempts are not penalised
		}
		eff := sess.Effective(sess.question(a, a.Current), a.Overrides)
		action = decide(eff.ViolationPolicy, eff.AllowedWarnings, a.Warnings)
		now := s.now()
		var clientTS *time.Time
		if in.ClientTS > 0 {
			t := time.UnixMilli(in.ClientTS)
			clientTS = &t
		}
		if len(in.Device) > 300 {
			in.Device = in.Device[:300]
		}
		if _, err := tx.Exec(ctx, `INSERT INTO live.violations(attempt_id, kind, action, client_ts, server_ts, device) VALUES ($1,$2,$3,$4,$5,$6)`,
			a.ID, in.Kind, action, clientTS, now, in.Device); err != nil {
			return err
		}
		a.Violations++
		a.DisconnectedAt = nil
		switch action {
		case ActionWarned:
			a.Warnings++
		case ActionInvalidated:
			a.State = StateInvalidated
			a.InvalidatedAt = ptrTime(now)
			a.InvalidReason = in.Kind
		}
		return s.event(ctx, tx, sess.ID, a.ID, "", "violation", map[string]string{"kind": in.Kind, "action": action})
	})
	if err != nil {
		return nil, err
	}
	if action != "" {
		s.hub.alert(sessRef, a, in.Kind, action)
	}
	return a, nil
}

// connected / disconnected implement the heartbeat rule: a socket that closes
// during an attempt and does not come back within the grace period counts as
// a "closed/reloaded" violation (architecture §1.2).
func (s *Service) connected(ctx context.Context, attemptID string) {
	if _, err := s.pool.Exec(ctx, `UPDATE live.attempts SET disconnected_at=NULL WHERE id=$1 AND disconnected_at IS NOT NULL`, attemptID); err != nil {
		s.log.Warn("mark connected", "err", err)
	}
}

func (s *Service) disconnected(ctx context.Context, attemptID string) {
	if _, err := s.pool.Exec(ctx, `UPDATE live.attempts SET disconnected_at=$2 WHERE id=$1 AND state='in_progress'`, attemptID, s.now()); err != nil {
		s.log.Warn("mark disconnected", "err", err)
	}
}
