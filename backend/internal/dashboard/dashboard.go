// Package dashboard is each person's own overview (D-99): what is live now,
// what's coming, what's overdue or was missed, and what's waiting on them,
// for students and teachers, in lists one page at a time. It only reads
// what the other modules record. Tutoring sessions come from the tutoring
// service, which the page asks itself.
package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// The lists.
const (
	ListLive     = "live"     // happening now
	ListUpcoming = "upcoming" // opening or due later
	ListOverdue  = "overdue"  // past due, or a quiz missed
	ListPending  = "pending"  // started and unfinished, or waiting on the teacher
)

var lists = []string{ListLive, ListUpcoming, ListOverdue, ListPending}

// Item is one thing on the dashboard.
type Item struct {
	Kind      string     `json:"kind"` // quiz, tutorial, presentation, meeting, poll, marking, enrolment
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Classroom string     `json:"classroom"`
	At        *time.Time `json:"at,omitempty"` // when it opens, is due, started or ended
	Status    string     `json:"status"`       // live, opens, due, due_soon, overdue, missed, in_progress, not_started, to_mark, to_approve
	Detail    string     `json:"detail,omitempty"`
	Href      string     `json:"href"`
}

// Summary counts each list.
type Summary struct {
	Role   string         `json:"role"`
	Counts map[string]int `json:"counts"`
}

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, now: time.Now} }

// SetClock is for tests.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// dueSoon is how close a due date counts as soon.
const dueSoon = 48 * time.Hour

// Lists builds every list for a person.
func (s *Service) Lists(ctx context.Context, u auth.User) (map[string][]Item, error) {
	out := map[string][]Item{}
	for _, l := range lists {
		out[l] = []Item{}
	}
	var err error
	switch u.Role {
	case auth.RoleStudent:
		err = s.student(ctx, u.ID, out)
	case auth.RoleTeacher:
		err = s.teacher(ctx, u.ID, out)
	}
	if err != nil {
		return nil, err
	}
	// Live and pending: newest first. Upcoming: soonest first. Overdue: most recent first.
	sortBy := func(l string, asc bool) {
		sort.SliceStable(out[l], func(i, j int) bool {
			a, b := out[l][i].At, out[l][j].At
			if a == nil || b == nil {
				return a != nil
			}
			if asc {
				return a.Before(*b)
			}
			return a.After(*b)
		})
	}
	sortBy(ListLive, false)
	sortBy(ListUpcoming, true)
	sortBy(ListOverdue, false)
	sortBy(ListPending, false)
	return out, nil
}

// classify places a tutorial or presentation by its dates and the person's progress.
func classify(now time.Time, opens, due *time.Time, started, finished bool) (list, status string, at *time.Time) {
	switch {
	case finished:
		return "", "", nil
	case opens != nil && opens.After(now):
		return ListUpcoming, "opens", opens
	case due != nil && due.Before(now):
		return ListOverdue, "overdue", due
	case due != nil && due.Sub(now) <= dueSoon:
		return ListUpcoming, "due_soon", due
	case due != nil:
		return ListUpcoming, "due", due
	case started:
		return ListPending, "in_progress", nil
	default:
		return ListPending, "not_started", nil
	}
}

