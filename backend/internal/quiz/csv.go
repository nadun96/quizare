package quiz

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CSV import limits (ADR-16: size and row limits, strict parsing).
const (
	MaxCSVBytes = 2 << 20
	MaxCSVRows  = 1000
)

// TemplateColumns is the BA §10.3 template plus optional extension columns.
var TemplateColumns = []string{
	"question_code", "type", "question_text", "options", "correct_answer", "marks",
	"time_limit_sec", "evaluation", "rubric", "feedback_correct", "feedback_incorrect",
	"negative_marks", "partial_credit", "word_limit", "case_sensitive", "tolerance",
}

// Template is the downloadable CSV template, with one example per type.
const Template = `question_code,type,question_text,options,correct_answer,marks,time_limit_sec,evaluation,rubric,feedback_correct,feedback_incorrect
Q001,SINGLE,What is the capital of France?,Paris|Rome|Madrid,Paris,1,30,KEY,,Correct!,Paris is the capital of France.
Q002,MULTI,Select the prime numbers,2|3|4|9,2|3,2,45,KEY,,,
Q003,MATCH,Match the country to its capital,France|Japan||Paris|Tokyo,France=Paris|Japan=Tokyo,2,60,KEY,,,
Q004,BLANK_TEXT,Water boils at [[1]] degrees Celsius,,1=100,1,30,KEY,,,
Q005,BLANK_OPT,The [[1]] is the largest planet,Jupiter|Mars|Venus,1=Jupiter,1,30,KEY,,,
Q006,DRAG,Put these in order from smallest to largest,Atom|Cell|Organ,Atom|Cell|Organ,1,60,KEY,,,
Q007,DRAG,Sort each animal into its class,Frog|Eagle||Amphibian|Bird,Frog=Amphibian|Eagle=Bird,2,60,KEY,,,
Q008,ESSAY,Explain photosynthesis in 100 words,,,5,300,LLM,Award marks for light energy; chlorophyll; glucose and oxygen,,
`

// RowError describes one rejected CSV row.
type RowError struct {
	Row    int               `json:"row"` // 1-based line number; the header is row 1
	Code   string            `json:"code"`
	Errors map[string]string `json:"errors"`
}

// ParsedRow is a CSV row turned into a question (possibly invalid).
type ParsedRow struct {
	Row      int
	Question Question
	Errors   map[string]string
}

// ParseQuestionsCSV parses a BA §10.3 file. File-level problems return an
// error; row-level problems are reported per row so valid rows can still be
// imported (FR-QZ-05, AC-09).
func ParseQuestionsCSV(data []byte) ([]ParsedRow, error) {
	header, records, err := readCSV(data)
	if err != nil {
		return nil, err
	}
	for _, req := range []string{"question_code", "type", "question_text"} {
		if _, ok := header[req]; !ok {
			return nil, fmt.Errorf("missing required column %q", req)
		}
	}
	out := make([]ParsedRow, 0, len(records))
	for _, rec := range records {
		get := rec.getter(header)
		q, errs := questionFromRow(get)
		if len(errs) == 0 {
			q.Normalise()
			errs = q.Validate()
		}
		out = append(out, ParsedRow{Row: rec.line, Question: q, Errors: errs})
	}
	return out, nil
}

type csvRecord struct {
	line   int
	fields []string
}

func readCSV(data []byte) (map[string]int, []csvRecord, error) {
	if len(data) > MaxCSVBytes {
		return nil, nil, fmt.Errorf("file is larger than %d MB", MaxCSVBytes>>20)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // Excel writes a UTF-8 BOM
	if !utf8.Valid(data) {
		return nil, nil, errors.New("file must be UTF-8 encoded")
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	var header map[string]int
	var records []csvRecord
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("invalid CSV: %w", err)
		}
		line, _ := r.FieldPos(0)
		if header == nil {
			header = map[string]int{}
			for i, h := range rec {
				header[strings.ToLower(strings.TrimSpace(h))] = i
			}
			continue
		}
		if strings.TrimSpace(strings.Join(rec, "")) == "" { // skip blank lines
			continue
		}
		if len(records) == MaxCSVRows {
			return nil, nil, fmt.Errorf("at most %d rows per file", MaxCSVRows)
		}
		records = append(records, csvRecord{line: line, fields: rec})
	}
	if header == nil {
		return nil, nil, errors.New("file is empty")
	}
	return header, records, nil
}

