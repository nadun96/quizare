package poll

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Classrooms is the slice of the content module polls need.
type Classrooms interface {
	GetClassroom(ctx context.Context, teacherID, id string) (content.Classroom, error)
	GetEnrolment(ctx context.Context, classroomID, userID string) (content.Enrolment, bool, error)
}

// Users resolves names of identified participants for the teacher.
type Users interface {
	UsersByID(ctx context.Context, ids []string) (map[string]auth.User, error)
}

type Service struct {
	pool    *pgxpool.Pool
	classes Classrooms
	users   Users
	baseURL string
	log     *slog.Logger
	now     func() time.Time
	hub     *Hub
	joins   *auth.Limiter // per IP
	answers *auth.Limiter // per participant
	uploads *auth.Limiter // per participant
}

func NewService(pool *pgxpool.Pool, classes Classrooms, users Users, baseURL string, log *slog.Logger) *Service {
	s := &Service{pool: pool, classes: classes, users: users, baseURL: strings.TrimRight(baseURL, "/"), log: log, now: time.Now,
		joins: auth.NewLimiter(30, 2*time.Second), answers: auth.NewLimiter(40, 100*time.Millisecond), uploads: auth.NewLimiter(5, 10*time.Second)}
	s.hub = newHub(s)
	return s
}

// SetClock lets tests control time.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Run flushes live results to connected screens at 2 Hz until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.hub.flush(ctx)
		}
	}
}

// Flush pushes pending changes now (tests; Run does this twice a second).
func (s *Service) Flush(ctx context.Context) { s.hub.flush(ctx) }

// ---------- polls ----------

type Settings struct {
	Identity    string `json:"identity"`     // anonymous | identified | optional
	Audience    string `json:"audience"`     // anyone | classroom
	Pacing      string `json:"pacing"`       // self | presenter
	ShowResults string `json:"show_results"` // live | after_answer | presenter | never
	AllowEdit   bool   `json:"allow_edit"`
	// Competition (V2-01, V2-02, D-42).
	Scoring     bool   `json:"scoring"`      // questions with an answer key award points
	SpeedBonus  bool   `json:"speed_bonus"`  // timed presenter-led questions: faster right answers earn more
	Leaderboard string `json:"leaderboard"`  // off | presenter | everyone
	ShowAnswers string `json:"show_answers"` // never | after_answer | presenter | after_close
	Names       string `json:"names"`        // nickname | name (identified participants only)
}

type Poll struct {
	ID          string  `json:"id"`
	TeacherID   string  `json:"teacher_id"`
	ClassroomID *string `json:"classroom_id"`
	Title       string  `json:"title"`
	JoinCode    string  `json:"join_code"`
	JoinURL     string  `json:"join_url"`
	Settings
	Status            string     `json:"status"`
	CurrentIndex      int        `json:"current_index"`
	Revealed          bool       `json:"revealed"`
	AnswersRevealed   bool       `json:"answers_revealed"`
	QuestionStartedAt *time.Time `json:"question_started_at"`
	CreatedAt         time.Time  `json:"created_at"`
	OpenedAt          *time.Time `json:"opened_at"`
	ClosedAt          *time.Time `json:"closed_at"`
	Questions         []Question `json:"questions,omitempty"`
	Participants      int        `json:"participants"`
}

type PollInput struct {
	Title       *string   `json:"title"`
	ClassroomID *string   `json:"classroom_id"`
	Settings    *Settings `json:"settings"`
}

const pollCols = `p.id, p.teacher_id, p.classroom_id, p.title, p.join_code, p.identity, p.audience, p.pacing, p.show_results,
	p.allow_edit, p.status, p.current_index, p.revealed, p.created_at, p.opened_at, p.closed_at,
	(SELECT count(*) FROM poll.participants x WHERE x.poll_id = p.id),
	p.scoring, p.speed_bonus, p.leaderboard, p.show_answers, p.names, p.answers_revealed, p.question_started_at`

