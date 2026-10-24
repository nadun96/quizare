package directory_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/directory/directorytest"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

const (
	people   = "OU=People,DC=school,DC=test"
	groups   = "OU=Groups,DC=school,DC=test"
	svcDN    = "CN=svc-quiz,OU=Service,DC=school,DC=test"
	teachers = "CN=Teachers," + groups
	students = "CN=Students," + groups
	year10   = "CN=Year 10," + groups
)

func person(cn, sam, mail, password string, guid byte, extra map[string][]string) *directorytest.Entry {
	a := map[string][]string{
		"objectClass": {"top", "person", "organizationalPerson", "user"}, "objectCategory": {"person"},
		"sAMAccountName": {sam}, "userPrincipalName": {sam + "@school.test"}, "displayName": {cn},
		"objectGUID":         {string([]byte{guid, 0x00, 0xff, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xa0, 0xb0, 0xc0, 0xd0})},
		"userAccountControl": {"512"},
	}
	if mail != "" {
		a["mail"] = []string{mail}
	}
	for k, v := range extra {
		a[k] = v
	}
	return &directorytest.Entry{DN: "CN=" + cn + "," + people, Password: password, Attrs: a}
}

// school: alice teaches; bob is a student through a nested group (Year 10
// inside Students); carol is in no group; dave is disabled; erin has no email.
func school(t *testing.T, startTLS bool) *directorytest.Server {
	return directorytest.Start(t, startTLS,
		&directorytest.Entry{DN: svcDN, Password: "svc-secret", Attrs: map[string][]string{"objectClass": {"user"}}},
		person("Alice Teacher", "alice", "alice@school.test", "alice-pw", 1, nil),
		person("Bob Student", "bob", "bob@school.test", "bob-pw", 2, nil),
		person("Carol Visitor", "carol", "carol@school.test", "carol-pw", 3, nil),
		person("Dave Left", "dave", "dave@school.test", "dave-pw", 4, map[string][]string{"userAccountControl": {"514"}}),
		person("Erin NoMail", "erin", "", "erin-pw", 5, map[string][]string{"userPrincipalName": {"not an address"}}),
		&directorytest.Entry{DN: teachers, Attrs: map[string][]string{"objectClass": {"group"}, "member": {"CN=Alice Teacher," + people, "CN=Erin NoMail," + people}}},
		&directorytest.Entry{DN: students, Attrs: map[string][]string{"objectClass": {"group"}, "member": {year10}}},
		&directorytest.Entry{DN: year10, Attrs: map[string][]string{"objectClass": {"group"}, "member": {"CN=Bob Student," + people}}},
	)
}

func settings(d *directorytest.Server, startTLS bool) map[string]any {
	return map[string]any{
		"enabled": true, "kind": "ad", "url": d.URL, "start_tls": startTLS, "ca_pem": d.CAPEM,
		"bind_dn": svcDN, "bind_password": "svc-secret", "base_dn": people,
		"teacher_group": teachers, "student_group": students, "nested_groups": true, "link_by_email": true,
	}
}

func login(e *apptest.Env, name, password string) (*apptest.Client, int, auth.User, string) {
	c := e.Client()
	code, body := c.Do("POST", "/api/auth/login", map[string]string{"email": name, "password": password})
	var u auth.User
	_ = json.Unmarshal(body, &u)
	var er struct{ Code string }
	_ = json.Unmarshal(body, &er)
	return c, code, u, er.Code
}

func TestDirectorySignIn(t *testing.T) {
	for _, startTLS := range []bool{false, true} {
		name := "ldaps"
		if startTLS {
			name = "starttls"
		}
		t.Run(name, func(t *testing.T) { signIn(t, startTLS) })
	}
}

