package poll

import (
	"math"
	"strings"
	"unicode/utf8"
)

// Scored polls (V2-01, V2-02, D-42). Questions that have a right answer can
// carry a Key; opinion questions (ratings, word clouds, essays, files…) never do.

// Key is a poll question's answer key. It never reaches a participant until
// the answers are revealed.
type Key struct {
	Correct       []string            `json:"correct,omitempty"`        // SINGLE, MULTI: option ids
	Pairs         map[string]string   `json:"pairs,omitempty"`          // MATCH left→right; DRAG item→box
	Blanks        map[string][]string `json:"blanks,omitempty"`         // BLANK_OPT blank→[option id]; BLANK_TEXT blank→accepted words
	Order         []string            `json:"order,omitempty"`          // DRAG ranking
	Accepted      []string            `json:"accepted,omitempty"`       // SHORT_TEXT answers; DATE, TIME values
	Value         *float64            `json:"value,omitempty"`          // NUMBER, SLIDER
	Tolerance     float64             `json:"tolerance,omitempty"`      // NUMBER, SLIDER: ± allowed
	CaseSensitive bool                `json:"case_sensitive,omitempty"` // SHORT_TEXT, BLANK_TEXT
}

// Scorable reports whether a type can have an answer key.
func (t Type) Scorable() bool {
	switch t {
	case Single, Multi, Match, BlankOpt, BlankText, Drag, ShortText, Number, Slider, Date, Time:
		return true
	}
	return false
}

const (
	DefaultPoints = 100
	MaxPoints     = 10000
)

func ids(cs []Choice) map[string]bool {
	m := map[string]bool{}
	for _, c := range cs {
		m[c.ID] = true
	}
	return m
}

