// Package grading is a classroom's gradebook and attendance (D-122):
// each quiz and tutorial with the weight it carries in the final result,
// every student's mark on each and their weighted total, and who came to
// the classroom's quiz sessions and meetings. Like the dashboard, it reads
// the other modules' tables and writes only its own weights.
package grading

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Item is a quiz or tutorial that can count towards the final result.
type Item struct {
	Kind  string `json:"kind"` // quiz or tutorial
	ID    string `json:"id"`
	Title string `json:"title"`
	Topic string `json:"topic"`
	// Where it sits, so a report can cover one module or topic (D-130); empty for a quiz outside them.
	TopicID  string   `json:"topic_id"`
	ModuleID string   `json:"module_id"`
	Module   string   `json:"module"`
	Weight   *float64 `json:"weight"` // percent of the final result; nil: doesn't count
	Taken    int      `json:"taken"`  // students with a mark
	Avg      *float64 `json:"avg"`    // their average percentage
	// Totals (D-133): the highest and lowest percentage, and the marks it is
	// out of (a quiz drawn from pools can differ per student: the most seen).
	High     *float64 `json:"high"`
	Low      *float64 `json:"low"`
	MaxMarks float64  `json:"max_marks"`
}

// Mark is one student's mark on one item.
type Mark struct {
	Pct    float64  `json:"pct"`
	Points *float64 `json:"points,omitempty"` // pct × weight / 100, when the item counts
	Score  float64  `json:"score"`            // marks gained (D-133)
	Max    float64  `json:"max"`              // out of
}

type Student struct {
	UserID        string          `json:"user_id"`
	Name          string          `json:"name"`
	StudentNumber string          `json:"student_number,omitempty"`
	Marks         map[string]Mark `json:"marks"`   // by item key, kind:id
	Average       *float64        `json:"average"` // plain average of their percentages
	Total         float64         `json:"total"`   // weighted points
	Missing       int             `json:"missing"` // weighted items with no mark (count as 0)
	Final         *float64        `json:"final"`   // total as a percentage of the weights, when any are set
	// Marks gained on every item and what they're out of, a missed item counting its marks (D-133).
	MarksTotal float64 `json:"marks_total"`
	MarksMax   float64 `json:"marks_max"`
}

type Gradebook struct {
	Classroom   string    `json:"classroom"`
	Items       []Item    `json:"items"`
	Students    []Student `json:"students"`
	WeightTotal float64   `json:"weight_total"` // the sum of the weights: what the total is out of
	Sessions    []Session `json:"sessions"`     // the quiz sessions run here, newest first, each with its own report (D-130)
}

// Session is one quiz session run in the classroom.
type Session struct {
	ID       string    `json:"id"`
	QuizID   string    `json:"quiz_id"`
	Title    string    `json:"title"`
	Status   string    `json:"status"`
	At       time.Time `json:"at"`
	Attempts int       `json:"attempts"` // students who started
	Results  int       `json:"results"`  // valid results
	Avg      *float64  `json:"avg"`      // their average percentage
}

