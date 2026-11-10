package grading

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// The class report (D-140): the teacher chooses which quizzes and tutorials
// are in it, each one's weight (0: shown but not in the total) and whether it
// uses a student's highest or lowest mark. The same setup says what students
// see of each item's results, and for how long.

const (
	PickHigh = "high"
	PickLow  = "low"

	DetailMarks       = "marks"       // the score only
	DetailCorrectness = "correctness" // and whether each answer was right
	DetailAnswers     = "answers"     // and the correct answers

	maxReportWeight  = 1 // a fraction: 0.4 counts a mark's percentage × 0.4 (D-142)
	maxReviewMinutes = 366 * 24 * 60
)

// Access is what one student may see of one item's results. An empty Detail
// leaves it to the item's own settings; 0 minutes is no time limit.
type Access struct {
	Detail        string
	ReviewMinutes int
}

// Review is what a student's result page may show now.
type Review struct {
	Detail string     `json:"detail,omitempty"` // empty: the item's own settings
	Until  *time.Time `json:"until,omitempty"`  // when question detail closes
	Closed bool       `json:"closed,omitempty"` // it has: the score only, for good
}

// At works out the review at now, for results released to the student at since.
func (a Access) At(since *time.Time, now time.Time) Review {
	r := Review{Detail: a.Detail}
	if a.ReviewMinutes > 0 && since != nil {
		until := since.Add(time.Duration(a.ReviewMinutes) * time.Minute)
		r.Until = &until
		if now.After(until) {
			r.Closed = true
			r.Detail = DetailMarks
		}
	}
	return r
}

// ResultAccess is the item's level and review time for this student, with
// their exception if they have one.
func (s *Service) ResultAccess(ctx context.Context, kind, itemID, userID string) (Access, error) {
	var a Access
	err := s.pool.QueryRow(ctx, `SELECT detail, review_minutes FROM grading.report_items WHERE kind = $1 AND item_id = $2 LIMIT 1`, kind, itemID).
		Scan(&a.Detail, &a.ReviewMinutes)
	if err != nil && err != pgx.ErrNoRows {
		return a, err
	}
	var detail string
	var minutes *int
	err = s.pool.QueryRow(ctx, `SELECT detail, review_minutes FROM grading.result_exceptions WHERE kind = $1 AND item_id = $2 AND user_id = $3 LIMIT 1`, kind, itemID, userID).
		Scan(&detail, &minutes)
	if err == pgx.ErrNoRows {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	if detail != "" {
		a.Detail = detail
	}
	if minutes != nil {
		a.ReviewMinutes = *minutes
	}
	return a, nil
}

// ReportItem is a quiz or tutorial with its setup, and how the class did on it.
type ReportItem struct {
	Kind          string    `json:"kind"`
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Topic         string    `json:"topic"`
	Module        string    `json:"module"`
	Included      bool      `json:"included"`
	Weight        float64   `json:"weight"`
	Pick          string    `json:"pick"`
	Detail        string    `json:"detail"`
	ReviewMinutes int       `json:"review_minutes"`
	GroupRule     string    `json:"group_rule"` // how a group's mark comes from its members' (D-141)
	Taken         int       `json:"taken"`
	Avg           *float64  `json:"avg"`
	High          *float64  `json:"high"`
	Low           *float64  `json:"low"`
	MaxMarks      float64   `json:"max_marks"`
	Date          time.Time `json:"date"` // a quiz's first session, a tutorial's publishing, a poll's opening
	TopicID       string    `json:"topic_id"`
	ModuleID      string    `json:"module_id"`
}

// ReportOptions says what else the report shows (D-156).
type ReportOptions struct {
	Violations bool `json:"violations"`
	Attendance bool `json:"attendance"`
	Dates      bool `json:"dates"`
	// A report of part of the classroom lists only those who took part (D-163).
	AttendedOnly bool `json:"attended_only"`
}

// ReportMark is the mark the report uses for one student on one item.
type ReportMark struct {
	Score       float64  `json:"score"`
	Max         float64  `json:"max"`
	Pct         float64  `json:"pct"`
	Points      *float64 `json:"points,omitempty"` // the final mark: marks ÷ out of × 100 × weight, when it counts
	exact       float64  // the percentage before rounding for showing: final marks use this
	Submissions int      `json:"submissions"` // valid ones to choose from
	Pending     bool     `json:"pending,omitempty"`
}

type ReportStudent struct {
	UserID        string                `json:"user_id"`
	Name          string                `json:"name"`
	StudentNumber string                `json:"student_number,omitempty"`
	Marks         map[string]ReportMark `json:"marks"`
	Total         float64               `json:"total"`   // weighted points
	Missing       int                   `json:"missing"` // weighted items with no mark, which count 0
	Final         *float64              `json:"final"`   // the total as a percentage of the weights
	// Violations in each quiz in the report, and in all, counting every attempt
	// of every session: an attempt thrown out for them matters most (D-156).
	Violations      map[string]int `json:"violations,omitempty"`
	ViolationsTotal int            `json:"violations_total"`
}

// ResultException is one student's different level or review time on an item.
type ResultException struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	UserID        string `json:"user_id"`
	Detail        string `json:"detail"`
	ReviewMinutes *int   `json:"review_minutes"`
}

