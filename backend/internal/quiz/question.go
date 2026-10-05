// Package quiz owns quizzes (question series), questions, resources and the
// CSV import format (FR-QZ-01…11, BA §10).
package quiz

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nadun96/quizplatform/internal/settings"
)

type Type string

// Question types from BA §10.1.
const (
	Single    Type = "SINGLE"
	Multi     Type = "MULTI"
	Match     Type = "MATCH"
	BlankOpt  Type = "BLANK_OPT"
	BlankText Type = "BLANK_TEXT"
	Drag      Type = "DRAG"
	Essay     Type = "ESSAY"
)

var AllTypes = []Type{Single, Multi, Match, BlankOpt, BlankText, Drag, Essay}

func (t Type) Valid() bool {
	for _, x := range AllTypes {
		if x == t {
			return true
		}
	}
	return false
}

// LLMAllowed reports whether a type may be marked by an LLM (BA §10.1).
func (t Type) LLMAllowed() bool { return t == Essay || t == BlankText }

// KeyAllowed reports whether a type can be marked by answer key.
func (t Type) KeyAllowed() bool { return t != Essay }

type Choice struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Body is the student-visible structure of a question.
type Body struct {
	Options   []Choice `json:"options,omitempty"` // SINGLE, MULTI, BLANK_OPT (shared pool), DRAG items
	Left      []Choice `json:"left,omitempty"`    // MATCH
	Right     []Choice `json:"right,omitempty"`   // MATCH
	Zones     []Choice `json:"zones,omitempty"`   // DRAG into zones; empty means DRAG ordering
	Blanks    []string `json:"blanks,omitempty"`  // BLANK_*: blank ids derived from [[n]] markers
	WordLimit int      `json:"word_limit,omitempty"`
	Format    string   `json:"format,omitempty"` // "" = plain text (CSV import, older questions); "markdown" = rich text editor (D-38)
}

// FormatMarkdown marks question text and predefined feedback as Markdown
// written by the rich text editor. Plain text stays plain, so existing and
// CSV-imported questions render exactly as before.
const FormatMarkdown = "markdown"

// Key is the answer key. It never reaches a student's device before results
// are released (BR-12, NFR-07).
type Key struct {
	Correct       []string            `json:"correct,omitempty"` // SINGLE, MULTI: option ids
	Pairs         map[string]string   `json:"pairs,omitempty"`   // MATCH left→right; DRAG item→zone
	Blanks        map[string][]string `json:"blanks,omitempty"`  // BLANK_OPT: blank→[option id]; BLANK_TEXT: blank→accepted answers
	Order         []string            `json:"order,omitempty"`   // DRAG ordering
	CaseSensitive bool                `json:"case_sensitive,omitempty"`
	Tolerance     int                 `json:"tolerance,omitempty"` // BLANK_TEXT: max edit distance ("spelling tolerance")
	ModelAnswer   string              `json:"model_answer,omitempty"`
	Rubric        string              `json:"rubric,omitempty"`
}

type Band struct {
	MinPct int    `json:"min_pct"`
	MaxPct int    `json:"max_pct"`
	Text   string `json:"text"`
}

// Feedback is predefined feedback (BA §10.2).
type Feedback struct {
	Correct   string            `json:"correct,omitempty"`
	Incorrect string            `json:"incorrect,omitempty"`
	Options   map[string]string `json:"options,omitempty"` // per option id or blank id
	Bands     []Band            `json:"bands,omitempty"`   // per score band
}

type Question struct {
	ID            string             `json:"id"`
	QuizID        string             `json:"quiz_id"`
	Code          string             `json:"code"`
	Position      int                `json:"position"`
	Type          Type               `json:"type"`
	Text          string             `json:"text"`
	Body          Body               `json:"body"`
	Key           Key                `json:"key"`
	Feedback      Feedback           `json:"feedback"`
	Marks         float64            `json:"marks"`
	NegativeMarks float64            `json:"negative_marks"`
	PartialCredit bool               `json:"partial_credit"`
	Settings      settings.Overrides `json:"settings"` // question level: time limit, evaluation, LLM, feedback mode, option order
	Resources     []Resource         `json:"resources"`
}

// StudentQuestion is what the student's browser receives.
type StudentQuestion struct {
	ID        string     `json:"id"`
	Code      string     `json:"code"`
	Type      Type       `json:"type"`
	Text      string     `json:"text"`
	Body      Body       `json:"body"`
	Marks     float64    `json:"marks"`
	Resources []Resource `json:"resources"`
}

