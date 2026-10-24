package tutorial

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
)

// A student's run through a tutorial (TU-FR-20 to TU-FR-34). Answers are
// checked page by page and marked as quiz answers are; written answers the
// teacher marks wait as pending. When marks and feedback show follows the
// tutorial's setting: once a page is checked, at the end, or on release.

var (
	errNotOpen     = httpx.NewError(http.StatusForbidden, "tutorial_not_open", "this tutorial isn't open yet")
	errForwardOnly = httpx.NewError(http.StatusConflict, "forward_only", "this tutorial goes forward only")
	errCheckpoint  = httpx.NewError(http.StatusConflict, "checkpoint", "finish this page first")
	errLocked      = httpx.NewError(http.StatusConflict, "answer_locked", "this answer has been checked and can't be changed")
	errFinished    = httpx.NewError(http.StatusConflict, "tutorial_finished", "you've finished this tutorial")
	errPastDue     = httpx.NewError(http.StatusConflict, "tutorial_past_due", "this tutorial is past its due date: you can read it, but not answer")
	errNotHere     = httpx.NewError(http.StatusConflict, "not_this_page", "answer the page you're on")
)

type answer struct {
	Response quiz.Response
	Score    float64
	Max      float64
	Correct  bool
	Status   string
	Tries    int
	Comment  string
}

// run is everything a student's view is built from.
type run struct {
	t        Tutorial
	id       string
	current  int
	reached  int
	finished *time.Time
	pages    []Page
	qs       map[string]quiz.Question
	effs     map[string]settings.Effective
	answers  map[string]answer
}

func (r *run) pastDue(now time.Time) bool { return r.t.DueAt != nil && now.After(*r.t.DueAt) }

// readOnly: nothing more can be answered, so every page can be read.
func (r *run) readOnly(now time.Time) bool { return r.finished != nil || r.pastDue(now) }

// feedbackShown: marks and feedback for checked answers are visible.
func (r *run) feedbackShown() bool {
	switch r.t.Feedback {
	case FeedbackPage:
		return true
	case FeedbackEnd:
		return r.finished != nil
	}
	return r.t.ReleasedAt != nil
}

// canRetry: a checked answer may be tried again (a correct-answer checkpoint
// with tries left, its feedback showing it's wrong).
func (r *run) canRetry(p Page, a answer) bool {
	return p.Checkpoint == CheckCorrect && a.Status == "marked" && !a.Correct && (p.MaxTries == 0 || a.Tries < p.MaxTries)
}

// locked: an answer that can't change any more.
func (r *run) locked(p Page, qid string) bool {
	a, ok := r.answers[qid]
	if !ok {
		return false
	}
	if r.t.Feedback == FeedbackPage || r.t.LockAnswers {
		return !r.canRetry(p, a)
	}
	return false
}

// passed: the page's checkpoint lets the student go on.
func (r *run) passed(p Page) bool {
	for _, qid := range p.QuestionIDs {
		if _, ok := r.qs[qid]; !ok {
			continue // deleted since
		}
		a, ok := r.answers[qid]
		switch p.Checkpoint {
		case CheckAnswered:
			if !ok {
				return false
			}
		case CheckCorrect:
			// Answers the teacher marks can't be checked now: answering them is enough.
			if !ok || (a.Status == "marked" && !a.Correct && r.canRetry(p, a)) {
				return false
			}
		}
	}
	return true
}

