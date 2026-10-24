package oidc_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/oidc"
	"github.com/nadun96/quizplatform/internal/oidc/oidctest"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

type fixture struct {
	e     *apptest.Env
	idp   *oidctest.Provider
	admin *apptest.Client
}

func setup(t *testing.T) *fixture {
	idp := oidctest.New(t)
	e := apptest.New(t, apptest.WithHTTPClient(idp.Server.Client()))
	return &fixture{e: e, idp: idp, admin: e.NewUser(auth.RoleAdmin)}
}

// provider adds an enabled provider for the fake, with the given changes.
func (f *fixture) provider(t *testing.T, change map[string]any) oidc.Provider {
	t.Helper()
	body := map[string]any{"name": "Test IdP", "enabled": true, "issuer": f.idp.Issuer(), "client_id": f.idp.ClientID,
		"client_secret": f.idp.ClientSecret, "new_accounts": "student", "link_by_email": true}
	for k, v := range change {
		body[k] = v
	}
	var p oidc.Provider
	f.admin.Call("POST", "/api/admin/sso", body, 201, &p)
	return p
}

func person(sub, email string, more map[string]any) map[string]any {
	c := map[string]any{"sub": sub, "email": email, "email_verified": true, "name": "Ann Perera"}
	for k, v := range more {
		c[k] = v
	}
	return c
}

// signIn runs a whole sign-in in a fresh browser; it returns the browser and where it ended up.
func (f *fixture) signIn(p oidc.Provider, claims map[string]any, next string) (*apptest.Client, string) {
	f.idp.SignInAs(claims)
	c := f.e.Client()
	return c, c.Follow("/api/auth/oidc/" + p.ID + "/start?next=" + next)
}

func TestProviderSettings(t *testing.T) {
	f := setup(t)
	var opts struct {
		SSO []auth.SSOOption `json:"sso"`
	}
	f.e.Client().Call("GET", "/api/auth/options", nil, 200, &opts)
	if len(opts.SSO) != 0 {
		t.Fatalf("no providers yet: %+v", opts.SSO)
	}

	// Admins only: a teacher or a manager can't decide who signs in.
	f.e.NewUser(auth.RoleTeacher).Call("GET", "/api/admin/sso", nil, 403, nil)

	for field, body := range map[string]map[string]any{
		"issuer":         {"name": "X", "issuer": "http://idp.example.org", "client_id": "a"},
		"client_id":      {"name": "X", "issuer": "https://idp.example.org"},
		"name":           {"name": " ", "issuer": "https://idp.example.org", "client_id": "a"},
		"teacher_values": {"name": "X", "issuer": "https://idp.example.org", "client_id": "a", "teacher_claim": "groups"},
		"new_accounts":   {"name": "X", "issuer": "https://idp.example.org", "client_id": "a", "new_accounts": "admin"},
	} {
		code, raw := f.admin.Do("POST", "/api/admin/sso", body)
		if code != 422 || !strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatalf("%s: %d %s", field, code, raw)
		}
	}

	// A provider on this machine may use http (a development Keycloak).
	var local oidc.Provider
	f.admin.Call("POST", "/api/admin/sso", map[string]any{"name": "Dev Keycloak", "issuer": "http://localhost:8180/realms/school/", "client_id": "quiz",
		"scopes": []string{"email", "email"}, "allowed_domains": []string{" @School.EDU ", ""}}, 201, &local)
	if local.Enabled || local.Issuer != "http://localhost:8180/realms/school" || strings.Join(local.Scopes, " ") != "openid email" ||
		strings.Join(local.AllowedDomains, ",") != "school.edu" || local.NewAccounts != "none" || local.ClientSecretSet {
		t.Fatalf("normalised = %+v", local)
	}

	p := f.provider(t, nil)
	if !p.ClientSecretSet || p.ClientSecret != "" {
		t.Fatalf("the secret is write-only: %+v", p)
	}
	var list struct {
		Providers   []oidc.Provider `json:"providers"`
		RedirectURL string          `json:"redirect_url"`
	}
	f.admin.Call("GET", "/api/admin/sso", nil, 200, &list)
	if len(list.Providers) != 2 || !strings.HasSuffix(list.RedirectURL, "/api/auth/oidc/callback") {
		t.Fatalf("list = %+v", list)
	}
	// Only enabled providers get a button.
	f.e.Client().Call("GET", "/api/auth/options", nil, 200, &opts)
	if len(opts.SSO) != 1 || opts.SSO[0].Name != "Test IdP" {
		t.Fatalf("options = %+v", opts.SSO)
	}

	// Saving without a secret keeps it.
	f.admin.Call("PUT", "/api/admin/sso/"+p.ID, map[string]any{"name": "Renamed", "enabled": true, "issuer": f.idp.Issuer(), "client_id": f.idp.ClientID, "new_accounts": "student"}, 200, &p)
	if !p.ClientSecretSet || p.Name != "Renamed" {
		t.Fatalf("kept secret: %+v", p)
	}
	if _, at := f.signIn(p, person("s-1", "ann@school.edu", nil), "/"); at != "/" {
		t.Fatalf("sign-in with the kept secret ended at %s", at)
	}

	// Test reads the discovery document.
	var res oidc.TestResult
	f.admin.Call("POST", "/api/admin/sso/test", map[string]string{"issuer": f.idp.Issuer()}, 200, &res)
	if !res.OK || !res.PKCE || !strings.HasSuffix(res.TokenEndpoint, "/token") {
		t.Fatalf("test = %+v", res)
	}
	f.admin.Call("POST", "/api/admin/sso/test", map[string]string{"issuer": f.idp.Issuer() + "/nothing-here"}, 200, &res)
	if res.OK {
		t.Fatal("a wrong issuer must fail the test")
	}

	f.admin.Call("DELETE", "/api/admin/sso/"+local.ID, nil, 204, nil)
	f.admin.Call("DELETE", "/api/admin/sso/"+local.ID, nil, 404, nil)
	f.admin.Call("PUT", "/api/admin/sso/not-a-uuid", map[string]any{"name": "X", "issuer": "https://a.example", "client_id": "a"}, 404, nil)
}

