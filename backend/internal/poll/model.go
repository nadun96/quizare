// Package poll runs live polls (D-40): anonymous or identified participants,
// every question input type, and results aggregated as answers arrive.
package poll

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Type is a poll question type. The first seven are the quiz types (BA §10.1)
// used as opinion questions; the rest are poll-only inputs.
type Type string

const (
	Single    Type = "SINGLE"     // radio buttons or a dropdown
	Multi     Type = "MULTI"      // checkboxes
	Match     Type = "MATCH"      // pair left items with right items
	BlankOpt  Type = "BLANK_OPT"  // choose a word per blank
	BlankText Type = "BLANK_TEXT" // type a word per blank
	Drag      Type = "DRAG"       // rank items, or sort them into boxes
	Essay     Type = "ESSAY"      // long text
	ShortText Type = "SHORT_TEXT" // one line of text
	WordCloud Type = "WORD_CLOUD" // a few words each, shown as a cloud
	Number    Type = "NUMBER"
	Date      Type = "DATE"
	Time      Type = "TIME"
	Rating    Type = "RATING" // stars
	Slider    Type = "SLIDER"
	Likert    Type = "LIKERT" // agreement scale, one row per statement
	Matrix    Type = "MATRIX" // rows × columns of radios, checkboxes or text boxes
	File      Type = "FILE"
	Audio     Type = "AUDIO" // recorded in the browser
	Video     Type = "VIDEO" // recorded in the browser
	Code      Type = "CODE"
)

var AllTypes = []Type{Single, Multi, Match, BlankOpt, BlankText, Drag, Essay, ShortText, WordCloud, Number, Date, Time, Rating, Slider, Likert, Matrix, File, Audio, Video, Code}

func (t Type) Valid() bool {
	for _, x := range AllTypes {
		if t == x {
			return true
		}
	}
	return false
}

// IsMedia reports whether answers are uploaded files.
func (t Type) IsMedia() bool { return t == File || t == Audio || t == Video }

// Upload limits (D-40): small enough for PostgreSQL on a shared 4 GB host.
const (
	MaxFileMB        = 5
	MaxAudioSeconds  = 60
	MaxVideoSeconds  = 30
	AudioMaxBytes    = 3 << 20
	VideoMaxBytes    = 12 << 20
	PollStorageLimit = 200 << 20 // per poll, all files together
	MaxParticipants  = 2000
	MaxQuestions     = 50
	// JoinBurst joins can arrive at once from one network address, then four a
	// second. A whole lecture hall behind one school router joins together
	// (D-51); the limit still slows a script filling a poll with fake people.
	JoinBurst = 150
)

type Choice struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Body holds a question's options and limits; each type uses some fields.
type Body struct {
	Format     string   `json:"format,omitempty"`      // "" plain or "markdown" (D-38) for the question text
	Options    []Choice `json:"options,omitempty"`     // SINGLE, MULTI, BLANK_OPT (pool), DRAG items
	Display    string   `json:"display,omitempty"`     // SINGLE: radio | dropdown
	MaxChoices int      `json:"max_choices,omitempty"` // MULTI: 0 means any number
	Left       []Choice `json:"left,omitempty"`        // MATCH
	Right      []Choice `json:"right,omitempty"`       // MATCH
	Zones      []Choice `json:"zones,omitempty"`       // DRAG into boxes; empty means ranking
	Blanks     []string `json:"blanks,omitempty"`      // BLANK_*: derived from [[n]]
	Rows       []Choice `json:"rows,omitempty"`        // MATRIX rows; LIKERT statements
	Columns    []Choice `json:"columns,omitempty"`     // MATRIX columns
	Mode       string   `json:"mode,omitempty"`        // MATRIX: single | multi | text
	Scale      []string `json:"scale,omitempty"`       // LIKERT: 5 or 7 labels
	Min        *float64 `json:"min,omitempty"`         // NUMBER, SLIDER
	Max        *float64 `json:"max,omitempty"`         // NUMBER, SLIDER
	Step       *float64 `json:"step,omitempty"`        // NUMBER, SLIDER
	MinLabel   string   `json:"min_label,omitempty"`   // SLIDER
	MaxLabel   string   `json:"max_label,omitempty"`   // SLIDER
	Unit       string   `json:"unit,omitempty"`        // NUMBER
	From       string   `json:"from,omitempty"`        // DATE YYYY-MM-DD, TIME HH:MM
	To         string   `json:"to,omitempty"`          // DATE, TIME
	Points     int      `json:"points,omitempty"`      // RATING: 3..10 stars
	MaxLength  int      `json:"max_length,omitempty"`  // SHORT_TEXT, CODE
	MaxWords   int      `json:"max_words,omitempty"`   // ESSAY: 0 means no limit
	MaxEntries int      `json:"max_entries,omitempty"` // WORD_CLOUD: words per participant
	Language   string   `json:"language,omitempty"`    // CODE: label shown to participants
	Accept     []string `json:"accept,omitempty"`      // FILE: image, pdf, document, any
	MaxMB      int      `json:"max_mb,omitempty"`      // FILE
	MaxSeconds int      `json:"max_seconds,omitempty"` // AUDIO, VIDEO
}