func (s *Service) loadRun(ctx context.Context, t Tutorial, userID string, start bool) (*run, error) {
	r := &run{t: t}
	err := s.pool.QueryRow(ctx, `SELECT id, current, reached, finished_at FROM tutorial.runs WHERE tutorial_id=$1 AND user_id=$2`, t.ID, userID).
		Scan(&r.id, &r.current, &r.reached, &r.finished)
	if errors.Is(err, pgx.ErrNoRows) && start {
		err = s.pool.QueryRow(ctx, `INSERT INTO tutorial.runs(tutorial_id, user_id) VALUES ($1,$2)
			ON CONFLICT (tutorial_id, user_id) DO UPDATE SET updated_at=now() RETURNING id, current, reached, finished_at`, t.ID, userID).
			Scan(&r.id, &r.current, &r.reached, &r.finished)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if r.pages, err = s.pages(ctx, t.ID); err != nil {
		return nil, err
	}
	qs, effs, err := s.questions.TutorialQuestions(ctx, t.QuizID)
	if err != nil {
		return nil, err
	}
	r.qs, r.effs = map[string]quiz.Question{}, effs
	for _, q := range qs {
		r.qs[q.ID] = q
	}
	r.answers = map[string]answer{}
	rows, err := s.pool.Query(ctx, `SELECT question_id, response, score, max_score, correct, status, tries, comment FROM tutorial.answers WHERE run_id=$1`, r.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var qid string
		var raw []byte
		var a answer
		if err := rows.Scan(&qid, &raw, &a.Score, &a.Max, &a.Correct, &a.Status, &a.Tries, &a.Comment); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &a.Response)
		r.answers[qid] = a
	}
	// Pages may have been removed since: stay within the tutorial.
	if n := len(r.pages); n > 0 {
		r.current = min(r.current, n-1)
		r.reached = min(max(r.reached, r.current), n-1)
	}
	return r, rows.Err()
}

// student finds a published tutorial a student may open: they're actively
// enrolled in its classroom, and it's open.
func (s *Service) student(ctx context.Context, userID, id string) (Tutorial, error) {
	t, err := scanTutorial(s.pool.QueryRow(ctx, `SELECT `+tutorialCols+` FROM tutorial.tutorials t WHERE t.id=$1 AND t.status='published'`, id))
	if err != nil {
		return t, err
	}
	tc, err := s.topics.TopicContext(ctx, "", t.TopicID)
	if err != nil {
		return t, err
	}
	e, ok, err := s.topics.GetEnrolment(ctx, tc.ClassroomID, userID)
	if err != nil {
		return t, err
	}
	if !ok || e.Status != "active" || tc.ClassroomArchived {
		return t, httpx.ErrNotFound
	}
	if t.OpensAt != nil && s.now().Before(*t.OpensAt) {
		return t, errNotOpen
	}
	return t, nil
}

// ---------- views ----------

// Mark is a checked answer's outcome, when feedback shows.
type Mark struct {
	Score    float64   `json:"score"`
	Max      float64   `json:"max"`
	Correct  bool      `json:"correct"`
	Pending  bool      `json:"pending"` // the teacher marks it
	Feedback string    `json:"feedback,omitempty"`
	Comment  string    `json:"comment,omitempty"`
	Key      *quiz.Key `json:"key,omitempty"` // the right answer, once theirs is final
}

// QuestionView is a question as the student sees it.
type QuestionView struct {
	quiz.StudentQuestion
	Response *quiz.Response `json:"response,omitempty"`
	Checked  bool           `json:"checked"`
	Locked   bool           `json:"locked"`
	Tries    int            `json:"tries"`
	Mark     *Mark          `json:"mark,omitempty"`
}

// PageView is one page as the student sees it.
type PageView struct {
	Index      int            `json:"index"`
	Title      string         `json:"title"`
	Blocks     []Block        `json:"blocks"`
	Questions  []QuestionView `json:"questions"`
	Checkpoint string         `json:"checkpoint"`
	MaxTries   int            `json:"max_tries"`
	Passed     bool           `json:"passed"` // the checkpoint lets them go on
}

// PageSummary is a page in the tutorial's map.
type PageSummary struct {
	Title     string `json:"title"`
	Questions int    `json:"questions"`
	Done      bool   `json:"done"` // every question on it answered
	Open      bool   `json:"open"` // they may go there now
}

// Score is the tutorial's total, when it shows.
type Score struct {
	Score   float64 `json:"score"`
	Max     float64 `json:"max"`
	Pending int     `json:"pending"`
}

