package tutorial

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// The teacher's view of a tutorial's students (TU-FR-33, TU-FR-34): who has
// started, how far they are, their marks, and answers waiting to be marked.

// StudentProgress is one student's progress.
type StudentProgress struct {
	UserID        string     `json:"user_id"`
	RunID         string     `json:"run_id,omitempty"`
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	StudentNumber *string    `json:"student_number,omitempty"`
	Started       bool       `json:"started"`
	Current       int        `json:"current"`
	Reached       int        `json:"reached"`
	Finished      bool       `json:"finished"`
	Answered      int        `json:"answered"`
	Score         float64    `json:"score"`
	Pending       int        `json:"pending"`
	Seconds       int        `json:"seconds"` // time on the tutorial's pages
	Leaves        int        `json:"leaves"`  // times they left the app where it's recorded or not allowed
	Locked        int        `json:"locked"`  // pages locked for leaving
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
}

// Progress is the tutorial's class list.
type Progress struct {
	Pages     int               `json:"pages"`
	Questions int               `json:"questions"`
	MaxScore  float64           `json:"max_score"`
	Students  []StudentProgress `json:"students"`
}

// Progress lists the classroom's students with their runs, and students who
// worked on it and have since left the classroom.
func (s *Service) Progress(ctx context.Context, teacherID, id string) (Progress, error) {
	t, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return Progress{}, err
	}
	tc, err := s.topics.TopicContext(ctx, teacherID, t.TopicID)
	if err != nil {
		return Progress{}, err
	}
	enrolled, err := s.topics.ListEnrolments(ctx, teacherID, tc.ClassroomID, "active")
	if err != nil {
		return Progress{}, err
	}
	pages, err := s.pages(ctx, id)
	if err != nil {
		return Progress{}, err
	}
	qs, _, err := s.questions.TutorialQuestions(ctx, t.QuizID)
	if err != nil {
		return Progress{}, err
	}
	marks := map[string]float64{}
	for _, q := range qs {
		marks[q.ID] = q.Marks
	}
	out := Progress{Pages: len(pages), Students: []StudentProgress{}}
	placed := map[string]bool{}
	for _, p := range pages {
		for _, qid := range p.QuestionIDs {
			if m, ok := marks[qid]; ok {
				out.Questions++
				out.MaxScore += m
				placed[qid] = true
			}
		}
	}
	runs := map[string]StudentProgress{}
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.user_id, r.current, r.reached, r.finished_at IS NOT NULL, r.updated_at,
			coalesce(array_agg(a.question_id::text) FILTER (WHERE a.question_id IS NOT NULL), '{}'),
			coalesce(array_agg(a.score) FILTER (WHERE a.question_id IS NOT NULL), '{}'),
			coalesce(array_agg(a.status) FILTER (WHERE a.question_id IS NOT NULL), '{}'),
			coalesce(v.seconds, 0), coalesce(v.leaves, 0), coalesce(v.locked, 0)
		FROM tutorial.runs r LEFT JOIN tutorial.answers a ON a.run_id = r.id
		LEFT JOIN LATERAL (SELECT sum(seconds)::int AS seconds, sum(leaves)::int AS leaves, count(locked_at)::int AS locked
			FROM tutorial.visits WHERE run_id = r.id) v ON true
		WHERE r.tutorial_id=$1 GROUP BY r.id, v.seconds, v.leaves, v.locked`, id)
	if err != nil {
		return Progress{}, err
	}
	for rows.Next() {
		var p StudentProgress
		var at time.Time
		var qids, statuses []string
		var scores []float64
		if err := rows.Scan(&p.RunID, &p.UserID, &p.Current, &p.Reached, &p.Finished, &at, &qids, &scores, &statuses, &p.Seconds, &p.Leaves, &p.Locked); err != nil {
			rows.Close()
			return Progress{}, err
		}
		p.Started, p.UpdatedAt = true, &at
		for i, qid := range qids {
			if !placed[qid] {
				continue
			}
			p.Answered++
			if statuses[i] == "pending" {
				p.Pending++
			} else {
				p.Score += scores[i]
			}
		}
		p.Score = math.Round(p.Score*100) / 100
		runs[p.UserID] = p
	}
	rows.Close()
	for _, e := range enrolled {
		p, ok := runs[e.UserID]
		if !ok {
			p = StudentProgress{UserID: e.UserID}
		}
		p.Name, p.Email, p.StudentNumber = e.StudentName, e.StudentEmail, e.StudentNumber
		out.Students = append(out.Students, p)
		delete(runs, e.UserID)
	}
	if len(runs) > 0 {
		ids := make([]string, 0, len(runs))
		for id := range runs {
			ids = append(ids, id)
		}
		users, err := s.users.UsersByID(ctx, ids)
		if err != nil {
			return Progress{}, err
		}
		for id, p := range runs {
			p.Name, p.Email = users[id].Name, users[id].Email
			out.Students = append(out.Students, p)
		}
	}
	return out, nil
}

// AnswerDetail is one answer, for marking.
type AnswerDetail struct {
	Question quiz.Question  `json:"question"`
	Page     int            `json:"page"`
	Response *quiz.Response `json:"response,omitempty"`
	Score    float64        `json:"score"`
	Max      float64        `json:"max"`
	Correct  bool           `json:"correct"`
	Status   string         `json:"status,omitempty"` // marked, pending, or "" when unanswered
	Tries    int            `json:"tries"`
	Comment  string         `json:"comment,omitempty"`
	// AI marking (D-75): how it's marked, what the AI wrote, and whether
	// the teacher should look at it.
	Method     string `json:"method,omitempty"`
	AIFeedback string `json:"ai_feedback,omitempty"`
	AIMarked   bool   `json:"ai_marked"`
	Marking    bool   `json:"marking"`
	Flagged    bool   `json:"flagged"`
	FlagReason string `json:"flag_reason,omitempty"`
}

// RunPage is one student's time on a page and the times they left from it.
type RunPage struct {
	PageID       string `json:"page_id"`
	Index        int    `json:"index"`
	Title        string `json:"title"`
	Leaving      string `json:"leaving"`
	Visited      bool   `json:"visited"`
	Seconds      int    `json:"seconds"`
	Leaves       int    `json:"leaves"`
	AllowedLeave int    `json:"allowed_leaves"`
	AwaySeconds  int    `json:"away_seconds"`
	Locked       bool   `json:"locked"`
}

// RunDetail is one student's answers, page by page.
type RunDetail struct {
	RunID    string         `json:"run_id"`
	UserID   string         `json:"user_id"`
	Name     string         `json:"name"`
	Finished bool           `json:"finished"`
	Answers  []AnswerDetail `json:"answers"`
	Pages    []RunPage      `json:"pages"`
}

func (s *Service) ownedRun(ctx context.Context, teacherID, id, runID string) (Tutorial, *run, string, error) {
	t, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return t, nil, "", err
	}
	var userID string
	err = s.pool.QueryRow(ctx, `SELECT user_id FROM tutorial.runs WHERE id=$1 AND tutorial_id=$2`, runID, id).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, nil, "", httpx.ErrNotFound
	}
	if err != nil {
		return t, nil, "", err
	}
	r, err := s.loadRun(ctx, t, userID, false)
	return t, r, userID, err
}

// Run shows one student's answers to the teacher.
func (s *Service) Run(ctx context.Context, teacherID, id, runID string) (RunDetail, error) {
	_, r, userID, err := s.ownedRun(ctx, teacherID, id, runID)
	if err != nil {
		return RunDetail{}, err
	}
	users, err := s.users.UsersByID(ctx, []string{userID})
	if err != nil {
		return RunDetail{}, err
	}
	d := RunDetail{RunID: runID, UserID: userID, Name: users[userID].Name, Finished: r.finished != nil, Answers: []AnswerDetail{}, Pages: []RunPage{}}
	for i, p := range r.pages {
		v, seen := r.visits[p.ID]
		d.Pages = append(d.Pages, RunPage{PageID: p.ID, Index: i, Title: p.Title, Leaving: p.Leaving, Visited: seen, Seconds: v.Seconds,
			Leaves: v.Leaves, AllowedLeave: v.Allowed, AwaySeconds: v.Away, Locked: v.LockedAt != nil})
		for _, qid := range p.QuestionIDs {
			q, ok := r.qs[qid]
			if !ok {
				continue
			}
			ad := AnswerDetail{Question: q, Page: i, Max: q.Marks}
			if a, ok := r.answers[qid]; ok {
				resp := a.Response
				ad.Response, ad.Score, ad.Correct, ad.Status, ad.Tries, ad.Comment = &resp, a.Score, a.Correct, a.Status, a.Tries, a.Comment
				ad.Method, ad.AIFeedback, ad.AIMarked, ad.Flagged, ad.FlagReason = a.Method, a.AIFeedback, a.AIMarked, a.Flagged, a.FlagReason
				ad.Marking = a.Status == "pending" && a.Method == "llm" && !a.Flagged
			}
			d.Answers = append(d.Answers, ad)
		}
	}
	return d, nil
}

// MarkInput is the teacher's mark for one answer.
type MarkInput struct {
	Score   float64 `json:"score"`
	Comment string  `json:"comment"`
}

// Mark sets an answer's mark: one waiting for the teacher, or a change to
// one the key marked.
func (s *Service) Mark(ctx context.Context, teacherID, id, runID, questionID string, in MarkInput) (RunDetail, error) {
	_, r, _, err := s.ownedRun(ctx, teacherID, id, runID)
	if err != nil {
		return RunDetail{}, err
	}
	q, ok := r.qs[questionID]
	if _, answered := r.answers[questionID]; !ok || !answered {
		return RunDetail{}, httpx.ErrNotFound
	}
	in.Comment = strings.TrimSpace(in.Comment)
	if in.Score < -q.NegativeMarks || in.Score > q.Marks || math.IsNaN(in.Score) {
		return RunDetail{}, httpx.Invalid(map[string]string{"score": "between 0 and the question's marks"})
	}
	if runes(in.Comment) > 2000 {
		return RunDetail{}, httpx.Invalid(map[string]string{"comment": "at most 2000 characters"})
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE tutorial.answers SET score=$3, correct=$3 >= max_score, status='marked', comment=$4, flagged=false WHERE run_id=$1 AND question_id=$2`,
			runID, questionID, in.Score, in.Comment); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "tutorial_answer_marked", "tutorial_run", runID, map[string]any{"question": questionID, "score": in.Score})
	})
	if err != nil {
		return RunDetail{}, err
	}
	return s.Run(ctx, teacherID, id, runID)
}
