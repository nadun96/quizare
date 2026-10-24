package tutorial_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/tutorial"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

const questionsCSV = `question_code,type,question_text,options,correct_answer,marks,time_limit_sec,evaluation,rubric,feedback_correct,feedback_incorrect
Q1,SINGLE,What is the capital of France?,Paris|Rome,Paris,1,,KEY,,Well done,Paris is the capital
Q2,SINGLE,2+2?,4|5,4,1,,KEY,,,
Q3,SINGLE,The powerhouse of the cell?,Mitochondria|Nucleus,Mitochondria,2,,KEY,,,
Q4,ESSAY,Explain photosynthesis,,,5,,MANUAL,,,
`

type fx struct {
	e       *apptest.Env
	teacher *apptest.Client
	topic   content.Topic
	class   content.Classroom
}

func setup(t *testing.T) *fx { return setupWith(t) }

func setupWith(t *testing.T, opts ...apptest.Option) *fx {
	e := apptest.New(t, opts...)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 9"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "Biology"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "Cells"}, 201, &topic)
	return &fx{e: e, teacher: teacher, topic: topic, class: c}
}

func (f *fx) student(t *testing.T) *apptest.Client {
	s := f.e.NewUser(auth.RoleStudent)
	s.Call("POST", "/api/enrolments", map[string]string{"join_code": f.class.JoinCode}, 200, nil)
	return s
}

// tutorial makes a tutorial with the four questions, and returns them by code.
func (f *fx) tutorial(t *testing.T, title string) (tutorial.Tutorial, map[string]quiz.Question) {
	var tu tutorial.Tutorial
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/tutorials", map[string]any{"title": title}, 201, &tu)
	f.teacher.Raw("POST", "/api/teacher/quizzes/"+tu.QuizID+"/questions/import", "text/csv", []byte(questionsCSV))
	var ed tutorial.Editor
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID, nil, 200, &ed)
	qs := map[string]quiz.Question{}
	for _, q := range ed.Questions {
		qs[q.Code] = q
	}
	if len(qs) != 4 {
		t.Fatalf("questions = %d", len(qs))
	}
	return tu, qs
}

func (f *fx) page(t *testing.T, id string, body map[string]any) tutorial.Page {
	t.Helper()
	var p tutorial.Page
	f.teacher.Call("POST", "/api/teacher/tutorials/"+id+"/pages", body, 201, &p)
	return p
}

func code(t *testing.T, c *apptest.Client, method, path string, body any, want int) string {
	t.Helper()
	status, raw := c.Do(method, path, body)
	if status != want {
		t.Fatalf("%s %s: %d, want %d: %s", method, path, status, want, raw)
	}
	var e struct {
		Code   string
		Fields map[string]string
	}
	_ = json.Unmarshal(raw, &e)
	if e.Code == "validation_failed" {
		for k := range e.Fields {
			return k
		}
	}
	return e.Code
}

func pick(id string) map[string]any { return map[string]any{"selected": []string{id}} }