func TestSignIn(t *testing.T) {
	f := setup(t)
	p := f.provider(t, nil)

	// First sign-in makes a student account and signs in, going where the person was headed.
	c, at := f.signIn(p, person("sub-ann", "Ann@School.edu", nil), "/my")
	if at != "/my" {
		t.Fatalf("ended at %s", at)
	}
	var me auth.User
	c.Call("GET", "/api/auth/me", nil, 200, &me)
	if me.Email != "ann@school.edu" || me.Role != auth.RoleStudent || me.Name != "Ann Perera" || !me.EmailVerified {
		t.Fatalf("me = %+v", me)
	}
	// The same person again, with a new email at the provider: the same account.
	c2, _ := f.signIn(p, person("sub-ann", "ann.perera@school.edu", nil), "/")
	var again auth.User
	c2.Call("GET", "/api/auth/me", nil, 200, &again)
	if again.ID != me.ID {
		t.Fatal("the subject decides the account, not the email")
	}
	// The new account has no password.
	f.e.Client().Call("POST", "/api/auth/login", map[string]string{"email": "ann@school.edu", "password": "!"}, 401, nil)

	// "next" stays on this site.
	for _, next := range []string{"//evil.example/x", "https://evil.example", "/\\evil.example"} {
		if _, at := f.signIn(p, person("sub-ann", "ann@school.edu", nil), next); at != "/" {
			t.Fatalf("next %q ended at %s", next, at)
		}
	}

	// The person says no at the provider.
	f.idp.Deny()
	if at := f.e.Client().Follow("/api/auth/oidc/" + p.ID + "/start"); at != "/login?sso_error=sso_cancelled" {
		t.Fatalf("cancelled: %s", at)
	}

	// A callback without this browser's state: forged, replayed or from another tab.
	if at := f.e.Client().Follow("/api/auth/oidc/callback?state=abc&code=xyz"); at != "/login?sso_error=sso_expired" {
		t.Fatalf("forged: %s", at)
	}
	// An attacker signs in at the provider as themselves, stops before the
	// callback and sends that link to someone else: it must not sign the
	// victim in as the attacker.
	f.idp.SignInAs(person("sub-attacker", "attacker@school.edu", nil))
	jar, _ := cookiejar.New(nil)
	attacker := f.idp.Server.Client()
	attacker.Jar = jar
	attacker.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if req.URL.Path == "/api/auth/oidc/callback" {
			return http.ErrUseLastResponse
		}
		return nil
	}
	resp, err := attacker.Get(f.e.Server.URL + "/api/auth/oidc/" + p.ID + "/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	link, _ := url.Parse(resp.Header.Get("Location"))
	if link.Path != "/api/auth/oidc/callback" || link.Query().Get("code") == "" {
		t.Fatalf("provider answer = %s", link)
	}
	victim := f.e.Client()
	if at := victim.Follow(link.RequestURI()); at != "/login?sso_error=sso_expired" {
		t.Fatalf("handed over: %s", at)
	}
	victim.Call("GET", "/api/auth/me", nil, 401, nil)

	// A disabled provider has no sign-in.
	f.admin.Call("PUT", "/api/admin/sso/"+p.ID, map[string]any{"name": p.Name, "enabled": false, "issuer": p.Issuer, "client_id": p.ClientID, "new_accounts": "student"}, 200, nil)
	if code, _ := f.e.Client().Do("GET", "/api/auth/oidc/"+p.ID+"/start", nil); code != 404 {
		t.Fatalf("disabled: %d", code)
	}
}

