package eval

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/quiz"
)

func templateQuestions(t *testing.T) map[string]quiz.Question {
	t.Helper()
	rows, err := quiz.ParseQuestionsCSV([]byte(quiz.Template))
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]quiz.Question{}
	for _, r := range rows {
		m[r.Question.Code] = r.Question
	}
	return m
}

func TestMarkSingle(t *testing.T) {
	q := templateQuestions(t)["Q001"] // Paris|Rome|Madrid, key o1
	if r := MarkByKey(q, &quiz.Response{Selected: []string{"o1"}}); r.Score != 1 || !r.Correct {
		t.Errorf("correct: %+v", r)
	}
	if r := MarkByKey(q, &quiz.Response{Selected: []string{"o2"}}); r.Score != 0 || r.Correct || !r.Answered {
		t.Errorf("wrong: %+v", r)
	}
	if r := MarkByKey(q, nil); r.Answered || r.Score != 0 || r.Max != 1 {
		t.Errorf("unanswered: %+v", r)
	}
	q.NegativeMarks = 0.25
	if r := MarkByKey(q, &quiz.Response{Selected: []string{"o3"}}); r.Score != -0.25 {
		t.Errorf("negative marking: %+v", r)
	}
	if r := MarkByKey(q, &quiz.Response{}); r.Score != 0 {
		t.Errorf("unanswered must not be penalised: %+v", r)
	}
}

func TestMarkMulti(t *testing.T) {
	q := templateQuestions(t)["Q002"] // 2|3|4|9 key o1,o2; marks 2
	if r := MarkByKey(q, &quiz.Response{Selected: []string{"o2", "o1"}}); r.Score != 2 || !r.Correct {
		t.Errorf("exact: %+v", r)
	}
	if r := MarkByKey(q, &quiz.Response{Selected: []string{"o1"}}); r.Score != 0 {
		t.Errorf("no partial credit by default: %+v", r)
	}
	q.PartialCredit = true
	if r := MarkByKey(q, &quiz.Response{Selected: []string{"o1"}}); r.Score != 1 {
		t.Errorf("half: %+v", r)
	}
	if r := MarkByKey(q, &quiz.Response{Selected: []string{"o1", "o3"}}); r.Score != 0 {
		t.Errorf("a wrong pick cancels a right one: %+v", r)
	}
	if r := MarkByKey(q, &quiz.Response{Selected: []string{"o1", "o2", "o3", "o4"}}); r.Score != 0 {
		t.Errorf("selecting everything must not pay: %+v", r)
	}
}

func TestMarkMatchBlankDrag(t *testing.T) {
	qs := templateQuestions(t)
	match := qs["Q003"] // partial by default, marks 2
	if r := MarkByKey(match, &quiz.Response{Pairs: map[string]string{"l1": "r1", "l2": "r1"}}); r.Score != 1 || r.Correct {
		t.Errorf("match half: %+v", r)
	}
	blank := qs["Q004"] // 1=100
	for _, got := range []string{"100", " 100 ", "100"} {
		if r := MarkByKey(blank, &quiz.Response{Blanks: map[string]string{"1": got}}); !r.Correct {
			t.Errorf("blank %q: %+v", got, r)
		}
	}
	bopt := qs["Q005"]
	if r := MarkByKey(bopt, &quiz.Response{Blanks: map[string]string{"1": "o1"}}); !r.Correct {
		t.Errorf("blank opt: %+v", r)
	}
	order := qs["Q006"] // Atom|Cell|Organ
	if r := MarkByKey(order, &quiz.Response{Order: []string{"o1", "o2", "o3"}}); !r.Correct {
		t.Errorf("order exact: %+v", r)
	}
	if r := MarkByKey(order, &quiz.Response{Order: []string{"o1", "o3", "o2"}}); r.Fraction < 0.33 || r.Fraction > 0.34 {
		t.Errorf("order partial: %+v", r)
	}
	zones := qs["Q007"]
	if r := MarkByKey(zones, &quiz.Response{Pairs: map[string]string{"o1": "z1", "o2": "z2"}}); r.Score != 2 {
		t.Errorf("zones: %+v", r)
	}
}

func TestBlankTextToleranceAndCase(t *testing.T) {
	q := quiz.Question{Type: quiz.BlankText, Text: "[[1]]", Marks: 1,
		Body: quiz.Body{Blanks: []string{"1"}},
		Key:  quiz.Key{Blanks: map[string][]string{"1": {"photosynthesis", "Photo synthesis"}}, Tolerance: 2}}
	cases := map[string]bool{
		"Photosynthesis":    true,
		"photosinthesis":    true, // 1 edit
		"photosinthesys":    true, // 2 edits
		"fotosinthesys":     false,
		"photo   synthesis": true,
		"":                  false,
	}
	for got, want := range cases {
		if r := MarkByKey(q, &quiz.Response{Blanks: map[string]string{"1": got}}); r.Correct != want {
			t.Errorf("%q: correct=%v want %v", got, r.Correct, want)
		}
	}
	q.Key.CaseSensitive = true
	q.Key.Tolerance = 0
	if r := MarkByKey(q, &quiz.Response{Blanks: map[string]string{"1": "PHOTOSYNTHESIS"}}); r.Correct {
		t.Error("case-sensitive key accepted wrong case")
	}
	short := quiz.Question{Type: quiz.BlankText, Text: "[[1]]", Marks: 1, Body: quiz.Body{Blanks: []string{"1"}},
		Key: quiz.Key{Blanks: map[string][]string{"1": {"cat"}}, Tolerance: 1}}
	if r := MarkByKey(short, &quiz.Response{Blanks: map[string]string{"1": "bat"}}); r.Correct {
		t.Error("tolerance applied to a 3-letter answer")
	}
}

func TestPredefinedFeedback(t *testing.T) {
	q := templateQuestions(t)["Q001"]
	q.Feedback.Options = map[string]string{"o2": "Rome is in Italy."}
	q.Feedback.Bands = []quiz.Band{{MinPct: 0, MaxPct: 49, Text: "Revise capitals."}, {MinPct: 50, MaxPct: 100, Text: "Well done."}}
	got := PredefinedFeedback(q, &quiz.Response{Selected: []string{"o2"}}, 0, false)
	want := "Rome is in Italy.\nParis is the capital of France.\nRevise capitals."
	if got != want {
		t.Errorf("wrong answer feedback:\n%q\nwant\n%q", got, want)
	}
	if got := PredefinedFeedback(q, &quiz.Response{Selected: []string{"o1"}}, 1, true); got != "Correct!\nWell done." {
		t.Errorf("right answer feedback: %q", got)
	}
}