type Question struct {
	ID       string `json:"id"`
	PollID   string `json:"poll_id"`
	Position int    `json:"position"`
	Type     Type   `json:"type"`
	Text     string `json:"text"`
	Body     Body   `json:"body"`
	Required bool   `json:"required"`
	// Competition (D-42): teacher-only key, points and an optional time limit.
	Key          *Key `json:"key,omitempty"`
	Points       int  `json:"points"`
	TimeLimitSec *int `json:"time_limit_sec"`
}

// DefaultLikert is the five-point agreement scale.
var DefaultLikert = []string{"Strongly disagree", "Disagree", "Neutral", "Agree", "Strongly agree"}

var (
	blankRe  = regexp.MustCompile(`\[\[(\d{1,2})\]\]`)
	dateRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timeRe   = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	accepted = map[string]bool{"image": true, "pdf": true, "document": true, "any": true}
)

func blankIDs(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range blankRe.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func f(v float64) *float64 { return &v }

func assignIDs(cs []Choice, prefix string) {
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

// Normalise trims text, assigns choice ids, derives blanks and fills defaults.
func (q *Question) Normalise() {
	q.Text = strings.TrimSpace(q.Text)
	q.Type = Type(strings.ToUpper(strings.TrimSpace(string(q.Type))))
	b := &q.Body
	b.Format = strings.ToLower(strings.TrimSpace(b.Format))
	assignIDs(b.Options, "o")
	assignIDs(b.Left, "l")
	assignIDs(b.Right, "r")
	assignIDs(b.Zones, "z")
	assignIDs(b.Rows, "row")
	assignIDs(b.Columns, "col")
	b.Blanks = nil
	if q.Type == BlankOpt || q.Type == BlankText {
		b.Blanks = blankIDs(q.Text)
	}
	switch q.Type {
	case Single:
		if b.Display != "dropdown" {
			b.Display = "radio"
		}
	case ShortText:
		if b.MaxLength <= 0 {
			b.MaxLength = 200
		}
	case Code:
		if b.MaxLength <= 0 {
			b.MaxLength = 10000
		}
	case WordCloud:
		if b.MaxEntries <= 0 {
			b.MaxEntries = 3
		}
	case Rating:
		if b.Points == 0 {
			b.Points = 5
		}
	case Slider:
		if b.Min == nil {
			b.Min = f(0)
		}
		if b.Max == nil {
			b.Max = f(100)
		}
		if b.Step == nil {
			b.Step = f(1)
		}
	case Likert:
		if len(b.Scale) == 0 {
			b.Scale = append([]string(nil), DefaultLikert...)
		}
	case Matrix:
		if b.Mode == "" {
			b.Mode = "single"
		}
	case File:
		if len(b.Accept) == 0 {
			b.Accept = []string{"any"}
		}
		if b.MaxMB <= 0 {
			b.MaxMB = MaxFileMB
		}
	case Audio:
		if b.MaxSeconds <= 0 {
			b.MaxSeconds = MaxAudioSeconds
		}
	case Video:
		if b.MaxSeconds <= 0 {
			b.MaxSeconds = MaxVideoSeconds
		}
	}
}

func checkChoices(cs []Choice, field string, min, max int, f map[string]string) map[string]bool {
	ids := map[string]bool{}
	if len(cs) < min || len(cs) > max {
		f[field] = fmt.Sprintf("%d to %d entries are required", min, max)
	}
	for i, c := range cs {
		if c.Text == "" || utf8.RuneCountInString(c.Text) > 300 {
			f[fmt.Sprintf("%s.%d", field, i)] = "text must be 1-300 characters"
		}
		if ids[c.ID] {
			f[fmt.Sprintf("%s.%d", field, i)] = "duplicate id " + c.ID
		}
		ids[c.ID] = true
	}
	return ids
}

// Validate returns field errors (empty when valid). Call Normalise first.
func (q Question) Validate() map[string]string {
	errs := map[string]string{}
	if !q.Type.Valid() {
		errs["type"] = "unknown question type"
		return errs
	}
	maxText := 2000
	if q.Body.Format == "markdown" {
		maxText = 6000
	} else if q.Body.Format != "" {
		errs["body.format"] = "format must be empty (plain text) or markdown"
	}
	if n := utf8.RuneCountInString(q.Text); n < 1 || n > maxText {
		errs["text"] = fmt.Sprintf("question text must be 1-%d characters", maxText)
	}
	b := q.Body
	switch q.Type {
	case Single:
		checkChoices(b.Options, "body.options", 2, 50, errs)
	case Multi:
		checkChoices(b.Options, "body.options", 2, 50, errs)
		if b.MaxChoices < 0 || b.MaxChoices > len(b.Options) {
			errs["body.max_choices"] = "max choices must be between 0 (any) and the number of options"
		}
	case Match:
		checkChoices(b.Left, "body.left", 2, 20, errs)
		checkChoices(b.Right, "body.right", 2, 20, errs)
	case BlankOpt:
		checkChoices(b.Options, "body.options", 2, 30, errs)
		if len(b.Blanks) == 0 {
			errs["text"] = "mark each blank in the text as [[1]], [[2]], ..."
		}
	case BlankText:
		if len(b.Blanks) == 0 {
			errs["text"] = "mark each blank in the text as [[1]], [[2]], ..."
		}
	case Drag:
		checkChoices(b.Options, "body.options", 2, 30, errs)
		if len(b.Zones) > 0 {
			checkChoices(b.Zones, "body.zones", 2, 10, errs)
		}
	case Essay:
		if b.MaxWords < 0 || b.MaxWords > 5000 {
			errs["body.max_words"] = "word limit must be 0-5000"
		}
	case ShortText:
		if b.MaxLength > 500 {
			errs["body.max_length"] = "at most 500 characters"
		}
	case Code:
		if b.MaxLength > 20000 {
			errs["body.max_length"] = "at most 20000 characters"
		}
		if utf8.RuneCountInString(b.Language) > 40 {
			errs["body.language"] = "at most 40 characters"
		}
	case WordCloud:
		if b.MaxEntries < 1 || b.MaxEntries > 5 {
			errs["body.max_entries"] = "1 to 5 words per participant"
		}
	case Number, Slider:
		if b.Min != nil && b.Max != nil && *b.Min >= *b.Max {
			errs["body.max"] = "maximum must be greater than minimum"
		}
		if b.Step != nil && *b.Step <= 0 {
			errs["body.step"] = "step must be positive"
		}
		if q.Type == Slider && b.Min != nil && b.Max != nil && b.Step != nil && (*b.Max-*b.Min)/(*b.Step) > 10000 {
			errs["body.step"] = "the slider would have more than 10000 steps"
		}
		if utf8.RuneCountInString(b.Unit) > 20 || utf8.RuneCountInString(b.MinLabel) > 60 || utf8.RuneCountInString(b.MaxLabel) > 60 {
			errs["body.unit"] = "labels are too long"
		}
	case Date:
		for k, v := range map[string]string{"body.from": b.From, "body.to": b.To} {
			if v != "" {
				if _, err := time.Parse(time.DateOnly, v); err != nil || !dateRe.MatchString(v) {
					errs[k] = "use YYYY-MM-DD"
				}
			}
		}
		if b.From != "" && b.To != "" && b.From > b.To {
			errs["body.to"] = "the last date is before the first"
		}
	case Time:
		for k, v := range map[string]string{"body.from": b.From, "body.to": b.To} {
			if v != "" && !timeRe.MatchString(v) {
				errs[k] = "use HH:MM (24-hour)"
			}
		}
	case Rating:
		if b.Points < 3 || b.Points > 10 {
			errs["body.points"] = "3 to 10 stars"
		}
	case Likert:
		checkChoices(b.Rows, "body.rows", 1, 20, errs)
		if len(b.Scale) != 5 && len(b.Scale) != 7 {
			errs["body.scale"] = "a Likert scale has 5 or 7 points"
		}
		for i, l := range b.Scale {
			if strings.TrimSpace(l) == "" || utf8.RuneCountInString(l) > 40 {
				errs[fmt.Sprintf("body.scale.%d", i)] = "label must be 1-40 characters"
			}
		}
	case Matrix:
		checkChoices(b.Rows, "body.rows", 1, 20, errs)
		checkChoices(b.Columns, "body.columns", 2, 10, errs)
		if b.Mode != "single" && b.Mode != "multi" && b.Mode != "text" {
			errs["body.mode"] = "mode must be single, multi or text"
		}
	case File:
		for _, a := range b.Accept {
			if !accepted[a] {
				errs["body.accept"] = "accept: image, pdf, document or any"
			}
		}
		if b.MaxMB < 1 || b.MaxMB > MaxFileMB {
			errs["body.max_mb"] = fmt.Sprintf("1 to %d MB", MaxFileMB)
		}
	case Audio:
		if b.MaxSeconds < 5 || b.MaxSeconds > MaxAudioSeconds {
			errs["body.max_seconds"] = fmt.Sprintf("5 to %d seconds", MaxAudioSeconds)
		}
	case Video:
		if b.MaxSeconds < 5 || b.MaxSeconds > MaxVideoSeconds {
			errs["body.max_seconds"] = fmt.Sprintf("5 to %d seconds", MaxVideoSeconds)
		}
	}
	return errs
}

// FileRef points at an uploaded file answer.
type FileRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int    `json:"size"`
	ContentType string `json:"content_type"`
	// Removed: an admin's clean-up deleted the file; the answer stays (PL-FR-06).
	Removed bool `json:"removed,omitempty"`
}

// Answer is one participant's answer; each type uses some fields.
type Answer struct {
	Selected []string            `json:"selected,omitempty"` // SINGLE, MULTI
	Pairs    map[string]string   `json:"pairs,omitempty"`    // MATCH left→right; DRAG item→zone
	Blanks   map[string]string   `json:"blanks,omitempty"`   // BLANK_OPT blank→option; BLANK_TEXT blank→text
	Order    []string            `json:"order,omitempty"`    // DRAG ranking
	Text     *string             `json:"text,omitempty"`     // ESSAY, SHORT_TEXT, CODE
	Words    []string            `json:"words,omitempty"`    // WORD_CLOUD
	Number   *float64            `json:"number,omitempty"`   // NUMBER, SLIDER, RATING
	Date     string              `json:"date,omitempty"`     // DATE
	Time     string              `json:"time,omitempty"`     // TIME
	Rows     map[string]string   `json:"rows,omitempty"`     // LIKERT row→point ("1".."7"); MATRIX single row→column
	Multi    map[string][]string `json:"multi,omitempty"`    // MATRIX multi row→columns
	Cells    map[string]string   `json:"cells,omitempty"`    // MATRIX text "row|column"→text
	File     *FileRef            `json:"file,omitempty"`     // FILE, AUDIO, VIDEO (set by the upload)
}

// NormaliseWord prepares a word-cloud entry: lower case, punctuation turned
// into spaces (apostrophes and hyphens inside words stay), single-spaced.
// "" means nothing usable.
func NormaliseWord(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\'' || r == '’' || r == '-' {
			return r
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsControl(r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, "'’- ")
}

func inSet(ids []Choice, id string) bool {
	for _, c := range ids {
		if c.ID == id {
			return true
		}
	}
	return false
}

func onStep(v, min, step float64) bool {
	n := (v - min) / step
	return math.Abs(n-math.Round(n)) < 1e-6
}

// CheckAnswer validates and normalises an answer for q. It reports empty=true
// when nothing was answered, which clears a saved answer.
func (q Question) CheckAnswer(a *Answer) (empty bool, err error) {
	bad := func(format string, args ...any) (bool, error) { return false, fmt.Errorf(format, args...) }
	b := q.Body
	switch q.Type {
	case Single, Multi:
		if len(a.Selected) == 0 {
			return true, nil
		}
		seen := map[string]bool{}
		for _, id := range a.Selected {
			if !inSet(b.Options, id) || seen[id] {
				return bad("unknown or repeated option %q", id)
			}
			seen[id] = true
		}
		if q.Type == Single && len(a.Selected) != 1 {
			return bad("choose one option")
		}
		if q.Type == Multi && b.MaxChoices > 0 && len(a.Selected) > b.MaxChoices {
			return bad("choose at most %d options", b.MaxChoices)
		}
	case Match:
		if len(a.Pairs) == 0 {
			return true, nil
		}
		for l, r := range a.Pairs {
			if !inSet(b.Left, l) || !inSet(b.Right, r) {
				return bad("unknown pair %s → %s", l, r)
			}
		}
	case BlankOpt, BlankText:
		out := map[string]string{}
		for k, v := range a.Blanks {
			ok := false
			for _, bl := range b.Blanks {
				ok = ok || bl == k
			}
			if !ok {
				return bad("unknown blank %q", k)
			}
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if q.Type == BlankOpt && !inSet(b.Options, v) {
				return bad("unknown option %q", v)
			}
			if utf8.RuneCountInString(v) > 100 {
				return bad("an answer is too long")
			}
			out[k] = v
		}
		if len(out) == 0 {
			return true, nil
		}
		a.Blanks = out
	case Drag:
		if len(b.Zones) == 0 {
			if len(a.Order) == 0 {
				return true, nil
			}
			if len(a.Order) != len(b.Options) {
				return bad("rank every item")
			}
			seen := map[string]bool{}
			for _, id := range a.Order {
				if !inSet(b.Options, id) || seen[id] {
					return bad("invalid ranking")
				}
				seen[id] = true
			}
		} else {
			if len(a.Pairs) == 0 {
				return true, nil
			}
			for item, zone := range a.Pairs {
				if !inSet(b.Options, item) || !inSet(b.Zones, zone) {
					return bad("unknown item or box")
				}
			}
		}
	case Essay, ShortText, Code:
		if a.Text == nil {
			return true, nil
		}
		t := *a.Text
		if q.Type != Code {
			t = strings.TrimSpace(t)
		} else if strings.TrimSpace(t) == "" {
			t = ""
		}
		if t == "" {
			return true, nil
		}
		limit := b.MaxLength
		if q.Type == Essay {
			limit = 20000
			if b.MaxWords > 0 && len(strings.Fields(t)) > b.MaxWords {
				return bad("at most %d words", b.MaxWords)
			}
		}
		if q.Type == ShortText && strings.ContainsAny(t, "\r\n") {
			t = strings.Join(strings.Fields(t), " ")
		}
		if utf8.RuneCountInString(t) > limit {
			return bad("at most %d characters", limit)
		}
		a.Text = &t
	case WordCloud:
		var words []string
		seen := map[string]bool{}
		for _, w := range a.Words {
			w = NormaliseWord(w)
			if w == "" || seen[w] {
				continue
			}
			if utf8.RuneCountInString(w) > 40 {
				return bad("each entry can be at most 40 characters")
			}
			seen[w] = true
			words = append(words, w)
		}
		if len(words) == 0 {
			return true, nil
		}
		if len(words) > b.MaxEntries {
			return bad("at most %d entries", b.MaxEntries)
		}
		a.Words = words
	case Number, Slider, Rating:
		if a.Number == nil {
			return true, nil
		}
		v := *a.Number
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e12 {
			return bad("not a number")
		}
		if q.Type == Rating {
			if v != math.Trunc(v) || v < 1 || v > float64(b.Points) {
				return bad("choose 1 to %d stars", b.Points)
			}
			break
		}
		if b.Min != nil && v < *b.Min || b.Max != nil && v > *b.Max {
			return bad("the value is out of range")
		}
		if b.Step != nil {
			min := 0.0
			if b.Min != nil {
				min = *b.Min
			}
			if !onStep(v, min, *b.Step) {
				return bad("the value must be in steps of %s", strconv.FormatFloat(*b.Step, 'f', -1, 64))
			}
		}
	case Date:
		if a.Date == "" {
			return true, nil
		}
		if _, err := time.Parse(time.DateOnly, a.Date); err != nil || !dateRe.MatchString(a.Date) {
			return bad("use a date like 2026-10-05")
		}
		if b.From != "" && a.Date < b.From || b.To != "" && a.Date > b.To {
			return bad("the date is out of range")
		}
	case Time:
		if a.Time == "" {
			return true, nil
		}
		if !timeRe.MatchString(a.Time) {
			return bad("use a time like 14:30")
		}
		if b.From != "" && a.Time < b.From || b.To != "" && a.Time > b.To {
			return bad("the time is out of range")
		}
	case Likert:
		if len(a.Rows) == 0 {
			return true, nil
		}
		for row, v := range a.Rows {
			n, err := strconv.Atoi(v)
			if !inSet(b.Rows, row) || err != nil || n < 1 || n > len(b.Scale) {
				return bad("invalid rating for %q", row)
			}
		}
	case Matrix:
		switch b.Mode {
		case "single":
			if len(a.Rows) == 0 {
				return true, nil
			}
			for row, col := range a.Rows {
				if !inSet(b.Rows, row) || !inSet(b.Columns, col) {
					return bad("invalid cell")
				}
			}
		case "multi":
			out := map[string][]string{}
			for row, cols := range a.Multi {
				if !inSet(b.Rows, row) {
					return bad("invalid row")
				}
				seen := map[string]bool{}
				for _, col := range cols {
					if !inSet(b.Columns, col) || seen[col] {
						return bad("invalid cell")
					}
					seen[col] = true
				}
				if len(cols) > 0 {
					out[row] = cols
				}
			}
			if len(out) == 0 {
				return true, nil
			}
			a.Multi = out
		case "text":
			out := map[string]string{}
			for k, v := range a.Cells {
				row, col, ok := strings.Cut(k, "|")
				if !ok || !inSet(b.Rows, row) || !inSet(b.Columns, col) {
					return bad("invalid cell")
				}
				v = strings.TrimSpace(v)
				if utf8.RuneCountInString(v) > 200 {
					return bad("a cell can hold at most 200 characters")
				}
				if v != "" {
					out[k] = v
				}
			}
			if len(out) == 0 {
				return true, nil
			}
			a.Cells = out
		}
	case File, Audio, Video:
		// Set only by the upload endpoint; a JSON answer may only clear it.
		if a.File != nil {
			return bad("upload the file instead")
		}
		return true, nil
	}
	// Keep only the fields this type uses.
	*a = a.only(q.Type, b)
	return false, nil
}

func (a Answer) only(t Type, b Body) Answer {
	switch t {
	case Single, Multi:
		return Answer{Selected: a.Selected}
	case Match:
		return Answer{Pairs: a.Pairs}
	case BlankOpt, BlankText:
		return Answer{Blanks: a.Blanks}
	case Drag:
		if len(b.Zones) == 0 {
			return Answer{Order: a.Order}
		}
		return Answer{Pairs: a.Pairs}
	case Essay, ShortText, Code:
		return Answer{Text: a.Text}
	case WordCloud:
		return Answer{Words: a.Words}
	case Number, Slider, Rating:
		return Answer{Number: a.Number}
	case Date:
		return Answer{Date: a.Date}
	case Time:
		return Answer{Time: a.Time}
	case Likert:
		return Answer{Rows: a.Rows}
	case Matrix:
		switch b.Mode {
		case "multi":
			return Answer{Multi: a.Multi}
		case "text":
			return Answer{Cells: a.Cells}
		}
		return Answer{Rows: a.Rows}
	}
	return a
}
