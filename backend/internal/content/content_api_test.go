package content_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func newClassroom(t *testing.T, teacher *apptest.Client, body map[string]any) content.Classroom {
	t.Helper()
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", body, 201, &c)
	return c
}

func TestClassroomCRUDAndHierarchy(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)

	c := newClassroom(t, teacher, map[string]any{"name": "Grade 10 Science", "settings": map[string]any{"countdown_seconds": 45}})
	if len(c.JoinCode) != 8 {
		t.Fatalf("join code %q", c.JoinCode)
	}
	var got content.Classroom
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID, nil, 200, &got)
	if got.Effective == nil || got.Effective.CountdownSeconds != 45 || got.Effective.AdmissionMode != "manual" {
		t.Fatalf("effective = %+v", got.Effective)
	}

	teacher.Call("PATCH", "/api/teacher/classrooms/"+c.ID, map[string]any{"name": "Grade 10 Physics"}, 200, &got)
	if got.Name != "Grade 10 Physics" || got.Effective.CountdownSeconds != 45 {
		t.Fatalf("patch lost data: %+v", got)
	}

	var m1, m2 content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "Term 1"}, 201, &m1)
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "Term 2"}, 201, &m2)
	if m1.Position != 0 || m2.Position != 1 {
		t.Fatalf("positions %d %d", m1.Position, m2.Position)
	}
	teacher.Call("PATCH", "/api/teacher/modules/"+m2.ID, map[string]any{"position": -1}, 204, nil)
	var mods struct{ Modules []content.Module }
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/modules", nil, 200, &mods)
	if len(mods.Modules) != 2 || mods.Modules[0].ID != m2.ID {
		t.Fatalf("reorder failed: %+v", mods)
	}

	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m1.ID+"/topics", map[string]any{"name": "Motion", "settings": map[string]any{"quiz_time_limit_sec": 600}}, 201, &topic)
	tc, err := e.App.Content.TopicContext(context.Background(), teacher.User.ID, topic.ID)
	if err != nil || tc.ClassroomID != c.ID || *tc.Topic.QuizTimeLimitSec != 600 || *tc.Classroom.CountdownSeconds != 45 {
		t.Fatalf("topic context = %+v, %v", tc, err)
	}

	var td content.TopicDetail
	teacher.Call("GET", "/api/teacher/topics/"+topic.ID, nil, 200, &td)
	if td.Name != "Motion" || td.ClassroomID != c.ID || td.ModuleName != "Term 1" || *td.Settings.QuizTimeLimitSec != 600 {
		t.Fatalf("topic detail = %+v", td)
	}

	// Settings are validated against the level they are set at.
	teacher.Call("POST", "/api/teacher/modules/"+m1.ID+"/topics", map[string]any{"name": "X", "settings": map[string]any{"admission_mode": "auto"}}, 422, nil)

	teacher.Call("DELETE", "/api/teacher/modules/"+m1.ID, nil, 204, nil)
	teacher.Call("PATCH", "/api/teacher/topics/"+topic.ID, map[string]any{"name": "gone"}, 404, nil)

	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/archive", nil, 204, nil)
	var list struct{ Classrooms []content.Classroom }
	teacher.Call("GET", "/api/teacher/classrooms", nil, 200, &list)
	if len(list.Classrooms) != 0 {
		t.Fatal("archived classroom listed by default")
	}
	teacher.Call("GET", "/api/teacher/classrooms?archived=1", nil, 200, &list)
	if len(list.Classrooms) != 1 || !list.Classrooms[0].Archived {
		t.Fatalf("archived list = %+v", list)
	}
	teacher.Call("DELETE", "/api/teacher/classrooms/"+c.ID, nil, 204, nil)
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID, nil, 404, nil)
}

func TestTeacherIsolation(t *testing.T) {
	e := apptest.New(t)
	alice, bob := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleTeacher)
	c := newClassroom(t, alice, map[string]any{"name": "Alice's class"})
	var m content.Module
	alice.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)

	// BR-14: Bob can neither see nor change Alice's content.
	bob.Call("GET", "/api/teacher/classrooms/"+c.ID, nil, 404, nil)
	bob.Call("PATCH", "/api/teacher/classrooms/"+c.ID, map[string]any{"name": "pwned"}, 404, nil)
	bob.Call("DELETE", "/api/teacher/classrooms/"+c.ID, nil, 404, nil)
	bob.Call("GET", "/api/teacher/classrooms/"+c.ID+"/modules", nil, 404, nil)
	bob.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "x"}, 404, nil)
	bob.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "x"}, 404, nil)
	bob.Call("DELETE", "/api/teacher/modules/"+m.ID, nil, 404, nil)
	bob.Call("GET", "/api/teacher/classrooms/"+c.ID+"/enrolments", nil, 404, nil)

	// Students cannot use teacher endpoints at all.
	e.NewUser(auth.RoleStudent).Call("GET", "/api/teacher/classrooms", nil, 403, nil)
}

