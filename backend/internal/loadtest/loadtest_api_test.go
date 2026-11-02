package loadtest_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/loadtest"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func newEnv(t *testing.T) *apptest.Env {
	e := apptest.New(t)
	e.App.LoadTest.SetTLSConfig(e.Server.Client().Transport.(*http.Transport).TLSClientConfig)
	return e
}

func count(t *testing.T, e *apptest.Env, q string) int {
	t.Helper()
	var n int
	if err := e.Pool.QueryRow(context.Background(), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func wait(t *testing.T, e *apptest.Env) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := e.App.LoadTest.Wait(ctx); err != nil {
		t.Fatal("the test didn't finish: ", err)
	}
}

// D-125: virtual students sign in, join, answer and submit in each step;
// the report has their timings; afterwards nothing of theirs is left.
func TestCapacityTestRunsAndCleansUp(t *testing.T) {
	e := newEnv(t)
	admin := e.NewUser(auth.RoleAdmin)
	before := count(t, e, `SELECT count(*) FROM auth.users`)

	var started struct{ ID string }
	admin.Call("POST", "/api/admin/loadtest", map[string]any{"steps": []int{3, 6}, "questions": 3, "think_seconds": 1, "ramp_seconds": 1}, 202, &started)
	admin.Call("POST", "/api/admin/loadtest", map[string]any{"steps": []int{3}, "questions": 3, "think_seconds": 1}, 409, nil) // one at a time
	var status struct {
		Running *loadtest.Progress `json:"running"`
	}
	admin.Call("GET", "/api/admin/loadtest", nil, 200, &status)
	if status.Running == nil || status.Running.RunID != started.ID {
		t.Fatalf("running = %+v", status.Running)
	}
	// While it runs, its accounts are in no list and can't be signed in to from outside.
	var users struct {
		Rows []auth.User `json:"rows"`
	}
	admin.Call("GET", "/api/admin/users?size=100", nil, 200, &users)
	for _, u := range users.Rows {
		if strings.HasSuffix(u.Email, "@loadtest.invalid") {
			t.Fatal("a virtual account is in the admin's list")
		}
	}
	wait(t, e)

	var run loadtest.Run
	admin.Call("GET", "/api/admin/loadtest/runs/"+started.ID, nil, 200, &run)
	if run.Report == nil || run.Report.Error != "" || len(run.Report.Steps) == 0 {
		t.Fatalf("report = %+v", run.Report)
	}
	for _, st := range run.Report.Steps {
		if st.Completed != st.Students || st.Joined != st.Students || st.Ops["save"].N != st.Students*3 || st.Ops["login"].N != st.Students || st.Drops != 0 {
			t.Fatalf("step %d: completed %d joined %d saves %d logins %d drops %d errors %v %v", st.Students, st.Completed, st.Joined, st.Ops["save"].N, st.Ops["login"].N, st.Drops, st.Errors, st.FirstError)
		}
		if len(st.Samples) == 0 || st.KBPerStudent <= 0 {
			t.Fatalf("step %d has no measurements: %+v", st.Students, st)
		}
	}
	if run.Status != "passed" && run.Status != "failed" {
		t.Fatalf("status = %s", run.Status)
	}
	if run.Status == "passed" && (run.Max != 6 || len(run.Report.Steps) != 2) {
		t.Fatalf("passed with max %d and %d steps", run.Max, len(run.Report.Steps))
	}

	if n := count(t, e, `SELECT count(*) FROM auth.users`); n != before {
		t.Fatalf("%d accounts after the test, %d before", n, before)
	}
	for _, q := range []string{
		`SELECT count(*) FROM auth.users WHERE synthetic`,
		`SELECT count(*) FROM content.classrooms WHERE name LIKE 'Capacity test%'`,
		`SELECT count(*) FROM live.sessions`,
		`SELECT count(*) FROM live.attempts`,
		`SELECT count(*) FROM quiz.quizzes WHERE title = 'Capacity test'`,
	} {
		if n := count(t, e, q); n != 0 {
			t.Fatalf("%s: %d left", q, n)
		}
	}

	var list struct{ Runs []loadtest.Run }
	admin.Call("GET", "/api/admin/loadtest", nil, 200, &list)
	if len(list.Runs) != 1 || list.Runs[0].Report != nil || list.Runs[0].AdminName != "Admin" {
		t.Fatalf("runs = %+v", list.Runs)
	}
	admin.Call("DELETE", "/api/admin/loadtest/runs/"+started.ID, nil, 204, nil)
	admin.Call("GET", "/api/admin/loadtest/runs/"+started.ID, nil, 404, nil)
}

// D-125: admins only; a sensible plan; never while students take a quiz.
func TestCapacityTestGuards(t *testing.T) {
	e := newEnv(t)
	admin := e.NewUser(auth.RoleAdmin)
	teacher := e.NewUser(auth.RoleTeacher)
	teacher.Call("GET", "/api/admin/loadtest", nil, 403, nil)
	plan := map[string]any{"steps": []int{3}, "questions": 3, "think_seconds": 1}
	teacher.Call("POST", "/api/admin/loadtest", plan, 403, nil)
	admin.Call("POST", "/api/admin/loadtest", map[string]any{"steps": []int{50, 20}, "questions": 3, "think_seconds": 1}, 422, nil)
	admin.Call("POST", "/api/admin/loadtest", map[string]any{"steps": []int{5000}, "questions": 3, "think_seconds": 1}, 422, nil)
	admin.Call("POST", "/api/admin/loadtest", map[string]any{"steps": []int{3}, "questions": 99, "think_seconds": 1}, 422, nil)

	// A real class waiting in a session.
	var c, m, tp, q, sess struct {
		ID       string `json:"id"`
		JoinCode string `json:"join_code"`
	}
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Real"}, 201, &c)
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &tp)
	teacher.Call("POST", "/api/teacher/topics/"+tp.ID+"/quizzes", map[string]any{"title": "Q"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,A?,x|y,x,1\n"))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)
	student := e.NewUser(auth.RoleStudent)
	student.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]any{}, 200, nil)
	code, body := admin.Do("POST", "/api/admin/loadtest", plan)
	if code != 409 || !strings.Contains(string(body), "server_in_use") {
		t.Fatalf("started during a live quiz: %d %s", code, body)
	}
}

// D-125: a virtual account signs in only with the running test's secret,
// and one left behind by a crash is removed when the server starts.
func TestVirtualAccountsStayInside(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	hash, err := e.App.Auth.HashPassword(ctx, "password123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pool.Exec(ctx, `INSERT INTO auth.users(email, name, role, status, password_hash, synthetic) VALUES ('lt-x-1@loadtest.invalid','V','student','active',$1,true)`, hash); err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	c.Call("POST", "/api/auth/login", map[string]string{"email": "lt-x-1@loadtest.invalid", "password": "password123"}, 401, nil)
	c.Headers = map[string]string{auth.LoadTestHeader: "guess"}
	c.Call("POST", "/api/auth/login", map[string]string{"email": "lt-x-1@loadtest.invalid", "password": "password123"}, 401, nil)

	if _, err := e.Pool.Exec(ctx, `INSERT INTO loadtest.runs(status, plan) VALUES ('running', '{}')`); err != nil {
		t.Fatal(err)
	}
	if err := e.App.LoadTest.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, e, `SELECT count(*) FROM auth.users WHERE synthetic`); n != 0 {
		t.Fatalf("%d virtual accounts left", n)
	}
	if n := count(t, e, `SELECT count(*) FROM loadtest.runs WHERE status = 'interrupted'`); n != 1 {
		t.Fatal("the cut-short test isn't marked interrupted")
	}
}
