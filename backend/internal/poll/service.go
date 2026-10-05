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
}

type Poll struct {
	ID          string  `json:"id"`
	TeacherID   string  `json:"teacher_id"`
	ClassroomID *string `json:"classroom_id"`
	Title       string  `json:"title"`
	JoinCode    string  `json:"join_code"`
	JoinURL     string  `json:"join_url"`
	Settings
	Status       string     `json:"status"`
	CurrentIndex int        `json:"current_index"`
	Revealed     bool       `json:"revealed"`
	CreatedAt    time.Time  `json:"created_at"`
	OpenedAt     *time.Time `json:"opened_at"`
	ClosedAt     *time.Time `json:"closed_at"`
	Questions    []Question `json:"questions,omitempty"`
	Participants int        `json:"participants"`
}

type PollInput struct {
	Title       *string   `json:"title"`
	ClassroomID *string   `json:"classroom_id"`
	Settings    *Settings `json:"settings"`
}

const pollCols = `p.id, p.teacher_id, p.classroom_id, p.title, p.join_code, p.identity, p.audience, p.pacing, p.show_results,
	p.allow_edit, p.status, p.current_index, p.revealed, p.created_at, p.opened_at, p.closed_at,
	(SELECT count(*) FROM poll.participants x WHERE x.poll_id = p.id)`

func (s *Service) scanPoll(row pgx.Row) (*Poll, error) {
	var p Poll
	err := row.Scan(&p.ID, &p.TeacherID, &p.ClassroomID, &p.Title, &p.JoinCode, &p.Identity, &p.Audience, &p.Pacing,
		&p.ShowResults, &p.AllowEdit, &p.Status, &p.CurrentIndex, &p.Revealed, &p.CreatedAt, &p.OpenedAt, &p.ClosedAt, &p.Participants)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.JoinURL = s.baseURL + "/p/" + p.JoinCode
	return &p, nil
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
	return f
}

