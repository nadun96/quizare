package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// Attempts is the slice of the live module the gateway needs.
type Attempts interface {
	MarkingData(ctx context.Context, attemptID string) (*live.MarkingData, error)
	SessionMarkingData(ctx context.Context, teacherID, sessionID string) ([]*live.MarkingData, error)
}

// Marks is the slice of the eval module the gateway writes through.
type Marks interface {
	ApplyLLM(ctx context.Context, attemptID, questionID, mode string, o eval.LLMOutcome) error
	LLMFailed(ctx context.Context, attemptID, questionID, mode, reason string) error
	MarkPendingTx(ctx context.Context, tx pgx.Tx, attemptID, questionID string, max float64) error
}

type Service struct {
	pool      *pgxpool.Pool
	vault     *Vault
	providers map[string]Provider
	jobs      jobs.Inserter
	attempts  Attempts
	marks     Marks
	log       *slog.Logger
	// NFR-16 / R6: per-teacher call rate limit, a simple circuit breaker
	// that protects teachers' quotas during bulk marking.
	limiter *auth.Limiter
}

func NewService(pool *pgxpool.Pool, vault *Vault, providers map[string]Provider, inserter jobs.Inserter, attempts Attempts, marks Marks, log *slog.Logger) *Service {
	return &Service{pool: pool, vault: vault, providers: providers, jobs: inserter, attempts: attempts, marks: marks, log: log,
		limiter: auth.NewLimiter(10, 2*time.Second)}
}

// DefaultProviders are the launch providers (Q-08).
func DefaultProviders() map[string]Provider {
	return map[string]Provider{"anthropic": Anthropic{}, "openai": OpenAI{}, "google": Google{}}
}

// ---------- keys (FR-EV-02, BR-10, AC-11) ----------

// Key is the masked view of a stored key. There is no way to read the secret
// back through the API: only set, test and delete (ADR-09).
type Key struct {
	ID            string     `json:"id"`
	Provider      string     `json:"provider"`
	Label         string     `json:"label"`
	Model         string     `json:"model"`
	Last4         string     `json:"last4"`
	IsDefault     bool       `json:"is_default"`
	LastTestAt    *time.Time `json:"last_test_at"`
	LastTestOK    *bool      `json:"last_test_ok"`
	LastTestError string     `json:"last_test_error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type KeyInput struct {
	Provider  string `json:"provider"`
	Label     string `json:"label"`
	Model     string `json:"model"`
	APIKey    string `json:"api_key"`
	IsDefault bool   `json:"is_default"`
}

const keyCols = `id, provider, label, model, last4, is_default, last_test_at, last_test_ok, last_test_error, created_at`

func scanKey(r pgx.Row) (Key, error) {
	var k Key
	err := r.Scan(&k.ID, &k.Provider, &k.Label, &k.Model, &k.Last4, &k.IsDefault, &k.LastTestAt, &k.LastTestOK, &k.LastTestError, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, httpx.ErrNotFound
	}
	return k, err
}

func (s *Service) ListKeys(ctx context.Context, teacherID string) ([]Key, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+keyCols+` FROM llm.keys WHERE teacher_id=$1 ORDER BY created_at`, teacherID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Key, error) { return scanKey(r) })
}

func (s *Service) AddKey(ctx context.Context, teacherID string, in KeyInput) (Key, error) {
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.APIKey = strings.TrimSpace(in.APIKey)
	in.Model = strings.TrimSpace(in.Model)
	f := map[string]string{}
	if _, ok := s.providers[in.Provider]; !ok {
		f["provider"] = "provider must be anthropic, openai or google"
	}
	if len(in.APIKey) < 8 || len(in.APIKey) > 500 {
		f["api_key"] = "paste the full API key"
	}
	if in.Model == "" {
		in.Model = DefaultModels[in.Provider]
	}
	if in.Model == "" || len(in.Model) > 100 {
		f["model"] = "choose a model"
	}
	if len(in.Label) > 100 {
		f["label"] = "at most 100 characters"
	}
	if len(f) > 0 {
		return Key{}, httpx.Invalid(f)
	}
	secret := []byte(in.APIKey)
	defer zero(secret)
	sealed, err := s.vault.Seal(secret, teacherID, in.Provider, 1)
	if err != nil {
		return Key{}, err
	}
	last4 := in.APIKey[len(in.APIKey)-4:]
	var k Key
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM llm.keys WHERE teacher_id=$1`, teacherID).Scan(&n); err != nil {
			return err
		}
		def := in.IsDefault || n == 0 // the first key becomes the default
		if def {
			if _, err := tx.Exec(ctx, `UPDATE llm.keys SET is_default=false WHERE teacher_id=$1`, teacherID); err != nil {
				return err
			}
		}
		var err error
		k, err = scanKey(tx.QueryRow(ctx, `INSERT INTO llm.keys(teacher_id, provider, label, model, ciphertext, nonce, wrapped_dek, dek_nonce, kek_id, last4, is_default)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+keyCols,
			teacherID, in.Provider, strings.TrimSpace(in.Label), in.Model, sealed.Ciphertext, sealed.Nonce, sealed.WrappedDEK, sealed.DEKNonce, sealed.KEKID, last4, def))
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "llm_key_added", "llm_key", k.ID, map[string]string{"provider": in.Provider})
	})
	return k, err
}

type KeyUpdate struct {
	Label     *string `json:"label"`
	Model     *string `json:"model"`
	IsDefault *bool   `json:"is_default"`
}

func (s *Service) UpdateKey(ctx context.Context, teacherID, id string, in KeyUpdate) (Key, error) {
	var k Key
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if in.IsDefault != nil && *in.IsDefault {
			if _, err := tx.Exec(ctx, `UPDATE llm.keys SET is_default=false WHERE teacher_id=$1`, teacherID); err != nil {
				return err
			}
		}
		var err error
		k, err = scanKey(tx.QueryRow(ctx, `UPDATE llm.keys SET label=coalesce($3,label), model=coalesce(nullif($4,''),model),
			is_default=coalesce($5,is_default) WHERE id=$1 AND teacher_id=$2 RETURNING `+keyCols, id, teacherID, in.Label, in.Model, in.IsDefault))
		return err
	})
	return k, err
}

func (s *Service) DeleteKey(ctx context.Context, teacherID, id string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM llm.keys WHERE id=$1 AND teacher_id=$2`, id, teacherID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(ctx, tx, teacherID, "llm_key_deleted", "llm_key", id, nil)
	})
}

