package quiz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/imageurl"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/platform/page"
	"github.com/nadun96/quizplatform/internal/settings"
)

const (
	StatusDraft    = "draft"
	StatusReady    = "ready"
	StatusArchived = "archived"
)

type Quiz struct {
	ID               string              `json:"id"`
	TopicID          string              `json:"topic_id"`
	TeacherID        string              `json:"teacher_id"`
	Title            string              `json:"title"`
	Description      string              `json:"description"`
	Status           string              `json:"status"`
	WarningsAccepted bool                `json:"warnings_accepted"`
	Settings         settings.Overrides  `json:"settings"`
	Effective        *settings.Effective `json:"effective,omitempty"`
	QuestionCount    int                 `json:"question_count"`
	TotalMarks       float64             `json:"total_marks"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

// Topics is the slice of the content module the quiz module needs.
type Topics interface {
	TopicContext(ctx context.Context, teacherID, topicID string) (content.TopicContext, error)
}

// SessionGuard reports whether a quiz has session results (BR-16).
type SessionGuard func(ctx context.Context, quizID string) (bool, error)

type Service struct {
	pool     *pgxpool.Pool
	topics   Topics
	settings *settings.Store
	jobs     jobs.Inserter
	checker  *imageurl.Checker
	guard    SessionGuard
}

func NewService(pool *pgxpool.Pool, topics Topics, st *settings.Store, inserter jobs.Inserter, checker *imageurl.Checker) *Service {
	return &Service{pool: pool, topics: topics, settings: st, jobs: inserter, checker: checker}
}

func (s *Service) SetSessionGuard(g SessionGuard) { s.guard = g }

var errNotReady = func(issues []string) error {
	return &httpx.Error{Status: http.StatusConflict, Code: "quiz_not_ready", Message: strings.Join(issues, "; ")}
}

// ---------- quizzes ----------

type QuizInput struct {
	Title       *string             `json:"title"`
	Description *string             `json:"description"`
	Settings    *settings.Overrides `json:"settings"`
}

func (in QuizInput) validate(creating bool) error {
	f := map[string]string{}
	if in.Title != nil || creating {
		t := ""
		if in.Title != nil {
			t = strings.TrimSpace(*in.Title)
		}
		if n := len([]rune(t)); n < 1 || n > 200 {
			f["title"] = "title must be 1-200 characters"
		}
	}
	if in.Settings != nil {
		if err := in.Settings.Validate(settings.LevelQuiz); err != nil {
			var ve *settings.ValidationError
			if asValidation(err, &ve) {
				for k, v := range ve.Fields {
					f["settings."+k] = v
				}
			}
		}
	}
	if len(f) > 0 {
		return httpx.Invalid(f)
	}
	return nil
}

func (s *Service) CreateQuiz(ctx context.Context, teacherID, topicID string, in QuizInput) (Quiz, error) {
	if err := in.validate(true); err != nil {
		return Quiz{}, err
	}
	tc, err := s.topics.TopicContext(ctx, teacherID, topicID)
	if err != nil {
		return Quiz{}, err
	}
	if tc.ClassroomArchived {
		return Quiz{}, httpx.Conflict("the classroom is archived")
	}
	var o settings.Overrides
	if in.Settings != nil {
		o = *in.Settings
	}
	desc := ""
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	var id string
	if err := s.pool.QueryRow(ctx, `INSERT INTO quiz.quizzes(topic_id, teacher_id, title, description, settings)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`, topicID, teacherID, strings.TrimSpace(*in.Title), desc, o.JSON()).Scan(&id); err != nil {
		return Quiz{}, err
	}
	return s.GetQuiz(ctx, teacherID, id)
}

const quizCols = `q.id, q.topic_id, q.teacher_id, q.title, q.description, q.status, q.warnings_accepted, q.settings, q.created_at, q.updated_at,
	(SELECT count(*) FROM quiz.questions x WHERE x.quiz_id=q.id), (SELECT coalesce(sum(marks),0) FROM quiz.questions x WHERE x.quiz_id=q.id)`

func scanQuiz(r pgx.Row) (Quiz, error) {
	var q Quiz
	var raw []byte
	err := r.Scan(&q.ID, &q.TopicID, &q.TeacherID, &q.Title, &q.Description, &q.Status, &q.WarningsAccepted, &raw,
		&q.CreatedAt, &q.UpdatedAt, &q.QuestionCount, &q.TotalMarks)
	if errors.Is(err, pgx.ErrNoRows) {
		return q, httpx.ErrNotFound
	}
	if err != nil {
		return q, err
	}
	q.Settings, err = settings.Parse(raw)
	return q, err
}

// GetQuiz returns a quiz owned by teacherID with its effective settings
// (platform → teacher → classroom → module → topic → quiz).
func (s *Service) GetQuiz(ctx context.Context, teacherID, id string) (Quiz, error) {
	q, err := scanQuiz(s.pool.QueryRow(ctx, `SELECT `+quizCols+` FROM quiz.quizzes q WHERE q.id=$1 AND q.teacher_id=$2`, id, teacherID))
	if err != nil {
		return q, err
	}
	eff, err := s.effective(ctx, q)
	if err != nil {
		return q, err
	}
	q.Effective = &eff
	return q, nil
}

// QuizLayers returns the settings layers down to quiz level, in merge order,
// on top of the teacher base.
func (s *Service) QuizLayers(ctx context.Context, q Quiz) (settings.Effective, []settings.Overrides, error) {
	tc, err := s.topics.TopicContext(ctx, q.TeacherID, q.TopicID)
	if err != nil {
		return settings.Effective{}, nil, err
	}
	base, err := s.settings.Base(ctx, q.TeacherID)
	if err != nil {
		return settings.Effective{}, nil, err
	}
	return base, append(tc.Layers(), q.Settings), nil
}

func (s *Service) effective(ctx context.Context, q Quiz) (settings.Effective, error) {
	base, layers, err := s.QuizLayers(ctx, q)
	if err != nil {
		return settings.Effective{}, err
	}
	return settings.Resolve(base, layers...), nil
}

// QuizSorts are the orders of a topic's quiz list (PL-FR-01).
var QuizSorts = page.Sorts{"created": "q.created_at", "title": "lower(q.title)", "status": "q.status"}

// ListQuizzes returns one page of a topic's quizzes, searched by title and
// filtered by status, and how many there are.
func (s *Service) ListQuizzes(ctx context.Context, teacherID, topicID, status string, p page.Request) ([]Quiz, int, error) {
	if _, err := s.topics.TopicContext(ctx, teacherID, topicID); err != nil {
		return nil, 0, err
	}
	where := ` FROM quiz.quizzes q WHERE q.topic_id=$1 AND q.teacher_id=$2 AND ($3 = '' OR q.title ILIKE $3) AND ($4 = '' OR q.status = $4)`
	args := []any{topicID, teacherID, p.Like(), status}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+quizCols+where+p.OrderBy(QuizSorts, "q.id")+p.Limit(), args...)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Quiz, error) { return scanQuiz(r) })
	return list, total, err
}

func (s *Service) UpdateQuiz(ctx context.Context, teacherID, id string, in QuizInput) (Quiz, error) {
	if err := in.validate(false); err != nil {
		return Quiz{}, err
	}
	var title, desc any
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	var raw []byte
	if in.Settings != nil {
		raw = in.Settings.JSON()
	}
	tag, err := s.pool.Exec(ctx, `UPDATE quiz.quizzes SET title=coalesce($3,title), description=coalesce($4,description),
		settings=coalesce($5,settings), updated_at=now() WHERE id=$1 AND teacher_id=$2`, id, teacherID, title, desc, raw)
	if err != nil {
		return Quiz{}, err
	}
	if tag.RowsAffected() == 0 {
		return Quiz{}, httpx.ErrNotFound
	}
	return s.GetQuiz(ctx, teacherID, id)
}

// DeleteQuiz deletes a quiz, or archives it when sessions have results (BR-16).
// It reports whether the quiz was archived instead.
func (s *Service) DeleteQuiz(ctx context.Context, teacherID, id string) (archived bool, err error) {
	if _, err := s.GetQuiz(ctx, teacherID, id); err != nil {
		return false, err
	}
	if s.guard != nil {
		has, err := s.guard(ctx, id)
		if err != nil {
			return false, err
		}
		if has {
			_, err = s.pool.Exec(ctx, `UPDATE quiz.quizzes SET status='archived', updated_at=now() WHERE id=$1`, id)
			return true, err
		}
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM quiz.quizzes WHERE id=$1 AND teacher_id=$2`, id, teacherID)
	return false, err
}