func (s *Service) scanPoll(row pgx.Row) (*Poll, error) {
	var p Poll
	err := row.Scan(&p.ID, &p.TeacherID, &p.ClassroomID, &p.Title, &p.JoinCode, &p.Identity, &p.Audience, &p.Pacing,
		&p.ShowResults, &p.AllowEdit, &p.Status, &p.CurrentIndex, &p.Revealed, &p.CreatedAt, &p.OpenedAt, &p.ClosedAt, &p.Participants,
		&p.Scoring, &p.SpeedBonus, &p.Leaderboard, &p.ShowAnswers, &p.Names, &p.AnswersRevealed, &p.QuestionStartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.JoinURL = s.baseURL + "/p/" + p.JoinCode
	return &p, nil
}

// fillDefaults gives settings added in v2 their defaults when a client omits them.
func fillDefaults(st *Settings) {
	if st.Leaderboard == "" {
		st.Leaderboard = "presenter"
	}
	if st.ShowAnswers == "" {
		st.ShowAnswers = "after_close"
		if st.Pacing == "presenter" {
			st.ShowAnswers = "presenter"
		}
	}
	if st.Names == "" {
		st.Names = "nickname"
	}
}

func validSettings(st Settings) map[string]string {
	f := map[string]string{}
	in := func(v string, opts ...string) bool {
		for _, o := range opts {
			if v == o {
				return true
			}
		}
		return false
	}
	if !in(st.Identity, "anonymous", "identified", "optional") {
		f["settings.identity"] = "identity must be anonymous, identified or optional"
	}
	if !in(st.Audience, "anyone", "classroom") {
		f["settings.audience"] = "audience must be anyone or classroom"
	}
	if st.Audience == "classroom" && st.Identity != "identified" {
		f["settings.audience"] = "a classroom-only poll must identify participants"
	}
	if !in(st.Pacing, "self", "presenter") {
		f["settings.pacing"] = "pacing must be self or presenter"
	}
	if !in(st.ShowResults, "live", "after_answer", "presenter", "never") {
		f["settings.show_results"] = "show_results must be live, after_answer, presenter or never"
	}
	if !in(st.Leaderboard, "off", "presenter", "everyone") {
		f["settings.leaderboard"] = "leaderboard must be off, presenter or everyone"
	}
	if !in(st.ShowAnswers, "never", "after_answer", "presenter", "after_close") {
		f["settings.show_answers"] = "show_answers must be never, after_answer, presenter or after_close"
	}
	if st.ShowAnswers == "presenter" && st.Pacing != "presenter" {
		f["settings.show_answers"] = "the presenter reveals answers only in presenter-led polls"
	}
	if !in(st.Names, "nickname", "name") {
		f["settings.names"] = "names must be nickname or name"
	}
	if st.Names == "name" && st.Identity == "anonymous" {
		f["settings.names"] = "an anonymous poll can't show real names"
	}
	return f
}

// DefaultSettings: anonymous, open to anyone with the code, self-paced.
func DefaultSettings() Settings {
	return Settings{Identity: "anonymous", Audience: "anyone", Pacing: "self", ShowResults: "after_answer", AllowEdit: true,
		Leaderboard: "presenter", ShowAnswers: "after_close", Names: "nickname"}
}

func (s *Service) checkClassroom(ctx context.Context, teacherID string, id *string, st Settings) error {
	if id == nil || *id == "" {
		if st.Audience == "classroom" {
			return httpx.Invalid(map[string]string{"classroom_id": "choose the classroom whose students may answer"})
		}
		return nil
	}
	_, err := s.classes.GetClassroom(ctx, teacherID, *id)
	return err
}

func (s *Service) CreatePoll(ctx context.Context, teacherID string, in PollInput) (*Poll, error) {
	st := DefaultSettings()
	if in.Settings != nil {
		st = *in.Settings
	}
	title := ""
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	fillDefaults(&st)
	f := validSettings(st)
	if n := utf8.RuneCountInString(title); n < 1 || n > 200 {
		f["title"] = "title must be 1-200 characters"
	}
	if len(f) > 0 {
		return nil, httpx.Invalid(f)
	}
	if in.ClassroomID != nil && *in.ClassroomID == "" {
		in.ClassroomID = nil
	}
	if err := s.checkClassroom(ctx, teacherID, in.ClassroomID, st); err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		var id string
		err := s.pool.QueryRow(ctx, `INSERT INTO poll.polls (teacher_id, classroom_id, title, join_code, identity, audience, pacing, show_results, allow_edit,
			scoring, speed_bonus, leaderboard, show_answers, names)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`,
			teacherID, in.ClassroomID, title, "P"+newCode(5), st.Identity, st.Audience, st.Pacing, st.ShowResults, st.AllowEdit,
			st.Scoring, st.SpeedBonus, st.Leaderboard, st.ShowAnswers, st.Names).Scan(&id)
		if err != nil && strings.Contains(err.Error(), "join_code") && attempt < 5 {
			continue // code collision
		}
		if err != nil {
			return nil, err
		}
		return s.GetPoll(ctx, teacherID, id)
	}
}

func (s *Service) ownedPoll(ctx context.Context, teacherID, id string) (*Poll, error) {
	p, err := s.scanPoll(s.pool.QueryRow(ctx, `SELECT `+pollCols+` FROM poll.polls p WHERE p.id=$1`, id))
	if err != nil {
		return nil, err
	}
	if p.TeacherID != teacherID {
		return nil, httpx.ErrNotFound
	}
	return p, nil
}

func (s *Service) GetPoll(ctx context.Context, teacherID, id string) (*Poll, error) {
	p, err := s.ownedPoll(ctx, teacherID, id)
	if err != nil {
		return nil, err
	}
	p.Questions, err = s.questions(ctx, p.ID)
	return p, err
}

func (s *Service) ListPolls(ctx context.Context, teacherID string) ([]*Poll, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+pollCols+` FROM poll.polls p WHERE p.teacher_id=$1 ORDER BY p.created_at DESC`, teacherID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Poll{}
	for rows.Next() {
		p, err := s.scanPoll(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) UpdatePoll(ctx context.Context, teacherID, id string, in PollInput) (*Poll, error) {
	p, err := s.ownedPoll(ctx, teacherID, id)
	if err != nil {
		return nil, err
	}
	title, st, classroom := p.Title, p.Settings, p.ClassroomID
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	if in.Settings != nil {
		st = *in.Settings
	}
	if in.ClassroomID != nil {
		classroom = in.ClassroomID
		if *classroom == "" {
			classroom = nil
		}
	}
	fillDefaults(&st)
	f := validSettings(st)
	if n := utf8.RuneCountInString(title); n < 1 || n > 200 {
		f["title"] = "title must be 1-200 characters"
	}
	if p.Participants > 0 && st.Identity != p.Identity {
		// Changing who is identified after people answered would misrepresent them.
		f["settings.identity"] = "identity can't change once people have joined; reset the responses first"
	}
	if len(f) > 0 {
		return nil, httpx.Invalid(f)
	}
	if err := s.checkClassroom(ctx, teacherID, classroom, st); err != nil {
		return nil, err
	}
	_, err = s.pool.Exec(ctx, `UPDATE poll.polls SET title=$2, classroom_id=$3, identity=$4, audience=$5, pacing=$6, show_results=$7,
		allow_edit=$8, scoring=$9, speed_bonus=$10, leaderboard=$11, show_answers=$12, names=$13, updated_at=now() WHERE id=$1`,
		id, title, classroom, st.Identity, st.Audience, st.Pacing, st.ShowResults, st.AllowEdit,
		st.Scoring, st.SpeedBonus, st.Leaderboard, st.ShowAnswers, st.Names)
	if err != nil {
		return nil, err
	}
	if st.Scoring != p.Scoring || st.SpeedBonus != p.SpeedBonus {
		if err := s.rescorePoll(ctx, id); err != nil {
			return nil, err
		}
	}
	s.hub.stateChanged(id)
	return s.GetPoll(ctx, teacherID, id)
}

func (s *Service) DeletePoll(ctx context.Context, teacherID, id string) error {
	if _, err := s.ownedPoll(ctx, teacherID, id); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM poll.polls WHERE id=$1`, id)
	s.hub.closed(id)
	return err
}

// SetStatus opens, closes or reopens a poll.
func (s *Service) SetStatus(ctx context.Context, teacherID, id, status string) (*Poll, error) {
	p, err := s.ownedPoll(ctx, teacherID, id)
	if err != nil {
		return nil, err
	}
	switch status {
	case "open":
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.questions WHERE poll_id=$1`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, httpx.Conflict("add at least one question before opening the poll")
		}
		_, err = s.pool.Exec(ctx, `UPDATE poll.polls SET status='open', opened_at=coalesce(opened_at, $2), closed_at=NULL, updated_at=now() WHERE id=$1`, id, s.now())
	case "closed":
		_, err = s.pool.Exec(ctx, `UPDATE poll.polls SET status='closed', closed_at=$2, updated_at=now() WHERE id=$1`, id, s.now())
	case "draft":
		_, err = s.pool.Exec(ctx, `UPDATE poll.polls SET status='draft', updated_at=now() WHERE id=$1`, id)
	default:
		return nil, httpx.BadRequest("status must be open, closed or draft")
	}
	if err != nil {
		return nil, err
	}
	_ = p
	s.hub.stateChanged(id)
	return s.GetPoll(ctx, teacherID, id)
}

