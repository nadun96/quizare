// Package content owns the teacher's structure: classrooms, modules, topics
// and student enrolments (FR-CLS-01…06, BR-02, BR-03, BR-14).
package content

import (
	"context"
	"crypto/rand"
	"errors"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
	"github.com/nadun96/quizplatform/internal/platform/props"
	"github.com/nadun96/quizplatform/internal/settings"
)

type Classroom struct {
	ID          string              `json:"id"`
	TeacherID   string              `json:"teacher_id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	JoinCode    string              `json:"join_code"`
	Settings    settings.Overrides  `json:"settings"`
	Effective   *settings.Effective `json:"effective,omitempty"`
	Archived    bool                `json:"archived"`
	CreatedAt   time.Time           `json:"created_at"`
	// Optional details for finding it (D-128), and its active students.
	Props    props.Props `json:"props"`
	Students int         `json:"students"`
	// For the asking teacher (D-121): owner, or coteacher; and the owner's name.
	Role      string `json:"role,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
}

type Module struct {
	ID          string             `json:"id"`
	ClassroomID string             `json:"classroom_id"`
	Name        string             `json:"name"`
	Position    int                `json:"position"`
	Settings    settings.Overrides `json:"settings"`
}

type Topic struct {
	ID       string             `json:"id"`
	ModuleID string             `json:"module_id"`
	Name     string             `json:"name"`
	Position int                `json:"position"`
	Settings settings.Overrides `json:"settings"`
}