type storedKey struct {
	Key
	teacherID string
	version   int
	sealed    Sealed
}

// resolveKey finds the key to use: the configured key id, else the teacher's default.
func (s *Service) resolveKey(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, teacherID, keyID string) (*storedKey, error) {
	var k storedKey
	err := q.QueryRow(ctx, `SELECT `+keyCols+`, teacher_id, key_version, ciphertext, nonce, wrapped_dek, dek_nonce, kek_id
		FROM llm.keys WHERE teacher_id=$1 AND (($2 <> '' AND id::text=$2) OR ($2 = '' AND is_default))`, teacherID, keyID).
		Scan(&k.ID, &k.Provider, &k.Label, &k.Model, &k.Last4, &k.IsDefault, &k.LastTestAt, &k.LastTestOK, &k.LastTestError, &k.CreatedAt,
			&k.teacherID, &k.version, &k.sealed.Ciphertext, &k.sealed.Nonce, &k.sealed.WrappedDEK, &k.sealed.DEKNonce, &k.sealed.KEKID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &k, err
}

// call decrypts the key only for the duration of one provider call and
// zeroes it afterwards (ADR-09).
func (s *Service) call(ctx context.Context, k *storedKey, model string, r GradeRequest) (GradeResult, error) {
	p, ok := s.providers[k.Provider]
	if !ok {
		return GradeResult{}, &PermanentError{"unsupported provider " + k.Provider}
	}
	secret, err := s.vault.Open(k.sealed, k.teacherID, k.Provider, k.version)
	if err != nil {
		return GradeResult{}, &PermanentError{err.Error()}
	}
	defer zero(secret)
	if model == "" {
		model = k.Model
	}
	return p.Grade(ctx, string(secret), model, r)
}

// TestKey sends a tiny marking request to check the key works.
func (s *Service) TestKey(ctx context.Context, teacherID, id string) (Key, error) {
	k, err := s.resolveKey(ctx, s.pool, teacherID, id)
	if err != nil {
		return Key{}, err
	}
	if k == nil || id == "" {
		return Key{}, httpx.ErrNotFound
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, callErr := s.call(cctx, k, "", GradeRequest{Pseudonym: "test", QuestionType: quiz.Essay, Question: "What is 2 + 2?",
		ModelAnswer: "4", Answer: "4", MaxScore: 1})
	msg := ""
	if callErr != nil {
		msg = truncate(callErr.Error(), 300)
	}
	return scanKey(s.pool.QueryRow(ctx, `UPDATE llm.keys SET last_test_at=now(), last_test_ok=$3, last_test_error=$4
		WHERE id=$1 AND teacher_id=$2 RETURNING `+keyCols, id, teacherID, callErr == nil, msg))
}

// ---------- queue (eval.LLMQueue) ----------

type MarkArgs struct {
	eval.LLMRequest
}

func (MarkArgs) Kind() string { return "llm_mark" }

// InsertOpts follow ADR-06 refinements: own queue with a small worker cap,
// MaxAttempts 4 instead of River's 25 (these calls cost the teacher money),
// and uniqueness by (attempt, question, mode) so a re-enqueue cannot bill twice.
func (MarkArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueLLM, MaxAttempts: 4, UniqueOpts: river.UniqueOpts{
		ByArgs: true,
		ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
			rivertype.JobStateRetryable, rivertype.JobStateScheduled},
	}}
}