// Readiness lists what stops a quiz from being started. Broken resources are
// warnings that the teacher may accept (UC-01 6a).
func (s *Service) Readiness(ctx context.Context, teacherID, id string) (blocking, warnings []string, err error) {
	qs, err := s.ListQuestions(ctx, teacherID, id)
	if err != nil {
		return nil, nil, err
	}
	if len(qs) == 0 {
		blocking = append(blocking, "add at least one question")
	}
	for _, q := range qs {
		if errs := q.Validate(); len(errs) > 0 {
			blocking = append(blocking, fmt.Sprintf("question %s is invalid", q.Code))
		}
		for _, r := range q.Resources {
			if r.Status == "broken" {
				warnings = append(warnings, fmt.Sprintf("question %s: resource %s_%d is broken (%s)", q.Code, r.Role, r.N, r.Message))
			}
		}
	}
	return blocking, warnings, nil
}

// SetStatus moves a quiz between draft and ready.
func (s *Service) SetStatus(ctx context.Context, teacherID, id, status string, acceptWarnings bool) (Quiz, error) {
	switch status {
	case StatusReady:
		blocking, warnings, err := s.Readiness(ctx, teacherID, id)
		if err != nil {
			return Quiz{}, err
		}
		if len(blocking) > 0 {
			return Quiz{}, errNotReady(blocking)
		}
		if len(warnings) > 0 && !acceptWarnings {
			return Quiz{}, &httpx.Error{Status: http.StatusConflict, Code: "quiz_has_warnings", Message: strings.Join(warnings, "; ")}
		}
	case StatusDraft:
	default:
		return Quiz{}, httpx.BadRequest("status must be draft or ready")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE quiz.quizzes SET status=$3, warnings_accepted=$4, updated_at=now() WHERE id=$1 AND teacher_id=$2`,
		id, teacherID, status, acceptWarnings && status == StatusReady)
	if err != nil {
		return Quiz{}, err
	}
	if tag.RowsAffected() == 0 {
		return Quiz{}, httpx.ErrNotFound
	}
	return s.GetQuiz(ctx, teacherID, id)
}

// ---------- questions ----------

const questionCols = `id, quiz_id, code, position, type, text, body, answer_key, feedback, marks, negative_marks, partial_credit, settings`

func scanQuestion(r pgx.Row) (Question, error) {
	var q Question
	var body, key, fb, st []byte
	err := r.Scan(&q.ID, &q.QuizID, &q.Code, &q.Position, &q.Type, &q.Text, &body, &key, &fb, &q.Marks, &q.NegativeMarks, &q.PartialCredit, &st)
	if errors.Is(err, pgx.ErrNoRows) {
		return q, httpx.ErrNotFound
	}
	if err != nil {
		return q, err
	}
	if err := json.Unmarshal(body, &q.Body); err != nil {
		return q, err
	}
	if err := json.Unmarshal(key, &q.Key); err != nil {
		return q, err
	}
	if err := json.Unmarshal(fb, &q.Feedback); err != nil {
		return q, err
	}
	q.Settings, err = settings.Parse(st)
	q.Resources = []Resource{}
	return q, err
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ListQuestions returns a quiz's questions in order, with resources.
func (s *Service) ListQuestions(ctx context.Context, teacherID, quizID string) ([]Question, error) {
	if _, err := s.ownedQuiz(ctx, s.pool, teacherID, quizID); err != nil {
		return nil, err
	}
	return s.loadQuestions(ctx, quizID)
}

func (s *Service) loadQuestions(ctx context.Context, quizID string) ([]Question, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+questionCols+` FROM quiz.questions WHERE quiz_id=$1 ORDER BY position, created_at`, quizID)
	if err != nil {
		return nil, err
	}
	qs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Question, error) { return scanQuestion(r) })
	if err != nil || len(qs) == 0 {
		return qs, err
	}
	idx := map[string]int{}
	for i, q := range qs {
		idx[q.ID] = i
	}
	rrows, err := s.pool.Query(ctx, `SELECT r.id, r.question_id, r.role, r.n, r.source_url, r.url, r.alt_text, r.status, r.message, r.checked_at
		FROM quiz.resources r JOIN quiz.questions q ON q.id=r.question_id WHERE q.quiz_id=$1 ORDER BY r.role, r.n`, quizID)
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var r Resource
		if err := rrows.Scan(&r.ID, &r.QuestionID, &r.Role, &r.N, &r.SourceURL, &r.URL, &r.AltText, &r.Status, &r.Message, &r.CheckedAt); err != nil {
			return nil, err
		}
		i := idx[r.QuestionID]
		qs[i].Resources = append(qs[i].Resources, r)
	}
	return qs, rrows.Err()
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ownedQuiz checks ownership and returns the quiz status.
func (s *Service) ownedQuiz(ctx context.Context, db querier, teacherID, quizID string) (string, error) {
	var status string
	err := db.QueryRow(ctx, `SELECT status FROM quiz.quizzes WHERE id=$1 AND teacher_id=$2`, quizID, teacherID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound
	}
	return status, err
}

func (s *Service) GetQuestion(ctx context.Context, teacherID, id string) (Question, error) {
	var quizID string
	err := s.pool.QueryRow(ctx, `SELECT q.quiz_id FROM quiz.questions q JOIN quiz.quizzes z ON z.id=q.quiz_id
		WHERE q.id=$1 AND z.teacher_id=$2`, id, teacherID).Scan(&quizID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Question{}, httpx.ErrNotFound
	}
	if err != nil {
		return Question{}, err
	}
	qs, err := s.loadQuestions(ctx, quizID)
	if err != nil {
		return Question{}, err
	}
	for _, q := range qs {
		if q.ID == id {
			return q, nil
		}
	}
	return Question{}, httpx.ErrNotFound
}

func validated(q *Question) error {
	q.Normalise()
	if f := q.Validate(); len(f) > 0 {
		return httpx.Invalid(f)
	}
	return nil
}

func insertQuestion(ctx context.Context, tx pgx.Tx, quizID string, q *Question) error {
	err := tx.QueryRow(ctx, `INSERT INTO quiz.questions(quiz_id, code, position, type, text, body, answer_key, feedback, marks, negative_marks, partial_credit, settings)
		VALUES ($1,$2,(SELECT coalesce(max(position)+1,0) FROM quiz.questions WHERE quiz_id=$1),$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, position`, quizID, q.Code, q.Type, q.Text, mustJSON(q.Body), mustJSON(q.Key), mustJSON(q.Feedback),
		q.Marks, q.NegativeMarks, q.PartialCredit, q.Settings.JSON()).Scan(&q.ID, &q.Position)
	if isUnique(err) {
		return httpx.Invalid(map[string]string{"code": "question code " + q.Code + " already exists in this quiz"})
	}
	q.QuizID = quizID
	return err
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// CreateQuestion adds one question (FR-QZ-04).
func (s *Service) CreateQuestion(ctx context.Context, teacherID, quizID string, q Question) (Question, error) {
	if err := validated(&q); err != nil {
		return q, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		if err := insertQuestion(ctx, tx, quizID, &q); err != nil {
			return err
		}
		return touchQuiz(ctx, tx, quizID)
	})
	q.Resources = []Resource{}
	return q, err
}

func touchQuiz(ctx context.Context, tx pgx.Tx, quizID string) error {
	_, err := tx.Exec(ctx, `UPDATE quiz.quizzes SET updated_at=now() WHERE id=$1`, quizID)
	return err
}

// UpdateQuestion replaces a question's content; resources are kept.
func (s *Service) UpdateQuestion(ctx context.Context, teacherID, id string, q Question) (Question, error) {
	cur, err := s.GetQuestion(ctx, teacherID, id)
	if err != nil {
		return q, err
	}
	if err := validated(&q); err != nil {
		return q, err
	}
	_, err = s.pool.Exec(ctx, `UPDATE quiz.questions SET code=$2, type=$3, text=$4, body=$5, answer_key=$6, feedback=$7,
		marks=$8, negative_marks=$9, partial_credit=$10, settings=$11, updated_at=now() WHERE id=$1`,
		id, q.Code, q.Type, q.Text, mustJSON(q.Body), mustJSON(q.Key), mustJSON(q.Feedback), q.Marks, q.NegativeMarks, q.PartialCredit, q.Settings.JSON())
	if isUnique(err) {
		return q, httpx.Invalid(map[string]string{"code": "question code " + q.Code + " already exists in this quiz"})
	}
	if err != nil {
		return q, err
	}
	return s.GetQuestion(ctx, teacherID, cur.ID)
}

func (s *Service) DeleteQuestion(ctx context.Context, teacherID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM quiz.questions q USING quiz.quizzes z WHERE z.id=q.quiz_id AND q.id=$1 AND z.teacher_id=$2`, id, teacherID)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return err
}

// DuplicateQuestion copies a question (with resources) to the end of its quiz.
func (s *Service) DuplicateQuestion(ctx context.Context, teacherID, id string) (Question, error) {
	q, err := s.GetQuestion(ctx, teacherID, id)
	if err != nil {
		return q, err
	}
	src := q
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for i := 1; ; i++ {
			suffix := "-copy"
			if i > 1 {
				suffix = fmt.Sprintf("-copy%d", i)
			}
			base := src.Code
			if len(base)+len(suffix) > 32 {
				base = base[:32-len(suffix)]
			}
			q.Code = base + suffix
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quiz.questions WHERE quiz_id=$1 AND code=$2)`, src.QuizID, q.Code).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				break
			}
		}
		if err := insertQuestion(ctx, tx, src.QuizID, &q); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO quiz.resources(question_id, role, n, source_url, url, alt_text, status, message, checked_at)
			SELECT $2, role, n, source_url, url, alt_text, status, message, checked_at FROM quiz.resources WHERE question_id=$1`, src.ID, q.ID)
		return err
	})
	if err != nil {
		return q, err
	}
	return s.GetQuestion(ctx, teacherID, q.ID)
}

