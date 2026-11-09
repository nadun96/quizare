package grading

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Offline assignments (D-159): work done away from the platform - a project,
// a practical, a paper test - kept in a topic like a quiz. The teacher enters
// marks here or through a downloaded sheet. In a group assignment the mark is
// the classroom group's (D-158), and each member gets it unless the teacher
// gives them their own. Marks count in the gradebook and the class report,
// and students see theirs once the teacher releases them.

const (
	ModeIndividual = "individual"
	ModeGroup      = "group"

	KindOffline = "offline"
)

// offlineMarksSQL is each active student's mark on each offline assignment of
// classroom $1: their own, else (in a group assignment) their first group's
// that has one. Columns: user_id, assignment id, score, out of.
const offlineMarksSQL = `
	SELECT e.user_id::text AS user_id, a.id::text AS assignment_id, x.score, a.max_marks::float8 AS max_marks
	FROM grading.offline_assignments a
	JOIN content.enrolments e ON e.classroom_id = a.classroom_id AND e.status = 'active'
	CROSS JOIN LATERAL (SELECT coalesce(
		(SELECT m.score FROM grading.offline_marks m WHERE m.assignment_id = a.id AND m.user_id = e.user_id),
		(SELECT g.score FROM content.enrolment_categories ec JOIN content.categories c ON c.id = ec.category_id
			JOIN grading.offline_group_marks g ON g.category_id = c.id AND g.assignment_id = a.id
			WHERE ec.enrolment_id = e.id AND a.mode = 'group' ORDER BY c.position, lower(c.name) LIMIT 1))::float8 AS score) x
	WHERE a.classroom_id = $1 AND x.score IS NOT NULL`

// Offline is an offline assignment.
type Offline struct {
	ID           string     `json:"id"`
	ClassroomID  string     `json:"classroom_id"`
	TopicID      string     `json:"topic_id"`
	Title        string     `json:"title"`
	Instructions string     `json:"instructions"`
	MaxMarks     float64    `json:"max_marks"`
	Mode         string     `json:"mode"` // individual or group
	DueAt        *time.Time `json:"due_at"`
	ReleasedAt   *time.Time `json:"released_at"`
	CreatedAt    time.Time  `json:"created_at"`
	Marked       int        `json:"marked"`   // students with a mark
	Students     int        `json:"students"` // active students
}

// OfflineInput creates or changes one.
type OfflineInput struct {
	Title        string     `json:"title"`
	Instructions string     `json:"instructions"`
	MaxMarks     float64    `json:"max_marks"`
	Mode         string     `json:"mode"`
	DueAt        *time.Time `json:"due_at"`
}

func (in *OfflineInput) check() error {
	in.Title = strings.TrimSpace(in.Title)
	in.Instructions = strings.TrimSpace(in.Instructions)
	if in.Mode == "" {
		in.Mode = ModeIndividual
	}
	bad := map[string]string{}
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 {
		bad["title"] = "a title of at most 200 characters"
	}
	if utf8.RuneCountInString(in.Instructions) > 5000 {
		bad["instructions"] = "at most 5000 characters"
	}
	if in.MaxMarks <= 0 || in.MaxMarks > 10000 || in.MaxMarks != round(in.MaxMarks, 2) {
		bad["max_marks"] = "more than 0 and at most 10000, to 2 decimal places"
	}
	if in.Mode != ModeIndividual && in.Mode != ModeGroup {
		bad["mode"] = "individual or group"
	}
	if len(bad) > 0 {
		return httpx.Invalid(bad)
	}
	return nil
}

const offlineCols = `a.id::text, a.classroom_id::text, a.topic_id::text, a.title, a.instructions, a.max_marks::float8, a.mode, a.due_at, a.released_at, a.created_at,
	(SELECT count(*)::int FROM (` + offlineMarksSQL + `) x WHERE x.assignment_id = a.id::text),
	(SELECT count(*)::int FROM content.enrolments e WHERE e.classroom_id = a.classroom_id AND e.status = 'active')`

func scanOffline(r pgx.Row) (Offline, error) {
	var o Offline
	err := r.Scan(&o.ID, &o.ClassroomID, &o.TopicID, &o.Title, &o.Instructions, &o.MaxMarks, &o.Mode, &o.DueAt, &o.ReleasedAt, &o.CreatedAt, &o.Marked, &o.Students)
	return o, err
}