// EnqueueTx implements eval.LLMQueue. Without a usable key it returns false
// so the answer is marked manually (BR-11).
func (s *Service) EnqueueTx(ctx context.Context, tx pgx.Tx, req eval.LLMRequest) (bool, error) {
	k, err := s.resolveKey(ctx, tx, req.TeacherID, req.KeyID)
	if err != nil || k == nil {
		return false, err
	}
	req.KeyID = k.ID
	if _, err := s.jobs.InsertTx(ctx, tx, MarkArgs{LLMRequest: req}, nil); err != nil {
		return false, err
	}
	return true, nil
}

// Remark re-runs LLM marking for one answer (FR-EV-09).
func (s *Service) Remark(ctx context.Context, teacherID, attemptID, questionID string) error {
	d, err := s.attempts.MarkingData(ctx, attemptID)
	if err != nil {
		return err
	}
	if d.TeacherID != teacherID {
		return httpx.ErrNotFound
	}
	var item *live.MarkingItem
	for i := range d.Items {
		if d.Items[i].Question.ID == questionID {
			item = &d.Items[i]
		}
	}
	if item == nil {
		return httpx.ErrNotFound
	}
	if !item.Question.Type.LLMAllowed() {
		return httpx.BadRequest("only essays and free-text blanks can be marked by an LLM")
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		ok, err := s.EnqueueTx(ctx, tx, eval.LLMRequest{TeacherID: teacherID, AttemptID: attemptID, QuestionID: questionID,
			Mode: "mark", KeyID: item.Effective.LLMKeyID, Model: item.Effective.LLMModel})
		if err != nil {
			return err
		}
		if !ok {
			return httpx.Conflict("add an LLM API key in your settings first")
		}
		if err := s.marks.MarkPendingTx(ctx, tx, attemptID, questionID, item.Question.Marks); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "llm_remark_requested", "answer", attemptID+"/"+questionID, nil)
	})
}

// Estimate reports how many answers in a session use LLM marking and a rough
// token count, shown before batch marking (NFR-16).
type Estimate struct {
	Answers         int `json:"answers"`
	EstimatedTokens int `json:"estimated_tokens"`
}

func (s *Service) Estimate(ctx context.Context, teacherID, sessionID string) (Estimate, error) {
	all, err := s.attempts.SessionMarkingData(ctx, teacherID, sessionID)
	if err != nil {
		return Estimate{}, err
	}
	var e Estimate
	for _, d := range all {
		for _, it := range d.Items {
			usesLLM := it.Question.Type == quiz.Essay || (it.Question.Type == quiz.BlankText && it.Effective.EvaluationMethod == "llm")
			wantsFeedback := it.Effective.FeedbackMode == "ai" || it.Effective.FeedbackMode == "both"
			if it.Response == nil || (!usesLLM && !wantsFeedback) || it.Effective.EvaluationMethod == "manual" {
				continue
			}
			e.Answers++
			e.EstimatedTokens += EstimateTokens(buildRequest(d, it, false))
		}
	}
	return e, nil
}

// ---------- worker ----------

func pseudonym(attemptID string) string {
	h := sha256.Sum256([]byte("pseudonym:" + attemptID))
	return "student-" + hex.EncodeToString(h[:3])
}

