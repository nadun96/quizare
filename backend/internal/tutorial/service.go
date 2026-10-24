package tutorial

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
)

// Topics finds a topic's classroom (content).
type Topics interface {
	TopicContext(ctx context.Context, teacherID, topicID string) (content.TopicContext, error)
	GetEnrolment(ctx context.Context, classroomID, userID string) (content.Enrolment, bool, error)
	ListEnrolments(ctx context.Context, teacherID, classroomID, status string) ([]content.Enrolment, error)
}

// Questions keeps a tutorial's questions (quiz).
type Questions interface {
	CreateTutorialQuestions(ctx context.Context, teacherID, topicID, title string) (quiz.Quiz, error)
	DeleteTutorialQuestions(ctx context.Context, teacherID, quizID string) error
	TutorialQuestions(ctx context.Context, quizID string) ([]quiz.Question, map[string]settings.Effective, error)
	CopyQuestions(ctx context.Context, teacherID, quizID string, ids []string) ([]quiz.Question, error)
}

// Users names the students (auth).
type Users interface {
	UsersByID(ctx context.Context, ids []string) (map[string]auth.User, error)
}

type Service struct {
	pool      *pgxpool.Pool
	topics    Topics
	questions Questions
	users     Users
	llm       LLMQueue
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool, topics Topics, questions Questions, users Users) *Service {
	return &Service{pool: pool, topics: topics, questions: questions, users: users, now: time.Now}
}

var (
	errHasRuns   = httpx.NewError(http.StatusConflict, "tutorial_has_runs", "students have worked on this tutorial; unpublish it instead, so their results stay")
	errNoPages   = httpx.NewError(http.StatusConflict, "tutorial_empty", "add a page before publishing")
	errArchived  = httpx.Conflict("the classroom is archived")
	errFeedbackX = httpx.Invalid(map[string]string{"feedback": "some pages need correct answers, which shows whether answers are right: keep feedback as students go, or change those pages"})
)

const tutorialCols = `t.id, t.teacher_id, t.topic_id, t.quiz_id, t.title, t.description, t.status, t.allow_back, t.lock_answers, t.feedback,
	t.released_at, t.opens_at, t.due_at, t.published_at, t.created_at, t.updated_at, (SELECT count(*) FROM tutorial.pages p WHERE p.tutorial_id=t.id)`

func scanTutorial(r pgx.Row) (Tutorial, error) {
	var t Tutorial
	err := r.Scan(&t.ID, &t.TeacherID, &t.TopicID, &t.QuizID, &t.Title, &t.Description, &t.Status, &t.AllowBack, &t.LockAnswers, &t.Feedback,
		&t.ReleasedAt, &t.OpensAt, &t.DueAt, &t.PublishedAt, &t.CreatedAt, &t.UpdatedAt, &t.Pages)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, httpx.ErrNotFound
	}
	return t, err
}

// owned returns a teacher's tutorial.
func (s *Service) owned(ctx context.Context, teacherID, id string) (Tutorial, error) {
	return scanTutorial(s.pool.QueryRow(ctx, `SELECT `+tutorialCols+` FROM tutorial.tutorials t WHERE t.id=$1 AND t.teacher_id=$2`, id, teacherID))
}

// Create makes a tutorial, and the quiz that holds its questions.
func (s *Service) Create(ctx context.Context, teacherID, topicID string, in TutorialInput) (Tutorial, error) {
	title := ""
	if in.Title != nil {
		title = strings.Join(strings.Fields(*in.Title), " ")
	}
	if n := runes(title); n < 1 || n > 200 {
		return Tutorial{}, httpx.Invalid(map[string]string{"title": "give the tutorial a title (at most 200 characters)"})
	}
	desc := ""
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	q, err := s.questions.CreateTutorialQuestions(ctx, teacherID, topicID, title)
	if err != nil {
		return Tutorial{}, err
	}
	var id string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO tutorial.tutorials(topic_id, teacher_id, quiz_id, title, description) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			topicID, teacherID, q.ID, title, desc).Scan(&id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "tutorial_created", "tutorial", id, nil)
	})
	if err != nil {
		_ = s.questions.DeleteTutorialQuestions(ctx, teacherID, q.ID)
		return Tutorial{}, err
	}
	return s.owned(ctx, teacherID, id)
}