type Enrolment struct {
	ID            string    `json:"id"`
	ClassroomID   string    `json:"classroom_id"`
	UserID        string    `json:"user_id"`
	StudentNumber *string   `json:"student_number"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	StudentName   string    `json:"student_name,omitempty"`
	StudentEmail  string    `json:"student_email,omitempty"`
	StudentAvatar string    `json:"student_avatar,omitempty"` // picture version (D-49)
	Categories    []string  `json:"categories,omitempty"`     // category ids (teacher view, V2-03)
}

// Users is the slice of the auth module that content needs.
type Users interface {
	UsersByID(ctx context.Context, ids []string) (map[string]auth.User, error)
}

type Service struct {
	pool     *pgxpool.Pool
	settings *settings.Store
	users    Users
	mailer   Mailer // co-teacher invitations (D-121)
	baseURL  string
}

func NewService(pool *pgxpool.Pool, st *settings.Store, users Users) *Service {
	return &Service{pool: pool, settings: st, users: users}
}

var (
	errArchived = httpx.NewError(http.StatusConflict, "classroom_archived", "this classroom is archived")
)

// ---------- validation helpers ----------

func cleanName(name string, fields map[string]string) string {
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 1 || n > 200 {
		fields["name"] = "name must be 1-200 characters"
	}
	return name
}

func validateSettings(o *settings.Overrides, level settings.Level, fields map[string]string) {
	if o == nil {
		return
	}
	var ve *settings.ValidationError
	if err := o.Validate(level); errors.As(err, &ve) {
		for k, v := range ve.Fields {
			fields["settings."+k] = v
		}
	}
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I

func newJoinCode() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

func isUnique(err error, constraint string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505" && (constraint == "" || pe.ConstraintName == constraint)
}

// ---------- classrooms ----------

type ClassroomInput struct {
	Name        *string             `json:"name"`
	Description *string             `json:"description"`
	Settings    *settings.Overrides `json:"settings"`
	Props       *props.Props        `json:"props"` // replaced whole when given (D-128)
}

func (s *Service) CreateClassroom(ctx context.Context, teacherID string, in ClassroomInput) (Classroom, error) {
	f := map[string]string{}
	c := Classroom{TeacherID: teacherID}
	if in.Name == nil {
		in.Name = new(string)
	}
	c.Name = cleanName(*in.Name, f)
	if in.Description != nil {
		c.Description = strings.TrimSpace(*in.Description)
	}
	validateSettings(in.Settings, settings.LevelClassroom, f)
	if in.Props != nil {
		maps.Copy(f, in.Props.Clean("props.", true))
		c.Props = *in.Props
	}
	if len(f) > 0 {
		return c, httpx.Invalid(f)
	}
	if in.Settings != nil {
		c.Settings = *in.Settings
	}
	if c.Props.Tags == nil {
		c.Props.Tags = []string{}
	}
	for range 5 {
		c.JoinCode = newJoinCode()
		err := s.pool.QueryRow(ctx, `INSERT INTO content.classrooms(teacher_id, name, description, join_code, settings,
				department, course_code, academic_year, semester, tags, starts_on, ends_on)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id, created_at`,
			append([]any{teacherID, c.Name, c.Description, c.JoinCode, c.Settings.JSON()}, c.Props.Values(true)...)...).
			Scan(&c.ID, &c.CreatedAt)
		if isUnique(err, "") {
			continue
		}
		return c, err
	}
	return c, errors.New("could not allocate a unique join code")
}

const classroomCols = `c.id, c.teacher_id, c.name, c.description, c.join_code, c.settings, c.archived_at IS NOT NULL, c.created_at,
	coalesce((SELECT u.name FROM auth.users u WHERE u.id = c.teacher_id), ''),
	(SELECT count(*)::int FROM content.enrolments e WHERE e.classroom_id = c.id AND e.status = 'active'), `

var propCols = props.Columns("c", true)

func scanClassroom(row pgx.Row) (Classroom, error) {
	var c Classroom
	var raw []byte
	err := row.Scan(append([]any{&c.ID, &c.TeacherID, &c.Name, &c.Description, &c.JoinCode, &raw, &c.Archived, &c.CreatedAt, &c.OwnerName, &c.Students}, c.Props.Dest(true)...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httpx.ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.Settings, err = settings.Parse(raw)
	return c, err
}

// ClassroomSorts are the orders of a teacher's classroom list (PL-FR-01).
var ClassroomSorts = page.Sorts{"created": "c.created_at", "name": "lower(c.name)", "starts": "c.starts_on", "year": "c.academic_year"}

// ListClassrooms returns one page of the classrooms a teacher owns or
// co-teaches (D-121), searched by name, course code, department or tag and
// narrowed by their details (D-128), and how many there are in all.
func (s *Service) ListClassrooms(ctx context.Context, teacherID string, includeArchived bool, f props.Filter, p page.Request) ([]Classroom, int, error) {
	args := []any{teacherID, includeArchived, p.Like()}
	where := ` FROM content.classrooms c WHERE content.teaches(c.id, $1) AND ($2 OR c.archived_at IS NULL)
		AND ($3 = '' OR c.name ILIKE $3 OR c.course_code ILIKE $3 OR c.department ILIKE $3 OR EXISTS (SELECT 1 FROM unnest(c.tags) t WHERE t ILIKE $3))` + f.SQL("c", &args)
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+classroomCols+propCols+where+p.OrderBy(ClassroomSorts, "c.id")+p.Limit(), args...)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Classroom, error) {
		c, err := scanClassroom(r)
		c.Role = roleOf(c, teacherID)
		return c, err
	})
	return list, total, err
}

// ClassroomFacets are the details in use among a teacher's classrooms, for the list's filters (D-128).
func (s *Service) ClassroomFacets(ctx context.Context, teacherID string, includeArchived bool) (props.Facets, error) {
	return props.LoadFacets(ctx, s.pool, `FROM content.classrooms c WHERE content.teaches(c.id, $1) AND ($2 OR c.archived_at IS NULL)`, "c", teacherID, includeArchived)
}

func roleOf(c Classroom, teacherID string) string {
	if c.TeacherID == teacherID {
		return "owner"
	}
	return "coteacher"
}

// errOwnerOnly: co-teachers teach the classroom; the classroom itself is its owner's (D-121).
var errOwnerOnly = httpx.NewError(http.StatusForbidden, "owner_only", "only the classroom's owner can do this")

// GetClassroom returns a classroom teacherID teaches (owns or co-teaches),
// with effective settings from its owner's defaults.
func (s *Service) GetClassroom(ctx context.Context, teacherID, id string) (Classroom, error) {
	c, err := scanClassroom(s.pool.QueryRow(ctx, `SELECT `+classroomCols+propCols+` FROM content.classrooms c WHERE c.id=$1 AND content.teaches(c.id, $2)`, id, teacherID))
	if err != nil {
		return c, err
	}
	c.Role = roleOf(c, teacherID)
	base, err := s.settings.Base(ctx, c.TeacherID)
	if err != nil {
		return c, err
	}
	eff := settings.Resolve(base, c.Settings)
	c.Effective = &eff
	return c, nil
}

func (s *Service) UpdateClassroom(ctx context.Context, teacherID, id string, in ClassroomInput) (Classroom, error) {
	c, err := s.GetClassroom(ctx, teacherID, id)
	if err != nil {
		return c, err
	}
	if c.Role != "owner" {
		return c, errOwnerOnly
	}
	f := map[string]string{}
	if in.Name != nil {
		c.Name = cleanName(*in.Name, f)
	}
	if in.Description != nil {
		c.Description = strings.TrimSpace(*in.Description)
	}
	validateSettings(in.Settings, settings.LevelClassroom, f)
	if in.Props != nil {
		maps.Copy(f, in.Props.Clean("props.", true))
		c.Props = *in.Props
	}
	if len(f) > 0 {
		return c, httpx.Invalid(f)
	}
	if in.Settings != nil {
		c.Settings = *in.Settings
	}
	if _, err := s.pool.Exec(ctx, `UPDATE content.classrooms SET name=$3, description=$4, settings=$5,
			department=$6, course_code=$7, academic_year=$8, semester=$9, tags=$10, starts_on=$11, ends_on=$12 WHERE id=$1 AND teacher_id=$2`,
		append([]any{id, teacherID, c.Name, c.Description, c.Settings.JSON()}, c.Props.Values(true)...)...); err != nil {
		return c, err
	}
	return s.GetClassroom(ctx, teacherID, id)
}

func (s *Service) SetArchived(ctx context.Context, teacherID, id string, archived bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE content.classrooms SET archived_at = CASE WHEN $3 THEN coalesce(archived_at, now()) END
		WHERE id=$1 AND teacher_id=$2`, id, teacherID, archived)
	if err == nil && tag.RowsAffected() == 0 {
		return s.ownerOr404(ctx, teacherID, id)
	}
	return err
}

