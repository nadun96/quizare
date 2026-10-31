package content

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// Overseeing every classroom (D-101), for admins and managers given
// "Classes": who teaches and who is in each, adding, approving and removing
// students, renaming, archiving, deleting, and moving a classroom with its
// work to another teacher. Never quiz content or answers (BA §4).

// OverseenClassroom is one row of the admin's classroom list.
type OverseenClassroom struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	JoinCode     string    `json:"join_code"`
	Archived     bool      `json:"archived"`
	CreatedAt    time.Time `json:"created_at"`
	TeacherID    string    `json:"teacher_id"`
	TeacherName  string    `json:"teacher_name"`
	TeacherEmail string    `json:"teacher_email"`
	Students     int       `json:"students"`
	Pending      int       `json:"pending"`
}

// OverseenEnrolment is one student of a classroom, for the admin.
type OverseenEnrolment struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	StudentNumber *string   `json:"student_number"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// OverseeSorts are the classroom list's columns.
var OverseeSorts = page.Sorts{"name": "lower(x.name)", "created": "x.created_at", "teacher": "lower(x.teacher_name)", "students": "x.students"}

const overseenCols = `SELECT c.id, c.name, c.join_code, c.archived_at IS NOT NULL AS archived, c.created_at, c.teacher_id,
	coalesce(u.name, '') AS teacher_name, coalesce(u.email, '') AS teacher_email,
	(SELECT count(*) FROM content.enrolments e WHERE e.classroom_id = c.id AND e.status = 'active')::int AS students,
	(SELECT count(*) FROM content.enrolments e WHERE e.classroom_id = c.id AND e.status = 'pending')::int AS pending
	FROM content.classrooms c LEFT JOIN auth.users u ON u.id = c.teacher_id`

func scanOverseen(r pgx.Row) (OverseenClassroom, error) {
	var c OverseenClassroom
	err := r.Scan(&c.ID, &c.Name, &c.JoinCode, &c.Archived, &c.CreatedAt, &c.TeacherID, &c.TeacherName, &c.TeacherEmail, &c.Students, &c.Pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httpx.ErrNotFound
	}
	return c, err
}

// OverseeClassrooms lists every classroom, searched by its name or its teacher's.
func (s *Service) OverseeClassrooms(ctx context.Context, archived bool, p page.Request) ([]OverseenClassroom, int, error) {
	where := ` WHERE ($1 OR c.archived_at IS NULL) AND ($2 = '' OR c.name ILIKE $2 OR u.name ILIKE $2 OR u.email ILIKE $2)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM content.classrooms c LEFT JOIN auth.users u ON u.id = c.teacher_id`+where, archived, p.Like()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT * FROM (`+overseenCols+where+`) x`+p.OrderBy(OverseeSorts, "x.id")+p.Limit(), archived, p.Like())
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OverseenClassroom, error) { return scanOverseen(r) })
	if out == nil {
		out = []OverseenClassroom{}
	}
	return out, total, err
}

// OverseeClassroom is one classroom, for the admin.
func (s *Service) OverseeClassroom(ctx context.Context, id string) (OverseenClassroom, error) {
	return scanOverseen(s.pool.QueryRow(ctx, overseenCols+` WHERE c.id::text = $1`, id))
}

// OverseeEnrolments lists a classroom's students, of a status or all.
func (s *Service) OverseeEnrolments(ctx context.Context, classroomID, status string, p page.Request) ([]OverseenEnrolment, int, error) {
	where := ` FROM content.enrolments e JOIN auth.users u ON u.id = e.user_id
		WHERE e.classroom_id::text = $1 AND ($2 = '' OR e.status = $2) AND e.status <> 'removed' AND ($3 = '' OR u.name ILIKE $3 OR u.email ILIKE $3 OR e.student_number ILIKE $3)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, classroomID, status, p.Like()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.user_id, u.name, u.email, e.student_number, e.status, e.created_at`+where+
		` ORDER BY e.status = 'pending' DESC, lower(u.name), e.id`+p.Limit(), classroomID, status, p.Like())
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OverseenEnrolment, error) {
		var e OverseenEnrolment
		return e, r.Scan(&e.ID, &e.UserID, &e.Name, &e.Email, &e.StudentNumber, &e.Status, &e.CreatedAt)
	})
	if out == nil {
		out = []OverseenEnrolment{}
	}
	return out, total, err
}

