// Package archive is one place for a teacher's finished activities: quiz
// sessions, polls, meetings and live presentations (D-149). Each can be put in
// the archive, which only takes it out of the everyday lists, brought back, or
// deleted from there for good. Tutoring sessions, in their own service, follow
// the same rules there. Like the dashboard it reads other modules' tables.
package archive

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// The kinds of activity here.
const (
	KindQuiz         = "quiz"
	KindPoll         = "poll"
	KindMeeting      = "meeting"
	KindPresentation = "presentation"
)

// The lists a teacher sees.
const (
	ViewPast     = "past"     // finished, not archived
	ViewArchived = "archived" // in the archive
)

// target says how to find one kind of activity and who may change it.
type target struct {
	// Selects whether it has finished and whether it's archived, for id $1 and teacher $2.
	state string
	table string
}

var targets = map[string]target{
	KindQuiz: {
		state: `SELECT status = 'ended', archived_at IS NOT NULL FROM live.sessions WHERE id = $1 AND content.teaches(classroom_id, $2)`,
		table: "live.sessions",
	},
	KindPoll: {
		state: `SELECT status = 'closed', archived_at IS NOT NULL FROM poll.polls WHERE id = $1 AND teacher_id = $2`,
		table: "poll.polls",
	},
	KindMeeting: {
		state: `SELECT status = 'ended', archived_at IS NOT NULL FROM meet.meetings WHERE id = $1 AND host_id = $2`,
		table: "meet.meetings",
	},
	KindPresentation: {
		state: `SELECT l.ended_at IS NOT NULL, l.archived_at IS NOT NULL FROM presentation.lives l
			JOIN presentation.presentations p ON p.id = l.presentation_id
			WHERE l.id = $1 AND content.teaches_topic(p.topic_id, $2)`,
		table: "presentation.lives",
	},
}

var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Item is one finished activity.
type Item struct {
	Kind        string     `json:"kind"`
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Detail      string     `json:"detail"` // the quiz a session ran
	ClassroomID *string    `json:"classroom_id"`
	Classroom   string     `json:"classroom"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     time.Time  `json:"ended_at"`
	ArchivedAt  *time.Time `json:"archived_at"`
	People      *int       `json:"people"` // who took part, when that's known
	Href        string     `json:"href"`   // its page, if it has one
}

// Filter narrows the list.
type Filter struct {
	View, Kind, ClassroomID string
	From, To                *time.Time // the days it started between, both included
}

// Service lists and changes finished activities.
type Service struct {
	pool *pgxpool.Pool
	// Told when one is deleted, to drop what's held in memory for it.
	forget map[string]func(id string)
}

// NewService makes the service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, forget: map[string]func(string){}}
}

// OnDelete has the function called when an activity of that kind is deleted.
func (s *Service) OnDelete(kind string, f func(id string)) { s.forget[kind] = f }

const items = `
	SELECT 'quiz' AS kind, se.id::text AS id, coalesce(nullif(se.title, ''), q.title, 'Quiz session') AS title, coalesce(q.title, '') AS detail,
		se.classroom_id::text AS classroom_id, c.name AS classroom, coalesce(se.started_at, se.created_at) AS started_at,
		coalesce(se.ended_at, se.created_at) AS ended_at, se.archived_at,
		(SELECT count(*) FROM live.attempts a WHERE a.session_id = se.id)::int AS people, '/t/sessions/' || se.id AS href
	FROM live.sessions se JOIN content.classrooms c ON c.id = se.classroom_id LEFT JOIN quiz.quizzes q ON q.id = se.quiz_id
	WHERE se.status = 'ended' AND content.teaches(se.classroom_id, $1)
	UNION ALL
	SELECT 'poll', p.id::text, p.title, '', p.classroom_id::text, coalesce(c.name, ''), coalesce(p.opened_at, p.created_at),
		coalesce(p.closed_at, p.created_at), p.archived_at,
		(SELECT count(*) FROM poll.participants x WHERE x.poll_id = p.id)::int, '/t/polls/' || p.id
	FROM poll.polls p LEFT JOIN content.classrooms c ON c.id = p.classroom_id
	WHERE p.teacher_id = $1 AND p.status = 'closed'
	UNION ALL
	SELECT 'meeting', m.id::text, m.title, '', m.classroom_id::text, coalesce(c.name, ''), m.created_at,
		coalesce(m.ended_at, m.created_at), m.archived_at,
		(SELECT count(*) FROM meet.attendance x WHERE x.meeting_id = m.id)::int, ''
	FROM meet.meetings m LEFT JOIN content.classrooms c ON c.id = m.classroom_id
	WHERE m.host_id = $1 AND m.status = 'ended'
	UNION ALL
	SELECT 'presentation', l.id::text, p.title, '', c.id::text, c.name, l.started_at, l.ended_at, l.archived_at,
		NULL::int, '/t/presentations/' || p.id
	FROM presentation.lives l JOIN presentation.presentations p ON p.id = l.presentation_id
	JOIN content.topics tp ON tp.id = p.topic_id JOIN content.modules md ON md.id = tp.module_id JOIN content.classrooms c ON c.id = md.classroom_id
	WHERE l.ended_at IS NOT NULL AND content.teaches_topic(p.topic_id, $1)`

// Sorts are the orders of the list.
var Sorts = page.Sorts{"ended": "ended_at", "started": "started_at", "title": "lower(title)", "kind": "kind", "classroom": "lower(classroom)", "archived": "archived_at"}

// List returns one page of a teacher's finished activities.
func (s *Service) List(ctx context.Context, teacherID string, f Filter, p page.Request) ([]Item, int, error) {
	where := ` FROM (` + items + `) x WHERE ($2::text = 'archived') = (x.archived_at IS NOT NULL)
		AND ($3::text = '' OR x.kind = $3) AND ($4::text = '' OR x.classroom_id = $4)
		AND ($5::text = '' OR x.title ILIKE $5 OR x.detail ILIKE $5 OR x.classroom ILIKE $5)
		AND ($6::timestamptz IS NULL OR x.started_at >= $6) AND ($7::timestamptz IS NULL OR x.started_at < $7)`
	var to *time.Time
	if f.To != nil {
		next := f.To.AddDate(0, 0, 1)
		to = &next
	}
	args := []any{teacherID, f.View, f.Kind, f.ClassroomID, p.Like(), f.From, to}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT x.kind, x.id, x.title, x.detail, x.classroom_id, x.classroom, x.started_at, x.ended_at, x.archived_at, x.people, x.href`+where+p.OrderBy(Sorts, "id")+p.Limit(), args...)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Item, error) {
		var it Item
		return it, r.Scan(&it.Kind, &it.ID, &it.Title, &it.Detail, &it.ClassroomID, &it.Classroom, &it.StartedAt, &it.EndedAt, &it.ArchivedAt, &it.People, &it.Href)
	})
	return list, total, err
}

