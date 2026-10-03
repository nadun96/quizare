package quiz

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Response is a student's answer to one question. Which fields are used
// depends on the question type:
//
//	SINGLE, MULTI  → Selected (option ids)
//	MATCH          → Pairs (left id → right id)
//	BLANK_OPT      → Blanks (blank id → option id)
//	BLANK_TEXT     → Blanks (blank id → typed text)
//	DRAG ordering  → Order (item ids); DRAG zones → Pairs (item id → zone id)
//	ESSAY          → Text
type Response struct {
	Selected []string          `json:"selected,omitempty"`
	Pairs    map[string]string `json:"pairs,omitempty"`
	Blanks   map[string]string `json:"blanks,omitempty"`
	Order    []string          `json:"order,omitempty"`
	Text     string            `json:"text,omitempty"`
}

const maxResponseBytes = 64 << 10

// ValidateResponse checks that a response only references ids that exist in
// the question. Partial answers are allowed: answers save on every change (NFR-11).
func ValidateResponse(q Question, r Response) error {
	if raw, _ := json.Marshal(r); len(raw) > maxResponseBytes {
		return fmt.Errorf("answer is too large")
	}
	ids := func(cs []Choice) map[string]bool {
		m := map[string]bool{}
		for _, c := range cs {
			m[c.ID] = true
		}
		return m
	}
	opts := ids(q.Body.Options)
	checkAll := func(list []string, known map[string]bool) error {
		seen := map[string]bool{}
		for _, id := range list {
			if !known[id] || seen[id] {
				return fmt.Errorf("unknown or repeated choice %q", id)
			}
			seen[id] = true
		}
		return nil
	}
	switch q.Type {
	case Single:
		if len(r.Selected) > 1 {
			return fmt.Errorf("choose one option")
		}
		return checkAll(r.Selected, opts)
	case Multi:
		return checkAll(r.Selected, opts)
	case Match:
		left, right := ids(q.Body.Left), ids(q.Body.Right)
		for l, rt := range r.Pairs {
			if !left[l] || !right[rt] {
				return fmt.Errorf("unknown match %s=%s", l, rt)
			}
		}
	case BlankOpt, BlankText:
		blanks := map[string]bool{}
		for _, b := range q.Body.Blanks {
			blanks[b] = true
		}
		for b, v := range r.Blanks {
			if !blanks[b] {
				return fmt.Errorf("unknown blank %s", b)
			}
			if q.Type == BlankOpt && v != "" && !opts[v] {
				return fmt.Errorf("unknown option %s for blank %s", v, b)
			}
			if len(v) > 500 {
				return fmt.Errorf("blank %s answer is too long", b)
			}
		}
	case Drag:
		if len(q.Body.Zones) == 0 {
			return checkAll(r.Order, opts)
		}
		zones := ids(q.Body.Zones)
		for item, z := range r.Pairs {
			if !opts[item] || !zones[z] {
				return fmt.Errorf("unknown placement %s=%s", item, z)
			}
		}
	case Essay:
		if q.Body.WordLimit > 0 && len(strings.Fields(r.Text)) > q.Body.WordLimit {
			return fmt.Errorf("the answer is longer than %d words", q.Body.WordLimit)
		}
	}
	return nil
}
