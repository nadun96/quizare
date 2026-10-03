package quiz_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/quiz"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

// setup creates a teacher with classroom → module → topic → quiz.
func setup(t *testing.T) (*apptest.Env, *apptest.Client, quiz.Quiz, content.Topic) {
	t.Helper()
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C", "settings": map[string]any{"pass_mark_pct": 60}}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T", "settings": map[string]any{"quiz_time_limit_sec": 900}}, 201, &topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Quiz 1", "settings": map[string]any{"question_order": "shuffled"}}, 201, &q)
	return e, teacher, q, topic
}

func TestQuizEffectiveSettingsInherit(t *testing.T) {
	_, teacher, q, _ := setup(t)
	if q.Status != "draft" || q.Effective == nil {
		t.Fatalf("quiz = %+v", q)
	}
	eff := q.Effective
	if eff.PassMarkPct != 60 || eff.QuizTimeLimitSec != 900 || eff.QuestionOrder != "shuffled" || eff.CountdownSeconds != 60 {
		t.Fatalf("effective = %+v", eff)
	}
	teacher.Call("PATCH", "/api/teacher/quizzes/"+q.ID, map[string]any{"settings": map[string]any{"quiz_time_limit_sec": 300}}, 200, &q)
	if q.Effective.QuizTimeLimitSec != 300 {
		t.Fatalf("quiz level must override topic: %d", q.Effective.QuizTimeLimitSec)
	}
	teacher.Call("PATCH", "/api/teacher/quizzes/"+q.ID, map[string]any{"settings": map[string]any{"student_id_required": true}}, 422, nil)
}

func TestQuestionCRUDDuplicateReorder(t *testing.T) {
	_, teacher, q, _ := setup(t)
	single := map[string]any{
		"code": "Q1", "type": "SINGLE", "text": "Capital of France?",
		"body":     map[string]any{"options": []map[string]string{{"text": "Paris"}, {"text": "Rome"}}},
		"key":      map[string]any{"correct": []string{"o1"}},
		"settings": map[string]any{"question_time_limit_sec": 30},
	}
	var q1 quiz.Question
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions", single, 201, &q1)
	if q1.Marks != 1 || q1.Body.Options[0].ID != "o1" || *q1.Settings.QuestionTimeLimitSec != 30 {
		t.Fatalf("q1 = %+v", q1)
	}
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions", single, 422, nil) // duplicate code

	essay := map[string]any{"code": "Q2", "type": "ESSAY", "text": "Explain", "marks": 5,
		"key": map[string]any{"model_answer": "...", "rubric": "..."}, "settings": map[string]any{"evaluation_method": "llm"}}
	var q2 quiz.Question
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions", essay, 201, &q2)

	var dup quiz.Question
	teacher.Call("POST", "/api/teacher/questions/"+q1.ID+"/duplicate", nil, 201, &dup)
	if dup.Code != "Q1-copy" || dup.Position != 2 {
		t.Fatalf("dup = %+v", dup)
	}
	teacher.Call("POST", "/api/teacher/questions/"+q1.ID+"/duplicate", nil, 201, &dup)
	if dup.Code != "Q1-copy2" {
		t.Fatalf("second dup code = %s", dup.Code)
	}

	teacher.Call("PUT", "/api/teacher/quizzes/"+q.ID+"/questions/order", map[string]any{"ids": []string{q2.ID, q1.ID}}, 400, nil)
	var list struct{ Questions []quiz.Question }
	teacher.Call("GET", "/api/teacher/quizzes/"+q.ID+"/questions", nil, 200, &list)
	ids := []string{q2.ID}
	for _, x := range list.Questions {
		if x.ID != q2.ID {
			ids = append(ids, x.ID)
		}
	}
	teacher.Call("PUT", "/api/teacher/quizzes/"+q.ID+"/questions/order", map[string]any{"ids": ids}, 204, nil)
	teacher.Call("GET", "/api/teacher/quizzes/"+q.ID+"/questions", nil, 200, &list)
	if list.Questions[0].ID != q2.ID {
		t.Fatal("reorder not applied")
	}

	single["text"] = "Capital of Italy?"
	single["key"] = map[string]any{"correct": []string{"o2"}}
	teacher.Call("PUT", "/api/teacher/questions/"+q1.ID, single, 200, &q1)
	if q1.Text != "Capital of Italy?" || q1.Key.Correct[0] != "o2" {
		t.Fatalf("update = %+v", q1)
	}
	teacher.Call("DELETE", "/api/teacher/questions/"+q1.ID, nil, 204, nil)
	teacher.Call("GET", "/api/teacher/questions/"+q1.ID, nil, 404, nil)

	var got quiz.Quiz
	teacher.Call("GET", "/api/teacher/quizzes/"+q.ID, nil, 200, &got)
	if got.QuestionCount != 3 || got.TotalMarks != 7 {
		t.Fatalf("count=%d marks=%v", got.QuestionCount, got.TotalMarks)
	}
}