type ClassReport struct {
	Classroom   string            `json:"classroom"`
	Items       []ReportItem      `json:"items"`        // every item; those included are the report's columns
	Students    []ReportStudent   `json:"students"`     // marks on included items only
	WeightTotal float64           `json:"weight_total"` // the weights added up
	OutOf       float64           `json:"out_of"`       // what the total is out of: 100 × that
	Exceptions  []ResultException `json:"exceptions"`
	ClassGroups []GroupRow        `json:"class_groups"` // the whole class's groups (D-141)
	ItemGroups  []ItemGroups      `json:"item_groups"`  // items with groups of their own
	Options     ReportOptions     `json:"options"`
	Scope       ReportScope       `json:"scope"` // what it covers (D-160)
	// Only the students who took part are listed (D-163).
	AttendedOnly bool            `json:"attended_only"`
	Sessions     []ReportSession `json:"sessions"` // the classroom's quiz sessions, to report on one
}

// submission is one valid mark a student has on an item.
type submission struct {
	score, max, pct float64
	complete        bool
}

// exactPct is the mark as a percentage from its marks, within 0 to 100.
func (x submission) exactPct() float64 {
	p := x.pct
	if x.max > 0 {
		p = x.score / x.max * 100
	}
	return min(100, max(0, p))
}

// pickMark chooses the submission the report uses. One still being marked is
// used only when there's no other: its mark isn't final, so it would look like
// the lowest.
func pickMark(list []submission, pick string) submission {
	var best submission
	found := false
	for _, x := range list {
		switch {
		case !found, x.complete && !best.complete:
			best, found = x, true
		case x.complete != best.complete:
		case pick == PickLow && x.pct < best.pct, pick != PickLow && x.pct > best.pct:
			best = x
		}
	}
	return best
}

