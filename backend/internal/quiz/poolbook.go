package quiz

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Working with many questions at once (D-123): importing with a preview
// first, a category for rows that name none, and codes made unique; moving
// or deleting questions in bulk; renaming and deleting categories; and an
// overview of every pool a teacher can draw from.

// ImportOptions shape an import.
type ImportOptions struct {
	Preview  bool   // check and show, save nothing
	Category string // for rows without a category (often from the file's name)
	Rename   bool   // give a code already in use the next free one ("Q1" → "Q1-2") instead of refusing the row
}

// PreviewRow is one row as it would be saved.
type PreviewRow struct {
	Row      int               `json:"row"`
	Code     string            `json:"code"`
	Renamed  string            `json:"renamed,omitempty"` // the code it was given, when it was in use
	Type     Type              `json:"type"`
	Text     string            `json:"text"`
	Category string            `json:"category"`
	Marks    float64           `json:"marks"`
	Errors   map[string]string `json:"errors,omitempty"`
}

// ImportResult is what an import did, or would do in a preview.
type ImportResult struct {
	ImportReport
	Preview    bool           `json:"preview"`
	Rows       []PreviewRow   `json:"rows"`
	Categories map[string]int `json:"categories"` // valid rows per category
	Renamed    int            `json:"renamed"`
}

func cleanCategory(c string) string { return strings.Join(strings.Fields(c), " ") }

// ImportWith imports a questions CSV with options; with Preview nothing is saved.
func (s *Service) ImportWith(ctx context.Context, teacherID, quizID string, data []byte, o ImportOptions) (ImportResult, error) {
	res := ImportResult{ImportReport: ImportReport{Rejected: []RowError{}}, Preview: o.Preview, Rows: []PreviewRow{}, Categories: map[string]int{}}
	o.Category = cleanCategory(o.Category)
	if len([]rune(o.Category)) > 60 {
		return res, httpx.Invalid(map[string]string{"category": "at most 60 characters"})
	}
	rows, err := ParseQuestionsCSV(data)
	if err != nil {
		return res, httpx.BadRequest(err.Error())
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		crow, err := tx.Query(ctx, `SELECT code FROM quiz.questions WHERE quiz_id=$1`, quizID)
		if err != nil {
			return err
		}
		codes, err := pgx.CollectRows(crow, pgx.RowTo[string])
		if err != nil {
			return err
		}
		used := map[string]bool{}
		for _, c := range codes {
			used[c] = true
		}
		for _, r := range rows {
			q := r.Question
			if q.Category == "" {
				q.Category = o.Category
			}
			pr := PreviewRow{Row: r.Row, Code: q.Code, Type: q.Type, Text: plainText(q.Text, 160), Category: q.Category, Marks: q.Marks, Errors: r.Errors}
			if len(pr.Errors) == 0 && used[q.Code] {
				if o.Rename {
					for n := 2; ; n++ {
						c := fmt.Sprintf("%s-%d", q.Code, n)
						if !used[c] && len(c) <= 40 {
							q.Code, pr.Renamed = c, c
							res.Renamed++
							break
						}
					}
				} else {
					pr.Errors = map[string]string{"code": "question code " + q.Code + " already exists here"}
				}
			}
			res.Rows = append(res.Rows, pr)
			if len(pr.Errors) > 0 {
				res.Rejected = append(res.Rejected, RowError{Row: r.Row, Code: r.Question.Code, Errors: pr.Errors})
				continue
			}
			used[q.Code] = true
			res.Categories[q.Category]++
			res.Imported++
			if o.Preview {
				continue
			}
			if err := insertQuestion(ctx, tx, quizID, &q); err != nil {
				return err
			}
		}
		if o.Preview {
			return nil
		}
		return touchQuiz(ctx, tx, quizID)
	})
	return res, err
}

// plainText shortens question text for a preview, without its markup.
func plainText(s string, max int) string {
	s = strings.Join(strings.Fields(strings.NewReplacer("**", "", "__", "", "`", "", "[[", "_", "]]", "_").Replace(s)), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

// BulkInput moves questions to a category, or deletes them.
type BulkInput struct {
	Action   string   `json:"action"` // move or delete
	IDs      []string `json:"ids"`
	Category string   `json:"category"`
}

// Bulk applies one action to many questions of a quiz, bank or pool.
func (s *Service) Bulk(ctx context.Context, teacherID, quizID string, in BulkInput) (int, error) {
	if len(in.IDs) == 0 || len(in.IDs) > 5000 {
		return 0, httpx.Invalid(map[string]string{"ids": "choose 1 to 5000 questions"})
	}
	in.Category = cleanCategory(in.Category)
	if len([]rune(in.Category)) > 60 {
		return 0, httpx.Invalid(map[string]string{"category": "at most 60 characters"})
	}
	var n int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		var tag interface{ RowsAffected() int64 }
		var err error
		switch in.Action {
		case "delete":
			tag, err = tx.Exec(ctx, `DELETE FROM quiz.questions WHERE quiz_id = $1 AND id::text = ANY($2)`, quizID, in.IDs)
		case "move":
			tag, err = tx.Exec(ctx, `UPDATE quiz.questions SET category = $3 WHERE quiz_id = $1 AND id::text = ANY($2)`, quizID, in.IDs, in.Category)
		default:
			return httpx.Invalid(map[string]string{"action": "move or delete"})
		}
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		if err := touchQuiz(ctx, tx, quizID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "questions_"+in.Action, "quiz", quizID, map[string]any{"count": n, "category": in.Category})
	})
	return int(n), err
}