// Present moves a presenter-paced poll to question index (restarting its
// clock) or, on the same question, shows or hides its results and answers.
func (s *Service) Present(ctx context.Context, teacherID, id string, index int, revealed, answersRevealed bool) (*Poll, error) {
	p, err := s.GetPoll(ctx, teacherID, id)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(p.Questions) {
		return nil, httpx.Invalid(map[string]string{"index": "no such question"})
	}
	if index != p.CurrentIndex || p.QuestionStartedAt == nil {
		_, err = s.pool.Exec(ctx, `UPDATE poll.polls SET current_index=$2, revealed=$3, answers_revealed=$4, question_started_at=$5, updated_at=now() WHERE id=$1`,
			id, index, revealed, answersRevealed, s.now())
	} else {
		_, err = s.pool.Exec(ctx, `UPDATE poll.polls SET revealed=$2, answers_revealed=$3, updated_at=now() WHERE id=$1`, id, revealed, answersRevealed)
	}
	if err != nil {
		return nil, err
	}
	s.hub.stateChanged(id)
	s.hub.markAll(id)
	return s.GetPoll(ctx, teacherID, id)
}

// Reset deletes every participant, answer and file, keeping the questions.
func (s *Service) Reset(ctx context.Context, teacherID, id string) error {
	if _, err := s.ownedPoll(ctx, teacherID, id); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, q := range []string{`DELETE FROM poll.participants WHERE poll_id=$1`,
		`DELETE FROM poll.hidden_words WHERE question_id IN (SELECT id FROM poll.questions WHERE poll_id=$1)`,
		`UPDATE poll.polls SET current_index=0, revealed=false, answers_revealed=false, question_started_at=NULL, updated_at=now() WHERE id=$1`} {
		if _, err := tx.Exec(ctx, q, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.hub.stateChanged(id)
	s.hub.markAll(id)
	return nil
}

// ---------- questions ----------

const questionCols = `id, poll_id, position, type, text, body, required, key, points, time_limit_sec`

func scanQuestion(r pgx.Row) (Question, error) {
	var q Question
	var body, key []byte
	if err := r.Scan(&q.ID, &q.PollID, &q.Position, &q.Type, &q.Text, &body, &q.Required, &key, &q.Points, &q.TimeLimitSec); err != nil {
		return q, err
	}
	if len(key) > 0 {
		q.Key = &Key{}
		if err := json.Unmarshal(key, q.Key); err != nil {
			return q, err
		}
	}
	return q, json.Unmarshal(body, &q.Body)
}

func keyJSON(k *Key) []byte {
	if k == nil {
		return nil
	}
	b, _ := json.Marshal(k)
	return b
}

func (s *Service) questions(ctx context.Context, pollID string) ([]Question, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+questionCols+` FROM poll.questions WHERE poll_id=$1 ORDER BY position, created_at`, pollID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Question, error) { return scanQuestion(r) })
	if out == nil {
		out = []Question{}
	}
	return out, err
}

type QuestionInput struct {
	Type         Type   `json:"type"`
	Text         string `json:"text"`
	Body         Body   `json:"body"`
	Required     bool   `json:"required"`
	Key          *Key   `json:"key"`            // scorable types only (D-42)
	Points       *int   `json:"points"`         // default 100
	TimeLimitSec *int   `json:"time_limit_sec"` // presenter pacing: answers close after this
}

func (in QuestionInput) question() (Question, error) {
	q := Question{Type: in.Type, Text: in.Text, Body: in.Body, Required: in.Required, Key: in.Key, Points: DefaultPoints, TimeLimitSec: in.TimeLimitSec}
	if in.Points != nil {
		q.Points = *in.Points
	}
	q.Normalise()
	f := q.Validate()
	for k, v := range q.ValidateKey(q.Key) {
		f[k] = v
	}
	if q.Points < 0 || q.Points > MaxPoints {
		f["points"] = "points must be 0-10000"
	}
	if q.TimeLimitSec != nil && (*q.TimeLimitSec < 5 || *q.TimeLimitSec > 3600) {
		f["time_limit_sec"] = "time limit must be 5-3600 seconds"
	}
	if len(f) > 0 {
		return q, httpx.Invalid(f)
	}
	return q, nil
}

func (s *Service) AddQuestion(ctx context.Context, teacherID, pollID string, in QuestionInput) (Question, error) {
	if _, err := s.ownedPoll(ctx, teacherID, pollID); err != nil {
		return Question{}, err
	}
	q, err := in.question()
	if err != nil {
		return q, err
	}
	body, _ := json.Marshal(q.Body)
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.questions WHERE poll_id=$1`, pollID).Scan(&n); err != nil {
		return q, err
	}
	if n >= MaxQuestions {
		return q, httpx.Conflict("a poll can have at most 50 questions")
	}
	q, err = scanQuestion(s.pool.QueryRow(ctx, `INSERT INTO poll.questions (poll_id, position, type, text, body, required, key, points, time_limit_sec)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+questionCols, pollID, n, q.Type, q.Text, body, q.Required, keyJSON(q.Key), q.Points, q.TimeLimitSec))
	s.hub.stateChanged(pollID)
	return q, err
}

