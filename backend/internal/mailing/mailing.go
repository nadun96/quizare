// Package mailing is email to groups of accounts (D-102), for admins and
// managers given "Email": templates with placeholders such as {{first_name}},
// filled in for each recipient, sent to everyone, a role, one classroom's
// students or a list of addresses, and a record of every send. Each email
// goes out as its own job on the email queue. It owns the "mailing" schema.
package mailing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// Placeholders are what a subject or body may contain, each filled in for
// the recipient. {{classroom}} only when sending to a classroom's students.
var Placeholders = []string{"name", "first_name", "email", "role", "classroom", "site_url"}

var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z_]+)\s*\}\}`)

// Audience kinds.
const (
	ToEveryone  = "everyone"
	ToStudents  = "students"
	ToTeachers  = "teachers"
	ToStaff     = "staff" // admins and managers
	ToClassroom = "classroom"
	ToEmails    = "emails"
)

const (
	maxSubject = 200
	maxBody    = 20000
	maxEmails  = 500
)

// Audience is who an email goes to.
type Audience struct {
	Kind        string   `json:"kind"`
	ClassroomID string   `json:"classroom_id,omitempty"`
	Emails      []string `json:"emails,omitempty"`
}

// Template is a saved email.
type Template struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	CreatedBy *string   `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Send is one email sent to a group.
type Send struct {
	ID            string    `json:"id"`
	TemplateID    *string   `json:"template_id"`
	Subject       string    `json:"subject"`
	Body          string    `json:"body"`
	AudienceLabel string    `json:"audience_label"`
	Recipients    int       `json:"recipients"`
	SentBy        string    `json:"sent_by"` // their name
	SentAt        time.Time `json:"sent_at"`
}

// Recipient is one account an email goes to.
type Recipient struct {
	Name, Email, Role, Classroom string
}

// Preview is what sending would do: how many it reaches, the listed
// addresses that aren't active accounts, and the email as the first
// recipient gets it.
type Preview struct {
	Recipients int           `json:"recipients"`
	Label      string        `json:"label"`
	Unknown    []string      `json:"unknown"`
	Sample     *mail.Message `json:"sample"`
}

type Service struct {
	pool    *pgxpool.Pool
	jobs    jobs.Inserter
	baseURL string
}

func NewService(pool *pgxpool.Pool, inserter jobs.Inserter, baseURL string) *Service {
	return &Service{pool: pool, jobs: inserter, baseURL: baseURL}
}

// check refuses an empty or oversized subject or body and placeholders that
// don't exist, naming them, so a typo never reaches anyone as "{{frist_name}}".
func check(subject, body string) error {
	bad := map[string]string{}
	subject, body = strings.TrimSpace(subject), strings.TrimSpace(body)
	switch {
	case subject == "":
		bad["subject"] = "required"
	case len(subject) > maxSubject:
		bad["subject"] = fmt.Sprintf("at most %d characters", maxSubject)
	case strings.ContainsAny(subject, "\r\n"):
		bad["subject"] = "one line only"
	}
	switch {
	case body == "":
		bad["body"] = "required"
	case len(body) > maxBody:
		bad["body"] = fmt.Sprintf("at most %d characters", maxBody)
	}
	for field, text := range map[string]string{"subject": subject, "body": body} {
		if _, done := bad[field]; done {
			continue
		}
		if u := unknownPlaceholders(text); len(u) > 0 {
			bad[field] = "unknown placeholder {{" + strings.Join(u, "}}, {{") + "}}: use " + "{{" + strings.Join(Placeholders, "}}, {{") + "}}"
		}
	}
	if len(bad) > 0 {
		return httpx.Invalid(bad)
	}
	return nil
}

