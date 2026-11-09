package grading_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/dashboard"
	"github.com/nadun96/quizplatform/internal/grading"
	"github.com/nadun96/quizplatform/internal/platform/xlsx"
)

func ptr(f float64) *float64 { return &f }

func studentOf(sh grading.OfflineSheet, userID string) grading.OfflineStudent {
	for _, st := range sh.Students {
		if st.UserID == userID {
			return st
		}
	}
	return grading.OfflineStudent{}
}

// D-159: an offline assignment in a topic, marked student by student, by
// hand or through a downloaded sheet; it counts in the report, and students
// see their marks once released.
func TestOfflineIndividual(t *testing.T) {
	f := newReportFx(t)
	amaya, ben := f.student(t), f.student(t)
	other := f.e.NewUser(auth.RoleTeacher)
	topicURL := "/api/teacher/topics/" + f.topic.ID + "/offline-assignments"

	f.teacher.Call("POST", topicURL, map[string]any{"title": " ", "max_marks": 20}, 422, nil)
	f.teacher.Call("POST", topicURL, map[string]any{"title": "Lab", "max_marks": 0}, 422, nil)
	f.teacher.Call("POST", topicURL, map[string]any{"title": "Lab", "max_marks": 20, "mode": "pairs"}, 422, nil)
	other.Call("POST", topicURL, map[string]any{"title": "Lab", "max_marks": 20}, 404, nil)
	due := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	var o grading.Offline
	f.teacher.Call("POST", topicURL, map[string]any{"title": "Lab report", "max_marks": 20, "instructions": "Hand in on paper", "due_at": due}, 201, &o)
	if o.Mode != "individual" || o.Students != 2 || o.Marked != 0 || o.ReleasedAt != nil {
		t.Fatalf("created = %+v", o)
	}
	var list struct{ Assignments []grading.Offline }
	f.teacher.Call("GET", topicURL, nil, 200, &list)
	if len(list.Assignments) != 1 {
		t.Fatalf("list = %+v", list)
	}
	url := "/api/teacher/offline-assignments/" + o.ID
	other.Call("GET", url, nil, 404, nil)

	// Due, before marks are released.
	var work dashboard.ClassroomWork
	amaya.Call("GET", "/api/my/classrooms/"+f.class.ID+"/work", nil, 200, &work)
	found := false
	for _, it := range work.Due {
		found = found || (it.Kind == "offline" && it.ID == o.ID && it.Href == "/my/offline/"+o.ID)
	}
	if !found {
		t.Fatalf("not due: %+v", work.Due)
	}

	// Marks by hand: checked.
	put := func(in grading.MarksInput, code int) grading.OfflineSheet {
		t.Helper()
		var sh grading.OfflineSheet
		f.teacher.Call("PUT", url+"/marks", in, code, &sh)
		return sh
	}
	put(grading.MarksInput{Students: []grading.MarkInput{{UserID: amaya.User.ID, Score: ptr(21)}}}, 422)
	put(grading.MarksInput{Students: []grading.MarkInput{{UserID: amaya.User.ID, Score: ptr(1.234)}}}, 422)
	put(grading.MarksInput{Students: []grading.MarkInput{{UserID: other.User.ID, Score: ptr(1)}}}, 422)
	put(grading.MarksInput{Groups: []grading.GroupMarkInput{{GroupID: "00000000-0000-0000-0000-000000000000", Score: ptr(1)}}}, 422)
	sh := put(grading.MarksInput{Students: []grading.MarkInput{{UserID: amaya.User.ID, Score: ptr(15), Comment: "Neat"}}}, 200)
	if st := studentOf(sh, amaya.User.ID); st.Score == nil || *st.Score != 15 || st.Comment != "Neat" || sh.Marked != 1 {
		t.Fatalf("marked = %+v", st)
	}
	f.teacher.Call("PUT", url, map[string]any{"title": "Lab report", "max_marks": 10}, 422, nil) // below a mark given

	// The sheet: download, fill in, upload, check, save.
	code, _, data := f.teacher.Get(url+"/sheet?format=xlsx", nil)
	if code != 200 {
		t.Fatalf("download: %d", code)
	}
	sheets, err := xlsx.Read(data)
	if err != nil || len(sheets) != 1 || len(sheets[0].Rows) != 3 || !strings.HasPrefix(sheets[0].Rows[0][4], "Marks (out of 20)") {
		t.Fatalf("sheet = %+v, %v", sheets, err)
	}
	rows := sheets[0].Rows
	for _, r := range rows[1:] {
		for len(r) < 7 {
			r = append(r, "")
		}
	}
	filled := [][]xlsx.Cell{}
	for i, r := range rows {
		for len(r) < 7 {
			r = append(r, "")
		}
		cells := make([]xlsx.Cell, len(r))
		for j, v := range r {
			cells[j] = xlsx.Text(v)
		}
		if i > 0 && r[6] == ben.User.ID {
			cells[4] = xlsx.Number(17.5)
			cells[5] = xlsx.Text("Good")
		}
		filled = append(filled, cells)
	}
	filled = append(filled, []xlsx.Cell{xlsx.Text("9"), xlsx.Text(""), xlsx.Text("Nobody"), xlsx.Text(""), xlsx.Number(3)})
	upload, err := xlsx.Write([]xlsx.Sheet{{Name: "Students", Rows: filled}})
	if err != nil {
		t.Fatal(err)
	}
	var up grading.Upload
	code, body := f.teacher.Raw("POST", url+"/sheet", xlsx.ContentType, upload)
	if code != 200 || json.Unmarshal(body, &up) != nil {
		t.Fatalf("upload: %d %s", code, body)
	}
	if len(up.Students) != 1 || up.Students[0].UserID != ben.User.ID || up.Students[0].Score != 17.5 || up.Students[0].Comment != "Good" || up.Same != 1 || len(up.Problems) != 1 || !strings.Contains(up.Problems[0].Message, "Nobody") {
		t.Fatalf("upload = %+v", up)
	}
	sh = put(grading.MarksInput{Students: []grading.MarkInput{{UserID: up.Students[0].UserID, Score: &up.Students[0].Score, Comment: up.Students[0].Comment}}}, 200)
	if st := studentOf(sh, ben.User.ID); st.Score == nil || *st.Score != 17.5 {
		t.Fatalf("ben = %+v", st)
	}

	// CSV too, with semicolons, a comma for the decimal point and a student ID.
	code, _, data = f.teacher.Get(url+"/sheet?format=csv", nil)
	if code != 200 || !strings.HasPrefix(string(data), "\xef\xbb\xbfNo.,Student ID,Name") {
		t.Fatalf("csv: %d %q", code, data)
	}
	csv := "No.;Student ID;Name;Groups;Marks (out of 20);Comment\n1;;" + amaya.User.Name + ";;12,5;Redo\n2;;Someone;;abc;\n"
	code, body = f.teacher.Raw("POST", url+"/sheet", "text/csv", []byte(csv))
	up = grading.Upload{}
	if code != 200 || json.Unmarshal(body, &up) != nil || len(up.Students) != 1 || up.Students[0].Score != 12.5 || up.Students[0].Was == nil || *up.Students[0].Was != 15 || len(up.Problems) != 1 {
		t.Fatalf("csv upload: %d %s", code, body)
	}
	code, body = f.teacher.Raw("POST", url+"/sheet", "text/csv", []byte("hello,world\n1,2\n"))
	if code != 200 || !strings.Contains(string(body), "no marks column") {
		t.Fatalf("not a mark sheet: %d %s", code, body)
	}

	// The report and the gradebook count it.
	var rep grading.ClassReport
	f.teacher.Call("GET", "/api/teacher/classrooms/"+f.class.ID+"/class-report", nil, 200, &rep)
	k := "offline:" + o.ID
	var item *grading.ReportItem
	for i := range rep.Items {
		if rep.Items[i].Kind == "offline" {
			item = &rep.Items[i]
		}
	}
	if item == nil || item.Title != "Lab report" {
		t.Fatalf("report items: %+v", rep.Items)
	}
	for _, st := range rep.Students {
		m, ok := st.Marks[k]
		if st.UserID == ben.User.ID && (!ok || m.Score != 17.5 || m.Max != 20 || m.Pct != 87.5) {
			t.Fatalf("ben's report mark = %+v", m)
		}
	}
	f.teacher.Call("PUT", "/api/teacher/classrooms/"+f.class.ID+"/class-report", setupFor(rep, func(x *grading.ReportItemInput) { x.Weight = 0.5 }), 200, nil)
	var g grading.Gradebook
	f.teacher.Call("GET", "/api/teacher/classrooms/"+f.class.ID+"/gradebook", nil, 200, &g)
	gotItem := false
	for _, it := range g.Items {
		gotItem = gotItem || (it.Kind == "offline" && it.ID == o.ID)
	}
	if !gotItem {
		t.Fatalf("gradebook items: %+v", g.Items)
	}
	f.teacher.Call("PUT", "/api/teacher/classrooms/"+f.class.ID+"/weights", map[string]any{"weights": []grading.WeightInput{{Kind: "offline", ID: o.ID, Weight: 20}}}, 200, nil)

	// Students see their mark only once released.
	var mine grading.MyOffline
	amaya.Call("GET", "/api/my/offline-assignments/"+o.ID, nil, 200, &mine)
	if mine.Released || mine.Score != nil || mine.Instructions != "Hand in on paper" {
		t.Fatalf("before release = %+v", mine)
	}
	f.e.NewUser(auth.RoleStudent).Call("GET", "/api/my/offline-assignments/"+o.ID, nil, 404, nil)
	f.teacher.Call("PUT", url+"/release", map[string]bool{"released": true}, 200, &o)
	if o.ReleasedAt == nil || o.Marked != 2 {
		t.Fatalf("released = %+v", o)
	}
	amaya.Call("GET", "/api/my/offline-assignments/"+o.ID, nil, 200, &mine)
	if !mine.Released || mine.Score == nil || *mine.Score != 15 || *mine.Pct != 75 || mine.Comment != "Neat" {
		t.Fatalf("after release = %+v", mine)
	}
	work = dashboard.ClassroomWork{}
	amaya.Call("GET", "/api/my/classrooms/"+f.class.ID+"/work", nil, 200, &work)
	done := false
	for _, d := range work.Completed {
		done = done || (d.Kind == "offline" && d.Score != nil && *d.Score == 15 && *d.Max == 20)
	}
	if !done {
		t.Fatalf("completed = %+v", work.Completed)
	}

	// Clearing a mark, and deleting the assignment.
	sh = put(grading.MarksInput{Students: []grading.MarkInput{{UserID: amaya.User.ID}}}, 200)
	if studentOf(sh, amaya.User.ID).Score != nil {
		t.Fatal("not cleared")
	}
	other.Call("DELETE", url, nil, 404, nil)
	f.teacher.Call("DELETE", url, nil, 204, nil)
	f.teacher.Call("GET", url, nil, 404, nil)
	f.teacher.Call("GET", "/api/teacher/classrooms/"+f.class.ID+"/class-report", nil, 200, &rep)
	for _, it := range rep.Items {
		if it.Kind == "offline" {
			t.Fatal("still in the report")
		}
	}
}

