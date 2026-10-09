package poll_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/poll"
)

// The admin frees the space of uploaded files of closed polls (PL-FR-06):
// the preview's figures match what goes, answers stay marked "removed",
// and open polls are untouched.
func TestCleanupOfOldPollFiles(t *testing.T) {
	f := setup(t, nil, fileQ)
	a := f.anon(t)
	a.Headers["X-File-Name"] = url.QueryEscape("diagram.png")
	if code, out := a.Raw("POST", "/api/polls/"+f.poll.JoinCode+"/files/"+f.qs[0].ID, "image/png", png); code != 200 {
		t.Fatalf("upload: %d %s", code, out)
	}
	admin := f.e.NewUser(auth.RoleAdmin)
	today := time.Now().UTC().Format(time.DateOnly)
	req := map[string]string{"area": "poll_files", "before": today}
	var freed struct{ Items, Bytes int64 }

	// Still open: nothing to clean.
	admin.Call("POST", "/api/admin/storage/cleanup/preview", req, 200, &freed)
	if freed.Items != 0 {
		t.Fatalf("open poll's files offered for clean-up: %+v", freed)
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/status", map[string]string{"status": "closed"}, 200, nil)
	if _, err := f.e.Pool.Exec(t.Context(), `UPDATE poll.polls SET closed_at = now() - interval '10 days' WHERE id=$1`, f.poll.ID); err != nil {
		t.Fatal(err)
	}
	admin.Call("POST", "/api/admin/storage/cleanup/preview", req, 200, &freed)
	if freed.Items != 1 || freed.Bytes != int64(len(png)) {
		t.Fatalf("preview = %+v, want 1 file of %d bytes", freed, len(png))
	}
	var done struct{ Items, Bytes int64 }
	admin.Call("POST", "/api/admin/storage/cleanup", req, 200, &done)
	if done != freed {
		t.Fatalf("deleted %+v, preview said %+v", done, freed)
	}
	admin.Call("POST", "/api/admin/storage/cleanup/preview", req, 200, &freed)
	if freed.Items != 0 {
		t.Fatal("files still there")
	}
	// The answer stays, marked removed.
	r := f.results().Results[f.qs[0].ID]
	if len(r.Files) != 1 || !r.Files[0].File.Removed {
		t.Fatalf("file answer after clean-up: %+v", r.Files)
	}
	f.teacher.Call("GET", "/api/teacher/polls/"+f.poll.ID+"/files/"+r.Files[0].File.ID, nil, 404, nil)
	var n int
	if err := f.e.Pool.QueryRow(t.Context(), `SELECT count(*) FROM audit.events WHERE action='cleanup_run' AND target_id='poll_files'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("clean-up audit events = %d, %v", n, err)
	}
	// Bad requests.
	admin.Call("POST", "/api/admin/storage/cleanup/preview", map[string]string{"area": "poll_files", "before": time.Now().UTC().AddDate(0, 0, 2).Format(time.DateOnly)}, 422, nil)
	admin.Call("POST", "/api/admin/storage/cleanup/preview", map[string]string{"area": "nope", "before": today}, 422, nil)
	_ = poll.FileRef{}
}
