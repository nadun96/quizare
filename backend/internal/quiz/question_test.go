package quiz

import (
	"encoding/json"
	"strings"
	"testing"
)

func parseOne(t *testing.T, header, row string) ParsedRow {
	t.Helper()
	rows, err := ParseQuestionsCSV([]byte(header + "\n" + row + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	return rows[0]
}

const hdr = "question_code,type,question_text,options,correct_answer,marks,time_limit_sec,evaluation"

// The BA §10.3 example rows must import exactly as documented.
func TestBATemplateExamples(t *testing.T) {
	rows, err := ParseQuestionsCSV([]byte(Template))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 8 {
		t.Fatalf("template rows = %d", len(rows))
	}
	for _, r := range rows {
		if len(r.Errors) > 0 {
			t.Errorf("row %d (%s): %v", r.Row, r.Question.Code, r.Errors)
		}
	}
	single := rows[0].Question
	if single.Type != Single || len(single.Body.Options) != 3 || single.Key.Correct[0] != "o1" ||
		*single.Settings.QuestionTimeLimitSec != 30 || single.Feedback.Correct != "Correct!" {
		t.Errorf("SINGLE parsed wrong: %+v", single)
	}
	multi := rows[1].Question
	if strings.Join(multi.Key.Correct, ",") != "o1,o2" || multi.Marks != 2 {
		t.Errorf("MULTI: %+v", multi.Key)
	}
	match := rows[2].Question
	if len(match.Body.Left) != 2 || match.Key.Pairs["l1"] != "r1" || match.Key.Pairs["l2"] != "r2" || !match.PartialCredit {
		t.Errorf("MATCH: %+v %+v", match.Body, match.Key)
	}
	blank := rows[3].Question
	if strings.Join(blank.Body.Blanks, ",") != "1" || blank.Key.Blanks["1"][0] != "100" {
		t.Errorf("BLANK_TEXT: %+v", blank)
	}
	bopt := rows[4].Question
	if bopt.Key.Blanks["1"][0] != "o1" {
		t.Errorf("BLANK_OPT: %+v", bopt.Key)
	}
	order := rows[5].Question
	if strings.Join(order.Key.Order, ",") != "o1,o2,o3" || len(order.Body.Zones) != 0 {
		t.Errorf("DRAG order: %+v", order)
	}
	zones := rows[6].Question
	if len(zones.Body.Zones) != 2 || zones.Key.Pairs["o1"] != "z1" {
		t.Errorf("DRAG zones: %+v %+v", zones.Body, zones.Key)
	}
	essay := rows[7].Question
	if essay.EvaluationMethod() != "llm" || essay.Key.Rubric == "" || essay.Marks != 5 {
		t.Errorf("ESSAY: %+v", essay)
	}
}

func TestCSVRowErrors(t *testing.T) {
	cases := map[string]string{
		"Q1,SINGLE,Pick,A|B,C,1,,KEY":          "correct_answer",
		"Q1,SINGLE,Pick,A|B,A|B,1,,KEY":        "key.correct",
		"Q1,MULTI,Pick,A,A,1,,KEY":             "body.options",
		"Q1,MATCH,M,A|B|C|D,A=C,1,,KEY":        "options",
		"Q1,MATCH,M,A|B||C|D,A=C,1,,KEY":       "key.pairs",
		"Q1,BLANK_TEXT,No blanks,,1=x,1,,":     "text",
		"Q1,BLANK_TEXT,A [[1]] [[2]],,1=x,1,,": "key.blanks.2",
		"Q1,BLANK_OPT,A [[1]],x|y,1=z,1,,":     "correct_answer",
		"Q1,DRAG,Order,a|b|c,a|b,1,,":          "key.order",
		"Q1,ESSAY,Write,,,5,,KEY":              "settings.evaluation_method",
		"Q1,SINGLE,Pick,A|B,A,1,,LLM":          "settings.evaluation_method",
		"Q1,SINGLE,Pick,A|B,A,zero,,":          "marks",
		"Q1,SINGLE,Pick,A|B,A,1,-5,":           "time_limit_sec",
		"Q1,SINGLE,Pick,A|B,A,1,,MAGIC":        "evaluation",
		"Q1,WHAT,Pick,A|B,A,1,,":               "type",
		"bad code!,SINGLE,Pick,A|B,A,1,,":      "code",
		",SINGLE,Pick,A|B,A,1,,":               "code",
		"Q1,SINGLE,,A|B,A,1,,":                 "text",
		"Q1,SINGLE,Pick,A|A,A,1,,":             "body.options",
	}
	for row, field := range cases {
		r := parseOne(t, hdr, row)
		if r.Errors[field] == "" {
			t.Errorf("%q: want error on %q, got %v", row, field, r.Errors)
		}
	}
}

func TestBlankTextWithoutKeyAllowedForLLM(t *testing.T) {
	r := parseOne(t, hdr, "Q1,BLANK_TEXT,Explain [[1]],,,1,,LLM")
	if len(r.Errors) != 0 {
		t.Fatalf("errors: %v", r.Errors)
	}
}

func TestCSVFileLevelErrors(t *testing.T) {
	if _, err := ParseQuestionsCSV([]byte("type,question_text\nSINGLE,x\n")); err == nil {
		t.Error("missing question_code column accepted")
	}
	if _, err := ParseQuestionsCSV([]byte{0xff, 0xfe, 'a'}); err == nil {
		t.Error("non-UTF-8 accepted")
	}
	big := hdr + "\n" + strings.Repeat("Q1,SINGLE,x,A|B,A,1,,\n", MaxCSVRows+1)
	if _, err := ParseQuestionsCSV([]byte(big)); err == nil {
		t.Error("row limit not enforced")
	}
	// Excel's BOM and blank lines are tolerated.
	rows, err := ParseQuestionsCSV([]byte("\xef\xbb\xbf" + hdr + "\n\nQ1,SINGLE,x,A|B,A,1,,\n,,,,,,,\n"))
	if err != nil || len(rows) != 1 || rows[0].Row != 3 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestEscapedPipe(t *testing.T) {
	r := parseOne(t, hdr, `Q1,SINGLE,Which is a pipe?,a\|b|c,a\|b,1,,`)
	if len(r.Errors) > 0 || r.Question.Body.Options[0].Text != "a|b" {
		t.Fatalf("%+v %v", r.Question.Body, r.Errors)
	}
}

func TestStudentViewHasNoAnswerKey(t *testing.T) {
	rows, _ := ParseQuestionsCSV([]byte(Template))
	for _, r := range rows {
		q := r.Question
		q.Resources = []Resource{{Role: RoleQuestion, N: 1, URL: "https://x/a.png", SourceURL: "src"}, {Role: RoleFeedback, N: 1, URL: "https://x/f.png"}}
		raw, _ := json.Marshal(q.StudentView())
		s := string(raw)
		for _, leak := range []string{`"key"`, `"correct"`, `"pairs"`, `"rubric"`, `"feedback"`, "Correct!", "light energy", "f.png", "source_url"} {
			if strings.Contains(s, leak) {
				t.Errorf("%s: student view leaks %s: %s", q.Code, leak, s)
			}
		}
	}
}

func TestParseResourceName(t *testing.T) {
	code, role, n, err := ParseResourceName("Q001_Q_1")
	if err != nil || code != "Q001" || role != RoleQuestion || n != 1 {
		t.Fatalf("%s %s %d %v", code, role, n, err)
	}
	code, role, n, err = ParseResourceName("UNIT_2_Q7_o_3") // codes may contain underscores
	if err != nil || code != "UNIT_2_Q7" || role != RoleOption || n != 3 {
		t.Fatalf("%s %s %d %v", code, role, n, err)
	}
	for _, bad := range []string{"Q001", "Q001_X_1", "Q001_Q_0", "Q001_Q_x"} {
		if _, _, _, err := ParseResourceName(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestParseResourcesCSVFromBA(t *testing.T) {
	rows, err := ParseResourcesCSV([]byte("resource_name,url,alt_text\nQ001_Q_1,https://drive.google.com/uc?id=abc123,Map of France\nQ002_O_3,https://drive.google.com/uc?id=def456,Number four illustration\n"))
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	if rows[1].Code != "Q002" || rows[1].Role != RoleOption || rows[1].N != 3 || rows[1].AltText != "Number four illustration" {
		t.Fatalf("%+v", rows[1])
	}
}