// Reorder sets question order; ids must list every question exactly once.
func (s *Service) Reorder(ctx context.Context, teacherID, quizID string, ids []string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM quiz.questions WHERE quiz_id=$1`, quizID).Scan(&n); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		if len(ids) != n || len(seen) != n {
			return httpx.BadRequest("list every question of the quiz exactly once")
		}
		for i, id := range ids {
			tag, err := tx.Exec(ctx, `UPDATE quiz.questions SET position=$3 WHERE id=$1 AND quiz_id=$2`, id, quizID, i)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return httpx.BadRequest("question " + id + " is not in this quiz")
			}
		}
		return touchQuiz(ctx, tx, quizID)
	})
}

// Preview returns the quiz as a student would see it (FR-QZ-10).
func (s *Service) Preview(ctx context.Context, teacherID, quizID string) ([]StudentQuestion, error) {
	qs, err := s.ListQuestions(ctx, teacherID, quizID)
	if err != nil {
		return nil, err
	}
	out := make([]StudentQuestion, len(qs))
	for i, q := range qs {
		out[i] = q.StudentView()
	}
	return out, nil
}

// ---------- CSV import ----------

type ImportReport struct {
	Imported int        `json:"imported"`
	Rejected []RowError `json:"rejected"`
}

// ImportQuestions imports valid rows and reports invalid ones (FR-QZ-05, AC-09).
func (s *Service) ImportQuestions(ctx context.Context, teacherID, quizID string, data []byte) (ImportReport, error) {
	rep := ImportReport{Rejected: []RowError{}}
	rows, err := ParseQuestionsCSV(data)
	if err != nil {
		return rep, httpx.BadRequest(err.Error())
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		existing := map[string]bool{}
		crow, err := tx.Query(ctx, `SELECT code FROM quiz.questions WHERE quiz_id=$1`, quizID)
		if err != nil {
			return err
		}
		codes, err := pgx.CollectRows(crow, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, c := range codes {
			existing[c] = true
		}
		for _, r := range rows {
			if len(r.Errors) == 0 && existing[r.Question.Code] {
				r.Errors = map[string]string{"code": "question code " + r.Question.Code + " already exists in this quiz"}
			}
			if len(r.Errors) > 0 {
				rep.Rejected = append(rep.Rejected, RowError{Row: r.Row, Code: r.Question.Code, Errors: r.Errors})
				continue
			}
			q := r.Question
			if err := insertQuestion(ctx, tx, quizID, &q); err != nil {
				return err
			}
			existing[q.Code] = true
			rep.Imported++
		}
		return touchQuiz(ctx, tx, quizID)
	})
	return rep, err
}

// ---------- resources ----------

type ResourceInput struct {
	Role    Role   `json:"role"`
	N       int    `json:"n"`
	URL     string `json:"url"`
	AltText string `json:"alt_text"`
}

func validateResource(q Question, in ResourceInput) (string, map[string]string) {
	f := map[string]string{}
	if !in.Role.Valid() {
		f["role"] = "role must be Q, O or F"
	}
	if in.N < 1 || in.N > 50 {
		f["n"] = "n must be 1-50"
	}
	if in.Role == RoleOption {
		count := len(q.Body.Options) + len(q.Body.Left) + len(q.Body.Right)
		if in.N > count {
			f["n"] = fmt.Sprintf("question %s has only %d options", q.Code, count)
		}
	}
	if len(in.AltText) > 500 {
		f["alt_text"] = "at most 500 characters"
	}
	norm, err := imageurl.Normalise(in.URL)
	if err != nil {
		f["url"] = err.Error()
	}
	return norm, f
}

func upsertResource(ctx context.Context, tx pgx.Tx, questionID string, in ResourceInput, norm string) error {
	_, err := tx.Exec(ctx, `INSERT INTO quiz.resources(question_id, role, n, source_url, url, alt_text)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (question_id, role, n) DO UPDATE SET source_url=EXCLUDED.source_url, url=EXCLUDED.url,
			alt_text=EXCLUDED.alt_text, status='unchecked', message='', checked_at=NULL`,
		questionID, in.Role, in.N, strings.TrimSpace(in.URL), norm, strings.TrimSpace(in.AltText))
	return err
}

// SetResource attaches or replaces one resource in the question editor.
func (s *Service) SetResource(ctx context.Context, teacherID, questionID string, in ResourceInput) (Question, error) {
	q, err := s.GetQuestion(ctx, teacherID, questionID)
	if err != nil {
		return q, err
	}
	norm, f := validateResource(q, in)
	if len(f) > 0 {
		return q, httpx.Invalid(f)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := upsertResource(ctx, tx, questionID, in, norm); err != nil {
			return err
		}
		_, err := s.jobs.InsertTx(ctx, tx, CheckResourcesArgs{QuizID: q.QuizID}, nil)
		return err
	})
	if err != nil {
		return q, err
	}
	return s.GetQuestion(ctx, teacherID, questionID)
}

func (s *Service) DeleteResource(ctx context.Context, teacherID, resourceID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM quiz.resources r USING quiz.questions q, quiz.quizzes z
		WHERE q.id=r.question_id AND z.id=q.quiz_id AND r.id=$1 AND z.teacher_id=$2`, resourceID, teacherID)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return err
}

