package live_test

import (
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/quiz"
)

const pooledQuiz = `question_code,type,question_text,options,correct_answer,marks,category
E1,SINGLE,Easy one,A|B,A,1,Easy
E2,SINGLE,Easy two,A|B,A,1,Easy
E3,SINGLE,Easy three,A|B,A,1,Easy
E4,SINGLE,Easy four,A|B,A,1,Easy
H1,SINGLE,Hard one,A|B,A,3,Hard
H2,SINGLE,Hard two,A|B,A,3,Hard
F1,ESSAY,Always asked,,,5,
`

const bankCSV = `question_code,type,question_text,options,correct_answer,marks,category
P1,SINGLE,Plants one,A|B,A,2,Plants
P2,SINGLE,Plants two,A|B,A,2,Plants
P3,SINGLE,Plants three,A|B,A,2,Plants
M1,SINGLE,Matter one,A|B,A,2,Matter
`

// D-66: categories make pools; a quiz draws from its own pools and from the
// teacher's question bank, and questions in a category without a rule are
// always asked.
func TestQuestionPoolsAndBank(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Pooled"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(pooledQuiz))

	// The bank: one per teacher, created on first use, filled like a quiz.
	var bank quiz.Quiz
	teacher.Call("GET", "/api/teacher/bank", nil, 200, &bank)
	var again quiz.Quiz
	teacher.Call("GET", "/api/teacher/bank", nil, 200, &again)
	if bank.Kind != quiz.KindBank || bank.ID != again.ID || bank.TopicID != "" {
		t.Fatalf("bank: %+v / %s", bank, again.ID)
	}
	teacher.Raw("POST", "/api/teacher/quizzes/"+bank.ID+"/questions/import", "text/csv", []byte(bankCSV))
	// It is never a quiz of its own.
	if code, _ := teacher.Do("POST", "/api/teacher/quizzes/"+bank.ID+"/status", map[string]any{"status": "ready"}); code != 409 {
		t.Fatalf("bank marked ready: %d", code)
	}
	if code, _ := teacher.Do("POST", "/api/teacher/quizzes/"+bank.ID+"/sessions", map[string]any{}); code != 409 {
		t.Fatalf("bank run as a session: %d", code)
	}
	if code, _ := teacher.Do("DELETE", "/api/teacher/quizzes/"+bank.ID, nil); code != 409 {
		t.Fatalf("bank deleted: %d", code)
	}
	// Another teacher can't see or draw from it.
	other := e.NewUser(auth.RoleTeacher)
	if code, _ := other.Do("GET", "/api/teacher/quizzes/"+bank.ID+"/questions", nil); code != 404 {
		t.Fatalf("someone else's bank: %d", code)
	}

	// A category set through the editor (PUT) moves a question between pools.
	var qs struct{ Questions []quiz.Question }
	teacher.Call("GET", "/api/teacher/quizzes/"+q.ID+"/questions", nil, 200, &qs)
	h2 := qs.Questions[5]
	if h2.Code != "H2" || h2.Category != "Hard" {
		t.Fatalf("CSV category: %+v", h2)
	}
	edit := func(cat string) quiz.Question {
		var saved quiz.Question
		teacher.Call("PUT", "/api/teacher/questions/"+h2.ID, map[string]any{"code": h2.Code, "type": h2.Type, "text": h2.Text, "body": h2.Body, "key": h2.Key, "marks": h2.Marks, "category": cat}, 200, &saved)
		return saved
	}
	if saved := edit("  Very   Hard "); saved.Category != "Very Hard" {
		t.Fatalf("category not normalised: %q", saved.Category)
	}
	edit("Hard")

	var info quiz.PoolInfo
	teacher.Call("GET", "/api/teacher/quizzes/"+q.ID+"/pools", nil, 200, &info)
	cats := map[string]int{}
	for _, x := range info.Quiz {
		cats["quiz:"+x.Category] = x.Count
	}
	for _, x := range info.Bank {
		cats["bank:"+x.Category] = x.Count
	}
	if cats["quiz:Easy"] != 4 || cats["quiz:Hard"] != 2 || cats["quiz:"] != 1 || cats["bank:Plants"] != 3 || cats["bank:Matter"] != 1 {
		t.Fatalf("categories: %v", cats)
	}

	// Bad rules are refused; a pool that asks for more than it has blocks Ready.
	if code, _ := teacher.Do("PUT", "/api/teacher/quizzes/"+q.ID+"/pools", map[string]any{"pools": []map[string]any{{"source": "elsewhere", "category": "Easy", "count": 1}}}); code != 422 {
		t.Fatalf("unknown source: %d", code)
	}
	teacher.Call("PUT", "/api/teacher/quizzes/"+q.ID+"/pools", map[string]any{"pools": []map[string]any{{"source": "quiz", "category": "Easy", "count": 9}}}, 200, nil)
	if code, body := teacher.Do("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}); code != 409 || !strings.Contains(string(body), "Easy draws 9 but has only 4") {
		t.Fatalf("over-drawn pool marked ready: %d %s", code, body)
	}

	// 2 of Easy, both Hard (0 = all), 2 of the bank's Plants; F1 (no category, no rule) always.
	teacher.Call("PUT", "/api/teacher/quizzes/"+q.ID+"/pools", map[string]any{"pools": []map[string]any{
		{"source": "quiz", "category": "easy", "count": 2},
		{"source": "quiz", "category": "Hard", "count": 0},
		{"source": "bank", "category": "Plants", "count": 2},
	}}, 200, nil)
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess live.Session
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)

	sets := map[string]bool{}
	for i := 0; i < 10; i++ {
		s := e.NewUser(auth.RoleStudent)
		var st live.StudentState
		s.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{}, 200, &st)
		teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
		if st.Total != 7 {
			t.Fatalf("student %d has %d questions, want 2 easy + 2 hard + 2 bank + 1 fixed", i, st.Total)
		}
		var codes []string
		for {
			codes = append(codes, st.Question.Code)
			if st.Index == st.Total-1 {
				break
			}
			s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
		}
		got := map[byte]int{}
		for _, code := range codes {
			if strings.HasPrefix(code, "B-") {
				got['B']++
				if !strings.HasPrefix(code, "B-P") {
					t.Fatalf("drew a bank question outside Plants: %v", codes)
				}
			} else {
				got[code[0]]++
			}
		}
		if got['E'] != 2 || got['H'] != 2 || got['B'] != 2 || got['F'] != 1 {
			t.Fatalf("student %d drew %v", i, codes)
		}
		sets[strings.Join(codes, ",")] = true
	}
	if len(sets) < 3 {
		t.Fatalf("10 students drew only %d different sets", len(sets))
	}

	// Editing the bank later doesn't change a session that has started.
	teacher.Raw("POST", "/api/teacher/quizzes/"+bank.ID+"/questions/import", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks,category\nP9,SINGLE,Late,A|B,A,2,Plants\n"))
	var d live.Dashboard
	teacher.Call("GET", "/api/teacher/sessions/"+sess.ID, nil, 200, &d)
	for _, r := range d.Rows {
		if r.Total != 7 {
			t.Fatalf("a running attempt changed: %+v", r)
		}
	}
}