// loadReportExtras fills in the options and each student's violations.
func (s *Service) loadReportExtras(ctx context.Context, classroomID, session string, rep *ClassReport) error {
	err := s.pool.QueryRow(ctx, `SELECT show_violations, show_attendance, show_dates, attended_only FROM grading.report_options WHERE classroom_id = $1`, classroomID).
		Scan(&rep.Options.Violations, &rep.Options.Attendance, &rep.Options.Dates, &rep.Options.AttendedOnly)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	included := map[string]bool{}
	for _, it := range rep.Items {
		included[Key(it.Kind, it.ID)] = it.Included
	}
	byUser := map[string]*ReportStudent{}
	for i := range rep.Students {
		byUser[rep.Students[i].UserID] = &rep.Students[i]
	}
	rows, err := s.pool.Query(ctx, `SELECT a.user_id::text, 'quiz:' || se.quiz_id::text, sum(a.violations)::int
		FROM live.attempts a JOIN live.sessions se ON se.id = a.session_id
		WHERE se.classroom_id = $1 AND a.violations > 0 AND ($2 = '' OR se.id::text = $2)
		GROUP BY a.user_id, se.quiz_id`, classroomID, session)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var user, key string
		var n int
		if err := rows.Scan(&user, &key, &n); err != nil {
			return err
		}
		st := byUser[user]
		if st == nil || !included[key] {
			continue
		}
		if st.Violations == nil {
			st.Violations = map[string]int{}
		}
		st.Violations[key] = n
		st.ViolationsTotal += n
	}
	return rows.Err()
}

// ClassReport builds the report from the current setup and marks.
func (s *Service) ClassReport(ctx context.Context, teacherID, classroomID string) (ClassReport, error) {
	return s.ScopedReport(ctx, teacherID, classroomID, "", "")
}

