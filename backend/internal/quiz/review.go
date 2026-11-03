package quiz

import (
	"context"
	"encoding/json"
	mrand "math/rand/v2"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Reviewing and balancing a pool's questions (D-132). Reviewing is a mark a
// teacher sets after checking a question; it's optional. Balancing moves the
// options of single-answer questions so the right answer isn't usually in
// the same place (a file written by hand often has it first); the options
// keep their ids, so answer keys and earlier results stay right.

// ReviewInput marks questions checked, or not.
type ReviewInput struct {
	IDs      []string `json:"ids"`
	Reviewed bool     `json:"reviewed"`
}

// Review sets or clears the reviewed mark on questions of a quiz or pool.
func (s *Service) Review(ctx context.Context, teacherID, quizID string, in ReviewInput) (int, error) {
	if len(in.IDs) == 0 || len(in.IDs) > 5000 {
		return 0, httpx.Invalid(map[string]string{"ids": "choose 1 to 5000 questions"})
	}
	var n int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE quiz.questions SET reviewed_at = CASE WHEN $3 THEN coalesce(reviewed_at, now()) END,
			reviewed_by = CASE WHEN $3 THEN coalesce(reviewed_by, $4::uuid) END
			WHERE quiz_id = $1 AND id::text = ANY($2)`, quizID, in.IDs, in.Reviewed, teacherID)
		n = tag.RowsAffected()
		return err
	})
	return int(n), err
}

// BalanceInput chooses what to balance: one category, or every question when nil.
type BalanceInput struct {
	Category *string `json:"category"`
}

// BalanceResult says what moved, and what was left as it was.
type BalanceResult struct {
	Moved    int `json:"moved"`    // single-answer questions whose options were reordered
	Shuffled int `json:"shuffled"` // multiple-answer questions shuffled
	Kept     int `json:"kept"`     // left alone: "all of the above", True/False and the like, where order matters
}

// orderMatters: options whose meaning depends on where they are, or a pair
// people expect in order.
var orderMatters = regexp.MustCompile(`(?i)\b(all|none|both|neither)\s+(of\s+)?(the\s+)?(above|below|these|those)\b|^\s*(both|neither)\b.*\b(and|nor)\b`)

func keepsOrder(opts []Choice) bool {
	for _, o := range opts {
		if orderMatters.MatchString(o.Text) {
			return true
		}
	}
	if len(opts) == 2 {
		a, b := strings.ToLower(strings.TrimSpace(opts[0].Text)), strings.ToLower(strings.TrimSpace(opts[1].Text))
		pair := a + "/" + b
		return pair == "true/false" || pair == "yes/no" || pair == "false/true" || pair == "no/yes"
	}
	return false
}

// Balance reorders the options of a quiz's single-answer questions so the
// right answer falls evenly on each position, A, B, C…, in question order,
// with the other options shuffled around it; multiple-answer questions are
// shuffled. Students who get options shuffled per student (option order:
// shuffled) see a new order anyway; this fixes the order everyone else sees.
func (s *Service) Balance(ctx context.Context, teacherID, quizID string, in BalanceInput, rng *mrand.Rand) (BalanceResult, error) {
	var out BalanceResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, type, body, answer_key FROM quiz.questions
			WHERE quiz_id = $1 AND type IN ('SINGLE','MULTI') AND ($2::text IS NULL OR lower(category) = lower($2))
			ORDER BY position, created_at FOR UPDATE`, quizID, in.Category)
		if err != nil {
			return err
		}
		type row struct {
			id   string
			typ  Type
			body Body
			key  Key
			raw  []byte
		}
		var list []row
		for rows.Next() {
			var r row
			var key []byte
			if err := rows.Scan(&r.id, &r.typ, &r.raw, &key); err != nil {
				rows.Close()
				return err
			}
			_ = json.Unmarshal(r.raw, &r.body)
			_ = json.Unmarshal(key, &r.key)
			list = append(list, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// How often the right answer has gone to each position, per number of options.
		used := map[int][]int{}
		for _, r := range list {
			opts := r.body.Options
			if len(opts) < 2 || keepsOrder(opts) {
				out.Kept++
				continue
			}
			rng.Shuffle(len(opts), func(i, j int) { opts[i], opts[j] = opts[j], opts[i] })
			if r.typ == Single && len(r.key.Correct) == 1 {
				n := len(opts)
				if used[n] == nil {
					used[n] = make([]int, n)
				}
				// The least used position, a random one of equals.
				best, ties := 0, 0
				for p := range n {
					switch {
					case used[n][p] < used[n][best]:
						best, ties = p, 1
					case used[n][p] == used[n][best]:
						ties++
						if rng.IntN(ties) == 0 {
							best = p
						}
					}
				}
				for i, o := range opts {
					if o.ID == r.key.Correct[0] {
						opts[i], opts[best] = opts[best], opts[i]
						break
					}
				}
				used[n][best]++
				out.Moved++
			} else {
				out.Shuffled++
			}
			r.body.Options = opts
			body, _ := json.Marshal(r.body)
			if _, err := tx.Exec(ctx, `UPDATE quiz.questions SET body = $2, updated_at = now() WHERE id = $1`, r.id, body); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) reviewRoutes(r chi.Router) {
	r.Method("POST", "/quizzes/{id}/questions/review", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in ReviewInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		n, err := s.Review(r.Context(), uid(r), id(r), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]int{"affected": n})
		return nil
	}))
	r.Method("POST", "/quizzes/{id}/questions/balance", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in BalanceInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		res, err := s.Balance(r.Context(), uid(r), id(r), in, mrand.New(mrand.NewPCG(mrand.Uint64(), mrand.Uint64())))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, res)
		return nil
	}))
}