// AC-09: a CSV with 50 rows, 2 invalid → 48 imported, report lists 2 failures with reasons.
func TestCSVImportAC09(t *testing.T) {
	_, teacher, q, _ := setup(t)
	var b strings.Builder
	b.WriteString("question_code,type,question_text,options,correct_answer,marks\n")
	for i := 1; i <= 50; i++ {
		switch i {
		case 7:
			b.WriteString("Q007,SINGLE,Bad row,A|B,C,1\n") // C is not an option
		case 31:
			b.WriteString("Q031,NOPE,Bad type,A|B,A,1\n")
		default:
			fmt.Fprintf(&b, "Q%03d,SINGLE,Question %d,A|B|C,B,1\n", i, i)
		}
	}
	status, raw := teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(b.String()))
	if status != 200 {
		t.Fatalf("import: %d %s", status, raw)
	}
	var rep quiz.ImportReport
	json.Unmarshal(raw, &rep)
	if rep.Imported != 48 || len(rep.Rejected) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Rejected[0].Row != 8 || rep.Rejected[0].Code != "Q007" || rep.Rejected[0].Errors["correct_answer"] == "" {
		t.Errorf("first rejection = %+v", rep.Rejected[0])
	}
	if rep.Rejected[1].Code != "Q031" || rep.Rejected[1].Errors["type"] == "" {
		t.Errorf("second rejection = %+v", rep.Rejected[1])
	}
	// UC-01 4a: fix and re-upload only the failed rows.
	fix := "question_code,type,question_text,options,correct_answer\nQ007,SINGLE,Fixed,A|B,A\nQ031,SINGLE,Fixed,A|B,B\nQ001,SINGLE,Dup,A|B,A\n"
	_, raw = teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(fix))
	json.Unmarshal(raw, &rep)
	if rep.Imported != 2 || len(rep.Rejected) != 1 || rep.Rejected[0].Errors["code"] == "" {
		t.Fatalf("re-upload report = %+v", rep)
	}
	var got quiz.Quiz
	teacher.Call("GET", "/api/teacher/quizzes/"+q.ID, nil, 200, &got)
	if got.QuestionCount != 50 {
		t.Fatalf("question count = %d", got.QuestionCount)
	}
	status, _ = teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte("nonsense\n"))
	if status != 400 {
		t.Fatalf("missing columns: status %d", status)
	}
}

func TestTemplateDownloadImportsCleanly(t *testing.T) {
	_, teacher, q, _ := setup(t)
	status, tmpl := teacher.Do("GET", "/api/teacher/quiz-template.csv", nil)
	if status != 200 {
		t.Fatal(status)
	}
	_, raw := teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", tmpl)
	var rep quiz.ImportReport
	json.Unmarshal(raw, &rep)
	if rep.Imported != 8 || len(rep.Rejected) != 0 {
		t.Fatalf("template import = %s", raw)
	}
}

