package content

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/settings"
)

var (
	errRejected       = httpx.NewError(http.StatusForbidden, "enrolment_rejected", "your enrolment in this classroom was declined")
	errNeedStudentNum = httpx.Invalid(map[string]string{"student_number": "this classroom requires your student ID"})
	errNumberTaken    = httpx.Invalid(map[string]string{"student_number": "this student ID is already used in this classroom"})
)

// ClassroomPreview is what a student sees on the join page before enrolling.
type ClassroomPreview struct {
	ClassroomID       string     `json:"classroom_id"`
	Name              string     `json:"name"`
	StudentIDRequired bool       `json:"student_id_required"`
	Enrolment         *Enrolment `json:"enrolment"`
}

func (s *Service) classroomByCode(ctx context.Context, code string) (Classroom, settings.Effective, error) {
	c, err := scanClassroom(s.pool.QueryRow(ctx, `SELECT `+classroomCols+` FROM content.classrooms WHERE join_code=$1`,
		strings.ToUpper(strings.TrimSpace(code))))
	if err != nil {
		return c, settings.Effective{}, err
	}
	eff, err := s.effectiveFor(ctx, c)
	return c, eff, err
}

func (s *Service) effectiveFor(ctx context.Context, c Classroom) (settings.Effective, error) {
	base, err := s.settings.Base(ctx, c.TeacherID)
	if err != nil {
		return settings.Effective{}, err
	}
	return settings.Resolve(base, c.Settings), nil
}

func (s *Service) Preview(ctx context.Context, userID, code string) (ClassroomPreview, error) {
	c, eff, err := s.classroomByCode(ctx, code)
	if err != nil {
		return ClassroomPreview{}, err
	}
	if c.Archived {
		return ClassroomPreview{}, errArchived
	}
	p := ClassroomPreview{ClassroomID: c.ID, Name: c.Name, StudentIDRequired: eff.StudentIDRequired}
	if e, ok, err := s.GetEnrolment(ctx, c.ID, userID); err != nil {
		return p, err
	} else if ok {
		p.Enrolment = &e
	}
	return p, nil
}

// EnrolByCode enrols the user using a classroom join code, link or QR (FR-CLS-03).
func (s *Service) EnrolByCode(ctx context.Context, userID, code, studentNumber string) (Enrolment, error) {
	c, eff, err := s.classroomByCode(ctx, code)
	if err != nil {
		return Enrolment{}, err
	}
	return s.enrol(ctx, c, eff, userID, studentNumber)
}

// EnsureEnrolled implements BR-02 for session joins: an existing active
// enrolment passes; otherwise the student is enrolled automatically when the
// classroom allows it. The returned enrolment may still be "pending" when the
// classroom requires approval; callers must check Status.
func (s *Service) EnsureEnrolled(ctx context.Context, classroomID, userID, studentNumber string) (Enrolment, error) {
	c, err := scanClassroom(s.pool.QueryRow(ctx, `SELECT `+classroomCols+` FROM content.classrooms WHERE id=$1`, classroomID))
	if err != nil {
		return Enrolment{}, err
	}
	eff, err := s.effectiveFor(ctx, c)
	if err != nil {
		return Enrolment{}, err
	}
	if e, ok, err := s.GetEnrolment(ctx, classroomID, userID); err != nil {
		return e, err
	} else if ok && e.Status == "active" && (e.StudentNumber != nil || !eff.StudentIDRequired) {
		return e, nil
	} else if (!ok || e.Status == "removed") && !eff.AutoEnrolOnJoin {
		return e, httpx.NewError(http.StatusForbidden, "not_enrolled", "you are not enrolled in this classroom")
	}
	return s.enrol(ctx, c, eff, userID, studentNumber)
}

func (s *Service) enrol(ctx context.Context, c Classroom, eff settings.Effective, userID, studentNumber string) (Enrolment, error) {
	if c.Archived {
		return Enrolment{}, errArchived
	}
	studentNumber = strings.TrimSpace(studentNumber)
	if len(studentNumber) > 64 {
		return Enrolment{}, httpx.Invalid(map[string]string{"student_number": "at most 64 characters"})
	}
	existing, ok, err := s.GetEnrolment(ctx, c.ID, userID)
	if err != nil {
		return existing, err
	}
	if ok && existing.Status == "rejected" {
		return existing, errRejected
	}
	if eff.StudentIDRequired && studentNumber == "" && (!ok || existing.StudentNumber == nil) {
		return existing, errNeedStudentNum
	}
	var num any
	if studentNumber != "" {
		num = studentNumber
	}
	status := "active"
	if eff.EnrolmentApproval {
		status = "pending"
	}
	var e Enrolment
	// Re-enrolling keeps an active status; the student number, once set, is
	// changed only by the teacher (it is attached to every answer, BR-03).
	err = s.pool.QueryRow(ctx, `INSERT INTO content.enrolments(classroom_id, user_id, student_number, status)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (classroom_id, user_id) DO UPDATE SET
			student_number = coalesce(content.enrolments.student_number, EXCLUDED.student_number),
			status = CASE WHEN content.enrolments.status IN ('active','pending') THEN content.enrolments.status ELSE EXCLUDED.status END
		RETURNING id, classroom_id, user_id, student_number, status, created_at`, c.ID, userID, num, status).
		Scan(&e.ID, &e.ClassroomID, &e.UserID, &e.StudentNumber, &e.Status, &e.CreatedAt)
	if isUnique(err, "enrolments_student_number_key") {
		return e, errNumberTaken
	}
	return e, err
}

