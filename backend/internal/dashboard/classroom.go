package dashboard

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// A student's view of one classroom they're enrolled in (D-138): what's on
// now, what's due, what's coming, and everything they've completed.

// FinishedQuiz is a quiz a student took, as the live module sees it: its
// release depends on the session's settings.
type FinishedQuiz struct {
	AttemptID string
	Title     string
	State     string // submitted or invalidated
	Released  bool
	At        *time.Time
}

// SetFinishedQuizzes gives the service a student's finished quizzes in a classroom.
func (s *Service) SetFinishedQuizzes(f func(ctx context.Context, userID, classroomID string) ([]FinishedQuiz, error)) {
	s.finished = f
}

// Done is something the student has completed.
type Done struct {
	Kind   string     `json:"kind"` // quiz, tutorial, presentation
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	At     *time.Time `json:"at,omitempty"`
	Status string     `json:"status"` // submitted, invalidated, finished
	// The mark, once the student may see it.
	Released bool     `json:"released"`
	Score    *float64 `json:"score,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Pct      *float64 `json:"pct,omitempty"`
	Passed   *bool    `json:"passed,omitempty"`
	Pending  bool     `json:"pending,omitempty"` // still being marked
	Tries    int      `json:"tries,omitempty"`   // of a quiz, when more than one
	Href     string   `json:"href,omitempty"`
}

// ClassroomWork is the whole view.
type ClassroomWork struct {
	ClassroomID   string  `json:"classroom_id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	Teacher       string  `json:"teacher"`
	Status        string  `json:"status"` // active, or pending approval
	StudentNumber *string `json:"student_number"`
	Live          []Item  `json:"live"`
	Due           []Item  `json:"due"`      // overdue first, then by date, then undated
	Upcoming      []Item  `json:"upcoming"` // not open yet
	Completed     []Done  `json:"completed"`
}

// dueRank orders the to-do list: overdue and missed first, then what has a date.
func dueRank(it Item) int {
	switch it.Status {
	case "overdue":
		return 0
	case "missed":
		return 1
	case "due_soon":
		return 2
	case "due":
		return 3
	case "in_progress":
		return 4
	}
	return 5
}

// Classroom builds the view for a student enrolled in the classroom.
func (s *Service) Classroom(ctx context.Context, userID, classroomID string) (ClassroomWork, error) {
	w := ClassroomWork{ClassroomID: classroomID, Live: []Item{}, Due: []Item{}, Upcoming: []Item{}, Completed: []Done{}}
	err := s.pool.QueryRow(ctx, `SELECT c.name, c.description, coalesce(u.name, ''), e.status, e.student_number
		FROM content.enrolments e JOIN content.classrooms c ON c.id = e.classroom_id AND c.archived_at IS NULL
		LEFT JOIN auth.users u ON u.id = c.teacher_id
		WHERE e.classroom_id::text = $1 AND e.user_id = $2 AND e.status IN ('active', 'pending')`, classroomID, userID).
		Scan(&w.Name, &w.Description, &w.Teacher, &w.Status, &w.StudentNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, httpx.ErrNotFound
	}
	if err != nil {
		return w, err
	}
	if w.Status != "active" {
		return w, nil // nothing to do until the teacher approves them
	}

	lists := map[string][]Item{}
	if err := s.student(ctx, userID, classroomID, lists); err != nil {
		return w, err
	}
	w.Live = append(w.Live, lists[ListLive]...)
	w.Due = append(append(w.Due, lists[ListOverdue]...), lists[ListPending]...)
	for _, it := range lists[ListUpcoming] {
		if it.Status == "opens" {
			w.Upcoming = append(w.Upcoming, it)
		} else {
			w.Due = append(w.Due, it)
		}
	}
	sort.SliceStable(w.Due, func(i, j int) bool {
		a, b := w.Due[i], w.Due[j]
		if ra, rb := dueRank(a), dueRank(b); ra != rb {
			return ra < rb
		}
		return a.At != nil && b.At != nil && a.At.Before(*b.At)
	})
	sort.SliceStable(w.Upcoming, func(i, j int) bool {
		a, b := w.Upcoming[i].At, w.Upcoming[j].At
		return a != nil && b != nil && a.Before(*b)
	})

	if err := s.completedQuizzes(ctx, userID, classroomID, &w); err != nil {
		return w, err
	}
	if err := s.completedLessons(ctx, userID, classroomID, &w); err != nil {
		return w, err
	}
	sort.SliceStable(w.Completed, func(i, j int) bool {
		a, b := w.Completed[i].At, w.Completed[j].At
		return a != nil && (b == nil || a.After(*b))
	})
	return w, nil
}

