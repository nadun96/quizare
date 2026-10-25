package tutor

import (
	"testing"
	"time"
)

// How schedules repeat (D-84, TS-FR-85).

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestOccurrences(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	colombo := mustZone(t, "Asia/Colombo")
	local := func(ts []time.Time, loc *time.Location) []string {
		var out []string
		for _, t := range ts {
			out = append(out, t.In(loc).Format("Mon 2006-01-02 15:04"))
		}
		return out
	}
	cases := []struct {
		name  string
		start time.Time
		r     Repeat
		want  []string
	}{
		{"once", time.Date(2027, 3, 1, 9, 0, 0, 0, colombo), Repeat{}, []string{"Mon 2027-03-01 09:00"}},
		// Daylight saving ends on 7 Nov 2027 in New York: still 09:00 local.
		{"daily across a clock change", time.Date(2027, 11, 6, 9, 0, 0, 0, ny), Repeat{Freq: "daily", Count: 3},
			[]string{"Sat 2027-11-06 09:00", "Sun 2027-11-07 09:00", "Mon 2027-11-08 09:00"}},
		{"every other day, until a date (inclusive)", time.Date(2027, 3, 1, 8, 30, 0, 0, colombo), Repeat{Freq: "daily", Interval: 2, Until: "2027-03-05"},
			[]string{"Mon 2027-03-01 08:30", "Wed 2027-03-03 08:30", "Fri 2027-03-05 08:30"}},
		// Starting on a Wednesday: Monday that week is before the start and is skipped.
		{"Mondays and Wednesdays", time.Date(2027, 3, 3, 15, 30, 0, 0, colombo), Repeat{Freq: "weekly", Days: []int{3, 1}, Count: 4},
			[]string{"Wed 2027-03-03 15:30", "Mon 2027-03-08 15:30", "Wed 2027-03-10 15:30", "Mon 2027-03-15 15:30"}},
		{"every other week, the start's weekday", time.Date(2027, 3, 2, 10, 0, 0, 0, colombo), Repeat{Freq: "weekly", Interval: 2, Until: "2027-03-31"},
			[]string{"Tue 2027-03-02 10:00", "Tue 2027-03-16 10:00", "Tue 2027-03-30 10:00"}},
	}
	for _, c := range cases {
		got, err := occurrences(c.start, c.r)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if g := local(got, c.start.Location()); len(g) != len(c.want) || g[0] != c.want[0] || g[len(g)-1] != c.want[len(c.want)-1] {
			t.Fatalf("%s = %v, want %v", c.name, g, c.want)
		}
		for i := range c.want {
			if local(got, c.start.Location())[i] != c.want[i] {
				t.Fatalf("%s[%d] = %v, want %v", c.name, i, local(got, c.start.Location()), c.want)
			}
		}
	}
	// The clock change shows in UTC, not in the local time.
	got, _ := occurrences(time.Date(2027, 11, 6, 9, 0, 0, 0, ny), Repeat{Freq: "daily", Count: 2})
	if got[1].Sub(got[0]) != 25*time.Hour {
		t.Fatalf("across the change: %v", got[1].Sub(got[0]))
	}

	// At most 100 and a year; and the rules are checked.
	got, _ = occurrences(time.Date(2027, 1, 1, 9, 0, 0, 0, colombo), Repeat{Freq: "daily", Until: "2029-01-01"})
	if len(got) != maxOccurrences {
		t.Fatalf("capped at %d", len(got))
	}
	got, _ = occurrences(time.Date(2027, 1, 1, 9, 0, 0, 0, colombo), Repeat{Freq: "weekly", Until: "2029-01-01"})
	if len(got) != 53 {
		t.Fatalf("a year of weeks = %d", len(got))
	}
	for name, r := range map[string]Repeat{
		"no end":       {Freq: "daily"},
		"monthly":      {Freq: "monthly", Count: 3},
		"bad day":      {Freq: "weekly", Days: []int{7}, Count: 3},
		"bad interval": {Freq: "weekly", Interval: 13, Count: 3},
		"until before": {Freq: "daily", Until: "2026-12-31"},
	} {
		if _, err := occurrences(time.Date(2027, 1, 1, 9, 0, 0, 0, colombo), r); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestICSFolding(t *testing.T) {
	long := "DESCRIPTION:Grade 10 Science · Ms Perera, with a very long description that goes on past seventy-five octets"
	out := fold(long)
	for _, l := range splitLines(out) {
		if len(l) > 75 {
			t.Fatalf("line of %d octets: %q", len(l), l)
		}
	}
	if icsText("a;b,c\\d\ne") != `a\;b\,c\\d\ne` {
		t.Fatalf("escaped = %q", icsText("a;b,c\\d\ne"))
	}
}

func splitLines(s string) []string {
	var out []string
	for len(s) > 0 {
		i := 0
		for i+1 < len(s) && !(s[i] == '\r' && s[i+1] == '\n') {
			i++
		}
		out = append(out, s[:i])
		if i+2 > len(s) {
			break
		}
		s = s[i+2:]
	}
	return out
}
