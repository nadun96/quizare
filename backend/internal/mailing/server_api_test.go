package mailing_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/mail/mailtest"
	"github.com/nadun96/quizplatform/internal/mailing"
)

// The admin sets the mail server in the console, tests it, and it takes
// over from the config file; the password is never shown (D-104).
func TestMailServer(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	srv := mailtest.Start(t, "app", "secret")
	host, port := mail.SplitAddr(srv.Addr)

	var st mailing.ServerState
	admin.Call("GET", "/api/admin/email/server", nil, 200, &st)
	if st.Source != "log" || st.Enabled {
		t.Fatalf("at first = %+v", st)
	}

	// A test with settings not saved yet: a wrong password says where it failed.
	settings := map[string]any{"enabled": true, "host": host, "port": port, "security": "none", "username": "app", "password": "wrong", "from": "no-reply@school.lk", "from_name": "School"}
	var res mailing.TestResult
	admin.Call("POST", "/api/admin/email/server/test", map[string]any{"to": "me@example.com", "server": settings}, 200, &res)
	if res.OK || res.Step != "login" || !strings.Contains(res.Error, "535") {
		t.Fatalf("wrong password = %+v", res)
	}
	settings["password"] = "secret"
	admin.Call("POST", "/api/admin/email/server/test", map[string]any{"to": "me@example.com", "server": settings}, 200, &res)
	if !res.OK || res.Via != srv.Addr || len(srv.Got()) != 1 {
		t.Fatalf("right password = %+v", res)
	}

	// Remote servers only get a password encrypted; a username needs one.
	admin.Call("PUT", "/api/admin/email/server", map[string]any{"enabled": true, "host": "smtp.example.com", "port": 25, "security": "none", "username": "app", "password": "x", "from": "a@b.lk"}, 422, nil)
	admin.Call("PUT", "/api/admin/email/server", map[string]any{"enabled": true, "host": host, "port": port, "security": "none", "username": "app", "from": "a@b.lk"}, 422, nil)

	// Saved: the password is sealed and never comes back.
	admin.Call("PUT", "/api/admin/email/server", settings, 200, &st)
	if st.Source != "console" || !st.PasswordSet || st.Password != "" || st.From != "no-reply@school.lk" {
		t.Fatalf("saved = %+v", st)
	}
	var raw []byte
	if err := e.Pool.QueryRow(context.Background(), `SELECT pw_ciphertext FROM mailing.server`).Scan(&raw); err != nil || strings.Contains(string(raw), "secret") {
		t.Fatalf("stored in the clear: %v", err)
	}
	// Saving again without the password keeps it; the worker's sender uses it.
	settings["password"] = ""
	settings["port"] = strconv.Itoa(port) // a string is refused
	admin.Call("PUT", "/api/admin/email/server", settings, 400, nil)
	settings["port"] = port
	admin.Call("PUT", "/api/admin/email/server", settings, 200, &st)
	if err := e.App.Mailing.Sender().Send(context.Background(), mail.Message{To: "ann@example.com", Subject: "Hello", Body: "Hi"}); err != nil {
		t.Fatal(err)
	}
	if got := srv.Got(); len(got) != 2 || !strings.Contains(got[1], `From: "School" <no-reply@school.lk>`) {
		t.Fatalf("worker's email = %v", got)
	}
	// A test with the saved settings.
	admin.Call("POST", "/api/admin/email/server/test", map[string]any{"to": "me@example.com"}, 200, &res)
	if !res.OK {
		t.Fatalf("saved test = %+v", res)
	}

	// Off again: back to the config file (here, the log).
	settings["enabled"] = false
	admin.Call("PUT", "/api/admin/email/server", settings, 200, &st)
	if st.Source != "log" {
		t.Fatalf("off = %+v", st)
	}

	// Managers with Email see the queue, never the server.
	mgr := e.NewUser(auth.RoleTeacher)
	admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": mgr.User.ID, "features": []string{"send_email"}}, 201, nil)
	mgr.Call("GET", "/api/admin/email/queue", nil, 200, nil)
	mgr.Call("GET", "/api/admin/email/server", nil, 404, nil)
}

// The queue shows every email with its status, and a send's progress.
func TestEmailQueue(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	s1 := e.NewUser(auth.RoleStudent)
	var sent mailing.Send
	admin.Call("POST", "/api/admin/email/sends", map[string]any{"subject": "News", "body": "Hi", "audience": map[string]any{"kind": "students"}}, 201, &sent)
	// One fails for good, as the worker would leave it.
	if _, err := e.Pool.Exec(context.Background(), `UPDATE river_job SET state = 'discarded', finalized_at = now(), attempt = 5,
		errors = ARRAY['{"at":"2026-10-31T00:00:00Z","attempt":5,"error":"send: 550 5.7.25 no PTR record"}'::jsonb]
		WHERE kind = 'email' AND args->>'send_id' = $1`, sent.ID); err != nil {
		t.Fatal(err)
	}
	var q struct {
		Emails []mailing.Delivery
		Total  int
		Counts mailing.Progress
	}
	admin.Call("GET", "/api/admin/email/queue?status=failed", nil, 200, &q)
	if q.Total != 1 || q.Emails[0].To != s1.User.Email || q.Emails[0].Status != "failed" || !strings.Contains(q.Emails[0].Error, "PTR") || q.Counts["failed"] != 1 || q.Counts["waiting"] < 1 {
		t.Fatalf("queue = %+v", q)
	}
	var h struct{ Sends []mailing.Send }
	admin.Call("GET", "/api/admin/email/sends", nil, 200, &h)
	if h.Sends[0].Progress["failed"] != 1 {
		t.Fatalf("progress = %+v", h.Sends[0])
	}
	var r struct{ Retried int }
	admin.Call("POST", "/api/admin/email/queue/retry", map[string]any{"send_id": sent.ID}, 200, &r)
	if r.Retried != 1 {
		t.Fatalf("retried = %+v", r)
	}
	admin.Call("GET", "/api/admin/email/queue?send="+sent.ID, nil, 200, &q)
	if q.Counts["failed"] != 0 || q.Emails[0].Status != "waiting" {
		t.Fatalf("after retry = %+v", q)
	}
	admin.Call("POST", "/api/admin/email/queue/retry", map[string]any{"id": 999999999}, 404, nil)
}