func (s *Service) student(ctx context.Context, uid string, out map[string][]Item) error {
	now := s.now()
	// Quiz sessions open or live in their classrooms, unless they've finished.
	rows, err := s.pool.Query(ctx, `SELECT s.id, s.title, c.name, coalesce(s.started_at, s.created_at), s.join_code, a.id, coalesce(a.state, '')
		FROM live.sessions s
		JOIN content.classrooms c ON c.id = s.classroom_id AND c.archived_at IS NULL
		JOIN content.enrolments e ON e.classroom_id = c.id AND e.user_id = $1 AND e.status = 'active'
		LEFT JOIN live.attempts a ON a.session_id = s.id AND a.user_id = $1
		WHERE s.status <> 'ended' AND (a.id IS NULL OR a.state IN ('waiting', 'admitted', 'in_progress', 'paused'))
		LIMIT 200`, uid)
	if err != nil {
		return err
	}
	if err := each(rows, func(r pgx.Rows) error {
		var it Item
		var at time.Time
		var code string
		var attempt *string
		var state string
		if err := r.Scan(&it.ID, &it.Title, &it.Classroom, &at, &code, &attempt, &state); err != nil {
			return err
		}
		it.Kind, it.Status, it.At = "quiz", "live", &at
		if attempt != nil {
			it.Href, it.Detail = "/attempt/"+*attempt, "You've joined"
		} else {
			it.Href, it.Detail = "/j/"+code, "Join with code "+code
		}
		out[ListLive] = append(out[ListLive], it)
		return nil
	}); err != nil {
		return err
	}

	// Quizzes they missed: ended in the last 60 days, after they joined the
	// classroom, without an attempt of theirs that got going.
	rows, err = s.pool.Query(ctx, `SELECT s.id, s.title, c.name, s.ended_at
		FROM live.sessions s
		JOIN content.classrooms c ON c.id = s.classroom_id AND c.archived_at IS NULL
		JOIN content.enrolments e ON e.classroom_id = c.id AND e.user_id = $1 AND e.status = 'active' AND e.created_at < s.created_at
		LEFT JOIN live.attempts a ON a.session_id = s.id AND a.user_id = $1
		WHERE s.status = 'ended' AND s.ended_at > $2 AND (a.id IS NULL OR a.state IN ('not_started', 'waiting'))
		ORDER BY s.ended_at DESC LIMIT 200`, uid, now.AddDate(0, 0, -60))
	if err != nil {
		return err
	}
	if err := each(rows, func(r pgx.Rows) error {
		var it Item
		var at time.Time
		if err := r.Scan(&it.ID, &it.Title, &it.Classroom, &at); err != nil {
			return err
		}
		it.Kind, it.Status, it.At, it.Href, it.Detail = "quiz", "missed", &at, "/my", "Missed quiz"
		out[ListOverdue] = append(out[ListOverdue], it)
		return nil
	}); err != nil {
		return err
	}

	// Tutorials and presentations of their classrooms, by date and progress.
	rows, err = s.pool.Query(ctx, `SELECT 'tutorial', t.id, t.title, c.name, t.opens_at, t.due_at, r.id IS NOT NULL, r.finished_at IS NOT NULL
		FROM tutorial.tutorials t
		JOIN content.topics tp ON tp.id = t.topic_id JOIN content.modules m ON m.id = tp.module_id
		JOIN content.classrooms c ON c.id = m.classroom_id AND c.archived_at IS NULL
		JOIN content.enrolments e ON e.classroom_id = c.id AND e.user_id = $1 AND e.status = 'active'
		LEFT JOIN tutorial.runs r ON r.tutorial_id = t.id AND r.user_id = $1
		WHERE t.status = 'published'
		UNION ALL
		SELECT 'presentation', p.id, p.title, c.name, p.opens_at, p.due_at, v.id IS NOT NULL, v.finished_at IS NOT NULL
		FROM presentation.presentations p
		JOIN content.topics tp ON tp.id = p.topic_id JOIN content.modules m ON m.id = tp.module_id
		JOIN content.classrooms c ON c.id = m.classroom_id AND c.archived_at IS NULL
		JOIN content.enrolments e ON e.classroom_id = c.id AND e.user_id = $1 AND e.status = 'active'
		LEFT JOIN presentation.viewers v ON v.presentation_id = p.id AND v.user_id = $1
		WHERE p.status = 'published' AND p.version > 0
		LIMIT 500`, uid)
	if err != nil {
		return err
	}
	if err := each(rows, func(r pgx.Rows) error {
		var it Item
		var opens, due *time.Time
		var started, finished bool
		if err := r.Scan(&it.Kind, &it.ID, &it.Title, &it.Classroom, &opens, &due, &started, &finished); err != nil {
			return err
		}
		list, status, at := classify(now, opens, due, started, finished)
		if list == "" {
			return nil
		}
		// Not open yet: nothing to follow until it opens.
		if status == "opens" {
			it.Href = ""
		} else if it.Kind == "tutorial" {
			it.Href = "/learn/" + it.ID
		} else {
			it.Href = "/slides/" + it.ID
		}
		it.Status, it.At = status, at
		out[list] = append(out[list], it)
		return nil
	}); err != nil {
		return err
	}

	// Presentations being presented, and meetings open, for their classrooms.
	rows, err = s.pool.Query(ctx, `SELECT 'presentation', p.id, p.title, c.name, l.started_at, ''
		FROM presentation.lives l JOIN presentation.presentations p ON p.id = l.presentation_id
		JOIN content.topics tp ON tp.id = p.topic_id JOIN content.modules m ON m.id = tp.module_id
		JOIN content.classrooms c ON c.id = m.classroom_id AND c.archived_at IS NULL
		JOIN content.enrolments e ON e.classroom_id = c.id AND e.user_id = $1 AND e.status = 'active'
		WHERE l.ended_at IS NULL
		UNION ALL
		SELECT 'meeting', mt.id, mt.title, c.name, mt.created_at, mt.code
		FROM meet.meetings mt JOIN content.classrooms c ON c.id = mt.classroom_id AND c.archived_at IS NULL
		JOIN content.enrolments e ON e.classroom_id = c.id AND e.user_id = $1 AND e.status = 'active'
		WHERE mt.status = 'open'
		LIMIT 200`, uid)
	if err != nil {
		return err
	}
	return each(rows, func(r pgx.Rows) error {
		var it Item
		var at time.Time
		var code string
		if err := r.Scan(&it.Kind, &it.ID, &it.Title, &it.Classroom, &at, &code); err != nil {
			return err
		}
		it.Status, it.At = "live", &at
		if it.Kind == "meeting" {
			it.Href, it.Detail = "/m/"+code, "Meeting open now"
		} else {
			it.Href, it.Detail = "/slides/"+it.ID+"/live", "Being presented now"
		}
		out[ListLive] = append(out[ListLive], it)
		return nil
	})
}

