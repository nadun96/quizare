package quiz

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Question pools and the question bank (D-66). A question's category puts it
// in a pool; a quiz's pool rules say how many questions each student draws
// from each pool, either of the quiz's own questions or of the teacher's
// question bank. A quiz question whose category has no rule is always asked.

const (
	KindQuiz     = "quiz"
	KindBank     = "bank"
	KindTutorial = "tutorial" // a tutorial's questions (D-74)

	SourceQuiz = "quiz"
	SourceBank = "bank"
)

// PoolRule draws Count questions (0 = all) from one category of a source.
type PoolRule struct {
	Source   string `json:"source"`
	Category string `json:"category"`
	Count    int    `json:"count"`
}

var (
	errBankNotAQuiz     = httpx.NewError(http.StatusConflict, "question_bank", "the question bank isn't a quiz: draw from it through a quiz's pools")
	errTutorialNotAQuiz = httpx.NewError(http.StatusConflict, "tutorial_questions", "these are a tutorial's questions: change the tutorial instead")
)

// notAQuiz refuses what only a quiz can do (pools, status, deleting) for the
// question bank and for a tutorial's questions.
func (s *Service) notAQuiz(ctx context.Context, id string) error {
	var kind string
	_ = s.pool.QueryRow(ctx, `SELECT kind FROM quiz.quizzes WHERE id=$1`, id).Scan(&kind)
	switch kind {
	case KindBank:
		return errBankNotAQuiz
	case KindTutorial:
		return errTutorialNotAQuiz
	}
	return nil
}

// Bank returns the teacher's question bank, creating it on first use.
func (s *Service) Bank(ctx context.Context, teacherID string) (Quiz, error) {
	if _, err := s.pool.Exec(ctx, `INSERT INTO quiz.quizzes(topic_id, teacher_id, title, kind) VALUES (NULL, $1, 'Question bank', 'bank')
		ON CONFLICT (teacher_id) WHERE kind = 'bank' DO NOTHING`, teacherID); err != nil {
		return Quiz{}, err
	}
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT id FROM quiz.quizzes WHERE teacher_id=$1 AND kind='bank'`, teacherID).Scan(&id); err != nil {
		return Quiz{}, err
	}
	return s.GetQuiz(ctx, teacherID, id)
}

// PoolCount is one category of a source and what it holds.
type PoolCount struct {
	Category string  `json:"category"`
	Count    int     `json:"count"`
	Marks    float64 `json:"marks"`
}

// PoolInfo is what the pools editor needs: the rules, and the categories
// of the quiz and of the bank with how many questions each holds.
type PoolInfo struct {
	Pools  []PoolRule  `json:"pools"`
	Quiz   []PoolCount `json:"quiz"`
	Bank   []PoolCount `json:"bank"`
	BankID string      `json:"bank_id"`
}

func (s *Service) categories(ctx context.Context, quizID string) ([]PoolCount, error) {
	rows, err := s.pool.Query(ctx, `SELECT category, count(*), coalesce(sum(marks), 0)::float8 FROM quiz.questions WHERE quiz_id=$1
		GROUP BY category ORDER BY category = '', lower(category)`, quizID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PoolCount, error) {
		var c PoolCount
		return c, r.Scan(&c.Category, &c.Count, &c.Marks)
	})
	if out == nil {
		out = []PoolCount{}
	}
	return out, err
}

// Pools returns a quiz's rules and the categories it can draw from.
func (s *Service) Pools(ctx context.Context, teacherID, quizID string) (PoolInfo, error) {
	q, err := s.GetQuiz(ctx, teacherID, quizID)
	if err != nil {
		return PoolInfo{}, err
	}
	if q.Kind == KindBank {
		return PoolInfo{}, errBankNotAQuiz
	}
	bank, err := s.Bank(ctx, teacherID)
	if err != nil {
		return PoolInfo{}, err
	}
	info := PoolInfo{Pools: q.Pools, BankID: bank.ID}
	if info.Quiz, err = s.categories(ctx, q.ID); err != nil {
		return info, err
	}
	info.Bank, err = s.categories(ctx, bank.ID)
	return info, err
}

func normaliseRules(in []PoolRule) ([]PoolRule, error) {
	f := map[string]string{}
	seen := map[string]bool{}
	out := make([]PoolRule, 0, len(in))
	if len(in) > 50 {
		return nil, httpx.Invalid(map[string]string{"pools": "at most 50 pools"})
	}
	for i, r := range in {
		r.Source = strings.ToLower(strings.TrimSpace(r.Source))
		r.Category = strings.Join(strings.Fields(r.Category), " ")
		k := fmt.Sprintf("pools.%d", i)
		switch {
		case r.Source != SourceQuiz && r.Source != SourceBank:
			f[k] = "a pool comes from this quiz or from your question bank"
		case r.Count < 0 || r.Count > 1000:
			f[k] = "draw between 0 (all of them) and 1000"
		case seen[r.Source+"|"+strings.ToLower(r.Category)]:
			f[k] = "this pool is listed twice"
		}
		seen[r.Source+"|"+strings.ToLower(r.Category)] = true
		out = append(out, r)
	}
	if len(f) > 0 {
		return nil, httpx.Invalid(f)
	}
	return out, nil
}

// SetPools replaces a quiz's pool rules. Rules are checked against what the
// quiz and the bank hold when the quiz is marked ready and when a session
// starts, since questions may still be added after.
func (s *Service) SetPools(ctx context.Context, teacherID, quizID string, rules []PoolRule) (PoolInfo, error) {
	rules, err := normaliseRules(rules)
	if err != nil {
		return PoolInfo{}, err
	}
	if err := s.notAQuiz(ctx, quizID); err != nil {
		return PoolInfo{}, err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE quiz.quizzes SET pools=$3, updated_at=now() WHERE id=$1 AND teacher_id=$2`, quizID, teacherID, mustJSON(rules))
	if err != nil {
		return PoolInfo{}, err
	}
	if tag.RowsAffected() == 0 {
		return PoolInfo{}, httpx.ErrNotFound
	}
	return s.Pools(ctx, teacherID, quizID)
}