// ImportResources attaches resources from a BA §10.4 mapping CSV (FR-QZ-06).
func (s *Service) ImportResources(ctx context.Context, teacherID, quizID string, data []byte) (ImportReport, error) {
	rep := ImportReport{Rejected: []RowError{}}
	rows, err := ParseResourcesCSV(data)
	if err != nil {
		return rep, httpx.BadRequest(err.Error())
	}
	qs, err := s.ListQuestions(ctx, teacherID, quizID)
	if err != nil {
		return rep, err
	}
	byCode := map[string]Question{}
	for _, q := range qs {
		byCode[strings.ToLower(q.Code)] = q
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, r := range rows {
			in := ResourceInput{Role: r.Role, N: r.N, URL: r.URL, AltText: r.AltText}
			q, ok := byCode[strings.ToLower(r.Code)]
			if len(r.Errors) == 0 && !ok {
				r.Errors["resource_name"] = "no question with code " + r.Code + " in this quiz"
			}
			var norm string
			if len(r.Errors) == 0 {
				var f map[string]string
				norm, f = validateResource(q, in)
				for k, v := range f {
					r.Errors[k] = v
				}
			}
			if len(r.Errors) > 0 {
				rep.Rejected = append(rep.Rejected, RowError{Row: r.Row, Code: r.Code, Errors: r.Errors})
				continue
			}
			if err := upsertResource(ctx, tx, q.ID, in, norm); err != nil {
				return err
			}
			rep.Imported++
		}
		if rep.Imported > 0 {
			if _, err := s.jobs.InsertTx(ctx, tx, CheckResourcesArgs{QuizID: quizID}, nil); err != nil {
				return err
			}
		}
		return nil
	})
	return rep, err
}