func TestEnrolmentWithStudentID(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	c := newClassroom(t, teacher, map[string]any{"name": "C", "settings": map[string]any{"student_id_required": true}})
	s1, s2 := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)

	var p content.ClassroomPreview
	s1.Call("GET", "/api/join/classrooms/"+c.JoinCode, nil, 200, &p)
	if !p.StudentIDRequired || p.Enrolment != nil || p.Name != "C" {
		t.Fatalf("preview = %+v", p)
	}
	// FR-CLS-05: student ID required.
	s1.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 422, nil)
	var en content.Enrolment
	s1.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "ST-001"}, 200, &en)
	if en.Status != "active" || *en.StudentNumber != "ST-001" {
		t.Fatalf("enrolment = %+v", en)
	}
	// FR-CLS-06: unique within the classroom (case-insensitive).
	s2.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "st-001"}, 422, nil)
	s2.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "ST-002"}, 200, nil)
	// Re-enrolling is idempotent and cannot change the number.
	s1.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "OTHER"}, 200, &en)
	if *en.StudentNumber != "ST-001" {
		t.Fatalf("student number changed to %s", *en.StudentNumber)
	}
	// The same number is fine in another classroom.
	c2 := newClassroom(t, teacher, map[string]any{"name": "C2", "settings": map[string]any{"student_id_required": true}})
	s2.Call("POST", "/api/enrolments", map[string]string{"join_code": c2.JoinCode, "student_number": "ST-001"}, 200, nil)

	var list struct{ Enrolments []content.Enrolment }
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/enrolments", nil, 200, &list)
	if len(list.Enrolments) != 2 || list.Enrolments[0].StudentName == "" {
		t.Fatalf("enrolments = %+v", list)
	}
	var mine struct{ Classrooms []content.StudentClassroom }
	s2.Call("GET", "/api/my/classrooms", nil, 200, &mine)
	if len(mine.Classrooms) != 2 {
		t.Fatalf("my classrooms = %+v", mine)
	}
}

func TestEnrolmentApprovalFlow(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	c := newClassroom(t, teacher, map[string]any{"name": "C", "settings": map[string]any{"enrolment_approval": true}})
	s := e.NewUser(auth.RoleStudent)
	var en content.Enrolment
	s.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, &en)
	if en.Status != "pending" {
		t.Fatalf("status = %s", en.Status)
	}
	teacher.Call("PATCH", "/api/teacher/enrolments/"+en.ID, map[string]string{"status": "active"}, 200, &en)
	if en.Status != "active" {
		t.Fatal("approve failed")
	}
	teacher.Call("PATCH", "/api/teacher/enrolments/"+en.ID, map[string]string{"status": "rejected"}, 200, nil)
	s.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 403, nil)
	teacher.Call("PATCH", "/api/teacher/enrolments/"+en.ID, map[string]string{"status": "bogus"}, 422, nil)
}

func TestEnsureEnrolledForSessionJoin(t *testing.T) {
	e := apptest.New(t)
	ctx := context.Background()
	teacher := e.NewUser(auth.RoleTeacher)
	open := newClassroom(t, teacher, map[string]any{"name": "open"})
	closed := newClassroom(t, teacher, map[string]any{"name": "closed", "settings": map[string]any{"auto_enrol_on_join": false}})
	s := e.NewUser(auth.RoleStudent)

	// BR-02: auto-enrol on QR join when the classroom allows it.
	en, err := e.App.Content.EnsureEnrolled(ctx, open.ID, s.User.ID, "")
	if err != nil || en.Status != "active" {
		t.Fatalf("auto-enrol: %+v %v", en, err)
	}
	if _, err := e.App.Content.EnsureEnrolled(ctx, closed.ID, s.User.ID, ""); err == nil {
		t.Fatal("joined a classroom that requires prior enrolment")
	}
	s.Call("POST", "/api/enrolments", map[string]string{"join_code": closed.JoinCode}, 200, nil)
	if _, err := e.App.Content.EnsureEnrolled(ctx, closed.ID, s.User.ID, ""); err != nil {
		t.Fatalf("enrolled student rejected: %v", err)
	}
}

func TestTeacherAndPlatformSettingsCascade(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	teacher := e.NewUser(auth.RoleTeacher)
	admin.Call("PUT", "/api/admin/settings", map[string]any{"pass_mark_pct": 40, "countdown_seconds": 90}, 200, nil)
	teacher.Call("PUT", "/api/teacher/settings", map[string]any{"countdown_seconds": 30}, 200, nil)
	teacher.Call("PUT", "/api/teacher/settings", map[string]any{"admission_mode": "auto"}, 422, nil) // not a teacher-level key

	c := newClassroom(t, teacher, map[string]any{"name": "C"})
	var got content.Classroom
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID, nil, 200, &got)
	if got.Effective.PassMarkPct != 40 || got.Effective.CountdownSeconds != 30 {
		t.Fatalf("cascade platform→teacher→classroom wrong: %+v", got.Effective)
	}
	teacher.Call("GET", "/api/admin/settings", nil, 403, nil)
}

