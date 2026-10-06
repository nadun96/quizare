package eval

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
)

// Mark statuses.
const (
	StatusMarked      = "marked"
	StatusPending     = "pending" // queued for the LLM
	StatusNeedsManual = "needs_manual"
	StatusOverridden  = "overridden"
)

// Attempts is the slice of the live module evaluation needs.
type Attempts interface {
	MarkingData(ctx context.Context, attemptID string) (*live.MarkingData, error)
	SessionMarkingData(ctx context.Context, teacherID, sessionID string) ([]*live.MarkingData, error)
	OwnMarkingData(ctx context.Context, userID, attemptID string) (*live.MarkingData, error)
	AttemptIDs(ctx context.Context, sessionID string, states ...string) ([]string, error)
	TeamRules(ctx context.Context, teacherID, sessionID string) (settings.Effective, error)
	SessionTeamRules(ctx context.Context, sessionID string) (settings.Effective, error)
	TeamInfos(ctx context.Context, sessionID string) ([]live.TeamInfo, error)
	SessionMaxScore(ctx context.Context, sessionID string) (float64, error)
}

// LLMQueue schedules LLM marking or feedback in the caller's transaction.
// ok=false means the teacher has no usable key, so the answer falls back to
// manual marking (BR-11).
type LLMQueue interface {
	EnqueueTx(ctx context.Context, tx pgx.Tx, req LLMRequest) (ok bool, err error)
}

// LLMRequest identifies one answer for the LLM gateway.
type LLMRequest struct {
	TeacherID  string `json:"teacher_id"`
	AttemptID  string `json:"attempt_id"`
	QuestionID string `json:"question_id"`
	Mode       string `json:"mode"` // "mark" (score + feedback) or "feedback" (feedback only)
	KeyID      string `json:"key_id"`
	Model      string `json:"model"`
}

// Hook lets the analytics module react when results change (ADR-15).
type Hook func(ctx context.Context, tx pgx.Tx, sessionID string) error

type Service struct {
	pool      *pgxpool.Pool
	attempts  Attempts
	jobs      jobs.Inserter
	llm       LLMQueue
	onResults Hook
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool, attempts Attempts, inserter jobs.Inserter) *Service {
	return &Service{pool: pool, attempts: attempts, jobs: inserter, now: time.Now}
}

func (s *Service) SetLLM(q LLMQueue)     { s.llm = q }
func (s *Service) SetResultsHook(h Hook) { s.onResults = h }

// ---------- jobs ----------

type EvaluateAttemptArgs struct {
	AttemptID string `json:"attempt_id"`
}

func (EvaluateAttemptArgs) Kind() string { return "evaluate_attempt" }
func (EvaluateAttemptArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Queue: jobs.QueueDefault}
}

type EvaluateWorker struct {
	river.WorkerDefaults[EvaluateAttemptArgs]
	Service *Service
}

func (w *EvaluateWorker) Work(ctx context.Context, job *river.Job[EvaluateAttemptArgs]) error {
	return w.Service.EvaluateAttempt(ctx, job.Args.AttemptID)
}

// OnAttemptFinished is the live-module hook: marking is queued in the same
// transaction that submits the attempt, so it is never lost (ADR-06).
func (s *Service) OnAttemptFinished(ctx context.Context, tx pgx.Tx, _ string, attemptID string) error {
	_, err := s.jobs.InsertTx(ctx, tx, EvaluateAttemptArgs{AttemptID: attemptID}, nil)
	return err
}

// OnSessionEnded queues marking for invalidated attempts so the teacher can
// still review what those students answered.
func (s *Service) OnSessionEnded(ctx context.Context, tx pgx.Tx, sessionID string) error {
	rows, err := tx.Query(ctx, `SELECT a.id FROM live.attempts a WHERE a.session_id=$1 AND a.state='invalidated'
		AND NOT EXISTS (SELECT 1 FROM eval.results r WHERE r.attempt_id=a.id)`, sessionID)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.jobs.InsertTx(ctx, tx, EvaluateAttemptArgs{AttemptID: id}, nil); err != nil {
			return err
		}
	}
	return nil
}