func unknownPlaceholders(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(text, -1) {
		name := strings.ToLower(m[1])
		known := false
		for _, p := range Placeholders {
			known = known || p == name
		}
		if !known && !seen[name] {
			seen[name] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Render fills the placeholders in for one recipient.
func Render(text string, r Recipient, siteURL string) string {
	return placeholderRe.ReplaceAllStringFunc(text, func(m string) string {
		switch strings.ToLower(placeholderRe.FindStringSubmatch(m)[1]) {
		case "name":
			return r.Name
		case "first_name":
			return firstName(r.Name)
		case "email":
			return r.Email
		case "role":
			return r.Role
		case "classroom":
			return r.Classroom
		case "site_url":
			return siteURL
		}
		return m
	})
}

// firstName skips a title (Ms, Mr, Dr…) the way the dashboard greets people.
func firstName(name string) string {
	parts := strings.Fields(name)
	for i, p := range parts {
		switch strings.ToLower(strings.TrimSuffix(p, ".")) {
		case "mr", "mrs", "ms", "miss", "mx", "dr", "prof", "rev", "sir":
			if i < len(parts)-1 {
				continue
			}
		}
		return p
	}
	return name
}

func (s *Service) message(subject, body string, r Recipient) mail.Message {
	return mail.Message{
		To:      r.Email,
		Subject: Render(strings.TrimSpace(subject), r, s.baseURL),
		Body:    Render(strings.TrimSpace(body), r, s.baseURL) + "\n\n--\nYou get this email because you have an account at " + s.baseURL + ".",
	}
}

// Recipients resolves an audience to active accounts, with the listed
// addresses that aren't any, and a label for the record.
func (s *Service) Recipients(ctx context.Context, a Audience) ([]Recipient, []string, string, error) {
	const cols = `SELECT u.name, u.email, u.role, '' FROM auth.users u WHERE u.status = 'active'`
	var (
		rows  pgx.Rows
		err   error
		label string
	)
	switch a.Kind {
	case ToEveryone:
		label = "Everyone"
		rows, err = s.pool.Query(ctx, cols+` ORDER BY u.email`)
	case ToStudents, ToTeachers:
		label = map[string]string{ToStudents: "All students", ToTeachers: "All teachers"}[a.Kind]
		rows, err = s.pool.Query(ctx, cols+` AND u.role = $1 ORDER BY u.email`, strings.TrimSuffix(a.Kind, "s"))
	case ToStaff:
		label = "Admins and managers"
		rows, err = s.pool.Query(ctx, cols+` AND (u.role IN ('admin', 'manager') OR EXISTS (SELECT 1 FROM auth.managers m WHERE m.user_id = u.id)) ORDER BY u.email`)
	case ToClassroom:
		var name string
		if err := s.pool.QueryRow(ctx, `SELECT name FROM content.classrooms WHERE id::text = $1`, a.ClassroomID).Scan(&name); err != nil {
			if errors.Is(err, pgx.ErrNoRows) || a.ClassroomID == "" {
				return nil, nil, "", httpx.Invalid(map[string]string{"classroom_id": "no such classroom"})
			}
			return nil, nil, "", err
		}
		label = "Students of " + name
		rows, err = s.pool.Query(ctx, `SELECT u.name, u.email, u.role, c.name FROM content.enrolments e
			JOIN auth.users u ON u.id = e.user_id AND u.status = 'active'
			JOIN content.classrooms c ON c.id = e.classroom_id
			WHERE e.classroom_id = $1::uuid AND e.status = 'active' ORDER BY u.email`, a.ClassroomID)
	case ToEmails:
		want := map[string]bool{}
		var list []string
		for _, e := range a.Emails {
			for _, one := range strings.FieldsFunc(e, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
				one = strings.ToLower(strings.Trim(one, "<>"))
				if one != "" && !want[one] {
					want[one] = true
					list = append(list, one)
				}
			}
		}
		if len(list) == 0 {
			return nil, nil, "", httpx.Invalid(map[string]string{"emails": "list at least one address"})
		}
		if len(list) > maxEmails {
			return nil, nil, "", httpx.Invalid(map[string]string{"emails": fmt.Sprintf("at most %d addresses; send to a role or a classroom instead", maxEmails)})
		}
		label = fmt.Sprintf("%d listed %s", len(list), map[bool]string{true: "address", false: "addresses"}[len(list) == 1])
		rows, err = s.pool.Query(ctx, cols+` AND u.email = ANY($1) ORDER BY u.email`, list)
		if err != nil {
			return nil, nil, "", err
		}
		out, err := collect(rows)
		if err != nil {
			return nil, nil, "", err
		}
		found := map[string]bool{}
		for _, r := range out {
			found[r.Email] = true
		}
		unknown := []string{}
		for _, e := range list {
			if !found[e] {
				unknown = append(unknown, e)
			}
		}
		return out, unknown, label, nil
	default:
		return nil, nil, "", httpx.Invalid(map[string]string{"audience": "everyone, students, teachers, staff, classroom or emails"})
	}
	if err != nil {
		return nil, nil, "", err
	}
	out, err := collect(rows)
	return out, []string{}, label, err
}

func collect(rows pgx.Rows) ([]Recipient, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Recipient, error) {
		var x Recipient
		return x, r.Scan(&x.Name, &x.Email, &x.Role, &x.Classroom)
	})
}

// PreviewSend says who an email would reach and shows it as the first gets it.
func (s *Service) PreviewSend(ctx context.Context, subject, body string, a Audience) (Preview, error) {
	if err := check(subject, body); err != nil {
		return Preview{}, err
	}
	rs, unknown, label, err := s.Recipients(ctx, a)
	if err != nil {
		return Preview{}, err
	}
	p := Preview{Recipients: len(rs), Label: label, Unknown: unknown}
	if len(rs) > 0 {
		m := s.message(subject, body, rs[0])
		p.Sample = &m
	}
	return p, nil
}

// SendTest sends the email only to the person writing it, filled in as if
// they were a recipient (with the classroom's name, for a classroom).
func (s *Service) SendTest(ctx context.Context, actorID, subject, body string, a Audience) error {
	if err := check(subject, body); err != nil {
		return err
	}
	var me Recipient
	if err := s.pool.QueryRow(ctx, `SELECT name, email, role FROM auth.users WHERE id = $1`, actorID).Scan(&me.Name, &me.Email, &me.Role); err != nil {
		return err
	}
	if a.Kind == ToClassroom {
		_ = s.pool.QueryRow(ctx, `SELECT name FROM content.classrooms WHERE id::text = $1`, a.ClassroomID).Scan(&me.Classroom)
	}
	m := s.message(subject, body, me)
	m.Subject = "[Test] " + m.Subject
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := s.jobs.InsertTx(ctx, tx, mail.Args{Message: m}, nil)
		return err
	})
}

