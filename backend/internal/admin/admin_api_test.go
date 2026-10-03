package admin_test

import (
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/admin"
	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func TestUsageAndAudit(t *testing.T) {
	e := apptest.New(t)
	ad := e.NewUser(auth.RoleAdmin)
	teacher := e.NewUser(auth.RoleTeacher)
	student := e.NewUser(auth.RoleStudent)
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Secret class name"}, 201, nil)
	ad.Call("POST", "/api/admin/users/"+student.User.ID+"/status", map[string]string{"status": "suspended"}, 204, nil)

	var u admin.Usage
	ad.Call("GET", "/api/admin/usage", nil, 200, &u)
	if u.UsersByRole["teacher"] != 1 || u.UsersByRole["student"] != 1 || u.UsersByStatus["suspended"] != 1 || u.Classrooms != 1 {
		t.Fatalf("usage = %+v", u)
	}
	var a struct{ Events []admin.AuditEvent }
	ad.Call("GET", "/api/admin/audit?action=user_status_changed", nil, 200, &a)
	if len(a.Events) != 1 || a.Events[0].ActorName != "Admin" || a.Events[0].TargetID != student.User.ID {
		t.Fatalf("audit = %+v", a.Events)
	}
	_, raw := ad.Do("GET", "/api/admin/usage", nil)
	if strings.Contains(string(raw), "Secret class name") {
		t.Fatal("admin usage exposes teacher content")
	}
	teacher.Call("GET", "/api/admin/usage", nil, 403, nil)
	teacher.Call("GET", "/api/admin/audit", nil, 403, nil)
}

func TestMyDataExportAndSelfDelete(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Maths"}, 201, &c)
	s := e.NewUser(auth.RoleStudent)
	s.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "ST-9"}, 200, nil)

	var d map[string]any
	s.Call("GET", "/api/my/data", nil, 200, &d)
	if d["profile"].(map[string]any)["email"] != s.User.Email {
		t.Fatalf("profile = %v", d["profile"])
	}
	en := d["enrolments"].([]any)
	if len(en) != 1 || en[0].(map[string]any)["student_number"] != "ST-9" {
		t.Fatalf("enrolments = %v", en)
	}
	s.Call("DELETE", "/api/auth/me", map[string]string{"password": "wrong-password"}, 401, nil)
	s.Call("DELETE", "/api/auth/me", map[string]string{"password": "password123"}, 204, nil)
	s.Call("GET", "/api/auth/me", nil, 401, nil)
	e.Client().Call("POST", "/api/auth/login", map[string]string{"email": s.User.Email, "password": "password123"}, 401, nil)

	ad := e.NewUser(auth.RoleAdmin)
	ad.Call("DELETE", "/api/auth/me", map[string]string{"password": "password123"}, 400, nil)
}
