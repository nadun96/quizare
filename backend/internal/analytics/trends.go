package analytics

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
)

// Long-term trends (D-119): month by month, per student in a classroom and
// per classroom, kept in analytics.student_months and classroom_months. A
// job every 15 minutes recomputes only the classroom-months whose results
// or tutorials changed since it last ran (and the current month of every
// open classroom, whose membership may have changed), so the charts read a
// few dozen small rows however many results there are. Like the dashboard,
// it reads the other modules' tables without changing them.

// ---------- the job ----------

type RollupArgs struct{}

func (RollupArgs) Kind() string { return "analytics_rollup" }
func (RollupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueAnalytics, MaxAttempts: 3}
}

type RollupWorker struct {
	river.WorkerDefaults[RollupArgs]
	Service *Service
}

func (w *RollupWorker) Work(ctx context.Context, _ *river.Job[RollupArgs]) error {
	_, err := w.Service.Rollup(ctx)
	return err
}

// RollupPeriodic runs the roll-up every 15 minutes.
func RollupPeriodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(15*time.Minute), func() (river.JobArgs, *river.InsertOpts) { return RollupArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// monthOf is the UTC calendar month a time falls in.
func monthOf(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Rollup recomputes the classroom-months that changed since the last run;
// the first run computes every month there is. It returns how many
// classroom-months it recomputed.
func (s *Service) Rollup(ctx context.Context) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// One run at a time: a second waits here, then finds nothing new. The
	// first run starts from 1970, so it computes every month there is.
	if _, err := tx.Exec(ctx, `INSERT INTO analytics.rollup_state(last_at) VALUES ('1970-01-01Z') ON CONFLICT DO NOTHING`); err != nil {
		return 0, err
	}
	var since, started time.Time
	if err := tx.QueryRow(ctx, `SELECT last_at, now() FROM analytics.rollup_state FOR UPDATE`).Scan(&since, &started); err != nil {
		return 0, err
	}
	// A minute of overlap: a result written while the last run read is caught now.
	from := since.Add(-time.Minute)
	current := monthOf(started)

	rows, err := tx.Query(ctx, `
		SELECT s.classroom_id, date_trunc('month', coalesce(a.submitted_at, s.ended_at, a.created_at) AT TIME ZONE 'UTC')::date
		FROM eval.results r JOIN live.attempts a ON a.id = r.attempt_id JOIN live.sessions s ON s.id = r.session_id
		WHERE r.updated_at >= $1
		UNION
		SELECT m.classroom_id, date_trunc('month', ru.finished_at AT TIME ZONE 'UTC')::date
		FROM tutorial.runs ru JOIN tutorial.tutorials tu ON tu.id = ru.tutorial_id
		JOIN content.topics tp ON tp.id = tu.topic_id JOIN content.modules m ON m.id = tp.module_id
		WHERE ru.finished_at IS NOT NULL AND ru.updated_at >= $1
		UNION
		SELECT c.id, $2::date FROM content.classrooms c WHERE c.archived_at IS NULL`, from, current)
	if err != nil {
		return 0, err
	}
	var rooms []string
	var months []time.Time
	for rows.Next() {
		var c string
		var m time.Time
		if err := rows.Scan(&c, &m); err != nil {
			rows.Close()
			return 0, err
		}
		rooms, months = append(rooms, c), append(months, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(rooms) > 0 {
		if err := s.rollupPairs(ctx, tx, rooms, months, current, started); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE analytics.rollup_state SET last_at = $1`, started); err != nil {
		return 0, err
	}
	return len(rooms), tx.Commit(ctx)
}

// rollupPairs recomputes the given classroom-months in a few set-based statements.
func (s *Service) rollupPairs(ctx context.Context, tx pgx.Tx, rooms []string, months []time.Time, current, now time.Time) error {
	// Each student's month: quiz results and finished tutorials, by the day they happened.
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE rollup_pairs ON COMMIT DROP AS
		SELECT * FROM unnest($1::uuid[], $2::date[]) AS p(classroom_id, month)`, rooms, months); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE rollup_events ON COMMIT DROP AS
		SELECT p.classroom_id, p.month, r.user_id, (t.at AT TIME ZONE 'UTC')::date AS day, s.id AS session_id, r.pct::float8 AS pct, r.passed, false AS tutorial
		FROM rollup_pairs p
		JOIN live.sessions s ON s.classroom_id = p.classroom_id
		JOIN eval.results r ON r.session_id = s.id
		JOIN live.attempts a ON a.id = r.attempt_id
		CROSS JOIN LATERAL (SELECT coalesce(a.submitted_at, s.ended_at, a.created_at) AS at) t
		WHERE r.complete AND NOT r.invalidated
		  AND t.at >= p.month::timestamp AT TIME ZONE 'UTC' AND t.at < (p.month + interval '1 month')::timestamp AT TIME ZONE 'UTC'
		UNION ALL
		SELECT p.classroom_id, p.month, ru.user_id, (ru.finished_at AT TIME ZONE 'UTC')::date, NULL, NULL, NULL, true
		FROM rollup_pairs p
		JOIN content.modules m ON m.classroom_id = p.classroom_id
		JOIN content.topics tp ON tp.module_id = m.id
		JOIN tutorial.tutorials tu ON tu.topic_id = tp.id
		JOIN tutorial.runs ru ON ru.tutorial_id = tu.id
		WHERE ru.finished_at >= p.month::timestamp AT TIME ZONE 'UTC' AND ru.finished_at < (p.month + interval '1 month')::timestamp AT TIME ZONE 'UTC'`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM analytics.student_months sm USING rollup_pairs p WHERE sm.classroom_id = p.classroom_id AND sm.month = p.month`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO analytics.student_months (classroom_id, month, user_id, quizzes, pct_sum, passed, tutorials, active_days)
		SELECT classroom_id, month, user_id,
			count(*) FILTER (WHERE NOT tutorial), coalesce(sum(pct), 0), count(*) FILTER (WHERE passed),
			count(*) FILTER (WHERE tutorial), count(DISTINCT day)
		FROM rollup_events GROUP BY classroom_id, month, user_id`); err != nil {
		return err
	}
	// Each classroom's month. Members are counted while the month runs and
	// kept once it is over; a month first computed later counts those who had
	// joined by its end and are, or were, members, and never fewer than took part.
	_, err := tx.Exec(ctx, `
		WITH agg AS (
			SELECT p.classroom_id, p.month,
				(SELECT count(*) FROM content.enrolments e WHERE e.classroom_id = p.classroom_id
					AND CASE WHEN p.month = $1 THEN e.status = 'active'
					ELSE e.status IN ('active', 'removed') AND e.created_at < (p.month + interval '1 month')::timestamp AT TIME ZONE 'UTC' END) AS members,
				count(DISTINCT ev.user_id) FILTER (WHERE e.user_id IS NOT NULL) AS active,
				count(DISTINCT ev.session_id) AS sessions,
				count(*) FILTER (WHERE ev.tutorial = false) AS results,
				coalesce(sum(ev.pct), 0) AS pct_sum,
				count(*) FILTER (WHERE ev.passed) AS passed,
				count(*) FILTER (WHERE ev.tutorial) AS tutorials
			FROM rollup_pairs p
			LEFT JOIN rollup_events ev ON ev.classroom_id = p.classroom_id AND ev.month = p.month
			LEFT JOIN content.enrolments e ON e.classroom_id = ev.classroom_id AND e.user_id = ev.user_id
			GROUP BY p.classroom_id, p.month)
		INSERT INTO analytics.classroom_months (classroom_id, month, enrolled, active, sessions, results, pct_sum, passed, tutorials, computed_at)
		SELECT classroom_id, month, CASE WHEN month = $1 THEN members ELSE greatest(members, active) END, active, sessions, results, pct_sum, passed, tutorials, $2
		FROM agg
		ON CONFLICT (classroom_id, month) DO UPDATE SET
			enrolled = CASE WHEN EXCLUDED.month = $1 THEN EXCLUDED.enrolled ELSE greatest(analytics.classroom_months.enrolled, EXCLUDED.active) END,
			active = EXCLUDED.active, sessions = EXCLUDED.sessions, results = EXCLUDED.results, pct_sum = EXCLUDED.pct_sum,
			passed = EXCLUDED.passed, tutorials = EXCLUDED.tutorials, computed_at = EXCLUDED.computed_at`, current, now)
	return err
}

// ---------- reading ----------

// TrendMonth is one month of a classroom, or of a student in it.
type TrendMonth struct {
	Month     string   `json:"month"` // 2026-03
	Enrolled  int      `json:"enrolled,omitempty"`
	Active    int      `json:"active"`              // classroom: members taking part; student: days with activity
	Taking    *float64 `json:"taking_part"`         // classroom: % of members taking part
	Sessions  int      `json:"sessions,omitempty"`  // classroom: quiz sessions
	Quizzes   int      `json:"quizzes"`             // results
	Avg       *float64 `json:"avg"`                 // average result %
	PassRate  *float64 `json:"pass_rate"`           // % of results passed
	Tutorials int      `json:"tutorials"`           // finished
	ClassAvg  *float64 `json:"class_avg,omitempty"` // student: the classroom's average that month
}

// TrendStudent is one student's line in a classroom's trends.
type TrendStudent struct {
	UserID        string   `json:"user_id"`
	Name          string   `json:"name"`
	StudentNumber string   `json:"student_number,omitempty"`
	Quizzes       int      `json:"quizzes"`
	Avg           *float64 `json:"avg"`
	Tutorials     int      `json:"tutorials"`
	ActiveMonths  int      `json:"active_months"`
	Recent        *float64 `json:"recent_avg"`  // the last three months
	Earlier       *float64 `json:"earlier_avg"` // the three before
	LastActive    string   `json:"last_active,omitempty"`
}

type ClassroomTrends struct {
	Classroom string         `json:"classroom"`
	Months    []TrendMonth   `json:"months"`
	Students  []TrendStudent `json:"students"`
	Updated   *time.Time     `json:"updated_at"`
}

type StudentTrends struct {
	Name      string       `json:"name"`
	Classroom string       `json:"classroom,omitempty"`
	Months    []TrendMonth `json:"months"`
	Updated   *time.Time   `json:"updated_at"`
}

// TrendRanges are the spans a trends view can show, in months.
var TrendRanges = []int{12, 24, 60}

func pct(sum float64, n int) *float64 {
	if n == 0 {
		return nil
	}
	v := math.Round(sum/float64(n)*10) / 10
	return &v
}

func share(part, whole int) *float64 {
	if whole == 0 {
		return nil
	}
	v := math.Round(math.Min(100, float64(part)/float64(whole)*100))
	return &v
}

// span lists the months shown, oldest first, ending with the current one.
func span(now time.Time, months int) []time.Time {
	end := monthOf(now)
	out := make([]time.Time, months)
	for i := range out {
		out[i] = end.AddDate(0, i-months+1, 0)
	}
	return out
}

func (s *Service) ownClassroom(ctx context.Context, teacherID, classroomID string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM content.classrooms WHERE id = $1 AND content.teaches(id, $2)`, classroomID, teacherID).Scan(&name)
	if err == pgx.ErrNoRows {
		return "", httpx.ErrNotFound
	}
	return name, err
}

// ClassroomTrends is a classroom's last months, and each student's figures over them.
func (s *Service) ClassroomTrends(ctx context.Context, teacherID, classroomID string, months int) (ClassroomTrends, error) {
	name, err := s.ownClassroom(ctx, teacherID, classroomID)
	if err != nil {
		return ClassroomTrends{}, err
	}
	out := ClassroomTrends{Classroom: name, Months: []TrendMonth{}, Students: []TrendStudent{}}
	ms := span(s.now(), months)
	byMonth := map[string]*TrendMonth{}
	for _, m := range ms {
		out.Months = append(out.Months, TrendMonth{Month: m.Format("2006-01")})
	}
	for i := range out.Months {
		byMonth[out.Months[i].Month] = &out.Months[i]
	}
	rows, err := s.pool.Query(ctx, `SELECT month, enrolled, active, sessions, results, pct_sum, passed, tutorials, computed_at
		FROM analytics.classroom_months WHERE classroom_id = $1 AND month >= $2`, classroomID, ms[0])
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var m, at time.Time
		var x TrendMonth
		var sum float64
		var passed int
		if err := rows.Scan(&m, &x.Enrolled, &x.Active, &x.Sessions, &x.Quizzes, &sum, &passed, &x.Tutorials, &at); err != nil {
			rows.Close()
			return out, err
		}
		x.Month = m.Format("2006-01")
		x.Avg, x.PassRate, x.Taking = pct(sum, x.Quizzes), share(passed, x.Quizzes), share(x.Active, x.Enrolled)
		if p := byMonth[x.Month]; p != nil {
			*p = x
		}
		if out.Updated == nil || at.After(*out.Updated) {
			a := at
			out.Updated = &a
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	// Students: everyone who is a member now, and anyone with activity in the span.
	recentFrom := ms[len(ms)-1].AddDate(0, -2, 0)
	earlierFrom := recentFrom.AddDate(0, -3, 0)
	rows, err = s.pool.Query(ctx, `
		WITH acts AS (
			SELECT user_id, sum(quizzes)::int q, sum(pct_sum) ps, sum(tutorials)::int tu, count(*) FILTER (WHERE quizzes + tutorials > 0)::int am, max(month) last,
				sum(quizzes) FILTER (WHERE month >= $3)::int rq, sum(pct_sum) FILTER (WHERE month >= $3) rps,
				sum(quizzes) FILTER (WHERE month >= $4 AND month < $3)::int eq, sum(pct_sum) FILTER (WHERE month >= $4 AND month < $3) eps
			FROM analytics.student_months WHERE classroom_id = $1 AND month >= $2 GROUP BY user_id)
		SELECT u.user_id, coalesce(e.student_number, ''), coalesce(a.q, 0), coalesce(a.ps, 0), coalesce(a.tu, 0), coalesce(a.am, 0), a.last,
			coalesce(a.rq, 0), coalesce(a.rps, 0), coalesce(a.eq, 0), coalesce(a.eps, 0)
		FROM (SELECT user_id FROM content.enrolments WHERE classroom_id = $1 AND status = 'active' UNION SELECT user_id FROM acts) u
		LEFT JOIN acts a ON a.user_id = u.user_id
		LEFT JOIN content.enrolments e ON e.classroom_id = $1 AND e.user_id = u.user_id`, classroomID, ms[0], recentFrom, earlierFrom)
	if err != nil {
		return out, err
	}
	var ids []string
	for rows.Next() {
		var st TrendStudent
		var sum, rsum, esum float64
		var rq, eq int
		var last *time.Time
		if err := rows.Scan(&st.UserID, &st.StudentNumber, &st.Quizzes, &sum, &st.Tutorials, &st.ActiveMonths, &last, &rq, &rsum, &eq, &esum); err != nil {
			rows.Close()
			return out, err
		}
		st.Avg, st.Recent, st.Earlier = pct(sum, st.Quizzes), pct(rsum, rq), pct(esum, eq)
		if last != nil {
			st.LastActive = last.Format("2006-01")
		}
		out.Students = append(out.Students, st)
		ids = append(ids, st.UserID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	users, err := s.users.UsersByID(ctx, ids)
	if err != nil {
		return out, err
	}
	for i := range out.Students {
		out.Students[i].Name = users[out.Students[i].UserID].Name
	}
	sort.SliceStable(out.Students, func(i, j int) bool { return out.Students[i].Name < out.Students[j].Name })
	return out, nil
}

// studentMonths is one student's months, in one classroom or across all of theirs.
func (s *Service) studentMonths(ctx context.Context, userID, classroomID string, ms []time.Time) ([]TrendMonth, *time.Time, error) {
	out := make([]TrendMonth, len(ms))
	idx := map[string]int{}
	for i, m := range ms {
		out[i] = TrendMonth{Month: m.Format("2006-01")}
		idx[out[i].Month] = i
	}
	rows, err := s.pool.Query(ctx, `
		SELECT sm.month, sum(sm.quizzes)::int, sum(sm.pct_sum), sum(sm.passed)::int, sum(sm.tutorials)::int, sum(sm.active_days)::int,
			sum(cm.pct_sum), sum(cm.results)::int, max(cm.computed_at)
		FROM analytics.student_months sm
		LEFT JOIN analytics.classroom_months cm ON cm.classroom_id = sm.classroom_id AND cm.month = sm.month
		WHERE sm.user_id = $1 AND sm.month >= $2 AND ($3 = '' OR sm.classroom_id::text = $3)
		GROUP BY sm.month`, userID, ms[0], classroomID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var updated *time.Time
	for rows.Next() {
		var m time.Time
		var x TrendMonth
		var sum float64
		var passed int
		var csum *float64
		var cn *int
		var at *time.Time
		if err := rows.Scan(&m, &x.Quizzes, &sum, &passed, &x.Tutorials, &x.Active, &csum, &cn, &at); err != nil {
			return nil, nil, err
		}
		x.Month = m.Format("2006-01")
		x.Avg, x.PassRate = pct(sum, x.Quizzes), share(passed, x.Quizzes)
		if csum != nil && cn != nil {
			x.ClassAvg = pct(*csum, *cn)
		}
		if i, ok := idx[x.Month]; ok {
			out[i] = x
		}
		if at != nil && (updated == nil || at.After(*updated)) {
			updated = at
		}
	}
	return out, updated, rows.Err()
}

// StudentTrends is one student's months in a teacher's classroom.
func (s *Service) StudentTrends(ctx context.Context, teacherID, classroomID, userID string, months int) (StudentTrends, error) {
	name, err := s.ownClassroom(ctx, teacherID, classroomID)
	if err != nil {
		return StudentTrends{}, err
	}
	var member bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM content.enrolments WHERE classroom_id = $1 AND user_id = $2)
		OR EXISTS (SELECT 1 FROM analytics.student_months WHERE classroom_id = $1 AND user_id = $2)`, classroomID, userID).Scan(&member); err != nil {
		return StudentTrends{}, err
	}
	if !member {
		return StudentTrends{}, httpx.ErrNotFound
	}
	ms, upd, err := s.studentMonths(ctx, userID, classroomID, span(s.now(), months))
	if err != nil {
		return StudentTrends{}, err
	}
	users, err := s.users.UsersByID(ctx, []string{userID})
	if err != nil {
		return StudentTrends{}, err
	}
	return StudentTrends{Name: users[userID].Name, Classroom: name, Months: ms, Updated: upd}, nil
}

// MyTrends is a student's own months across all their classrooms.
func (s *Service) MyTrends(ctx context.Context, u auth.User, months int) (StudentTrends, error) {
	ms, upd, err := s.studentMonths(ctx, u.ID, "", span(s.now(), months))
	if err != nil {
		return StudentTrends{}, err
	}
	return StudentTrends{Name: u.Name, Months: ms, Updated: upd}, nil
}

func monthsParam(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("months"))
	for _, m := range TrendRanges {
		if n == m {
			return m
		}
	}
	return 12
}

// TrendRoutes mounts the classroom trends under /api/teacher.
func (s *Service) TrendRoutes(r chi.Router) {
	r.Method("GET", "/classrooms/{id}/trends", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.ClassroomTrends(r.Context(), uid(r), chi.URLParam(r, "id"), monthsParam(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, out)
		return nil
	}))
	r.Method("GET", "/classrooms/{id}/students/{user}/trends", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.StudentTrends(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "user"), monthsParam(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, out)
		return nil
	}))
}

// MyTrendRoutes mounts GET /api/my/trends for students.
func (s *Service) MyTrendRoutes(r chi.Router) {
	r.Method("GET", "/my/trends", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.MyTrends(r.Context(), auth.MustUser(r.Context()), monthsParam(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, out)
		return nil
	}))
}
