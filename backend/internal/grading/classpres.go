package grading

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Group presentations in a classroom (D-166). The teacher sets presentation
// tasks; each classroom group (D-158) uploads one presentation a task. The
// browser turns the PDF (or PowerPoint, converted through the classroom
// owner's Google Drive) into slide pictures. The teacher allows a group to
// present; then a member presses Present and it goes on the classroom's
// screen: they turn the slides on their own device and classmates who watch
// follow. The teacher can take over or stop it at any time, marks it (every
// member gets the group's mark, in the class report as a presentation), and
// may later delete the uploads, with or without their marks.

const (
	KindPresentation = "presentation"

	maxPresSlides     = 100
	maxPresSlideBytes = 1 << 20
	// TaskStorageLimit is what one task's slides may take, all groups together.
	TaskStorageLimit = 300 << 20
)

var (
	errNotUploader   = httpx.NewError(http.StatusForbidden, "not_uploader", "only your group's leader can upload its presentation")
	errNotAllowed    = httpx.NewError(http.StatusForbidden, "not_allowed", "your teacher hasn't let your group present this yet")
	errNotPresenting = httpx.NewError(http.StatusForbidden, "not_presenting", "you aren't presenting now")
	errNotEnrolled   = httpx.ErrNotFound
)

// SlideConverter turns a PowerPoint file into a PDF through a teacher's Google Drive.
type SlideConverter func(ctx context.Context, teacherID, name string, data []byte) ([]byte, error)

// SetSlideConverter gives the service the Drive conversion.
func (s *Service) SetSlideConverter(f SlideConverter) { s.convert = f }

// PresTask is a presentation task.
type PresTask struct {
	ID           string     `json:"id"`
	ClassroomID  string     `json:"classroom_id"`
	TopicID      *string    `json:"topic_id"`
	Topic        string     `json:"topic"`
	Title        string     `json:"title"`
	Instructions string     `json:"instructions"`
	UploadBy     string     `json:"upload_by"` // any member, or the leader
	OutOf        float64    `json:"out_of"`    // 0: not marked
	Rating       bool       `json:"rating"`
	DueAt        *time.Time `json:"due_at"`
	CreatedAt    time.Time  `json:"created_at"`
	Submitted    int        `json:"submitted"` // groups with slides in
	Marked       int        `json:"marked"`
	Groups       int        `json:"groups"` // the classroom's groups
}

const presTaskCols = `t.id::text, t.classroom_id::text, t.topic_id::text, coalesce(tp.name, ''), t.title, t.instructions, t.upload_by, t.out_of::float8,
	t.rating, t.due_at, t.created_at,
	(SELECT count(*)::int FROM grading.pres_submissions x WHERE x.task_id = t.id AND x.ready),
	(SELECT count(*)::int FROM grading.pres_submissions x WHERE x.task_id = t.id AND x.mark IS NOT NULL),
	(SELECT count(*)::int FROM content.categories c WHERE c.classroom_id = t.classroom_id)`

const presTaskFrom = ` FROM grading.pres_tasks t LEFT JOIN content.topics tp ON tp.id = t.topic_id`

func scanPresTask(r pgx.Row) (PresTask, error) {
	var t PresTask
	err := r.Scan(&t.ID, &t.ClassroomID, &t.TopicID, &t.Topic, &t.Title, &t.Instructions, &t.UploadBy, &t.OutOf, &t.Rating, &t.DueAt, &t.CreatedAt,
		&t.Submitted, &t.Marked, &t.Groups)
	return t, err
}

// PresTaskInput creates or changes a task.
type PresTaskInput struct {
	Title        string     `json:"title"`
	Instructions string     `json:"instructions"`
	TopicID      string     `json:"topic_id"` // optional
	UploadBy     string     `json:"upload_by"`
	OutOf        float64    `json:"out_of"`
	Rating       bool       `json:"rating"`
	DueAt        *time.Time `json:"due_at"`
}

func (in *PresTaskInput) check() error {
	in.Title = strings.TrimSpace(in.Title)
	in.Instructions = strings.TrimSpace(in.Instructions)
	if in.UploadBy == "" {
		in.UploadBy = "any"
	}
	bad := map[string]string{}
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 {
		bad["title"] = "a title of at most 200 characters"
	}
	if utf8.RuneCountInString(in.Instructions) > 5000 {
		bad["instructions"] = "at most 5000 characters"
	}
	if in.UploadBy != "any" && in.UploadBy != "leader" {
		bad["upload_by"] = "any or leader"
	}
	if in.OutOf < 0 || in.OutOf > 10000 || in.OutOf != round(in.OutOf, 2) {
		bad["out_of"] = "0 to 10000, to 2 decimal places"
	}
	if len(bad) > 0 {
		return httpx.Invalid(bad)
	}
	return nil
}

// teachesClass checks the teacher teaches the classroom.
func (s *Service) teachesClass(ctx context.Context, teacherID, classroomID string) error {
	if !isUUID(classroomID) {
		return httpx.ErrNotFound
	}
	_, err := s.teaches(ctx, teacherID, classroomID)
	return err
}

