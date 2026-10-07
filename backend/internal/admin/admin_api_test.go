package admin_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
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
	if v, isText := d["avatar"].(string); isText {
		t.Fatalf("no picture yet: %.40v", v) // shown as an empty section, like the others
	}
	// The profile picture is part of the export, and goes with the account.
	var pic bytes.Buffer
	_ = png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 20, 20)))
	if code, body := s.Raw("PUT", "/api/auth/me/avatar", "image/png", pic.Bytes()); code != 200 {
		t.Fatalf("avatar: %d %s", code, body)
	}
	s.Call("GET", "/api/my/data", nil, 200, &d)
	if url, _ := d["avatar"].(string); !strings.HasPrefix(url, "data:image/jpeg;base64,/9j/") {
		t.Fatalf("avatar in export: %.60v", d["avatar"])
	}
	s.Call("DELETE", "/api/auth/me", map[string]string{"password": "wrong-password"}, 401, nil)
	s.Call("DELETE", "/api/auth/me", map[string]string{"password": "password123"}, 204, nil)
	s.Call("GET", "/api/auth/me", nil, 401, nil)
	e.Client().Call("POST", "/api/auth/login", map[string]string{"email": s.User.Email, "password": "password123"}, 401, nil)
	var pics int
	_ = e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM auth.avatars WHERE user_id=$1`, s.User.ID).Scan(&pics)
	if pics != 0 {
		t.Fatal("picture kept after the account was deleted")
	}

	ad := e.NewUser(auth.RoleAdmin)
	ad.Call("DELETE", "/api/auth/me", map[string]string{"password": "password123"}, 400, nil)
}