// List returns a topic's tutorials.
func (s *Service) List(ctx context.Context, teacherID, topicID string) ([]Tutorial, error) {
	if _, err := s.topics.TopicContext(ctx, teacherID, topicID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+tutorialCols+` FROM tutorial.tutorials t WHERE t.topic_id=$1 AND t.teacher_id=$2 ORDER BY t.created_at`, topicID, teacherID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Tutorial, error) { return scanTutorial(r) })
	if out == nil {
		out = []Tutorial{}
	}
	return out, err
}

// Update changes a tutorial's details and settings.
func (s *Service) Update(ctx context.Context, teacherID, id string, in TutorialInput) (Tutorial, error) {
	t, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return t, err
	}
	f := map[string]string{}
	if in.Title != nil {
		t.Title = strings.Join(strings.Fields(*in.Title), " ")
		if n := runes(t.Title); n < 1 || n > 200 {
			f["title"] = "give the tutorial a title (at most 200 characters)"
		}
	}
	if in.Description != nil {
		t.Description = strings.TrimSpace(*in.Description)
		if runes(t.Description) > 5000 {
			f["description"] = "at most 5000 characters"
		}
	}
	if in.AllowBack != nil {
		t.AllowBack = *in.AllowBack
	}
	if in.LockAnswers != nil {
		t.LockAnswers = *in.LockAnswers
	}
	if in.Feedback != nil {
		t.Feedback = *in.Feedback
		if t.Feedback != FeedbackPage && t.Feedback != FeedbackEnd && t.Feedback != FeedbackRelease {
			f["feedback"] = "choose page, end or release"
		}
	}
	if in.ClearDates {
		t.OpensAt, t.DueAt = nil, nil
	}
	if in.OpensAt != nil {
		t.OpensAt = in.OpensAt
	}
	if in.DueAt != nil {
		t.DueAt = in.DueAt
	}
	if t.OpensAt != nil && t.DueAt != nil && !t.DueAt.After(*t.OpensAt) {
		f["due_at"] = "the due date comes after the opening date"
	}
	if len(f) > 0 {
		return t, httpx.Invalid(f)
	}
	if t.Feedback != FeedbackPage {
		var strict bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tutorial.pages WHERE tutorial_id=$1 AND checkpoint='correct')`, id).Scan(&strict); err != nil {
			return t, err
		}
		if strict {
			return t, errFeedbackX
		}
	}
	_, err = s.pool.Exec(ctx, `UPDATE tutorial.tutorials SET title=$2, description=$3, allow_back=$4, lock_answers=$5, feedback=$6, opens_at=$7, due_at=$8, updated_at=now() WHERE id=$1`,
		id, t.Title, t.Description, t.AllowBack, t.LockAnswers, t.Feedback, t.OpensAt, t.DueAt)
	if err != nil {
		return t, err
	}
	return s.owned(ctx, teacherID, id)
}

// SetStatus publishes a tutorial to its classroom, or takes it back to a draft.
func (s *Service) SetStatus(ctx context.Context, teacherID, id, status string) (Tutorial, error) {
	t, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return t, err
	}
	switch status {
	case StatusPublished:
		if t.Pages == 0 {
			return t, errNoPages
		}
		tc, err := s.topics.TopicContext(ctx, teacherID, t.TopicID)
		if err != nil {
			return t, err
		}
		if tc.ClassroomArchived {
			return t, errArchived
		}
	case StatusDraft:
	default:
		return t, httpx.Invalid(map[string]string{"status": "draft or published"})
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE tutorial.tutorials SET status=$2, published_at=CASE WHEN $2='published' THEN coalesce(published_at, now()) ELSE published_at END, updated_at=now() WHERE id=$1`, id, status); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "tutorial_"+status, "tutorial", id, nil)
	})
	if err != nil {
		return t, err
	}
	return s.owned(ctx, teacherID, id)
}

