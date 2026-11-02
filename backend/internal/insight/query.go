package insight

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// The catalog: what can be measured and what it can be broken down by.
// A query names these keys only; the SQL comes from here, never from the
// request, so nothing a client sends reaches the database as SQL.

// Fact is a table of the warehouse.
type Fact string

const (
	FactActivity Fact = "activity"
	FactAnswers  Fact = "answers"
)

type measure struct {
	Key, Label, Unit string
	Fact             Fact
	sql              string
	Help             string
}

type dimension struct {
	Key, Label string
	Facts      []Fact
	key, label string // SQL for the value and its name
	join       string // joins it needs, by name
	staffOK    bool   // admins may break down by it
}

var measures = []measure{
	{Key: "results", Label: "Quiz results", Fact: FactActivity, sql: `count(*) FILTER (WHERE f.kind = 'quiz')`, Help: "Complete, valid quiz results"},
	{Key: "avg_score", Label: "Average score", Unit: "%", Fact: FactActivity, sql: `round(avg(f.pct) FILTER (WHERE f.kind = 'quiz')::numeric, 1)`},
	{Key: "pass_rate", Label: "Pass rate", Unit: "%", Fact: FactActivity, sql: `round(100 * avg(f.passed::int) FILTER (WHERE f.kind = 'quiz'))`},
	{Key: "tutorials", Label: "Tutorials finished", Fact: FactActivity, sql: `count(*) FILTER (WHERE f.kind = 'tutorial')`},
	{Key: "students", Label: "Students", Fact: FactActivity, sql: `count(DISTINCT f.user_id)`, Help: "Different students with a result or a finished tutorial"},
	{Key: "quiz_minutes", Label: "Minutes per quiz", Unit: "min", Fact: FactActivity, sql: `round(avg(f.minutes) FILTER (WHERE f.kind = 'quiz')::numeric, 1)`, Help: "From starting to handing in"},
	{Key: "tutorial_minutes", Label: "Minutes per tutorial", Unit: "min", Fact: FactActivity, sql: `round(avg(f.minutes) FILTER (WHERE f.kind = 'tutorial')::numeric, 1)`, Help: "Time on its pages"},
	{Key: "answers", Label: "Answers marked", Fact: FactAnswers, sql: `count(*)`},
	{Key: "pct_correct", Label: "Marks gained", Unit: "%", Fact: FactAnswers, sql: `round(100 * avg(f.fraction)::numeric, 1)`, Help: "Of the questions' marks, on average"},
	{Key: "fully_right", Label: "Fully right", Unit: "%", Fact: FactAnswers, sql: `round(100 * avg(f.correct::int)::numeric, 1)`},
	{Key: "answer_students", Label: "Students", Fact: FactAnswers, sql: `count(DISTINCT f.user_id)`},
}

var both = []Fact{FactActivity, FactAnswers}

var dimensions = []dimension{
	{Key: "date", Label: "Date", Facts: both, staffOK: true},
	{Key: "classroom", Label: "Classroom", Facts: both, key: `f.classroom_id::text`, label: `c.name`, staffOK: true},
	{Key: "teacher", Label: "Teacher", Facts: both, key: `c.teacher_id::text`, label: `c.teacher_name`, staffOK: true},
	{Key: "module", Label: "Module", Facts: both, key: `t.module_id::text`, label: `coalesce(t.module_name, 'No module')`, join: "topic", staffOK: true},
	{Key: "topic", Label: "Topic", Facts: both, key: `f.topic_id::text`, label: `coalesce(t.name, 'No topic')`, join: "topic", staffOK: true},
	{Key: "item", Label: "Quiz or tutorial", Facts: both, key: `f.item_id::text`, label: `coalesce(i.title, 'Deleted')`, join: "item", staffOK: true},
	{Key: "kind", Label: "Activity", Facts: []Fact{FactActivity}, key: `f.kind`, label: `CASE f.kind WHEN 'quiz' THEN 'Quiz' ELSE 'Tutorial' END`, staffOK: true},
	{Key: "qtype", Label: "Question type", Facts: []Fact{FactAnswers}, key: `f.qtype`, label: qtypeLabel, staffOK: true},
	{Key: "student", Label: "Student", Facts: both, key: `f.user_id::text`, label: `coalesce(p.name, 'Unknown')`, join: "person"},
}

const qtypeLabel = `CASE f.qtype WHEN 'SINGLE' THEN 'One answer' WHEN 'MULTI' THEN 'Several answers' WHEN 'MATCH' THEN 'Matching'
	WHEN 'BLANK_OPT' THEN 'Fill in (choices)' WHEN 'BLANK_TEXT' THEN 'Fill in (typed)' WHEN 'DRAG' THEN 'Drag and drop'
	WHEN 'ESSAY' THEN 'Written' ELSE 'Other' END`

var joins = map[string]string{
	"topic":  `LEFT JOIN olap.topics t ON t.id = f.topic_id`,
	"item":   `LEFT JOIN olap.items i ON i.id = f.item_id`,
	"person": `LEFT JOIN olap.people p ON p.id = f.user_id`,
}