// poolProblems lists rules the quiz or the bank can't satisfy.
func (s *Service) poolProblems(ctx context.Context, teacherID string, q Quiz) ([]string, error) {
	if len(q.Pools) == 0 {
		return nil, nil
	}
	info, err := s.Pools(ctx, teacherID, q.ID)
	if err != nil {
		return nil, err
	}
	have := map[string]int{}
	for _, c := range info.Quiz {
		have[SourceQuiz+"|"+strings.ToLower(c.Category)] = c.Count
	}
	for _, c := range info.Bank {
		have[SourceBank+"|"+strings.ToLower(c.Category)] = c.Count
	}
	var out []string
	for _, r := range q.Pools {
		n := have[r.Source+"|"+strings.ToLower(r.Category)]
		name := PoolName(r)
		switch {
		case n == 0:
			out = append(out, fmt.Sprintf("pool %s has no questions", name))
		case r.Count > n:
			out = append(out, fmt.Sprintf("pool %s draws %d but has only %d", name, r.Count, n))
		}
	}
	return out, nil
}

// PoolName is how a pool is named in messages: "Easy", "bank: Hard", "uncategorised".
func PoolName(r PoolRule) string {
	name := r.Category
	if name == "" {
		name = "uncategorised"
	}
	if r.Source == SourceBank {
		return "bank: " + name
	}
	return name
}

// SessionQuestions are the questions a new session copies: the quiz's own,
// then the bank questions its pools draw from (marked FromBank, with codes
// prefixed "B-" so they stay apart from the quiz's).
func (s *Service) SessionQuestions(ctx context.Context, teacherID string, q Quiz) ([]Question, error) {
	qs, err := s.ListQuestions(ctx, teacherID, q.ID)
	if err != nil {
		return nil, err
	}
	if problems, err := s.poolProblems(ctx, teacherID, q); err != nil {
		return nil, err
	} else if len(problems) > 0 {
		return nil, errNotReady(problems)
	}
	want := map[string]bool{}
	for _, r := range q.Pools {
		if r.Source == SourceBank {
			want[strings.ToLower(r.Category)] = true
		}
	}
	if len(want) == 0 {
		return qs, nil
	}
	bank, err := s.Bank(ctx, teacherID)
	if err != nil {
		return nil, err
	}
	bq, err := s.loadQuestions(ctx, bank.ID)
	if err != nil {
		return nil, err
	}
	for _, b := range bq {
		if want[strings.ToLower(b.Category)] {
			b.FromBank = true
			b.Code = "B-" + b.Code
			qs = append(qs, b)
		}
	}
	return qs, nil
}