// answerText flattens a response into what the LLM reads.
func answerText(q quiz.Question, r *quiz.Response) string {
	if r == nil {
		return ""
	}
	switch q.Type {
	case quiz.Essay:
		return r.Text
	case quiz.BlankText:
		var parts []string
		for _, b := range q.Body.Blanks {
			parts = append(parts, fmt.Sprintf("Blank %s: %s", b, r.Blanks[b]))
		}
		return strings.Join(parts, "\n")
	default:
		// Objective answers, for AI feedback only: show chosen option texts.
		names := map[string]string{}
		for _, c := range append(append(append([]quiz.Choice{}, q.Body.Options...), q.Body.Left...), q.Body.Right...) {
			names[c.ID] = c.Text
		}
		var parts []string
		for _, id := range append(append([]string{}, r.Selected...), r.Order...) {
			parts = append(parts, names[id])
		}
		for k, v := range r.Pairs {
			parts = append(parts, names[k]+" -> "+names[v])
		}
		for b, v := range r.Blanks {
			if n, ok := names[v]; ok {
				v = n
			}
			parts = append(parts, fmt.Sprintf("Blank %s: %s", b, v))
		}
		return strings.Join(parts, "\n")
	}
}

func buildRequest(d *live.MarkingData, it live.MarkingItem, feedbackOnly bool) GradeRequest {
	q := it.Question
	model := q.Key.ModelAnswer
	if model == "" && q.Type == quiz.BlankText {
		var parts []string
		for _, b := range q.Body.Blanks {
			parts = append(parts, fmt.Sprintf("Blank %s: %s", b, strings.Join(q.Key.Blanks[b], " / ")))
		}
		model = strings.Join(parts, "\n")
	}
	// Scrub here, at the boundary, so no adapter ever holds personal data (ADR-16).
	return GradeRequest{Pseudonym: pseudonym(d.AttemptID), QuestionType: q.Type, Question: q.Text, ModelAnswer: model,
		Rubric: q.Key.Rubric, Answer: Scrub(answerText(q, it.Response)), MaxScore: q.Marks, FeedbackOnly: feedbackOnly, WantsFeedback: true}
}

type Worker struct {
	river.WorkerDefaults[MarkArgs]
	Service *Service
}

// Timeout overrides River's 1-minute default: provider calls can take 60-90 s (ADR-06).
func (w *Worker) Timeout(*river.Job[MarkArgs]) time.Duration { return 120 * time.Second }

func (w *Worker) Work(ctx context.Context, job *river.Job[MarkArgs]) error {
	err := w.Service.Process(ctx, job.Args.LLMRequest)
	var perm *PermanentError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &perm):
		if ferr := w.Service.marks.LLMFailed(ctx, job.Args.AttemptID, job.Args.QuestionID, job.Args.Mode, perm.Reason); ferr != nil {
			return ferr
		}
		return river.JobCancel(err)
	case errors.Is(err, errRateLimited):
		return river.JobSnooze(15 * time.Second)
	default:
		if job.Attempt >= job.MaxAttempts {
			if ferr := w.Service.marks.LLMFailed(ctx, job.Args.AttemptID, job.Args.QuestionID, job.Args.Mode, truncate(err.Error(), 200)); ferr != nil {
				return ferr
			}
		}
		return err // River retries with backoff
	}
}

var errRateLimited = errors.New("teacher LLM rate limit reached")

// Process marks or writes feedback for one answer.
func (s *Service) Process(ctx context.Context, req eval.LLMRequest) error {
	d, err := s.attempts.MarkingData(ctx, req.AttemptID)
	if err != nil {
		if errors.Is(err, httpx.ErrNotFound) {
			return &PermanentError{"the attempt no longer exists"}
		}
		return err
	}
	var item *live.MarkingItem
	for i := range d.Items {
		if d.Items[i].Question.ID == req.QuestionID {
			item = &d.Items[i]
		}
	}
	if item == nil {
		return &PermanentError{"the question is not part of this attempt"}
	}
	k, err := s.resolveKey(ctx, s.pool, d.TeacherID, req.KeyID)
	if err != nil {
		return err
	}
	if k == nil {
		return &PermanentError{"the teacher's API key was removed"}
	}
	if !s.limiter.Allow(d.TeacherID) {
		return errRateLimited
	}
	feedbackOnly := req.Mode == "feedback"
	gr := buildRequest(d, *item, feedbackOnly)
	if feedbackOnly {
		gr.KnownScore = eval.MarkByKey(item.Question, item.Response).Score
	}
	res, err := s.call(ctx, k, req.Model, gr)
	if err != nil {
		return err
	}
	res, flagged, reason := Check(gr, res)
	return s.marks.ApplyLLM(ctx, req.AttemptID, req.QuestionID, req.Mode, eval.LLMOutcome{
		Score: res.Score, Feedback: res.Feedback, Rationale: res.Rationale, Flagged: flagged, FlagReason: reason})
}