// Grains the date dimension can take.
var grains = map[string]string{"day": "day", "week": "week", "month": "month", "quarter": "quarter", "year": "year"}

// Scope is who asks: a teacher sees their classrooms, with their students;
// an admin sees everything, but no student by name and no group of fewer
// than MinGroup students (BA §4: staff don't see one student's results).
type Scope struct {
	UserID string
	Staff  bool
}

const (
	MinGroup = 5
	MaxDims  = 3
	MaxRows  = 5000
)

// Query asks for measures by dimensions.
type Query struct {
	Measures   []string            `json:"measures"`
	Dimensions []string            `json:"dimensions"`
	Grain      string              `json:"grain"` // for the date: day, week, month (default), quarter, year
	From       string              `json:"from"`  // 2026-01-01, inclusive; default a year ago
	To         string              `json:"to"`    // inclusive; default today
	Filters    map[string][]string `json:"filters"`
	Totals     bool                `json:"totals"` // add a total row, and subtotals for the first dimension
}

type Column struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // dimension or measure
	Unit  string `json:"unit,omitempty"`
}

type Cell struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Row struct {
	Dims   map[string]*Cell    `json:"dims"`   // nil for a total
	Values map[string]*float64 `json:"values"` // nil where hidden or empty
	Level  int                 `json:"level"`  // 0 a row; 1 a subtotal of the first dimension; 2 the grand total
	Hidden bool                `json:"hidden,omitempty"`
}

type Result struct {
	Columns   []Column   `json:"columns"`
	Rows      []Row      `json:"rows"`
	Truncated bool       `json:"truncated"`
	From      string     `json:"from"`
	To        string     `json:"to"`
	Grain     string     `json:"grain"`
	Loaded    *time.Time `json:"loaded_at"`
	MinGroup  int        `json:"min_group,omitempty"`
}

func findMeasure(k string) *measure {
	for i := range measures {
		if measures[i].Key == k {
			return &measures[i]
		}
	}
	return nil
}

func findDim(k string) *dimension {
	for i := range dimensions {
		if dimensions[i].Key == k {
			return &dimensions[i]
		}
	}
	return nil
}

func hasFact(fs []Fact, f Fact) bool {
	for _, x := range fs {
		if x == f {
			return true
		}
	}
	return false
}

