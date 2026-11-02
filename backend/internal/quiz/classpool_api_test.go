package quiz_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// questionsCSV makes n single-answer questions Q1…Qn, with a category column when cat isn't "".
func questionsCSV(n int, cat string) []byte {
	var b strings.Builder
	b.WriteString("question_code,type,question_text,options,correct_answer,marks,category\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "Q%d,SINGLE,Question %d?,Yes|No,Yes,1,%s\n", i, i, cat)
	}
	return []byte(b.String())
}

// Classroom question pools (D-123): one per classroom, filled by CSV with a
// preview first, worked on in bulk and by category, and drawn into the
// classroom's quizzes so many from each category.
func TestClassPools(t *testing.T) {
	e, teacher, q, topic := setup(t)
	ctx := context.Background()
	var cid string
	e.Pool.QueryRow(ctx, `SELECT m.classroom_id FROM content.topics t JOIN content.modules m ON m.id = t.module_id WHERE t.id = $1`, topic.ID).Scan(&cid)

	var pool quiz.Quiz
	teacher.Call("GET", "/api/teacher/classrooms/"+cid+"/pool", nil, 200, &pool)
	if pool.Kind != "pool" || pool.ClassroomID != cid {
		t.Fatalf("pool = %+v", pool)
	}
	var again quiz.Quiz
	teacher.Call("GET", "/api/teacher/classrooms/"+cid+"/pool", nil, 200, &again)
	if again.ID != pool.ID {
		t.Fatal("a second pool was made")
	}
	imp := "/api/teacher/quizzes/" + pool.ID + "/questions/import"

	// A preview: the file's category fills rows without one; nothing is saved.
	var res quiz.ImportResult
	code, raw := teacher.Raw("POST", imp+"?preview=1&category=Algebra", "text/csv", questionsCSV(12, ""))
	if code != 200 || decode(raw, &res) != nil || !res.Preview || res.Imported != 12 || res.Categories["Algebra"] != 12 || res.Rows[0].Category != "Algebra" {
		t.Fatalf("preview = %d %s", code, raw)
	}
	teacher.Call("GET", "/api/teacher/quizzes/"+pool.ID, nil, 200, &pool)
	if pool.QuestionCount != 0 {
		t.Fatalf("a preview saved %d", pool.QuestionCount)
	}
	// Approved: saved. A second file with the same codes: renamed, not refused.
	teacher.Raw("POST", imp+"?category=Algebra", "text/csv", questionsCSV(12, ""))
	code, raw = teacher.Raw("POST", imp+"?rename=1", "text/csv", questionsCSV(12, "Geometry"))
	if code != 200 || decode(raw, &res) != nil || res.Imported != 12 || res.Renamed != 12 || res.Rows[0].Renamed != "Q1-2" {
		t.Fatalf("second file = %d %s", code, raw)
	}
	code, raw = teacher.Raw("POST", imp, "text/csv", questionsCSV(2, "Logic"))
	if decode(raw, &res) != nil || res.Imported != 0 || len(res.Rejected) != 2 {
		t.Fatalf("without rename = %d %s", code, raw)
	}

	// The overview: the bank, and this classroom's pool by category.
	var ov struct{ Pools []quiz.PoolSummary }
	teacher.Call("GET", "/api/teacher/pools", nil, 200, &ov)
	if len(ov.Pools) != 2 || ov.Pools[0].Kind != "bank" || ov.Pools[1].Questions != 24 || len(ov.Pools[1].Categories) != 2 || ov.Pools[1].Categories[0].Category != "Algebra" {
		t.Fatalf("overview = %+v", ov.Pools)
	}

	// A quiz draws 3 from each category of the class pool.
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", questionsCSV(1, ""))
	rules := []quiz.PoolRule{{Source: "class", Category: "Algebra", Count: 3}, {Source: "class", Category: "geometry", Count: 3}}
	var info quiz.PoolInfo
	teacher.Call("PUT", "/api/teacher/quizzes/"+q.ID+"/pools", map[string]any{"pools": rules}, 200, &info)
	if info.ClassPoolID != pool.ID || len(info.Class) != 2 {
		t.Fatalf("pool info = %+v", info)
	}
	teacher.Call("PUT", "/api/teacher/quizzes/"+q.ID+"/pools", map[string]any{"pools": []quiz.PoolRule{{Source: "class", Category: "Algebra", Count: 50}}}, 200, nil)
	if code, raw := teacher.Do("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}); code != 409 || !strings.Contains(string(raw), "class pool: Algebra draws 50 but has only 12") {
		t.Fatalf("too many = %d %s", code, raw)
	}
	teacher.Call("PUT", "/api/teacher/quizzes/"+q.ID+"/pools", map[string]any{"pools": rules}, 200, nil)
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess live.Session
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)
	var fromClass int
	e.Pool.QueryRow(ctx, `SELECT count(*) FROM live.sessions s, jsonb_array_elements(s.snapshot->'questions') x WHERE s.id = $1 AND (x->>'from_class')::bool`, sess.ID).Scan(&fromClass)
	if fromClass != 24 {
		t.Fatalf("snapshot has %d class pool questions", fromClass)
	}

	// The pool can't be run or marked ready itself.
	teacher.Call("POST", "/api/teacher/quizzes/"+pool.ID+"/sessions", map[string]any{}, 409, nil)
	teacher.Call("POST", "/api/teacher/quizzes/"+pool.ID+"/status", map[string]any{"status": "ready"}, 409, nil)

	// Bulk and category work.
	var qs struct{ Questions []quiz.Question }
	teacher.Call("GET", "/api/teacher/quizzes/"+pool.ID+"/questions", nil, 200, &qs)
	var ids []string
	for _, x := range qs.Questions[:3] {
		ids = append(ids, x.ID)
	}
	var n struct{ Affected int }
	teacher.Call("POST", "/api/teacher/quizzes/"+pool.ID+"/questions/bulk", map[string]any{"action": "move", "ids": ids, "category": " Number   theory "}, 200, &n)
	if n.Affected != 3 {
		t.Fatalf("moved %d", n.Affected)
	}
	teacher.Call("POST", "/api/teacher/quizzes/"+pool.ID+"/questions/bulk", map[string]any{"action": "delete", "ids": ids[:1]}, 200, &n)
	teacher.Call("POST", "/api/teacher/quizzes/"+pool.ID+"/questions/bulk", map[string]any{"action": "explode", "ids": ids}, 422, nil)
	teacher.Call("POST", "/api/teacher/quizzes/"+pool.ID+"/categories/rename", map[string]any{"from": "geometry", "to": "Shapes"}, 200, &n)
	if n.Affected != 12 {
		t.Fatalf("renamed %d", n.Affected)
	}
	teacher.Call("POST", "/api/teacher/quizzes/"+pool.ID+"/categories/delete", map[string]any{"name": "Number theory", "delete_questions": true}, 200, &n)
	if n.Affected != 2 {
		t.Fatalf("deleted %d with the category", n.Affected)
	}
	teacher.Call("POST", "/api/teacher/quizzes/"+pool.ID+"/categories/delete", map[string]any{"name": "Shapes"}, 200, &n)
	var cats []quiz.PoolCount
	teacher.Call("GET", "/api/teacher/pools", nil, 200, &ov)
	cats = ov.Pools[1].Categories
	if len(cats) != 2 || cats[0].Category != "Algebra" || cats[0].Count != 9 || cats[1].Category != "" || cats[1].Count != 12 {
		t.Fatalf("categories now = %+v", cats)
	}

	// Another teacher can't; a co-teacher shares the pool.
	other := e.NewUser(auth.RoleTeacher)
	other.Call("GET", "/api/teacher/classrooms/"+cid+"/pool", nil, 404, nil)
	other.Call("GET", "/api/teacher/quizzes/"+pool.ID+"/questions", nil, 404, nil)
	teacher.Call("POST", "/api/teacher/classrooms/"+cid+"/teachers", map[string]string{"email": other.User.Email}, 201, nil)
	other.Call("POST", "/api/teacher/invitations/"+cid+"/accept", nil, 204, nil)
	var theirs quiz.Quiz
	other.Call("GET", "/api/teacher/classrooms/"+cid+"/pool", nil, 200, &theirs)
	if theirs.ID != pool.ID {
		t.Fatal("the co-teacher got another pool")
	}
}

func decode(raw []byte, v any) error { return json.Unmarshal(raw, v) }
