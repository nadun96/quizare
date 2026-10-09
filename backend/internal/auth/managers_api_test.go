package auth_test

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
)

// Managers (PL-FR-10 to PL-FR-17, PL-NFR-06 to PL-NFR-09, D-56).

func makeManager(admin, c *apptest.Client, features ...auth.Feature) {
	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": c.User.ID, "features": features}, 201, nil)
}

func me(c *apptest.Client) auth.User {
	var u auth.User
	c.Call("GET", "/api/auth/me", nil, 200, &u)
	return u
}

func refusals(t *testing.T, e *apptest.Env, actorID string) int {
	t.Helper()
	var n int
	if err := e.Pool.QueryRow(t.Context(), `SELECT count(*) FROM audit.events WHERE action='access_refused' AND actor_id=$1`, actorID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func emailCount(t *testing.T, e *apptest.Env, to, subject string) int {
	t.Helper()
	var n int
	if err := e.Pool.QueryRow(t.Context(), `SELECT count(*) FROM river_job WHERE kind='email' AND args->>'to'=$1 AND args->>'subject'=$2`, to, subject).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTeacherManagerKeepsTeachingAndChangesApplyAtOnce(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	teacher := e.NewUser(auth.RoleTeacher)

	// A new manager has nothing until the admin gives some (PL-FR-10, PL-NFR-07).
	makeManager(admin, teacher)
	if m := me(teacher).Manager; m == nil || len(m.Features) != 0 {
		t.Fatalf("manager = %+v, want no features", m)
	}
	if emailCount(t, e, teacher.User.Email, "You're now a manager") != 1 {
		t.Fatal("no email on becoming a manager (PL-NFR-09)")
	}
	// Without the feature: 404, and the attempt is logged (PL-NFR-06).
	teacher.Call("GET", "/api/admin/users", nil, 404, nil)
	if refusals(t, e, teacher.User.ID) != 1 {
		t.Fatal("refused attempt not logged")
	}

	// Given at once, without logging in again (PL-FR-12).
	admin.Call("PUT", "/api/admin/managers/"+teacher.User.ID+"/features", map[string]any{"features": []string{"view_users"}}, 200, nil)
	teacher.Call("GET", "/api/admin/users", nil, 200, nil)
	teacher.Call("GET", "/api/admin/usage", nil, 404, nil)
	if emailCount(t, e, teacher.User.Email, "Your manager features changed") != 1 {
		t.Fatal("no email on changed features")
	}
	// Still a teacher (PL-FR-11).
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Maths"}, 201, nil)

	// Taken away at once too.
	admin.Call("PUT", "/api/admin/managers/"+teacher.User.ID+"/features", map[string]any{"features": []string{}}, 200, nil)
	teacher.Call("GET", "/api/admin/users", nil, 404, nil)

	// Removing the role leaves a plain teacher, still logged in.
	admin.Call("DELETE", "/api/admin/managers/"+teacher.User.ID, nil, 204, nil)
	if me(teacher).Manager != nil {
		t.Fatal("still a manager after removal")
	}
	teacher.Call("GET", "/api/admin/users", nil, 403, nil)
	teacher.Call("GET", "/api/teacher/classrooms", nil, 200, nil)
	if emailCount(t, e, teacher.User.Email, "You're no longer a manager") != 1 {
		t.Fatal("no email on removal")
	}
	admin.Call("DELETE", "/api/admin/managers/"+teacher.User.ID, nil, 404, nil)
}

// Each feature grants exactly its own endpoints (PL-NFR-07).
func TestEachFeatureGrantsOnlyItself(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	student := e.NewUser(auth.RoleStudent)
	endpoints := []struct {
		method, path string
		body         any
		ok           int
		needs        auth.Feature // "" = admins only
	}{
		{"GET", "/api/admin/users", nil, 200, auth.FeatViewUsers},
		{"POST", "/api/admin/users/" + student.User.ID + "/status", map[string]string{"status": "active"}, 204, auth.FeatManageUsers},
		{"GET", "/api/admin/auth-policy", nil, 200, auth.FeatApprovalPolicy},
		{"PUT", "/api/admin/auth-policy", map[string]bool{"require_teacher_approval": false}, 200, auth.FeatApprovalPolicy},
		{"GET", "/api/admin/settings", nil, 200, auth.FeatSettings},
		{"GET", "/api/admin/usage", nil, 200, auth.FeatUsage},
		{"GET", "/api/admin/audit", nil, 200, auth.FeatAudit},
		{"GET", "/api/admin/storage", nil, 200, auth.FeatStorage},
		{"GET", "/api/admin/storage/cleanup", nil, 200, auth.FeatCleanup},
		{"GET", "/api/admin/backups", nil, 200, auth.FeatBackups},
		{"GET", "/api/admin/managers", nil, 200, ""},
		{"POST", "/api/admin/managers", map[string]any{"email": "x@example.com", "name": "X", "features": []string{}}, 201, ""},
	}
	for _, f := range auth.Features {
		t.Run(string(f), func(t *testing.T) {
			m := e.NewUser(auth.RoleTeacher)
			makeManager(admin, m, f)
			granted := []auth.Feature{f}
			if f == auth.FeatManageUsers {
				granted = append(granted, auth.FeatViewUsers) // needs the list to act on it
			}
			for _, ep := range endpoints {
				want := 404
				if ep.needs != "" && slices.Contains(granted, ep.needs) {
					want = ep.ok
				}
				m.Call(ep.method, ep.path, ep.body, want, nil)
			}
		})
	}
}

func TestManagersCannotControlAdminsOrManagers(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	m := e.NewUser(auth.RoleTeacher)
	other := e.NewUser(auth.RoleTeacher)
	makeManager(admin, m, auth.FeatManageUsers)
	makeManager(admin, other, auth.FeatAudit)
	var solo auth.User
	admin.Call("POST", "/api/admin/managers", map[string]any{"email": "solo@example.com", "name": "Solo", "features": []string{}}, 201, &solo)
	student := e.NewUser(auth.RoleStudent)
	teacher := e.NewUser(auth.RoleTeacher)

	// Teachers and students: yes.
	m.Call("POST", "/api/admin/users/"+student.User.ID+"/status", map[string]string{"status": "suspended"}, 204, nil)
	m.Call("DELETE", "/api/admin/users/"+teacher.User.ID, nil, 204, nil)
	// Admins and managers: never, and each try is logged (PL-FR-13).
	for _, id := range []string{admin.User.ID, other.User.ID, solo.ID} {
		m.Call("POST", "/api/admin/users/"+id+"/status", map[string]string{"status": "suspended"}, 403, nil)
		m.Call("DELETE", "/api/admin/users/"+id, nil, 403, nil)
	}
	if n := refusals(t, e, m.User.ID); n != 6 {
		t.Fatalf("refusals logged = %d, want 6", n)
	}
	me(other) // still logged in, not suspended
	// No making managers, nor changing anyone's features, their own included.
	m.Call("POST", "/api/admin/managers", map[string]any{"user_id": student.User.ID, "features": []string{}}, 404, nil)
	m.Call("PUT", "/api/admin/managers/"+m.User.ID+"/features", map[string]any{"features": []string{"usage"}}, 404, nil)
	m.Call("DELETE", "/api/admin/managers/"+other.User.ID, nil, 404, nil)

	// The user list shows who is an admin or a manager, but not their features (PL-FR-14).
	var list struct{ Users []auth.User }
	m.Call("GET", "/api/admin/users?role=manager", nil, 200, &list)
	if len(list.Users) != 3 {
		t.Fatalf("managers listed = %d, want 3", len(list.Users))
	}
	for _, u := range list.Users {
		if u.Manager == nil || len(u.Manager.Features) != 0 {
			t.Fatalf("manager %s shows %+v to a manager", u.Name, u.Manager)
		}
	}
	m.Call("GET", "/api/admin/users?role=admin", nil, 200, &list)
	if len(list.Users) != 1 || list.Users[0].Role != auth.RoleAdmin {
		t.Fatalf("admins = %+v", list.Users)
	}
	// The admin does see features.
	admin.Call("GET", "/api/admin/users?q="+other.User.Email, nil, 200, &list)
	if len(list.Users) != 1 || list.Users[0].Manager == nil || !slices.Equal(list.Users[0].Manager.Features, []auth.Feature{auth.FeatAudit}) {
		t.Fatalf("admin view = %+v", list.Users)
	}
}

func TestManagerOnlyAccount(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	var u auth.User
	admin.Call("POST", "/api/admin/managers", map[string]any{"email": " Helper@Example.com ", "name": "Helper", "features": []string{"usage"}}, 201, &u)
	if u.Role != auth.RoleManager || u.Email != "helper@example.com" || u.Manager == nil {
		t.Fatalf("created %+v", u)
	}
	login := map[string]string{"email": u.Email, "password": "a-new-password"}
	// No password until they set one from the email.
	e.Client().Call("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": "!"}, 401, nil)
	token := e.LastEmailToken(u.Email, "Your manager account")
	e.Client().Call("POST", "/api/auth/password-reset/confirm", map[string]string{"token": token, "password": login["password"]}, 204, nil)
	c := e.Client()
	c.Call("POST", "/api/auth/login", login, 200, &c.User)
	if c.User.Manager == nil || !slices.Equal(c.User.Manager.Features, []auth.Feature{auth.FeatUsage}) {
		t.Fatalf("login user = %+v", c.User)
	}
	c.Call("GET", "/api/admin/usage", nil, 200, nil)
	// No teaching pages (PL-FR-11), and no deleting itself.
	c.Call("GET", "/api/teacher/classrooms", nil, 403, nil)
	c.Call("DELETE", "/api/auth/me", map[string]string{"password": login["password"]}, 400, nil)

	// Removing the role suspends a manager-only account and signs it out.
	admin.Call("DELETE", "/api/admin/managers/"+u.ID, nil, 204, nil)
	c.Call("GET", "/api/auth/me", nil, 401, nil)
	_, body := e.Client().Do("POST", "/api/auth/login", login)
	if !strings.Contains(string(body), "account_suspended") {
		t.Fatalf("login after removal: %s", body)
	}
}

func TestMakeManagerValidation(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	teacher := e.NewUser(auth.RoleTeacher)
	student := e.NewUser(auth.RoleStudent)
	other := e.NewUser(auth.RoleAdmin)

	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": teacher.User.ID, "features": []string{"backdoor"}}, 422, nil)
	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": student.User.ID, "features": []string{}}, 400, nil)
	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": other.User.ID, "features": []string{}}, 400, nil)
	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": "not-a-uuid", "features": []string{}}, 404, nil)
	admin.Call("POST", "/api/admin/managers", map[string]any{"email": teacher.User.Email, "name": "Dup", "features": []string{}}, 422, nil)
	admin.Call("POST", "/api/admin/managers", map[string]any{"email": "bad", "name": "", "features": []string{}}, 422, nil)

	var u auth.User
	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": teacher.User.ID, "features": []string{"audit", "manage_users", "audit"}}, 201, &u)
	if !slices.Equal(u.Manager.Features, []auth.Feature{auth.FeatViewUsers, auth.FeatManageUsers, auth.FeatAudit}) {
		t.Fatalf("features = %v", u.Manager.Features)
	}
	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": teacher.User.ID, "features": []string{}}, 409, nil)
	// Registration can't make a manager.
	e.Client().Call("POST", "/api/auth/register", map[string]string{"email": "m@example.com", "name": "M", "password": "password123", "role": "manager"}, 422, nil)
}

func TestManagersPageAndAuditByManager(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	var first *apptest.Client
	for i := 0; i < 7; i++ {
		c := e.NewUser(auth.RoleTeacher)
		makeManager(admin, c, auth.FeatManageUsers)
		if first == nil {
			first = c
		}
	}
	rows := admin.CheckPages("/api/admin/managers", "managers", "id", 7, 3)
	if rows[0]["granted_by"] != "Admin" || rows[0]["manager"] == nil || rows[0]["granted_at"] == nil {
		t.Fatalf("row = %v", rows[0])
	}
	admin.Call("GET", "/api/admin/managers?q="+first.User.Email, nil, 200, nil)
	p := admin.Page("/api/admin/managers?q="+first.User.Email, "managers", 1, 25)
	if p.Total != 1 {
		t.Fatalf("search total = %d", p.Total)
	}
	// The manager acts; the admin finds it under that manager, marked "manager" (PL-FR-15).
	student := e.NewUser(auth.RoleStudent)
	first.Call("POST", "/api/admin/users/"+student.User.ID+"/status", map[string]string{"status": "suspended"}, 204, nil)
	var a struct {
		Events []struct {
			Action    string
			ActorRole string `json:"actor_role"`
		}
	}
	admin.Call("GET", "/api/admin/audit?actor="+first.User.ID, nil, 200, &a)
	if len(a.Events) != 1 || a.Events[0].ActorRole != "manager" {
		t.Fatalf("manager's events = %+v", a.Events)
	}
	admin.Call("GET", "/api/admin/audit?actor_role=manager", nil, 200, &a)
	if len(a.Events) != 1 {
		t.Fatalf("manager events = %+v", a.Events)
	}
	admin.Call("GET", "/api/admin/audit?actor=nope", nil, 404, nil)
	p = admin.Page("/api/admin/managers?sort=active&dir=desc", "managers", 1, 1)
	if p.Rows[0]["id"] != first.User.ID || p.Rows[0]["last_active_at"] == nil {
		t.Fatalf("most recently active = %v", p.Rows[0])
	}
}

func TestThereIsAlwaysAnAdmin(t *testing.T) {
	e := apptest.New(t)
	a := e.NewUser(auth.RoleAdmin)
	b := e.NewUser(auth.RoleAdmin)
	ctx := t.Context()
	// Only reachable through the service: over HTTP an admin can't act on themself.
	a.Call("POST", "/api/admin/users/"+b.User.ID+"/status", map[string]string{"status": "suspended"}, 204, nil)
	err := e.App.Auth.SetStatus(ctx, b.User, a.User.ID, auth.StatusSuspended)
	if err == nil || !strings.Contains(err.Error(), "At least one admin is needed") {
		t.Fatalf("suspending the last admin: %v", err)
	}
	if err := e.App.Auth.DeleteUser(ctx, b.User, a.User.ID); err == nil {
		t.Fatal("deleting the last admin succeeded")
	}

	// Two admins suspending each other at once: one admin always remains.
	c := e.NewUser(auth.RoleAdmin)
	d := e.NewUser(auth.RoleAdmin)
	a.Call("DELETE", "/api/admin/users/"+b.User.ID, nil, 204, nil)
	a.Call("POST", "/api/admin/users/"+c.User.ID+"/status", map[string]string{"status": "suspended"}, 204, nil)
	var wg sync.WaitGroup
	for _, p := range [][2]*apptest.Client{{a, d}, {d, a}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p[0].Do("POST", "/api/admin/users/"+p[1].User.ID+"/status", map[string]string{"status": "suspended"})
		}()
	}
	wg.Wait()
	var active int
	if err := e.Pool.QueryRow(ctx, `SELECT count(*) FROM auth.users WHERE role='admin' AND status='active'`).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active admins = %d, %v", active, err)
	}
}

