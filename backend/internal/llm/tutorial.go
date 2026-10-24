package llm

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/tutorial"
)

// Marking written answers in tutorials (D-75), with the same keys, limits,
// checks and pseudonyms as quiz answers.

// Tutorials is the slice of the tutorial module the gateway marks for.
type Tutorials interface {
	LLMItem(ctx context.Context, runID, questionID string) (tutorial.LLMItem, error)
	ApplyLLM(ctx context.Context, runID, questionID string, answeredAt time.Time, o eval.LLMOutcome) error
	LLMFailed(ctx context.Context, runID, questionID, reason string) error
}

func (s *Service) SetTutorials(t Tutorials) { s.tutorials = t }

type TutorialArgs struct {
	tutorial.LLMRequest
}

func (TutorialArgs) Kind() string { return "llm_tutorial_mark" }

// InsertOpts: as MarkArgs, unique per answer so a re-enqueue can't bill twice.
func (TutorialArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueLLM, MaxAttempts: 4, UniqueOpts: river.UniqueOpts{
		ByArgs: true,
		ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
			rivertype.JobStateRetryable, rivertype.JobStateScheduled},
	}}
}

// EnqueueTutorialTx implements tutorial.LLMQueue. Without a usable key it
// returns false, so the teacher marks the answer.
func (s *Service) EnqueueTutorialTx(ctx context.Context, tx pgx.Tx, req tutorial.LLMRequest) (bool, error) {
	k, err := s.resolveKey(ctx, tx, req.TeacherID, req.KeyID)
	if err != nil || k == nil {
		return false, err
	}
	req.KeyID = k.ID
	if _, err := s.jobs.InsertTx(ctx, tx, TutorialArgs{LLMRequest: req}, nil); err != nil {
		return false, err
	}
	return true, nil
}

type TutorialWorker struct {
	river.WorkerDefaults[TutorialArgs]
	Service *Service
}

func (w *TutorialWorker) Timeout(*river.Job[TutorialArgs]) time.Duration { return 120 * time.Second }

func (w *TutorialWorker) Work(ctx context.Context, job *river.Job[TutorialArgs]) error {
	return settle(w.Service.ProcessTutorial(ctx, job.Args.LLMRequest), job.Attempt >= job.MaxAttempts, func(reason string) error {
		return w.Service.tutorials.LLMFailed(ctx, job.Args.RunID, job.Args.QuestionID, reason)
	})
}

// ProcessTutorial marks one tutorial answer.
func (s *Service) ProcessTutorial(ctx context.Context, req tutorial.LLMRequest) error {
	it, err := s.tutorials.LLMItem(ctx, req.RunID, req.QuestionID)
	if errors.Is(err, httpx.ErrNotFound) {
		return nil // changed, marked by the teacher, or gone: nothing to mark
	}
	if err != nil {
		return err
	}
	k, err := s.resolveKey(ctx, s.pool, it.TeacherID, req.KeyID)
	if err != nil {
		return err
	}
	if k == nil {
		return &PermanentError{"the teacher's API key was removed"}
	}
	if !s.limiter.Allow(it.TeacherID) {
		return errRateLimited
	}
	gr := gradeRequest(pseudonym("tutorial:"+req.RunID), it.Question, &it.Response, false)
	res, err := s.call(ctx, k, req.Model, gr)
	if err != nil {
		return err
	}
	res, flagged, reason := Check(gr, res)
	return s.tutorials.ApplyLLM(ctx, req.RunID, req.QuestionID, it.AnsweredAt, eval.LLMOutcome{
		Score: res.Score, Feedback: res.Feedback, Rationale: res.Rationale, Flagged: flagged, FlagReason: reason})
}