// state finds an activity the teacher may change, inside tx.
func state(ctx context.Context, tx pgx.Tx, teacherID, kind, id string) (t target, finished, archived bool, err error) {
	t, ok := targets[kind]
	if !ok || !idPattern.MatchString(id) {
		return t, false, false, httpx.ErrNotFound
	}
	err = tx.QueryRow(ctx, t.state+` FOR UPDATE`, id, teacherID).Scan(&finished, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		err = httpx.ErrNotFound
	}
	return t, finished, archived, err
}

// Archive takes a finished activity out of the everyday lists. Its marks and
// attendance stay where they are.
func (s *Service) Archive(ctx context.Context, teacherID, kind, id string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		t, finished, archived, err := state(ctx, tx, teacherID, kind, id)
		if err != nil {
			return err
		}
		if !finished {
			return httpx.NewError(http.StatusConflict, "still_running", "it's still running: end it first")
		}
		if archived {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE `+t.table+` SET archived_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "activity_archived", kind, id, nil)
	})
}

// Restore brings one back from the archive.
func (s *Service) Restore(ctx context.Context, teacherID, kind, id string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		t, _, archived, err := state(ctx, tx, teacherID, kind, id)
		if err != nil || !archived {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE `+t.table+` SET archived_at = NULL WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "activity_restored", kind, id, nil)
	})
}

// Delete removes an archived activity for good, with everything kept with it:
// a quiz session's marks, a poll's answers, a meeting's attendance.
func (s *Service) Delete(ctx context.Context, teacherID, kind, id string) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		t, _, archived, err := state(ctx, tx, teacherID, kind, id)
		if err != nil {
			return err
		}
		if !archived {
			return httpx.NewError(http.StatusConflict, "not_archived", "archive it first: only what's in the archive can be deleted")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM `+t.table+` WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "activity_deleted", kind, id, nil)
	})
	if err == nil {
		if f := s.forget[kind]; f != nil {
			f(id)
		}
	}
	return err
}

func day(v string) (*time.Time, bool) {
	if v == "" {
		return nil, true
	}
	d, err := time.Parse("2006-01-02", v)
	return &d, err == nil
}

// TeacherRoutes mounts the archive under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	uid := func(r *http.Request) string { return auth.MustUser(r.Context()).ID }
	act := func(f func(context.Context, string, string, string) error) http.Handler {
		return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			if err := f(r.Context(), uid(r), chi.URLParam(r, "kind"), chi.URLParam(r, "id")); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		})
	}
	r.Method("GET", "/archive", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		q := r.URL.Query()
		f := Filter{View: q.Get("view"), Kind: q.Get("kind"), ClassroomID: q.Get("classroom_id")}
		if f.View == "" {
			f.View = ViewPast
		}
		bad := map[string]string{}
		var ok bool
		if f.From, ok = day(q.Get("from")); !ok {
			bad["from"] = "choose a date"
		}
		if f.To, ok = day(q.Get("to")); !ok {
			bad["to"] = "choose a date"
		}
		if len(bad) > 0 {
			return httpx.Invalid(bad)
		}
		p := page.Parse(r, Sorts, "ended", true)
		list, total, err := s.List(r.Context(), uid(r), f, p)
		if err != nil {
			return err
		}
		page.Write(w, "items", list, total, p)
		return nil
	}))
	r.Method("POST", "/archive/{kind}/{id}", act(s.Archive))
	r.Method("POST", "/archive/{kind}/{id}/restore", act(s.Restore))
	r.Method("DELETE", "/archive/{kind}/{id}", act(s.Delete))
}