func isUUID(v string) bool {
	if len(v) != 36 {
		return false
	}
	for i, c := range v {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'):
			return false
		}
	}
	return true
}

// topicOf checks the topic is in the classroom; "" is none.
func (s *Service) topicOf(ctx context.Context, classroomID, topicID string) (any, error) {
	if topicID == "" {
		return nil, nil
	}
	var ok bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM content.topics t JOIN content.modules m ON m.id = t.module_id
		WHERE t.id::text = $1 AND m.classroom_id = $2)`, topicID, classroomID).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.Invalid(map[string]string{"topic_id": "a topic of this classroom"})
	}
	return topicID, nil
}

// PresTasks lists a classroom's presentation tasks, newest first.
func (s *Service) PresTasks(ctx context.Context, teacherID, classroomID string) ([]PresTask, error) {
	if err := s.teachesClass(ctx, teacherID, classroomID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+presTaskCols+presTaskFrom+` WHERE t.classroom_id = $1 ORDER BY t.created_at DESC`, classroomID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PresTask, error) { return scanPresTask(r) })
}

func (s *Service) presTask(ctx context.Context, id string) (PresTask, error) {
	t, err := scanPresTask(s.pool.QueryRow(ctx, `SELECT `+presTaskCols+presTaskFrom+` WHERE t.id::text = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return t, httpx.ErrNotFound
	}
	return t, err
}

// teacherTask is a task of a classroom the teacher teaches.
func (s *Service) teacherTask(ctx context.Context, teacherID, id string) (PresTask, error) {
	t, err := s.presTask(ctx, id)
	if err != nil {
		return t, err
	}
	return t, s.teachesClass(ctx, teacherID, t.ClassroomID)
}