func (s *Service) ownedQuestion(ctx context.Context, teacherID, questionID string) (Question, error) {
	q, err := scanQuestion(s.pool.QueryRow(ctx, `SELECT `+questionCols+` FROM poll.questions WHERE id=$1`, questionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return q, httpx.ErrNotFound
	}
	if err != nil {
		return q, err
	}
	_, err = s.ownedPoll(ctx, teacherID, q.PollID)
	return q, err
}

func (s *Service) UpdateQuestion(ctx context.Context, teacherID, questionID string, in QuestionInput) (Question, error) {
	old, err := s.ownedQuestion(ctx, teacherID, questionID)
	if err != nil {
		return old, err
	}
	q, err := in.question()
	if err != nil {
		return q, err
	}
	if q.Type != old.Type {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.responses WHERE question_id=$1`, questionID).Scan(&n); err != nil {
			return q, err
		}
		if n > 0 {
			return q, httpx.Conflict("this question has answers; its type can't change")
		}
	}
	body, _ := json.Marshal(q.Body)
	q, err = scanQuestion(s.pool.QueryRow(ctx, `UPDATE poll.questions SET type=$2, text=$3, body=$4, required=$5, key=$6, points=$7, time_limit_sec=$8
		WHERE id=$1 RETURNING `+questionCols, questionID, q.Type, q.Text, body, q.Required, keyJSON(q.Key), q.Points, q.TimeLimitSec))
	if err != nil {
		return q, err
	}
	// A corrected key or new points apply to answers already given.
	p, err := s.pollByID(ctx, old.PollID)
	if err != nil {
		return q, err
	}
	if err := s.rescore(ctx, p, q); err != nil {
		return q, err
	}
	s.hub.stateChanged(old.PollID)
	s.hub.markDirty(old.PollID, questionID)
	return q, err
}

func (s *Service) DeleteQuestion(ctx context.Context, teacherID, questionID string) error {
	q, err := s.ownedQuestion(ctx, teacherID, questionID)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM poll.questions WHERE id=$1`, questionID); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE poll.questions q SET position = r.n - 1 FROM (
		SELECT id, row_number() OVER (ORDER BY position, created_at) AS n FROM poll.questions WHERE poll_id=$1) r WHERE q.id = r.id`, q.PollID)
	s.hub.stateChanged(q.PollID)
	return err
}

func (s *Service) Reorder(ctx context.Context, teacherID, pollID string, ids []string) error {
	if _, err := s.ownedPoll(ctx, teacherID, pollID); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, id := range ids {
		tag, err := tx.Exec(ctx, `UPDATE poll.questions SET position=$3 WHERE id=$1 AND poll_id=$2`, id, pollID, i)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return httpx.BadRequest("unknown question " + id)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.hub.stateChanged(pollID)
	return nil
}

// ---------- participants ----------

// Caller identifies whoever is answering: a logged-in user and/or the
// anonymous device token the browser keeps for this poll.
type Caller struct {
	User  *auth.User
	Token string
}

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func newCode(n int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func (s *Service) pollByCode(ctx context.Context, code string) (*Poll, error) {
	return s.scanPoll(s.pool.QueryRow(ctx, `SELECT `+pollCols+` FROM poll.polls p WHERE p.join_code=$1`, strings.ToUpper(strings.TrimSpace(code))))
}

var (
	errLoginRequired = httpx.NewError(http.StatusUnauthorized, "login_required", "log in to take part in this poll")
	errNotInClass    = httpx.NewError(http.StatusForbidden, "not_enrolled", "this poll is only for students of its classroom")
	errNotJoined     = httpx.NewError(http.StatusUnauthorized, "not_joined", "join the poll first")
	errClosed        = httpx.NewError(http.StatusConflict, "poll_closed", "this poll is not accepting answers")
)

// findParticipant returns the caller's participant id, or "" when none.
func (s *Service) findParticipant(ctx context.Context, p *Poll, c Caller) (string, bool, error) {
	var id string
	if c.User != nil && p.Identity != "anonymous" {
		err := s.pool.QueryRow(ctx, `SELECT id FROM poll.participants WHERE poll_id=$1 AND user_id=$2`, p.ID, c.User.ID).Scan(&id)
		if err == nil {
			return id, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", false, err
		}
	}
	if c.Token != "" && p.Identity != "identified" {
		err := s.pool.QueryRow(ctx, `SELECT id FROM poll.participants WHERE poll_id=$1 AND token_hash=$2`, p.ID, hashToken(c.Token)).Scan(&id)
		if err == nil {
			return id, false, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", false, err
		}
	}
	return "", false, nil
}

type JoinInput struct {
	// Optional-identity polls: true to answer under the logged-in account.
	Identify bool `json:"identify"`
	// Shown on the leaderboard instead of "Participant N" (D-42).
	Nickname string `json:"nickname"`
}

type JoinResult struct {
	ParticipantID string `json:"participant_id"`
	Identified    bool   `json:"identified"`
	Token         string `json:"token,omitempty"` // anonymous: keep it to answer again from this device
}

// Join registers the caller. Anonymous participants get a device token,
// which is all that links their answers; no account is ever recorded.
func (s *Service) Join(ctx context.Context, code string, c Caller, ip string, in JoinInput) (JoinResult, error) {
	p, err := s.pollByCode(ctx, code)
	if err != nil {
		return JoinResult{}, err
	}
	if p.Status != "open" {
		return JoinResult{}, errClosed
	}
	nick := CleanNickname(in.Nickname)
	var nickArg any
	if nick != "" {
		nickArg = nick
	}
	// Joining again returns the same participant, so nobody is counted twice;
	// a new nickname replaces the old one.
	if id, identified, err := s.findParticipant(ctx, p, c); err != nil || id != "" {
		if err == nil && nick != "" {
			_, err = s.pool.Exec(ctx, `UPDATE poll.participants SET nickname=$2 WHERE id=$1`, id, nick)
			s.hub.markAll(p.ID)
		}
		return JoinResult{ParticipantID: id, Identified: identified}, err
	}
	if !s.joins.Allow("ip:" + ip) {
		return JoinResult{}, httpx.NewError(http.StatusTooManyRequests, "rate_limited", "too many joins from this network; try again shortly")
	}
	identify := p.Identity == "identified" || (p.Identity == "optional" && in.Identify)
	if identify {
		if c.User == nil {
			return JoinResult{}, errLoginRequired
		}
		if p.Audience == "classroom" && p.ClassroomID != nil {
			e, ok, err := s.classes.GetEnrolment(ctx, *p.ClassroomID, c.User.ID)
			if err != nil {
				return JoinResult{}, err
			}
			if !ok || e.Status != "active" {
				return JoinResult{}, errNotInClass
			}
		}
	}
	if p.Participants >= MaxParticipants {
		return JoinResult{}, httpx.Conflict("this poll is full")
	}
	if identify {
		var id string
		err := s.pool.QueryRow(ctx, `INSERT INTO poll.participants (poll_id, user_id, nickname) VALUES ($1,$2,$3)
			ON CONFLICT (poll_id, user_id) DO UPDATE SET user_id=EXCLUDED.user_id RETURNING id`, p.ID, c.User.ID, nickArg).Scan(&id)
		s.hub.markAll(p.ID)
		return JoinResult{ParticipantID: id, Identified: true}, err
	}
	tok := newToken()
	var id string
	err = s.pool.QueryRow(ctx, `INSERT INTO poll.participants (poll_id, token_hash, nickname) VALUES ($1,$2,$3) RETURNING id`, p.ID, hashToken(tok), nickArg).Scan(&id)
	s.hub.markAll(p.ID)
	return JoinResult{ParticipantID: id, Token: tok}, err
}

// ---------- participant view ----------

type PublicPoll struct {
	Type        string            `json:"type"` // "poll" over the socket
	ServerTime  int64             `json:"server_time"`
	Code        string            `json:"code"`
	Title       string            `json:"title"`
	Identity    string            `json:"identity"`
	Audience    string            `json:"audience"`
	Pacing      string            `json:"pacing"`
	ShowResults string            `json:"show_results"`
	AllowEdit   bool              `json:"allow_edit"`
	Status      string            `json:"status"`
	Index       int               `json:"current_index"`
	Revealed    bool              `json:"revealed"`
	Total       int               `json:"total"`
	Questions   []Question        `json:"questions"` // presenter pacing: only the current one
	Joined      bool              `json:"joined"`
	Identified  bool              `json:"identified"`
	Answers     map[string]Answer `json:"answers,omitempty"`
	Results     map[string]Result `json:"results,omitempty"` // what this participant may see
	// Competition (D-42).
	Scoring           bool             `json:"scoring"`
	SpeedBonus        bool             `json:"speed_bonus"`
	LeaderboardMode   string           `json:"leaderboard_mode"`
	ShowAnswers       string           `json:"show_answers"`
	AnswersRevealed   bool             `json:"answers_revealed"`
	QuestionStartedAt *int64           `json:"question_started_at,omitempty"` // server epoch ms
	Keys              map[string]Key   `json:"keys,omitempty"`                // revealed answer keys only
	Scores            map[string]Score `json:"scores,omitempty"`              // own scores, where the key is revealed
	Leaderboard       []Rank           `json:"leaderboard,omitempty"`         // top 10, when shown to everyone
	Me                *Rank            `json:"me,omitempty"`                  // own rank (personal view)
	MeKey             string           `json:"me_key,omitempty"`              // finds yourself in a shared top 10
	Nickname          string           `json:"nickname,omitempty"`
}

// Score is a participant's result on one question.
type Score struct {
	Points  float64 `json:"points"`
	Correct bool    `json:"correct"`
}

func (q Question) public() Question {
	q.Key = nil
	return q
}

func publicQuestions(qs []Question) []Question {
	out := make([]Question, len(qs))
	for i, q := range qs {
		out[i] = q.public()
	}
	return out
}

// answersVisible decides whether a participant may see q's answer key.
// sharedOnly is true for the update every socket receives: "after answering"
// keys are then left out and come with each participant's own answer instead.
func (p *Poll) answersVisible(q Question, answered, sharedOnly bool) bool {
	if q.Key == nil {
		return false
	}
	switch p.ShowAnswers {
	case "after_answer":
		return (answered && !sharedOnly) || p.Status == "closed"
	case "presenter":
		return p.Status == "closed" || p.Pacing == "presenter" && p.AnswersRevealed && len(p.Questions) > p.CurrentIndex && p.Questions[p.CurrentIndex].ID == q.ID
	case "after_close":
		return p.Status == "closed"
	}
	return false
}

func (p *Poll) competition(v *PublicPoll) {
	v.Scoring, v.SpeedBonus, v.LeaderboardMode, v.ShowAnswers, v.AnswersRevealed = p.Scoring, p.SpeedBonus, p.Leaderboard, p.ShowAnswers, p.AnswersRevealed
	if p.QuestionStartedAt != nil && p.Pacing == "presenter" {
		ms := p.QuestionStartedAt.UnixMilli()
		v.QuestionStartedAt = &ms
	}
}

func top(ranks []Rank, n int) []Rank {
	if len(ranks) > n {
		ranks = ranks[:n]
	}
	out := make([]Rank, len(ranks))
	for i, r := range ranks {
		out[i] = Rank{Rank: r.Rank, Key: r.Key, Name: r.Name, Score: r.Score, Correct: r.Correct, Answered: r.Answered}
	}
	return out
}

func (p *Poll) visibleQuestions(all []Question) []Question {
	if p.Pacing == "presenter" {
		if p.Status != "open" && p.Status != "closed" || p.CurrentIndex >= len(all) {
			return []Question{}
		}
		return []Question{all[p.CurrentIndex]}
	}
	return all
}

// resultsVisible decides whether a participant may see results for q.
func (p *Poll) resultsVisible(q Question, answered bool) bool {
	switch p.ShowResults {
	case "live":
		return true
	case "after_answer":
		return answered || p.Status == "closed"
	case "presenter":
		return p.Pacing == "presenter" && p.Revealed && len(p.Questions) > p.CurrentIndex && p.Questions[p.CurrentIndex].ID == q.ID
	}
	return false
}

// View returns the poll as the caller sees it.
func (s *Service) View(ctx context.Context, code string, c Caller) (PublicPoll, error) {
	p, err := s.pollByCode(ctx, code)
	if err != nil {
		return PublicPoll{}, err
	}
	if p.Status == "draft" {
		return PublicPoll{}, httpx.NewError(http.StatusConflict, "poll_not_open", "this poll hasn't started yet")
	}
	all, err := s.questions(ctx, p.ID)
	if err != nil {
		return PublicPoll{}, err
	}
	p.Questions = all
	v := PublicPoll{Type: "poll", ServerTime: s.now().UnixMilli(), Code: p.JoinCode, Title: p.Title, Identity: p.Identity, Audience: p.Audience,
		Pacing: p.Pacing, ShowResults: p.ShowResults, AllowEdit: p.AllowEdit, Status: p.Status, Index: p.CurrentIndex, Revealed: p.Revealed,
		Total: len(all), Questions: publicQuestions(p.visibleQuestions(all))}
	p.competition(&v)
	pid, identified, err := s.findParticipant(ctx, p, c)
	if err != nil {
		return v, err
	}
	v.Joined, v.Identified = pid != "", identified
	answered := map[string]bool{}
	scores := map[string]Score{}
	if pid != "" {
		v.Answers, scores, err = s.answersOf(ctx, pid)
		if err != nil {
			return v, err
		}
		for k := range v.Answers {
			answered[k] = true
		}
		v.MeKey = rankKey(p.ID, pid)
		_ = s.pool.QueryRow(ctx, `SELECT coalesce(nickname, '') FROM poll.participants WHERE id=$1`, pid).Scan(&v.Nickname)
	}
	for _, q := range p.visibleQuestions(all) {
		if p.answersVisible(q, answered[q.ID], false) {
			if v.Keys == nil {
				v.Keys = map[string]Key{}
			}
			v.Keys[q.ID] = *q.Key
			if sc, ok := scores[q.ID]; ok {
				if v.Scores == nil {
					v.Scores = map[string]Score{}
				}
				v.Scores[q.ID] = sc
			}
		}
	}
	if p.Scoring && p.Leaderboard == "everyone" {
		ranks, err := s.leaderboard(ctx, p, false)
		if err != nil {
			return v, err
		}
		v.Leaderboard = top(ranks, 10)
		for i := range ranks {
			if ranks[i].Key == v.MeKey {
				me := ranks[i]
				v.Me = &me
			}
		}
	}
	for _, q := range v.Questions {
		if p.resultsVisible(q, answered[q.ID]) {
			if v.Results == nil {
				v.Results = map[string]Result{}
			}
			r, err := s.hub.result(ctx, p.ID, q, false)
			if err != nil {
				return v, err
			}
			v.Results[q.ID] = r
		}
	}
	return v, nil
}

func (s *Service) pollByID(ctx context.Context, id string) (*Poll, error) {
	p, err := s.scanPoll(s.pool.QueryRow(ctx, `SELECT `+pollCols+` FROM poll.polls p WHERE p.id=$1`, id))
	if err != nil {
		return nil, err
	}
	p.Questions, err = s.questions(ctx, id)
	return p, err
}

// publicUpdate is the state every participant socket receives on a change:
// no answers, no names, and results only where the poll allows them to be
// shared ("after answering" is applied by each browser to its own view).
func (s *Service) publicUpdate(ctx context.Context, p *Poll) (PublicPoll, error) {
	v := PublicPoll{Type: "update", ServerTime: s.now().UnixMilli(), Code: p.JoinCode, Title: p.Title, Identity: p.Identity, Audience: p.Audience,
		Pacing: p.Pacing, ShowResults: p.ShowResults, AllowEdit: p.AllowEdit, Status: p.Status, Index: p.CurrentIndex, Revealed: p.Revealed,
		Total: len(p.Questions), Questions: publicQuestions(p.visibleQuestions(p.Questions))}
	p.competition(&v)
	if p.Status == "draft" {
		v.Questions = []Question{}
		return v, nil
	}
	for _, q := range p.visibleQuestions(p.Questions) {
		if p.answersVisible(q, false, true) {
			if v.Keys == nil {
				v.Keys = map[string]Key{}
			}
			v.Keys[q.ID] = *q.Key
		}
	}
	if p.Scoring && p.Leaderboard == "everyone" {
		ranks, err := s.leaderboard(ctx, p, false)
		if err != nil {
			return v, err
		}
		v.Leaderboard = top(ranks, 10)
	}
	for _, q := range p.visibleQuestions(p.Questions) {
		if p.ShowResults == "after_answer" || p.resultsVisible(q, false) {
			r, err := s.hub.result(ctx, p.ID, q, false)
			if err != nil {
				return v, err
			}
			if v.Results == nil {
				v.Results = map[string]Result{}
			}
			v.Results[q.ID] = r
		}
	}
	return v, nil
}

func (s *Service) answersOf(ctx context.Context, participantID string) (map[string]Answer, map[string]Score, error) {
	rows, err := s.pool.Query(ctx, `SELECT question_id, value, score, coalesce(correct, false) FROM poll.responses WHERE participant_id=$1`, participantID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := map[string]Answer{}
	scores := map[string]Score{}
	for rows.Next() {
		var qid string
		var raw []byte
		var score *float64
		var correct bool
		if err := rows.Scan(&qid, &raw, &score, &correct); err != nil {
			return nil, nil, err
		}
		var a Answer
		if json.Unmarshal(raw, &a) == nil {
			out[qid] = a
		}
		if score != nil {
			scores[qid] = Score{Points: *score, Correct: correct}
		}
	}
	return out, scores, rows.Err()
}

// answerTarget resolves the poll, question and participant for an answer.
func (s *Service) answerTarget(ctx context.Context, code, questionID string, c Caller) (*Poll, Question, string, error) {
	p, err := s.pollByCode(ctx, code)
	if err != nil {
		return nil, Question{}, "", err
	}
	if p.Status != "open" {
		return nil, Question{}, "", errClosed
	}
	pid, _, err := s.findParticipant(ctx, p, c)
	if err != nil {
		return nil, Question{}, "", err
	}
	if pid == "" {
		return nil, Question{}, "", errNotJoined
	}
	all, err := s.questions(ctx, p.ID)
	if err != nil {
		return nil, Question{}, "", err
	}
	p.Questions = all
	for _, q := range p.visibleQuestions(all) {
		if q.ID == questionID {
			if p.timeUp(q, s.now()) {
				return nil, q, "", errTimeUp
			}
			if !p.AllowEdit {
				var exists bool
				if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM poll.responses WHERE participant_id=$1 AND question_id=$2)`, pid, q.ID).Scan(&exists); err != nil {
					return nil, q, "", err
				}
				if exists {
					return nil, q, "", httpx.Conflict("answers can't be changed in this poll")
				}
			}
			return p, q, pid, nil
		}
	}
	return nil, Question{}, "", httpx.NewError(http.StatusConflict, "question_not_active", "this question isn't open right now")
}

