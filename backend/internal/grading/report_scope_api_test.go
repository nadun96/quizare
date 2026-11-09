package grading_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/grading"
)

// D-160: the class report of one module, topic, item or quiz session.
func TestScopedReport(t *testing.T) {
	f := newReportFx(t)
	one, two := f.quiz(t, "One"), f.quiz(t, "Two")
	amaya := f.student(t)
	f.take(t, amaya, one, 1) // first run: 50 %
	f.take(t, amaya, one, 2) // second run: 100 %
	f.take(t, amaya, two, 2)
	// Another module, with an offline assignment.
	var m content.Module
	f.teacher.Call("POST", "/api/teacher/classrooms/"+f.class.ID+"/modules", map[string]any{"name": "Practicals"}, 201, &m)
	var tp content.Topic
	f.teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "Lab"}, 201, &tp)
	var o grading.Offline
	f.teacher.Call("POST", "/api/teacher/topics/"+tp.ID+"/offline-assignments", map[string]any{"title": "Lab book", "max_marks": 10}, 201, &o)
	f.teacher.Call("PUT", "/api/teacher/offline-assignments/"+o.ID+"/marks", grading.MarksInput{Students: []grading.MarkInput{{UserID: amaya.User.ID, Score: ptr(7)}}}, 200, nil)

	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	get := func(scope string) grading.ClassReport {
		t.Helper()
		var r grading.ClassReport
		f.teacher.Call("GET", url+"?scope="+scope, nil, 200, &r)
		return r
	}
	all := get("")
	if all.Scope.Kind != "all" || len(all.Items) != 3 || len(all.Sessions) != 3 {
		t.Fatalf("whole report: %+v items, %+v sessions", all.Items, all.Sessions)
	}
	// Sessions come newest first; One's first run is the oldest of its two.
	var first string
	for _, x := range all.Sessions {
		if x.QuizID == one {
			first = x.ID // the last one seen is the earliest
		}
	}
	if pct := all.Students[0].Marks["quiz:"+one].Pct; pct != 100 {
		t.Fatalf("whole report uses the best run: %v", pct)
	}

	sess := get("session:" + first)
	if sess.Scope.Kind != "session" || len(sess.Items) != 1 || sess.Items[0].ID != one || sess.Students[0].Marks["quiz:"+one].Pct != 50 || sess.Items[0].Taken != 1 {
		t.Fatalf("one session: %+v %+v", sess.Scope, sess.Students[0].Marks)
	}
	item := get("item:quiz:" + two)
	if item.Scope.Label != "Two" || len(item.Items) != 1 || item.Students[0].Marks["quiz:"+two].Pct != 100 {
		t.Fatalf("one quiz: %+v", item)
	}
	// Leaving an item out of the report doesn't hide it when asked for by name.
	f.teacher.Call("PUT", url, setupFor(all, func(x *grading.ReportItemInput) {
		x.Included = x.ID != two
		x.Weight = 0.5
	}), 200, nil)
	if r := get("item:quiz:" + two); !r.Items[0].Included || r.Students[0].Marks["quiz:"+two].Pct != 100 {
		t.Fatalf("left out, asked for: %+v", r.Items)
	}
	topic := get("topic:" + f.topic.ID)
	if topic.Scope.Label != "T" || len(topic.Items) != 2 {
		t.Fatalf("topic: %+v", topic.Items)
	}
	// Totals are of what the report covers: the Practicals module alone is the lab book.
	mod := get("module:" + m.ID)
	if mod.Scope.Label != "Practicals" || len(mod.Items) != 1 || mod.Items[0].Kind != "offline" || mod.OutOf != 50 || mod.Students[0].Final == nil || *mod.Students[0].Final != 70 {
		t.Fatalf("module: %+v %+v", mod.Items, mod.Students)
	}

	f.teacher.Call("GET", url+"?scope=session:00000000-0000-0000-0000-000000000000", nil, 422, nil)
	f.teacher.Call("GET", url+"?scope=item:quiz:"+o.ID, nil, 422, nil)
	f.teacher.Call("GET", url+"?scope=planet:x", nil, 422, nil)
	f.e.NewUser(auth.RoleTeacher).Call("GET", url+"?scope=session:"+first, nil, 404, nil)
}