func TestCategories(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var a, b content.Enrolment
	s1, s2 := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	for i, s := range []*apptest.Client{s1, s2} {
		en, err := e.App.Content.EnsureEnrolled(context.Background(), c.ID, s.User.ID, fmt.Sprintf("S%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			a = en
		} else {
			b = en
		}
	}

	var red, blue content.Category
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/categories", map[string]any{"name": "Group A"}, 201, &red)
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/categories", map[string]any{"name": "Needs support", "color": 6}, 201, &blue)
	if red.Color != 1 || blue.Color != 6 {
		t.Fatalf("colors: %d %d", red.Color, blue.Color)
	}
	if code, _ := teacher.Do("POST", "/api/teacher/classrooms/"+c.ID+"/categories", map[string]any{"name": "group a"}); code != 409 {
		t.Fatalf("duplicate name (case-insensitive): %d", code)
	}
	if code, _ := teacher.Do("POST", "/api/teacher/classrooms/"+c.ID+"/categories", map[string]any{"name": "x", "color": 9}); code != 422 {
		t.Fatalf("bad color: %d", code)
	}

	teacher.Call("POST", "/api/teacher/categories/"+red.ID+"/members", map[string]any{"enrolment_ids": []string{a.ID, b.ID}, "assigned": true}, 200, nil)
	teacher.Call("POST", "/api/teacher/categories/"+blue.ID+"/members", map[string]any{"enrolment_ids": []string{a.ID}, "assigned": true}, 200, nil)
	var list struct{ Enrolments []content.Enrolment }
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/enrolments", nil, 200, &list)
	got := map[string]int{}
	for _, en := range list.Enrolments {
		got[en.ID] = len(en.Categories)
	}
	if got[a.ID] != 2 || got[b.ID] != 1 {
		t.Fatalf("categories per student: %v", got)
	}
	var cats struct{ Categories []content.Category }
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/categories", nil, 200, &cats)
	if len(cats.Categories) != 2 || cats.Categories[0].Members != 2 {
		t.Fatalf("category list: %+v", cats.Categories)
	}
	members, err := e.App.Content.CategoryMembers(context.Background(), teacher.User.ID, c.ID)
	if err != nil || len(members) != 3 {
		t.Fatalf("members for grouping: %v %v", members, err)
	}

	// Removing, renaming, deleting.
	var rm struct{ Affected int }
	teacher.Call("POST", "/api/teacher/categories/"+red.ID+"/members", map[string]any{"enrolment_ids": []string{b.ID}, "assigned": false}, 200, &rm)
	if rm.Affected != 1 {
		t.Fatalf("removed %d", rm.Affected)
	}
	teacher.Call("PATCH", "/api/teacher/categories/"+red.ID, map[string]any{"name": "Team Red"}, 200, nil)
	teacher.Call("DELETE", "/api/teacher/categories/"+blue.ID, nil, 204, nil)
	list.Enrolments = nil // decode fresh: an empty category list is omitted from the JSON
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/enrolments", nil, 200, &list)
	for _, en := range list.Enrolments {
		if en.ID == a.ID && (len(en.Categories) != 1 || en.Categories[0] != red.ID) {
			t.Fatalf("after delete: %v", en.Categories)
		}
		if en.ID == b.ID && len(en.Categories) != 0 {
			t.Fatalf("after removal: %v", en.Categories)
		}
	}

	// Another teacher can't see or change them, nor add their own students.
	other := e.NewUser(auth.RoleTeacher)
	if code, _ := other.Do("GET", "/api/teacher/classrooms/"+c.ID+"/categories", nil); code != 404 {
		t.Fatalf("foreign list: %d", code)
	}
	if code, _ := other.Do("POST", "/api/teacher/categories/"+red.ID+"/members", map[string]any{"enrolment_ids": []string{a.ID}, "assigned": true}); code != 404 {
		t.Fatalf("foreign assign: %d", code)
	}
	var oc content.Classroom
	other.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "O"}, 201, &oc)
	var ocat content.Category
	other.Call("POST", "/api/teacher/classrooms/"+oc.ID+"/categories", map[string]any{"name": "X"}, 201, &ocat)
	var res struct{ Affected int }
	other.Call("POST", "/api/teacher/categories/"+ocat.ID+"/members", map[string]any{"enrolment_ids": []string{a.ID}, "assigned": true}, 200, &res)
	if res.Affected != 0 {
		t.Fatal("a student of another classroom was added to a category")
	}
}
