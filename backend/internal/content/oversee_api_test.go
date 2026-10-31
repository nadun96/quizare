package content_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
)

type overseen struct {
	Classrooms []content.OverseenClassroom
	Total      int
}

// Admins and managers given "Classes" see every classroom and run it:
// students in and out, rename, archive, move it to another teacher, delete
// (D-101).
func TestOverseeingClassrooms(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	ann, bob := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleTeacher)
	s1, s2 := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	tr := build(t, e, ann, "Physics 7")
	var other content.Classroom
	bob.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Bob's art", "settings": map[string]any{"enrolment_approval": true}}, 201, &other)
	s1.Call("POST", "/api/enrolments", map[string]string{"join_code": other.JoinCode}, 200, nil)

	// Every classroom, searched by its teacher's name too.
	var l overseen
	admin.Call("GET", "/api/admin/classrooms?sort=name", nil, 200, &l)
	if l.Total != 2 || l.Classrooms[0].Name != "Bob's art" || l.Classrooms[0].Pending != 1 || l.Classrooms[1].TeacherEmail != ann.User.Email {
		t.Fatalf("list = %+v", l)
	}
	admin.Call("GET", "/api/admin/classrooms?q="+bob.User.Email, nil, 200, &l)
	if l.Total != 1 || l.Classrooms[0].ID != other.ID {
		t.Fatalf("search = %+v", l)
	}

	// Students: approve the waiting one, add one by email, remove one.
	var st struct{ Students []content.OverseenEnrolment }
	admin.Call("GET", "/api/admin/classrooms/"+other.ID+"/students", nil, 200, &st)
	if len(st.Students) != 1 || st.Students[0].Status != "pending" {
		t.Fatalf("students = %+v", st)
	}
	admin.Call("PUT", "/api/admin/classrooms/"+other.ID+"/students/"+st.Students[0].ID, map[string]string{"status": "active"}, 204, nil)
	admin.Call("POST", "/api/admin/classrooms/"+other.ID+"/students", map[string]string{"email": s2.User.Email}, 204, nil)
	admin.Call("POST", "/api/admin/classrooms/"+other.ID+"/students", map[string]string{"email": ann.User.Email}, 422, nil) // not a student
	var c content.OverseenClassroom
	admin.Call("GET", "/api/admin/classrooms/"+other.ID, nil, 200, &c)
	if c.Students != 2 || c.Pending != 0 {
		t.Fatalf("after = %+v", c)
	}
	admin.Call("PUT", "/api/admin/classrooms/"+other.ID+"/students/"+st.Students[0].ID, map[string]string{"status": "removed"}, 204, nil)

	// Rename and archive.
	admin.Call("PATCH", "/api/admin/classrooms/"+other.ID, map[string]any{"name": "Art 9", "archived": true}, 200, &c)
	if c.Name != "Art 9" || !c.Archived {
		t.Fatalf("patched = %+v", c)
	}

	// Move Ann's classroom, with its quiz, to Bob: Bob runs it now.
	var moved struct {
		Classroom content.OverseenClassroom
		Quizzes   int
	}
	admin.Call("POST", "/api/admin/classrooms/"+tr.classroom.ID+"/owner", map[string]string{"teacher_email": "nobody@example.com"}, 422, nil)
	admin.Call("POST", "/api/admin/classrooms/"+tr.classroom.ID+"/owner", map[string]string{"teacher_email": bob.User.Email}, 200, &moved)
	if moved.Classroom.TeacherID != bob.User.ID || moved.Quizzes < 1 {
		t.Fatalf("moved = %+v", moved)
	}
	bob.Call("GET", "/api/teacher/quizzes/"+tr.quizID, nil, 200, nil)
	ann.Call("GET", "/api/teacher/quizzes/"+tr.quizID, nil, 404, nil)
	ann.Call("GET", "/api/teacher/classrooms/"+tr.classroom.ID, nil, 404, nil)

	// Delete, as the teacher would.
	admin.Call("DELETE", "/api/admin/classrooms/"+tr.classroom.ID, nil, 204, nil)
	bob.Call("GET", "/api/teacher/classrooms/"+tr.classroom.ID, nil, 404, nil)

	// Managers need "Classes"; teachers and students never see it.
	mgr := e.NewUser(auth.RoleTeacher)
	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": mgr.User.ID, "features": []string{"usage"}}, 201, nil)
	mgr.Call("GET", "/api/admin/classrooms", nil, 404, nil)
	admin.Call("PUT", "/api/admin/managers/"+mgr.User.ID+"/features", map[string]any{"features": []string{"manage_classes"}}, 200, nil)
	mgr.Call("GET", "/api/admin/classrooms", nil, 200, nil)
	s2.Call("GET", "/api/admin/classrooms", nil, 403, nil)
}