// ValidateKey checks k against q (call after q.Normalise). An empty key on a
// scorable type is an error; any key on an unscorable type is too.
func (q Question) ValidateKey(k *Key) map[string]string {
	f := map[string]string{}
	if k == nil {
		return f
	}
	if !q.Type.Scorable() {
		f["key"] = "this question type has no right answer, so it can't have an answer key"
		return f
	}
	b := q.Body
	switch q.Type {
	case Single, Multi:
		opt := ids(b.Options)
		if len(k.Correct) == 0 || (q.Type == Single && len(k.Correct) != 1) {
			f["key.correct"] = map[bool]string{true: "choose the right option", false: "choose at least one right option"}[q.Type == Single]
		}
		for _, id := range k.Correct {
			if !opt[id] {
				f["key.correct"] = "unknown option " + id
			}
		}
	case Match:
		l, r := ids(b.Left), ids(b.Right)
		if len(k.Pairs) != len(b.Left) {
			f["key.pairs"] = "pair every left item"
		}
		for a, z := range k.Pairs {
			if !l[a] || !r[z] {
				f["key.pairs"] = "unknown pair " + a + " → " + z
			}
		}
	case BlankOpt, BlankText:
		opt := ids(b.Options)
		for _, bl := range b.Blanks {
			v := k.Blanks[bl]
			if len(v) == 0 {
				f["key.blanks."+bl] = "give the answer for blank " + bl
			}
			for _, x := range v {
				if q.Type == BlankOpt && (!opt[x] || len(v) != 1) {
					f["key.blanks."+bl] = "choose one option for blank " + bl
				}
				if q.Type == BlankText && (strings.TrimSpace(x) == "" || utf8.RuneCountInString(x) > 100) {
					f["key.blanks."+bl] = "accepted answers must be 1-100 characters"
				}
			}
		}
		for bl := range k.Blanks {
			if !contains(b.Blanks, bl) {
				f["key.blanks"] = "unknown blank " + bl
			}
		}
	case Drag:
		opt := ids(b.Options)
		if len(b.Zones) == 0 {
			if len(k.Order) != len(b.Options) {
				f["key.order"] = "give the full correct order"
			}
			seen := map[string]bool{}
			for _, id := range k.Order {
				if !opt[id] || seen[id] {
					f["key.order"] = "the order must list every item once"
				}
				seen[id] = true
			}
		} else {
			z := ids(b.Zones)
			if len(k.Pairs) != len(b.Options) {
				f["key.pairs"] = "put every item in a box"
			}
			for item, zone := range k.Pairs {
				if !opt[item] || !z[zone] {
					f["key.pairs"] = "unknown item or box"
				}
			}
		}
	case ShortText, Date, Time:
		if len(k.Accepted) == 0 || len(k.Accepted) > 20 {
			f["key.accepted"] = "give 1-20 accepted answers"
		}
		for _, a := range k.Accepted {
			switch q.Type {
			case Date:
				if !dateRe.MatchString(a) {
					f["key.accepted"] = "use YYYY-MM-DD"
				}
			case Time:
				if !timeRe.MatchString(a) {
					f["key.accepted"] = "use HH:MM (24-hour)"
				}
			default:
				if strings.TrimSpace(a) == "" || utf8.RuneCountInString(a) > 200 {
					f["key.accepted"] = "accepted answers must be 1-200 characters"
				}
			}
		}
	case Number, Slider:
		if k.Value == nil {
			f["key.value"] = "give the right value"
		} else if math.IsNaN(*k.Value) || math.IsInf(*k.Value, 0) {
			f["key.value"] = "not a number"
		}
		if k.Tolerance < 0 || math.IsNaN(k.Tolerance) {
			f["key.tolerance"] = "tolerance must be zero or positive"
		}
	}
	return f
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func normText(s string, caseSensitive bool) string {
	s = strings.Join(strings.Fields(s), " ")
	if !caseSensitive {
		s = strings.ToLower(s)
	}
	return s
}

// Fraction is how right an answer is, from 0 to 1. Partial credit follows the
// quiz rules (D-24): the share of right parts; MULTI subtracts wrong picks.
func Fraction(q Question, k *Key, a Answer) float64 {
	if k == nil {
		return 0
	}
	switch q.Type {
	case Single:
		if len(a.Selected) == 1 && contains(k.Correct, a.Selected[0]) {
			return 1
		}
		return 0
	case Multi:
		right, wrong := 0, 0
		for _, id := range a.Selected {
			if contains(k.Correct, id) {
				right++
			} else {
				wrong++
			}
		}
		if len(k.Correct) == 0 {
			return 0
		}
		return math.Max(0, float64(right-wrong)/float64(len(k.Correct)))
	case Match:
		return share(k.Pairs, a.Pairs)
	case BlankOpt, BlankText:
		if len(k.Blanks) == 0 {
			return 0
		}
		ok := 0
		for bl, accepted := range k.Blanks {
			got := a.Blanks[bl]
			for _, x := range accepted {
				if q.Type == BlankOpt && got == x || q.Type == BlankText && normText(got, k.CaseSensitive) == normText(x, k.CaseSensitive) && got != "" {
					ok++
					break
				}
			}
		}
		return float64(ok) / float64(len(k.Blanks))
	case Drag:
		if len(q.Body.Zones) > 0 {
			return share(k.Pairs, a.Pairs)
		}
		if len(k.Order) == 0 || len(a.Order) != len(k.Order) {
			return 0
		}
		ok := 0
		for i := range k.Order {
			if a.Order[i] == k.Order[i] {
				ok++
			}
		}
		return float64(ok) / float64(len(k.Order))
	case ShortText:
		if a.Text == nil {
			return 0
		}
		for _, x := range k.Accepted {
			if normText(*a.Text, k.CaseSensitive) == normText(x, k.CaseSensitive) {
				return 1
			}
		}
	case Date:
		if contains(k.Accepted, a.Date) {
			return 1
		}
	case Time:
		if contains(k.Accepted, a.Time) {
			return 1
		}
	case Number, Slider:
		if a.Number != nil && k.Value != nil && math.Abs(*a.Number-*k.Value) <= k.Tolerance+1e-9 {
			return 1
		}
	}
	return 0
}

func share(want, got map[string]string) float64 {
	if len(want) == 0 {
		return 0
	}
	ok := 0
	for k, v := range want {
		if got[k] == v {
			ok++
		}
	}
	return float64(ok) / float64(len(want))
}

// Points turns a fraction into points. With a speed bonus, a right answer
// earns between half and all of its points, falling linearly with the time
// taken over the question's limit (D-42).
func Points(points int, fraction float64, speedBonus bool, elapsedMs, limitSec int) float64 {
	p := float64(points) * fraction
	if speedBonus && fraction > 0 && limitSec > 0 && elapsedMs >= 0 {
		left := 1 - math.Min(1, float64(elapsedMs)/float64(limitSec*1000))
		p *= 0.5 + 0.5*left
	}
	return math.Round(p*100) / 100
}
