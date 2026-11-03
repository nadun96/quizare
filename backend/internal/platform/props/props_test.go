package props

import (
	"net/url"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestClean(t *testing.T) {
	p := Props{Department: "  Information   Systems ", CourseCode: " is2201 ", Semester: " Semester 1 ",
		Tags: []string{" #lab ", "Lab", "", "first year"}, StartsOn: ptr("2026-01-05"), EndsOn: ptr(" ")}
	if f := p.Clean("props.", true); len(f) != 0 {
		t.Fatalf("problems: %v", f)
	}
	if p.Department != "Information Systems" || p.CourseCode != "IS2201" || p.Semester != "Semester 1" || strings.Join(p.Tags, "|") != "lab|first year" || *p.StartsOn != "2026-01-05" || p.EndsOn != nil {
		t.Fatalf("cleaned = %+v", p)
	}
	bad := Props{Year: ptr(1700), StartsOn: ptr("2026-03-01"), EndsOn: ptr("2026-02-01"), Tags: strings.Split("a b c d e f g h i j k", " ")}
	f := bad.Clean("props.", true)
	if f["props.year"] == "" || f["props.ends_on"] == "" || f["props.tags"] == "" {
		t.Fatalf("problems = %v", f)
	}
	if f := (&Props{StartsOn: ptr("31/01/2026")}).Clean("", true); f["starts_on"] == "" {
		t.Fatal("a bad date passed")
	}
	// Meetings and sessions keep no term dates.
	m := Props{StartsOn: ptr("2026-01-05")}
	m.Clean("", false)
	if m.StartsOn != nil {
		t.Fatal("dates kept without withDates")
	}
}

func TestFilterSQL(t *testing.T) {
	f := FilterFrom(url.Values{"department": {"IS"}, "year": {"2025"}, "tag": {"Lab", "first year"}, "when": {"current"}, "semester": {""}})
	args := []any{"teacher"}
	sql := f.SQL("c", &args)
	for _, want := range []string{"lower(c.department) = lower($2)", "c.academic_year = $3", "@> $4::text[]", "c.starts_on <= current_date"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %q in %s", want, sql)
		}
	}
	if strings.Contains(sql, "semester") || len(args) != 4 || args[3].([]string)[0] != "lab" {
		t.Fatalf("args = %v, sql = %s", args, sql)
	}
	if g := FilterFrom(url.Values{"tags": {"lab, first year"}}).Tags; len(g) != 2 || g[1] != "first year" {
		t.Fatalf("tags = %v", g)
	}
	if p := (Props{Tags: []string{"lab, first year"}}); len(p.Clean("", false)) != 0 || len(p.Tags) != 2 {
		t.Fatalf("comma tags = %v", p.Tags)
	}
	if FilterFrom(url.Values{"year": {"abc"}}).Year != nil {
		t.Fatal("a bad year filtered")
	}
}