// RunView is a student's state in a tutorial.
type RunView struct {
	Tutorial struct {
		ID          string     `json:"id"`
		Title       string     `json:"title"`
		Description string     `json:"description"`
		AllowBack   bool       `json:"allow_back"`
		Feedback    string     `json:"feedback"`
		DueAt       *time.Time `json:"due_at,omitempty"`
	} `json:"tutorial"`
	Current  int           `json:"current"`
	Reached  int           `json:"reached"`
	Finished bool          `json:"finished"`
	ReadOnly bool          `json:"read_only"`
	PastDue  bool          `json:"past_due"`
	Pages    []PageSummary `json:"pages"`
	Page     *PageView     `json:"page,omitempty"`
	Score    *Score        `json:"score,omitempty"`
}

func feedbackText(q quiz.Question, a answer) string {
	if a.Correct {
		return q.Feedback.Correct
	}
	return q.Feedback.Incorrect
}

func (s *Service) pageView(r *run, i int) *PageView {
	p := r.pages[i]
	v := &PageView{Index: i, Title: p.Title, Blocks: p.Blocks, Checkpoint: p.Checkpoint, MaxTries: p.MaxTries, Passed: r.passed(p), Questions: []QuestionView{}}
	if v.Blocks == nil {
		v.Blocks = []Block{}
	}
	shown := r.feedbackShown()
	ro := r.readOnly(s.now())
	for _, qid := range p.QuestionIDs {
		q, ok := r.qs[qid]
		if !ok {
			continue
		}
		qv := QuestionView{StudentQuestion: q.StudentView(), Locked: ro || r.locked(p, qid)}
		if a, ok := r.answers[qid]; ok {
			resp := a.Response
			qv.Response, qv.Checked, qv.Tries = &resp, true, a.Tries
			if shown {
				m := &Mark{Score: a.Score, Max: a.Max, Correct: a.Correct, Pending: a.Status == "pending", Comment: a.Comment}
				if a.Status == "marked" {
					m.Feedback = feedbackText(q, a)
				}
				// The right answer, once theirs can't change.
				if (qv.Locked || ro) && q.Type.KeyAllowed() {
					k := q.Key
					m.Key = &k
				}
				qv.Mark = m
			}
		}
		v.Questions = append(v.Questions, qv)
	}
	return v
}

// mayOpen: page i can be shown now.
func (r *run) mayOpen(i int, now time.Time) bool {
	switch {
	case i < 0 || i >= len(r.pages):
		return false
	case r.readOnly(now):
		return true
	case i > r.reached:
		return false
	case i < r.current && !r.t.AllowBack:
		return false
	}
	return true
}

func (s *Service) view(r *run, page int) RunView {
	var v RunView
	v.Tutorial.ID, v.Tutorial.Title, v.Tutorial.Description = r.t.ID, r.t.Title, r.t.Description
	v.Tutorial.AllowBack, v.Tutorial.Feedback, v.Tutorial.DueAt = r.t.AllowBack, r.t.Feedback, r.t.DueAt
	now := s.now()
	v.Current, v.Reached, v.Finished, v.ReadOnly, v.PastDue = r.current, r.reached, r.finished != nil, r.readOnly(now), r.pastDue(now)
	v.Pages = make([]PageSummary, len(r.pages))
	for i, p := range r.pages {
		done := true
		n := 0
		for _, qid := range p.QuestionIDs {
			if _, ok := r.qs[qid]; !ok {
				continue
			}
			n++
			if _, ok := r.answers[qid]; !ok {
				done = false
			}
		}
		v.Pages[i] = PageSummary{Title: p.Title, Questions: n, Done: done, Open: r.mayOpen(i, now)}
	}
	if len(r.pages) > 0 {
		if !r.mayOpen(page, now) {
			page = r.current
		}
		v.Page = s.pageView(r, page)
	}
	if r.feedbackShown() {
		sc := &Score{}
		for _, p := range r.pages {
			for _, qid := range p.QuestionIDs {
				q, ok := r.qs[qid]
				if !ok {
					continue
				}
				sc.Max += q.Marks
				if a, ok := r.answers[qid]; ok {
					if a.Status == "pending" {
						sc.Pending++
					} else {
						sc.Score += a.Score
					}
				}
			}
		}
		v.Score = sc
	}
	return v
}