// topicClassroom is the classroom of a topic the teacher teaches.
func (s *Service) topicClassroom(ctx context.Context, teacherID, topicID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT m.classroom_id::text FROM content.topics t JOIN content.modules m ON m.id = t.module_id
		WHERE t.id = $1 AND content.teaches(m.classroom_id, $2)`, topicID, teacherID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound
	}
	return id, err
}

// ListOffline is a topic's offline assignments.
func (s *Service) ListOffline(ctx context.Context, teacherID, topicID string) ([]Offline, error) {
	classroom, err := s.topicClassroom(ctx, teacherID, topicID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+offlineCols+` FROM grading.offline_assignments a
		WHERE a.classroom_id = $1 AND a.topic_id = $2 ORDER BY a.created_at`, classroom, topicID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Offline, error) { return scanOffline(r) })
}

// getOffline reads one by id, with the counts for its classroom.
func (s *Service) getOffline(ctx context.Context, classroomID, id string) (Offline, error) {
	o, err := scanOffline(s.pool.QueryRow(ctx, `SELECT `+offlineCols+` FROM grading.offline_assignments a WHERE a.classroom_id = $1 AND a.id = $2`, classroomID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return o, httpx.ErrNotFound
	}
	return o, err
}

// offlineClassroom finds the classroom of an assignment the teacher teaches.
func (s *Service) offlineClassroom(ctx context.Context, teacherID, id string) (string, error) {
	var classroom string
	err := s.pool.QueryRow(ctx, `SELECT classroom_id::text FROM grading.offline_assignments WHERE id = $1 AND content.teaches(classroom_id, $2)`, id, teacherID).Scan(&classroom)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound
	}
	return classroom, err
}

// CreateOffline adds one to a topic.
func (s *Service) CreateOffline(ctx context.Context, teacherID, topicID string, in OfflineInput) (Offline, error) {
	classroom, err := s.topicClassroom(ctx, teacherID, topicID)
	if err != nil {
		return Offline{}, err
	}
	if err := in.check(); err != nil {
		return Offline{}, err
	}
	var id string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO grading.offline_assignments (classroom_id, topic_id, title, instructions, max_marks, mode, due_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id::text`, classroom, topicID, in.Title, in.Instructions, in.MaxMarks, in.Mode, in.DueAt, teacherID).Scan(&id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "offline_created", "offline_assignment", id, map[string]any{"title": in.Title, "mode": in.Mode})
	})
	if err != nil {
		return Offline{}, err
	}
	return s.getOffline(ctx, classroom, id)
}

// UpdateOffline changes it. Out of fewer marks than someone already has isn't allowed.
func (s *Service) UpdateOffline(ctx context.Context, teacherID, id string, in OfflineInput) (Offline, error) {
	classroom, err := s.offlineClassroom(ctx, teacherID, id)
	if err != nil {
		return Offline{}, err
	}
	if err := in.check(); err != nil {
		return Offline{}, err
	}
	var top float64
	if err := s.pool.QueryRow(ctx, `SELECT greatest(
			coalesce((SELECT max(score) FROM grading.offline_marks WHERE assignment_id = $1), 0),
			coalesce((SELECT max(score) FROM grading.offline_group_marks WHERE assignment_id = $1), 0))::float8`, id).Scan(&top); err != nil {
		return Offline{}, err
	}
	if in.MaxMarks < top {
		return Offline{}, httpx.Invalid(map[string]string{"max_marks": "a mark already given is higher than this"})
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE grading.offline_assignments SET title = $2, instructions = $3, max_marks = $4, mode = $5, due_at = $6, updated_at = now() WHERE id = $1`,
			id, in.Title, in.Instructions, in.MaxMarks, in.Mode, in.DueAt); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "offline_updated", "offline_assignment", id, nil)
	})
	if err != nil {
		return Offline{}, err
	}
	return s.getOffline(ctx, classroom, id)
}

// DeleteOffline removes it with its marks.
func (s *Service) DeleteOffline(ctx context.Context, teacherID, id string) error {
	classroom, err := s.offlineClassroom(ctx, teacherID, id)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM grading.offline_assignments WHERE id = $1`, id); err != nil {
			return err
		}
		for _, table := range []string{"grading.report_items", "grading.weights", "grading.result_exceptions"} {
			if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE classroom_id = $1 AND kind = 'offline' AND item_id = $2`, classroom, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM grading.groups WHERE classroom_id = $1 AND kind = 'offline' AND item_id = $2`, classroom, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "offline_deleted", "offline_assignment", id, nil)
	})
}