const enrolmentCols = `id, classroom_id, user_id, student_number, status, created_at`

func scanEnrolment(r pgx.Row) (Enrolment, error) {
	var e Enrolment
	err := r.Scan(&e.ID, &e.ClassroomID, &e.UserID, &e.StudentNumber, &e.Status, &e.CreatedAt)
	return e, err
}

// GetEnrolment returns a student's enrolment in a classroom, if any.
func (s *Service) GetEnrolment(ctx context.Context, classroomID, userID string) (Enrolment, bool, error) {
	e, err := scanEnrolment(s.pool.QueryRow(ctx, `SELECT `+enrolmentCols+` FROM content.enrolments WHERE classroom_id=$1 AND user_id=$2`, classroomID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return e, false, nil
	}
	return e, err == nil, err
}

// ListEnrolments returns a classroom's enrolments with student names for its teacher.
func (s *Service) ListEnrolments(ctx context.Context, teacherID, classroomID, status string) ([]Enrolment, error) {
	if _, err := s.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+enrolmentCols+` FROM content.enrolments
		WHERE classroom_id=$1 AND ($2 = '' OR status=$2) ORDER BY created_at`, classroomID, status)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Enrolment, error) { return scanEnrolment(r) })
	if err != nil || len(list) == 0 {
		return list, err
	}
	ids := make([]string, len(list))
	for i, e := range list {
		ids[i] = e.UserID
	}
	users, err := s.users.UsersByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	cats, err := s.categoriesOf(ctx, classroomID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		u := users[list[i].UserID]
		list[i].StudentName, list[i].StudentEmail = u.Name, u.Email
		list[i].Categories = cats[list[i].ID]
	}
	return list, nil
}

type EnrolmentUpdate struct {
	Status        *string `json:"status"`         // active (approve), rejected, removed
	StudentNumber *string `json:"student_number"` // teacher may correct it
}

// UpdateEnrolment lets the teacher approve, reject or remove an enrolment
// and correct the student ID (FR-CLS-04).
func (s *Service) UpdateEnrolment(ctx context.Context, teacherID, enrolmentID string, in EnrolmentUpdate) (Enrolment, error) {
	if in.Status != nil {
		switch *in.Status {
		case "active", "rejected", "removed":
		default:
			return Enrolment{}, httpx.Invalid(map[string]string{"status": "must be active, rejected or removed"})
		}
	}
	var num any
	if in.StudentNumber != nil {
		if v := strings.TrimSpace(*in.StudentNumber); v != "" {
			num = v
		}
	}
	var e Enrolment
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		e, err = scanEnrolment(tx.QueryRow(ctx, `UPDATE content.enrolments en SET
			status = coalesce($3, en.status),
			student_number = CASE WHEN $4 THEN $5 ELSE en.student_number END
			FROM content.classrooms c WHERE c.id=en.classroom_id AND en.id=$1 AND c.teacher_id=$2
			RETURNING en.id, en.classroom_id, en.user_id, en.student_number, en.status, en.created_at`,
			enrolmentID, teacherID, in.Status, in.StudentNumber != nil, num))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if isUnique(err, "enrolments_student_number_key") {
			return errNumberTaken
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "enrolment_updated", "enrolment", enrolmentID, in)
	})
	return e, err
}

// StudentClassroom is a classroom as listed for an enrolled student.
type StudentClassroom struct {
	ClassroomID   string  `json:"classroom_id"`
	Name          string  `json:"name"`
	Status        string  `json:"status"`
	StudentNumber *string `json:"student_number"`
}

func (s *Service) MyClassrooms(ctx context.Context, userID string) ([]StudentClassroom, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.name, e.status, e.student_number
		FROM content.enrolments e JOIN content.classrooms c ON c.id=e.classroom_id
		WHERE e.user_id=$1 AND e.status IN ('active','pending') AND c.archived_at IS NULL ORDER BY c.name`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (StudentClassroom, error) {
		var sc StudentClassroom
		err := r.Scan(&sc.ClassroomID, &sc.Name, &sc.Status, &sc.StudentNumber)
		return sc, err
	})
}