func TestBuildAndPublish(t *testing.T) {
	f := setup(t)
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/tutorials", map[string]any{"title": " "}, 422, nil)
	f.e.NewUser(auth.RoleTeacher).Call("POST", "/api/teacher/topics/"+f.topic.ID+"/tutorials", map[string]any{"title": "Mine"}, 404, nil)
	tu, qs := f.tutorial(t, "  Cells   and organelles ")
	if tu.Title != "Cells and organelles" || tu.Status != "draft" || tu.Feedback != "page" || !tu.AllowBack {
		t.Fatalf("tutorial = %+v", tu)
	}

	// Its questions aren't a quiz: not listed, not run, not deleted on their own.
	var quizzes struct{ Quizzes []quiz.Quiz }
	f.teacher.Call("GET", "/api/teacher/topics/"+f.topic.ID+"/quizzes", nil, 200, &quizzes)
	if len(quizzes.Quizzes) != 0 {
		t.Fatalf("listed: %+v", quizzes.Quizzes)
	}
	if c := code(t, f.teacher, "POST", "/api/teacher/quizzes/"+tu.QuizID+"/status", map[string]any{"status": "ready"}, 409); c != "tutorial_questions" {
		t.Fatal(c)
	}
	if c := code(t, f.teacher, "POST", "/api/teacher/quizzes/"+tu.QuizID+"/sessions", map[string]any{}, 409); c != "tutorial_questions" {
		t.Fatal(c)
	}
	code(t, f.teacher, "DELETE", "/api/teacher/quizzes/"+tu.QuizID, nil, 409)

	// A content page: every kind of block, each turned into what it shows.
	read := f.page(t, tu.ID, map[string]any{"title": "Read this", "blocks": []map[string]any{
		{"type": "text", "text": "Cells are the **building blocks** of life."},
		{"type": "image", "url": "https://drive.google.com/file/d/1AbCdEfGhIjKlMnOp/view", "alt": "A plant cell"},
		{"type": "video", "url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10"},
		{"type": "video", "url": "https://youtu.be/dQw4w9WgXcQ"},
		{"type": "document", "url": "https://docs.google.com/presentation/d/1AbCdEfGhIjKlMnOpQr/edit#slide=1"},
		{"type": "link", "url": "https://en.wikipedia.org/wiki/Cell_(biology)", "text": "More on Wikipedia"},
	}})
	b := read.Blocks
	if b[1].Embed != "https://drive.google.com/thumbnail?id=1AbCdEfGhIjKlMnOp&sz=w1000" || b[2].Embed != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" ||
		b[3].Embed != b[2].Embed || b[4].Embed != "https://docs.google.com/presentation/d/1AbCdEfGhIjKlMnOpQr/preview" || b[5].Text != "More on Wikipedia" {
		t.Fatalf("blocks = %+v", b)
	}
	for field, blocks := range map[string][]map[string]any{
		"blocks.0": {{"type": "video", "url": "https://vimeo.com/123"}},
		"blocks.1": {{"type": "text", "text": "ok"}, {"type": "document", "url": "https://example.com/a.pdf"}},
		"blocks.2": {{"type": "text", "text": "ok"}, {"type": "text", "text": "ok"}, {"type": "link", "url": "javascript:alert(1)"}},
	} {
		if c := code(t, f.teacher, "POST", "/api/teacher/tutorials/"+tu.ID+"/pages", map[string]any{"blocks": blocks}, 422); c != field {
			t.Fatalf("%s: %s", field, c)
		}
	}

	// Question pages; a question goes on one page only, and must be the tutorial's.
	ask := f.page(t, tu.ID, map[string]any{"title": "Check", "question_ids": []string{qs["Q1"].ID, qs["Q2"].ID}, "checkpoint": "answered"})
	if c := code(t, f.teacher, "POST", "/api/teacher/tutorials/"+tu.ID+"/pages", map[string]any{"question_ids": []string{qs["Q1"].ID}}, 422); c != "question_ids" {
		t.Fatal(c)
	}
	other, oqs := f.tutorial(t, "Other")
	code(t, f.teacher, "POST", "/api/teacher/tutorials/"+tu.ID+"/pages", map[string]any{"question_ids": []string{oqs["Q1"].ID}}, 422)
	hard := f.page(t, tu.ID, map[string]any{"question_ids": []string{qs["Q3"].ID}, "checkpoint": "correct", "max_tries": 2})

	// Needing correct answers needs feedback as students go.
	if c := code(t, f.teacher, "PUT", "/api/teacher/tutorials/"+tu.ID, map[string]any{"feedback": "end"}, 422); c != "feedback" {
		t.Fatal(c)
	}

	// Order: questions first, then the reading, then back.
	var ordered struct{ Pages []tutorial.Page }
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+tu.ID+"/page-order", map[string]any{"ids": []string{ask.ID, read.ID, hard.ID}}, 200, &ordered)
	if ordered.Pages[0].ID != ask.ID || ordered.Pages[1].ID != read.ID {
		t.Fatalf("order = %+v", ordered.Pages)
	}
	code(t, f.teacher, "PUT", "/api/teacher/tutorials/"+tu.ID+"/page-order", map[string]any{"ids": []string{ask.ID}}, 422)
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+tu.ID+"/page-order", map[string]any{"ids": []string{read.ID, ask.ID, hard.ID}}, 200, nil)

	// Publishing: not without pages.
	if c := code(t, f.teacher, "POST", "/api/teacher/tutorials/"+other.ID+"/status", map[string]any{"status": "published"}, 409); c != "tutorial_empty" {
		t.Fatal(c)
	}
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/status", map[string]any{"status": "published"}, 200, &tu)
	if tu.Status != "published" || tu.PublishedAt == nil || tu.Pages != 3 {
		t.Fatalf("published = %+v", tu)
	}
	var list struct{ Tutorials []tutorial.Tutorial }
	f.teacher.Call("GET", "/api/teacher/topics/"+f.topic.ID+"/tutorials", nil, 200, &list)
	if len(list.Tutorials) != 2 {
		t.Fatalf("list = %d", len(list.Tutorials))
	}

	// Deleting: fine before anyone starts; after, unpublish instead.
	f.teacher.Call("DELETE", "/api/teacher/tutorials/"+other.ID, nil, 204, nil)
	s := f.student(t)
	s.Call("POST", "/api/tutorials/"+tu.ID+"/open", nil, 200, nil)
	if c := code(t, f.teacher, "DELETE", "/api/teacher/tutorials/"+tu.ID, nil, 409); c != "tutorial_has_runs" {
		t.Fatal(c)
	}
	// Deleting a page keeps its questions in the tutorial.
	f.teacher.Call("DELETE", "/api/teacher/tutorials/"+tu.ID+"/pages/"+hard.ID, nil, 204, nil)
	var ed tutorial.Editor
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID, nil, 200, &ed)
	if len(ed.Pages) != 2 || len(ed.Questions) != 4 {
		t.Fatalf("after page delete: %d pages, %d questions", len(ed.Pages), len(ed.Questions))
	}
}

