package poll

import "testing"

func TestScorableTypes(t *testing.T) {
	scorable := map[Type]bool{Single: true, Multi: true, Match: true, BlankOpt: true, BlankText: true, Drag: true, ShortText: true, Number: true, Slider: true, Date: true, Time: true}
	for _, typ := range AllTypes {
		if typ.Scorable() != scorable[typ] {
			t.Errorf("%s scorable = %v", typ, typ.Scorable())
		}
	}
}

func TestValidateKey(t *testing.T) {
	single := q(Single, "Pick", Body{Options: opts("a", "b")})
	if f := single.ValidateKey(&Key{Correct: []string{"o2"}}); len(f) != 0 {
		t.Fatalf("good key rejected: %v", f)
	}
	for name, c := range map[string]struct {
		q Question
		k Key
	}{
		"single two right":    {single, Key{Correct: []string{"o1", "o2"}}},
		"unknown option":      {single, Key{Correct: []string{"o9"}}},
		"match incomplete":    {q(Match, "M", Body{Left: opts("x", "y"), Right: opts("1", "2")}), Key{Pairs: map[string]string{"l1": "r1"}}},
		"blank missing":       {q(BlankText, "a [[1]] b [[2]]", Body{}), Key{Blanks: map[string][]string{"1": {"x"}}}},
		"drag order short":    {q(Drag, "R", Body{Options: opts("a", "b", "c")}), Key{Order: []string{"o1", "o2"}}},
		"number no value":     {q(Number, "N", Body{}), Key{}},
		"date format":         {q(Date, "D", Body{}), Key{Accepted: []string{"05/10/2026"}}},
		"key on opinion type": {q(Rating, "R", Body{Points: 5}), Key{Value: ptr(5.0)}},
	} {
		k := c.k
		if len(c.q.ValidateKey(&k)) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFraction(t *testing.T) {
	multi := q(Multi, "M", Body{Options: opts("a", "b", "c", "d")})
	mk := &Key{Correct: []string{"o1", "o2"}}
	for sel, want := range map[string]float64{"o1,o2": 1, "o1": 0.5, "o1,o3": 0, "o3": 0, "o1,o2,o3": 0.5} {
		a := Answer{Selected: split(sel)}
		if got := Fraction(multi, mk, a); got != want {
			t.Errorf("multi %s = %v, want %v", sel, got, want)
		}
	}
	short := q(ShortText, "Capital?", Body{})
	if Fraction(short, &Key{Accepted: []string{"Paris"}}, Answer{Text: ptr("  paris ")}) != 1 {
		t.Error("short text should ignore case and spaces")
	}
	if Fraction(short, &Key{Accepted: []string{"Paris"}, CaseSensitive: true}, Answer{Text: ptr("paris")}) != 0 {
		t.Error("case-sensitive key")
	}
	num := q(Number, "g", Body{})
	if Fraction(num, &Key{Value: ptr(9.81), Tolerance: 0.05}, Answer{Number: ptr(9.8)}) != 1 || Fraction(num, &Key{Value: ptr(9.81)}, Answer{Number: ptr(9.8)}) != 0 {
		t.Error("number tolerance")
	}
	rank := q(Drag, "R", Body{Options: opts("a", "b", "c", "d")})
	if got := Fraction(rank, &Key{Order: []string{"o1", "o2", "o3", "o4"}}, Answer{Order: []string{"o1", "o2", "o4", "o3"}}); got != 0.5 {
		t.Errorf("ranking partial: %v", got)
	}
	blank := q(BlankText, "Water boils at [[1]] and freezes at [[2]]", Body{})
	if got := Fraction(blank, &Key{Blanks: map[string][]string{"1": {"100", "100°C"}, "2": {"0"}}}, Answer{Blanks: map[string]string{"1": "100°c", "2": "5"}}); got != 0.5 {
		t.Errorf("blank partial: %v", got)
	}
	if Fraction(blank, nil, Answer{}) != 0 {
		t.Error("no key scores nothing")
	}
}

func TestPointsAndSpeedBonus(t *testing.T) {
	for _, c := range []struct {
		points   int
		fraction float64
		speed    bool
		elapsed  int
		limit    int
		want     float64
	}{
		{100, 1, false, 9000, 10, 100},
		{100, 0.5, false, 0, 0, 50},
		{100, 1, true, 0, 10, 100},    // instant: full points
		{100, 1, true, 5000, 10, 75},  // half the time: 75%
		{100, 1, true, 10000, 10, 50}, // at the limit: half
		{100, 1, true, 20000, 10, 50}, // never below half
		{100, 0, true, 0, 10, 0},      // wrong answers earn nothing
		{100, 1, true, 3000, 0, 100},  // no limit: no bonus
		{1000, 0.5, true, 2500, 10, 437.5},
	} {
		if got := Points(c.points, c.fraction, c.speed, c.elapsed, c.limit); got != c.want {
			t.Errorf("Points(%+v) = %v", c, got)
		}
	}
}

func split(s string) []string {
	var out []string
	cur := ""
	for _, r := range s + "," {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	return out
}