// timeUp reports whether a timed presenter-led question has closed (with a
// two-second grace for the network).
func (p *Poll) timeUp(q Question, now time.Time) bool {
	if p.Pacing != "presenter" || q.TimeLimitSec == nil || p.QuestionStartedAt == nil {
		return false
	}
	return now.After(p.QuestionStartedAt.Add(time.Duration(*q.TimeLimitSec)*time.Second + 2*time.Second))
}

var errTimeUp = httpx.NewError(http.StatusConflict, "time_up", "time is up for this question")

var errRevealed = httpx.NewError(http.StatusConflict, "answer_revealed", "the correct answer has been shown, so answers can't change")

// elapsedMs is how long after the question appeared the answer came, or -1
// when that isn't known (self-paced polls).
func (p *Poll) elapsedMs(q Question, now time.Time) int {
	if p.Pacing != "presenter" || p.QuestionStartedAt == nil || len(p.Questions) <= p.CurrentIndex || p.Questions[p.CurrentIndex].ID != q.ID {
		return -1
	}
	return int(now.Sub(*p.QuestionStartedAt).Milliseconds())
}

// score returns the points and correctness of a for q, or nil when the poll
// isn't scored or q has no key.
func (p *Poll) score(q Question, a Answer, elapsed int) (*float64, *bool) {
	if !p.Scoring || q.Key == nil {
		return nil, nil
	}
	f := Fraction(q, q.Key, a)
	limit := 0
	if q.TimeLimitSec != nil {
		limit = *q.TimeLimitSec
	}
	pts := Points(q.Points, f, p.SpeedBonus && elapsed >= 0, elapsed, limit)
	ok := f == 1
	return &pts, &ok
}

