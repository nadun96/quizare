package presentation

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// A student going through a presentation at their own pace (PR-FR-20 to
// PR-FR-22, PR-FR-30). Time counts only while the slide is on screen and in
// use; the browser reports it, and a report adds no more than has passed.

var (
	errNotOpen = httpx.NewError(http.StatusForbidden, "presentation_not_open", "this presentation isn't open yet")
	errInOrder = httpx.NewError(http.StatusConflict, "in_order", "this presentation goes in order: see the slides before it first")
)

const maxReport = 60 // seconds one time report may add

// student finds a published presentation a student may open.
func (s *Service) student(ctx context.Context, userID, id string) (Presentation, error) {
	p, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+from+` WHERE p.id=$1 AND p.status='published' AND p.version > 0`, id))
	if err != nil {
		return p, err
	}
	tc, err := s.topics.TopicContext(ctx, "", p.TopicID)
	if err != nil {
		return p, err
	}
	e, ok, err := s.topics.GetEnrolment(ctx, tc.ClassroomID, userID)
	if err != nil {
		return p, err
	}
	if !ok || e.Status != "active" || tc.ClassroomArchived {
		return p, httpx.ErrNotFound
	}
	if p.OpensAt != nil && s.now().Before(*p.OpensAt) {
		return p, errNotOpen
	}
	return p, nil
}

type viewer struct {
	id       string
	current  int
	reached  int
	finished *time.Time
}

func (s *Service) viewer(ctx context.Context, p Presentation, userID string, start bool) (viewer, error) {
	var v viewer
	err := s.pool.QueryRow(ctx, `SELECT id, current, reached, finished_at FROM presentation.viewers WHERE presentation_id=$1 AND user_id=$2`, p.ID, userID).
		Scan(&v.id, &v.current, &v.reached, &v.finished)
	if errors.Is(err, pgx.ErrNoRows) && start {
		err = s.pool.QueryRow(ctx, `INSERT INTO presentation.viewers(presentation_id, user_id) VALUES ($1,$2)
			ON CONFLICT (presentation_id, user_id) DO UPDATE SET updated_at=now() RETURNING id, current, reached, finished_at`, p.ID, userID).
			Scan(&v.id, &v.current, &v.reached, &v.finished)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return v, httpx.ErrNotFound
	}
	v.current = min(max(v.current, 1), max(p.Slides, 1))
	v.reached = min(max(v.reached, v.current), max(p.Slides, 1))
	return v, err
}

// mayOpen: slide n can be shown now; in order, that's up to the next unseen one.
func (p Presentation) mayOpen(v viewer, n int) bool {
	return n >= 1 && n <= p.Slides && (p.AllowJump || n <= v.reached+1)
}

// View is what the student's viewer shows.
type View struct {
	Presentation struct {
		ID            string     `json:"id"`
		Title         string     `json:"title"`
		Description   string     `json:"description"`
		AllowJump     bool       `json:"allow_jump"`
		AllowDownload bool       `json:"allow_download"`
		Version       int        `json:"version"`
		DueAt         *time.Time `json:"due_at,omitempty"`
	} `json:"presentation"`
	Slides   []Slide `json:"slides"`
	Current  int     `json:"current"`
	Reached  int     `json:"reached"`
	Finished bool    `json:"finished"`
}

func (s *Service) view(ctx context.Context, p Presentation, v viewer) (View, error) {
	var out View
	out.Presentation.ID, out.Presentation.Title, out.Presentation.Description = p.ID, p.Title, p.Description
	out.Presentation.AllowJump, out.Presentation.AllowDownload, out.Presentation.Version, out.Presentation.DueAt = p.AllowJump, p.AllowDownload, p.Version, p.DueAt
	out.Current, out.Reached, out.Finished = v.current, v.reached, v.finished != nil
	var err error
	out.Slides, err = s.slides(ctx, p.ID, p.Version)
	return out, err
}

// arrive records that the student is on slide n.
func (s *Service) arrive(ctx context.Context, p Presentation, v *viewer, n int) error {
	now := s.now()
	v.current = n
	v.reached = max(v.reached, n)
	var finishing *time.Time // when they first reach the last slide
	if v.finished == nil && v.reached >= p.Slides {
		finishing, v.finished = &now, &now
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE presentation.viewers SET current=$2, reached=$3, finished_at=coalesce(finished_at, $4), updated_at=$5 WHERE id=$1`,
			v.id, v.current, v.reached, finishing, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO presentation.views(viewer_id, n, mode, first_at, last_at) VALUES ($1,$2,'self',$3,$3)
			ON CONFLICT (viewer_id, n, mode) DO UPDATE SET last_at=$3`, v.id, n, now)
		return err
	})
}