// ScopedReport is the report of part of the classroom (D-160): a module, a
// topic, one item, or one quiz session; "" is the whole classroom.
//
// attended "1" or "0" says whether to list only the students who took part,
// for this report alone; "" follows the report's option (D-163).
func (s *Service) ScopedReport(ctx context.Context, teacherID, classroomID, scope, attended string) (ClassReport, error) {
	name, err := s.teaches(ctx, teacherID, classroomID)
	if err != nil {
		return ClassReport{}, err
	}
	rep := ClassReport{Classroom: name}

	// Weights start from the gradebook's, until the report sets its own.
	rows, err := s.pool.Query(ctx, `
		WITH items AS (
			SELECT 'quiz' AS kind, q.id, q.title, coalesce(tp.name, '') AS topic, coalesce(m.name, '') AS module, se.first AS at,
				coalesce(tp.id::text, '') AS topic_id, coalesce(m.id::text, '') AS module_id
			FROM quiz.quizzes q
			JOIN (SELECT quiz_id, min(created_at) AS first FROM live.sessions WHERE classroom_id = $1 GROUP BY quiz_id) se ON se.quiz_id = q.id
			LEFT JOIN content.topics tp ON tp.id = q.topic_id
			LEFT JOIN content.modules m ON m.id = tp.module_id
			UNION ALL
			SELECT 'tutorial', t.id, t.title, tp.name, m.name, coalesce(t.published_at, t.created_at), tp.id::text, m.id::text
			FROM tutorial.tutorials t JOIN content.topics tp ON tp.id = t.topic_id JOIN content.modules m ON m.id = tp.module_id
			WHERE m.classroom_id = $1 AND t.status = 'published' AND EXISTS (SELECT 1 FROM quiz.questions x WHERE x.quiz_id = t.quiz_id)
			UNION ALL
			SELECT 'poll', p.id, p.title, coalesce(ptp.name, ''), coalesce(pm.name, ''), coalesce(p.opened_at, p.created_at),
				coalesce(ptp.id::text, ''), coalesce(pm.id::text, '')
			FROM poll.polls p
			LEFT JOIN content.topics ptp ON ptp.id = p.topic_id LEFT JOIN content.modules pm ON pm.id = ptp.module_id
			WHERE p.classroom_id = $1 AND p.scoring AND p.identity <> 'anonymous' AND p.status <> 'draft'
				AND EXISTS (SELECT 1 FROM poll.questions x WHERE x.poll_id = p.id AND x.key IS NOT NULL AND x.points > 0)
			UNION ALL
			SELECT 'offline', a.id, a.title, tp.name, m.name, coalesce(a.due_at, a.created_at), tp.id::text, m.id::text
			FROM grading.offline_assignments a JOIN content.topics tp ON tp.id = a.topic_id JOIN content.modules m ON m.id = tp.module_id
			WHERE a.classroom_id = $1
			UNION ALL
			-- A poll's presentations, once one is marked (D-165).
			SELECT 'presentation', p.id, p.title, coalesce(ptp.name, ''), coalesce(pm.name, ''), coalesce(p.opened_at, p.created_at),
				coalesce(ptp.id::text, ''), coalesce(pm.id::text, '')
			FROM poll.polls p JOIN poll.pres_settings ps ON ps.poll_id = p.id AND ps.mode <> 'off' AND ps.out_of > 0
			LEFT JOIN content.topics ptp ON ptp.id = p.topic_id LEFT JOIN content.modules pm ON pm.id = ptp.module_id
			WHERE p.classroom_id = $1 AND EXISTS (SELECT 1 FROM poll.submissions x WHERE x.poll_id = p.id AND x.mark IS NOT NULL)
		)
		SELECT i.kind, i.id::text, i.title, i.topic, i.module, coalesce(ri.included, true),
			coalesce(ri.weight, w.weight / 100, 0)::float8, coalesce(ri.pick, 'high'), coalesce(ri.detail, ''), coalesce(ri.review_minutes, 0), coalesce(ri.group_rule, 'average'), i.at,
			i.topic_id, i.module_id
		FROM items i
		LEFT JOIN grading.report_items ri ON ri.classroom_id = $1 AND ri.kind = i.kind AND ri.item_id = i.id
		LEFT JOIN grading.weights w ON w.classroom_id = $1 AND w.kind = i.kind AND w.item_id = i.id
		ORDER BY i.at, i.id`, classroomID)
	if err != nil {
		return rep, err
	}
	if rep.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReportItem, error) {
		var it ReportItem
		return it, r.Scan(&it.Kind, &it.ID, &it.Title, &it.Topic, &it.Module, &it.Included, &it.Weight, &it.Pick, &it.Detail, &it.ReviewMinutes, &it.GroupRule, &it.Date, &it.TopicID, &it.ModuleID)
	}); err != nil {
		return rep, err
	}
	if err := s.scopeReport(ctx, classroomID, scope, &rep); err != nil {
		return rep, err
	}

	// Every valid submission: all tries of all sessions, finished tutorials, and
	// what a signed-in student scored on a poll.
	subs := map[string]map[string][]submission{}
	rows, err = s.pool.Query(ctx, `
		SELECT r.user_id::text, 'quiz:' || se.quiz_id::text, r.score::float8, r.max_score::float8, r.pct::float8, r.complete
		FROM eval.results r JOIN live.sessions se ON se.id = r.session_id
		WHERE se.classroom_id = $1 AND NOT r.invalidated AND ($2 = '' OR se.id::text = $2)
		UNION ALL
		SELECT ru.user_id::text, 'tutorial:' || t.id::text,
			coalesce((SELECT sum(a.score) FROM tutorial.answers a WHERE a.run_id = ru.id), 0)::float8, mx.marks::float8,
			100 * coalesce((SELECT sum(a.score) FROM tutorial.answers a WHERE a.run_id = ru.id), 0)::float8 / mx.marks::float8,
			NOT EXISTS (SELECT 1 FROM tutorial.answers a WHERE a.run_id = ru.id AND a.status = 'pending')
		FROM tutorial.runs ru JOIN tutorial.tutorials t ON t.id = ru.tutorial_id
		JOIN content.topics tp ON tp.id = t.topic_id JOIN content.modules m ON m.id = tp.module_id
		CROSS JOIN LATERAL (SELECT sum(x.marks) AS marks FROM quiz.questions x WHERE x.quiz_id = t.quiz_id) mx
		WHERE m.classroom_id = $1 AND ru.finished_at IS NOT NULL AND mx.marks > 0
		UNION ALL
		SELECT pa.user_id::text, 'poll:' || p.id::text, coalesce(sum(r.score) FILTER (WHERE NOT r.hidden), 0)::float8, mx.points::float8,
			100 * coalesce(sum(r.score) FILTER (WHERE NOT r.hidden), 0)::float8 / mx.points::float8, true
		FROM poll.polls p
		JOIN poll.participants pa ON pa.poll_id = p.id AND pa.user_id IS NOT NULL
		JOIN poll.responses r ON r.participant_id = pa.id
		CROSS JOIN LATERAL (SELECT sum(x.points) AS points FROM poll.questions x WHERE x.poll_id = p.id AND x.key IS NOT NULL) mx
		WHERE p.classroom_id = $1 AND p.scoring AND p.identity <> 'anonymous' AND p.status <> 'draft' AND mx.points > 0
		GROUP BY pa.user_id, p.id, mx.points
		UNION ALL
		SELECT o.user_id, 'offline:' || o.assignment_id, o.score, o.max_marks, 100 * o.score / o.max_marks, true
		FROM (`+offlineMarksSQL+`) o
		UNION ALL
		SELECT m.user_id, 'presentation:' || x.poll_id::text, x.mark::float8, ps.out_of::float8, 100 * x.mark::float8 / ps.out_of::float8, true
		FROM poll.submissions x JOIN poll.pres_settings ps ON ps.poll_id = x.poll_id JOIN poll.polls p ON p.id = x.poll_id
		CROSS JOIN LATERAL (
			SELECT pa.user_id::text AS user_id FROM poll.participants pa WHERE x.owner_kind = 'student' AND pa.id = x.owner_id AND pa.user_id IS NOT NULL
			UNION SELECT pa.user_id::text FROM poll.participants pa WHERE x.owner_kind = 'poll_group' AND pa.group_id = x.owner_id AND pa.user_id IS NOT NULL
			UNION SELECT e.user_id::text FROM content.enrolment_categories ec JOIN content.enrolments e ON e.id = ec.enrolment_id
				WHERE x.owner_kind = 'class_group' AND ec.category_id = x.owner_id) m
		WHERE p.classroom_id = $1 AND x.mark IS NOT NULL AND ps.out_of > 0`, classroomID, rep.Scope.SessionID())
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var user, key string
		var x submission
		if err := rows.Scan(&user, &key, &x.score, &x.max, &x.pct, &x.complete); err != nil {
			rows.Close()
			return rep, err
		}
		if subs[user] == nil {
			subs[user] = map[string][]submission{}
		}
		subs[user][key] = append(subs[user][key], x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}

	rows, err = s.pool.Query(ctx, `SELECT e.user_id::text, u.name, coalesce(e.student_number, '')
		FROM content.enrolments e JOIN auth.users u ON u.id = e.user_id
		WHERE e.classroom_id = $1 AND e.status = 'active'`, classroomID)
	if err != nil {
		return rep, err
	}
	if rep.Students, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReportStudent, error) {
		st := ReportStudent{Marks: map[string]ReportMark{}}
		return st, r.Scan(&st.UserID, &st.Name, &st.StudentNumber)
	}); err != nil {
		return rep, err
	}

	for i := range rep.Items {
		it := &rep.Items[i]
		k := Key(it.Kind, it.ID)
		counted := it.Included && it.Weight > 0
		if counted {
			rep.WeightTotal += it.Weight
		}
		var sum, high, low float64
		outOf := map[float64]int{}
		for j := range rep.Students {
			st := &rep.Students[j]
			list := subs[st.UserID][k]
			if len(list) == 0 {
				if counted {
					st.Missing++
				}
				continue
			}
			x := pickMark(list, it.Pick)
			pct := round(min(100, max(0, x.pct)), 1)
			if it.Taken == 0 || pct > high {
				high = pct
			}
			if it.Taken == 0 || pct < low {
				low = pct
			}
			it.Taken++
			sum += pct
			outOf[round(x.max, 2)]++
			if !it.Included {
				continue
			}
			m := ReportMark{Score: round(x.score, 2), Max: round(x.max, 2), Pct: pct, Submissions: len(list), Pending: !x.complete, exact: x.exactPct()}
			if counted {
				p := round(m.exact*it.Weight, 2)
				m.Points = &p
				st.Total += p
			}
			st.Marks[k] = m
		}
		n := 0
		for v, c := range outOf {
			if c > n || (c == n && v > it.MaxMarks) {
				it.MaxMarks, n = v, c
			}
		}
		if it.Taken > 0 {
			a, h, l := round(sum/float64(it.Taken), 1), high, low
			it.Avg, it.High, it.Low = &a, &h, &l
		}
	}
	if err := s.keepAttended(ctx, classroomID, attended, subs, &rep); err != nil {
		return rep, err
	}
	if err := s.loadReportExtras(ctx, classroomID, rep.Scope.SessionID(), &rep); err != nil {
		return rep, err
	}
	rep.WeightTotal = round(rep.WeightTotal, 4)
	rep.OutOf = round(rep.WeightTotal*100, 2)
	for j := range rep.Students {
		st := &rep.Students[j]
		st.Total = round(st.Total, 2)
		if rep.OutOf > 0 {
			f := round(st.Total/rep.OutOf*100, 1)
			st.Final = &f
		}
	}
	sort.SliceStable(rep.Students, func(i, j int) bool {
		return strings.ToLower(rep.Students[i].Name) < strings.ToLower(rep.Students[j].Name)
	})

	rows, err = s.pool.Query(ctx, `SELECT kind, item_id::text, user_id::text, detail, review_minutes FROM grading.result_exceptions WHERE classroom_id = $1 ORDER BY kind, item_id, user_id`, classroomID)
	if err != nil {
		return rep, err
	}
	rep.Exceptions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ResultException, error) {
		var x ResultException
		return x, r.Scan(&x.Kind, &x.ID, &x.UserID, &x.Detail, &x.ReviewMinutes)
	})
	if err != nil {
		return rep, err
	}
	return rep, s.loadGroups(ctx, classroomID, &rep)
}