// Release shows (or hides again) marks and feedback when the tutorial gives them on release.
func (s *Service) Release(ctx context.Context, teacherID, id string, released bool) (Tutorial, error) {
	if _, err := s.owned(ctx, teacherID, id); err != nil {
		return Tutorial{}, err
	}
	_, err := s.pool.Exec(ctx, `UPDATE tutorial.tutorials SET released_at=CASE WHEN $2 THEN coalesce(released_at, now()) END, updated_at=now() WHERE id=$1`, id, released)
	if err != nil {
		return Tutorial{}, err
	}
	return s.owned(ctx, teacherID, id)
}

// Delete removes a tutorial nobody has started; one with runs is unpublished instead.
func (s *Service) Delete(ctx context.Context, teacherID, id string) error {
	t, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return err
	}
	var runs bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tutorial.runs WHERE tutorial_id=$1)`, id).Scan(&runs); err != nil {
		return err
	}
	if runs {
		return errHasRuns
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM tutorial.tutorials WHERE id=$1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "tutorial_deleted", "tutorial", id, map[string]string{"title": t.Title})
	}); err != nil {
		return err
	}
	return s.questions.DeleteTutorialQuestions(ctx, teacherID, t.QuizID)
}

// ---------- pages ----------

func (s *Service) pages(ctx context.Context, tutorialID string) ([]Page, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, position, title, blocks, question_ids::text[], checkpoint, max_tries, leaving, lock_after FROM tutorial.pages WHERE tutorial_id=$1 ORDER BY position, created_at`, tutorialID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Page, error) {
		var p Page
		var raw []byte
		err := r.Scan(&p.ID, &p.Position, &p.Title, &raw, &p.QuestionIDs, &p.Checkpoint, &p.MaxTries, &p.Leaving, &p.LockAfter)
		if err == nil {
			err = json.Unmarshal(raw, &p.Blocks)
		}
		return p, err
	})
	if out == nil {
		out = []Page{}
	}
	return out, err
}

// Editor is everything the tutorial editor shows.
type Editor struct {
	Tutorial  Tutorial        `json:"tutorial"`
	Pages     []Page          `json:"pages"`
	Questions []quiz.Question `json:"questions"`
}

// Get returns a tutorial with its pages and questions, for its teacher.
func (s *Service) Get(ctx context.Context, teacherID, id string) (Editor, error) {
	t, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return Editor{}, err
	}
	pages, err := s.pages(ctx, id)
	if err != nil {
		return Editor{}, err
	}
	qs, _, err := s.questions.TutorialQuestions(ctx, t.QuizID)
	if err != nil {
		return Editor{}, err
	}
	if qs == nil {
		qs = []quiz.Question{}
	}
	// Questions deleted in the editor drop off their pages.
	have := map[string]bool{}
	for _, q := range qs {
		have[q.ID] = true
	}
	for i := range pages {
		pages[i].QuestionIDs = slices.DeleteFunc(pages[i].QuestionIDs, func(id string) bool { return !have[id] })
	}
	return Editor{Tutorial: t, Pages: pages, Questions: qs}, nil
}