// D-159: a group assignment: each member gets the group's mark unless they
// have their own; marks by hand and through the Groups sheet.
func TestOfflineGroups(t *testing.T) {
	f := newReportFx(t)
	amaya, ben, chen := f.student(t), f.student(t), f.student(t)
	f.classGroup(t, "Lions", amaya, ben)
	f.classGroup(t, "Tigers", chen, amaya) // Amaya is in both: Lions comes first
	var o grading.Offline
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/offline-assignments", map[string]any{"title": "Project", "max_marks": 50, "mode": "group"}, 201, &o)
	url := "/api/teacher/offline-assignments/" + o.ID
	var sh grading.OfflineSheet
	f.teacher.Call("GET", url, nil, 200, &sh)
	if len(sh.Groups) != 2 || len(sh.Students) != 3 {
		t.Fatalf("sheet = %+v", sh)
	}
	lions, tigers := sh.Groups[0], sh.Groups[1]
	if lions.Name != "Lions" || len(lions.Members) != 2 {
		t.Fatalf("groups = %+v", sh.Groups)
	}
	f.teacher.Call("PUT", url+"/marks", grading.MarksInput{Groups: []grading.GroupMarkInput{{GroupID: lions.ID, Score: ptr(40), Comment: "Great"}, {GroupID: tigers.ID, Score: ptr(30)}}}, 200, &sh)
	check := func(u *apptest.Client, want float64, group string) {
		t.Helper()
		st := studentOf(sh, u.User.ID)
		if st.Score == nil || *st.Score != want || st.GroupName != group {
			t.Fatalf("%s: %+v", u.User.Name, st)
		}
	}
	check(amaya, 40, "Lions")
	check(ben, 40, "Lions")
	check(chen, 30, "Tigers")
	// Ben didn't take part: his own mark replaces the group's.
	f.teacher.Call("PUT", url+"/marks", grading.MarksInput{Students: []grading.MarkInput{{UserID: ben.User.ID, Score: ptr(0), Comment: "Absent"}}}, 200, &sh)
	check(ben, 0, "Lions")

	// The Excel file has a Groups sheet; uploading it changes group marks.
	code, _, data := f.teacher.Get(url+"/sheet?format=xlsx", nil)
	sheets, err := xlsx.Read(data)
	if code != 200 || err != nil || len(sheets) != 2 || sheets[0].Name != "Groups" || sheets[1].Name != "Students" {
		t.Fatalf("workbook: %d %v %+v", code, err, sheets)
	}
	csv := "No.,Group,Members,Marks (out of 50),Comment,Key\n1,Lions,,45,,\n2,Bears,,10,,\n"
	var up grading.Upload
	code, body := f.teacher.Raw("POST", url+"/sheet", "text/csv", []byte(csv))
	if code != 200 || json.Unmarshal(body, &up) != nil || len(up.Groups) != 1 || up.Groups[0].GroupID != lions.ID || up.Groups[0].Score != 45 || len(up.Problems) != 1 {
		t.Fatalf("groups upload: %d %s", code, body)
	}
	code, _, data = f.teacher.Get(url+"/sheet?format=csv&sheet=groups", nil)
	if code != 200 || !strings.Contains(string(data), "Lions") {
		t.Fatalf("groups csv: %d %s", code, data)
	}

	// The report: each member's mark.
	var rep grading.ClassReport
	f.teacher.Call("GET", "/api/teacher/classrooms/"+f.class.ID+"/class-report", nil, 200, &rep)
	for _, st := range rep.Students {
		m := st.Marks["offline:"+o.ID]
		want := map[string]float64{amaya.User.ID: 40, ben.User.ID: 0, chen.User.ID: 30}[st.UserID]
		if m.Score != want {
			t.Fatalf("%s's report mark = %+v", st.Name, m)
		}
	}

	// An individual assignment takes no group marks.
	var solo grading.Offline
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/offline-assignments", map[string]any{"title": "Solo", "max_marks": 10}, 201, &solo)
	f.teacher.Call("PUT", "/api/teacher/offline-assignments/"+solo.ID+"/marks", grading.MarksInput{Groups: []grading.GroupMarkInput{{GroupID: lions.ID, Score: ptr(5)}}}, 422, nil)
	f.teacher.Call("GET", "/api/teacher/offline-assignments/"+solo.ID+"/sheet?format=csv&sheet=groups", nil, 422, nil)

	var mine grading.MyOffline
	f.teacher.Call("PUT", url+"/release", map[string]bool{"released": true}, 200, nil)
	amaya.Call("GET", "/api/my/offline-assignments/"+o.ID, nil, 200, &mine)
	if mine.Score == nil || *mine.Score != 40 || mine.Group != "Lions" || mine.Comment != "Great" {
		t.Fatalf("amaya sees = %+v", mine)
	}
	_ = content.Category{}
}