// DeleteClassroom deletes a classroom with everything inside; one with
// results needs DeleteNode and its name (D-92).
func (s *Service) DeleteClassroom(ctx context.Context, teacherID, id string) error {
	return s.DeleteNode(ctx, teacherID, "classroom", id, "")
}

func (s *Service) RegenerateJoinCode(ctx context.Context, teacherID, id string) (string, error) {
	for range 5 {
		code := newJoinCode()
		tag, err := s.pool.Exec(ctx, `UPDATE content.classrooms SET join_code=$3 WHERE id=$1 AND teacher_id=$2`, id, teacherID, code)
		if isUnique(err, "") {
			continue
		}
		if err == nil && tag.RowsAffected() == 0 {
			return "", s.ownerOr404(ctx, teacherID, id)
		}
		return code, err
	}
	return "", errors.New("could not allocate a unique join code")
}

// ---------- modules & topics ----------

type NodeInput struct {
	Name     *string             `json:"name"`
	Position *int                `json:"position"`
	Settings *settings.Overrides `json:"settings"`
}

func (in NodeInput) validate(level settings.Level, creating bool) (string, error) {
	f := map[string]string{}
	name := ""
	if in.Name != nil || creating {
		if in.Name == nil {
			in.Name = new(string)
		}
		name = cleanName(*in.Name, f)
	}
	validateSettings(in.Settings, level, f)
	if len(f) > 0 {
		return "", httpx.Invalid(f)
	}
	return name, nil
}