func TestWhoGetsIn(t *testing.T) {
	f := setup(t)

	// No new accounts: only people who already have one.
	none := f.provider(t, map[string]any{"name": "Existing only", "new_accounts": "none"})
	if _, at := f.signIn(none, person("x1", "new@school.edu", nil), "/"); at != "/login?sso_error=sso_no_account" {
		t.Fatalf("none: %s", at)
	}

	// An existing student signs in by email; the password still works.
	f.e.Client().Call("POST", "/api/auth/register", map[string]any{"email": "kasun@school.edu", "name": "Kasun", "password": "password123", "role": "student"}, 201, nil)
	c, at := f.signIn(none, person("x2", "kasun@school.edu", nil), "/")
	if at != "/" {
		t.Fatalf("linked by email: %s", at)
	}
	c.Call("GET", "/api/auth/me", nil, 200, nil)
	f.e.Client().Call("POST", "/api/auth/login", map[string]string{"email": "kasun@school.edu", "password": "password123"}, 200, nil)

	// An unconfirmed email links and creates nothing, unless the admin trusts the provider's emails.
	if _, at := f.signIn(none, person("x3", "kasun@school.edu", map[string]any{"email_verified": false}), "/"); at != "/login?sso_error=sso_email_unverified" {
		t.Fatalf("unverified: %s", at)
	}
	trusting := f.provider(t, map[string]any{"name": "Entra", "trust_email": true, "email_claim": "preferred_username", "new_accounts": "none"})
	if _, at := f.signIn(trusting, map[string]any{"sub": "e1", "preferred_username": "kasun@school.edu"}, "/"); at != "/" {
		t.Fatalf("trusted emails: %s", at)
	}

	// Admins are never joined by email.
	admin := f.admin.User
	if _, at := f.signIn(none, person("x4", admin.Email, nil), "/"); at != "/login?sso_error=sso_conflict" {
		t.Fatalf("admin by email: %s", at)
	}
	// Nor is anyone when the provider says not to.
	strict := f.provider(t, map[string]any{"name": "Strict", "link_by_email": false, "new_accounts": "none"})
	if _, at := f.signIn(strict, person("x5", "kasun@school.edu", nil), "/"); at != "/login?sso_error=sso_conflict" {
		t.Fatalf("no linking: %s", at)
	}

	// Only the school's domains.
	school := f.provider(t, map[string]any{"name": "Google", "allowed_domains": []string{"school.edu"}})
	if _, at := f.signIn(school, person("g1", "someone@gmail.com", nil), "/"); at != "/login?sso_error=sso_domain" {
		t.Fatalf("domain: %s", at)
	}
	if _, at := f.signIn(school, person("g2", "nimal@school.edu", nil), "/"); at != "/" {
		t.Fatalf("school domain: %s", at)
	}

	// Teachers by group claim; with approval on, they wait for an admin.
	staff := f.provider(t, map[string]any{"name": "Staff", "teacher_claim": "groups", "teacher_values": []string{"staff"}})
	c, _ = f.signIn(staff, person("t1", "ms.silva@school.edu", map[string]any{"groups": []string{"year9", "staff"}}), "/")
	var me auth.User
	c.Call("GET", "/api/auth/me", nil, 200, &me)
	if me.Role != auth.RoleTeacher {
		t.Fatalf("role = %s", me.Role)
	}
	f.admin.Call("PUT", "/api/admin/auth-policy", map[string]bool{"require_teacher_approval": true}, 200, nil)
	if _, at := f.signIn(staff, person("t2", "mr.de.silva@school.edu", map[string]any{"groups": "staff"}), "/"); at != "/login?sso_error=pending_approval" {
		t.Fatalf("approval: %s", at)
	}

	// Suspended accounts stay out.
	f.admin.Call("POST", "/api/admin/users/"+me.ID+"/status", map[string]string{"status": "suspended"}, 204, nil)
	if _, at := f.signIn(staff, person("t1", "ms.silva@school.edu", nil), "/"); at != "/login?sso_error=account_suspended" {
		t.Fatalf("suspended: %s", at)
	}

	// A wrong client secret fails without saying more than that.
	wrong := f.provider(t, map[string]any{"name": "Misconfigured", "client_secret": "wrong"})
	if _, at := f.signIn(wrong, person("w1", "w@school.edu", nil), "/"); at != "/login?sso_error=sso_failed" {
		t.Fatalf("wrong secret: %s", at)
	}
}

