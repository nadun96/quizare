package content

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Category is a teacher-defined label for students in a classroom (V2-03, D-41).
type Category struct {
	ID          string `json:"id"`
	ClassroomID string `json:"classroom_id"`
	Name        string `json:"name"`
	Color       int    `json:"color"` // 1-8, a slot of the categorical palette
	Position    int    `json:"position"`
	Members     int    `json:"members"`
}

type CategoryInput struct {
	Name  *string `json:"name"`
	Color *int    `json:"color"`
}

const maxCategories = 50

var errCategoryName = httpx.Conflict("a category with this name already exists in the classroom")

func validCategory(name string, color int) map[string]string {
	f := map[string]string{}
	if n := utf8.RuneCountInString(name); n < 1 || n > 60 {
		f["name"] = "name must be 1-60 characters"
	}
	if color < 1 || color > 8 {
		f["color"] = "color must be 1-8"
	}
	return f
}

func (s *Service) ListCategories(ctx context.Context, teacherID, classroomID string) ([]Category, error) {
	if _, err := s.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return nil, err
	}
	return s.categories(ctx, classroomID)
}

func (s *Service) categories(ctx context.Context, classroomID string) ([]Category, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.classroom_id, c.name, c.color, c.position,
		(SELECT count(*) FROM content.enrolment_categories ec JOIN content.enrolments e ON e.id=ec.enrolment_id
		 WHERE ec.category_id=c.id AND e.status='active')
		FROM content.categories c WHERE c.classroom_id=$1 ORDER BY c.position, lower(c.name)`, classroomID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Category, error) {
		var c Category
		err := r.Scan(&c.ID, &c.ClassroomID, &c.Name, &c.Color, &c.Position, &c.Members)
		return c, err
	})
	if out == nil {
		out = []Category{}
	}
	return out, err
}

func (s *Service) CreateCategory(ctx context.Context, teacherID, classroomID string, in CategoryInput) (Category, error) {
	if _, err := s.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return Category{}, err
	}
	name, color := "", 0
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM content.categories WHERE classroom_id=$1`, classroomID).Scan(&n); err != nil {
		return Category{}, err
	}
	color = n%8 + 1 // next palette slot, so neighbouring categories differ
	if in.Color != nil {
		color = *in.Color
	}
	if f := validCategory(name, color); len(f) > 0 {
		return Category{}, httpx.Invalid(f)
	}
	if n >= maxCategories {
		return Category{}, httpx.Conflict("a classroom can have at most 50 categories")
	}
	var c Category
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO content.categories (classroom_id, name, color, position) VALUES ($1,$2,$3,$4)
			RETURNING id, classroom_id, name, color, position`, classroomID, name, color, n).Scan(&c.ID, &c.ClassroomID, &c.Name, &c.Color, &c.Position)
		if isUnique(err, "categories_name_key") {
			return errCategoryName
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "category_created", "category", c.ID, map[string]any{"name": name})
	})
	return c, err
}

// ownedCategory returns the category if it belongs to one of the teacher's classrooms.
func (s *Service) ownedCategory(ctx context.Context, teacherID, id string) (Category, error) {
	var c Category
	err := s.pool.QueryRow(ctx, `SELECT c.id, c.classroom_id, c.name, c.color, c.position FROM content.categories c
		JOIN content.classrooms k ON k.id=c.classroom_id WHERE c.id=$1 AND k.teacher_id=$2`, id, teacherID).
		Scan(&c.ID, &c.ClassroomID, &c.Name, &c.Color, &c.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httpx.ErrNotFound
	}
	return c, err
}

func (s *Service) UpdateCategory(ctx context.Context, teacherID, id string, in CategoryInput) (Category, error) {
	c, err := s.ownedCategory(ctx, teacherID, id)
	if err != nil {
		return c, err
	}
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if in.Color != nil {
		c.Color = *in.Color
	}
	if f := validCategory(c.Name, c.Color); len(f) > 0 {
		return c, httpx.Invalid(f)
	}
	_, err = s.pool.Exec(ctx, `UPDATE content.categories SET name=$2, color=$3 WHERE id=$1`, id, c.Name, c.Color)
	if isUnique(err, "categories_name_key") {
		return c, errCategoryName
	}
	return c, err
}

func (s *Service) DeleteCategory(ctx context.Context, teacherID, id string) error {
	if _, err := s.ownedCategory(ctx, teacherID, id); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM content.categories WHERE id=$1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "category_deleted", "category", id, nil)
	})
}

type AssignInput struct {
	EnrolmentIDs []string `json:"enrolment_ids"`
	Assigned     bool     `json:"assigned"` // true adds them to the category, false removes them
}

// AssignCategory adds students to, or removes them from, a category.
func (s *Service) AssignCategory(ctx context.Context, teacherID, categoryID string, in AssignInput) (int, error) {
	c, err := s.ownedCategory(ctx, teacherID, categoryID)
	if err != nil {
		return 0, err
	}
	if len(in.EnrolmentIDs) == 0 || len(in.EnrolmentIDs) > 1000 {
		return 0, httpx.Invalid(map[string]string{"enrolment_ids": "give 1-1000 enrolments"})
	}
	var n int64
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var tag interface{ RowsAffected() int64 }
		var err error
		if in.Assigned {
			// Only enrolments of the same classroom can be added.
			t, e := tx.Exec(ctx, `INSERT INTO content.enrolment_categories (enrolment_id, category_id)
				SELECT e.id, $1 FROM content.enrolments e WHERE e.id = ANY($2::uuid[]) AND e.classroom_id=$3
				ON CONFLICT DO NOTHING`, categoryID, in.EnrolmentIDs, c.ClassroomID)
			tag, err = t, e
		} else {
			t, e := tx.Exec(ctx, `DELETE FROM content.enrolment_categories WHERE category_id=$1 AND enrolment_id = ANY($2::uuid[])`, categoryID, in.EnrolmentIDs)
			tag, err = t, e
		}
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		return audit.Log(ctx, tx, teacherID, "category_assigned", "category", categoryID, in)
	})
	return int(n), err
}

// categoriesOf returns the category ids of each enrolment, for the student list.
func (s *Service) categoriesOf(ctx context.Context, classroomID string) (map[string][]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT ec.enrolment_id, ec.category_id FROM content.enrolment_categories ec
		JOIN content.categories c ON c.id=ec.category_id WHERE c.classroom_id=$1 ORDER BY c.position, lower(c.name)`, classroomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var e, c string
		if err := rows.Scan(&e, &c); err != nil {
			return nil, err
		}
		out[e] = append(out[e], c)
	}
	return out, rows.Err()
}