// OverseeAddStudent enrols an active student, by email, at once.
func (s *Service) OverseeAddStudent(ctx context.Context, actorID, classroomID, email string) error {
	c, err := s.OverseeClassroom(ctx, classroomID)
	if err != nil {
		return err
	}
	var uid string
	err = s.pool.QueryRow(ctx, `SELECT id FROM auth.users WHERE email = lower(trim($1)) AND role = 'student' AND status = 'active'`, email).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.Invalid(map[string]string{"email": "no active student with that email address"})
	}
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO content.enrolments (classroom_id, user_id, status) VALUES ($1, $2, 'active')
			ON CONFLICT (classroom_id, user_id) DO UPDATE SET status = 'active'`, c.ID, uid); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "enrolment_added", "classroom", c.ID, map[string]string{"student": uid})
	})
}

// OverseeEnrolment approves (active) or removes a student.
func (s *Service) OverseeEnrolment(ctx context.Context, actorID, enrolmentID, status string) error {
	if status != "active" && status != "removed" {
		return httpx.BadRequest("status must be active or removed")
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var classroom, student string
		err := tx.QueryRow(ctx, `UPDATE content.enrolments SET status = $2 WHERE id::text = $1 RETURNING classroom_id, user_id`, enrolmentID, status).Scan(&classroom, &student)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "enrolment_"+map[string]string{"active": "approved", "removed": "removed"}[status], "classroom", classroom, map[string]string{"student": student})
	})
}

// OverseeUpdate renames or archives a classroom.
func (s *Service) OverseeUpdate(ctx context.Context, actorID, id string, name *string, archived *bool) (OverseenClassroom, error) {
	c, err := s.OverseeClassroom(ctx, id)
	if err != nil {
		return c, err
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if l := len([]rune(n)); l < 1 || l > 200 {
			return c, httpx.Invalid(map[string]string{"name": "name must be 1-200 characters"})
		}
		if _, err := s.pool.Exec(ctx, `UPDATE content.classrooms SET name = $2 WHERE id = $1`, c.ID, n); err != nil {
			return c, err
		}
	}
	if archived != nil {
		if _, err := s.pool.Exec(ctx, `UPDATE content.classrooms SET archived_at = CASE WHEN $2 THEN coalesce(archived_at, now()) END WHERE id = $1`, c.ID, *archived); err != nil {
			return c, err
		}
	}
	_ = audit.Log(ctx, s.pool, actorID, "classroom_updated", "classroom", c.ID, map[string]any{"name": name, "archived": archived})
	return s.OverseeClassroom(ctx, id)
}

// TransferResult says what moved, and what to check.
type TransferResult struct {
	Classroom OverseenClassroom `json:"classroom"`
	Quizzes   int               `json:"quizzes"`
	// BankQuizzes draw questions from a question bank: after the move, from
	// the new teacher's bank, so the teacher should check them.
	BankQuizzes int `json:"bank_quizzes"`
}

// OverseeTransfer moves a classroom to another teacher, with its quizzes,
// tutorials, presentations and past sessions, so they can run it and see
// its results. Nothing moves while one of its quiz sessions is open.
func (s *Service) OverseeTransfer(ctx context.Context, actorID, id, teacherID string) (TransferResult, error) {
	c, err := s.OverseeClassroom(ctx, id)
	if err != nil {
		return TransferResult{}, err
	}
	if c.TeacherID == teacherID {
		return TransferResult{}, httpx.Invalid(map[string]string{"teacher": "this teacher already owns the classroom"})
	}
	var live bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM live.sessions WHERE classroom_id = $1 AND status <> 'ended')`, c.ID).Scan(&live); err != nil {
		return TransferResult{}, err
	}
	if live {
		return TransferResult{}, httpx.NewError(http.StatusConflict, "session_live", "a quiz session in this classroom is still open: end it first")
	}
	var res TransferResult
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		const inClass = `topic_id IN (SELECT t.id FROM content.topics t JOIN content.modules m ON m.id = t.module_id WHERE m.classroom_id = $1)`
		if _, err := tx.Exec(ctx, `UPDATE content.classrooms SET teacher_id = $2 WHERE id = $1`, c.ID, teacherID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE quiz.quizzes SET teacher_id = $2 WHERE `+inClass, c.ID, teacherID)
		if err != nil {
			return err
		}
		res.Quizzes = int(tag.RowsAffected())
		for _, sql := range []string{
			`UPDATE tutorial.tutorials SET teacher_id = $2 WHERE ` + inClass,
			`UPDATE presentation.presentations SET teacher_id = $2 WHERE ` + inClass,
			`UPDATE live.sessions SET teacher_id = $2 WHERE classroom_id = $1`,
		} {
			if _, err := tx.Exec(ctx, sql, c.ID, teacherID); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM quiz.quizzes WHERE `+inClass+` AND pools @> '[{"source": "bank"}]'`, c.ID).Scan(&res.BankQuizzes); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "classroom_transferred", "classroom", c.ID, map[string]string{"from": c.TeacherID, "to": teacherID})
	})
	if err != nil {
		return res, err
	}
	res.Classroom, err = s.OverseeClassroom(ctx, id)
	return res, err
}

