package quiz_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/quiz"
)

// D-132: questions are marked reviewed and unmarked; changing one clears
// the mark; balancing spreads the right answers over the positions and
// leaves options whose order matters alone.
func TestReviewAndBalance(t *testing.T) {
	_, teacher, _, topic := setup(t)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Pool-like"}, 201, &q)
	var b strings.Builder
	b.WriteString("question_code,type,question_text,options,correct_answer,marks,category\n")
	for i := 1; i <= 12; i++ { // every right answer first, as hand-written files often have it
		fmt.Fprintf(&b, "S%d,SINGLE,Question %d?,right %d|wrong a%d|wrong b%d|wrong c%d,right %d,1,HTML\n", i, i, i, i, i, i, i)
	}
	b.WriteString("A1,SINGLE,Which are tags?,<p>|<div>|<span>|All of the above,All of the above,1,HTML\n")
	b.WriteString("T1,SINGLE,HTML is a programming language.,True|False,False,1,HTML\n")
	b.WriteString("M1,MULTI,Pick the primes,2|3|4|9,2|3,1,HTML\n")
	b.WriteString("C1,SINGLE,Other category?,x|y|z,x,1,CSS\n")
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(b.String()))
	list := func() map[string]quiz.Question {
		var r struct{ Questions []quiz.Question }
		teacher.Call("GET", "/api/teacher/quizzes/"+q.ID+"/questions", nil, 200, &r)
		m := map[string]quiz.Question{}
		for _, x := range r.Questions {
			m[x.Code] = x
		}
		return m
	}
	qs := list()
	if len(qs) != 16 || qs["S1"].ReviewedAt != nil {
		t.Fatalf("imported %d, S1 reviewed %v", len(qs), qs["S1"].ReviewedAt)
	}

	// Reviewing.
	var n struct{ Affected int }
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions/review", map[string]any{"ids": []string{qs["S1"].ID, qs["S2"].ID}, "reviewed": true}, 200, &n)
	if n.Affected != 2 {
		t.Fatalf("reviewed %d", n.Affected)
	}
	qs = list()
	if qs["S1"].ReviewedAt == nil || qs["S2"].ReviewedAt == nil || qs["S3"].ReviewedAt != nil {
		t.Fatal("review marks not kept")
	}
	s2 := qs["S2"]
	teacher.Call("PUT", "/api/teacher/questions/"+s2.ID, map[string]any{"code": s2.Code, "type": s2.Type, "text": "Question 2, corrected?",
		"body": s2.Body, "key": s2.Key, "marks": s2.Marks, "category": s2.Category}, 200, nil)
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions/review", map[string]any{"ids": []string{qs["S1"].ID}, "reviewed": false}, 200, nil)
	if qs = list(); qs["S1"].ReviewedAt != nil || qs["S2"].ReviewedAt != nil {
		t.Fatal("a changed or unmarked question still shows as reviewed")
	}
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions/review", map[string]any{"ids": []string{}, "reviewed": true}, 422, nil)

	// Balancing one category.
	var res quiz.BalanceResult
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions/balance", map[string]any{"category": "html"}, 200, &res)
	if res.Moved != 12 || res.Kept != 2 || res.Shuffled != 1 {
		t.Fatalf("balance = %+v", res)
	}
	qs = list()
	at := make([]int, 4)
	for i := 1; i <= 12; i++ {
		x := qs[fmt.Sprintf("S%d", i)]
		found := false
		for p, o := range x.Body.Options {
			if o.ID == x.Key.Correct[0] {
				at[p]++
				found = true
				if !strings.HasPrefix(o.Text, "right") {
					t.Fatalf("%s: the key points at %q", x.Code, o.Text)
				}
			}
		}
		if !found || len(x.Body.Options) != 4 {
			t.Fatalf("%s lost its right answer: %+v", x.Code, x.Body.Options)
		}
	}
	if at[0] != 3 || at[1] != 3 || at[2] != 3 || at[3] != 3 {
		t.Fatalf("right answers at A, B, C, D = %v, want 3 each", at)
	}
	if o := qs["A1"].Body.Options; o[3].Text != "All of the above" {
		t.Fatalf("“All of the above” moved: %+v", o)
	}
	if o := qs["T1"].Body.Options; o[0].Text != "True" {
		t.Fatalf("True/False reordered: %+v", o)
	}
	if o := qs["C1"].Body.Options; o[0].Text != "x" || o[1].Text != "y" {
		t.Fatalf("another category changed: %+v", o)
	}
}
