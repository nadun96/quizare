package grading_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/grading"
)

// classGroup makes one of the classroom's groups (D-158) with these students.
func (f *reportFx) classGroup(t *testing.T, name string, users ...*apptest.Client) {
	t.Helper()
	var ens struct{ Enrolments []content.Enrolment }
	f.teacher.Call("GET", "/api/teacher/classrooms/"+f.class.ID+"/enrolments", nil, 200, &ens)
	enrolment := map[string]string{}
	for _, en := range ens.Enrolments {
		enrolment[en.UserID] = en.ID
	}
	var c content.Category
	f.teacher.Call("POST", "/api/teacher/classrooms/"+f.class.ID+"/categories", map[string]any{"name": name}, 201, &c)
	ids := []string{}
	for _, u := range users {
		ids = append(ids, enrolment[u.User.ID])
	}
	f.teacher.Call("POST", "/api/teacher/categories/"+c.ID+"/members", map[string]any{"enrolment_ids": ids, "assigned": true}, 200, nil)
}

func groupOf(r grading.ClassReport, name string) grading.GroupRow {
	for _, g := range r.ClassGroups {
		if g.Name == name {
			return g
		}
	}
	return grading.GroupRow{}
}

// D-141: groups of the class and of one item, with marks by each item's rule.
func TestReportGroups(t *testing.T) {
	f := newReportFx(t)
	q, q2 := f.quiz(t, "One"), f.quiz(t, "Two")
	amaya, ben, chen := f.student(t), f.student(t), f.student(t)
	f.take(t, amaya, q, 2)  // 100 %
	f.take(t, ben, q, 1)    // 50 %
	f.take(t, amaya, q2, 2) // chen took neither
	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	groupsURL := "/api/teacher/classrooms/" + f.class.ID + "/report-groups"
	k := "quiz:" + q

	var r grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &r)
	f.teacher.Call("PUT", url, setupFor(r, func(x *grading.ReportItemInput) { x.Weight = 0.5 }), 200, nil)
	put := func(rule string) grading.ClassReport {
		t.Helper()
		var cur, out grading.ClassReport
		f.teacher.Call("GET", url, nil, 200, &cur)
		f.teacher.Call("PUT", url, setupFor(cur, func(x *grading.ReportItemInput) { x.GroupRule = rule }), 200, &out)
		return out
	}
	// The class's groups are the classroom's groups (D-158).
	f.classGroup(t, "Red", amaya, ben)
	f.classGroup(t, "Blue", chen)
	f.classGroup(t, "Readers", amaya) // a student can be in several
	f.teacher.Call("GET", url, nil, 200, &r)
	if len(r.ClassGroups) != 3 || groupOf(r, "Readers").Marks[k].Pct != 100 {
		t.Fatalf("groups: %+v", r.ClassGroups)
	}

	// Average by default: 100 and 50, of 4 marks.
	r = put("")
	m := groupOf(r, "Red").Marks[k]
	if m.Pct != 75 || m.Score != 3 || m.Max != 4 || m.Marked != 2 || m.Points == nil || *m.Points != 37.5 {
		t.Fatalf("average: %+v", m)
	}
	if m := groupOf(r, "Blue").Marks[k]; m.Pct != 0 || m.Marked != 0 {
		t.Fatalf("a group with no marks: %+v", m)
	}
	// On Two, Ben has no mark: he counts 0, so the group's average is 50.
	if m := groupOf(r, "Red").Marks["quiz:"+q2]; m.Pct != 50 || m.Marked != 1 {
		t.Fatalf("a member without a mark: %+v", m)
	}
	red := groupOf(r, "Red")
	if red.Total != 62.5 || red.Final == nil || *red.Final != 62.5 {
		t.Fatalf("group total: %+v", red)
	}
	r = put(grading.RuleTotal)
	if m := groupOf(r, "Red").Marks[k]; m.Score != 6 || m.Max != 8 || m.Pct != 75 {
		t.Fatalf("total: %+v", m)
	}
	r = put(grading.RuleHighest)
	if m := groupOf(r, "Red").Marks[k]; m.Score != 4 || m.Pct != 100 {
		t.Fatalf("highest: %+v", m)
	}
	r = put(grading.RuleLowest)
	if m := groupOf(r, "Red").Marks[k]; m.Score != 2 || m.Pct != 50 {
		t.Fatalf("lowest: %+v", m)
	}

	// An item's own groups sit beside the class's.
	r = grading.ClassReport{}
	f.teacher.Call("PUT", groupsURL, grading.GroupsInput{Kind: "quiz", ID: q, Groups: []grading.GroupInput{{Name: "Solo", Members: []string{amaya.User.ID}}}}, 200, &r)
	if len(r.ItemGroups) != 1 || r.ItemGroups[0].ID != q || r.ItemGroups[0].Groups[0].Marks[k].Pct != 100 || len(r.ClassGroups) != 3 {
		t.Fatalf("own groups: %+v", r.ItemGroups)
	}
	// None removes them, and leaves the class's alone.
	r = grading.ClassReport{}
	f.teacher.Call("PUT", groupsURL, grading.GroupsInput{Kind: "quiz", ID: q}, 200, &r)
	if len(r.ItemGroups) != 0 || len(r.ClassGroups) != 3 {
		t.Fatalf("after removing: %+v %+v", r.ItemGroups, r.ClassGroups)
	}

	// Checked, and the classroom's teachers' only.
	bad := func(in grading.GroupsInput) {
		t.Helper()
		f.teacher.Call("PUT", groupsURL, in, 422, nil)
	}
	bad(grading.GroupsInput{Kind: "class", Groups: []grading.GroupInput{{Name: "A", Members: []string{amaya.User.ID}}}}) // edited on the Groups tab
	bad(grading.GroupsInput{Kind: "quiz", ID: q, Groups: []grading.GroupInput{{Name: "A", Members: []string{amaya.User.ID}}, {Name: "B", Members: []string{amaya.User.ID}}}})
	bad(grading.GroupsInput{Kind: "quiz", ID: q, Groups: []grading.GroupInput{{Name: "A"}, {Name: "a"}}})
	bad(grading.GroupsInput{Kind: "quiz", ID: q, Groups: []grading.GroupInput{{Name: " "}}})
	bad(grading.GroupsInput{Kind: "quiz", ID: q, Groups: []grading.GroupInput{{Name: "A", Members: []string{f.e.NewUser(auth.RoleStudent).User.ID}}}})
	bad(grading.GroupsInput{Kind: "quiz", ID: "00000000-0000-0000-0000-000000000000"})
	f.e.NewUser(auth.RoleTeacher).Call("PUT", groupsURL, grading.GroupsInput{Kind: "class"}, 404, nil)
	amaya.Call("PUT", groupsURL, grading.GroupsInput{Kind: "class"}, 403, nil)
}