// SendAll queues the email for each recipient and records the send.
func (s *Service) SendAll(ctx context.Context, actorID, subject, body string, a Audience, templateID string) (Send, error) {
	if err := check(subject, body); err != nil {
		return Send{}, err
	}
	rs, _, label, err := s.Recipients(ctx, a)
	if err != nil {
		return Send{}, err
	}
	if len(rs) == 0 {
		return Send{}, httpx.Invalid(map[string]string{"audience": "nobody to send to: no active account matches"})
	}
	aud, _ := json.Marshal(a)
	var out Send
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, r := range rs {
			if _, err := s.jobs.InsertTx(ctx, tx, mail.Args{Message: s.message(subject, body, r)}, nil); err != nil {
				return err
			}
		}
		err := tx.QueryRow(ctx, `INSERT INTO mailing.sends (template_id, subject, body, audience, audience_label, recipients, sent_by)
			VALUES ((SELECT id FROM mailing.templates WHERE id::text = $1), $2, $3, $4, $5, $6, $7)
			RETURNING id, template_id, subject, body, audience_label, recipients, sent_at`,
			templateID, strings.TrimSpace(subject), strings.TrimSpace(body), aud, label, len(rs), actorID).
			Scan(&out.ID, &out.TemplateID, &out.Subject, &out.Body, &out.AudienceLabel, &out.Recipients, &out.SentAt)
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "email_sent", "email", out.ID, map[string]any{"audience": label, "recipients": len(rs), "subject": out.Subject})
	})
	if err != nil {
		return Send{}, err
	}
	_ = s.pool.QueryRow(ctx, `SELECT coalesce(name, '') FROM auth.users WHERE id = $1`, actorID).Scan(&out.SentBy)
	return out, nil
}

// SendSorts are the history's columns.
var SendSorts = page.Sorts{"sent": "s.sent_at", "subject": "lower(s.subject)", "recipients": "s.recipients"}

// Sends lists what was sent, newest first.
func (s *Service) Sends(ctx context.Context, p page.Request) ([]Send, int, error) {
	where := ` FROM mailing.sends s LEFT JOIN auth.users u ON u.id = s.sent_by WHERE ($1 = '' OR s.subject ILIKE $1 OR s.audience_label ILIKE $1 OR u.name ILIKE $1)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, p.Like()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT s.id, s.template_id, s.subject, s.body, s.audience_label, s.recipients, coalesce(u.name, ''), s.sent_at`+where+p.OrderBy(SendSorts, "s.id")+p.Limit(), p.Like())
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Send, error) {
		var x Send
		return x, r.Scan(&x.ID, &x.TemplateID, &x.Subject, &x.Body, &x.AudienceLabel, &x.Recipients, &x.SentBy, &x.SentAt)
	})
	if out == nil {
		out = []Send{}
	}
	return out, total, err
}