// ReportItemInput is one item's setup.
type ReportItemInput struct {
	Kind          string  `json:"kind"`
	ID            string  `json:"id"`
	Included      bool    `json:"included"`
	Weight        float64 `json:"weight"`
	Pick          string  `json:"pick"`
	Detail        string  `json:"detail"`
	ReviewMinutes int     `json:"review_minutes"`
	GroupRule     string  `json:"group_rule"`
}

// ReportSetup is the whole setup; it replaces what was saved.
type ReportSetup struct {
	Items      []ReportItemInput `json:"items"`
	Exceptions []ResultException `json:"exceptions"`
	Options    *ReportOptions    `json:"options,omitempty"` // left out: as they are
}

func validDetail(d string) bool {
	return d == "" || d == DetailMarks || d == DetailCorrectness || d == DetailAnswers
}

// SetClassReport saves the setup. Any teacher of the classroom may.
func (s *Service) SetClassReport(ctx context.Context, teacherID, classroomID string, in ReportSetup) (ClassReport, error) {
	rep, err := s.ClassReport(ctx, teacherID, classroomID)
	if err != nil {
		return rep, err
	}
	known := map[string]bool{}
	for _, it := range rep.Items {
		known[Key(it.Kind, it.ID)] = true
	}
	students := map[string]bool{}
	for _, st := range rep.Students {
		students[st.UserID] = true
	}
	bad := map[string]string{}
	seen := map[string]bool{}
	for _, it := range in.Items {
		k := Key(it.Kind, it.ID)
		switch {
		case !known[k]:
			bad["items"] = "one of these isn't a quiz or tutorial of this classroom"
		case seen[k]:
			bad["items"] = "an item is listed twice"
		case it.Weight < 0 || it.Weight > maxReportWeight:
			bad["items"] = fmt.Sprintf("a weight must be between 0 and %d, such as 0.4", maxReportWeight)
		case it.Pick != PickHigh && it.Pick != PickLow:
			bad["items"] = "choose the highest or the lowest mark"
		case !validDetail(it.Detail):
			bad["items"] = "choose what students see"
		case it.ReviewMinutes < 0 || it.ReviewMinutes > maxReviewMinutes:
			bad["items"] = "a review time can be up to a year"
		case (it.Kind == "poll" || it.Kind == KindOffline || it.Kind == "presentation") && (it.Detail != "" || it.ReviewMinutes != 0):
			bad["items"] = "what students see of a poll or an offline assignment follows its own settings"
		case it.GroupRule != "" && it.GroupRule != RuleAverage && it.GroupRule != RuleTotal && it.GroupRule != RuleHighest && it.GroupRule != RuleLowest:
			bad["items"] = "choose how a group's mark is worked out"
		}
		seen[k] = true
	}
	seenX := map[string]bool{}
	for _, x := range in.Exceptions {
		k := Key(x.Kind, x.ID) + ":" + x.UserID
		switch {
		case !known[Key(x.Kind, x.ID)] || x.Kind == "poll" || x.Kind == KindOffline || x.Kind == "presentation":
			bad["exceptions"] = "one of these isn't a quiz or tutorial of this classroom"
		case !students[x.UserID]:
			bad["exceptions"] = "one of these students isn't in this classroom"
		case seenX[k]:
			bad["exceptions"] = "a student has two exceptions for the same item"
		case !validDetail(x.Detail):
			bad["exceptions"] = "choose what the student sees"
		case x.ReviewMinutes != nil && (*x.ReviewMinutes < 0 || *x.ReviewMinutes > maxReviewMinutes):
			bad["exceptions"] = "a review time can be up to a year"
		case x.Detail == "" && x.ReviewMinutes == nil:
			bad["exceptions"] = "an exception must change what the student sees or their review time"
		}
		seenX[k] = true
	}
	if len(bad) > 0 {
		return rep, httpx.Invalid(bad)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM grading.report_items WHERE classroom_id = $1`, classroomID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM grading.result_exceptions WHERE classroom_id = $1`, classroomID); err != nil {
			return err
		}
		for _, it := range in.Items {
			if it.GroupRule == "" {
				it.GroupRule = RuleAverage
			}
			if _, err := tx.Exec(ctx, `INSERT INTO grading.report_items (classroom_id, kind, item_id, included, weight, pick, detail, review_minutes, group_rule, updated_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, classroomID, it.Kind, it.ID, it.Included, round(it.Weight, 4), it.Pick, it.Detail, it.ReviewMinutes, it.GroupRule, teacherID); err != nil {
				return err
			}
		}
		for _, x := range in.Exceptions {
			if _, err := tx.Exec(ctx, `INSERT INTO grading.result_exceptions (classroom_id, kind, item_id, user_id, detail, review_minutes)
				VALUES ($1, $2, $3, $4, $5, $6)`, classroomID, x.Kind, x.ID, x.UserID, x.Detail, x.ReviewMinutes); err != nil {
				return err
			}
		}
		if in.Options != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO grading.report_options (classroom_id, show_violations, show_attendance, show_dates, attended_only) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (classroom_id) DO UPDATE SET show_violations = $2, show_attendance = $3, show_dates = $4, attended_only = $5, updated_at = now()`,
				classroomID, in.Options.Violations, in.Options.Attendance, in.Options.Dates, in.Options.AttendedOnly); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, teacherID, "class_report_set", "classroom", classroomID, map[string]int{"items": len(in.Items), "exceptions": len(in.Exceptions)})
	})
	if err != nil {
		return rep, err
	}
	return s.ClassReport(ctx, teacherID, classroomID)
}