func (s *Service) CreateModule(ctx context.Context, teacherID, classroomID string, in NodeInput) (Module, error) {
	name, err := in.validate(settings.LevelModule, true)
	if err != nil {
		return Module{}, err
	}
	m := Module{ClassroomID: classroomID, Name: name}
	if in.Settings != nil {
		m.Settings = *in.Settings
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO content.modules(classroom_id, name, position, settings)
		SELECT c.id, $3, coalesce($4, (SELECT count(*) FROM content.modules WHERE classroom_id=c.id)), $5
		FROM content.classrooms c WHERE c.id=$1 AND content.teaches(c.id, $2)
		RETURNING id, position`, classroomID, teacherID, name, in.Position, m.Settings.JSON()).Scan(&m.ID, &m.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, httpx.ErrNotFound
	}
	return m, err
}

// NodeSorts order modules and topics: by their position (the teacher's own
// order) or by name.
var NodeSorts = page.Sorts{"position": "position", "name": "lower(name)"}

// ListModules returns one page of a classroom's modules and how many there are.
func (s *Service) ListModules(ctx context.Context, teacherID, classroomID string, p page.Request) ([]Module, int, error) {
	where := ` FROM content.modules m JOIN content.classrooms c ON c.id=m.classroom_id
		WHERE m.classroom_id=$1 AND content.teaches(c.id, $2) AND ($3 = '' OR m.name ILIKE $3)`
	args := []any{classroomID, teacherID, p.Like()}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT m.id, m.classroom_id, m.name, m.position, m.settings`+where+
		p.OrderBy(page.Sorts{"position": "m.position", "name": "lower(m.name)"}, "m.created_at")+p.Limit(), args...)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Module, error) {
		var m Module
		var raw []byte
		if err := r.Scan(&m.ID, &m.ClassroomID, &m.Name, &m.Position, &raw); err != nil {
			return m, err
		}
		m.Settings, err = settings.Parse(raw)
		return m, err
	})
	return list, total, err
}

func (s *Service) UpdateModule(ctx context.Context, teacherID, id string, in NodeInput) error {
	name, err := in.validate(settings.LevelModule, false)
	if err != nil {
		return err
	}
	var raw []byte
	if in.Settings != nil {
		raw = in.Settings.JSON()
	}
	tag, err := s.pool.Exec(ctx, `UPDATE content.modules m SET
		name = CASE WHEN $3 <> '' THEN $3 ELSE m.name END,
		position = coalesce($4, m.position),
		settings = coalesce($5, m.settings)
		FROM content.classrooms c WHERE c.id=m.classroom_id AND m.id=$1 AND content.teaches(c.id, $2)`, id, teacherID, name, in.Position, raw)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return err
}

func (s *Service) DeleteModule(ctx context.Context, teacherID, id string) error {
	return s.DeleteNode(ctx, teacherID, "module", id, "")
}

func (s *Service) CreateTopic(ctx context.Context, teacherID, moduleID string, in NodeInput) (Topic, error) {
	name, err := in.validate(settings.LevelTopic, true)
	if err != nil {
		return Topic{}, err
	}
	t := Topic{ModuleID: moduleID, Name: name}
	if in.Settings != nil {
		t.Settings = *in.Settings
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO content.topics(module_id, name, position, settings)
		SELECT m.id, $3, coalesce($4, (SELECT count(*) FROM content.topics WHERE module_id=m.id)), $5
		FROM content.modules m JOIN content.classrooms c ON c.id=m.classroom_id
		WHERE m.id=$1 AND content.teaches(c.id, $2)
		RETURNING id, position`, moduleID, teacherID, name, in.Position, t.Settings.JSON()).Scan(&t.ID, &t.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, httpx.ErrNotFound
	}
	return t, err
}

// ListTopics returns one page of a module's topics and how many there are.
func (s *Service) ListTopics(ctx context.Context, teacherID, moduleID string, p page.Request) ([]Topic, int, error) {
	where := ` FROM content.topics t JOIN content.modules m ON m.id=t.module_id JOIN content.classrooms c ON c.id=m.classroom_id
		WHERE t.module_id=$1 AND content.teaches(c.id, $2) AND ($3 = '' OR t.name ILIKE $3)`
	args := []any{moduleID, teacherID, p.Like()}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id, t.module_id, t.name, t.position, t.settings`+where+
		p.OrderBy(page.Sorts{"position": "t.position", "name": "lower(t.name)"}, "t.created_at")+p.Limit(), args...)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Topic, error) {
		var t Topic
		var raw []byte
		if err := r.Scan(&t.ID, &t.ModuleID, &t.Name, &t.Position, &raw); err != nil {
			return t, err
		}
		t.Settings, err = settings.Parse(raw)
		return t, err
	})
	return list, total, err
}

