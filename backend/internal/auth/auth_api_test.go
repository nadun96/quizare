package auth_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
)

func TestStudentRegisterIsLoggedInAndGetsVerificationEmail(t *testing.T) {
	e := apptest.New(t)
	s := e.NewUser(auth.RoleStudent)
	var me auth.User
	s.Call("GET", "/api/auth/me", nil, 200, &me)
	if me.Role != auth.RoleStudent || me.EmailVerified {
		t.Fatalf("me = %+v", me)
	}
	token := e.LastEmailToken(me.Email, "Verify your email")
	s.Call("POST", "/api/auth/verify-email", map[string]string{"token": token}, 204, nil)
	s.Call("GET", "/api/auth/me", nil, 200, &me)
	if !me.EmailVerified {
		t.Fatal("email not verified after following link")
	}
	// Tokens are single-use.
	s.Call("POST", "/api/auth/verify-email", map[string]string{"token": token}, 400, nil)
}

func TestRegisterValidation(t *testing.T) {
	e := apptest.New(t)
	c := e.Client()
	status, body := c.Do("POST", "/api/auth/register", map[string]string{"email": "nope", "name": "", "password": "short", "role": "admin"})
	if status != 422 {
		t.Fatalf("status %d", status)
	}
	for _, f := range []string{"email", "name", "password", "role"} {
		if !strings.Contains(string(body), `"`+f+`"`) {
			t.Errorf("missing field error for %s: %s", f, body)
		}
	}
	s := e.NewUser(auth.RoleStudent)
	status, _ = c.Do("POST", "/api/auth/register", map[string]string{"email": strings.ToUpper(s.User.Email), "name": "X", "password": "password123", "role": "student"})
	if status != 422 {
		t.Fatalf("duplicate email (case-insensitive) status = %d", status)
	}
}

func TestLoginLogout(t *testing.T) {
	e := apptest.New(t)
	s := e.NewUser(auth.RoleStudent)
	c := e.Client()
	c.Call("POST", "/api/auth/login", map[string]string{"email": s.User.Email, "password": "wrong-password"}, 401, nil)
	c.Call("POST", "/api/auth/login", map[string]string{"email": "nobody@example.com", "password": "password123"}, 401, nil)
	c.Call("POST", "/api/auth/login", map[string]string{"email": s.User.Email, "password": "password123"}, 200, nil)
	c.Call("GET", "/api/auth/me", nil, 200, nil)
	c.Call("POST", "/api/auth/logout", nil, 204, nil)
	c.Call("GET", "/api/auth/me", nil, 401, nil)
}

func TestSessionCookieAttributes(t *testing.T) {
	e := apptest.New(t)
	s := e.NewUser(auth.RoleStudent)
	req, _ := http.NewRequest("POST", e.Server.URL+"/api/auth/login",
		strings.NewReader(`{"email":"`+s.User.Email+`","password":"password123"}`))
	req.Header.Set("Origin", e.Server.URL)
	req.Header.Set("X-Requested-With", "fetch")
	resp, err := e.Server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sc := resp.Header.Get("Set-Cookie")
	for _, want := range []string{"__Host-sid=", "Path=/", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(sc, want) {
			t.Errorf("Set-Cookie %q missing %q", sc, want)
		}
	}
	if strings.Contains(sc, "Domain=") {
		t.Error("__Host- cookies must not set Domain")
	}
}