// TemplateSorts are the template list's columns.
var TemplateSorts = page.Sorts{"name": "lower(name)", "updated": "updated_at"}

const templateCols = `SELECT id, name, subject, body, created_by, created_at, updated_at FROM mailing.templates`

func scanTemplate(r pgx.Row) (Template, error) {
	var t Template
	err := r.Scan(&t.ID, &t.Name, &t.Subject, &t.Body, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, httpx.ErrNotFound
	}
	return t, err
}

// Templates lists the saved templates.
func (s *Service) Templates(ctx context.Context, p page.Request) ([]Template, int, error) {
	where := ` WHERE ($1 = '' OR name ILIKE $1 OR subject ILIKE $1)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM mailing.templates`+where, p.Like()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, templateCols+where+p.OrderBy(TemplateSorts, "id")+p.Limit(), p.Like())
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Template, error) { return scanTemplate(r) })
	if out == nil {
		out = []Template{}
	}
	return out, total, err
}

func (s *Service) Template(ctx context.Context, id string) (Template, error) {
	return scanTemplate(s.pool.QueryRow(ctx, templateCols+` WHERE id::text = $1`, id))
}

func checkTemplate(name, subject, body string) error {
	err := check(subject, body)
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		var he *httpx.Error
		if !errors.As(err, &he) {
			he = httpx.Invalid(map[string]string{})
		}
		he.Fields["name"] = map[bool]string{true: "required", false: "at most 120 characters"}[name == ""]
		return he
	}
	return err
}

var errTaken = httpx.Invalid(map[string]string{"name": "a template with this name already exists"})

func taken(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.ConstraintName == "templates_name_key"
}

// SaveTemplate creates a template, or replaces one when id is given.
func (s *Service) SaveTemplate(ctx context.Context, actorID, id, name, subject, body string) (Template, error) {
	if err := checkTemplate(name, subject, body); err != nil {
		return Template{}, err
	}
	name, subject, body = strings.TrimSpace(name), strings.TrimSpace(subject), strings.TrimSpace(body)
	var row pgx.Row
	if id == "" {
		row = s.pool.QueryRow(ctx, `INSERT INTO mailing.templates (name, subject, body, created_by) VALUES ($1, $2, $3, $4)
			RETURNING id, name, subject, body, created_by, created_at, updated_at`, name, subject, body, actorID)
	} else {
		row = s.pool.QueryRow(ctx, `UPDATE mailing.templates SET name = $2, subject = $3, body = $4, updated_at = now() WHERE id::text = $1
			RETURNING id, name, subject, body, created_by, created_at, updated_at`, id, name, subject, body)
	}
	t, err := scanTemplate(row)
	if taken(err) {
		return t, errTaken
	}
	return t, err
}

func (s *Service) DeleteTemplate(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM mailing.templates WHERE id::text = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return err
}

// Classroom is one a classroom audience can pick.
type Classroom struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Teacher  string `json:"teacher"`
	Students int    `json:"students"`
}

// Classrooms finds classrooms to send to, by their name or their teacher's.
func (s *Service) Classrooms(ctx context.Context, q string) ([]Classroom, error) {
	p := page.Request{Q: strings.TrimSpace(q)}
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.name, coalesce(u.name, ''),
		(SELECT count(*) FROM content.enrolments e WHERE e.classroom_id = c.id AND e.status = 'active')::int
		FROM content.classrooms c LEFT JOIN auth.users u ON u.id = c.teacher_id
		WHERE c.archived_at IS NULL AND ($1 = '' OR c.name ILIKE $1 OR u.name ILIKE $1 OR u.email ILIKE $1)
		ORDER BY lower(c.name), c.id LIMIT 20`, p.Like())
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Classroom, error) {
		var c Classroom
		return c, r.Scan(&c.ID, &c.Name, &c.Teacher, &c.Students)
	})
	if out == nil {
		out = []Classroom{}
	}
	return out, err
}
