package content

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/balance"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Random groups (D-162): a number of new groups, the classroom's active
// students shared among them at random and evenly, at most so many a group,
// each gender spread evenly if asked. Students keep their other groups;
// those who don't fit stay out for the teacher to place, who can then move
// anyone, ignoring the limit.

// RandomGroupsInput says how.
type RandomGroupsInput struct {
	Count   int    `json:"count"`
	Max     int    `json:"max"`     // most a group; 0 none
	Balance bool   `json:"balance"` // spread each gender evenly
	Prefix  string `json:"prefix"`  // the groups are "<prefix> 1", "<prefix> 2"...; empty "Group"
}

// RandomGroupsResult is what was made.
type RandomGroupsResult struct {
	Groups []string `json:"groups"` // their ids
	Placed int      `json:"placed"` // students put in them
	Left   int      `json:"left"`   // who didn't fit
}

// RandomGroups makes the groups and shares the students out.
func (s *Service) RandomGroups(ctx context.Context, teacherID, classroomID string, in RandomGroupsInput) (RandomGroupsResult, error) {
	if _, err := s.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return RandomGroupsResult{}, err
	}
	in.Prefix = strings.Join(strings.Fields(in.Prefix), " ")
	if in.Prefix == "" {
		in.Prefix = "Group"
	}
	bad := map[string]string{}
	var existing int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM content.categories WHERE classroom_id = $1`, classroomID).Scan(&existing); err != nil {
		return RandomGroupsResult{}, err
	}
	switch {
	case in.Count < 2 || in.Count > maxCategories:
		bad["count"] = fmt.Sprintf("from 2 to %d groups", maxCategories)
	case existing+in.Count > maxCategories:
		bad["count"] = fmt.Sprintf("a classroom can have at most %d groups; it has %d", maxCategories, existing)
	}
	if in.Max < 0 || in.Max > 1000 {
		bad["max"] = "at most 1000 a group, or 0 for no limit"
	}
	if utf8.RuneCountInString(in.Prefix) > 50 {
		bad["prefix"] = "at most 50 characters"
	}
	if len(bad) > 0 {
		return RandomGroupsResult{}, httpx.Invalid(bad)
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id::text, coalesce(u.gender, '') FROM content.enrolments e LEFT JOIN auth.users u ON u.id = e.user_id
		WHERE e.classroom_id = $1 AND e.status = 'active'`, classroomID)
	if err != nil {
		return RandomGroupsResult{}, err
	}
	people, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (balance.Person, error) {
		var p balance.Person
		return p, r.Scan(&p.ID, &p.Gender)
	})
	if err != nil {
		return RandomGroupsResult{}, err
	}
	at := balance.Share(people, make([]balance.Group, in.Count), in.Max, in.Balance, nil)

	res := RandomGroupsResult{Groups: []string{}}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Names that aren't taken: "Group 1"... or the next free numbers.
		taken := map[string]bool{}
		nrows, err := tx.Query(ctx, `SELECT lower(name) FROM content.categories WHERE classroom_id = $1`, classroomID)
		if err != nil {
			return err
		}
		names, err := pgx.CollectRows(nrows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, n := range names {
			taken[n] = true
		}
		k := 1
		for i := range in.Count {
			name := ""
			for ; ; k++ {
				name = fmt.Sprintf("%s %d", in.Prefix, k)
				if !taken[strings.ToLower(name)] {
					k++
					break
				}
			}
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO content.categories (classroom_id, name, color, position) VALUES ($1, $2, $3, $4) RETURNING id::text`,
				classroomID, name, (existing+i)%8+1, existing+i).Scan(&id); err != nil {
				return err
			}
			res.Groups = append(res.Groups, id)
		}
		for i, g := range at {
			if g < 0 {
				res.Left++
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO content.enrolment_categories (enrolment_id, category_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, people[i].ID, res.Groups[g]); err != nil {
				return err
			}
			res.Placed++
		}
		return audit.Log(ctx, tx, teacherID, "random_groups_made", "classroom", classroomID, in)
	})
	return res, err
}

func (s *Service) randomGroupRoutes(r chi.Router) {
	r.Method("POST", "/classrooms/{id}/groups/random", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in RandomGroupsInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		res, err := s.RandomGroups(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, res)
		return nil
	}))
}