func TestStudentRun(t *testing.T) {
	f := setup(t)
	tu, qs := f.tutorial(t, "Cells")
	f.page(t, tu.ID, map[string]any{"title": "Read", "blocks": []map[string]any{{"type": "text", "text": "Read me"}}})
	f.page(t, tu.ID, map[string]any{"title": "Check", "question_ids": []string{qs["Q1"].ID, qs["Q2"].ID}, "checkpoint": "answered"})
	f.page(t, tu.ID, map[string]any{"title": "Hard", "question_ids": []string{qs["Q3"].ID}, "checkpoint": "correct", "max_tries": 2})
	s := f.student(t)
	outsider := f.e.NewUser(auth.RoleStudent)

	// Drafts are invisible.
	code(t, s, "POST", "/api/tutorials/"+tu.ID+"/open", nil, 404)
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/status", map[string]any{"status": "published"}, 200, nil)
	code(t, outsider, "POST", "/api/tutorials/"+tu.ID+"/open", nil, 404)
	var mine struct{ Tutorials []tutorial.MyTutorial }
	s.Call("GET", "/api/my/tutorials", nil, 200, &mine)
	if len(mine.Tutorials) != 1 || mine.Tutorials[0].Classroom != "Grade 9" || mine.Tutorials[0].Started {
		t.Fatalf("mine = %+v", mine.Tutorials)
	}

	var v tutorial.RunView
	s.Call("POST", "/api/tutorials/"+tu.ID+"/open", nil, 200, fresh(&v))
	if v.Current != 0 || len(v.Pages) != 3 || v.Page.Blocks[0].Text != "Read me" || !v.Pages[0].Open || v.Pages[1].Open || v.Score == nil {
		t.Fatalf("open = %+v", v)
	}
	// Can't skip a checkpoint, nor peek ahead.
	if c := code(t, s, "POST", "/api/tutorials/"+tu.ID+"/go", map[string]int{"to": 2}, 409); c != "checkpoint" {
		t.Fatal(c)
	}
	code(t, s, "GET", "/api/tutorials/"+tu.ID+"/pages/2", nil, 404)
	s.Call("POST", "/api/tutorials/"+tu.ID+"/go", map[string]int{"to": 1}, 200, fresh(&v))
	if len(v.Page.Questions) != 2 || v.Page.Questions[0].Mark != nil || strings.Contains(mustJSON(v), `"key"`) {
		t.Fatalf("questions page = %+v", v.Page)
	}
	// Only the page you're on; only real ids.
	code(t, s, "POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 2, "responses": map[string]any{qs["Q3"].ID: pick("o1")}}, 409)
	code(t, s, "POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 1, "responses": map[string]any{qs["Q1"].ID: pick("o9")}}, 422)

	// Answer one: the page isn't passed until both are.
	s.Call("POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 1, "responses": map[string]any{qs["Q1"].ID: pick("o1")}}, 200, fresh(&v))
	q1 := v.Page.Questions[0]
	if !q1.Checked || q1.Mark == nil || !q1.Mark.Correct || q1.Mark.Feedback != "Well done" || q1.Mark.Key == nil || v.Page.Passed {
		t.Fatalf("checked Q1 = %+v passed=%v", q1, v.Page.Passed)
	}
	code(t, s, "POST", "/api/tutorials/"+tu.ID+"/go", map[string]int{"to": 2}, 409)
	// Feedback as they go: a checked answer is final.
	if c := code(t, s, "POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 1, "responses": map[string]any{qs["Q1"].ID: pick("o2")}}, 409); c != "answer_locked" {
		t.Fatal(c)
	}
	s.Call("POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 1, "responses": map[string]any{qs["Q2"].ID: pick("o2")}}, 200, fresh(&v))
	if !v.Page.Passed || v.Page.Questions[1].Mark.Correct || v.Page.Questions[1].Mark.Feedback != "" {
		t.Fatalf("Q2 = %+v", v.Page.Questions[1])
	}

	// A correct-answer checkpoint with 2 tries: wrong, try again, wrong: then on.
	s.Call("POST", "/api/tutorials/"+tu.ID+"/go", map[string]int{"to": 2}, 200, fresh(&v))
	s.Call("POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 2, "responses": map[string]any{qs["Q3"].ID: pick("o2")}}, 200, fresh(&v))
	q3 := v.Page.Questions[0]
	if q3.Mark.Correct || q3.Locked || q3.Mark.Key != nil || q3.Tries != 1 || v.Page.Passed {
		t.Fatalf("first try = %+v mark=%+v passed=%v", q3, q3.Mark, v.Page.Passed)
	}
	code(t, s, "POST", "/api/tutorials/"+tu.ID+"/finish", nil, 409)
	s.Call("POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 2, "responses": map[string]any{qs["Q3"].ID: pick("o2")}}, 200, fresh(&v))
	q3 = v.Page.Questions[0]
	if !q3.Locked || q3.Tries != 2 || q3.Mark.Key == nil || !v.Page.Passed {
		t.Fatalf("second try = %+v", q3)
	}
	// Going back is allowed here, and resuming lands on the page you were on.
	s.Call("POST", "/api/tutorials/"+tu.ID+"/go", map[string]int{"to": 0}, 200, nil)
	s.Call("POST", "/api/tutorials/"+tu.ID+"/open", nil, 200, fresh(&v))
	if v.Current != 0 || v.Reached != 2 || !v.Pages[2].Open {
		t.Fatalf("resume = %+v", v)
	}
	code(t, s, "POST", "/api/tutorials/"+tu.ID+"/finish", nil, 409) // from the last page
	s.Call("POST", "/api/tutorials/"+tu.ID+"/go", map[string]int{"to": 2}, 200, nil)
	s.Call("POST", "/api/tutorials/"+tu.ID+"/finish", nil, 200, fresh(&v))
	if !v.Finished || !v.ReadOnly || v.Score.Score != 1 || v.Score.Max != 4 {
		t.Fatalf("finished = %+v score %+v", v, v.Score)
	}
	code(t, s, "POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 2, "responses": map[string]any{qs["Q3"].ID: pick("o1")}}, 409)

	// The teacher's view.
	var pr tutorial.Progress
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID+"/progress", nil, 200, &pr)
	if pr.Pages != 3 || pr.Questions != 3 || pr.MaxScore != 4 || len(pr.Students) != 1 || !pr.Students[0].Finished || pr.Students[0].Score != 1 || pr.Students[0].Answered != 3 {
		t.Fatalf("progress = %+v", pr)
	}
	f.e.NewUser(auth.RoleTeacher).Call("GET", "/api/teacher/tutorials/"+tu.ID+"/progress", nil, 404, nil)

	// Forward only.
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+tu.ID, map[string]any{"allow_back": false}, 200, nil)
	s2 := f.student(t)
	s2.Call("POST", "/api/tutorials/"+tu.ID+"/open", nil, 200, nil)
	s2.Call("POST", "/api/tutorials/"+tu.ID+"/go", map[string]int{"to": 1}, 200, nil)
	if c := code(t, s2, "POST", "/api/tutorials/"+tu.ID+"/go", map[string]int{"to": 0}, 409); c != "forward_only" {
		t.Fatal(c)
	}
	code(t, s2, "GET", "/api/tutorials/"+tu.ID+"/pages/0", nil, 404)
}

func TestFeedbackTimingAndMarking(t *testing.T) {
	f := setup(t)
	tu, qs := f.tutorial(t, "Later")
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+tu.ID, map[string]any{"feedback": "release"}, 200, nil)
	f.page(t, tu.ID, map[string]any{"question_ids": []string{qs["Q1"].ID, qs["Q4"].ID}})
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/status", map[string]any{"status": "published"}, 200, nil)
	s := f.student(t)

	var v tutorial.RunView
	s.Call("POST", "/api/tutorials/"+tu.ID+"/open", nil, 200, fresh(&v))
	s.Call("POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 0, "responses": map[string]any{
		qs["Q1"].ID: pick("o2"), qs["Q4"].ID: map[string]any{"text": "Light makes sugar."},
	}}, 200, fresh(&v))
	if v.Page.Questions[0].Mark != nil || v.Score != nil || v.Page.Questions[0].Locked {
		t.Fatalf("hidden until released: %+v", v)
	}
	// Answers can change until the end when feedback waits.
	s.Call("POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 0, "responses": map[string]any{qs["Q1"].ID: pick("o1")}}, 200, nil)
	s.Call("POST", "/api/tutorials/"+tu.ID+"/finish", nil, 200, fresh(&v))
	if v.Score != nil {
		t.Fatal("still hidden after finishing, until released")
	}

	// The essay waits for the teacher.
	var pr tutorial.Progress
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID+"/progress", nil, 200, &pr)
	st := pr.Students[0]
	if st.Pending != 1 || st.Score != 1 {
		t.Fatalf("progress = %+v", st)
	}
	var run tutorial.RunDetail
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID+"/runs/"+st.RunID, nil, 200, &run)
	if len(run.Answers) != 2 || run.Answers[1].Status != "pending" || run.Answers[1].Response.Text != "Light makes sugar." {
		t.Fatalf("run = %+v", run)
	}
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+tu.ID+"/runs/"+st.RunID+"/answers/"+qs["Q4"].ID, map[string]any{"score": 9}, 422, nil)
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+tu.ID+"/runs/"+st.RunID+"/answers/"+qs["Q4"].ID, map[string]any{"score": 4, "comment": "Mention chlorophyll."}, 200, &run)
	if run.Answers[1].Status != "marked" || run.Answers[1].Score != 4 {
		t.Fatalf("marked = %+v", run.Answers[1])
	}

	// Released: marks, feedback and the teacher's comment show.
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/release", map[string]bool{"released": true}, 200, nil)
	s.Call("GET", "/api/tutorials/"+tu.ID+"/pages/0", nil, 200, fresh(&v))
	if v.Score == nil || v.Score.Score != 5 || v.Score.Max != 6 || v.Page.Questions[1].Mark.Comment != "Mention chlorophyll." || v.Page.Questions[0].Mark.Key == nil {
		t.Fatalf("released = %+v %+v", v.Score, v.Page.Questions)
	}

	// At the end: hidden while working, shown once finished.
	end, eqs := f.tutorial(t, "At the end")
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+end.ID, map[string]any{"feedback": "end", "lock_answers": true}, 200, nil)
	f.page(t, end.ID, map[string]any{"question_ids": []string{eqs["Q2"].ID}})
	f.teacher.Call("POST", "/api/teacher/tutorials/"+end.ID+"/status", map[string]any{"status": "published"}, 200, nil)
	s.Call("POST", "/api/tutorials/"+end.ID+"/open", nil, 200, nil)
	s.Call("POST", "/api/tutorials/"+end.ID+"/answers", map[string]any{"page": 0, "responses": map[string]any{eqs["Q2"].ID: pick("o1")}}, 200, fresh(&v))
	if v.Page.Questions[0].Mark != nil || !v.Page.Questions[0].Locked {
		t.Fatalf("at the end, locked: %+v mark=%+v feedback=%s", v.Page.Questions[0], v.Page.Questions[0].Mark, v.Tutorial.Feedback)
	}
	s.Call("POST", "/api/tutorials/"+end.ID+"/finish", nil, 200, fresh(&v))
	if v.Score == nil || v.Score.Score != 1 || v.Page.Questions[0].Mark == nil {
		t.Fatalf("after finishing: %+v", v.Score)
	}
}

