package auth_test

import (
	"context"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
)

// The verification link can be sent again; only the newest works, and it
// can't be asked for endlessly (D-98).
func TestResendVerification(t *testing.T) {
	e := apptest.New(t)
	s := e.NewUser(auth.RoleStudent)
	first := e.LastEmailToken(s.User.Email, "Verify your email")
	var st auth.EmailStatus
	st = auth.EmailStatus{}
	s.Call("GET", "/api/auth/me/email", nil, 200, &st)
	if st.Verified || st.Email != s.User.Email || !st.CanChange || st.PendingEmail != "" {
		t.Fatalf("status = %+v", st)
	}
	s.Call("POST", "/api/auth/me/email/verify", nil, 204, nil)
	second := e.LastEmailToken(s.User.Email, "Verify your email")
	if second == first {
		t.Fatal("no new link")
	}
	s.Call("POST", "/api/auth/verify-email", map[string]string{"token": first}, 400, nil)
	s.Call("POST", "/api/auth/me/email/verify", nil, 204, nil)
	s.Call("POST", "/api/auth/me/email/verify", nil, 204, nil)
	s.Call("POST", "/api/auth/me/email/verify", nil, 429, nil) // three in ten minutes
	s.Call("POST", "/api/auth/verify-email", map[string]string{"token": e.LastEmailToken(s.User.Email, "Verify your email")}, 204, nil)
	st = auth.EmailStatus{}
	s.Call("GET", "/api/auth/me/email", nil, 200, &st)
	if !st.Verified {
		t.Fatal("not verified")
	}
	s.Call("POST", "/api/auth/me/email/verify", nil, 409, nil)
	e.Client().Call("POST", "/api/auth/me/email/verify", nil, 401, nil)
}

// Changing the email: the password is asked for, the new address confirms,
// the old one is told, and until then logging in uses the old one (D-98).
func TestChangeEmail(t *testing.T) {
	e := apptest.New(t)
	s := e.NewUser(auth.RoleTeacher)
	other := e.NewUser(auth.RoleStudent)
	old := s.User.Email

	s.Call("PUT", "/api/auth/me/email", map[string]string{"email": "new@example.com", "password": "wrong-password"}, 422, nil)
	s.Call("PUT", "/api/auth/me/email", map[string]string{"email": "not an email", "password": "password123"}, 422, nil)
	s.Call("PUT", "/api/auth/me/email", map[string]string{"email": other.User.Email, "password": "password123"}, 422, nil)
	s.Call("PUT", "/api/auth/me/email", map[string]string{"email": " New@Example.com ", "password": "password123"}, 204, nil)

	var st auth.EmailStatus
	st = auth.EmailStatus{}
	s.Call("GET", "/api/auth/me/email", nil, 200, &st)
	if st.Email != old || st.PendingEmail != "new@example.com" {
		t.Fatalf("pending = %+v", st)
	}
	var n int
	if err := e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM river_job WHERE kind='email' AND args->>'to'=$1 AND args->>'subject'='Your email address is being changed'`, old).Scan(&n); err != nil || n != 1 {
		t.Fatalf("old address told: %d %v", n, err)
	}
	// Until confirmed, the old address still logs in.
	e.Client().Call("POST", "/api/auth/login", map[string]string{"email": old, "password": "password123"}, 200, nil)

	token := e.LastEmailToken("new@example.com", "Confirm your new email address")
	e.Client().Call("POST", "/api/auth/verify-email", map[string]string{"token": token}, 204, nil)
	st = auth.EmailStatus{}
	s.Call("GET", "/api/auth/me/email", nil, 200, &st)
	if st.Email != "new@example.com" || !st.Verified || st.PendingEmail != "" {
		t.Fatalf("after confirming = %+v", st)
	}
	e.Client().Call("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "password123"}, 200, nil)
	e.Client().Call("POST", "/api/auth/login", map[string]string{"email": old, "password": "password123"}, 401, nil)

	// A change can be cancelled: its link then does nothing.
	s.Call("PUT", "/api/auth/me/email", map[string]string{"email": "third@example.com", "password": "password123"}, 204, nil)
	token = e.LastEmailToken("third@example.com", "Confirm your new email address")
	s.Call("DELETE", "/api/auth/me/email/pending", nil, 204, nil)
	e.Client().Call("POST", "/api/auth/verify-email", map[string]string{"token": token}, 400, nil)
	st = auth.EmailStatus{}
	s.Call("GET", "/api/auth/me/email", nil, 200, &st)
	if st.Email != "new@example.com" || st.PendingEmail != "" {
		t.Fatalf("after cancelling = %+v", st)
	}
}

func TestEditProfileName(t *testing.T) {
	e := apptest.New(t)
	s := e.NewUser(auth.RoleStudent)
	s.Call("PUT", "/api/auth/me/profile", map[string]string{"name": "  "}, 422, nil)
	var out struct{ Name string }
	s.Call("PUT", "/api/auth/me/profile", map[string]string{"name": "  Amaya   Silva "}, 200, &out)
	var me auth.User
	s.Call("GET", "/api/auth/me", nil, 200, &me)
	if out.Name != "Amaya Silva" || me.Name != "Amaya Silva" {
		t.Fatalf("name = %q / %q", out.Name, me.Name)
	}
}
