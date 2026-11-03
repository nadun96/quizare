// Package props is the optional catalogue details a classroom, a meeting or
// a tutoring session carries (D-128): department, course code, academic
// year, semester and free tags, plus a term's dates on a classroom. They're
// for finding things: lists filter by them and offer the values in use.
package props

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

const (
	MaxTags   = 10
	maxTag    = 30
	maxText   = 100
	maxCode   = 40
	dayLayout = "2006-01-02"
)

// Props are the details. Every field is optional.
type Props struct {
	Department string   `json:"department"`
	CourseCode string   `json:"course_code"`
	Year       *int     `json:"year"` // academic year, e.g. 2025
	Semester   string   `json:"semester"`
	Tags       []string `json:"tags"`
	// A classroom's term; meetings and sessions have their own times.
	StartsOn *string `json:"starts_on,omitempty"`
	EndsOn   *string `json:"ends_on,omitempty"`
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

// Clean tidies the details in place (spaces, tag duplicates whatever the
// case) and reports what is wrong, as fields under prefix ("props.").
// Dates are kept only when withDates.
func (p *Props) Clean(prefix string, withDates bool) map[string]string {
	f := map[string]string{}
	p.Department = oneLine(p.Department, maxText)
	p.Semester = oneLine(p.Semester, maxCode)
	p.CourseCode = strings.ToUpper(oneLine(p.CourseCode, maxCode))
	if p.Year != nil && (*p.Year < 1990 || *p.Year > 2100) {
		f[prefix+"year"] = "a year like 2025"
	}
	seen := map[string]bool{}
	tags := []string{}
	var parts []string
	for _, t := range p.Tags {
		parts = append(parts, strings.Split(t, ",")...) // "lab, first year" is two tags
	}
	for _, t := range parts {
		t = oneLine(strings.TrimPrefix(strings.TrimSpace(t), "#"), maxTag)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		tags = append(tags, t)
	}
	if len(tags) > MaxTags {
		f[prefix+"tags"] = fmt.Sprintf("at most %d tags", MaxTags)
	}
	p.Tags = tags
	if !withDates {
		p.StartsOn, p.EndsOn = nil, nil
		return f
	}
	day := func(v **string, key string) (time.Time, bool) {
		if *v == nil || strings.TrimSpace(**v) == "" {
			*v = nil
			return time.Time{}, false
		}
		t, err := time.Parse(dayLayout, strings.TrimSpace(**v))
		if err != nil {
			f[prefix+key] = "a date like 2026-01-31"
			return t, false
		}
		s := t.Format(dayLayout)
		*v = &s
		return t, true
	}
	start, okS := day(&p.StartsOn, "starts_on")
	end, okE := day(&p.EndsOn, "ends_on")
	if okS && okE && end.Before(start) {
		f[prefix+"ends_on"] = "the end can't be before the start"
	}
	return f
}

// Invalid is Clean's problems as a validation error, or nil.
func Invalid(f map[string]string) error {
	if len(f) == 0 {
		return nil
	}
	return httpx.Invalid(f)
}

// Columns are the details' columns, under a table alias, in Scan's order.
func Columns(alias string, withDates bool) string {
	a := alias + "."
	cols := a + "department, " + a + "course_code, " + a + "academic_year, " + a + "semester, " + a + "tags"
	if withDates {
		cols += ", to_char(" + a + "starts_on, 'YYYY-MM-DD'), to_char(" + a + "ends_on, 'YYYY-MM-DD')"
	}
	return cols
}

// Dest are the scan targets for Columns.
func (p *Props) Dest(withDates bool) []any {
	d := []any{&p.Department, &p.CourseCode, &p.Year, &p.Semester, &p.Tags}
	if withDates {
		d = append(d, &p.StartsOn, &p.EndsOn)
	}
	return d
}

// Values are the details for an INSERT or UPDATE, in Columns' order.
func (p Props) Values(withDates bool) []any {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	v := []any{p.Department, p.CourseCode, p.Year, p.Semester, tags}
	if withDates {
		v = append(v, p.StartsOn, p.EndsOn)
	}
	return v
}

// Filter is what a list may be narrowed by.
type Filter struct {
	Department string
	CourseCode string
	Year       *int
	Semester   string
	Tags       []string // all of them
	When       string   // classrooms: "current", "upcoming" or "past" by the term's dates
}

// FilterFrom reads ?department=&course_code=&year=&semester=&tag=&tag=&when=.
func FilterFrom(q url.Values) Filter {
	f := Filter{Department: q.Get("department"), CourseCode: q.Get("course_code"), Semester: q.Get("semester"), When: q.Get("when")}
	if y, err := strconv.Atoi(q.Get("year")); err == nil {
		f.Year = &y
	}
	for _, t := range append(q["tag"], strings.Split(q.Get("tags"), ",")...) { // ?tag=a&tag=b or ?tags=a,b
		if t = strings.TrimSpace(t); t != "" {
			f.Tags = append(f.Tags, t)
		}
	}
	return f
}

// SQL is the filter as conditions on alias, each starting " AND ", with its
// arguments appended to args.
func (f Filter) SQL(alias string, args *[]any) string {
	a := alias + "."
	arg := func(v any) string {
		*args = append(*args, v)
		return fmt.Sprintf("$%d", len(*args))
	}
	var b strings.Builder
	if f.Department != "" {
		fmt.Fprintf(&b, " AND lower(%sdepartment) = lower(%s)", a, arg(f.Department))
	}
	if f.CourseCode != "" {
		fmt.Fprintf(&b, " AND upper(%scourse_code) = upper(%s)", a, arg(f.CourseCode))
	}
	if f.Year != nil {
		fmt.Fprintf(&b, " AND %sacademic_year = %s", a, arg(*f.Year))
	}
	if f.Semester != "" {
		fmt.Fprintf(&b, " AND lower(%ssemester) = lower(%s)", a, arg(f.Semester))
	}
	if len(f.Tags) > 0 {
		lower := make([]string, len(f.Tags))
		for i, t := range f.Tags {
			lower[i] = strings.ToLower(t)
		}
		fmt.Fprintf(&b, " AND (SELECT array_agg(lower(t)) FROM unnest(%stags) t) @> %s::text[]", a, arg(lower))
	}
	switch f.When {
	case "current":
		fmt.Fprintf(&b, " AND (%[1]sstarts_on IS NULL OR %[1]sstarts_on <= current_date) AND (%[1]sends_on IS NULL OR %[1]sends_on >= current_date)", a)
	case "upcoming":
		fmt.Fprintf(&b, " AND %sstarts_on > current_date", a)
	case "past":
		fmt.Fprintf(&b, " AND %sends_on < current_date", a)
	}
	return b.String()
}

// Count is one value in use and how many have it.
type Count struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// Facets are the values in use, most common first, for a list's filters.
type Facets struct {
	Departments []Count `json:"departments"`
	CourseCodes []Count `json:"course_codes"`
	Years       []Count `json:"years"`
	Semesters   []Count `json:"semesters"`
	Tags        []Count `json:"tags"`
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// LoadFacets counts the values in use among the rows of from ("FROM t x
// WHERE …"), whose alias is alias.
func LoadFacets(ctx context.Context, q querier, from, alias string, args ...any) (Facets, error) {
	a := alias + "."
	var out Facets
	for _, c := range []struct {
		expr string
		dst  *[]Count
	}{
		{a + "department", &out.Departments},
		{a + "course_code", &out.CourseCodes},
		{a + "academic_year::text", &out.Years},
		{a + "semester", &out.Semesters},
		{"unnest(" + a + "tags)", &out.Tags},
	} {
		rows, err := q.Query(ctx, `SELECT v, count(*)::int FROM (SELECT `+c.expr+` AS v `+from+`) s
			WHERE v IS NOT NULL AND v <> '' GROUP BY v ORDER BY count(*) DESC, v LIMIT 100`, args...)
		if err != nil {
			return out, err
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Count, error) {
			var x Count
			return x, r.Scan(&x.Value, &x.Count)
		})
		if err != nil {
			return out, err
		}
		*c.dst = list
	}
	// Years newest first; the rest by use.
	slices.SortFunc(out.Years, func(x, y Count) int { return strings.Compare(y.Value, x.Value) })
	return out, nil
}