// TopicDetail is a topic with its place in the hierarchy, for the topic page.
type TopicDetail struct {
	Topic
	ClassroomID   string `json:"classroom_id"`
	ClassroomName string `json:"classroom_name"`
	ModuleName    string `json:"module_name"`
}

func (s *Service) GetTopic(ctx context.Context, teacherID, id string) (TopicDetail, error) {
	var t TopicDetail
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT t.id, t.module_id, t.name, t.position, t.settings, c.id, c.name, m.name
		FROM content.topics t JOIN content.modules m ON m.id=t.module_id JOIN content.classrooms c ON c.id=m.classroom_id
		WHERE t.id=$1 AND content.teaches(c.id, $2)`, id, teacherID).Scan(&t.ID, &t.ModuleID, &t.Name, &t.Position, &raw, &t.ClassroomID, &t.ClassroomName, &t.ModuleName)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, httpx.ErrNotFound
	}
	if err != nil {
		return t, err
	}
	t.Settings, err = settings.Parse(raw)
	return t, err
}

func (s *Service) UpdateTopic(ctx context.Context, teacherID, id string, in NodeInput) error {
	name, err := in.validate(settings.LevelTopic, false)
	if err != nil {
		return err
	}
	var raw []byte
	if in.Settings != nil {
		raw = in.Settings.JSON()
	}
	tag, err := s.pool.Exec(ctx, `UPDATE content.topics t SET
		name = CASE WHEN $3 <> '' THEN $3 ELSE t.name END,
		position = coalesce($4, t.position),
		settings = coalesce($5, t.settings)
		FROM content.modules m JOIN content.classrooms c ON c.id=m.classroom_id
		WHERE m.id=t.module_id AND t.id=$1 AND content.teaches(c.id, $2)`, id, teacherID, name, in.Position, raw)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return err
}

func (s *Service) DeleteTopic(ctx context.Context, teacherID, id string) error {
	return s.DeleteNode(ctx, teacherID, "topic", id, "")
}

// TopicContext is what downstream modules (quiz, live sessions) need to know
// about a topic: who owns it and the configuration layers above it.
type TopicContext struct {
	TopicID, ModuleID, ClassroomID, TeacherID string
	ClassroomArchived                         bool
	Classroom, Module, Topic                  settings.Overrides
}

// Layers returns classroom → module → topic overrides in merge order.
func (tc TopicContext) Layers() []settings.Overrides {
	return []settings.Overrides{tc.Classroom, tc.Module, tc.Topic}
}

// TopicContext loads a topic and checks teacherID teaches its classroom ("" skips the check).
// TeacherID is the classroom's owner, whose defaults the settings start from.
func (s *Service) TopicContext(ctx context.Context, teacherID, topicID string) (TopicContext, error) {
	var tc TopicContext
	var cRaw, mRaw, tRaw []byte
	err := s.pool.QueryRow(ctx, `SELECT t.id, m.id, c.id, c.teacher_id, c.archived_at IS NOT NULL, c.settings, m.settings, t.settings
		FROM content.topics t JOIN content.modules m ON m.id=t.module_id JOIN content.classrooms c ON c.id=m.classroom_id
		WHERE t.id=$1 AND ($2 = '' OR content.teaches(c.id, nullif($2, '')::uuid))`, topicID, teacherID).
		Scan(&tc.TopicID, &tc.ModuleID, &tc.ClassroomID, &tc.TeacherID, &tc.ClassroomArchived, &cRaw, &mRaw, &tRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return tc, httpx.ErrNotFound
	}
	if err != nil {
		return tc, err
	}
	if tc.Classroom, err = settings.Parse(cRaw); err != nil {
		return tc, err
	}
	if tc.Module, err = settings.Parse(mRaw); err != nil {
		return tc, err
	}
	tc.Topic, err = settings.Parse(tRaw)
	return tc, err
}

// ownerOr404: a co-teacher is told it's the owner's to do; anyone else, that there's no such classroom.
func (s *Service) ownerOr404(ctx context.Context, teacherID, id string) error {
	if c, err := s.GetClassroom(ctx, teacherID, id); err == nil && c.Role != "owner" {
		return errOwnerOnly
	}
	return httpx.ErrNotFound
}