func (rec csvRecord) getter(header map[string]int) func(string) string {
	return func(col string) string {
		if idx, ok := header[col]; ok && idx < len(rec.fields) {
			return strings.TrimSpace(rec.fields[idx])
		}
		return ""
	}
}

// splitList splits on '|' with '\|' as an escaped literal pipe.
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case s[i] == '|':
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(out, strings.TrimSpace(cur.String()))
}

// splitGroups splits "a|b||c|d" into [a b] and [c d] (MATCH, DRAG zones).
func splitGroups(s string) ([]string, []string, bool) {
	parts := splitList(s)
	for i, p := range parts {
		if p == "" {
			return parts[:i], parts[i+1:], true
		}
	}
	return parts, nil, false
}

func choices(texts []string) []Choice {
	cs := make([]Choice, len(texts))
	for i, t := range texts {
		cs[i] = Choice{ID: fmt.Sprintf("o%d", i+1), Text: t}
	}
	return cs
}

func byText(cs []Choice) map[string]string {
	m := map[string]string{}
	for _, c := range cs {
		m[strings.ToLower(c.Text)] = c.ID
	}
	return m
}

func splitPair(s string) (string, string, bool) {
	k, v, ok := strings.Cut(s, "=")
	return strings.TrimSpace(k), strings.TrimSpace(v), ok && strings.TrimSpace(k) != ""
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "1", "y", "yes", "true":
		return true, true
	case "0", "n", "no", "false":
		return false, true
	}
	return false, false
}

func questionFromRow(get func(string) string) (Question, map[string]string) {
	f := map[string]string{}
	q := Question{Code: get("question_code"), Type: Type(strings.ToUpper(get("type"))), Text: get("question_text")}
	q.PartialCredit = DefaultPartialCredit(q.Type)

	num := func(col string, dst *float64) {
		if v := get(col); v != "" {
			x, err := strconv.ParseFloat(v, 64)
			if err != nil {
				f[col] = "must be a number"
			}
			*dst = x
		}
	}
	integer := func(col string) *int {
		v := get(col)
		if v == "" {
			return nil
		}
		x, err := strconv.Atoi(v)
		if err != nil || x < 0 {
			f[col] = "must be a whole number of zero or more"
			return nil
		}
		return &x
	}
	num("marks", &q.Marks)
	num("negative_marks", &q.NegativeMarks)
	if v := get("partial_credit"); v != "" {
		b, ok := parseBool(v)
		if !ok {
			f["partial_credit"] = "use yes or no"
		}
		q.PartialCredit = b
	}
	if tl := integer("time_limit_sec"); tl != nil && *tl > 0 {
		q.Settings.QuestionTimeLimitSec = tl
	}
	switch ev := strings.ToLower(get("evaluation")); ev {
	case "":
		if q.Type == Essay {
			llm := "llm" // BA §10.3: defaults by type; BR-11 falls back to manual without a key
			q.Settings.EvaluationMethod = &llm
		}
	case "key", "llm", "manual":
		q.Settings.EvaluationMethod = &ev
	default:
		f["evaluation"] = "must be KEY, LLM or MANUAL"
	}
	q.Key.Rubric = get("rubric")
	q.Feedback.Correct = get("feedback_correct")
	q.Feedback.Incorrect = get("feedback_incorrect")
	if wl := integer("word_limit"); wl != nil {
		q.Body.WordLimit = *wl
	}
	if v := get("case_sensitive"); v != "" {
		b, ok := parseBool(v)
		if !ok {
			f["case_sensitive"] = "use yes or no"
		}
		q.Key.CaseSensitive = b
	}
	if tol := integer("tolerance"); tol != nil {
		q.Key.Tolerance = *tol
	}

	opts, correct := get("options"), get("correct_answer")
	switch q.Type {
	case Single, Multi:
		q.Body.Options = choices(splitList(opts))
		ids := byText(q.Body.Options)
		for _, c := range splitList(correct) {
			id, ok := ids[strings.ToLower(c)]
			if !ok {
				f["correct_answer"] = fmt.Sprintf("%q is not one of the options", c)
				continue
			}
			q.Key.Correct = append(q.Key.Correct, id)
		}
	case Match, Drag:
		left, right, grouped := splitGroups(opts)
		if q.Type == Match && !grouped {
			f["options"] = "list left items, then '||', then right items (e.g. France|Japan||Paris|Tokyo)"
			break
		}
		items := choices(left)
		if q.Type == Match {
			q.Body.Left = relabel(items, "l")
			q.Body.Right = relabel(choices(right), "r")
		} else {
			q.Body.Options = items
			if grouped {
				q.Body.Zones = relabel(choices(right), "z")
			}
		}
		if q.Type == Drag && !grouped {
			ids := byText(items)
			for _, c := range splitList(correct) {
				id, ok := ids[strings.ToLower(c)]
				if !ok {
					f["correct_answer"] = fmt.Sprintf("%q is not one of the items", c)
				}
				q.Key.Order = append(q.Key.Order, id)
			}
			break
		}
		from := byText(q.Body.Left)
		to := byText(q.Body.Right)
		if q.Type == Drag {
			from, to = byText(q.Body.Options), byText(q.Body.Zones)
		}
		q.Key.Pairs = map[string]string{}
		for _, p := range splitList(correct) {
			l, r, ok := splitPair(p)
			lid, lok := from[strings.ToLower(l)]
			rid, rok := to[strings.ToLower(r)]
			if !ok || !lok || !rok {
				f["correct_answer"] = fmt.Sprintf("pair %q must be left=right using listed items", p)
				continue
			}
			q.Key.Pairs[lid] = rid
		}
	case BlankOpt, BlankText:
		if q.Type == BlankOpt {
			q.Body.Options = choices(splitList(opts))
		}
		ids := byText(q.Body.Options)
		q.Key.Blanks = map[string][]string{}
		for _, p := range splitList(correct) {
			blank, ans, ok := splitPair(p)
			if !ok || ans == "" {
				f["correct_answer"] = fmt.Sprintf("%q must look like 1=answer", p)
				continue
			}
			if q.Type == BlankOpt {
				id, ok := ids[strings.ToLower(ans)]
				if !ok {
					f["correct_answer"] = fmt.Sprintf("%q is not one of the options", ans)
					continue
				}
				ans = id
			}
			q.Key.Blanks[blank] = append(q.Key.Blanks[blank], ans)
		}
	case Essay:
		q.Key.ModelAnswer = correct
	}
	return q, f
}