// CategoryMember is an active student in a category, for forming groups (V2-06).
type CategoryMember struct {
	CategoryID   string
	CategoryName string
	Color        int
	UserID       string
}

// CategoryMembers lists active students per category of a classroom the teacher owns.
func (s *Service) CategoryMembers(ctx context.Context, teacherID, classroomID string) ([]CategoryMember, error) {
	if _, err := s.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.name, c.color, e.user_id FROM content.categories c
		JOIN content.enrolment_categories ec ON ec.category_id=c.id
		JOIN content.enrolments e ON e.id=ec.enrolment_id AND e.status='active'
		WHERE c.classroom_id=$1 ORDER BY c.position, lower(c.name), e.created_at`, classroomID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CategoryMember, error) {
		var m CategoryMember
		err := r.Scan(&m.CategoryID, &m.CategoryName, &m.Color, &m.UserID)
		return m, err
	})
}

// SharesClassroom reports whether one of a and b teaches a classroom the
// other is enrolled in (pending or active). Teachers and their students may
// see each other's profile pictures (D-49).
func (s *Service) SharesClassroom(ctx context.Context, a, b string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM content.classrooms c JOIN content.enrolments e ON e.classroom_id=c.id
		WHERE e.status IN ('pending', 'active') AND ((c.teacher_id=$1 AND e.user_id=$2) OR (c.teacher_id=$2 AND e.user_id=$1)))`, a, b).Scan(&ok)
	return ok, err
}