// ReleaseOffline shows students their marks, or hides them again.
func (s *Service) ReleaseOffline(ctx context.Context, teacherID, id string, released bool) (Offline, error) {
	classroom, err := s.offlineClassroom(ctx, teacherID, id)
	if err != nil {
		return Offline{}, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE grading.offline_assignments SET released_at = CASE WHEN $2 THEN coalesce(released_at, now()) END WHERE id = $1`, id, released); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "offline_released", "offline_assignment", id, map[string]bool{"released": released})
	})
	if err != nil {
		return Offline{}, err
	}
	return s.getOffline(ctx, classroom, id)
}

// OfflineStudent is a student's row on the mark sheet.
type OfflineStudent struct {
	UserID        string   `json:"user_id"`
	Name          string   `json:"name"`
	StudentNumber *string  `json:"student_number"`
	Groups        []string `json:"groups"`               // every group they're in
	GroupID       *string  `json:"group_id,omitempty"`   // the group whose mark they get
	GroupName     string   `json:"group_name,omitempty"` // its name
	GroupScore    *float64 `json:"group_score,omitempty"`
	Own           *float64 `json:"own"` // their own mark
	OwnComment    string   `json:"own_comment"`
	Score         *float64 `json:"score"` // the mark that counts
	Comment       string   `json:"comment"`
}

// OfflineGroup is a classroom group's row.
type OfflineGroup struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Color   int      `json:"color"`
	Members []string `json:"members"` // names
	Score   *float64 `json:"score"`
	Comment string   `json:"comment"`
}

// OfflineSheet is the assignment with everyone's marks.
type OfflineSheet struct {
	Offline
	Classroom string           `json:"classroom"`
	Topic     string           `json:"topic"`
	Module    string           `json:"module"`
	Students  []OfflineStudent `json:"students"`
	Groups    []OfflineGroup   `json:"groups"` // the classroom's groups
}

// Sheet is the assignment with its marks.
func (s *Service) Sheet(ctx context.Context, teacherID, id string) (OfflineSheet, error) {
	classroom, err := s.offlineClassroom(ctx, teacherID, id)
	if err != nil {
		return OfflineSheet{}, err
	}
	o, err := s.getOffline(ctx, classroom, id)
	if err != nil {
		return OfflineSheet{}, err
	}
	sh := OfflineSheet{Offline: o, Students: []OfflineStudent{}, Groups: []OfflineGroup{}}
	if err := s.pool.QueryRow(ctx, `SELECT c.name, t.name, m.name FROM content.topics t JOIN content.modules m ON m.id = t.module_id
		JOIN content.classrooms c ON c.id = m.classroom_id WHERE t.id = $1`, o.TopicID).Scan(&sh.Classroom, &sh.Topic, &sh.Module); err != nil {
		return sh, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.user_id::text, coalesce(u.name, ''), e.student_number,
			coalesce((SELECT array_agg(coalesce(c.public_name, c.name) ORDER BY c.position, lower(c.name)) FROM content.enrolment_categories ec
				JOIN content.categories c ON c.id = ec.category_id WHERE ec.enrolment_id = e.id), '{}'),
			m.score::float8, coalesce(m.comment, ''), gm.id, coalesce(gm.name, ''), gm.score, coalesce(gm.comment, '')
		FROM content.enrolments e LEFT JOIN auth.users u ON u.id = e.user_id
		LEFT JOIN grading.offline_marks m ON m.assignment_id = $2 AND m.user_id = e.user_id
		LEFT JOIN LATERAL (SELECT c.id::text AS id, coalesce(c.public_name, c.name) AS name, g.score::float8 AS score, g.comment
			FROM content.enrolment_categories ec JOIN content.categories c ON c.id = ec.category_id
			JOIN grading.offline_group_marks g ON g.category_id = c.id AND g.assignment_id = $2
			WHERE ec.enrolment_id = e.id AND $3 ORDER BY c.position, lower(c.name) LIMIT 1) gm ON true
		WHERE e.classroom_id = $1 AND e.status = 'active' ORDER BY lower(u.name), e.created_at`, classroom, id, o.Mode == ModeGroup)
	if err != nil {
		return sh, err
	}
	if sh.Students, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (OfflineStudent, error) {
		var st OfflineStudent
		var groupComment string
		if err := r.Scan(&st.UserID, &st.Name, &st.StudentNumber, &st.Groups, &st.Own, &st.OwnComment, &st.GroupID, &st.GroupName, &st.GroupScore, &groupComment); err != nil {
			return st, err
		}
		st.Score, st.Comment = st.GroupScore, groupComment
		if st.Own != nil {
			st.Score, st.Comment = st.Own, st.OwnComment
		}
		return st, nil
	}); err != nil {
		return sh, err
	}
	rows, err = s.pool.Query(ctx, `SELECT c.id::text, coalesce(c.public_name, c.name), c.color,
			coalesce((SELECT array_agg(coalesce(u.name, '') ORDER BY lower(u.name)) FROM content.enrolment_categories ec
				JOIN content.enrolments e ON e.id = ec.enrolment_id AND e.status = 'active' LEFT JOIN auth.users u ON u.id = e.user_id
				WHERE ec.category_id = c.id), '{}'),
			g.score::float8, coalesce(g.comment, '')
		FROM content.categories c LEFT JOIN grading.offline_group_marks g ON g.category_id = c.id AND g.assignment_id = $2
		WHERE c.classroom_id = $1 ORDER BY c.position, lower(c.name)`, classroom, id)
	if err != nil {
		return sh, err
	}
	sh.Groups, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (OfflineGroup, error) {
		var g OfflineGroup
		return g, r.Scan(&g.ID, &g.Name, &g.Color, &g.Members, &g.Score, &g.Comment)
	})
	return sh, err
}

