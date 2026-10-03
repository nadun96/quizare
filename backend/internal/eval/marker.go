// Package eval marks answers and attaches feedback (FR-EV, UC-04, BR-11, BR-12).
package eval

import (
	"math"
	"sort"
	"strings"

	"github.com/nadun96/quizplatform/internal/quiz"
)

// KeyResult is the outcome of marking one answer against its key.
type KeyResult struct {
	Score    float64 `json:"score"`
	Max      float64 `json:"max"`
	Fraction float64 `json:"fraction"` // 0..1 share of the answer that is right
	Correct  bool    `json:"correct"`  // fully correct
	Answered bool    `json:"answered"`
}

// Answered reports whether the response contains anything at all.
func Answered(r *quiz.Response) bool {
	if r == nil {
		return false
	}
	if len(r.Selected) > 0 || len(r.Order) > 0 || strings.TrimSpace(r.Text) != "" {
		return true
	}
	for _, v := range r.Pairs {
		if v != "" {
			return true
		}
	}
	for _, v := range r.Blanks {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// MarkByKey scores a response against the answer key (FR-EV-01, BA §10.1).
//
// Partial credit, when enabled, is the share of parts (pairs, blanks, items)
// that are right; for MULTI it is (right picks − wrong picks) / correct
// options, floored at 0. Negative marks apply only to an answered response
// that earns nothing (FR-QZ-09). Unanswered questions score 0.
func MarkByKey(q quiz.Question, r *quiz.Response) KeyResult {
	res := KeyResult{Max: q.Marks, Answered: Answered(r)}
	if !res.Answered {
		return res
	}
	frac := fraction(q, r)
	res.Correct = frac >= 1
	if !q.PartialCredit && !res.Correct {
		frac = 0
	}
	res.Fraction = frac
	res.Score = round2(q.Marks * frac)
	if frac == 0 && q.NegativeMarks > 0 {
		res.Score = -q.NegativeMarks
	}
	return res
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

func fraction(q quiz.Question, r *quiz.Response) float64 {
	k := q.Key
	switch q.Type {
	case quiz.Single:
		return boolf(len(r.Selected) == 1 && len(k.Correct) == 1 && r.Selected[0] == k.Correct[0])
	case quiz.Multi:
		want := set(k.Correct)
		hits, wrong := 0, 0
		for _, s := range unique(r.Selected) {
			if want[s] {
				hits++
			} else {
				wrong++
			}
		}
		if len(want) == 0 {
			return 0
		}
		return math.Max(0, float64(hits-wrong)/float64(len(want)))
	case quiz.Match:
		return pairShare(k.Pairs, r.Pairs)
	case quiz.BlankOpt:
		return blankShare(q, r, func(want []string, got string) bool { return len(want) == 1 && want[0] == got })
	case quiz.BlankText:
		return blankShare(q, r, func(want []string, got string) bool { return textMatches(want, got, k.CaseSensitive, k.Tolerance) })
	case quiz.Drag:
		if len(q.Body.Zones) > 0 {
			return pairShare(k.Pairs, r.Pairs)
		}
		if len(k.Order) == 0 {
			return 0
		}
		right := 0
		for i, id := range k.Order {
			if i < len(r.Order) && r.Order[i] == id {
				right++
			}
		}
		return float64(right) / float64(len(k.Order))
	}
	return 0 // ESSAY is never key-marked
}

func boolf(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func set(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func unique(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func pairShare(want, got map[string]string) float64 {
	if len(want) == 0 {
		return 0
	}
	right := 0
	for k, v := range want {
		if got[k] == v {
			right++
		}
	}
	return float64(right) / float64(len(want))
}

func blankShare(q quiz.Question, r *quiz.Response, ok func(want []string, got string) bool) float64 {
	if len(q.Body.Blanks) == 0 {
		return 0
	}
	right := 0
	for _, b := range q.Body.Blanks {
		if ok(q.Key.Blanks[b], r.Blanks[b]) {
			right++
		}
	}
	return float64(right) / float64(len(q.Body.Blanks))
}

// textMatches compares a typed blank with accepted answers, ignoring
// surrounding and repeated whitespace, case unless caseSensitive, and up to
// tolerance edits ("spelling tolerance", BA §10.1).
func textMatches(accepted []string, got string, caseSensitive bool, tolerance int) bool {
	norm := func(s string) string {
		s = strings.Join(strings.Fields(s), " ")
		if !caseSensitive {
			s = strings.ToLower(s)
		}
		return s
	}
	g := norm(got)
	if g == "" {
		return false
	}
	for _, a := range accepted {
		a = norm(a)
		if a == g {
			return true
		}
		// Short answers get no tolerance: "cat" must not accept "bat".
		if tolerance > 0 && len([]rune(a)) > 3 && levenshtein(a, g) <= tolerance {
			return true
		}
	}
	return false
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// PredefinedFeedback assembles teacher-written feedback for an answer
// (FR-EV-04, BA §10.2): per selected option or blank, correct/incorrect text,
// and the matching score band.
func PredefinedFeedback(q quiz.Question, r *quiz.Response, fraction float64, correct bool) string {
	f := q.Feedback
	var parts []string
	if r != nil && len(f.Options) > 0 {
		keys := append([]string{}, r.Selected...)
		for b := range r.Blanks {
			keys = append(keys, b)
		}
		for _, v := range r.Blanks {
			keys = append(keys, v) // BLANK_OPT: option chosen for a blank
		}
		sort.Strings(keys)
		for _, k := range unique(keys) {
			if t := strings.TrimSpace(f.Options[k]); t != "" {
				parts = append(parts, t)
			}
		}
	}
	if correct && f.Correct != "" {
		parts = append(parts, f.Correct)
	} else if !correct && f.Incorrect != "" {
		parts = append(parts, f.Incorrect)
	}
	pct := int(math.Round(fraction * 100))
	for _, b := range f.Bands {
		if pct >= b.MinPct && pct <= b.MaxPct && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
			break
		}
	}
	return strings.Join(parts, "\n")
}