// ---------- marking ----------

// method resolves how a question is marked (BA §7 evaluation method; D-14).
func method(q quiz.Question, configured string) string {
	switch {
	case configured == "manual":
		return "manual"
	case q.Type == quiz.Essay:
		return "llm" // essays cannot be key-marked
	case configured == "llm" && q.Type.LLMAllowed():
		return "llm"
	default:
		return "key"
	}
}

type markRow struct {
	method, status string
	score          *float64
	max            float64
	fraction       *float64
	correct        *bool
	feedback       string
}

func f64(v float64) *float64 { return &v }
func bptr(v bool) *bool      { return &v }

// EvaluateAttempt marks every question of a finished attempt (UC-04 step 1-3).
// It is idempotent: re-running keeps teacher overrides and completed AI marks.
func (s *Service) EvaluateAttempt(ctx context.Context, attemptID string) error {
	d, err := s.attempts.MarkingData(ctx, attemptID)
	if err != nil {
		return err
	}
	if d.State != live.StateSubmitted && d.State != live.StateInvalidated {
		return nil
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		type prev struct {
			status   string
			aiMarked bool
		}
		existing := map[string]prev{}
		rows, err := tx.Query(ctx, `SELECT question_id, status, ai_marked FROM eval.marks WHERE attempt_id=$1 FOR UPDATE`, attemptID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var qid string
			var p prev
			if err := rows.Scan(&qid, &p.status, &p.aiMarked); err != nil {
				rows.Close()
				return err
			}
			existing[qid] = p
		}
		rows.Close()

		for _, it := range d.Items {
			q, eff := it.Question, it.Effective
			// Keep teacher overrides, queued LLM work and finished AI marks.
			if p := existing[q.ID]; p.status == StatusOverridden || p.status == StatusPending || p.aiMarked {
				continue
			}
			m := markRow{method: method(q, eff.EvaluationMethod), max: q.Marks}
			answered := Answered(it.Response)
			wantAIFeedback := answered && (eff.FeedbackMode == "ai" || eff.FeedbackMode == "both")
			wantPredef := eff.FeedbackMode == "predefined" || eff.FeedbackMode == "both"
			switch {
			case !answered:
				// Nothing to judge: unanswered scores 0 by any method.
				m.status, m.score, m.fraction, m.correct = StatusMarked, f64(0), f64(0), bptr(false)
				if wantPredef {
					m.feedback = PredefinedFeedback(q, nil, 0, false)
				}
			case m.method == "key":
				r := MarkByKey(q, it.Response)
				m.status, m.score, m.fraction, m.correct = StatusMarked, f64(r.Score), f64(r.Fraction), bptr(r.Correct)
				if wantPredef {
					m.feedback = PredefinedFeedback(q, it.Response, r.Fraction, r.Correct)
				}
			case m.method == "llm":
				m.status = StatusNeedsManual // BR-11 unless a key is available
				if s.llm != nil {
					ok, err := s.llm.EnqueueTx(ctx, tx, LLMRequest{TeacherID: d.TeacherID, AttemptID: attemptID, QuestionID: q.ID,
						Mode: "mark", KeyID: eff.LLMKeyID, Model: eff.LLMModel})
					if err != nil {
						return err
					}
					if ok {
						m.status = StatusPending
					}
				}
				wantAIFeedback = false // marking also writes feedback
			default:
				m.status = StatusNeedsManual
			}
			if err := upsertMark(ctx, tx, attemptID, q.ID, m); err != nil {
				return err
			}
			if wantAIFeedback && s.llm != nil {
				if _, err := s.llm.EnqueueTx(ctx, tx, LLMRequest{TeacherID: d.TeacherID, AttemptID: attemptID, QuestionID: q.ID,
					Mode: "feedback", KeyID: eff.LLMKeyID, Model: eff.LLMModel}); err != nil {
					return err
				}
			}
		}
		return s.recompute(ctx, tx, d)
	})
}