// CheckResources validates every resource URL of a quiz (FR-QZ-11, UC-01 step 6).
func (s *Service) CheckResources(ctx context.Context, quizID string, onlyUnchecked bool) error {
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.url FROM quiz.resources r JOIN quiz.questions q ON q.id=r.question_id
		WHERE q.quiz_id=$1 AND (NOT $2 OR r.status='unchecked')`, quizID, onlyUnchecked)
	if err != nil {
		return err
	}
	type item struct{ id, url string }
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
		var it item
		return it, r.Scan(&it.id, &it.url)
	})
	if err != nil {
		return err
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for _, it := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			res := s.checker.Check(ctx, it.url)
			status := "ok"
			if !res.OK {
				status = "broken"
			}
			if _, err := s.pool.Exec(ctx, `UPDATE quiz.resources SET status=$2, message=$3, checked_at=now() WHERE id=$1`, it.id, status, res.Message); err != nil {
				mu.Lock()
				firstErr = err
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

// RequestCheck enqueues a re-check of all resources of a quiz.
func (s *Service) RequestCheck(ctx context.Context, teacherID, quizID string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		_, err := s.jobs.InsertTx(ctx, tx, CheckResourcesArgs{QuizID: quizID, All: true}, nil)
		return err
	})
}

// CheckResourcesArgs is the River job that validates resource URLs off the request path.
type CheckResourcesArgs struct {
	QuizID string `json:"quiz_id"`
	All    bool   `json:"all"`
}

func (CheckResourcesArgs) Kind() string { return "quiz_resource_check" }

func (CheckResourcesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3}
}

type CheckResourcesWorker struct {
	river.WorkerDefaults[CheckResourcesArgs]
	Service *Service
}

func (w *CheckResourcesWorker) Work(ctx context.Context, job *river.Job[CheckResourcesArgs]) error {
	return w.Service.CheckResources(ctx, job.Args.QuizID, !job.Args.All)
}
