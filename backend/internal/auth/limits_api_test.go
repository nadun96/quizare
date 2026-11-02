package auth_test

import (
	"fmt"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
)

// D-126: the admin sets the per-network sign-in limit and the password
// checks at a time; they apply at once and are kept.
func TestSignInLimits(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	teacher := e.NewUser(auth.RoleTeacher)
	teacher.Call("GET", "/api/admin/sign-in-limits", nil, 403, nil)

	var l auth.SignInLimits
	admin.Call("GET", "/api/admin/sign-in-limits", nil, 200, &l)
	if l.NetworkBurst != 600 || l.NetworkPerSec != 20 || l.PasswordWorkers != nil || l.Workers != 2 || l.DefaultWorkers != 2 || l.CPUs < 1 {
		t.Fatalf("defaults = %+v", l)
	}
	admin.Call("PUT", "/api/admin/sign-in-limits", map[string]any{"network_burst": 10, "network_per_sec": 500, "password_workers": 9}, 422, nil)
	teacher.Call("PUT", "/api/admin/sign-in-limits", map[string]any{"network_burst": 100, "network_per_sec": 5}, 403, nil)

	admin.Call("PUT", "/api/admin/sign-in-limits", map[string]any{"network_burst": 50, "network_per_sec": 1, "password_workers": 3}, 200, &l)
	if l.NetworkBurst != 50 || l.Workers != 3 || *l.PasswordWorkers != 3 {
		t.Fatalf("saved = %+v", l)
	}
	// From one address, the 51st sign-in in a burst is held back.
	c := e.Client()
	limited := 0
	for i := 0; i < 55; i++ {
		if code, _ := c.Do("POST", "/api/auth/login", map[string]string{"email": fmt.Sprintf("nobody%d@example.com", i), "password": "wrong password"}); code == 429 {
			limited++
		}
	}
	// It refills at one a second, so a slow machine holds back fewer; the old 600 would hold back none.
	if limited == 0 {
		t.Fatal("the new per-network limit isn't applied")
	}
	admin.Call("PUT", "/api/admin/sign-in-limits", map[string]any{"network_burst": 600, "network_per_sec": 20, "password_workers": nil}, 200, &l)
	if l.PasswordWorkers != nil || l.Workers != 2 {
		t.Fatalf("back to the server's own setting: %+v", l)
	}
	if err := e.App.Auth.LoadSignInLimits(t.Context()); err != nil {
		t.Fatal(err)
	}
}