// RenameCategory renames a category in a quiz, bank or pool; into one that
// exists, it merges them.
func (s *Service) RenameCategory(ctx context.Context, teacherID, quizID, from, to string) (int, error) {
	from, to = cleanCategory(from), cleanCategory(to)
	if len([]rune(to)) > 60 {
		return 0, httpx.Invalid(map[string]string{"to": "at most 60 characters"})
	}
	var n int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE quiz.questions SET category = $3 WHERE quiz_id = $1 AND lower(category) = lower($2)`, quizID, from, to)
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		return touchQuiz(ctx, tx, quizID)
	})
	return int(n), err
}

// DeleteCategory deletes a category with its questions, or keeps them uncategorised.
func (s *Service) DeleteCategory(ctx context.Context, teacherID, quizID, name string, withQuestions bool) (int, error) {
	name = cleanCategory(name)
	var n int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.ownedQuiz(ctx, tx, teacherID, quizID); err != nil {
			return err
		}
		var tag interface{ RowsAffected() int64 }
		var err error
		if withQuestions {
			tag, err = tx.Exec(ctx, `DELETE FROM quiz.questions WHERE quiz_id = $1 AND lower(category) = lower($2)`, quizID, name)
		} else {
			tag, err = tx.Exec(ctx, `UPDATE quiz.questions SET category = '' WHERE quiz_id = $1 AND lower(category) = lower($2)`, quizID, name)
		}
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		if err := touchQuiz(ctx, tx, quizID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "category_deleted", "quiz", quizID, map[string]any{"category": name, "questions_deleted": withQuestions, "count": n})
	})
	return int(n), err
}

// PoolSummary is one place questions are kept: the teacher's bank or a classroom's pool.
type PoolSummary struct {
	Kind        string      `json:"kind"` // bank or pool
	QuizID      string      `json:"quiz_id"`
	ClassroomID string      `json:"classroom_id,omitempty"`
	Classroom   string      `json:"classroom,omitempty"`
	Role        string      `json:"role,omitempty"` // the teacher's part in the classroom: owner or coteacher
	Questions   int         `json:"questions"`
	Categories  []PoolCount `json:"categories"`
}

// PoolsOverview lists the teacher's bank and the pool of every classroom they teach (not archived).
func (s *Service) PoolsOverview(ctx context.Context, teacherID string) ([]PoolSummary, error) {
	bank, err := s.Bank(ctx, teacherID)
	if err != nil {
		return nil, err
	}
	out := []PoolSummary{{Kind: KindBank, QuizID: bank.ID, Questions: bank.QuestionCount}}
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.name, CASE WHEN c.teacher_id = $1 THEN 'owner' ELSE 'coteacher' END,
			coalesce(p.id::text, ''), (SELECT count(*) FROM quiz.questions x WHERE x.quiz_id = p.id)::int
		FROM content.classrooms c LEFT JOIN quiz.quizzes p ON p.classroom_id = c.id AND p.kind = 'pool'
		WHERE content.teaches(c.id, $1) AND c.archived_at IS NULL ORDER BY lower(c.name)`, teacherID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p PoolSummary
		p.Kind = KindPool
		if err := rows.Scan(&p.ClassroomID, &p.Classroom, &p.Role, &p.QuizID, &p.Questions); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Categories = []PoolCount{}
		if out[i].QuizID != "" {
			if out[i].Categories, err = s.categories(ctx, out[i].QuizID); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// poolRoutes mounts the D-123 endpoints.
func (s *Service) poolRoutes(r chi.Router) {
	r.Method("GET", "/pools", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.PoolsOverview(r.Context(), uid(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"pools": list})
		return nil
	}))
	r.Method("GET", "/classrooms/{id}/pool", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		q, err := s.ClassPool(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, q)
		return nil
	}))
	r.Method("POST", "/quizzes/{id}/questions/bulk", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in BulkInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		n, err := s.Bulk(r.Context(), uid(r), id(r), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]int{"affected": n})
		return nil
	}))
	r.Method("POST", "/quizzes/{id}/categories/rename", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		n, err := s.RenameCategory(r.Context(), uid(r), id(r), in.From, in.To)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]int{"affected": n})
		return nil
	}))
	r.Method("POST", "/quizzes/{id}/categories/delete", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Name      string `json:"name"`
			Questions bool   `json:"delete_questions"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		n, err := s.DeleteCategory(r.Context(), uid(r), id(r), in.Name, in.Questions)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]int{"affected": n})
		return nil
	}))
}

// importOptions reads ?preview=1&category=…&rename=1.
func importOptions(r *http.Request) ImportOptions {
	b := func(k string) bool { v, _ := strconv.ParseBool(r.URL.Query().Get(k)); return v }
	return ImportOptions{Preview: b("preview"), Category: r.URL.Query().Get("category"), Rename: b("rename")}
}