// Managers, teacher-managers included, get admin session limits (PL-NFR-08).
func TestManagerSessionLimits(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	idle := e.NewUser(auth.RoleTeacher)
	old := e.NewUser(auth.RoleTeacher)
	plain := e.NewUser(auth.RoleTeacher)
	ctx := t.Context()
	for _, q := range []struct{ sql, id string }{
		{`UPDATE auth.sessions SET last_seen_at = now() - interval '31 minutes' WHERE user_id=$1`, idle.User.ID},
		{`UPDATE auth.sessions SET created_at = now() - interval '13 hours', last_seen_at = now() WHERE user_id=$1`, old.User.ID},
		{`UPDATE auth.sessions SET last_seen_at = now() - interval '31 minutes' WHERE user_id=$1`, plain.User.ID},
	} {
		if _, err := e.Pool.Exec(ctx, q.sql, q.id); err != nil {
			t.Fatal(err)
		}
	}
	makeManager(admin, idle) // also drops their cached sessions
	makeManager(admin, old)
	idle.Call("GET", "/api/auth/me", nil, 401, nil)
	old.Call("GET", "/api/auth/me", nil, 401, nil)
	// A plain teacher keeps the 24-hour idle limit.
	plain.Call("GET", "/api/auth/me", nil, 200, nil)
}
