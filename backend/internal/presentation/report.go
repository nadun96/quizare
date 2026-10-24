package presentation

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Who saw which slide, and for how long (PR-FR-31): per student, and per slide.

// StudentViews is one student's viewing.
type StudentViews struct {
	UserID        string     `json:"user_id"`
	ViewerID      string     `json:"viewer_id,omitempty"`
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	StudentNumber *string    `json:"student_number,omitempty"`
	Started       bool       `json:"started"`
	Seen          int        `json:"seen"` // slides opened
	Seconds       int        `json:"seconds"`
	Current       int        `json:"current"`
	Finished      bool       `json:"finished"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
}

// SlideViews is one slide's viewing.
type SlideViews struct {
	N             int    `json:"n"`
	Text          string `json:"text"`
	Viewers       int    `json:"viewers"`
	MedianSeconds int    `json:"median_seconds"`
	TotalSeconds  int    `json:"total_seconds"`
}

// Report is the whole presentation's.
type Report struct {
	Slides   []SlideViews   `json:"slides"`
	Students []StudentViews `json:"students"`
}

func median(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	if n := len(s); n%2 == 1 {
		return s[n/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// Report lists the classroom's students with their viewing, and each slide's.
func (s *Service) Report(ctx context.Context, teacherID, id string) (Report, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return Report{}, err
	}
	tc, err := s.topics.TopicContext(ctx, teacherID, p.TopicID)
	if err != nil {
		return Report{}, err
	}
	enrolled, err := s.topics.ListEnrolments(ctx, teacherID, tc.ClassroomID, "active")
	if err != nil {
		return Report{}, err
	}
	slides, err := s.slides(ctx, id, p.Version)
	if err != nil {
		return Report{}, err
	}
	out := Report{Slides: make([]SlideViews, len(slides)), Students: []StudentViews{}}
	per := make([][]int, len(slides))
	for i, sl := range slides {
		out.Slides[i] = SlideViews{N: sl.N, Text: sl.Text}
	}
	viewers := map[string]StudentViews{}
	rows, err := s.pool.Query(ctx, `SELECT v.id, v.user_id, v.current, v.finished_at IS NOT NULL, v.updated_at,
			coalesce(array_agg(w.n) FILTER (WHERE w.n IS NOT NULL), '{}'), coalesce(array_agg(w.seconds) FILTER (WHERE w.n IS NOT NULL), '{}')
		FROM presentation.viewers v LEFT JOIN presentation.views w ON w.viewer_id = v.id AND w.mode = 'self'
		WHERE v.presentation_id=$1 GROUP BY v.id`, id)
	if err != nil {
		return Report{}, err
	}
	for rows.Next() {
		var sv StudentViews
		var at time.Time
		var ns, secs []int32
		if err := rows.Scan(&sv.ViewerID, &sv.UserID, &sv.Current, &sv.Finished, &at, &ns, &secs); err != nil {
			rows.Close()
			return Report{}, err
		}
		sv.Started, sv.UpdatedAt = true, &at
		for i, n := range ns {
			if n < 1 || int(n) > len(slides) {
				continue // a slide of a longer file, since replaced
			}
			sv.Seen++
			sv.Seconds += int(secs[i])
			out.Slides[n-1].Viewers++
			out.Slides[n-1].TotalSeconds += int(secs[i])
			per[n-1] = append(per[n-1], int(secs[i]))
		}
		viewers[sv.UserID] = sv
	}
	rows.Close()
	for i := range out.Slides {
		out.Slides[i].MedianSeconds = median(per[i])
	}
	for _, e := range enrolled {
		sv, ok := viewers[e.UserID]
		if !ok {
			sv = StudentViews{UserID: e.UserID}
		}
		sv.Name, sv.Email, sv.StudentNumber = e.StudentName, e.StudentEmail, e.StudentNumber
		out.Students = append(out.Students, sv)
		delete(viewers, e.UserID)
	}
	if len(viewers) > 0 { // students who have left the classroom since
		ids := make([]string, 0, len(viewers))
		for id := range viewers {
			ids = append(ids, id)
		}
		users, err := s.users.UsersByID(ctx, ids)
		if err != nil {
			return Report{}, err
		}
		for id, sv := range viewers {
			sv.Name, sv.Email = users[id].Name, users[id].Email
			out.Students = append(out.Students, sv)
		}
	}
	return out, nil
}

// SlideTime is one slide's time for one student.
type SlideTime struct {
	N       int        `json:"n"`
	Seconds int        `json:"seconds"`
	FirstAt *time.Time `json:"first_at,omitempty"`
}

// ViewerDetail is one student's time per slide.
type ViewerDetail struct {
	ViewerID string      `json:"viewer_id"`
	Name     string      `json:"name"`
	Slides   []SlideTime `json:"slides"`
}

// Viewer shows one student's time on each slide.
func (s *Service) Viewer(ctx context.Context, teacherID, id, viewerID string) (ViewerDetail, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return ViewerDetail{}, err
	}
	var userID string
	err = s.pool.QueryRow(ctx, `SELECT user_id FROM presentation.viewers WHERE id=$1 AND presentation_id=$2`, viewerID, id).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ViewerDetail{}, httpx.ErrNotFound
	}
	if err != nil {
		return ViewerDetail{}, err
	}
	users, err := s.users.UsersByID(ctx, []string{userID})
	if err != nil {
		return ViewerDetail{}, err
	}
	d := ViewerDetail{ViewerID: viewerID, Name: users[userID].Name, Slides: make([]SlideTime, p.Slides)}
	for i := range d.Slides {
		d.Slides[i].N = i + 1
	}
	rows, err := s.pool.Query(ctx, `SELECT n, seconds, first_at FROM presentation.views WHERE viewer_id=$1 AND mode='self'`, viewerID)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var st SlideTime
		var at time.Time
		if err := rows.Scan(&st.N, &st.Seconds, &at); err != nil {
			return d, err
		}
		if st.N >= 1 && st.N <= len(d.Slides) {
			st.FirstAt = &at
			d.Slides[st.N-1] = st
		}
	}
	return d, rows.Err()
}
