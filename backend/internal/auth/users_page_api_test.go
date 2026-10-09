package auth_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
)

// The admin's user list is paginated, searched and sorted on the server (PL-FR-01 to PL-FR-03).
func TestAdminUsersArePaginated(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	for i := range 12 {
		// Names out of order, so sorting has something to do.
		if _, err := e.Pool.Exec(context.Background(), `INSERT INTO auth.users (email, password_hash, name, role, status)
			VALUES ($1, 'x', $2, 'student', 'active')`, fmt.Sprintf("pager%02d@example.com", i), fmt.Sprintf("Pager %c", 'Z'-i)); err != nil {
			t.Fatal(err)
		}
	}
	all := admin.CheckPages("/api/admin/users?q=pager", "users", "id", 12, 5)
	if all[0]["email"] != "pager11@example.com" {
		t.Fatalf("newest first by default, got %v", all[0]["email"])
	}
	// Sorting applies across pages, not only within one.
	byName := admin.CheckPages("/api/admin/users?q=pager&sort=name", "users", "id", 12, 5)
	if byName[0]["name"] != "Pager O" || byName[11]["name"] != "Pager Z" {
		t.Fatalf("by name: first %v, last %v", byName[0]["name"], byName[11]["name"])
	}
	if p := admin.Page("/api/admin/users?q=pager&sort=name&dir=desc", "users", 1, 5); p.Rows[0]["name"] != "Pager Z" {
		t.Fatalf("by name, descending: %v", p.Rows[0]["name"])
	}
	// Filters and search combine with the count; the search's wildcards are literal.
	if p := admin.Page("/api/admin/users?q=pager&role=teacher", "users", 1, 25); p.Total != 0 {
		t.Fatalf("role filter: %d", p.Total)
	}
	if p := admin.Page("/api/admin/users?q=pager_", "users", 1, 25); p.Total != 0 {
		t.Fatalf("an underscore is not a wildcard: %d", p.Total)
	}
}