// MarkInput sets a student's own mark; no score takes it away.
type MarkInput struct {
	UserID  string   `json:"user_id"`
	Score   *float64 `json:"score"`
	Comment string   `json:"comment"`
}

// GroupMarkInput sets a group's mark; no score takes it away.
type GroupMarkInput struct {
	GroupID string   `json:"group_id"`
	Score   *float64 `json:"score"`
	Comment string   `json:"comment"`
}

// MarksInput changes the marks listed and leaves the rest.
type MarksInput struct {
	Students []MarkInput      `json:"students"`
	Groups   []GroupMarkInput `json:"groups"`
}

func scoreProblem(score *float64, max float64) string {
	if score == nil {
		return ""
	}
	if *score < 0 || *score > max || math.IsNaN(*score) {
		return "a mark from 0 to the most it is out of"
	}
	if *score != round(*score, 2) {
		return "a mark to at most 2 decimal places"
	}
	return ""
}

// SetMarks saves marks. Any teacher of the classroom may.
func (s *Service) SetMarks(ctx context.Context, teacherID, id string, in MarksInput) (OfflineSheet, error) {
	classroom, err := s.offlineClassroom(ctx, teacherID, id)
	if err != nil {
		return OfflineSheet{}, err
	}
	o, err := s.getOffline(ctx, classroom, id)
	if err != nil {
		return OfflineSheet{}, err
	}
	if len(in.Students) > 5000 || len(in.Groups) > 500 {
		return OfflineSheet{}, httpx.BadRequest("too many marks at once")
	}
	var students, groups []string
	if err := s.pool.QueryRow(ctx, `SELECT
			coalesce((SELECT array_agg(user_id::text) FROM content.enrolments WHERE classroom_id = $1 AND status = 'active'), '{}'),
			coalesce((SELECT array_agg(id::text) FROM content.categories WHERE classroom_id = $1), '{}')`, classroom).Scan(&students, &groups); err != nil {
		return OfflineSheet{}, err
	}
	isStudent, isGroup := set(students), set(groups)
	bad := map[string]string{}
	seen := map[string]bool{}
	for i := range in.Students {
		m := &in.Students[i]
		m.Comment = strings.TrimSpace(m.Comment)
		switch {
		case !isStudent[m.UserID]:
			bad["students"] = "one of these students isn't in this classroom"
		case seen[m.UserID]:
			bad["students"] = "a student is listed twice"
		case scoreProblem(m.Score, o.MaxMarks) != "":
			bad["students"] = scoreProblem(m.Score, o.MaxMarks)
		case utf8.RuneCountInString(m.Comment) > 1000:
			bad["students"] = "a comment of at most 1000 characters"
		}
		seen[m.UserID] = true
	}
	if len(in.Groups) > 0 && o.Mode != ModeGroup {
		bad["groups"] = "this assignment is marked student by student"
	}
	for i := range in.Groups {
		g := &in.Groups[i]
		g.Comment = strings.TrimSpace(g.Comment)
		switch {
		case !isGroup[g.GroupID]:
			bad["groups"] = "one of these groups isn't in this classroom"
		case seen[g.GroupID]:
			bad["groups"] = "a group is listed twice"
		case scoreProblem(g.Score, o.MaxMarks) != "":
			bad["groups"] = scoreProblem(g.Score, o.MaxMarks)
		case utf8.RuneCountInString(g.Comment) > 1000:
			bad["groups"] = "a comment of at most 1000 characters"
		}
		seen[g.GroupID] = true
	}
	if len(bad) > 0 {
		return OfflineSheet{}, httpx.Invalid(bad)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, m := range in.Students {
			var err error
			if m.Score == nil {
				_, err = tx.Exec(ctx, `DELETE FROM grading.offline_marks WHERE assignment_id = $1 AND user_id = $2`, id, m.UserID)
			} else {
				_, err = tx.Exec(ctx, `INSERT INTO grading.offline_marks (assignment_id, user_id, score, comment, updated_by) VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT (assignment_id, user_id) DO UPDATE SET score = $3, comment = $4, updated_by = $5, updated_at = now()`, id, m.UserID, *m.Score, m.Comment, teacherID)
			}
			if err != nil {
				return err
			}
		}
		for _, g := range in.Groups {
			var err error
			if g.Score == nil {
				_, err = tx.Exec(ctx, `DELETE FROM grading.offline_group_marks WHERE assignment_id = $1 AND category_id = $2`, id, g.GroupID)
			} else {
				_, err = tx.Exec(ctx, `INSERT INTO grading.offline_group_marks (assignment_id, category_id, score, comment, updated_by) VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT (assignment_id, category_id) DO UPDATE SET score = $3, comment = $4, updated_by = $5, updated_at = now()`, id, g.GroupID, *g.Score, g.Comment, teacherID)
			}
			if err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, teacherID, "offline_marked", "offline_assignment", id, map[string]int{"students": len(in.Students), "groups": len(in.Groups)})
	})
	if err != nil {
		return OfflineSheet{}, err
	}
	return s.Sheet(ctx, teacherID, id)
}