func signIn(t *testing.T, startTLS bool) {
	e := apptest.New(t)
	d := school(t, startTLS)
	admin := e.NewUser(auth.RoleAdmin)

	var opts struct {
		Directory bool `json:"directory"`
	}
	e.Client().Call("GET", "/api/auth/options", nil, 200, &opts)
	if opts.Directory {
		t.Fatal("directory sign-in is on before it's set up")
	}
	// A student registered before the school switched over, with the same email as bob.
	early := e.Client()
	early.Call("POST", "/api/auth/register", map[string]any{"email": "bob@school.test", "name": "Bob", "password": "local-password-1", "role": "student"}, 201, &early.User)

	var saved map[string]any
	admin.Call("PUT", "/api/admin/directory", settings(d, startTLS), 200, &saved)
	if saved["bind_password_set"] != true || saved["bind_password"] != nil {
		t.Fatalf("saved = %v (the password must never come back)", saved)
	}
	e.Client().Call("GET", "/api/auth/options", nil, 200, &opts)
	if !opts.Directory {
		t.Fatal("options don't say directory sign-in is on")
	}

	// The Windows name, the user@domain name and the email all work, for one account.
	_, code, alice, _ := login(e, "alice", "alice-pw")
	if code != 200 || alice.Role != auth.RoleTeacher || !alice.Directory || alice.Email != "alice@school.test" || alice.Name != "Alice Teacher" {
		t.Fatalf("alice: %d %+v", code, alice)
	}
	for _, n := range []string{"alice@school.test", "ALICE"} {
		if _, code, again, _ := login(e, n, "alice-pw"); code != 200 || again.ID != alice.ID {
			t.Fatalf("%s: %d %s, want %s", n, code, again.ID, alice.ID)
		}
	}
	// Nested groups make bob a student, and his existing account moves over (same id).
	bobC, code, bob, _ := login(e, "bob", "bob-pw")
	if code != 200 || bob.Role != auth.RoleStudent || bob.ID != early.User.ID || !bob.Directory {
		t.Fatalf("bob: %d %+v (early account %s)", code, bob, early.User.ID)
	}
	if _, code, _, _ := login(e, "bob@school.test", "local-password-1"); code != 401 {
		t.Fatalf("the old local password still works: %d", code)
	}
	if code, body := bobC.Do("POST", "/api/auth/me/password", map[string]string{"current_password": "bob-pw", "new_password": "something-new-1"}); code != 400 {
		t.Fatalf("a school account changed its password here: %d %s", code, body)
	}

	for _, c := range []struct {
		login, password string
		code            int
		err             string
	}{
		{"carol", "carol-pw", 403, "directory_not_allowed"}, // in no group
		{"dave", "dave-pw", 401, "invalid_credentials"},     // disabled in AD
		{"erin", "erin-pw", 403, "directory_no_email"},
		{"alice", "wrong", 401, "invalid_credentials"},
		{"alice", "", 401, "invalid_credentials"}, // an empty password is an anonymous bind; never accepted
		{"*", "alice-pw", 401, "invalid_credentials"},
		{"alice)(|(sAMAccountName=*", "alice-pw", 401, "invalid_credentials"}, // filter injection
		{"nobody", "x", 401, "invalid_credentials"},
	} {
		if _, code, _, er := login(e, c.login, c.password); code != c.code || er != c.err {
			t.Errorf("%s: %d %s, want %d %s", c.login, code, er, c.code, c.err)
		}
	}
	for _, f := range d.Searches() {
		if strings.Contains(f, "sAMAccountName=*") {
			t.Fatalf("an unescaped name reached the directory: %s", f)
		}
	}

	// The admin's own password still works, and the directory never stands in for it.
	if _, code, _, _ := login(e, admin.User.Email, "password123"); code != 200 {
		t.Fatalf("admin sign-in: %d", code)
	}

	// The admin's test reports each step and what the account would get.
	var res struct {
		OK    bool
		Steps []struct {
			Name   string
			OK     bool
			Detail string
		}
		Account struct {
			Email string
			Role  string
		}
	}
	admin.Call("POST", "/api/admin/directory/test", map[string]any{"config": settings(d, startTLS), "login": "bob", "password": "bob-pw"}, 200, &res)
	if !res.OK || res.Account.Role != "student" || res.Account.Email != "bob@school.test" || len(res.Steps) < 4 {
		t.Fatalf("test: %+v", res)
	}
	admin.Call("POST", "/api/admin/directory/test", map[string]any{"config": settings(d, startTLS), "login": "carol", "password": "carol-pw"}, 200, &res)
	if res.OK || res.Steps[len(res.Steps)-1].OK {
		t.Fatalf("carol passed the test: %+v", res)
	}

	// The hourly check signs out an account disabled in the directory, and no one else.
	aliceC, _, _, _ := login(e, "alice", "alice-pw")
	d.Set("CN=Alice Teacher,"+people, "userAccountControl", "514")
	if err := e.App.Directory.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code, _ := aliceC.Do("GET", "/api/auth/me", nil); code != 401 {
		t.Fatalf("disabled alice still signed in: %d", code)
	}
	if code, _ := bobC.Do("GET", "/api/auth/me", nil); code != 200 {
		t.Fatalf("bob was signed out too: %d", code)
	}
	// Removed from the directory: signed out at the next check.
	d.Remove("CN=Bob Student," + people)
	_ = e.App.Directory.Check(context.Background())
	if code, _ := bobC.Do("GET", "/api/auth/me", nil); code != 401 {
		t.Fatalf("removed bob still signed in: %d", code)
	}
}

func TestDirectorySettingsAndOutage(t *testing.T) {
	e := apptest.New(t)
	d := school(t, false)
	admin := e.NewUser(auth.RoleAdmin)
	student := e.NewUser(auth.RoleStudent)

	// Unencrypted LDAP is refused; so is a filter without {login}.
	bad := settings(d, false)
	bad["url"] = "ldap://dc1.school.test"
	bad["user_filter"] = "(sAMAccountName=alice)"
	code, body := admin.Do("PUT", "/api/admin/directory", bad)
	if code != 422 || !strings.Contains(string(body), "unencrypted") || !strings.Contains(string(body), "{login}") {
		t.Fatalf("bad settings: %d %s", code, body)
	}
	// Only admins: a manager or a teacher can't see or change it.
	if code, _ := student.Do("GET", "/api/admin/directory", nil); code != 404 && code != 403 {
		t.Fatalf("student read the directory settings: %d", code)
	}
	admin.Call("PUT", "/api/admin/directory", settings(d, false), 200, nil)
	// Saving again without the password keeps the stored one.
	again := settings(d, false)
	delete(again, "bind_password")
	admin.Call("PUT", "/api/admin/directory", again, 200, nil)
	e.App.Directory.Check(context.Background()) //nolint:errcheck
	if _, code, _, _ := login(e, "alice", "alice-pw"); code != 200 {
		t.Fatalf("after re-saving: %d", code)
	}

	// The directory goes away: school accounts get "try again shortly", local ones still sign in.
	down := settings(d, false)
	down["url"] = "ldaps://127.0.0.1:1"
	admin.Call("PUT", "/api/admin/directory", down, 200, nil)
	if _, code, _, er := login(e, "alice", "alice-pw"); code != 503 || er != "directory_unavailable" {
		t.Fatalf("directory down: %d %s", code, er)
	}
	if _, code, _, _ := login(e, student.User.Email, "password123"); code != 200 {
		t.Fatalf("a local account couldn't sign in during the outage: %d", code)
	}
	// And the hourly check signs nobody out when it can't reach the directory.
	if err := e.App.Directory.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}