// DefaultSettings: anonymous, open to anyone with the code, self-paced.
func DefaultSettings() Settings {
	return Settings{Identity: "anonymous", Audience: "anyone", Pacing: "self", ShowResults: "after_answer", AllowEdit: true}
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
		err := s.pool.QueryRow(ctx, `INSERT INTO poll.polls (teacher_id, classroom_id, title, join_code, identity, audience, pacing, show_results, allow_edit)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
			teacherID, in.ClassroomID, title, "P"+newCode(5), st.Identity, st.Audience, st.Pacing, st.ShowResults, st.AllowEdit).Scan(&id)
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
		allow_edit=$8, updated_at=now() WHERE id=$1`, id, title, classroom, st.Identity, st.Audience, st.Pacing, st.ShowResults, st.AllowEdit)
	if err != nil {
		return nil, err
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

// Present moves a presenter-paced poll to question index and hides results.
func (s *Service) Present(ctx context.Context, teacherID, id string, index int, revealed bool) (*Poll, error) {
	p, err := s.GetPoll(ctx, teacherID, id)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(p.Questions) {
		return nil, httpx.Invalid(map[string]string{"index": "no such question"})
	}
	if _, err := s.pool.Exec(ctx, `UPDATE poll.polls SET current_index=$2, revealed=$3, updated_at=now() WHERE id=$1`, id, index, revealed); err != nil {
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
		`UPDATE poll.polls SET current_index=0, revealed=false, updated_at=now() WHERE id=$1`} {
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

const questionCols = `id, poll_id, position, type, text, body, required`

func scanQuestion(r pgx.Row) (Question, error) {
	var q Question
	var body []byte
	if err := r.Scan(&q.ID, &q.PollID, &q.Position, &q.Type, &q.Text, &body, &q.Required); err != nil {
		return q, err
	}
	return q, json.Unmarshal(body, &q.Body)
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
	Type     Type   `json:"type"`
	Text     string `json:"text"`
	Body     Body   `json:"body"`
	Required bool   `json:"required"`
}

func (in QuestionInput) question() (Question, error) {
	q := Question{Type: in.Type, Text: in.Text, Body: in.Body, Required: in.Required}
	q.Normalise()
	if f := q.Validate(); len(f) > 0 {
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
	q, err = scanQuestion(s.pool.QueryRow(ctx, `INSERT INTO poll.questions (poll_id, position, type, text, body, required)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+questionCols, pollID, n, q.Type, q.Text, body, q.Required))
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
	q, err = scanQuestion(s.pool.QueryRow(ctx, `UPDATE poll.questions SET type=$2, text=$3, body=$4, required=$5 WHERE id=$1 RETURNING `+questionCols,
		questionID, q.Type, q.Text, body, q.Required))
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
	// Joining again returns the same participant, so nobody is counted twice.
	if id, identified, err := s.findParticipant(ctx, p, c); err != nil || id != "" {
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
		err := s.pool.QueryRow(ctx, `INSERT INTO poll.participants (poll_id, user_id) VALUES ($1,$2)
			ON CONFLICT (poll_id, user_id) DO UPDATE SET user_id=EXCLUDED.user_id RETURNING id`, p.ID, c.User.ID).Scan(&id)
		s.hub.markAll(p.ID)
		return JoinResult{ParticipantID: id, Identified: true}, err
	}
	tok := newToken()
	var id string
	err = s.pool.QueryRow(ctx, `INSERT INTO poll.participants (poll_id, token_hash) VALUES ($1,$2) RETURNING id`, p.ID, hashToken(tok)).Scan(&id)
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
		Total: len(all), Questions: p.visibleQuestions(all)}
	pid, identified, err := s.findParticipant(ctx, p, c)
	if err != nil {
		return v, err
	}
	v.Joined, v.Identified = pid != "", identified
	answered := map[string]bool{}
	if pid != "" {
		v.Answers, err = s.answersOf(ctx, pid)
		if err != nil {
			return v, err
		}
		for k := range v.Answers {
			answered[k] = true
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
		Total: len(p.Questions), Questions: p.visibleQuestions(p.Questions)}
	if p.Status == "draft" {
		v.Questions = []Question{}
		return v, nil
	}
	for _, q := range v.Questions {
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

func (s *Service) answersOf(ctx context.Context, participantID string) (map[string]Answer, error) {
	rows, err := s.pool.Query(ctx, `SELECT question_id, value FROM poll.responses WHERE participant_id=$1`, participantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Answer{}
	for rows.Next() {
		var qid string
		var raw []byte
		if err := rows.Scan(&qid, &raw); err != nil {
			return nil, err
		}
		var a Answer
		if json.Unmarshal(raw, &a) == nil {
			out[qid] = a
		}
	}
	return out, rows.Err()
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

// Answer saves (or with an empty answer, clears) the caller's answer.
func (s *Service) Answer(ctx context.Context, code, questionID string, c Caller, a Answer) (Answer, error) {
	p, q, pid, err := s.answerTarget(ctx, code, questionID, c)
	if err != nil {
		return a, err
	}
	if !s.answers.Allow(pid) {
		return a, httpx.NewError(http.StatusTooManyRequests, "rate_limited", "slow down")
	}
	empty, err := q.CheckAnswer(&a)
	if err != nil {
		return a, httpx.Invalid(map[string]string{"value": err.Error()})
	}
	if empty {
		if _, err := s.pool.Exec(ctx, `DELETE FROM poll.responses WHERE participant_id=$1 AND question_id=$2`, pid, q.ID); err != nil {
			return a, err
		}
		if q.Type.IsMedia() {
			if _, err := s.pool.Exec(ctx, `DELETE FROM poll.files WHERE participant_id=$1 AND question_id=$2`, pid, q.ID); err != nil {
				return a, err
			}
		}
	} else {
		raw, _ := json.Marshal(a)
		if _, err := s.pool.Exec(ctx, `INSERT INTO poll.responses (participant_id, question_id, poll_id, value, updated_at) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (participant_id, question_id) DO UPDATE SET value=EXCLUDED.value, updated_at=EXCLUDED.updated_at`,
			pid, q.ID, p.ID, raw, s.now()); err != nil {
			return a, err
		}
	}
	s.hub.markDirty(p.ID, q.ID)
	return a, nil
}

// ---------- results (teacher) ----------

type TeacherResults struct {
	Type         string            `json:"type"` // "results" over the socket
	ServerTime   int64             `json:"server_time"`
	Poll         *Poll             `json:"poll"`
	Participants int               `json:"participants"`
	Results      map[string]Result `json:"results"`
}

func (s *Service) teacherResults(ctx context.Context, p *Poll) (TeacherResults, error) {
	out := TeacherResults{Type: "results", ServerTime: s.now().UnixMilli(), Poll: p, Participants: p.Participants, Results: map[string]Result{}}
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
}

func (s *Service) Moderate(ctx context.Context, teacherID, pollID string, in ModerationInput) error {
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
