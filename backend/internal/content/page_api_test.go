package content_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
)

// Classrooms, students, modules, topics and a student's own classrooms are
// paginated, searched and sorted on the server (PL-FR-01 to PL-FR-03).
func TestContentListsArePaginated(t *testing.T) {
	e := apptest.New(t)
	ctx := context.Background()
	teacher := e.NewUser(auth.RoleTeacher)

	for i := range 7 {
		teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": fmt.Sprintf("Class %c", 'G'-i)}, 201, nil)
	}
	all := teacher.CheckPages("/api/teacher/classrooms", "classrooms", "id", 7, 3)
	if all[0]["name"] != "Class A" {
		t.Fatalf("newest first: %v", all[0]["name"])
	}
	if byName := teacher.CheckPages("/api/teacher/classrooms?sort=name", "classrooms", "id", 7, 3); byName[0]["name"] != "Class A" || byName[6]["name"] != "Class G" {
		t.Fatalf("by name: %v … %v", byName[0]["name"], byName[6]["name"])
	}
	if p := teacher.Page("/api/teacher/classrooms?q=class%20c", "classrooms", 1, 25); p.Total != 1 {
		t.Fatalf("search: %d", p.Total)
	}

	// Students: 9 enrolments, searched by name or student ID, sorted by name.
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Big class"}, 201, &c)
	for i := range 9 {
		var uid string
		if err := e.Pool.QueryRow(ctx, `INSERT INTO auth.users (email, password_hash, name, role) VALUES ($1, 'x', $2, 'student') RETURNING id`,
			fmt.Sprintf("kid%d@example.com", i), fmt.Sprintf("Kid %c", 'I'-i)).Scan(&uid); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Pool.Exec(ctx, `INSERT INTO content.enrolments (classroom_id, user_id, status, student_number) VALUES ($1, $2, 'active', $3)`,
			c.ID, uid, fmt.Sprintf("S%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/teacher/classrooms/" + c.ID + "/enrolments"
	byName := teacher.CheckPages(path+"?sort=name", "enrolments", "id", 9, 4)
	if byName[0]["student_name"] != "Kid A" || byName[8]["student_name"] != "Kid I" {
		t.Fatalf("students by name: %v … %v", byName[0]["student_name"], byName[8]["student_name"])
	}
	if p := teacher.Page(path+"?q=s007", "enrolments", 1, 25); p.Total != 1 || p.Rows[0]["student_name"] != "Kid B" {
		t.Fatalf("search by student ID: %+v", p)
	}
	if p := teacher.Page(path+"?q=kid%20c", "enrolments", 1, 25); p.Total != 1 {
		t.Fatalf("search by name: %d", p.Total)
	}

	// Modules and topics keep the teacher's order by default.
	for i := range 5 {
		teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": fmt.Sprintf("Module %d", i)}, 201, nil)
	}
	mods := teacher.CheckPages("/api/teacher/classrooms/"+c.ID+"/modules", "modules", "id", 5, 2)
	if mods[0]["name"] != "Module 0" || mods[4]["name"] != "Module 4" {
		t.Fatalf("modules in order: %v … %v", mods[0]["name"], mods[4]["name"])
	}
	modID := mods[0]["id"].(string)
	for i := range 4 {
		teacher.Call("POST", "/api/teacher/modules/"+modID+"/topics", map[string]any{"name": fmt.Sprintf("Topic %d", i)}, 201, nil)
	}
	teacher.CheckPages("/api/teacher/modules/"+modID+"/topics", "topics", "id", 4, 3)

	// A student sees a page of their own classrooms.
	student := e.NewUser(auth.RoleStudent)
	for i := range 3 {
		var cc content.Classroom
		teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": fmt.Sprintf("Joined %d", i)}, 201, &cc)
		student.Call("POST", "/api/enrolments", map[string]any{"join_code": cc.JoinCode}, 200, nil)
	}
	mine := student.CheckPages("/api/my/classrooms", "classrooms", "classroom_id", 3, 2)
	if mine[0]["name"] != "Joined 0" {
		t.Fatalf("my classrooms by name: %v", mine[0]["name"])
	}
}
