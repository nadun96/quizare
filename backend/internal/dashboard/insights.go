package dashboard

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Insights (D-105): the dashboard's charts, over the last 30, 90 or 365
// days with the period before it to compare. A student sees their scores
// against each quiz's class average, their weeks of activity and their
// classrooms; a teacher sees their students' activity, each quiz's average
// and pass rate, and each classroom's participation. Only results the
// student could see (released, complete, not invalidated) count for them;
// a teacher sees every complete result in their quizzes.

// Ranges a dashboard can show, in days.
var Ranges = []int{30, 90, 365}

const weeks = 12 // the activity strip

// KPI is one headline number, with the period before it.
type KPI struct {
	Value float64  `json:"value"`
	Prev  *float64 `json:"prev"` // nil when the period before had nothing to compare
}

// Score is one quiz result on the student's chart.
type Score struct {
	At        time.Time `json:"at"`
	Title     string    `json:"title"`
	Classroom string    `json:"classroom"`
	Pct       float64   `json:"pct"`
	ClassAvg  *float64  `json:"class_avg"`
	Passed    bool      `json:"passed"`
}

// SessionStat is one quiz session on the teacher's chart.
type SessionStat struct {
	At        time.Time `json:"at"`
	Title     string    `json:"title"`
	Classroom string    `json:"classroom"`
	Avg       float64   `json:"avg"`
	PassRate  float64   `json:"pass_rate"`
	Results   int       `json:"results"`
}

// Week is one week of activity (Monday first).
type Week struct {
	Week      string `json:"week"` // 2006-01-02, its Monday
	Quizzes   int    `json:"quizzes"`
	Tutorials int    `json:"tutorials"`
	Students  int    `json:"students,omitempty"` // for teachers: students active that week
}

// ClassroomStat is one classroom's figures in the period.
type ClassroomStat struct {
	Name          string   `json:"name"`
	Avg           *float64 `json:"avg"`
	Quizzes       int      `json:"quizzes"`
	Students      int      `json:"students,omitempty"`
	Participation *float64 `json:"participation,omitempty"` // for teachers: % of students with a result
}

type Insights struct {
	Role       string          `json:"role"`
	Days       int             `json:"days"`
	KPIs       map[string]KPI  `json:"kpis"`
	Scores     []Score         `json:"scores,omitempty"`
	Sessions   []SessionStat   `json:"sessions,omitempty"`
	Weeks      []Week          `json:"weeks"`
	Classrooms []ClassroomStat `json:"classrooms"`
}

func monday(t time.Time) time.Time {
	t = t.UTC()
	d := (int(t.Weekday()) + 6) % 7
	return time.Date(t.Year(), t.Month(), t.Day()-d, 0, 0, 0, 0, time.UTC)
}

// emptyWeeks lists the last twelve weeks, oldest first.
func emptyWeeks(now time.Time) ([]Week, map[string]*Week) {
	out := make([]Week, weeks)
	idx := map[string]*Week{}
	start := monday(now).AddDate(0, 0, -7*(weeks-1))
	for i := range out {
		out[i].Week = start.AddDate(0, 0, 7*i).Format("2006-01-02")
	}
	for i := range out {
		idx[out[i].Week] = &out[i]
	}
	return out, idx
}

func avg(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	v := math.Round(s/float64(len(xs))*10) / 10
	return &v
}

func kpi(cur, prev float64, hasPrev bool) KPI {
	k := KPI{Value: cur}
	if hasPrev {
		k.Prev = &prev
	}
	return k
}

func pctKPI(cur, prev []float64) KPI {
	k := KPI{}
	if a := avg(cur); a != nil {
		k.Value = *a
	}
	k.Prev = avg(prev)
	return k
}