// Key names an item in a student's marks.
func Key(kind, id string) string { return kind + ":" + id }

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func (s *Service) teaches(ctx context.Context, teacherID, classroomID string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM content.classrooms WHERE id = $1 AND content.teaches(id, $2)`, classroomID, teacherID).Scan(&name)
	if err == pgx.ErrNoRows {
		return "", httpx.ErrNotFound
	}
	return name, err
}

// Gradebook is the classroom's items, their weights, and every active
// student's marks: a quiz's best valid attempt, a finished tutorial's
// score. A weighted item a student has no mark on counts 0 for them.
func (s *Service) Gradebook(ctx context.Context, teacherID, classroomID string) (Gradebook, error) {
	name, err := s.teaches(ctx, teacherID, classroomID)
	if err != nil {
		return Gradebook{}, err
	}
	g := Gradebook{Classroom: name, Items: []Item{}, Students: []Student{}, Sessions: []Session{}}

	// Quizzes run here (in the order first run), and tutorials with questions.
	rows, err := s.pool.Query(ctx, `
		SELECT 'quiz', q.id::text, q.title, coalesce(tp.name, ''), w.weight::float8, se.first,
			coalesce(tp.id::text, ''), coalesce(m.id::text, ''), coalesce(m.name, '')
		FROM quiz.quizzes q
		JOIN (SELECT quiz_id, min(created_at) AS first FROM live.sessions WHERE classroom_id = $1 GROUP BY quiz_id) se ON se.quiz_id = q.id
		LEFT JOIN content.topics tp ON tp.id = q.topic_id
		LEFT JOIN content.modules m ON m.id = tp.module_id
		LEFT JOIN grading.weights w ON w.classroom_id = $1 AND w.kind = 'quiz' AND w.item_id = q.id
		UNION ALL
		SELECT 'tutorial', t.id::text, t.title, tp.name, w.weight::float8, coalesce(t.published_at, t.created_at),
			tp.id::text, m.id::text, m.name
		FROM tutorial.tutorials t JOIN content.topics tp ON tp.id = t.topic_id JOIN content.modules m ON m.id = tp.module_id
		LEFT JOIN grading.weights w ON w.classroom_id = $1 AND w.kind = 'tutorial' AND w.item_id = t.id
		WHERE m.classroom_id = $1 AND t.status = 'published' AND EXISTS (SELECT 1 FROM quiz.questions x WHERE x.quiz_id = t.quiz_id)
		ORDER BY 6, 2`, classroomID)
	if err != nil {
		return g, err
	}
	for rows.Next() {
		var it Item
		var at time.Time
		if err := rows.Scan(&it.Kind, &it.ID, &it.Title, &it.Topic, &it.Weight, &at, &it.TopicID, &it.ModuleID, &it.Module); err != nil {
			rows.Close()
			return g, err
		}
		if it.Weight != nil {
			g.WeightTotal += *it.Weight
		}
		g.Items = append(g.Items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return g, err
	}
	g.WeightTotal = round(g.WeightTotal, 2)

	// Each student's mark on each: the best valid quiz attempt, a finished tutorial's score out of its marks.
	type raw struct{ pct, score, max float64 }
	marks := map[string]map[string]raw{}
	rows, err = s.pool.Query(ctx, `
		(SELECT DISTINCT ON (r.user_id, se.quiz_id) r.user_id::text, 'quiz:' || se.quiz_id::text, r.pct::float8, r.score::float8, r.max_score::float8
		FROM eval.results r JOIN live.sessions se ON se.id = r.session_id
		WHERE se.classroom_id = $1 AND NOT r.invalidated ORDER BY r.user_id, se.quiz_id, r.pct DESC)
		UNION ALL
		SELECT ru.user_id::text, 'tutorial:' || t.id::text,
			100 * coalesce((SELECT sum(a.score) FROM tutorial.answers a WHERE a.run_id = ru.id), 0)::float8
				/ (SELECT sum(x.marks) FROM quiz.questions x WHERE x.quiz_id = t.quiz_id)::float8,
			coalesce((SELECT sum(a.score) FROM tutorial.answers a WHERE a.run_id = ru.id), 0)::float8,
			(SELECT sum(x.marks) FROM quiz.questions x WHERE x.quiz_id = t.quiz_id)::float8
		FROM tutorial.runs ru JOIN tutorial.tutorials t ON t.id = ru.tutorial_id
		JOIN content.topics tp ON tp.id = t.topic_id JOIN content.modules m ON m.id = tp.module_id
		WHERE m.classroom_id = $1 AND ru.finished_at IS NOT NULL
			AND (SELECT sum(x.marks) FROM quiz.questions x WHERE x.quiz_id = t.quiz_id) > 0`, classroomID)
	if err != nil {
		return g, err
	}
	for rows.Next() {
		var user, key string
		var m raw
		if err := rows.Scan(&user, &key, &m.pct, &m.score, &m.max); err != nil {
			rows.Close()
			return g, err
		}
		if marks[user] == nil {
			marks[user] = map[string]raw{}
		}
		m.pct = round(math.Max(0, math.Min(100, m.pct)), 1)
		m.score, m.max = round(m.score, 2), round(m.max, 2)
		marks[user][key] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return g, err
	}

	// The roster: active students.
	rows, err = s.pool.Query(ctx, `SELECT e.user_id::text, u.name, coalesce(e.student_number, '')
		FROM content.enrolments e JOIN auth.users u ON u.id = e.user_id
		WHERE e.classroom_id = $1 AND e.status = 'active'`, classroomID)
	if err != nil {
		return g, err
	}
	for rows.Next() {
		var st Student
		if err := rows.Scan(&st.UserID, &st.Name, &st.StudentNumber); err != nil {
			rows.Close()
			return g, err
		}
		g.Students = append(g.Students, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return g, err
	}

	sums := map[string]float64{}
	counts := map[string]int{}
	high, low := map[string]float64{}, map[string]float64{}
	// What each item is out of: the marks seen most often (D-133).
	outOf := map[string]map[float64]int{}
	for _, byItem := range marks {
		for k, m := range byItem {
			if outOf[k] == nil {
				outOf[k] = map[float64]int{}
			}
			outOf[k][m.max]++
		}
	}
	maxOf := map[string]float64{}
	for k, seen := range outOf {
		best, n := 0.0, 0
		for v, c := range seen {
			if c > n || (c == n && v > best) {
				best, n = v, c
			}
		}
		maxOf[k] = best
	}
	for i := range g.Students {
		st := &g.Students[i]
		st.Marks = map[string]Mark{}
		var sum float64
		n := 0
		for _, it := range g.Items {
			k := Key(it.Kind, it.ID)
			got, ok := marks[st.UserID][k]
			if !ok {
				if it.Weight != nil {
					st.Missing++
				}
				st.MarksMax += maxOf[k]
				continue
			}
			pct := got.pct
			m := Mark{Pct: pct, Score: got.score, Max: got.max}
			st.MarksTotal += got.score
			st.MarksMax += got.max
			if c := counts[k]; c == 0 || pct > high[k] {
				high[k] = pct
			}
			if c := counts[k]; c == 0 || pct < low[k] {
				low[k] = pct
			}
			if it.Weight != nil {
				p := round(pct**it.Weight/100, 2)
				m.Points = &p
				st.Total += p
			}
			st.Marks[k] = m
			sum += pct
			n++
			sums[k] += pct
			counts[k]++
		}
		st.Total = round(st.Total, 2)
		st.MarksTotal, st.MarksMax = round(st.MarksTotal, 2), round(st.MarksMax, 2)
		if n > 0 {
			a := round(sum/float64(n), 1)
			st.Average = &a
		}
		if g.WeightTotal > 0 {
			f := round(st.Total/g.WeightTotal*100, 1)
			st.Final = &f
		}
	}
	for i := range g.Items {
		k := Key(g.Items[i].Kind, g.Items[i].ID)
		g.Items[i].Taken = counts[k]
		g.Items[i].MaxMarks = maxOf[k]
		if counts[k] > 0 {
			a := round(sums[k]/float64(counts[k]), 1)
			h, l := high[k], low[k]
			g.Items[i].Avg, g.Items[i].High, g.Items[i].Low = &a, &h, &l
		}
	}
	sort.SliceStable(g.Students, func(i, j int) bool { return strings.ToLower(g.Students[i].Name) < strings.ToLower(g.Students[j].Name) })

	// The sessions, for a report of each.
	rows, err = s.pool.Query(ctx, `
		SELECT se.id::text, se.quiz_id::text, se.title, se.status, coalesce(se.started_at, se.created_at),
			(SELECT count(*)::int FROM live.attempts a WHERE a.session_id = se.id AND a.state <> 'waiting' AND a.superseded_at IS NULL),
			(SELECT count(*)::int FROM eval.results r WHERE r.session_id = se.id AND NOT r.invalidated AND r.counts),
			(SELECT round(avg(r.pct), 1)::float8 FROM eval.results r WHERE r.session_id = se.id AND NOT r.invalidated AND r.counts)
		FROM live.sessions se WHERE se.classroom_id = $1
		ORDER BY coalesce(se.started_at, se.created_at) DESC LIMIT 500`, classroomID)
	if err != nil {
		return g, err
	}
	g.Sessions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) {
		var x Session
		return x, r.Scan(&x.ID, &x.QuizID, &x.Title, &x.Status, &x.At, &x.Attempts, &x.Results, &x.Avg)
	})
	return g, err
}

// WeightInput sets one item's weight; 0 (or none) means it doesn't count.
type WeightInput struct {
	Kind   string  `json:"kind"`
	ID     string  `json:"id"`
	Weight float64 `json:"weight"`
}

// SetWeights replaces the classroom's weights. Any teacher of the classroom may.
func (s *Service) SetWeights(ctx context.Context, teacherID, classroomID string, in []WeightInput) (Gradebook, error) {
	if _, err := s.teaches(ctx, teacherID, classroomID); err != nil {
		return Gradebook{}, err
	}
	g, err := s.Gradebook(ctx, teacherID, classroomID)
	if err != nil {
		return g, err
	}
	known := map[string]bool{}
	for _, it := range g.Items {
		known[Key(it.Kind, it.ID)] = true
	}
	bad := map[string]string{}
	var total float64
	for _, w := range in {
		switch {
		case !known[Key(w.Kind, w.ID)]:
			bad["weights"] = "a quiz or tutorial that isn't in this classroom"
		case math.IsNaN(w.Weight) || w.Weight < 0 || w.Weight > 100:
			bad["weights"] = "each weight is between 0 and 100"
		}
		total += w.Weight
	}
	if total > 100.0001 {
		bad["weights"] = "the weights add up to more than 100%"
	}
	if len(bad) > 0 {
		return g, httpx.Invalid(bad)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM grading.weights WHERE classroom_id = $1`, classroomID); err != nil {
			return err
		}
		for _, w := range in {
			if w.Weight == 0 {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO grading.weights (classroom_id, kind, item_id, weight, updated_by) VALUES ($1, $2, $3, $4, $5)`,
				classroomID, w.Kind, w.ID, round(w.Weight, 2), teacherID); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, teacherID, "weights_set", "classroom", classroomID, map[string]any{"weights": in})
	})
	if err != nil {
		return g, err
	}
	return s.Gradebook(ctx, teacherID, classroomID)
}

// ---------- attendance ----------

// Event is a quiz session or a meeting of the classroom.
type Event struct {
	Kind  string    `json:"kind"` // quiz or meeting
	ID    string    `json:"id"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
	// A quiz session's quiz, topic and module, so attendance can cover one of them (D-130).
	QuizID   string `json:"quiz_id,omitempty"`
	TopicID  string `json:"topic_id,omitempty"`
	Topic    string `json:"topic,omitempty"`
	ModuleID string `json:"module_id,omitempty"`
	Module   string `json:"module,omitempty"`
}

type Attendee struct {
	UserID        string             `json:"user_id"`
	Name          string             `json:"name"`
	StudentNumber string             `json:"student_number,omitempty"`
	Attended      []string           `json:"attended"` // event ids
	Minutes       map[string]float64 `json:"minutes"`  // meetings: minutes in
}

type Attendance struct {
	Classroom string     `json:"classroom"`
	From      string     `json:"from"`
	To        string     `json:"to"`
	Events    []Event    `json:"events"`
	Students  []Attendee `json:"students"`
}

// Attendance lists the classroom's quiz sessions and meetings between two
// dates (inclusive), and which each active student came to: an attempt in
// a quiz session, having joined a meeting. Tutoring sessions come from the
// tutoring service; the page adds them.
func (s *Service) Attendance(ctx context.Context, teacherID, classroomID string, from, to time.Time) (Attendance, error) {
	name, err := s.teaches(ctx, teacherID, classroomID)
	if err != nil {
		return Attendance{}, err
	}
	out := Attendance{Classroom: name, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Events: []Event{}, Students: []Attendee{}}
	end := to.AddDate(0, 0, 1)
	rows, err := s.pool.Query(ctx, `
		SELECT 'quiz', s.id::text, s.title, coalesce(s.started_at, s.created_at),
			s.quiz_id::text, coalesce(tp.id::text, ''), coalesce(tp.name, ''), coalesce(md.id::text, ''), coalesce(md.name, '')
		FROM live.sessions s JOIN quiz.quizzes q ON q.id = s.quiz_id
		LEFT JOIN content.topics tp ON tp.id = q.topic_id LEFT JOIN content.modules md ON md.id = tp.module_id
		WHERE s.classroom_id = $1 AND coalesce(s.started_at, s.created_at) >= $2 AND coalesce(s.started_at, s.created_at) < $3
		UNION ALL
		SELECT 'meeting', m.id::text, m.title, m.created_at, '', '', '', '', '' FROM meet.meetings m
		WHERE m.classroom_id = $1 AND m.created_at >= $2 AND m.created_at < $3
		ORDER BY 4, 2`, classroomID, from, end)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Kind, &e.ID, &e.Title, &e.At, &e.QuizID, &e.TopicID, &e.Topic, &e.ModuleID, &e.Module); err != nil {
			rows.Close()
			return out, err
		}
		out.Events = append(out.Events, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	came := map[string][]string{}
	mins := map[string]map[string]float64{}
	rows, err = s.pool.Query(ctx, `
		SELECT a.user_id::text, s.id::text, NULL::float8 FROM live.attempts a JOIN live.sessions s ON s.id = a.session_id
		WHERE s.classroom_id = $1 AND coalesce(s.started_at, s.created_at) >= $2 AND coalesce(s.started_at, s.created_at) < $3
			AND a.state <> 'waiting' AND a.superseded_at IS NULL
		UNION ALL
		SELECT a.user_id::text, m.id::text, extract(epoch FROM a.last_seen - a.first_joined)::float8 / 60
		FROM meet.attendance a JOIN meet.meetings m ON m.id = a.meeting_id
		WHERE m.classroom_id = $1 AND m.created_at >= $2 AND m.created_at < $3`, classroomID, from, end)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var user, event string
		var m *float64
		if err := rows.Scan(&user, &event, &m); err != nil {
			rows.Close()
			return out, err
		}
		came[user] = append(came[user], event)
		if m != nil {
			if mins[user] == nil {
				mins[user] = map[string]float64{}
			}
			mins[user][event] = round(*m, 1)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	rows, err = s.pool.Query(ctx, `SELECT e.user_id::text, u.name, coalesce(e.student_number, '')
		FROM content.enrolments e JOIN auth.users u ON u.id = e.user_id
		WHERE e.classroom_id = $1 AND e.status = 'active' ORDER BY lower(u.name), u.id`, classroomID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Attendee
		if err := rows.Scan(&a.UserID, &a.Name, &a.StudentNumber); err != nil {
			return out, err
		}
		a.Attended = came[a.UserID]
		if a.Attended == nil {
			a.Attended = []string{}
		}
		a.Minutes = mins[a.UserID]
		if a.Minutes == nil {
			a.Minutes = map[string]float64{}
		}
		out.Students = append(out.Students, a)
	}
	return out, rows.Err()
}