func set(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, v := range list {
		m[v] = true
	}
	return m
}

// MyOffline is an offline assignment as a student of its classroom sees it.
type MyOffline struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Instructions string     `json:"instructions"`
	Classroom    string     `json:"classroom"`
	ClassroomID  string     `json:"classroom_id"`
	Topic        string     `json:"topic"`
	MaxMarks     float64    `json:"max_marks"`
	Mode         string     `json:"mode"`
	DueAt        *time.Time `json:"due_at"`
	Released     bool       `json:"released"`
	Group        string     `json:"group,omitempty"` // whose mark they got
	Score        *float64   `json:"score,omitempty"`
	Pct          *float64   `json:"pct,omitempty"`
	Comment      string     `json:"comment,omitempty"`
}

// MyOfflineView is what a student sees: their mark only once released.
func (s *Service) MyOfflineView(ctx context.Context, userID, id string) (MyOffline, error) {
	var v MyOffline
	var released *time.Time
	err := s.pool.QueryRow(ctx, `SELECT a.id::text, a.title, a.instructions, c.name, c.id::text, t.name, a.max_marks::float8, a.mode, a.due_at, a.released_at
		FROM grading.offline_assignments a JOIN content.classrooms c ON c.id = a.classroom_id JOIN content.topics t ON t.id = a.topic_id
		JOIN content.enrolments e ON e.classroom_id = a.classroom_id AND e.user_id = $2 AND e.status = 'active'
		WHERE a.id = $1`, id, userID).Scan(&v.ID, &v.Title, &v.Instructions, &v.Classroom, &v.ClassroomID, &v.Topic, &v.MaxMarks, &v.Mode, &v.DueAt, &released)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, httpx.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.Released = released != nil
	if !v.Released {
		return v, nil
	}
	var own, group *float64
	var ownComment, groupComment, groupName string
	err = s.pool.QueryRow(ctx, `SELECT
			(SELECT score::float8 FROM grading.offline_marks WHERE assignment_id = $1 AND user_id = $2),
			coalesce((SELECT comment FROM grading.offline_marks WHERE assignment_id = $1 AND user_id = $2), ''),
			gm.score, coalesce(gm.comment, ''), coalesce(gm.name, '')
		FROM (SELECT 1) one LEFT JOIN LATERAL (SELECT g.score::float8 AS score, g.comment, coalesce(c.public_name, c.name) AS name
			FROM content.enrolments e JOIN content.enrolment_categories ec ON ec.enrolment_id = e.id JOIN content.categories c ON c.id = ec.category_id
			JOIN grading.offline_group_marks g ON g.category_id = c.id AND g.assignment_id = $1
			WHERE e.classroom_id = $3 AND e.user_id = $2 AND $4 ORDER BY c.position, lower(c.name) LIMIT 1) gm ON true`,
		id, userID, v.ClassroomID, v.Mode == ModeGroup).Scan(&own, &ownComment, &group, &groupComment, &groupName)
	if err != nil {
		return v, err
	}
	switch {
	case own != nil:
		v.Score, v.Comment = own, ownComment
	case group != nil:
		v.Score, v.Comment, v.Group = group, groupComment, groupName
	}
	if v.Score != nil {
		p := round(*v.Score/v.MaxMarks*100, 1)
		v.Pct = &p
	}
	return v, nil
}