// AnswerResult is the saved answer, plus its key and score when the poll
// shows answers right after answering.
type AnswerResult struct {
	Value Answer `json:"value"`
	Key   *Key   `json:"key,omitempty"`
	Score *Score `json:"score,omitempty"`
}

// Answer saves (or with an empty answer, clears) the caller's answer.
func (s *Service) Answer(ctx context.Context, code, questionID string, c Caller, a Answer) (AnswerResult, error) {
	res, err := s.answer(ctx, code, questionID, c, a)
	return res, err
}

func (s *Service) answer(ctx context.Context, code, questionID string, c Caller, a Answer) (AnswerResult, error) {
	p, q, pid, err := s.answerTarget(ctx, code, questionID, c)
	if err != nil {
		return AnswerResult{Value: a}, err
	}
	res := AnswerResult{Value: a}
	if !s.answers.Allow(pid) {
		return res, httpx.NewError(http.StatusTooManyRequests, "rate_limited", "slow down")
	}
	empty, err := q.CheckAnswer(&a)
	if err != nil {
		return res, httpx.Invalid(map[string]string{"value": err.Error()})
	}
	res.Value = a
	if p.Scoring && q.Key != nil {
		// Once the correct answer is out, answers can't change (D-42).
		if p.answersVisible(q, false, true) {
			return res, errRevealed
		}
		if p.ShowAnswers == "after_answer" {
			var n int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.responses WHERE participant_id=$1 AND question_id=$2`, pid, q.ID).Scan(&n); err != nil {
				return res, err
			}
			if n > 0 {
				return res, errRevealed
			}
		}
	}
	if empty {
		if _, err := s.pool.Exec(ctx, `DELETE FROM poll.responses WHERE participant_id=$1 AND question_id=$2`, pid, q.ID); err != nil {
			return res, err
		}
		if q.Type.IsMedia() {
			if _, err := s.pool.Exec(ctx, `DELETE FROM poll.files WHERE participant_id=$1 AND question_id=$2`, pid, q.ID); err != nil {
				return res, err
			}
		}
	} else {
		now := s.now()
		elapsed := p.elapsedMs(q, now)
		pts, ok := p.score(q, a, elapsed)
		raw, _ := json.Marshal(a)
		var el any
		if elapsed >= 0 {
			el = elapsed
		}
		if _, err := s.pool.Exec(ctx, `INSERT INTO poll.responses (participant_id, question_id, poll_id, value, updated_at, score, correct, elapsed_ms)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (participant_id, question_id) DO UPDATE SET value=EXCLUDED.value, updated_at=EXCLUDED.updated_at,
				score=EXCLUDED.score, correct=EXCLUDED.correct, elapsed_ms=EXCLUDED.elapsed_ms`,
			pid, q.ID, p.ID, raw, now, pts, ok, el); err != nil {
			return res, err
		}
		if p.ShowAnswers == "after_answer" && q.Key != nil {
			k := *q.Key
			res.Key = &k
			if pts != nil {
				res.Score = &Score{Points: *pts, Correct: *ok}
			}
		}
	}
	s.hub.markDirty(p.ID, q.ID)
	return res, nil
}

// rescore recomputes stored scores for q after its key, points or the poll's
// scoring settings change. The speed bonus uses each answer's recorded time.
func (s *Service) rescore(ctx context.Context, p *Poll, q Question) error {
	rows, err := s.pool.Query(ctx, `SELECT participant_id, value, coalesce(elapsed_ms, -1) FROM poll.responses WHERE question_id=$1`, q.ID)
	if err != nil {
		return err
	}
	type row struct {
		pid     string
		a       Answer
		elapsed int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		var raw []byte
		err := r.Scan(&x.pid, &raw, &x.elapsed)
		if err == nil {
			err = json.Unmarshal(raw, &x.a)
		}
		return x, err
	})
	if err != nil {
		return err
	}
	for _, x := range list {
		pts, ok := p.score(q, x.a, x.elapsed)
		if _, err := s.pool.Exec(ctx, `UPDATE poll.responses SET score=$3, correct=$4 WHERE participant_id=$1 AND question_id=$2`, x.pid, q.ID, pts, ok); err != nil {
			return err
		}
	}
	s.hub.markDirty(p.ID, q.ID)
	return nil
}

func (s *Service) rescorePoll(ctx context.Context, pollID string) error {
	p, err := s.pollByID(ctx, pollID)
	if err != nil {
		return err
	}
	for _, q := range p.Questions {
		if err := s.rescore(ctx, p, q); err != nil {
			return err
		}
	}
	return nil
}

// ---------- results (teacher) ----------

type TeacherResults struct {
	Type         string            `json:"type"` // "results" over the socket
	ServerTime   int64             `json:"server_time"`
	Poll         *Poll             `json:"poll"`
	Participants int               `json:"participants"`
	Results      map[string]Result `json:"results"`
	Leaderboard  []Rank            `json:"leaderboard,omitempty"` // scored polls: everyone, with names and nicknames
}

func (s *Service) teacherResults(ctx context.Context, p *Poll) (TeacherResults, error) {
	out := TeacherResults{Type: "results", ServerTime: s.now().UnixMilli(), Poll: p, Participants: p.Participants, Results: map[string]Result{}}
	if p.Scoring {
		ranks, err := s.leaderboard(ctx, p, true)
		if err != nil {
			return out, err
		}
		out.Leaderboard = ranks
	}
	for _, q := range p.Questions {
		r, err := s.hub.result(ctx, p.ID, q, true)
		if err != nil {
			return out, err
		}
		out.Results[q.ID] = r
	}
	return out, nil
}

func (s *Service) Results(ctx context.Context, teacherID, id string) (TeacherResults, error) {
	p, err := s.GetPoll(ctx, teacherID, id)
	if err != nil {
		return TeacherResults{}, err
	}
	return s.teacherResults(ctx, p)
}

// compute aggregates one question from the database.
func (s *Service) compute(ctx context.Context, q Question, teacher bool) (Result, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.participant_id, p.user_id, r.value, r.hidden, r.updated_at
		FROM poll.responses r JOIN poll.participants p ON p.id = r.participant_id WHERE r.question_id=$1`, q.ID)
	if err != nil {
		return Result{}, err
	}
	type row struct {
		e      Entry
		userID *string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		var raw []byte
		err := r.Scan(&x.e.ParticipantID, &x.userID, &raw, &x.e.Hidden, &x.e.At)
		if err == nil {
			err = json.Unmarshal(raw, &x.e.Value)
		}
		return x, err
	})
	if err != nil {
		return Result{}, err
	}
	names := map[string]auth.User{}
	if teacher {
		var ids []string
		for _, x := range list {
			if x.userID != nil {
				ids = append(ids, *x.userID)
			}
		}
		if len(ids) > 0 {
			if names, err = s.users.UsersByID(ctx, ids); err != nil {
				return Result{}, err
			}
		}
	}
	entries := make([]Entry, len(list))
	for i, x := range list {
		entries[i] = x.e
		if x.userID != nil {
			entries[i].Name = names[*x.userID].Name
		}
	}
	hidden := map[string]bool{}
	hrows, err := s.pool.Query(ctx, `SELECT word FROM poll.hidden_words WHERE question_id=$1`, q.ID)
	if err != nil {
		return Result{}, err
	}
	words, err := pgx.CollectRows(hrows, pgx.RowTo[string])
	if err != nil {
		return Result{}, err
	}
	for _, w := range words {
		hidden[w] = true
	}
	return Aggregate(q, entries, hidden, teacher), nil
}