// Insights builds a person's charts for the last days.
func (s *Service) Insights(ctx context.Context, u auth.User, days int) (Insights, error) {
	now := s.now()
	from := now.AddDate(0, 0, -days)
	since := from.AddDate(0, 0, -days) // the period before, to compare
	if w := monday(now).AddDate(0, 0, -7*(weeks-1)); w.Before(since) {
		since = w
	}
	out := Insights{Role: string(u.Role), Days: days, KPIs: map[string]KPI{}, Classrooms: []ClassroomStat{}}
	var err error
	switch u.Role {
	case auth.RoleStudent:
		err = s.studentInsights(ctx, u.ID, now, from, since, &out)
	case auth.RoleTeacher:
		err = s.teacherInsights(ctx, u.ID, now, from, since, &out)
	default:
		out.Weeks = []Week{}
	}
	return out, err
}

func (s *Service) studentInsights(ctx context.Context, uid string, now, from, since time.Time, out *Insights) error {
	prevFrom := from.AddDate(0, 0, -out.Days)
	rows, err := s.pool.Query(ctx, `SELECT coalesce(a.submitted_at, s.ended_at, a.created_at), s.title, c.name, r.pct::float8, r.passed,
		(SELECT avg(r2.pct)::float8 FROM eval.results r2 WHERE r2.session_id = s.id AND r2.complete AND NOT r2.invalidated)
		FROM eval.results r JOIN live.attempts a ON a.id = r.attempt_id JOIN live.sessions s ON s.id = r.session_id
		JOIN content.classrooms c ON c.id = s.classroom_id
		WHERE r.user_id = $1 AND s.released_at IS NOT NULL AND r.complete AND NOT r.invalidated
		AND coalesce(a.submitted_at, s.ended_at, a.created_at) >= $2 ORDER BY 1`, uid, since)
	if err != nil {
		return err
	}
	var all []Score
	if err := each(rows, func(r pgx.Rows) error {
		var sc Score
		if err := r.Scan(&sc.At, &sc.Title, &sc.Classroom, &sc.Pct, &sc.Passed, &sc.ClassAvg); err != nil {
			return err
		}
		sc.Pct = math.Round(sc.Pct*10) / 10
		if sc.ClassAvg != nil {
			v := math.Round(*sc.ClassAvg*10) / 10
			sc.ClassAvg = &v
		}
		all = append(all, sc)
		return nil
	}); err != nil {
		return err
	}
	rows, err = s.pool.Query(ctx, `SELECT r.finished_at FROM tutorial.runs r WHERE r.user_id = $1 AND r.finished_at >= $2`, uid, since)
	if err != nil {
		return err
	}
	tutorials, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil {
		return err
	}

	ws, idx := emptyWeeks(now)
	var cur, prev []float64
	passed, curTut, prevTut := 0, 0, 0
	byRoom := map[string][]float64{}
	out.Scores = []Score{}
	for _, sc := range all {
		if w := idx[monday(sc.At).Format("2006-01-02")]; w != nil {
			w.Quizzes++
		}
		switch {
		case !sc.At.Before(from):
			cur = append(cur, sc.Pct)
			byRoom[sc.Classroom] = append(byRoom[sc.Classroom], sc.Pct)
			out.Scores = append(out.Scores, sc)
			if sc.Passed {
				passed++
			}
		case !sc.At.Before(prevFrom):
			prev = append(prev, sc.Pct)
		}
	}
	for _, t := range tutorials {
		if w := idx[monday(t).Format("2006-01-02")]; w != nil {
			w.Tutorials++
		}
		switch {
		case !t.Before(from):
			curTut++
		case !t.Before(prevFrom):
			prevTut++
		}
	}
	if len(out.Scores) > 30 {
		out.Scores = out.Scores[len(out.Scores)-30:]
	}
	out.Weeks = ws
	out.KPIs["avg_score"] = pctKPI(cur, prev)
	out.KPIs["quizzes"] = kpi(float64(len(cur)), float64(len(prev)), true)
	out.KPIs["tutorials"] = kpi(float64(curTut), float64(prevTut), true)
	if len(cur) > 0 {
		out.KPIs["pass_rate"] = KPI{Value: math.Round(float64(passed) / float64(len(cur)) * 100)}
	}
	for name, xs := range byRoom {
		out.Classrooms = append(out.Classrooms, ClassroomStat{Name: name, Avg: avg(xs), Quizzes: len(xs)})
	}
	sort.Slice(out.Classrooms, func(i, j int) bool { return *out.Classrooms[i].Avg > *out.Classrooms[j].Avg })
	return nil
}