// OverseeRoutes mounts the classroom oversight under /api/admin.
func (s *Service) OverseeRoutes(r chi.Router, a *auth.Service) {
	r.With(a.RequireFeature(auth.FeatClasses)).Route("/classrooms", func(r chi.Router) {
		actor := func(r *http.Request) string { return auth.MustUser(r.Context()).ID }
		id := func(r *http.Request) string { return chi.URLParam(r, "id") }
		r.Method("GET", "/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			p := page.Parse(r, OverseeSorts, "name", false)
			list, total, err := s.OverseeClassrooms(r.Context(), r.URL.Query().Get("archived") == "1", p)
			if err != nil {
				return err
			}
			page.Write(w, "classrooms", list, total, p)
			return nil
		}))
		r.Method("GET", "/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			c, err := s.OverseeClassroom(r.Context(), id(r))
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, c)
			return nil
		}))
		r.Method("PATCH", "/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				Name     *string `json:"name"`
				Archived *bool   `json:"archived"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			c, err := s.OverseeUpdate(r.Context(), actor(r), id(r), in.Name, in.Archived)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, c)
			return nil
		}))
		r.Method("GET", "/{id}/students", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			p := page.Parse(r, page.Sorts{"name": "u.name"}, "name", false)
			list, total, err := s.OverseeEnrolments(r.Context(), id(r), r.URL.Query().Get("status"), p)
			if err != nil {
				return err
			}
			page.Write(w, "students", list, total, p)
			return nil
		}))
		r.Method("POST", "/{id}/students", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				Email string `json:"email"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			if err := s.OverseeAddStudent(r.Context(), actor(r), id(r), in.Email); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
		r.Method("PUT", "/{id}/students/{enrolment}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				Status string `json:"status"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			if err := s.OverseeEnrolment(r.Context(), actor(r), chi.URLParam(r, "enrolment"), in.Status); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
		r.Method("POST", "/{id}/owner", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				TeacherEmail string `json:"teacher_email"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			t, err := a.ActiveTeacherByEmail(r.Context(), in.TeacherEmail)
			if err != nil {
				return err
			}
			res, err := s.OverseeTransfer(r.Context(), actor(r), id(r), t.ID)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, res)
			return nil
		}))
		// Deleting, as its teacher would (D-92), on the teacher's behalf.
		r.Method("GET", "/{id}/delete-preview", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			c, err := s.OverseeClassroom(r.Context(), id(r))
			if err != nil {
				return err
			}
			p, err := s.DeletePreview(r.Context(), c.TeacherID, "classroom", c.ID)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, p)
			return nil
		}))
		r.Method("DELETE", "/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			c, err := s.OverseeClassroom(r.Context(), id(r))
			if err != nil {
				return err
			}
			if err := s.DeleteNode(r.Context(), c.TeacherID, "classroom", c.ID, r.URL.Query().Get("confirm")); err != nil {
				return err
			}
			_ = audit.Log(r.Context(), s.pool, actor(r), "classroom_deleted_by_staff", "classroom", c.ID, map[string]string{"name": c.Name, "teacher": c.TeacherID})
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
	})
}
