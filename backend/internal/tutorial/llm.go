package tutorial

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// AI marking of written answers in tutorials (D-75, TU-FR-30): the teacher's
// own LLM key marks them, as in quizzes (ADR-09, ADR-16). Until it has, the
// answer waits; if it can't, the answer waits for the teacher instead.

// LLMRequest names one tutorial answer for the LLM gateway.
type LLMRequest struct {
	TeacherID  string `json:"teacher_id"`
	RunID      string `json:"run_id"`
	QuestionID string `json:"question_id"`
	KeyID      string `json:"key_id"`
	Model      string `json:"model"`
}

// LLMQueue schedules AI marking in the caller's transaction. ok=false means
// the teacher has no usable key, so the teacher marks the answer.
type LLMQueue interface {
	EnqueueTutorialTx(ctx context.Context, tx pgx.Tx, req LLMRequest) (ok bool, err error)
}

func (s *Service) SetLLM(q LLMQueue) { s.llm = q }

// LLMItem is the answer the AI marks.
type LLMItem struct {
	TeacherID  string
	Question   quiz.Question
	Response   quiz.Response
	AnsweredAt time.Time // the answer marked: a changed answer isn't marked with the old one's result
}

// LLMItem returns an answer still waiting for the AI, or ErrNotFound when
// it isn't (changed, marked by the teacher, or gone).
func (s *Service) LLMItem(ctx context.Context, runID, questionID string) (LLMItem, error) {
	var it LLMItem
	var quizID string
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT t.teacher_id, t.quiz_id, a.response, a.answered_at
		FROM tutorial.answers a JOIN tutorial.runs r ON r.id=a.run_id JOIN tutorial.tutorials t ON t.id=r.tutorial_id
		WHERE a.run_id=$1 AND a.question_id=$2 AND a.status='pending' AND a.method='llm' AND NOT a.flagged`, runID, questionID).
		Scan(&it.TeacherID, &quizID, &raw, &it.AnsweredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, httpx.ErrNotFound
	}
	if err != nil {
		return it, err
	}
	if err := json.Unmarshal(raw, &it.Response); err != nil {
		return it, err
	}
	qs, _, err := s.questions.TutorialQuestions(ctx, quizID)
	if err != nil {
		return it, err
	}
	for _, q := range qs {
		if q.ID == questionID {
			it.Question = q
			return it, nil
		}
	}
	return it, httpx.ErrNotFound
}

// ApplyLLM stores the AI's mark, if the answer is still the one it marked
// and still waiting. A flagged mark stands, and the teacher sees the flag.
func (s *Service) ApplyLLM(ctx context.Context, runID, questionID string, answeredAt time.Time, o eval.LLMOutcome) error {
	_, err := s.pool.Exec(ctx, `UPDATE tutorial.answers SET status='marked', score=LEAST(GREATEST($4, 0), max_score), correct=$4 >= max_score,
			ai_feedback=$5, ai_marked=true, flagged=$6, flag_reason=$7
		WHERE run_id=$1 AND question_id=$2 AND answered_at=$3 AND status='pending' AND method='llm'`,
		runID, questionID, answeredAt, math.Round(o.Score*100)/100, o.Feedback, o.Flagged, o.FlagReason)
	return err
}

// LLMFailed leaves the answer for the teacher to mark, saying why.
func (s *Service) LLMFailed(ctx context.Context, runID, questionID, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE tutorial.answers SET method='manual', flagged=true, flag_reason=$3
		WHERE run_id=$1 AND question_id=$2 AND status='pending' AND method='llm'`, runID, questionID, "AI marking failed: "+reason)
	return err
}