// ---------- moderation ----------

type ModerationInput struct {
	QuestionID    string `json:"question_id"`
	ParticipantID string `json:"participant_id,omitempty"` // hide one answer
	Word          string `json:"word,omitempty"`           // or hide a word from the cloud
	Hidden        bool   `json:"hidden"`
	// With participant_id and no question_id: replace that participant's
	// nickname ("" falls back to "Participant N").
	Nickname *string `json:"nickname,omitempty"`
}

func (s *Service) Moderate(ctx context.Context, teacherID, pollID string, in ModerationInput) error {
	if in.Nickname != nil && in.QuestionID == "" {
		if _, err := s.ownedPoll(ctx, teacherID, pollID); err != nil {
			return err
		}
		var nick any
		if n := CleanNickname(*in.Nickname); n != "" {
			nick = n
		}
		tag, err := s.pool.Exec(ctx, `UPDATE poll.participants SET nickname=$3 WHERE id=$1 AND poll_id=$2`, in.ParticipantID, pollID, nick)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		s.hub.markAll(pollID)
		return nil
	}
	q, err := s.ownedQuestion(ctx, teacherID, in.QuestionID)
	if err != nil {
		return err
	}
	if q.PollID != pollID {
		return httpx.ErrNotFound
	}
	switch {
	case in.ParticipantID != "":
		_, err = s.pool.Exec(ctx, `UPDATE poll.responses SET hidden=$3 WHERE question_id=$1 AND participant_id=$2`, q.ID, in.ParticipantID, in.Hidden)
	case in.Word != "":
		w := NormaliseWord(in.Word)
		if in.Hidden {
			_, err = s.pool.Exec(ctx, `INSERT INTO poll.hidden_words (question_id, word) VALUES ($1,$2) ON CONFLICT DO NOTHING`, q.ID, w)
		} else {
			_, err = s.pool.Exec(ctx, `DELETE FROM poll.hidden_words WHERE question_id=$1 AND word=$2`, q.ID, w)
		}
	default:
		return httpx.BadRequest("give participant_id or word")
	}
	if err != nil {
		return err
	}
	s.hub.markDirty(pollID, q.ID)
	return nil
}