// ---------- the student's actions ----------

// Open starts or resumes a run and shows the page they're on.
func (s *Service) Open(ctx context.Context, userID, id string) (RunView, error) {
	t, err := s.student(ctx, userID, id)
	if err != nil {
		return RunView{}, err
	}
	r, err := s.loadRun(ctx, t, userID, true)
	if err != nil {
		return RunView{}, err
	}
	return s.view(r, r.current), nil
}

// Page shows one page, if they may see it.
func (s *Service) Page(ctx context.Context, userID, id string, i int) (RunView, error) {
	t, err := s.student(ctx, userID, id)
	if err != nil {
		return RunView{}, err
	}
	r, err := s.loadRun(ctx, t, userID, true)
	if err != nil {
		return RunView{}, err
	}
	if !r.mayOpen(i, s.now()) {
		return RunView{}, httpx.ErrNotFound
	}
	return s.view(r, i), nil
}

// Go moves to another page: back, if the tutorial allows it, or on, once
// every page before it is passed.
func (s *Service) Go(ctx context.Context, userID, id string, to int) (RunView, error) {
	t, err := s.student(ctx, userID, id)
	if err != nil {
		return RunView{}, err
	}
	r, err := s.loadRun(ctx, t, userID, true)
	if err != nil {
		return RunView{}, err
	}
	now := s.now()
	switch {
	case to < 0 || to >= len(r.pages):
		return RunView{}, httpx.ErrNotFound
	case r.readOnly(now):
		return s.view(r, to), nil // reading only: nothing to record
	case to < r.current && !t.AllowBack:
		return RunView{}, errForwardOnly
	}
	for i := r.current; i < to; i++ {
		if !r.passed(r.pages[i]) {
			return RunView{}, errCheckpoint
		}
	}
	r.current = to
	r.reached = max(r.reached, to)
	if _, err := s.pool.Exec(ctx, `UPDATE tutorial.runs SET current=$2, reached=$3, updated_at=now() WHERE id=$1`, r.id, r.current, r.reached); err != nil {
		return RunView{}, err
	}
	return s.view(r, to), nil
}

// AnswerInput is a page's answers to check, by question id.
type AnswerInput struct {
	Page      int                      `json:"page"`
	Responses map[string]quiz.Response `json:"responses"`
}

// Answer checks answers on the page they're on, and marks them.
func (s *Service) Answer(ctx context.Context, userID, id string, in AnswerInput) (RunView, error) {
	t, err := s.student(ctx, userID, id)
	if err != nil {
		return RunView{}, err
	}
	r, err := s.loadRun(ctx, t, userID, true)
	if err != nil {
		return RunView{}, err
	}
	now := s.now()
	switch {
	case r.finished != nil:
		return RunView{}, errFinished
	case r.pastDue(now):
		return RunView{}, errPastDue
	case in.Page != r.current || in.Page < 0 || in.Page >= len(r.pages):
		return RunView{}, errNotHere
	case len(in.Responses) == 0:
		return RunView{}, httpx.Invalid(map[string]string{"responses": "answer at least one question"})
	}
	p := r.pages[in.Page]
	onPage := map[string]bool{}
	for _, qid := range p.QuestionIDs {
		onPage[qid] = true
	}
	f := map[string]string{}
	for qid, resp := range in.Responses {
		q, ok := r.qs[qid]
		switch {
		case !ok || !onPage[qid]:
			f[qid] = "not a question on this page"
		case r.locked(p, qid):
			return RunView{}, errLocked
		default:
			if err := quiz.ValidateResponse(q, resp); err != nil {
				f[qid] = err.Error()
			} else if !eval.Answered(&resp) {
				f[qid] = "answer the question"
			}
		}
	}
	if len(f) > 0 {
		return RunView{}, httpx.Invalid(f)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for qid, resp := range in.Responses {
			q := r.qs[qid]
			a := answer{Response: resp, Max: q.Marks, Status: "marked", Tries: r.answers[qid].Tries + 1}
			if m := eval.Method(q, r.effs[qid].EvaluationMethod); m == "key" {
				res := eval.MarkByKey(q, &resp)
				a.Score, a.Correct = res.Score, res.Correct
			} else {
				a.Status = "pending" // written answers: the teacher marks them (LLM marking comes later)
			}
			raw, _ := json.Marshal(resp)
			if _, err := tx.Exec(ctx, `INSERT INTO tutorial.answers(run_id, question_id, response, score, max_score, correct, status, tries, answered_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
				ON CONFLICT (run_id, question_id) DO UPDATE SET response=EXCLUDED.response, score=EXCLUDED.score, max_score=EXCLUDED.max_score,
					correct=EXCLUDED.correct, status=EXCLUDED.status, tries=EXCLUDED.tries, comment='', answered_at=EXCLUDED.answered_at`,
				r.id, qid, raw, a.Score, a.Max, a.Correct, a.Status, a.Tries, now); err != nil {
				return err
			}
			r.answers[qid] = a
		}
		_, err := tx.Exec(ctx, `UPDATE tutorial.runs SET updated_at=now() WHERE id=$1`, r.id)
		return err
	})
	if err != nil {
		return RunView{}, err
	}
	return s.view(r, in.Page), nil
}