func TestDates(t *testing.T) {
	f := setup(t)
	tu, qs := f.tutorial(t, "Dated")
	f.page(t, tu.ID, map[string]any{"question_ids": []string{qs["Q1"].ID}, "checkpoint": "answered"})
	f.page(t, tu.ID, map[string]any{"blocks": []map[string]any{{"type": "text", "text": "End"}}})
	future := time.Now().Add(time.Hour)
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+tu.ID, map[string]any{"opens_at": future, "due_at": future.Add(-2 * time.Hour)}, 422, nil)
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+tu.ID, map[string]any{"opens_at": future}, 200, nil)
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/status", map[string]any{"status": "published"}, 200, nil)
	s := f.student(t)
	if c := code(t, s, "POST", "/api/tutorials/"+tu.ID+"/open", nil, 403); c != "tutorial_not_open" {
		t.Fatal(c)
	}
	// Past due: readable throughout, nothing more answered.
	past := time.Now().Add(-time.Hour)
	f.teacher.Call("PUT", "/api/teacher/tutorials/"+tu.ID, map[string]any{"clear_dates": true, "due_at": past}, 200, nil)
	var v tutorial.RunView
	s.Call("POST", "/api/tutorials/"+tu.ID+"/open", nil, 200, fresh(&v))
	if !v.PastDue || !v.ReadOnly || !v.Pages[1].Open {
		t.Fatalf("past due = %+v", v)
	}
	s.Call("GET", "/api/tutorials/"+tu.ID+"/pages/1", nil, 200, nil)
	if c := code(t, s, "POST", "/api/tutorials/"+tu.ID+"/answers", map[string]any{"page": 0, "responses": map[string]any{qs["Q1"].ID: pick("o1")}}, 409); c != "tutorial_past_due" {
		t.Fatal(c)
	}

}

// fresh empties v before a response is decoded into it, so fields the
// server leaves out don't keep values from the last response.
func fresh(v *tutorial.RunView) *tutorial.RunView {
	*v = tutorial.RunView{}
	return v
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