func (s *Service) teacher(ctx context.Context, uid string, out map[string][]Item) error {
	now := s.now()
	// Their activities live now.
	rows, err := s.pool.Query(ctx, `SELECT 'quiz', s.id, s.title, c.name, coalesce(s.started_at, s.created_at), '/t/sessions/' || s.id,
			(SELECT count(*) FROM live.attempts a WHERE a.session_id = s.id AND a.state IN ('waiting', 'admitted', 'in_progress', 'paused'))::int
		FROM live.sessions s JOIN content.classrooms c ON c.id = s.classroom_id
		WHERE s.teacher_id = $1 AND s.status <> 'ended'
		UNION ALL
		SELECT 'poll', p.id, p.title, coalesce(c.name, ''), coalesce(p.opened_at, p.created_at), '/t/polls/' || p.id,
			(SELECT count(*) FROM poll.participants x WHERE x.poll_id = p.id)::int
		FROM poll.polls p LEFT JOIN content.classrooms c ON c.id = p.classroom_id
		WHERE p.teacher_id = $1 AND p.status = 'open'
		UNION ALL
		SELECT 'meeting', m.id, m.title, coalesce(c.name, ''), m.created_at, '/m/' || m.code, -1
		FROM meet.meetings m LEFT JOIN content.classrooms c ON c.id = m.classroom_id
		WHERE m.host_id = $1 AND m.status = 'open'
		UNION ALL
		SELECT 'presentation', p.id, p.title, c.name, l.started_at, '/t/presentations/' || p.id || '/present', -1
		FROM presentation.lives l JOIN presentation.presentations p ON p.id = l.presentation_id
		JOIN content.topics tp ON tp.id = p.topic_id JOIN content.modules m ON m.id = tp.module_id
		JOIN content.classrooms c ON c.id = m.classroom_id
		WHERE p.teacher_id = $1 AND l.ended_at IS NULL
		LIMIT 200`, uid)
	if err != nil {
		return err
	}
	if err := each(rows, func(r pgx.Rows) error {
		var it Item
		var at time.Time
		var n int
		if err := r.Scan(&it.Kind, &it.ID, &it.Title, &it.Classroom, &at, &it.Href, &n); err != nil {
			return err
		}
		it.Status, it.At = "live", &at
		switch {
		case it.Kind == "quiz":
			it.Detail = plural(n, "student", "students") + " in it"
		case it.Kind == "poll":
			it.Detail = plural(n, "participant", "participants")
		}
		out[ListLive] = append(out[ListLive], it)
		return nil
	}); err != nil {
		return err
	}

	// Their tutorials and presentations: opening or due later, or past due
	// in the last 30 days with students who haven't finished.
	rows, err = s.pool.Query(ctx, `WITH mine AS (
			SELECT 'tutorial' AS kind, t.id, t.title, c.id AS classroom_id, c.name AS classroom, t.opens_at, t.due_at,
				(SELECT count(*) FROM tutorial.runs r WHERE r.tutorial_id = t.id AND r.finished_at IS NOT NULL) AS finished
			FROM tutorial.tutorials t JOIN content.topics tp ON tp.id = t.topic_id JOIN content.modules m ON m.id = tp.module_id
			JOIN content.classrooms c ON c.id = m.classroom_id AND c.archived_at IS NULL
			WHERE t.teacher_id = $1 AND t.status = 'published'
			UNION ALL
			SELECT 'presentation', p.id, p.title, c.id, c.name, p.opens_at, p.due_at,
				(SELECT count(*) FROM presentation.viewers v WHERE v.presentation_id = p.id AND v.finished_at IS NOT NULL)
			FROM presentation.presentations p JOIN content.topics tp ON tp.id = p.topic_id JOIN content.modules m ON m.id = tp.module_id
			JOIN content.classrooms c ON c.id = m.classroom_id AND c.archived_at IS NULL
			WHERE p.teacher_id = $1 AND p.status = 'published')
		SELECT kind, id, title, classroom, opens_at, due_at, finished::int,
			(SELECT count(*) FROM content.enrolments e WHERE e.classroom_id = mine.classroom_id AND e.status = 'active')::int
		FROM mine WHERE opens_at > $2 OR due_at > $2 - interval '30 days'
		LIMIT 500`, uid, now)
	if err != nil {
		return err
	}
	if err := each(rows, func(r pgx.Rows) error {
		var it Item
		var opens, due *time.Time
		var finished, students int
		if err := r.Scan(&it.Kind, &it.ID, &it.Title, &it.Classroom, &opens, &due, &finished, &students); err != nil {
			return err
		}
		if it.Kind == "tutorial" {
			it.Href = "/t/tutorials/" + it.ID
		} else {
			it.Href = "/t/presentations/" + it.ID
		}
		it.Detail = fmt.Sprintf("%d of %s finished", finished, plural(students, "student", "students"))
		list, status, at := classify(now, opens, due, false, false)
		if list == ListPending || (list == ListOverdue && finished >= students) {
			return nil // no date to show, or everyone finished
		}
		it.Status, it.At = status, at
		out[list] = append(out[list], it)
		return nil
	}); err != nil {
		return err
	}

	// Waiting on them: answers to mark, and students asking to join.
	rows, err = s.pool.Query(ctx, `SELECT 'marking', s.id, s.title, c.name, max(s.ended_at), '/t/sessions/' || s.id || '/results', count(*)::int
		FROM eval.marks mk JOIN live.attempts a ON a.id = mk.attempt_id JOIN live.sessions s ON s.id = a.session_id
		JOIN content.classrooms c ON c.id = s.classroom_id
		WHERE s.teacher_id = $1 AND mk.status IN ('pending', 'needs_manual')
		GROUP BY s.id, s.title, c.name
		UNION ALL
		SELECT 'marking', t.id, t.title, c.name, max(an.answered_at), '/t/tutorials/' || t.id, count(*)::int
		FROM tutorial.answers an JOIN tutorial.runs r ON r.id = an.run_id JOIN tutorial.tutorials t ON t.id = r.tutorial_id
		JOIN content.topics tp ON tp.id = t.topic_id JOIN content.modules m ON m.id = tp.module_id
		JOIN content.classrooms c ON c.id = m.classroom_id
		WHERE t.teacher_id = $1 AND an.status = 'pending'
		GROUP BY t.id, t.title, c.name
		UNION ALL
		SELECT 'enrolment', c.id, c.name, c.name, max(e.created_at), '/t/classrooms/' || c.id, count(*)::int
		FROM content.enrolments e JOIN content.classrooms c ON c.id = e.classroom_id
		WHERE c.teacher_id = $1 AND e.status = 'pending'
		GROUP BY c.id, c.name
		LIMIT 200`, uid)
	if err != nil {
		return err
	}
	return each(rows, func(r pgx.Rows) error {
		var it Item
		var at *time.Time
		var n int
		if err := r.Scan(&it.Kind, &it.ID, &it.Title, &it.Classroom, &at, &it.Href, &n); err != nil {
			return err
		}
		it.At = at
		if it.Kind == "enrolment" {
			it.Status, it.Detail = "to_approve", plural(n, "student asks", "students ask")+" to join"
		} else {
			it.Status, it.Detail = "to_mark", plural(n, "answer", "answers")+" to mark"
		}
		out[ListPending] = append(out[ListPending], it)
		return nil
	})
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func each(rows pgx.Rows, f func(pgx.Rows) error) error {
	defer rows.Close()
	for rows.Next() {
		if err := f(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Routes mounts GET /api/my/dashboard (counts) and /api/my/dashboard/{list}
// (one page of a list) for students and teachers.
func (s *Service) Routes(r chi.Router) {
	r.Method("GET", "/my/dashboard", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u := auth.MustUser(r.Context())
		all, err := s.Lists(r.Context(), u)
		if err != nil {
			return err
		}
		sum := Summary{Role: string(u.Role), Counts: map[string]int{}}
		for l, items := range all {
			sum.Counts[l] = len(items)
		}
		httpx.JSON(w, http.StatusOK, sum)
		return nil
	}))
	r.Method("GET", "/my/dashboard/insights", httpx.Handler(s.insightsRoute)) // charts (D-105)
	r.Method("GET", "/my/dashboard/{list}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u := auth.MustUser(r.Context())
		list := chi.URLParam(r, "list")
		all, err := s.Lists(r.Context(), u)
		if err != nil {
			return err
		}
		items, ok := all[list]
		if !ok {
			return httpx.ErrNotFound
		}
		page, size := intParam(r, "page", 1, 1, 10000), intParam(r, "size", 10, 1, 50)
		from := min((page-1)*size, len(items))
		to := min(from+size, len(items))
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items[from:to], "total": len(items), "page": page, "size": size})
		return nil
	}))
}

func intParam(r *http.Request, k string, def, lo, hi int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(k))
	if err != nil {
		return def
	}
	return max(lo, min(hi, n))
}