// D-141: a scored poll students signed in to is an assignment in the report.
func TestPollInClassReport(t *testing.T) {
	f := newReportFx(t)
	ctx := context.Background()
	s := f.student(t)
	poll := func(title, identity string, scoring bool) string {
		var id string
		if err := f.e.Pool.QueryRow(ctx, `INSERT INTO poll.polls (teacher_id, classroom_id, title, join_code, identity, status, scoring)
			VALUES ($1, $2, $3, substr(md5(random()::text), 1, 8), $4, 'closed', $5) RETURNING id::text`, f.teacher.User.ID, f.class.ID, title, identity, scoring).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var q string
		if err := f.e.Pool.QueryRow(ctx, `INSERT INTO poll.questions (poll_id, position, type, text, key, points) VALUES ($1, 0, 'SINGLE', 'Q', '{"correct":["a"]}', 100) RETURNING id::text`, id).Scan(&q); err != nil {
			t.Fatal(err)
		}
		if _, err := f.e.Pool.Exec(ctx, `INSERT INTO poll.questions (poll_id, position, type, text, key, points) VALUES ($1, 1, 'SINGLE', 'Q2', '{"correct":["a"]}', 100)`, id); err != nil {
			t.Fatal(err)
		}
		var pa string
		if err := f.e.Pool.QueryRow(ctx, `INSERT INTO poll.participants (poll_id, user_id) VALUES ($1, $2) RETURNING id::text`, id, s.User.ID).Scan(&pa); err != nil {
			t.Fatal(err)
		}
		if _, err := f.e.Pool.Exec(ctx, `INSERT INTO poll.responses (participant_id, question_id, poll_id, value, score, correct) VALUES ($1, $2, $3, '{}', 100, true)`, pa, q, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	scored := poll("Scored", "identified", true)
	poll("Opinions", "identified", false)
	poll("Anonymous", "anonymous", true)

	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	var r grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &r)
	if len(r.Items) != 1 || r.Items[0].Kind != "poll" || r.Items[0].ID != scored {
		t.Fatalf("items: %+v", r.Items)
	}
	m := r.Students[0].Marks["poll:"+scored]
	if m.Score != 100 || m.Max != 200 || m.Pct != 50 {
		t.Fatalf("mark: %+v", m)
	}
	// It takes a weight, and counts in the total; what students see is the poll's own.
	in := setupFor(r, func(x *grading.ReportItemInput) { x.Weight = 0.1 })
	r = grading.ClassReport{}
	f.teacher.Call("PUT", url, in, 200, &r)
	if r.Students[0].Total != 5 || r.OutOf != 10 {
		t.Fatalf("total: %+v", r.Students[0])
	}
	bad := setupFor(r, func(x *grading.ReportItemInput) { x.Detail = grading.DetailMarks })
	f.teacher.Call("PUT", url, bad, 422, nil)
}

// D-141: a classroom's report header and footer.
func TestReportHeader(t *testing.T) {
	f := newReportFx(t)
	url := "/api/teacher/classrooms/" + f.class.ID + "/report-header"
	var h grading.ReportHeader
	f.teacher.Call("GET", url, nil, 200, &h)
	if h.Classroom != "Science" || h.Teacher == "" || h.DateMode != "today" || !h.ShowTeacher || !h.PageNumbers {
		t.Fatalf("defaults: %+v", h)
	}
	h.Title, h.Footer, h.DateMode, h.DateValue, h.ShowTeacher = " Term report ", "© 2026 RUSL", "custom", "2026-11-06", false
	var got grading.ReportHeader
	f.teacher.Call("PUT", url, h, 200, &got)
	if got.Title != "Term report" || got.Footer != "© 2026 RUSL" || got.DateValue != "2026-11-06" || got.ShowTeacher {
		t.Fatalf("saved: %+v", got)
	}
	h.DateValue = "soon"
	f.teacher.Call("PUT", url, h, 422, nil)
	h.DateMode, h.DateValue = "someday", ""
	f.teacher.Call("PUT", url, h, 422, nil)
	f.e.NewUser(auth.RoleTeacher).Call("GET", url, nil, 404, nil)
}

// D-143: a logo must be a real PNG or JPEG of a sensible size, and a colour a colour.
func TestReportLogoAndColour(t *testing.T) {
	f := newReportFx(t)
	url := "/api/teacher/classrooms/" + f.class.ID + "/report-header"
	var h grading.ReportHeader
	f.teacher.Call("GET", url, nil, 200, &h)
	if h.Logo != "" || h.Accent != "#1e4fa3" || !h.ColorMarks {
		t.Fatalf("defaults: %+v", h)
	}
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), 1, 2, 3))
	h.Logo, h.Accent, h.ColorMarks = png, "#0F766E", false
	var got grading.ReportHeader
	f.teacher.Call("PUT", url, h, 200, &got)
	if got.Logo != png || got.Accent != "#0f766e" || got.ColorMarks {
		t.Fatalf("saved: %+v", got)
	}
	got = grading.ReportHeader{}
	f.teacher.Call("GET", url, nil, 200, &got)
	if got.Logo != png {
		t.Fatalf("read back: %q", got.Logo)
	}

	bad := func(logo, accent string) {
		t.Helper()
		x := h
		x.Logo, x.Accent = logo, accent
		f.teacher.Call("PUT", url, x, 422, nil)
	}
	bad("data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString([]byte("<svg onload=alert(1)>")), "#0f766e")
	bad("data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("<svg onload=alert(1)>")), "#0f766e") // not what it says
	bad("data:image/png;base64,not base64!", "#0f766e")
	bad("https://example.com/logo.png", "#0f766e")
	bad("data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, 300*1024)...)), "#0f766e") // too big
	bad("", "red")
	bad("", "#12345")

	// Taken away again.
	h.Logo = ""
	got = grading.ReportHeader{}
	f.teacher.Call("PUT", url, h, 200, &got)
	if got.Logo != "" {
		t.Fatalf("not removed: %q", got.Logo)
	}
}

// D-146: a classroom's mark colours are ranges its teacher sets, each with a colour.
func TestReportColorRanges(t *testing.T) {
	f := newReportFx(t)
	url := "/api/teacher/classrooms/" + f.class.ID + "/report-header"
	var h grading.ReportHeader
	f.teacher.Call("GET", url, nil, 200, &h)
	if len(h.ColorRanges) != 3 || h.ColorRanges[0].From != 75 || h.ColorRanges[1].From != 50 || h.ColorRanges[2].From != 0 {
		t.Fatalf("defaults: %+v", h.ColorRanges)
	}
	h.ColorRanges = []grading.ColorRange{{From: 90, Color: "#CCFBF1"}, {From: 70, Color: "#dbeafe"}, {From: 40, Color: "#ffedd5"}, {From: 0, Color: "#7f1d1d"}}
	f.teacher.Call("PUT", url, h, 200, nil)
	var got grading.ReportHeader
	f.teacher.Call("GET", url, nil, 200, &got)
	if len(got.ColorRanges) != 4 || got.ColorRanges[0] != (grading.ColorRange{From: 90, Color: "#ccfbf1"}) || got.ColorRanges[3] != (grading.ColorRange{From: 0, Color: "#7f1d1d"}) {
		t.Fatalf("saved: %+v", got.ColorRanges)
	}
	// One range colours every mark alike.
	h.ColorRanges = []grading.ColorRange{{From: 0, Color: "#dbeafe"}}
	f.teacher.Call("PUT", url, h, 200, nil)

	c := "#dbeafe"
	for name, bad := range map[string][]grading.ColorRange{
		"none":         {},
		"too many":     {{90, c}, {80, c}, {70, c}, {60, c}, {50, c}, {40, c}, {0, c}},
		"not from 0":   {{50, c}, {10, c}},
		"over 100":     {{101, c}, {0, c}},
		"out of order": {{40, c}, {60, c}, {0, c}},
		"the same":     {{50, c}, {50, c}, {0, c}},
		"below 0":      {{50, c}, {-1, c}},
		"not a colour": {{50, "red;background:url(x)"}, {0, c}},
	} {
		x := h
		x.ColorRanges = bad
		t.Run(name, func(t *testing.T) { f.teacher.Call("PUT", url, x, 422, nil) })
	}
}

// D-165: a poll's marked presentations are an item of the class report: a
// student's own, and a group's for each of its members.
func TestPresentationsInClassReport(t *testing.T) {
	f := newReportFx(t)
	ctx := context.Background()
	amaya, ben, chen := f.student(t), f.student(t), f.student(t)
	f.classGroup(t, "Lions", amaya, ben)
	var id string
	if err := f.e.Pool.QueryRow(ctx, `INSERT INTO poll.polls (teacher_id, classroom_id, title, join_code, identity, status)
		VALUES ($1, $2, 'Talks', substr(md5(random()::text), 1, 8), 'identified', 'closed') RETURNING id::text`, f.teacher.User.ID, f.class.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	f.teacher.Call("PUT", "/api/teacher/polls/"+id+"/presentations", map[string]any{"mode": "class_group", "out_of": 20}, 200, nil)
	var lions string
	if err := f.e.Pool.QueryRow(ctx, `SELECT id::text FROM content.categories WHERE classroom_id = $1 AND name = 'Lions'`, f.class.ID).Scan(&lions); err != nil {
		t.Fatal(err)
	}
	var sub string
	if err := f.e.Pool.QueryRow(ctx, `INSERT INTO poll.submissions (poll_id, owner_kind, owner_id, title, slides, ready) VALUES ($1, 'class_group', $2, 'Volcanoes', 1, true) RETURNING id::text`, id, lions).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	url := "/api/teacher/classrooms/" + f.class.ID + "/class-report"
	var r grading.ClassReport
	f.teacher.Call("GET", url, nil, 200, &r)
	for _, it := range r.Items {
		if it.Kind == "presentation" {
			t.Fatal("in the report before a mark")
		}
	}
	f.teacher.Call("PUT", "/api/teacher/poll-submissions/"+sub+"/mark", map[string]any{"mark": 15}, 204, nil)
	f.teacher.Call("GET", url, nil, 200, &r)
	found := false
	for _, it := range r.Items {
		found = found || (it.Kind == "presentation" && it.ID == id)
	}
	if !found {
		t.Fatalf("items: %+v", r.Items)
	}
	for _, st := range r.Students {
		m, ok := st.Marks["presentation:"+id]
		switch st.UserID {
		case amaya.User.ID, ben.User.ID:
			if !ok || m.Score != 15 || m.Max != 20 || m.Pct != 75 {
				t.Fatalf("%s: %+v", st.Name, m)
			}
		case chen.User.ID:
			if ok {
				t.Fatalf("not in the group: %+v", m)
			}
		}
	}
}
