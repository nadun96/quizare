package auth_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
)

func role(t *testing.T, c *apptest.Client) auth.Role {
	t.Helper()
	var me auth.User
	c.Call("GET", "/api/auth/me", nil, 200, &me)
	return me.Role
}

// Admins change anyone's role; managers only students and teachers; the
// last admin stays; a teacher with classrooms isn't made a student (D-101).
func TestChangingRoles(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	student, teacher := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleTeacher)
	path := func(u *apptest.Client) string { return "/api/admin/users/" + u.User.ID + "/role" }

	// Up and down, and the session sees it at once.
	admin.Call("POST", path(student), map[string]string{"role": "teacher"}, 204, nil)
	if role(t, student) != auth.RoleTeacher {
		t.Fatal("not a teacher")
	}
	student.Call("GET", "/api/teacher/classrooms", nil, 200, nil)
	admin.Call("POST", path(student), map[string]string{"role": "student"}, 204, nil)
	student.Call("GET", "/api/teacher/classrooms", nil, 403, nil)
	admin.Call("POST", path(student), map[string]string{"role": "manager"}, 400, nil)
	admin.Call("POST", path(admin), map[string]string{"role": "teacher"}, 400, nil) // not oneself

	// A teacher who owns a classroom stays a teacher until it moves.
	var c struct{ ID string }
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Mine"}, 201, &c)
	var bad struct{ Code string }
	admin.Call("POST", path(teacher), map[string]string{"role": "student"}, 409, &bad)
	if bad.Code != "owns_classrooms" {
		t.Fatalf("owns = %+v", bad)
	}

	// Making an admin, and the last admin.
	admin.Call("POST", path(teacher), map[string]string{"role": "admin"}, 204, nil)
	if role(t, teacher) != auth.RoleAdmin {
		t.Fatal("not an admin")
	}
	admin.Call("POST", path(teacher), map[string]string{"role": "teacher"}, 204, nil)
	other := e.NewUser(auth.RoleAdmin)
	other.Call("POST", path(admin), map[string]string{"role": "teacher"}, 204, nil)
	// Nobody changes their own role, so an admin never demotes themself into none being left.
	other.Call("POST", "/api/admin/users/"+other.User.ID+"/role", map[string]string{"role": "teacher"}, 400, nil)

	// Managers: students and teachers only, never making admins.
	mgr := e.NewUser(auth.RoleTeacher)
	other.Call("POST", "/api/admin/managers", map[string]any{"user_id": mgr.User.ID, "features": []string{"manage_users"}}, 201, nil)
	s2 := e.NewUser(auth.RoleStudent)
	mgr.Call("POST", path(s2), map[string]string{"role": "teacher"}, 204, nil)
	mgr.Call("POST", path(s2), map[string]string{"role": "admin"}, 403, nil)
	mgr.Call("POST", path(other), map[string]string{"role": "student"}, 403, nil)
	// A manager keeps the manager role only as a teacher.
	other.Call("POST", path(mgr), map[string]string{"role": "student"}, 409, &bad)
	if bad.Code != "is_manager" {
		t.Fatalf("manager = %+v", bad)
	}
	// Plain teachers can't.
	teacher.Call("POST", path(s2), map[string]string{"role": "student"}, 403, nil)
}