// StudentView strips the key, feedback and rubric.
func (q Question) StudentView() StudentQuestion {
	res := make([]Resource, 0, len(q.Resources))
	for _, r := range q.Resources {
		if r.Role != RoleFeedback { // feedback resources are shown with results only
			res = append(res, r.Public())
		}
	}
	return StudentQuestion{ID: q.ID, Code: q.Code, Type: q.Type, Text: q.Text, Body: q.Body, Marks: q.Marks, Resources: res}
}

// DefaultPartialCredit follows BA §10.1 ("Partial credit" column).
func DefaultPartialCredit(t Type) bool {
	switch t {
	case Single, Multi:
		return false
	default:
		return true
	}
}

var (
	blankRe = regexp.MustCompile(`\[\[(\d{1,2})\]\]`)
	codeRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)
)

// BlankIDs returns the distinct blank markers in text, in order of appearance.
func BlankIDs(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range blankRe.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Normalise trims text, assigns missing choice ids and derives blanks.
func (q *Question) Normalise() {
	q.Code = strings.TrimSpace(q.Code)
	q.Text = strings.TrimSpace(q.Text)
	q.Type = Type(strings.ToUpper(strings.TrimSpace(string(q.Type))))
	q.Body.Format = strings.ToLower(strings.TrimSpace(q.Body.Format))
	assign := func(cs []Choice, prefix string) {
		used := map[string]bool{}
		for _, c := range cs {
			used[c.ID] = true
		}
		n := 1
		for i := range cs {
			cs[i].Text = strings.TrimSpace(cs[i].Text)
			if cs[i].ID == "" {
				for used[fmt.Sprintf("%s%d", prefix, n)] {
					n++
				}
				cs[i].ID = fmt.Sprintf("%s%d", prefix, n)
				used[cs[i].ID] = true
			}
		}
	}
	assign(q.Body.Options, "o")
	assign(q.Body.Left, "l")
	assign(q.Body.Right, "r")
	assign(q.Body.Zones, "z")
	if q.Type == BlankOpt || q.Type == BlankText {
		q.Body.Blanks = BlankIDs(q.Text)
	} else {
		q.Body.Blanks = nil
	}
	if q.Marks == 0 {
		q.Marks = 1
	}
}

// EvaluationMethod returns the method set on the question itself, if any.
func (q Question) EvaluationMethod() string {
	if q.Settings.EvaluationMethod != nil {
		return *q.Settings.EvaluationMethod
	}
	return ""
}

// Validate returns field errors (empty when valid). Call Normalise first.
func (q Question) Validate() map[string]string {
	f := map[string]string{}
	if !codeRe.MatchString(q.Code) {
		f["code"] = "code must be 1-32 letters, digits, '_', '.', or '-'"
	}
	if !q.Type.Valid() {
		f["type"] = "type must be one of SINGLE, MULTI, MATCH, BLANK_OPT, BLANK_TEXT, DRAG, ESSAY"
		return f
	}
	maxText := 5000
	switch q.Body.Format {
	case "":
	case FormatMarkdown:
		maxText = 10000 // formatting syntax (tables especially) takes room
	default:
		f["body.format"] = "format must be empty (plain text) or markdown"
	}
	if n := len([]rune(q.Text)); n < 1 || n > maxText {
		f["text"] = fmt.Sprintf("question text must be 1-%d characters", maxText)
	}
	if q.Marks <= 0 || q.Marks > 1000 {
		f["marks"] = "marks must be greater than 0 and at most 1000"
	}
	if q.NegativeMarks < 0 || q.NegativeMarks > q.Marks {
		f["negative_marks"] = "negative marks must be between 0 and the question's marks"
	}
	var ve *settings.ValidationError
	if err := q.Settings.Validate(settings.LevelQuestion); err != nil {
		if ok := asValidation(err, &ve); ok {
			for k, v := range ve.Fields {
				f["settings."+k] = v
			}
		}
	}
	switch m := q.EvaluationMethod(); {
	case m == "llm" && !q.Type.LLMAllowed():
		f["settings.evaluation_method"] = "LLM marking is available for ESSAY and BLANK_TEXT only"
	case m == "key" && !q.Type.KeyAllowed():
		f["settings.evaluation_method"] = "essays cannot be marked by answer key; use llm or manual"
	}
	needKey := q.Type.KeyAllowed() && q.EvaluationMethod() != "manual" && q.EvaluationMethod() != "llm"

	b, k := q.Body, q.Key
	switch q.Type {
	case Single, Multi:
		ids := checkChoices(b.Options, "options", 2, f)
		if q.Type == Single && len(k.Correct) != 1 {
			f["key.correct"] = "exactly one correct option is required"
		} else if q.Type == Multi && len(k.Correct) < 1 {
			f["key.correct"] = "at least one correct option is required"
		}
		checkRefs(k.Correct, ids, "key.correct", f)
	case Match:
		left := checkChoices(b.Left, "left", 2, f)
		right := checkChoices(b.Right, "right", 2, f)
		checkPairs(k.Pairs, left, right, "left item", f)
	case BlankOpt:
		ids := checkChoices(b.Options, "options", 2, f)
		if len(b.Blanks) == 0 {
			f["text"] = "mark each blank in the text as [[1]], [[2]], ..."
		}
		for _, bl := range b.Blanks {
			if v := k.Blanks[bl]; len(v) != 1 || !ids[v[0]] {
				f["key.blanks."+bl] = "choose the correct option for blank " + bl
			}
		}
		checkExtraBlanks(k.Blanks, b.Blanks, f)
	case BlankText:
		if len(b.Blanks) == 0 {
			f["text"] = "mark each blank in the text as [[1]], [[2]], ..."
		}
		for _, bl := range b.Blanks {
			if needKey && len(nonEmpty(k.Blanks[bl])) == 0 {
				f["key.blanks."+bl] = "give at least one accepted answer for blank " + bl
			}
		}
		checkExtraBlanks(k.Blanks, b.Blanks, f)
		if k.Tolerance < 0 || k.Tolerance > 3 {
			f["key.tolerance"] = "spelling tolerance must be 0-3"
		}
	case Drag:
		items := checkChoices(b.Options, "options", 2, f)
		if len(b.Zones) > 0 {
			zones := checkChoices(b.Zones, "zones", 1, f)
			checkPairs(k.Pairs, items, zones, "item", f)
		} else {
			if len(k.Order) != len(b.Options) {
				f["key.order"] = "the correct order must list every item exactly once"
			} else {
				seen := map[string]bool{}
				for _, id := range k.Order {
					if !items[id] || seen[id] {
						f["key.order"] = "the correct order must list every item exactly once"
					}
					seen[id] = true
				}
			}
		}
	case Essay:
		if b.WordLimit < 0 || b.WordLimit > 10000 {
			f["body.word_limit"] = "word limit must be 0-10000"
		}
	}
	return f
}

func asValidation(err error, target **settings.ValidationError) bool {
	ve, ok := err.(*settings.ValidationError)
	if ok {
		*target = ve
	}
	return ok
}

func checkChoices(cs []Choice, field string, min int, f map[string]string) map[string]bool {
	ids := map[string]bool{}
	if len(cs) < min {
		f["body."+field] = fmt.Sprintf("at least %d %s are required", min, field)
	}
	if len(cs) > 50 {
		f["body."+field] = "at most 50 entries"
	}
	texts := map[string]bool{}
	for _, c := range cs {
		if c.Text == "" || len(c.Text) > 1000 {
			f["body."+field] = "every entry needs text (at most 1000 characters)"
		}
		if ids[c.ID] {
			f["body."+field] = "duplicate id " + c.ID
		}
		if texts[strings.ToLower(c.Text)] {
			f["body."+field] = "duplicate entry " + c.Text
		}
		ids[c.ID], texts[strings.ToLower(c.Text)] = true, true
	}
	return ids
}

func checkRefs(refs []string, ids map[string]bool, field string, f map[string]string) {
	seen := map[string]bool{}
	for _, r := range refs {
		if !ids[r] || seen[r] {
			f[field] = "refers to an unknown or repeated option"
		}
		seen[r] = true
	}
}

func checkPairs(pairs map[string]string, from, to map[string]bool, what string, f map[string]string) {
	for id := range from {
		if !to[pairs[id]] {
			f["key.pairs"] = "every " + what + " needs a valid match"
		}
	}
	for id := range pairs {
		if !from[id] {
			f["key.pairs"] = "pair refers to unknown " + what + " " + id
		}
	}
}

func checkExtraBlanks(key map[string][]string, blanks []string, f map[string]string) {
	known := map[string]bool{}
	for _, b := range blanks {
		known[b] = true
	}
	var extra []string
	for b := range key {
		if !known[b] {
			extra = append(extra, b)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		f["key.blanks"] = "answers given for blanks not in the text: " + strings.Join(extra, ", ")
	}
}

func nonEmpty(xs []string) []string {
	var out []string
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}