// checkQuestions makes sure a page lists only this tutorial's questions,
// and none that another page already has.
func (s *Service) checkQuestions(ctx context.Context, t Tutorial, pageID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	qs, _, err := s.questions.TutorialQuestions(ctx, t.QuizID)
	if err != nil {
		return err
	}
	mine := map[string]bool{}
	for _, q := range qs {
		mine[q.ID] = true
	}
	for _, id := range ids {
		if !mine[id] {
			return httpx.Invalid(map[string]string{"question_ids": "a question isn't part of this tutorial"})
		}
	}
	var clash bool
	err = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tutorial.pages WHERE tutorial_id=$1 AND id::text <> $2 AND question_ids && $3::uuid[])`, t.ID, pageID, ids).Scan(&clash)
	if err != nil {
		return err
	}
	if clash {
		return httpx.Invalid(map[string]string{"question_ids": "a question is already on another page"})
	}
	return nil
}

// AddPage adds a page at the end.
func (s *Service) AddPage(ctx context.Context, teacherID, tutorialID string, in PageInput) (Page, error) {
	t, err := s.owned(ctx, teacherID, tutorialID)
	if err != nil {
		return Page{}, err
	}
	if t.Pages >= maxPages {
		return Page{}, httpx.Invalid(map[string]string{"pages": "a tutorial has at most 100 pages"})
	}
	if err := in.clean(t.Feedback); err != nil {
		return Page{}, err
	}
	if err := s.checkQuestions(ctx, t, "", in.QuestionIDs); err != nil {
		return Page{}, err
	}
	blocks, _ := json.Marshal(in.Blocks)
	var id string
	err = s.pool.QueryRow(ctx, `INSERT INTO tutorial.pages(tutorial_id, position, title, blocks, question_ids, checkpoint, max_tries, leaving, lock_after)
		VALUES ($1, (SELECT coalesce(max(position), 0) + 1 FROM tutorial.pages WHERE tutorial_id=$1), $2, $3, $4::uuid[], $5, $6, $7, $8) RETURNING id`,
		tutorialID, in.Title, blocks, in.QuestionIDs, in.Checkpoint, in.MaxTries, in.Leaving, in.LockAfter).Scan(&id)
	if err != nil {
		return Page{}, err
	}
	s.touch(ctx, tutorialID)
	return s.page(ctx, tutorialID, id)
}

// UpdatePage changes a page.
func (s *Service) UpdatePage(ctx context.Context, teacherID, tutorialID, pageID string, in PageInput) (Page, error) {
	t, err := s.owned(ctx, teacherID, tutorialID)
	if err != nil {
		return Page{}, err
	}
	if err := in.clean(t.Feedback); err != nil {
		return Page{}, err
	}
	if err := s.checkQuestions(ctx, t, pageID, in.QuestionIDs); err != nil {
		return Page{}, err
	}
	blocks, _ := json.Marshal(in.Blocks)
	tag, err := s.pool.Exec(ctx, `UPDATE tutorial.pages SET title=$3, blocks=$4, question_ids=$5::uuid[], checkpoint=$6, max_tries=$7, leaving=$8, lock_after=$9, updated_at=now()
		WHERE id=$1 AND tutorial_id=$2`, pageID, tutorialID, in.Title, blocks, in.QuestionIDs, in.Checkpoint, in.MaxTries, in.Leaving, in.LockAfter)
	if err != nil {
		return Page{}, err
	}
	if tag.RowsAffected() == 0 {
		return Page{}, httpx.ErrNotFound
	}
	s.touch(ctx, tutorialID)
	return s.page(ctx, tutorialID, pageID)
}

func (s *Service) page(ctx context.Context, tutorialID, pageID string) (Page, error) {
	all, err := s.pages(ctx, tutorialID)
	if err != nil {
		return Page{}, err
	}
	for _, p := range all {
		if p.ID == pageID {
			return p, nil
		}
	}
	return Page{}, httpx.ErrNotFound
}

func (s *Service) touch(ctx context.Context, tutorialID string) {
	_, _ = s.pool.Exec(ctx, `UPDATE tutorial.tutorials SET updated_at=now() WHERE id=$1`, tutorialID)
}

// DeletePage removes a page. Its questions stay in the tutorial, unplaced.
func (s *Service) DeletePage(ctx context.Context, teacherID, tutorialID, pageID string) error {
	if _, err := s.owned(ctx, teacherID, tutorialID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM tutorial.pages WHERE id=$1 AND tutorial_id=$2`, pageID, tutorialID)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	s.touch(ctx, tutorialID)
	return err
}