func relabel(cs []Choice, prefix string) []Choice {
	for i := range cs {
		cs[i].ID = fmt.Sprintf("%s%d", prefix, i+1)
	}
	return cs
}

// ---------- resource mapping CSV (BA §10.4) ----------

type ResourceRow struct {
	Row     int
	Code    string
	Role    Role
	N       int
	URL     string
	AltText string
	Errors  map[string]string
}

// ParseResourceName splits "<question_code>_<role>_<n>" (the code itself may
// contain underscores, so it is parsed from the right).
func ParseResourceName(name string) (code string, role Role, n int, err error) {
	parts := strings.Split(strings.TrimSpace(name), "_")
	if len(parts) < 3 {
		return "", "", 0, errors.New("name must look like Q001_Q_1 (<question_code>_<Q|O|F>_<n>)")
	}
	n, convErr := strconv.Atoi(parts[len(parts)-1])
	role = Role(strings.ToUpper(parts[len(parts)-2]))
	code = strings.Join(parts[:len(parts)-2], "_")
	if convErr != nil || n < 1 || n > 50 {
		return "", "", 0, errors.New("the number after the role must be 1-50")
	}
	if !role.Valid() {
		return "", "", 0, errors.New("role must be Q (question), O (option) or F (feedback)")
	}
	return code, role, n, nil
}

func ParseResourcesCSV(data []byte) ([]ResourceRow, error) {
	header, records, err := readCSV(data)
	if err != nil {
		return nil, err
	}
	for _, req := range []string{"resource_name", "url"} {
		if _, ok := header[req]; !ok {
			return nil, fmt.Errorf("missing required column %q", req)
		}
	}
	out := make([]ResourceRow, 0, len(records))
	for _, rec := range records {
		get := rec.getter(header)
		row := ResourceRow{Row: rec.line, URL: get("url"), AltText: get("alt_text"), Errors: map[string]string{}}
		var perr error
		row.Code, row.Role, row.N, perr = ParseResourceName(get("resource_name"))
		if perr != nil {
			row.Errors["resource_name"] = perr.Error()
		}
		if len(row.AltText) > 500 {
			row.Errors["alt_text"] = "at most 500 characters"
		}
		out = append(out, row)
	}
	return out, nil
}