// Finish ends the run, from the last page once it's passed.
func (s *Service) Finish(ctx context.Context, userID, id string) (RunView, error) {
	t, err := s.student(ctx, userID, id)
	if err != nil {
		return RunView{}, err
	}
	r, err := s.loadRun(ctx, t, userID, true)
	if err != nil {
		return RunView{}, err
	}
	if r.finished != nil {
		return s.view(r, r.current), nil
	}
	last := len(r.pages) - 1
	if last < 0 || r.current != last {
		return RunView{}, httpx.NewError(http.StatusConflict, "not_last_page", "go to the last page to finish")
	}
	if !r.passed(r.pages[last]) && !r.pastDue(s.now()) {
		return RunView{}, errCheckpoint
	}
	now := s.now()
	if _, err := s.pool.Exec(ctx, `UPDATE tutorial.runs SET finished_at=$2, updated_at=$2 WHERE id=$1`, r.id, now); err != nil {
		return RunView{}, err
	}
	r.finished = &now
	return s.view(r, r.current), nil
}

// MyTutorial is a published tutorial in a student's list.
type MyTutorial struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Classroom   string     `json:"classroom"`
	Pages       int        `json:"page_count"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	OpensAt     *time.Time `json:"opens_at,omitempty"`
	Started     bool       `json:"started"`
	Reached     int        `json:"reached"`
	Finished    bool       `json:"finished"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// Mine lists the published tutorials of the classrooms a student is in.
func (s *Service) Mine(ctx context.Context, userID string) ([]MyTutorial, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id, t.title, c.name, (SELECT count(*) FROM tutorial.pages p WHERE p.tutorial_id=t.id), t.due_at, t.opens_at,
			r.id IS NOT NULL, coalesce(r.reached, 0), r.finished_at IS NOT NULL, t.published_at
		FROM tutorial.tutorials t
		JOIN content.topics tp ON tp.id = t.topic_id
		JOIN content.modules m ON m.id = tp.module_id
		JOIN content.classrooms c ON c.id = m.classroom_id AND c.archived_at IS NULL
		JOIN content.enrolments e ON e.classroom_id = c.id AND e.user_id = $1 AND e.status = 'active'
		LEFT JOIN tutorial.runs r ON r.tutorial_id = t.id AND r.user_id = $1
		WHERE t.status = 'published'
		ORDER BY (r.finished_at IS NOT NULL), t.due_at NULLS LAST, t.published_at DESC LIMIT 200`, userID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MyTutorial, error) {
		var m MyTutorial
		return m, r.Scan(&m.ID, &m.Title, &m.Classroom, &m.Pages, &m.DueAt, &m.OpensAt, &m.Started, &m.Reached, &m.Finished, &m.PublishedAt)
	})
	if out == nil {
		out = []MyTutorial{}
	}
	return out, err
}