func upsertMark(ctx context.Context, tx pgx.Tx, attemptID, questionID string, m markRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO eval.marks(attempt_id, question_id, method, status, score, max_score, fraction, correct, feedback, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
		ON CONFLICT (attempt_id, question_id) DO UPDATE SET method=EXCLUDED.method, status=EXCLUDED.status, score=EXCLUDED.score,
			max_score=EXCLUDED.max_score, fraction=EXCLUDED.fraction, correct=EXCLUDED.correct, feedback=EXCLUDED.feedback, updated_at=now()`,
		attemptID, questionID, m.method, m.status, m.score, m.max, m.fraction, m.correct, m.feedback)
	return err
}

// recompute totals an attempt. The total is floored at 0 so negative marking
// cannot make an attempt worse than blank (D-24).
func (s *Service) recompute(ctx context.Context, tx pgx.Tx, d *live.MarkingData) error {
	var score, max float64
	var outstanding int
	err := tx.QueryRow(ctx, `SELECT coalesce(sum(score) FILTER (WHERE status IN ('marked','overridden')),0),
		count(*) FILTER (WHERE status IN ('pending','needs_manual'))
		FROM eval.marks WHERE attempt_id=$1`, d.AttemptID).Scan(&score, &outstanding)
	if err != nil {
		return err
	}
	for _, it := range d.Items {
		max += it.Question.Marks
	}
	score = math.Max(0, score)
	pct := 0.0
	if max > 0 {
		pct = math.Round(score/max*10000) / 100
	}
	pass := d.Session.PassMarkPct
	invalid := d.State == live.StateInvalidated
	_, err = tx.Exec(ctx, `INSERT INTO eval.results(attempt_id, session_id, user_id, score, max_score, pct, pass_mark_pct, passed, complete, invalidated, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now())
		ON CONFLICT (attempt_id) DO UPDATE SET score=EXCLUDED.score, max_score=EXCLUDED.max_score, pct=EXCLUDED.pct,
			pass_mark_pct=EXCLUDED.pass_mark_pct, passed=EXCLUDED.passed, complete=EXCLUDED.complete, invalidated=EXCLUDED.invalidated, updated_at=now()`,
		d.AttemptID, d.SessionID, d.UserID, score, max, pct, pass, !invalid && pct >= float64(pass), outstanding == 0, invalid)
	if err != nil {
		return err
	}
	if s.onResults != nil {
		return s.onResults(ctx, tx, d.SessionID)
	}
	return nil
}

// Recompute reloads an attempt and recomputes its total (used after LLM marks).
func (s *Service) Recompute(ctx context.Context, tx pgx.Tx, attemptID string) error {
	d, err := s.attempts.MarkingData(ctx, attemptID)
	if err != nil {
		return err
	}
	return s.recompute(ctx, tx, d)
}

// ---------- teacher review (FR-EV-06) ----------

type MarkView struct {
	QuestionID  string          `json:"question_id"`
	Code        string          `json:"code"`
	Type        quiz.Type       `json:"type"`
	Text        string          `json:"text"`
	Format      string          `json:"format,omitempty"` // text format of the question and feedback
	Response    *quiz.Response  `json:"response"`
	Key         *quiz.Key       `json:"key,omitempty"`
	Method      string          `json:"method"`
	Status      string          `json:"status"`
	Score       *float64        `json:"score"`
	MaxScore    float64         `json:"max_score"`
	Correct     *bool           `json:"correct"`
	Feedback    string          `json:"feedback"`
	AIFeedback  string          `json:"ai_feedback"`
	AIRationale string          `json:"ai_rationale,omitempty"`
	AIMarked    bool            `json:"ai_marked"`
	Flagged     bool            `json:"flagged"`
	FlagReason  string          `json:"flag_reason,omitempty"`
	Resources   []quiz.Resource `json:"resources,omitempty"`
}

type AttemptResult struct {
	AttemptID     string     `json:"attempt_id"`
	UserID        string     `json:"user_id"`
	StudentNumber *string    `json:"student_number"`
	State         string     `json:"state"`
	Score         float64    `json:"score"`
	MaxScore      float64    `json:"max_score"`
	Pct           float64    `json:"pct"`
	PassMarkPct   int        `json:"pass_mark_pct"`
	Passed        bool       `json:"passed"`
	Complete      bool       `json:"complete"`
	Invalidated   bool       `json:"invalidated"`
	Marks         []MarkView `json:"marks"`
}

func (s *Service) loadMarks(ctx context.Context, attemptID string) (map[string]MarkView, error) {
	rows, err := s.pool.Query(ctx, `SELECT question_id, method, status, score, max_score, correct, feedback, ai_feedback, ai_rationale,
		ai_marked, flagged, flag_reason FROM eval.marks WHERE attempt_id=$1`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]MarkView{}
	for rows.Next() {
		var m MarkView
		if err := rows.Scan(&m.QuestionID, &m.Method, &m.Status, &m.Score, &m.MaxScore, &m.Correct, &m.Feedback, &m.AIFeedback,
			&m.AIRationale, &m.AIMarked, &m.Flagged, &m.FlagReason); err != nil {
			return nil, err
		}
		out[m.QuestionID] = m
	}
	return out, rows.Err()
}

func (s *Service) result(ctx context.Context, d *live.MarkingData) (AttemptResult, error) {
	r := AttemptResult{AttemptID: d.AttemptID, UserID: d.UserID, StudentNumber: d.StudentNumber, State: d.State, Marks: []MarkView{}}
	err := s.pool.QueryRow(ctx, `SELECT score, max_score, pct, pass_mark_pct, passed, complete, invalidated FROM eval.results WHERE attempt_id=$1`, d.AttemptID).
		Scan(&r.Score, &r.MaxScore, &r.Pct, &r.PassMarkPct, &r.Passed, &r.Complete, &r.Invalidated)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return r, err
	}
	marks, err := s.loadMarks(ctx, d.AttemptID)
	if err != nil {
		return r, err
	}
	for _, it := range d.Items {
		m, ok := marks[it.Question.ID]
		if !ok {
			m = MarkView{QuestionID: it.Question.ID, Status: StatusPending, MaxScore: it.Question.Marks}
		}
		m.Code, m.Type, m.Text, m.Format, m.Response = it.Question.Code, it.Question.Type, it.Question.Text, it.Question.Body.Format, it.Response
		key := it.Question.Key
		m.Key = &key
		m.Resources = it.Question.Resources
		r.Marks = append(r.Marks, m)
	}
	return r, nil
}

// SessionResults lists every finished attempt with its marks for review.
func (s *Service) SessionResults(ctx context.Context, teacherID, sessionID string) ([]AttemptResult, error) {
	all, err := s.attempts.SessionMarkingData(ctx, teacherID, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]AttemptResult, 0, len(all))
	for _, d := range all {
		r, err := s.result(ctx, d)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

type OverrideInput struct {
	Score    *float64 `json:"score"`
	Feedback *string  `json:"feedback"`
}

// Override sets a teacher mark and/or feedback; overrides are final and audited
// (FR-EV-06, ADR-16 audit log, teacher remains the final authority).
func (s *Service) Override(ctx context.Context, teacherID, attemptID, questionID string, in OverrideInput) (MarkView, error) {
	d, err := s.attempts.MarkingData(ctx, attemptID)
	if err != nil {
		return MarkView{}, err
	}
	if d.TeacherID != teacherID {
		return MarkView{}, httpx.ErrNotFound
	}
	var q *quiz.Question
	for i := range d.Items {
		if d.Items[i].Question.ID == questionID {
			q = &d.Items[i].Question
		}
	}
	if q == nil {
		return MarkView{}, httpx.ErrNotFound
	}
	if d.State != live.StateSubmitted && d.State != live.StateInvalidated {
		return MarkView{}, httpx.Conflict("the attempt has not finished yet")
	}
	if in.Score == nil && in.Feedback == nil {
		return MarkView{}, httpx.BadRequest("give a score and/or feedback")
	}
	if in.Score != nil && (*in.Score < -q.NegativeMarks || *in.Score > q.Marks) {
		return MarkView{}, httpx.Invalid(map[string]string{"score": "score must be between the negative mark and the question's marks"})
	}
	if in.Feedback != nil && len(*in.Feedback) > 5000 {
		return MarkView{}, httpx.Invalid(map[string]string{"feedback": "at most 5000 characters"})
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var score any
		if in.Score != nil {
			score = math.Round(*in.Score*100) / 100
		}
		var fb any
		if in.Feedback != nil {
			fb = strings.TrimSpace(*in.Feedback)
		}
		_, err := tx.Exec(ctx, `INSERT INTO eval.marks(attempt_id, question_id, method, status, score, max_score, feedback, overridden_by, overridden_at)
			VALUES ($1,$2,'manual','overridden',$3,$4,coalesce($5,''),$6,now())
			ON CONFLICT (attempt_id, question_id) DO UPDATE SET
				score = CASE WHEN $3::numeric IS NULL THEN eval.marks.score ELSE $3 END,
				status = CASE WHEN $3::numeric IS NULL AND eval.marks.status <> 'overridden' THEN eval.marks.status ELSE 'overridden' END,
				feedback = coalesce($5, eval.marks.feedback),
				correct = CASE WHEN $3::numeric IS NULL THEN eval.marks.correct ELSE $3::numeric >= eval.marks.max_score END,
				flagged = false, overridden_by=$6, overridden_at=now(), updated_at=now()`,
			attemptID, questionID, score, q.Marks, fb, teacherID)
		if err != nil {
			return err
		}
		if err := audit.Log(ctx, tx, teacherID, "mark_overridden", "answer", attemptID+"/"+questionID, in); err != nil {
			return err
		}
		return s.recompute(ctx, tx, d)
	})
	if err != nil {
		return MarkView{}, err
	}
	marks, err := s.loadMarks(ctx, attemptID)
	if err != nil {
		return MarkView{}, err
	}
	return marks[questionID], nil
}

// ---------- student results (FR-EV-08, BR-12) ----------

// StudentResult is what a student sees after release. Each part is shown
// only if the teacher's settings allow it (BA §11 student-facing results).
type StudentResult struct {
	AttemptID     string            `json:"attempt_id"`
	SessionTitle  string            `json:"session_title"`
	QuizTitle     string            `json:"quiz_title"`
	State         string            `json:"state"`
	StudentNumber *string           `json:"student_number"`
	Score         float64           `json:"score"`
	MaxScore      float64           `json:"max_score"`
	Pct           float64           `json:"pct"`
	PassMarkPct   int               `json:"pass_mark_pct"`
	Passed        bool              `json:"passed"`
	Complete      bool              `json:"complete"` // false: "marking in progress" badge (ADR-15)
	Questions     []StudentQuestion `json:"questions"`
	Team          *StudentTeam      `json:"team,omitempty"` // D-44
}

type StudentQuestion struct {
	Question          quiz.StudentQuestion `json:"question"`
	Response          *quiz.Response       `json:"response,omitempty"`
	Key               *quiz.Key            `json:"correct_answer,omitempty"`
	Score             *float64             `json:"score"`
	MaxScore          float64              `json:"max_score"`
	Status            string               `json:"status"`
	Correct           *bool                `json:"correct"`
	Feedback          string               `json:"feedback,omitempty"`
	AIFeedback        string               `json:"ai_feedback,omitempty"`
	AIMarked          bool                 `json:"ai_marked"` // AI-mark badge (R7)
	FeedbackResources []quiz.Resource      `json:"feedback_resources,omitempty"`
}

func (s *Service) StudentResult(ctx context.Context, userID, attemptID string) (StudentResult, error) {
	d, err := s.attempts.OwnMarkingData(ctx, userID, attemptID)
	if err != nil {
		return StudentResult{}, err
	}
	if !d.Released {
		return StudentResult{}, live.ErrNotReleased()
	}
	full, err := s.result(ctx, d)
	if err != nil {
		return StudentResult{}, err
	}
	eff := d.Session
	out := StudentResult{AttemptID: d.AttemptID, SessionTitle: d.SessionTitle, QuizTitle: d.QuizTitle, State: d.State,
		StudentNumber: d.StudentNumber, Score: full.Score, MaxScore: full.MaxScore, Pct: full.Pct, PassMarkPct: full.PassMarkPct,
		Passed: full.Passed, Complete: full.Complete}
	for i, m := range full.Marks {
		q := d.Items[i].Question
		sq := StudentQuestion{Question: q.StudentView(), MaxScore: m.MaxScore, Status: m.Status, AIMarked: m.AIMarked}
		if m.Status == StatusMarked || m.Status == StatusOverridden {
			sq.Score, sq.Correct = m.Score, m.Correct
		}
		if eff.ResultsShowAnswers {
			sq.Response = m.Response
		}
		if eff.ResultsShowCorrect {
			k := q.Key
			k.Rubric = "" // the rubric is the teacher's marking guide, not an answer
			sq.Key = &k
		}
		if eff.ResultsShowFeedback {
			sq.Feedback, sq.AIFeedback = m.Feedback, m.AIFeedback
			for _, r := range q.Resources {
				if r.Role == quiz.RoleFeedback {
					sq.FeedbackResources = append(sq.FeedbackResources, r.Public())
				}
			}
		}
		out.Questions = append(out.Questions, sq)
	}
	if out.Team, err = s.studentTeam(ctx, d.SessionID, d.AttemptID); err != nil {
		return out, err
	}
	return out, nil
}

// ---------- LLM results (written by the LLM gateway) ----------

// LLMOutcome is a checked provider result for one answer.
type LLMOutcome struct {
	Score      float64
	Feedback   string
	Rationale  string
	Flagged    bool
	FlagReason string
}

// ApplyLLM stores an AI mark ("mark") or AI feedback ("feedback"). A teacher
// override is never replaced: the teacher is the final authority (ADR-16).
func (s *Service) ApplyLLM(ctx context.Context, attemptID, questionID, mode string, o LLMOutcome) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var max float64
		err := tx.QueryRow(ctx, `SELECT status, max_score FROM eval.marks WHERE attempt_id=$1 AND question_id=$2 FOR UPDATE`, attemptID, questionID).Scan(&status, &max)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if mode == "feedback" || status == StatusOverridden {
			_, err = tx.Exec(ctx, `UPDATE eval.marks SET ai_feedback=$3, updated_at=now() WHERE attempt_id=$1 AND question_id=$2`, attemptID, questionID, o.Feedback)
			return err
		}
		frac := 0.0
		if max > 0 {
			frac = o.Score / max
		}
		if _, err := tx.Exec(ctx, `UPDATE eval.marks SET status='marked', score=$3, fraction=$4, correct=$5, ai_feedback=$6, ai_rationale=$7,
			ai_marked=true, flagged=$8, flag_reason=$9, updated_at=now() WHERE attempt_id=$1 AND question_id=$2`,
			attemptID, questionID, o.Score, frac, o.Score >= max, o.Feedback, o.Rationale, o.Flagged, o.FlagReason); err != nil {
			return err
		}
		return s.Recompute(ctx, tx, attemptID)
	})
}

// LLMFailed sends an answer to manual marking after a permanent or final
// failure, and notifies the teacher through the flag (UC-04 exception).
func (s *Service) LLMFailed(ctx context.Context, attemptID, questionID, mode, reason string) error {
	if mode == "feedback" {
		return nil // the mark stands; only the AI feedback is missing
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE eval.marks SET status='needs_manual', flagged=true, flag_reason=$3, updated_at=now()
			WHERE attempt_id=$1 AND question_id=$2 AND status='pending'`, attemptID, questionID, "AI marking failed: "+reason)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return s.Recompute(ctx, tx, attemptID)
	})
}

// MarkPendingTx resets a mark to pending before an LLM re-run (FR-EV-09).
func (s *Service) MarkPendingTx(ctx context.Context, tx pgx.Tx, attemptID, questionID string, max float64) error {
	_, err := tx.Exec(ctx, `INSERT INTO eval.marks(attempt_id, question_id, method, status, max_score) VALUES ($1,$2,'llm','pending',$3)
		ON CONFLICT (attempt_id, question_id) DO UPDATE SET method='llm', status='pending', score=NULL, ai_marked=false,
			flagged=false, flag_reason='', overridden_by=NULL, overridden_at=NULL, updated_at=now()`, attemptID, questionID, max)
	return err
}