// CreatePresTask adds a task to a classroom.
func (s *Service) CreatePresTask(ctx context.Context, teacherID, classroomID string, in PresTaskInput) (PresTask, error) {
	if err := s.teachesClass(ctx, teacherID, classroomID); err != nil {
		return PresTask{}, err
	}
	if err := in.check(); err != nil {
		return PresTask{}, err
	}
	topic, err := s.topicOf(ctx, classroomID, in.TopicID)
	if err != nil {
		return PresTask{}, err
	}
	var id string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO grading.pres_tasks (classroom_id, topic_id, title, instructions, upload_by, out_of, rating, due_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id::text`,
			classroomID, topic, in.Title, in.Instructions, in.UploadBy, in.OutOf, in.Rating, in.DueAt, teacherID).Scan(&id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "pres_task_created", "pres_task", id, map[string]any{"classroom_id": classroomID, "title": in.Title})
	})
	if err != nil {
		return PresTask{}, err
	}
	return s.presTask(ctx, id)
}

// UpdatePresTask changes a task.
func (s *Service) UpdatePresTask(ctx context.Context, teacherID, id string, in PresTaskInput) (PresTask, error) {
	t, err := s.teacherTask(ctx, teacherID, id)
	if err != nil {
		return t, err
	}
	if err := in.check(); err != nil {
		return t, err
	}
	topic, err := s.topicOf(ctx, t.ClassroomID, in.TopicID)
	if err != nil {
		return t, err
	}
	var over bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM grading.pres_submissions WHERE task_id = $1 AND mark > $2)`, id, in.OutOf).Scan(&over); err != nil {
		return t, err
	}
	if over && in.OutOf > 0 {
		return t, httpx.Invalid(map[string]string{"out_of": "some marks are higher than that"})
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE grading.pres_tasks SET topic_id = $2, title = $3, instructions = $4, upload_by = $5, out_of = $6, rating = $7, due_at = $8, updated_at = now()
			WHERE id = $1`, id, topic, in.Title, in.Instructions, in.UploadBy, in.OutOf, in.Rating, in.DueAt); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "pres_task_updated", "pres_task", id, in)
	})
	if err != nil {
		return t, err
	}
	return s.presTask(ctx, id)
}

// DeletePresTask removes a task with its presentations, marks and ratings.
func (s *Service) DeletePresTask(ctx context.Context, teacherID, id string) error {
	if _, err := s.teacherTask(ctx, teacherID, id); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, q := range []string{
			`DELETE FROM grading.report_items WHERE kind = 'presentation' AND item_id = $1`,
			`DELETE FROM grading.weights WHERE kind = 'presentation' AND item_id = $1`,
			`DELETE FROM grading.pres_tasks WHERE id = $1`,
		} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, teacherID, "pres_task_deleted", "pres_task", id, nil)
	})
}

// GroupPres is a group's presentation for a task.
type GroupPres struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	GroupID      string     `json:"group_id"`
	Group        string     `json:"group"` // its public name, or its name
	Title        string     `json:"title"`
	FileName     string     `json:"file_name"`
	Slides       int        `json:"slides"`
	Ready        bool       `json:"ready"`
	FilesRemoved bool       `json:"files_removed"`
	Allowed      bool       `json:"allowed"`
	UploadedBy   string     `json:"uploaded_by"`
	PresentedAt  *time.Time `json:"presented_at"`
	Mark         *float64   `json:"mark"`
	Comment      string     `json:"comment"`
	Rating       *float64   `json:"rating"`
	Ratings      int        `json:"ratings"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

const groupPresCols = `x.id::text, x.task_id::text, x.category_id::text, coalesce(c.public_name, c.name), x.title, x.file_name, x.slides, x.ready, x.files_removed,
	x.allowed_at IS NOT NULL, coalesce(u.name, ''), x.presented_at, x.mark::float8, x.comment, x.updated_at,
	(SELECT avg(stars)::float8 FROM grading.pres_ratings r WHERE r.submission_id = x.id),
	(SELECT count(*)::int FROM grading.pres_ratings r WHERE r.submission_id = x.id)`

const groupPresFrom = ` FROM grading.pres_submissions x JOIN content.categories c ON c.id = x.category_id LEFT JOIN auth.users u ON u.id = x.uploaded_by`

func scanGroupPres(r pgx.Row) (GroupPres, error) {
	var x GroupPres
	err := r.Scan(&x.ID, &x.TaskID, &x.GroupID, &x.Group, &x.Title, &x.FileName, &x.Slides, &x.Ready, &x.FilesRemoved, &x.Allowed, &x.UploadedBy,
		&x.PresentedAt, &x.Mark, &x.Comment, &x.UpdatedAt, &x.Rating, &x.Ratings)
	return x, err
}

func (s *Service) groupPres(ctx context.Context, id string) (GroupPres, error) {
	x, err := scanGroupPres(s.pool.QueryRow(ctx, `SELECT `+groupPresCols+groupPresFrom+` WHERE x.id::text = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return x, httpx.ErrNotFound
	}
	return x, err
}

// TaskGroup is one of the classroom's groups, with its presentation for the task.
type TaskGroup struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Members      int        `json:"members"`
	Presentation *GroupPres `json:"presentation"`
}

// TaskView is a task with every group's presentation, for the teacher.
type TaskView struct {
	Task   PresTask    `json:"task"`
	Groups []TaskGroup `json:"groups"`
	Stage  *ClassStage `json:"stage"`
}

// PresTaskView is a task with its groups, for the teacher.
func (s *Service) PresTaskView(ctx context.Context, teacherID, id string) (TaskView, error) {
	t, err := s.teacherTask(ctx, teacherID, id)
	if err != nil {
		return TaskView{}, err
	}
	v := TaskView{Task: t, Groups: []TaskGroup{}}
	rows, err := s.pool.Query(ctx, `SELECT c.id::text, coalesce(c.public_name, c.name),
			(SELECT count(*)::int FROM content.enrolment_categories ec JOIN content.enrolments e ON e.id = ec.enrolment_id
				WHERE ec.category_id = c.id AND e.status = 'active')
		FROM content.categories c WHERE c.classroom_id = $1 ORDER BY c.position, lower(c.name)`, t.ClassroomID)
	if err != nil {
		return v, err
	}
	if v.Groups, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (TaskGroup, error) {
		var g TaskGroup
		return g, r.Scan(&g.ID, &g.Name, &g.Members)
	}); err != nil {
		return v, err
	}
	rows, err = s.pool.Query(ctx, `SELECT `+groupPresCols+groupPresFrom+` WHERE x.task_id = $1`, id)
	if err != nil {
		return v, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (GroupPres, error) { return scanGroupPres(r) })
	if err != nil {
		return v, err
	}
	for i := range list {
		for j := range v.Groups {
			if v.Groups[j].ID == list[i].GroupID {
				v.Groups[j].Presentation = &list[i]
			}
		}
	}
	v.Stage, err = s.classStage(ctx, t.ClassroomID, "")
	return v, err
}

// teacherPres is a presentation in a classroom the teacher teaches, and its task.
func (s *Service) teacherPres(ctx context.Context, teacherID, id string) (PresTask, GroupPres, error) {
	x, err := s.groupPres(ctx, id)
	if err != nil {
		return PresTask{}, x, err
	}
	t, err := s.teacherTask(ctx, teacherID, x.TaskID)
	return t, x, err
}

// AllowPresenting lets a group present (or stops letting it). Stopping takes
// it off the screen if one of them is presenting it.
func (s *Service) AllowPresenting(ctx context.Context, teacherID, id string, allowed bool) error {
	t, x, err := s.teacherPres(ctx, teacherID, id)
	if err != nil {
		return err
	}
	if allowed && (!x.Ready || x.FilesRemoved) {
		return httpx.Conflict("its slides aren't all in")
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if allowed {
			if _, err := tx.Exec(ctx, `UPDATE grading.pres_submissions SET allowed_at = coalesce(allowed_at, now()) WHERE id = $1`, id); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE grading.pres_submissions SET allowed_at = NULL WHERE id = $1`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM grading.pres_stage WHERE classroom_id = $1 AND submission_id = $2 AND presenter IS NOT NULL`, t.ClassroomID, id); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, teacherID, "pres_allowed", "pres_submission", id, map[string]bool{"allowed": allowed})
	})
}

// MarkPres gives a group's presentation its mark; no mark takes it away.
func (s *Service) MarkPres(ctx context.Context, teacherID, id string, mark *float64, comment string) error {
	t, _, err := s.teacherPres(ctx, teacherID, id)
	if err != nil {
		return err
	}
	comment = strings.TrimSpace(comment)
	switch {
	case mark != nil && t.OutOf == 0:
		return httpx.Invalid(map[string]string{"mark": "set what this task is marked out of first"})
	case mark != nil && (*mark < 0 || *mark > t.OutOf || *mark != round(*mark, 2)):
		return httpx.Invalid(map[string]string{"mark": fmt.Sprintf("0 to %g, to 2 decimal places", t.OutOf)})
	case utf8.RuneCountInString(comment) > 1000:
		return httpx.Invalid(map[string]string{"comment": "at most 1000 characters"})
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE grading.pres_submissions SET mark = $2, comment = $3, updated_at = now() WHERE id = $1`, id, mark, comment); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "pres_marked", "pres_submission", id, map[string]any{"mark": mark})
	})
}