// Reorder sets the order of the pages.
func (s *Service) Reorder(ctx context.Context, teacherID, tutorialID string, ids []string) ([]Page, error) {
	if _, err := s.owned(ctx, teacherID, tutorialID); err != nil {
		return nil, err
	}
	have, err := s.pages(ctx, tutorialID)
	if err != nil {
		return nil, err
	}
	if len(ids) != len(have) {
		return nil, httpx.Invalid(map[string]string{"ids": "list every page once"})
	}
	known := map[string]bool{}
	for _, p := range have {
		known[p.ID] = true
	}
	for _, id := range ids {
		if !known[id] {
			return nil, httpx.Invalid(map[string]string{"ids": "list every page once"})
		}
		delete(known, id)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for i, id := range ids {
			if _, err := tx.Exec(ctx, `UPDATE tutorial.pages SET position=$2 WHERE id=$1`, id, i+1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.touch(ctx, tutorialID)
	return s.pages(ctx, tutorialID)
}

// CopyQuestions copies some of the teacher's own questions, from the
// question bank say, into the tutorial (TU-FR-04), ready to place on a page.
func (s *Service) CopyQuestions(ctx context.Context, teacherID, tutorialID string, ids []string) ([]quiz.Question, error) {
	t, err := s.owned(ctx, teacherID, tutorialID)
	if err != nil {
		return nil, err
	}
	qs, err := s.questions.CopyQuestions(ctx, teacherID, t.QuizID, ids)
	if err == nil {
		s.touch(ctx, tutorialID)
	}
	return qs, err
}

// DuplicatePage copies a page, with copies of its questions (a question is
// on one page at most), and puts it straight after the original (TU-FR-02).
func (s *Service) DuplicatePage(ctx context.Context, teacherID, tutorialID, pageID string) (Page, error) {
	t, err := s.owned(ctx, teacherID, tutorialID)
	if err != nil {
		return Page{}, err
	}
	if t.Pages >= maxPages {
		return Page{}, httpx.Invalid(map[string]string{"pages": "a tutorial has at most 100 pages"})
	}
	src, err := s.page(ctx, tutorialID, pageID)
	if err != nil {
		return Page{}, err
	}
	ed, err := s.Get(ctx, teacherID, tutorialID) // drops deleted questions from the page
	if err != nil {
		return Page{}, err
	}
	for _, p := range ed.Pages {
		if p.ID == pageID {
			src.QuestionIDs = p.QuestionIDs
		}
	}
	ids := []string{}
	if len(src.QuestionIDs) > 0 {
		copies, err := s.questions.CopyQuestions(ctx, teacherID, t.QuizID, src.QuestionIDs)
		if err != nil {
			return Page{}, err
		}
		for _, q := range copies {
			ids = append(ids, q.ID)
		}
	}
	title := src.Title
	if title != "" && runes(title) <= 193 {
		title += " (copy)"
	}
	blocks, _ := json.Marshal(src.Blocks)
	var id string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE tutorial.pages SET position=position+1 WHERE tutorial_id=$1 AND position > $2`, tutorialID, src.Position); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO tutorial.pages(tutorial_id, position, title, blocks, question_ids, checkpoint, max_tries, leaving, lock_after)
			VALUES ($1,$2,$3,$4,$5::uuid[],$6,$7,$8,$9) RETURNING id`,
			tutorialID, src.Position+1, title, blocks, ids, src.Checkpoint, src.MaxTries, src.Leaving, src.LockAfter).Scan(&id)
	})
	if err != nil {
		return Page{}, err
	}
	s.touch(ctx, tutorialID)
	return s.page(ctx, tutorialID, id)
}