func (s *Service) completedQuizzes(ctx context.Context, userID, classroomID string, w *ClassroomWork) error {
	if s.finished == nil {
		return nil
	}
	quizzes, err := s.finished(ctx, userID, classroomID)
	if err != nil {
		return err
	}
	for _, q := range quizzes {
		d := Done{Kind: "quiz", ID: q.AttemptID, Title: q.Title, At: q.At, Status: q.State, Released: q.Released}
		if err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM live.attempts o JOIN live.attempts a ON a.session_id = o.session_id AND a.user_id = o.user_id
			WHERE o.id::text = $1`, q.AttemptID).Scan(&d.Tries); err != nil {
			return err
		}
		if q.Released {
			d.Href = "/my/results/" + q.AttemptID
			var score, max, pct float64
			var passed, complete bool
			err := s.pool.QueryRow(ctx, `SELECT score::float8, max_score::float8, pct::float8, passed, complete FROM eval.results WHERE attempt_id::text = $1`, q.AttemptID).
				Scan(&score, &max, &pct, &passed, &complete)
			switch {
			case errors.Is(err, pgx.ErrNoRows) || (err == nil && !complete):
				d.Pending = true
			case err != nil:
				return err
			default:
				d.Score, d.Max, d.Pct, d.Passed = &score, &max, &pct, &passed
			}
		}
		w.Completed = append(w.Completed, d)
	}
	return nil
}

// completedLessons adds finished tutorials and presentations. A tutorial's
// score shows unless the teacher releases marks and hasn't yet.
func (s *Service) completedLessons(ctx context.Context, userID, classroomID string, w *ClassroomWork) error {
	rows, err := s.pool.Query(ctx, `SELECT 'tutorial', t.id::text, t.title, r.finished_at,
			t.feedback <> 'release' OR t.released_at IS NOT NULL,
			coalesce((SELECT sum(a.score) FROM tutorial.answers a WHERE a.run_id = r.id), 0)::float8,
			coalesce((SELECT sum(x.marks) FROM quiz.questions x WHERE x.quiz_id = t.quiz_id), 0)::float8,
			EXISTS (SELECT 1 FROM tutorial.answers a WHERE a.run_id = r.id AND a.status = 'pending')
		FROM tutorial.runs r JOIN tutorial.tutorials t ON t.id = r.tutorial_id
		JOIN content.topics tp ON tp.id = t.topic_id JOIN content.modules m ON m.id = tp.module_id
		WHERE r.user_id = $1 AND m.classroom_id::text = $2 AND r.finished_at IS NOT NULL
		UNION ALL
		SELECT 'presentation', p.id::text, p.title, v.finished_at, false, 0, 0, false
		FROM presentation.viewers v JOIN presentation.presentations p ON p.id = v.presentation_id
		JOIN content.topics tp ON tp.id = p.topic_id JOIN content.modules m ON m.id = tp.module_id
		WHERE v.user_id = $1 AND m.classroom_id::text = $2 AND v.finished_at IS NOT NULL
		LIMIT 500`, userID, classroomID)
	if err != nil {
		return err
	}
	return each(rows, func(r pgx.Rows) error {
		d := Done{Status: "finished"}
		var at time.Time
		var score, max float64
		var pending bool
		if err := r.Scan(&d.Kind, &d.ID, &d.Title, &at, &d.Released, &score, &max, &pending); err != nil {
			return err
		}
		d.At = &at
		if d.Kind == "tutorial" {
			d.Href = "/learn/" + d.ID
			if d.Released && max > 0 {
				if pending {
					d.Pending = true
				} else {
					pct := float64(int64(score/max*1000+0.5)) / 10
					d.Score, d.Max, d.Pct = &score, &max, &pct
				}
			}
		} else {
			d.Href = "/slides/" + d.ID
		}
		w.Completed = append(w.Completed, d)
		return nil
	})
}

func (s *Service) classroomRoute(w http.ResponseWriter, r *http.Request) error {
	work, err := s.Classroom(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, work)
	return nil
}