// DeletePres removes a group's presentation with its slides, mark and ratings.
func (s *Service) DeletePres(ctx context.Context, teacherID, id string) error {
	if _, _, err := s.teacherPres(ctx, teacherID, id); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM grading.pres_submissions WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "pres_deleted", "pres_submission", id, nil)
	})
}

// RemoveSlides deletes uploaded slides to free space, keeping marks and
// ratings: one presentation's (id), or every one of a task's (taskID).
func (s *Service) RemoveSlides(ctx context.Context, teacherID, taskID, id string) (int, error) {
	var where string
	var arg string
	if id != "" {
		t, _, err := s.teacherPres(ctx, teacherID, id)
		if err != nil {
			return 0, err
		}
		taskID, where, arg = t.ID, `x.id::text = $1`, id
	} else {
		if _, err := s.teacherTask(ctx, teacherID, taskID); err != nil {
			return 0, err
		}
		where, arg = `x.task_id::text = $1`, taskID
	}
	var n int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM grading.pres_stage st USING grading.pres_submissions x WHERE st.submission_id = x.id AND `+where, arg); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM grading.pres_slides sl USING grading.pres_submissions x WHERE sl.submission_id = x.id AND `+where, arg); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE grading.pres_submissions x SET files_removed = true, ready = false, allowed_at = NULL, updated_at = now()
			WHERE NOT x.files_removed AND `+where, arg)
		if err != nil {
			return err
		}
		n = int(tag.RowsAffected())
		return audit.Log(ctx, tx, teacherID, "pres_slides_removed", "pres_task", taskID, map[string]any{"submission_id": id, "count": n})
	})
	return n, err
}

// ---------- students ----------

// member is a student's active enrolment in the classroom, and their groups there.
type member struct {
	enrolmentID string
	groups      map[string]string // group id: role
}

func (s *Service) memberOf(ctx context.Context, userID, classroomID string) (member, error) {
	m := member{groups: map[string]string{}}
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM content.enrolments WHERE classroom_id::text = $1 AND user_id = $2 AND status = 'active'`,
		classroomID, userID).Scan(&m.enrolmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, errNotEnrolled
	}
	if err != nil {
		return m, err
	}
	rows, err := s.pool.Query(ctx, `SELECT category_id::text, role FROM content.enrolment_categories WHERE enrolment_id = $1`, m.enrolmentID)
	if err != nil {
		return m, err
	}
	for rows.Next() {
		var g, role string
		if err := rows.Scan(&g, &role); err != nil {
			rows.Close()
			return m, err
		}
		m.groups[g] = role
	}
	rows.Close()
	return m, rows.Err()
}

// mayUpload says whether a member may upload for a group under a task.
func (m member) mayUpload(t PresTask, group string) bool {
	role, in := m.groups[group]
	return in && (t.UploadBy == "any" || role == "leader" || role == "assistant")
}

// MyGroupTask is a task as a student sees it, with their groups' presentations.
type MyGroupTask struct {
	PresTask
	MyGroups []MyTaskGroup `json:"my_groups"`
}

// MyTaskGroup is one of the student's groups, for a task.
type MyTaskGroup struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	CanUpload    bool       `json:"can_upload"`
	Presentation *GroupPres `json:"presentation"`
}

// MyClassPresentations is what a student sees in their classroom.
type MyClassPresentations struct {
	Tasks      []MyGroupTask `json:"tasks"`
	Stage      *ClassStage   `json:"stage"`
	CanConvert bool          `json:"can_convert"`
}