func TestCSRFProtection(t *testing.T) {
	e := apptest.New(t)
	body := `{"email":"a@example.com","password":"password123"}`
	cases := map[string]map[string]string{
		"no custom header": {"Origin": e.Server.URL},
		"foreign origin":   {"Origin": "https://evil.example", "X-Requested-With": "fetch"},
	}
	for name, headers := range cases {
		req, _ := http.NewRequest("POST", e.Server.URL+"/api/auth/login", strings.NewReader(body))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := e.Server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 403 {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
}

func TestPasswordReset(t *testing.T) {
	e := apptest.New(t)
	s := e.NewUser(auth.RoleStudent)
	anon := e.Client()
	// Unknown emails get the same answer (no account enumeration).
	anon.Call("POST", "/api/auth/password-reset/request", map[string]string{"email": "ghost@example.com"}, 202, nil)
	anon.Call("POST", "/api/auth/password-reset/request", map[string]string{"email": s.User.Email}, 202, nil)
	token := e.LastEmailToken(s.User.Email, "Reset your password")
	anon.Call("POST", "/api/auth/password-reset/confirm", map[string]string{"token": token, "password": "x"}, 422, nil)
	anon.Call("POST", "/api/auth/password-reset/confirm", map[string]string{"token": token, "password": "new-password-1"}, 204, nil)
	// All existing sessions are revoked.
	s.Call("GET", "/api/auth/me", nil, 401, nil)
	anon.Call("POST", "/api/auth/login", map[string]string{"email": s.User.Email, "password": "password123"}, 401, nil)
	anon.Call("POST", "/api/auth/login", map[string]string{"email": s.User.Email, "password": "new-password-1"}, 200, nil)
	anon.Call("POST", "/api/auth/password-reset/confirm", map[string]string{"token": token, "password": "another-pass"}, 400, nil)
}

func TestTeacherApprovalPolicy(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	admin.Call("PUT", "/api/admin/auth-policy", map[string]bool{"require_teacher_approval": true}, 200, nil)

	c := e.Client()
	var u auth.User
	c.Call("POST", "/api/auth/register", map[string]string{"email": "t@example.com", "name": "T", "password": "password123", "role": "teacher"}, 201, &u)
	if u.Status != auth.StatusPendingApproval {
		t.Fatalf("status = %s", u.Status)
	}
	c.Call("GET", "/api/auth/me", nil, 401, nil) // no session for pending accounts
	_, body := c.Do("POST", "/api/auth/login", map[string]string{"email": "t@example.com", "password": "password123"})
	if !strings.Contains(string(body), "pending_approval") {
		t.Fatalf("login body = %s", body)
	}
	admin.Call("POST", "/api/admin/users/"+u.ID+"/status", map[string]string{"status": "active"}, 204, nil)
	c.Call("POST", "/api/auth/login", map[string]string{"email": "t@example.com", "password": "password123"}, 200, nil)
}

func TestAdminSuspendAndDelete(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	s := e.NewUser(auth.RoleStudent)

	admin.Call("POST", "/api/admin/users/"+s.User.ID+"/status", map[string]string{"status": "suspended"}, 204, nil)
	s.Call("GET", "/api/auth/me", nil, 401, nil) // revoked immediately, cache included
	_, body := e.Client().Do("POST", "/api/auth/login", map[string]string{"email": s.User.Email, "password": "password123"})
	if !strings.Contains(string(body), "account_suspended") {
		t.Fatalf("login body = %s", body)
	}

	admin.Call("DELETE", "/api/admin/users/"+s.User.ID, nil, 204, nil)
	var list struct{ Users []auth.User }
	admin.Call("GET", "/api/admin/users?q="+s.User.Email, nil, 200, &list)
	if len(list.Users) != 0 {
		t.Fatalf("deleted user still searchable by email: %+v", list.Users)
	}
	// The email address can be reused after deletion.
	e.Client().Call("POST", "/api/auth/register", map[string]string{"email": s.User.Email, "name": "New", "password": "password123", "role": "student"}, 201, nil)

	var n int
	if err := e.Pool.QueryRow(t.Context(), `SELECT count(*) FROM audit.events WHERE target_id=$1`, s.User.ID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit events = %d, %v", n, err)
	}
	admin.Call("DELETE", "/api/admin/users/not-a-uuid", nil, 404, nil)
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	e := apptest.New(t)
	e.Client().Call("GET", "/api/admin/users", nil, 401, nil)
	e.NewUser(auth.RoleTeacher).Call("GET", "/api/admin/users", nil, 403, nil)
	e.NewUser(auth.RoleStudent).Call("GET", "/api/admin/users", nil, 403, nil)
	e.NewUser(auth.RoleAdmin).Call("GET", "/api/admin/users", nil, 200, nil)
}