// Open starts or resumes, on the slide they were on.
func (s *Service) Open(ctx context.Context, userID, id string) (View, error) {
	p, err := s.student(ctx, userID, id)
	if err != nil {
		return View{}, err
	}
	v, err := s.viewer(ctx, p, userID, true)
	if err != nil {
		return View{}, err
	}
	if err := s.arrive(ctx, p, &v, v.current); err != nil {
		return View{}, err
	}
	return s.view(ctx, p, v)
}

// Go moves to slide n: any slide, or in order up to the next unseen one.
func (s *Service) Go(ctx context.Context, userID, id string, n int) (View, error) {
	p, err := s.student(ctx, userID, id)
	if err != nil {
		return View{}, err
	}
	v, err := s.viewer(ctx, p, userID, true)
	if err != nil {
		return View{}, err
	}
	if n < 1 || n > p.Slides {
		return View{}, httpx.ErrNotFound
	}
	if !p.mayOpen(v, n) {
		return View{}, errInOrder
	}
	if err := s.arrive(ctx, p, &v, n); err != nil {
		return View{}, err
	}
	return s.view(ctx, p, v)
}

// Time adds seconds spent on the slide the student is on: at most a
// minute a report, and no more than has passed since the last one.
func (s *Service) Time(ctx context.Context, userID, id string, n, seconds int) error {
	p, err := s.student(ctx, userID, id)
	if err != nil {
		return err
	}
	v, err := s.viewer(ctx, p, userID, false)
	if errors.Is(err, httpx.ErrNotFound) || (err == nil && n != v.current) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE presentation.views SET
			seconds = seconds + LEAST($3, GREATEST(0, ceil(extract(epoch FROM $4 - last_at))::int + 2)), last_at = $4
		WHERE viewer_id=$1 AND n=$2 AND mode='self'`, v.id, n, min(max(seconds, 0), maxReport), s.now())
	return err
}

// Image returns a slide's JPEG, if the student may see it (and the next one
// in order, so it can load ahead, PR-NFR-01).
func (s *Service) Image(ctx context.Context, userID, id string, version, n int) ([]byte, error) {
	p, err := s.student(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if version != p.Version {
		return nil, httpx.ErrNotFound
	}
	v, err := s.viewer(ctx, p, userID, false)
	if err != nil {
		return nil, err
	}
	if !p.mayOpen(v, n) {
		return nil, httpx.ErrNotFound
	}
	return s.image(ctx, id, version, n)
}

// Download returns the PDF, when the teacher allows it (PR-FR-21).
func (s *Service) Download(ctx context.Context, userID, id string) (PDF, error) {
	p, err := s.student(ctx, userID, id)
	if err != nil {
		return PDF{}, err
	}
	if !p.AllowDownload {
		return PDF{}, httpx.ErrNotFound
	}
	return s.pdf(ctx, id, p.Version)
}

// Mine is a published presentation in a student's list.
type Mine struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Classroom   string     `json:"classroom"`
	Slides      int        `json:"slide_count"`
	OpensAt     *time.Time `json:"opens_at,omitempty"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	Started     bool       `json:"started"`
	Reached     int        `json:"reached"`
	Finished    bool       `json:"finished"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// MyPresentations lists the published presentations of a student's classrooms.
func (s *Service) MyPresentations(ctx context.Context, userID string) ([]Mine, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id, p.title, c.name, f.slides, p.opens_at, p.due_at,
			v.id IS NOT NULL, coalesce(v.reached, 0), v.finished_at IS NOT NULL, p.published_at
		FROM presentation.presentations p
		JOIN presentation.files f ON f.presentation_id = p.id AND f.version = p.version
		JOIN content.topics tp ON tp.id = p.topic_id
		JOIN content.modules m ON m.id = tp.module_id
		JOIN content.classrooms c ON c.id = m.classroom_id AND c.archived_at IS NULL
		JOIN content.enrolments e ON e.classroom_id = c.id AND e.user_id = $1 AND e.status = 'active'
		LEFT JOIN presentation.viewers v ON v.presentation_id = p.id AND v.user_id = $1
		WHERE p.status = 'published'
		ORDER BY (v.finished_at IS NOT NULL), p.due_at NULLS LAST, p.published_at DESC LIMIT 200`, userID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Mine, error) {
		var m Mine
		return m, r.Scan(&m.ID, &m.Title, &m.Classroom, &m.Slides, &m.OpensAt, &m.DueAt, &m.Started, &m.Reached, &m.Finished, &m.PublishedAt)
	})
	if out == nil {
		out = []Mine{}
	}
	return out, err
}