func TestResourcesMappingAndValidation(t *testing.T) {
	e, teacher, q, _ := setup(t)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(quiz.Template))

	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".png") {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("png"))
			return
		}
		w.WriteHeader(404)
	}))
	defer img.Close()

	mapping := "resource_name,url,alt_text\n" +
		"Q001_Q_1,https://drive.google.com/file/d/1AbCdEfGhIjKlMnOp/view?usp=sharing,Map of France\n" +
		"Q001_O_2," + img.URL + "/rome.png,Rome\n" +
		"Q002_Q_1," + img.URL + "/missing.jpg,Broken\n" +
		"Q999_Q_1," + img.URL + "/x.png,No such question\n" +
		"Q001_O_9," + img.URL + "/x.png,Option out of range\n" +
		"Q001_Q_1x,javascript:alert(1),bad\n"
	status, raw := teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/resources/import", "text/csv", []byte(mapping))
	var rep quiz.ImportReport
	json.Unmarshal(raw, &rep)
	if status != 200 || rep.Imported != 3 || len(rep.Rejected) != 3 {
		t.Fatalf("resource import %d: %s", status, raw)
	}

	// The River job would run this; drive it directly.
	if err := e.App.Quiz.CheckResources(context.Background(), q.ID, true); err != nil {
		t.Fatal(err)
	}
	var list struct{ Questions []quiz.Question }
	teacher.Call("GET", "/api/teacher/quizzes/"+q.ID+"/questions", nil, 200, &list)
	byCode := map[string]quiz.Question{}
	for _, x := range list.Questions {
		byCode[x.Code] = x
	}
	r := byCode["Q001"].Resources
	if len(r) != 2 || !strings.HasPrefix(r[1].URL, "https://drive.google.com/thumbnail?id=1AbCdEfGhIjKlMnOp") {
		t.Fatalf("Q001 resources = %+v", r)
	}
	if r[0].Status != "ok" || r[0].Role != quiz.RoleOption {
		t.Errorf("rome.png = %+v", r[0])
	}
	if br := byCode["Q002"].Resources[0]; br.Status != "broken" || br.Message == "" {
		t.Errorf("broken link not flagged: %+v", br)
	}

	// UC-01 6a: broken links block Ready until the warning is accepted.
	_, body := teacher.Do("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"})
	if !strings.Contains(string(body), "quiz_has_warnings") {
		t.Fatalf("ready with broken link: %s", body)
	}
	var ready quiz.Quiz
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready", "accept_warnings": true}, 200, &ready)
	if ready.Status != "ready" || !ready.WarningsAccepted {
		t.Fatalf("ready = %+v", ready)
	}

	// The question editor can replace or delete one resource.
	var q2 quiz.Question
	teacher.Call("PUT", "/api/teacher/questions/"+byCode["Q002"].ID+"/resources",
		map[string]any{"role": "q", "n": 1, "url": img.URL + "/fixed.png", "alt_text": "Fixed"}, 200, &q2)
	if q2.Resources[0].Status != "unchecked" || q2.Resources[0].AltText != "Fixed" {
		t.Fatalf("replace = %+v", q2.Resources)
	}
	teacher.Call("DELETE", "/api/teacher/resources/"+q2.Resources[0].ID, nil, 204, nil)

	var n int
	e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM river_job WHERE kind='quiz_resource_check'`).Scan(&n)
	if n < 2 {
		t.Fatalf("resource check jobs enqueued = %d", n)
	}
}

func TestReadinessAndPreviewHideKeys(t *testing.T) {
	_, teacher, q, _ := setup(t)
	_, body := teacher.Do("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"})
	if !strings.Contains(string(body), "quiz_not_ready") {
		t.Fatalf("empty quiz became ready: %s", body)
	}
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(quiz.Template))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)

	status, raw := teacher.Do("GET", "/api/teacher/quizzes/"+q.ID+"/preview", nil)
	if status != 200 {
		t.Fatal(status)
	}
	for _, leak := range []string{`"key"`, `"correct"`, `"pairs"`, `"rubric"`, "Paris is the capital"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("preview leaks %s", leak)
		}
	}
}

func TestQuizIsolationAndTopicDeleteProtection(t *testing.T) {
	e, alice, q, topic := setup(t)
	bob := e.NewUser(auth.RoleTeacher)
	bob.Call("GET", "/api/teacher/quizzes/"+q.ID, nil, 404, nil)
	bob.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "x"}, 404, nil)
	bob.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions", map[string]any{"code": "Q1", "type": "ESSAY", "text": "x"}, 404, nil)
	bob.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(quiz.Template))
	var list struct{ Questions []quiz.Question }
	alice.Call("GET", "/api/teacher/quizzes/"+q.ID+"/questions", nil, 200, &list)
	if len(list.Questions) != 0 {
		t.Fatal("another teacher imported into this quiz")
	}
	// A topic with quizzes cannot be deleted.
	alice.Call("DELETE", "/api/teacher/topics/"+topic.ID, nil, 409, nil)
	var del map[string]bool
	alice.Call("DELETE", "/api/teacher/quizzes/"+q.ID, nil, 200, &del)
	if !del["deleted"] {
		t.Fatalf("delete = %v", del)
	}
	alice.Call("DELETE", "/api/teacher/topics/"+topic.ID, nil, 204, nil)
}