// MyPresentations is a student's view of their classroom's presentation tasks.
func (s *Service) MyPresentations(ctx context.Context, userID, classroomID string) (MyClassPresentations, error) {
	v := MyClassPresentations{Tasks: []MyGroupTask{}, CanConvert: s.convert != nil}
	m, err := s.memberOf(ctx, userID, classroomID)
	if err != nil {
		return v, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+presTaskCols+presTaskFrom+` WHERE t.classroom_id::text = $1 ORDER BY t.created_at DESC`, classroomID)
	if err != nil {
		return v, err
	}
	tasks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PresTask, error) { return scanPresTask(r) })
	if err != nil {
		return v, err
	}
	ids := make([]string, 0, len(m.groups))
	for g := range m.groups {
		ids = append(ids, g)
	}
	rows, err = s.pool.Query(ctx, `SELECT c.id::text, coalesce(c.public_name, c.name) FROM content.categories c WHERE c.id::text = ANY($1) ORDER BY c.position, lower(c.name)`, ids)
	if err != nil {
		return v, err
	}
	type group struct{ id, name string }
	groups, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (group, error) {
		var g group
		return g, r.Scan(&g.id, &g.name)
	})
	if err != nil {
		return v, err
	}
	rows, err = s.pool.Query(ctx, `SELECT `+groupPresCols+groupPresFrom+` JOIN grading.pres_tasks t ON t.id = x.task_id
		WHERE t.classroom_id::text = $1 AND x.category_id::text = ANY($2)`, classroomID, ids)
	if err != nil {
		return v, err
	}
	subs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (GroupPres, error) { return scanGroupPres(r) })
	if err != nil {
		return v, err
	}
	for _, t := range tasks {
		mt := MyGroupTask{PresTask: t, MyGroups: []MyTaskGroup{}}
		for _, g := range groups {
			tg := MyTaskGroup{ID: g.id, Name: g.name, CanUpload: m.mayUpload(t, g.id)}
			for i := range subs {
				if subs[i].TaskID == t.ID && subs[i].GroupID == g.id {
					tg.Presentation = &subs[i]
				}
			}
			mt.MyGroups = append(mt.MyGroups, tg)
		}
		v.Tasks = append(v.Tasks, mt)
	}
	v.Stage, err = s.classStage(ctx, classroomID, userID)
	return v, err
}

// GroupPresInput starts (or replaces) a group's presentation; slides come next.
type GroupPresInput struct {
	GroupID  string `json:"group_id"`
	Title    string `json:"title"`
	FileName string `json:"file_name"`
	Slides   int    `json:"slides"`
}

// StartGroupPres readies a group's presentation for its slides: the old ones go.
func (s *Service) StartGroupPres(ctx context.Context, userID, taskID string, in GroupPresInput) (GroupPres, error) {
	t, err := s.presTask(ctx, taskID)
	if err != nil {
		return GroupPres{}, err
	}
	m, err := s.memberOf(ctx, userID, t.ClassroomID)
	if err != nil {
		return GroupPres{}, err
	}
	if _, in := m.groups[in.GroupID]; !in {
		return GroupPres{}, httpx.NewError(http.StatusForbidden, "no_group", "you aren't in that group")
	}
	if !m.mayUpload(t, in.GroupID) {
		return GroupPres{}, errNotUploader
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		in.Title = strings.TrimSpace(strings.TrimSuffix(in.FileName, fileExt(in.FileName)))
	}
	bad := map[string]string{}
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 {
		bad["title"] = "a title of at most 200 characters"
	}
	if in.Slides < 1 || in.Slides > maxPresSlides {
		bad["slides"] = fmt.Sprintf("1 to %d slides", maxPresSlides)
	}
	if r := []rune(in.FileName); len(r) > 200 {
		in.FileName = string(r[:200])
	}
	if len(bad) > 0 {
		return GroupPres{}, httpx.Invalid(bad)
	}
	var id string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Not while it's on the screen: the class would lose its slides.
		var onStage bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM grading.pres_stage st JOIN grading.pres_submissions x ON x.id = st.submission_id
			WHERE x.task_id = $1 AND x.category_id::text = $2)`, taskID, in.GroupID).Scan(&onStage); err != nil {
			return err
		}
		if onStage {
			return httpx.Conflict("this presentation is on the screen now; replace it afterwards")
		}
		// A new upload needs the teacher's permission again.
		if err := tx.QueryRow(ctx, `INSERT INTO grading.pres_submissions (task_id, category_id, uploaded_by, title, file_name, slides)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (task_id, category_id) DO UPDATE SET uploaded_by = $3, title = $4, file_name = $5, slides = $6, ready = false,
				files_removed = false, allowed_at = NULL, updated_at = now()
			RETURNING id::text`, taskID, in.GroupID, userID, in.Title, in.FileName, in.Slides).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM grading.pres_slides WHERE submission_id = $1`, id)
		return err
	})
	if err != nil {
		return GroupPres{}, err
	}
	return s.groupPres(ctx, id)
}

func fileExt(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i:]
	}
	return ""
}

