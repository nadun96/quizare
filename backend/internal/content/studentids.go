package content

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// A classroom's student ID pattern (D-162): a regular expression every
// student ID must match in full, with a hint shown when one doesn't (for
// example "MGT/2024/447"). Students can't enter an ID that doesn't match;
// IDs entered before the pattern are flagged on the Students tab.

// StudentIDRule is the pattern and its hint; an empty pattern allows any ID.
type StudentIDRule struct {
	Pattern string `json:"pattern"`
	Hint    string `json:"hint"`
}

// compile anchors the pattern so the whole ID must match.
func (r StudentIDRule) compile() (*regexp.Regexp, error) {
	if r.Pattern == "" {
		return nil, nil
	}
	return regexp.Compile(`^(?:` + r.Pattern + `)$`)
}

// idRule is a classroom's rule and its compiled pattern (nil: any ID).
func (s *Service) idRule(ctx context.Context, classroomID string) (StudentIDRule, *regexp.Regexp, error) {
	var rule StudentIDRule
	var p, h *string
	err := s.pool.QueryRow(ctx, `SELECT student_id_pattern, student_id_hint FROM content.classrooms WHERE id = $1`, classroomID).Scan(&p, &h)
	if errors.Is(err, pgx.ErrNoRows) {
		return rule, nil, httpx.ErrNotFound
	}
	if err != nil {
		return rule, nil, err
	}
	if p != nil {
		rule.Pattern = *p
	}
	if h != nil {
		rule.Hint = *h
	}
	re, err := rule.compile()
	if err != nil {
		return rule, nil, nil // a pattern that no longer compiles checks nothing
	}
	return rule, re, nil
}

// checkID says what's wrong with a student ID under the classroom's rule.
func (s *Service) checkID(ctx context.Context, classroomID, number string) error {
	if number == "" {
		return nil
	}
	rule, re, err := s.idRule(ctx, classroomID)
	if err != nil || re == nil || re.MatchString(number) {
		return err
	}
	msg := "this isn't a student ID of this classroom's form"
	if rule.Hint != "" {
		msg = fmt.Sprintf("a student ID here looks like %s", rule.Hint)
	}
	return httpx.Invalid(map[string]string{"student_number": msg})
}

// GetStudentIDRule is the classroom's rule, for its teachers.
func (s *Service) GetStudentIDRule(ctx context.Context, teacherID, classroomID string) (StudentIDRule, error) {
	if _, err := s.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return StudentIDRule{}, err
	}
	rule, _, err := s.idRule(ctx, classroomID)
	return rule, err
}

// SetStudentIDRule saves the rule; it says how many current IDs don't match.
func (s *Service) SetStudentIDRule(ctx context.Context, teacherID, classroomID string, in StudentIDRule) (StudentIDRule, int, error) {
	if _, err := s.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return in, 0, err
	}
	in.Pattern, in.Hint = strings.TrimSpace(in.Pattern), strings.TrimSpace(in.Hint)
	bad := map[string]string{}
	if utf8.RuneCountInString(in.Pattern) > 200 {
		bad["pattern"] = "at most 200 characters"
	}
	if utf8.RuneCountInString(in.Hint) > 100 {
		bad["hint"] = "at most 100 characters"
	}
	re, err := in.compile()
	if err != nil {
		bad["pattern"] = "this pattern isn't valid: " + strings.TrimPrefix(err.Error(), "error parsing regexp: ")
	}
	if re != nil && in.Hint != "" && !re.MatchString(in.Hint) {
		bad["hint"] = "the example doesn't match the pattern"
	}
	if len(bad) > 0 {
		return in, 0, httpx.Invalid(bad)
	}
	var pattern, hint any
	if in.Pattern != "" {
		pattern = in.Pattern
		if in.Hint != "" {
			hint = in.Hint
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE content.classrooms SET student_id_pattern = $2, student_id_hint = $3 WHERE id = $1`, classroomID, pattern, hint); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "student_id_rule_set", "classroom", classroomID, in)
	})
	if err != nil {
		return in, 0, err
	}
	if in.Pattern == "" {
		in.Hint = ""
	}
	wrong := 0
	if re != nil {
		rows, err := s.pool.Query(ctx, `SELECT student_number FROM content.enrolments WHERE classroom_id = $1 AND student_number IS NOT NULL AND status IN ('active', 'pending')`, classroomID)
		if err != nil {
			return in, 0, err
		}
		nums, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return in, 0, err
		}
		for _, n := range nums {
			if !re.MatchString(n) {
				wrong++
			}
		}
	}
	return in, wrong, nil
}

func (s *Service) studentIDRoutes(r chi.Router) {
	r.Method("GET", "/classrooms/{id}/student-ids", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		rule, err := s.GetStudentIDRule(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, rule)
		return nil
	}))
	r.Method("PUT", "/classrooms/{id}/student-ids", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in StudentIDRule
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		rule, wrong, err := s.SetStudentIDRule(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"pattern": rule.Pattern, "hint": rule.Hint, "not_matching": wrong})
		return nil
	}))
}