func TestLinkFromAccountPage(t *testing.T) {
	f := setup(t)
	p := f.provider(t, map[string]any{"new_accounts": "none"})
	teacher := f.e.NewUser(auth.RoleTeacher)

	// Signed out, there's nothing to link.
	if code, _ := f.e.Client().Do("GET", "/api/auth/oidc/"+p.ID+"/link", nil); code != 401 {
		t.Fatalf("signed out: %d", code)
	}
	// Linking works with any email, since the person proved both.
	f.idp.SignInAs(person("work-account", "different@elsewhere.org", nil))
	if at := teacher.Follow("/api/auth/oidc/" + p.ID + "/link"); at != "/account?sso=linked" {
		t.Fatalf("link: %s", at)
	}
	var ids struct {
		Identities []struct {
			ProviderID string `json:"provider_id"`
			Name       string `json:"name"`
			Email      string `json:"email"`
		} `json:"identities"`
		Available []auth.SSOOption `json:"available"`
	}
	teacher.Call("GET", "/api/auth/oidc/identities", nil, 200, &ids)
	if len(ids.Identities) != 1 || ids.Identities[0].Name != "Test IdP" || ids.Identities[0].Email != "different@elsewhere.org" || len(ids.Available) != 1 {
		t.Fatalf("identities = %+v", ids)
	}
	// Now that sign-in opens the teacher's account.
	c, _ := f.signIn(p, person("work-account", "different@elsewhere.org", nil), "/")
	var me auth.User
	c.Call("GET", "/api/auth/me", nil, 200, &me)
	if me.ID != teacher.User.ID {
		t.Fatal("linked sign-in opens the linked account")
	}
	// Someone else can't take it.
	other := f.e.NewUser(auth.RoleTeacher)
	if at := other.Follow("/api/auth/oidc/" + p.ID + "/link"); at != "/account?sso_error=sso_taken" {
		t.Fatalf("taken: %s", at)
	}
	// Unlinking: fine with a password to fall back on.
	teacher.Call("DELETE", "/api/auth/oidc/identities/"+p.ID, nil, 204, nil)
	teacher.Call("DELETE", "/api/auth/oidc/identities/"+p.ID, nil, 404, nil)

	// An account made by sign-in has no password, so its only sign-in stays.
	open := f.provider(t, map[string]any{"name": "Open"})
	c, _ = f.signIn(open, person("only-way", "only@school.edu", nil), "/")
	c.Call("DELETE", "/api/auth/oidc/identities/"+open.ID, nil, 400, nil)

	// Removing a provider removes its sign-ins.
	f.admin.Call("DELETE", "/api/admin/sso/"+open.ID, nil, 204, nil)
	c.Call("GET", "/api/auth/oidc/identities", nil, 200, &ids)
	if len(ids.Identities) != 0 {
		t.Fatalf("after removal: %+v", ids.Identities)
	}
}