// Run answers a query within the scope.
func (s *Service) Run(ctx context.Context, sc Scope, q Query) (Result, error) {
	bad := map[string]string{}
	if len(q.Measures) == 0 {
		bad["measures"] = "choose at least one measure"
	}
	var ms []*measure
	var fact Fact
	for _, k := range q.Measures {
		m := findMeasure(k)
		if m == nil {
			bad["measures"] = "unknown measure: " + k
			continue
		}
		if fact != "" && m.Fact != fact {
			bad["measures"] = "question measures can't be mixed with activity measures"
			continue
		}
		fact = m.Fact
		ms = append(ms, m)
	}
	if len(q.Dimensions) > MaxDims {
		bad["dimensions"] = fmt.Sprintf("break down by at most %d", MaxDims)
	}
	var ds []*dimension
	seen := map[string]bool{}
	for _, k := range q.Dimensions {
		d := findDim(k)
		switch {
		case d == nil:
			bad["dimensions"] = "unknown dimension: " + k
		case seen[k]:
			bad["dimensions"] = "each dimension once"
		case fact != "" && !hasFact(d.Facts, fact):
			bad["dimensions"] = d.Label + " doesn't go with these measures"
		case sc.Staff && !d.staffOK:
			bad["dimensions"] = "staff see groups, not single students"
		default:
			seen[k] = true
			ds = append(ds, d)
		}
	}
	grain := q.Grain
	if grain == "" {
		grain = "month"
	}
	if grains[grain] == "" {
		bad["grain"] = "day, week, month, quarter or year"
	}
	today := s.now().UTC()
	from, to := today.AddDate(-1, 0, 1), today
	if q.From != "" {
		t, err := time.Parse("2006-01-02", q.From)
		if err != nil {
			bad["from"] = "a date like 2026-01-31"
		}
		from = t
	}
	if q.To != "" {
		t, err := time.Parse("2006-01-02", q.To)
		if err != nil {
			bad["to"] = "a date like 2026-01-31"
		}
		to = t
	}
	if to.Before(from) {
		bad["to"] = "after the start"
	}
	for k := range q.Filters {
		d := findDim(k)
		if d == nil || d.Key == "date" || (fact != "" && !hasFact(d.Facts, fact)) {
			bad["filters"] = "can't filter by " + k
		} else if sc.Staff && !d.staffOK {
			bad["filters"] = "staff see groups, not single students"
		}
	}
	if len(bad) > 0 {
		return Result{}, httpx.Invalid(bad)
	}

	// SELECT the dimensions (value and name), the measures, and for staff the group's size.
	args := []any{from.Format("2006-01-02"), to.AddDate(0, 0, 1).Format("2006-01-02")}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	need := map[string]bool{}
	var sel, group []string
	for _, d := range ds {
		key, label := d.key, d.label
		if d.Key == "date" {
			key = fmt.Sprintf(`to_char(date_trunc('%s', f.at AT TIME ZONE 'UTC'), 'YYYY-MM-DD')`, grains[grain])
			label = key
		}
		if d.join != "" {
			need[d.join] = true
		}
		sel = append(sel, key, label)
		group = append(group, key, label)
	}
	for _, m := range ms {
		sel = append(sel, m.sql)
	}
	sel = append(sel, `count(DISTINCT f.user_id)`)
	if len(ds) > 0 {
		// grouping() says which rows are subtotals: 0 for a row.
		sel = append(sel, fmt.Sprintf("grouping(%s)", strings.Join(group, ", ")))
	} else {
		sel = append(sel, "0")
	}

	table := "olap.activity"
	if fact == FactAnswers {
		table = "olap.answers"
	}
	where := []string{`f.at >= $1::date`, `f.at < $2::date`}
	if !sc.Staff {
		where = append(where, `c.teacher_id = `+arg(sc.UserID))
	}
	for k, vals := range q.Filters {
		d := findDim(k)
		if len(vals) == 0 {
			continue
		}
		if d.join != "" {
			need[d.join] = true
		}
		where = append(where, fmt.Sprintf(`%s = ANY(%s::text[])`, d.key, arg(vals)))
	}
	var from_ strings.Builder
	from_.WriteString(table + " f JOIN olap.classrooms c ON c.id = f.classroom_id")
	for _, j := range []string{"topic", "item", "person"} {
		if need[j] {
			from_.WriteString(" " + joins[j])
		}
	}
	groupBy := ""
	if len(ds) > 0 {
		inner := "(" + strings.Join(group, ", ") + ")"
		if q.Totals {
			sets := []string{inner}
			if len(ds) > 1 {
				sets = append(sets, "("+strings.Join(group[:2], ", ")+")")
			}
			sets = append(sets, "()")
			groupBy = " GROUP BY GROUPING SETS (" + strings.Join(sets, ", ") + ")"
		} else {
			groupBy = " GROUP BY " + strings.Join(group, ", ")
		}
	}
	order := ""
	if len(ds) > 0 {
		// Totals after the rows they total; then by the dimensions' values, dates in order.
		var o []string
		for i, d := range ds {
			if d.Key == "date" {
				o = append(o, fmt.Sprintf("%d", 2*i+1))
			} else {
				o = append(o, fmt.Sprintf("%d", 2*i+2))
			}
		}
		order = " ORDER BY " + strings.Join(o, ", ")
		if q.Totals {
			order = fmt.Sprintf(" ORDER BY %s", strings.Join(o, " NULLS LAST, ")+" NULLS LAST")
		}
	}
	sql := "SELECT " + strings.Join(sel, ", ") + " FROM " + from_.String() + " WHERE " + strings.Join(where, " AND ") + groupBy + order + fmt.Sprintf(" LIMIT %d", MaxRows+1)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	out := Result{Rows: []Row{}, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Grain: grain}
	if sc.Staff {
		out.MinGroup = MinGroup
	}
	for _, d := range ds {
		out.Columns = append(out.Columns, Column{Key: d.Key, Label: d.Label, Kind: "dimension"})
	}
	for _, m := range ms {
		out.Columns = append(out.Columns, Column{Key: m.Key, Label: m.Label, Kind: "measure", Unit: m.Unit})
	}
	all := (1 << (2 * len(ds))) - 1
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return out, err
		}
		if len(out.Rows) == MaxRows {
			out.Truncated = true
			break
		}
		r := Row{Dims: map[string]*Cell{}, Values: map[string]*float64{}}
		for i, d := range ds {
			if vals[2*i] == nil {
				continue
			}
			r.Dims[d.Key] = &Cell{ID: fmt.Sprint(vals[2*i]), Label: fmt.Sprint(vals[2*i+1])}
		}
		n := num(vals[2*len(ds)+len(ms)])
		g := int(num(vals[2*len(ds)+len(ms)+1]))
		switch {
		case len(ds) == 0 || g == 0:
			r.Level = 0
		case g == all:
			r.Level, r.Dims = 2, nil
		default:
			r.Level = 1
		}
		r.Hidden = sc.Staff && n < MinGroup
		for i, m := range ms {
			if r.Hidden || vals[2*len(ds)+i] == nil {
				r.Values[m.Key] = nil
				continue
			}
			v := num(vals[2*len(ds)+i])
			r.Values[m.Key] = &v
		}
		out.Rows = append(out.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	var loaded time.Time
	if err := s.pool.QueryRow(ctx, `SELECT last_at FROM olap.load_state`).Scan(&loaded); err == nil && loaded.Year() > 1970 {
		out.Loaded = &loaded
	}
	return out, nil
}

// num reads a number pgx returned as int64, float64 or numeric.
func num(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case float64:
		return x
	case pgtype.Numeric:
		f, _ := x.Float64Value()
		return f.Float64
	}
	var f float64
	if _, err := fmt.Sscan(fmt.Sprint(v), &f); err == nil && !math.IsNaN(f) {
		return f
	}
	return 0
}