// ownPres is a presentation of one of the student's groups, with its task and their membership.
func (s *Service) ownPres(ctx context.Context, userID, id string) (PresTask, GroupPres, member, error) {
	x, err := s.groupPres(ctx, id)
	if err != nil {
		return PresTask{}, x, member{}, err
	}
	t, err := s.presTask(ctx, x.TaskID)
	if err != nil {
		return t, x, member{}, err
	}
	m, err := s.memberOf(ctx, userID, t.ClassroomID)
	if err != nil {
		return t, x, m, err
	}
	if _, in := m.groups[x.GroupID]; !in {
		return t, x, m, httpx.ErrNotFound
	}
	return t, x, m, nil
}

// PutPresSlide stores one slide picture, a JPEG.
func (s *Service) PutPresSlide(ctx context.Context, userID, id string, n int, image []byte, text string) error {
	t, x, m, err := s.ownPres(ctx, userID, id)
	if err != nil {
		return err
	}
	if !m.mayUpload(t, x.GroupID) {
		return errNotUploader
	}
	if n < 1 || n > x.Slides {
		return httpx.Invalid(map[string]string{"n": fmt.Sprintf("slide 1 to %d", x.Slides)})
	}
	if len(image) == 0 || len(image) > maxPresSlideBytes || !bytes.HasPrefix(image, []byte{0xFF, 0xD8, 0xFF}) {
		return httpx.Invalid(map[string]string{"image": "a JPEG picture of at most 1 MB"})
	}
	if r := []rune(text); len(r) > 5000 {
		text = string(r[:5000])
	}
	var used int64
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(length(sl.image)), 0) FROM grading.pres_slides sl JOIN grading.pres_submissions x ON x.id = sl.submission_id
		WHERE x.task_id = $1 AND NOT (sl.submission_id = $2 AND sl.n = $3)`, t.ID, id, n).Scan(&used); err != nil {
		return err
	}
	if used+int64(len(image)) > TaskStorageLimit {
		return httpx.NewError(http.StatusInsufficientStorage, "task_full", "this task's presentations have used all their space; ask your teacher")
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO grading.pres_slides (submission_id, n, image, text) VALUES ($1, $2, $3, $4)
		ON CONFLICT (submission_id, n) DO UPDATE SET image = $3, text = $4`, id, n, image, text)
	return err
}

// FinishGroupPres marks it ready once every slide is in.
func (s *Service) FinishGroupPres(ctx context.Context, userID, id string) (GroupPres, error) {
	t, x, m, err := s.ownPres(ctx, userID, id)
	if err != nil {
		return x, err
	}
	if !m.mayUpload(t, x.GroupID) {
		return x, errNotUploader
	}
	var have int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM grading.pres_slides WHERE submission_id = $1 AND n <= $2`, id, x.Slides).Scan(&have); err != nil {
		return x, err
	}
	if have != x.Slides {
		return x, httpx.Invalid(map[string]string{"slides": fmt.Sprintf("%d of %d slides are in", have, x.Slides)})
	}
	if _, err := s.pool.Exec(ctx, `UPDATE grading.pres_submissions SET ready = true, updated_at = now() WHERE id = $1`, id); err != nil {
		return x, err
	}
	return s.groupPres(ctx, id)
}

// ConvertForClass turns a student's PowerPoint file into a PDF through the
// classroom owner's Google Drive.
func (s *Service) ConvertForClass(ctx context.Context, userID, classroomID, name string, data []byte) ([]byte, error) {
	if _, err := s.memberOf(ctx, userID, classroomID); err != nil {
		return nil, err
	}
	if s.convert == nil {
		return nil, httpx.NewError(http.StatusConflict, "no_drive", "PowerPoint can't be turned into slides here: save it as a PDF and upload that")
	}
	var owner string
	if err := s.pool.QueryRow(ctx, `SELECT teacher_id::text FROM content.classrooms WHERE id::text = $1`, classroomID).Scan(&owner); err != nil {
		return nil, err
	}
	return s.convert(ctx, owner, name, data)
}

// RatePres is a classmate's 1-5 for a presentation they've seen; not their own group's.
func (s *Service) RatePres(ctx context.Context, userID, id string, stars int) error {
	x, err := s.groupPres(ctx, id)
	if err != nil {
		return err
	}
	t, err := s.presTask(ctx, x.TaskID)
	if err != nil {
		return err
	}
	m, err := s.memberOf(ctx, userID, t.ClassroomID)
	if err != nil {
		return err
	}
	switch {
	case !t.Rating:
		return httpx.NewError(http.StatusConflict, "no_rating", "this task doesn't ask for ratings")
	case stars < 1 || stars > 5:
		return httpx.Invalid(map[string]string{"stars": "1 to 5"})
	case x.PresentedAt == nil:
		return httpx.Conflict("it hasn't been presented yet")
	}
	if _, own := m.groups[x.GroupID]; own {
		return httpx.NewError(http.StatusForbidden, "own_presentation", "you can't rate your own group's presentation")
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO grading.pres_ratings (submission_id, user_id, stars) VALUES ($1, $2, $3)
		ON CONFLICT (submission_id, user_id) DO UPDATE SET stars = $3`, id, userID, stars)
	return err
}