func (s *Service) teacherInsights(ctx context.Context, tid string, now, from, since time.Time, out *Insights) error {
	prevFrom := from.AddDate(0, 0, -out.Days)
	// Every complete result in the teacher's quizzes since the earliest date needed.
	rows, err := s.pool.Query(ctx, `SELECT s.id, coalesce(s.started_at, s.created_at), s.title, c.name, r.user_id, r.pct::float8, r.passed,
		coalesce(a.submitted_at, s.ended_at, a.created_at)
		FROM eval.results r JOIN live.attempts a ON a.id = r.attempt_id JOIN live.sessions s ON s.id = r.session_id
		JOIN content.classrooms c ON c.id = s.classroom_id
		WHERE s.teacher_id = $1 AND r.complete AND NOT r.invalidated AND coalesce(a.submitted_at, s.ended_at, a.created_at) >= $2`, tid, since)
	if err != nil {
		return err
	}
	type res struct {
		session, title, room, user string
		started, at              time.Time
		pct                      float64
		passed                   bool
	}
	var all []res
	if err := each(rows, func(r pgx.Rows) error {
		var x res
		if err := r.Scan(&x.session, &x.started, &x.title, &x.room, &x.user, &x.pct, &x.passed, &x.at); err != nil {
			return err
		}
		all = append(all, x)
		return nil
	}); err != nil {
		return err
	}
	rows, err = s.pool.Query(ctx, `SELECT r.user_id, r.finished_at FROM tutorial.runs r JOIN tutorial.tutorials t ON t.id = r.tutorial_id
		WHERE t.teacher_id = $1 AND r.finished_at >= $2`, tid, since)
	if err != nil {
		return err
	}
	type fin struct {
		user string
		at   time.Time
	}
	var tuts []fin
	if err := each(rows, func(r pgx.Rows) error {
		var f fin
		if err := r.Scan(&f.user, &f.at); err != nil {
			return err
		}
		tuts = append(tuts, f)
		return nil
	}); err != nil {
		return err
	}
	// Their classrooms and how many students each has now.
	rows, err = s.pool.Query(ctx, `SELECT c.name, (SELECT count(*) FROM content.enrolments e WHERE e.classroom_id = c.id AND e.status = 'active')::int
		FROM content.classrooms c WHERE c.teacher_id = $1 AND c.archived_at IS NULL`, tid)
	if err != nil {
		return err
	}
	students := map[string]int{}
	total := 0
	if err := each(rows, func(r pgx.Rows) error {
		var name string
		var n int
		if err := r.Scan(&name, &n); err != nil {
			return err
		}
		students[name] += n
		total += n
		return nil
	}); err != nil {
		return err
	}
	var enrolled int
	if err := s.pool.QueryRow(ctx, `SELECT count(DISTINCT e.user_id) FROM content.enrolments e JOIN content.classrooms c ON c.id = e.classroom_id
		WHERE c.teacher_id = $1 AND c.archived_at IS NULL AND e.status = 'active'`, tid).Scan(&enrolled); err != nil {
		return err
	}

	ws, idx := emptyWeeks(now)
	weekUsers := map[string]map[string]bool{}
	mark := func(at time.Time, user string) {
		k := monday(at).Format("2006-01-02")
		if idx[k] == nil {
			return
		}
		if weekUsers[k] == nil {
			weekUsers[k] = map[string]bool{}
		}
		weekUsers[k][user] = true
	}
	var cur, prev []float64
	curPassed := 0
	activeCur, activePrev := map[string]bool{}, map[string]bool{}
	sessionsCur, sessionsPrev := map[string]bool{}, map[string]bool{}
	type agg struct {
		stat   SessionStat
		pcts   []float64
		passed int
	}
	bySession := map[string]*agg{}
	roomPcts, roomUsers := map[string][]float64{}, map[string]map[string]bool{}
	for _, x := range all {
		if w := idx[monday(x.at).Format("2006-01-02")]; w != nil {
			w.Quizzes++
		}
		mark(x.at, x.user)
		switch {
		case !x.at.Before(from):
			cur = append(cur, x.pct)
			if x.passed {
				curPassed++
			}
			activeCur[x.user] = true
			sessionsCur[x.session] = true
			a := bySession[x.session]
			if a == nil {
				a = &agg{stat: SessionStat{At: x.started, Title: x.title, Classroom: x.room}}
				bySession[x.session] = a
			}
			a.pcts = append(a.pcts, x.pct)
			if x.passed {
				a.passed++
			}
			roomPcts[x.room] = append(roomPcts[x.room], x.pct)
			if roomUsers[x.room] == nil {
				roomUsers[x.room] = map[string]bool{}
			}
			roomUsers[x.room][x.user] = true
		case !x.at.Before(prevFrom):
			prev = append(prev, x.pct)
			activePrev[x.user] = true
			sessionsPrev[x.session] = true
		}
	}
	for _, f := range tuts {
		if w := idx[monday(f.at).Format("2006-01-02")]; w != nil {
			w.Tutorials++
		}
		mark(f.at, f.user)
		switch {
		case !f.at.Before(from):
			activeCur[f.user] = true
		case !f.at.Before(prevFrom):
			activePrev[f.user] = true
		}
	}
	for i := range ws {
		ws[i].Students = len(weekUsers[ws[i].Week])
	}
	out.Weeks = ws

	out.KPIs["students"] = KPI{Value: float64(enrolled)}
	out.KPIs["active_students"] = kpi(float64(len(activeCur)), float64(len(activePrev)), true)
	out.KPIs["sessions"] = kpi(float64(len(sessionsCur)), float64(len(sessionsPrev)), true)
	out.KPIs["avg_score"] = pctKPI(cur, prev)
	if len(cur) > 0 {
		out.KPIs["pass_rate"] = KPI{Value: math.Round(float64(curPassed) / float64(len(cur)) * 100)}
	}

	out.Sessions = []SessionStat{}
	for _, a := range bySession {
		a.stat.Avg = *avg(a.pcts)
		a.stat.Results = len(a.pcts)
		a.stat.PassRate = math.Round(float64(a.passed) / float64(len(a.pcts)) * 100)
		out.Sessions = append(out.Sessions, a.stat)
	}
	sort.Slice(out.Sessions, func(i, j int) bool { return out.Sessions[i].At.Before(out.Sessions[j].At) })
	if len(out.Sessions) > 12 {
		out.Sessions = out.Sessions[len(out.Sessions)-12:]
	}
	for name, n := range students {
		c := ClassroomStat{Name: name, Students: n, Avg: avg(roomPcts[name]), Quizzes: len(roomPcts[name])}
		if n > 0 {
			v := math.Round(float64(len(roomUsers[name])) / float64(n) * 100)
			c.Participation = &v
		}
		out.Classrooms = append(out.Classrooms, c)
	}
	sort.Slice(out.Classrooms, func(i, j int) bool {
		a, b := out.Classrooms[i].Participation, out.Classrooms[j].Participation
		if a == nil || b == nil {
			return a != nil
		}
		return *a > *b
	})
	return nil
}

// insightsRoute serves GET /api/my/dashboard/insights?days=30|90|365.
func (s *Service) insightsRoute(w http.ResponseWriter, r *http.Request) error {
	days := 90
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil {
		for _, d := range Ranges {
			if n == d {
				days = d
			}
		}
	}
	in, err := s.Insights(r.Context(), auth.MustUser(r.Context()), days)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, in)
	return nil
}