// ---------- the classroom's screen ----------

// ClassStage is what's on the classroom's screen.
type ClassStage struct {
	SubmissionID  string    `json:"submission_id"`
	TaskID        string    `json:"task_id"`
	Task          string    `json:"task"`
	Title         string    `json:"title"`
	Group         string    `json:"group"`
	Slide         int       `json:"slide"`
	Slides        int       `json:"slides"`
	PresenterName string    `json:"presenter_name,omitempty"` // empty: the teacher
	Presenting    bool      `json:"presenting"`               // the one asking turns the slides
	Mine          bool      `json:"mine"`                     // it's the asking student's group's
	Rating        bool      `json:"rating"`                   // classmates may rate it
	MyStars       int       `json:"my_stars"`                 // what the asking student gave it
	StartedAt     time.Time `json:"started_at"`
}

// classStage is the classroom's stage, or nil; userID, a student asking.
func (s *Service) classStage(ctx context.Context, classroomID, userID string) (*ClassStage, error) {
	var st ClassStage
	var presenter *string
	var group string
	err := s.pool.QueryRow(ctx, `SELECT x.id::text, t.id::text, t.title, x.title, x.category_id::text, coalesce(c.public_name, c.name), st.slide, x.slides,
			st.presenter::text, coalesce(u.name, ''), t.rating, st.started_at,
			coalesce((SELECT stars FROM grading.pres_ratings r WHERE r.submission_id = x.id AND r.user_id::text = $2), 0)
		FROM grading.pres_stage st JOIN grading.pres_submissions x ON x.id = st.submission_id JOIN grading.pres_tasks t ON t.id = x.task_id
		JOIN content.categories c ON c.id = x.category_id LEFT JOIN auth.users u ON u.id = st.presenter
		WHERE st.classroom_id::text = $1`, classroomID, userID).
		Scan(&st.SubmissionID, &st.TaskID, &st.Task, &st.Title, &group, &st.Group, &st.Slide, &st.Slides, &presenter, &st.PresenterName, &st.Rating, &st.StartedAt, &st.MyStars)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if userID != "" {
		st.Presenting = presenter != nil && *presenter == userID
		var mine bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM content.enrolment_categories ec JOIN content.enrolments e ON e.id = ec.enrolment_id
			WHERE ec.category_id::text = $1 AND e.user_id::text = $2)`, group, userID).Scan(&mine); err != nil {
			return nil, err
		}
		st.Mine = mine
	}
	return &st, nil
}

// TeacherStage is the classroom's stage, for its teachers.
func (s *Service) TeacherStage(ctx context.Context, teacherID, classroomID string) (*ClassStage, error) {
	if err := s.teachesClass(ctx, teacherID, classroomID); err != nil {
		return nil, err
	}
	return s.classStage(ctx, classroomID, "")
}

// MyStage is the classroom's stage, for a student.
func (s *Service) MyStage(ctx context.Context, userID, classroomID string) (*ClassStage, error) {
	if _, err := s.memberOf(ctx, userID, classroomID); err != nil {
		return nil, err
	}
	return s.classStage(ctx, classroomID, userID)
}

func (s *Service) putOnStage(ctx context.Context, actor, classroomID, id string, presenter any, keepSlide bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := `INSERT INTO grading.pres_stage (classroom_id, submission_id, slide, presenter) VALUES ($1, $2, 1, $3)
			ON CONFLICT (classroom_id) DO UPDATE SET submission_id = $2, slide = 1, presenter = $3, started_at = now()`
		if keepSlide {
			q = `INSERT INTO grading.pres_stage (classroom_id, submission_id, slide, presenter) VALUES ($1, $2, 1, $3)
				ON CONFLICT (classroom_id) DO UPDATE SET presenter = $3,
					slide = CASE WHEN grading.pres_stage.submission_id = $2 THEN grading.pres_stage.slide ELSE 1 END,
					started_at = CASE WHEN grading.pres_stage.submission_id = $2 THEN grading.pres_stage.started_at ELSE now() END,
					submission_id = $2`
		}
		if _, err := tx.Exec(ctx, q, classroomID, id, presenter); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE grading.pres_submissions SET presented_at = coalesce(presented_at, now()) WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actor, "pres_on_screen", "pres_submission", id, map[string]any{"presenter": presenter})
	})
}

// ShowPres puts a presentation on the screen for the teacher to present (or
// takes over from the student presenting it, on the same slide).
func (s *Service) ShowPres(ctx context.Context, teacherID, id string) error {
	t, x, err := s.teacherPres(ctx, teacherID, id)
	if err != nil {
		return err
	}
	if !x.Ready || x.FilesRemoved {
		return httpx.Conflict("its slides aren't all in")
	}
	return s.putOnStage(ctx, teacherID, t.ClassroomID, id, nil, true)
}

// StopStage takes whatever is on the screen off it.
func (s *Service) StopStage(ctx context.Context, teacherID, classroomID string) error {
	if err := s.teachesClass(ctx, teacherID, classroomID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM grading.pres_stage WHERE classroom_id = $1`, classroomID)
	return err
}

// moveStage goes to slide n of what's on the classroom's screen; for a
// student, only while they are presenting it.
func (s *Service) moveStage(ctx context.Context, classroomID, presenter string, n int) error {
	q := `UPDATE grading.pres_stage st SET slide = $2 FROM grading.pres_submissions x
		WHERE st.classroom_id::text = $1 AND x.id = st.submission_id AND $2 BETWEEN 1 AND x.slides`
	args := []any{classroomID, n}
	if presenter != "" {
		q += ` AND st.presenter::text = $3`
		args = append(args, presenter)
	}
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if presenter != "" {
			var presenting bool
			if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM grading.pres_stage WHERE classroom_id::text = $1 AND presenter::text = $2)`, classroomID, presenter).Scan(&presenting); err != nil {
				return err
			}
			if !presenting {
				return errNotPresenting
			}
		}
		return httpx.Invalid(map[string]string{"n": "no such slide, or nothing is being presented"})
	}
	return nil
}

// TeacherTurn moves the screen's slide; the teacher can always.
func (s *Service) TeacherTurn(ctx context.Context, teacherID, classroomID string, n int) error {
	if err := s.teachesClass(ctx, teacherID, classroomID); err != nil {
		return err
	}
	return s.moveStage(ctx, classroomID, "", n)
}

// Present puts the student's group's presentation on the classroom's screen,
// with them turning the slides, once the teacher has allowed it and nothing
// else is on.
func (s *Service) Present(ctx context.Context, userID, id string) (*ClassStage, error) {
	t, x, _, err := s.ownPres(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if !x.Allowed || !x.Ready || x.FilesRemoved {
		return nil, errNotAllowed
	}
	cur, err := s.classStage(ctx, t.ClassroomID, userID)
	if err != nil {
		return nil, err
	}
	if cur != nil && cur.SubmissionID != id {
		who := cur.PresenterName
		if who == "" {
			who = "your teacher"
		}
		return nil, httpx.Conflict(fmt.Sprintf("%s is presenting “%s” now; try when it ends", who, cur.Title))
	}
	if cur != nil && cur.PresenterName == "" {
		return nil, httpx.Conflict("your teacher is presenting it now")
	}
	if err := s.putOnStage(ctx, userID, t.ClassroomID, id, userID, true); err != nil {
		return nil, err
	}
	return s.classStage(ctx, t.ClassroomID, userID)
}

// StudentTurn moves the slide, for the student presenting.
func (s *Service) StudentTurn(ctx context.Context, userID, classroomID string, n int) error {
	if _, err := s.memberOf(ctx, userID, classroomID); err != nil {
		return err
	}
	return s.moveStage(ctx, classroomID, userID, n)
}

// EndMyPresenting takes the student's presentation off the screen.
func (s *Service) EndMyPresenting(ctx context.Context, userID, classroomID string) error {
	if _, err := s.memberOf(ctx, userID, classroomID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM grading.pres_stage WHERE classroom_id::text = $1 AND presenter = $2`, classroomID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotPresenting
	}
	return nil
}

func (s *Service) presSlide(ctx context.Context, id string, n int) ([]byte, error) {
	var img []byte
	err := s.pool.QueryRow(ctx, `SELECT image FROM grading.pres_slides WHERE submission_id::text = $1 AND n = $2`, id, n).Scan(&img)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return img, err
}

// TeacherPresSlide is a slide, for the classroom's teachers.
func (s *Service) TeacherPresSlide(ctx context.Context, teacherID, id string, n int) ([]byte, error) {
	if _, _, err := s.teacherPres(ctx, teacherID, id); err != nil {
		return nil, err
	}
	return s.presSlide(ctx, id, n)
}

// MyPresSlide is a slide for a student: of their group's presentation, or of
// what's on their classroom's screen.
func (s *Service) MyPresSlide(ctx context.Context, userID, id string, n int) ([]byte, error) {
	x, err := s.groupPres(ctx, id)
	if err != nil {
		return nil, err
	}
	t, err := s.presTask(ctx, x.TaskID)
	if err != nil {
		return nil, err
	}
	m, err := s.memberOf(ctx, userID, t.ClassroomID)
	if err != nil {
		return nil, err
	}
	if _, own := m.groups[x.GroupID]; !own {
		var onStage bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM grading.pres_stage WHERE classroom_id = $1 AND submission_id::text = $2)`, t.ClassroomID, id).Scan(&onStage); err != nil {
			return nil, err
		}
		if !onStage {
			return nil, httpx.ErrNotFound
		}
	}
	return s.presSlide(ctx, id, n)
}
