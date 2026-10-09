package storage_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/db"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/storage"
	"github.com/nadun96/quizplatform/migrations"
)

// Storage and backups (PL-FR-04 to PL-FR-09, PL-NFR-01 to PL-NFR-05, D-57).

func TestMain(m *testing.M) { dbtest.Main(m) }

const pass = "correct horse battery staple"

type backupRow struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	SizeBytes int64  `json:"size_bytes"`
	OnServer  bool   `json:"on_server"`
	CreatedBy string `json:"created_by"`
	Error     string `json:"error"`
}

func backups(t *testing.T, c *apptest.Client, query string) []backupRow {
	t.Helper()
	var out struct{ Backups []backupRow }
	c.Call("GET", "/api/admin/backups"+query, nil, 200, &out)
	return out.Backups
}

// makeBackup starts a backup and waits for it.
func makeBackup(t *testing.T, e *apptest.Env, c *apptest.Client) backupRow {
	t.Helper()
	var b backupRow
	c.Call("POST", "/api/admin/backups", map[string]string{"passphrase": pass}, 202, &b)
	e.App.Storage.Wait()
	for _, x := range backups(t, c, "") {
		if x.ID == b.ID {
			if x.Status != "done" {
				t.Fatalf("backup %+v", x)
			}
			return x
		}
	}
	t.Fatal("backup not listed")
	return b
}

func download(t *testing.T, c *apptest.Client, id string) []byte {
	t.Helper()
	var link struct{ URL string }
	c.Call("POST", "/api/admin/backups/"+id+"/link", nil, 201, &link)
	code, _, body := c.Get(link.URL, nil)
	if code != 200 {
		t.Fatalf("download: %d %s", code, body)
	}
	return body
}

func count(t *testing.T, e *apptest.Env, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.Pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBackupRestoresEverything(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	teacher := e.NewUser(auth.RoleTeacher)
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Secret classroom 42"}, 201, nil)
	teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "Check-in"}, 201, nil)

	admin.Call("POST", "/api/admin/backups", map[string]string{"passphrase": "short"}, 422, nil)
	b := makeBackup(t, e, admin)
	if b.SizeBytes == 0 || !b.OnServer || b.Kind != "console" || b.CreatedBy != "Admin" {
		t.Fatalf("backup = %+v", b)
	}
	file := download(t, admin, b.ID)
	// Encrypted on disk and in the download (PL-NFR-04).
	if !bytes.HasPrefix(file, []byte("age-encryption.org/v1")) || bytes.Contains(file, []byte("Secret classroom 42")) {
		t.Fatal("the backup isn't encrypted")
	}
	if _, err := storage.Open(bytes.NewReader(file), "wrong passphrase!!"); !errors.Is(err, storage.ErrPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	r, err := storage.Open(bytes.NewReader(file), pass)
	if err != nil {
		t.Fatal(err)
	}
	m, counts, err := storage.Check(r)
	if err != nil || counts["content.classrooms"] != 1 || m.KEKID == "" {
		t.Fatalf("check: %v %v", err, counts)
	}
	for _, tb := range m.Tables {
		if tb.Name == "storage.backups" || strings.HasPrefix(tb.Name, "public.") {
			t.Fatalf("%s should not be in a backup", tb.Name)
		}
	}

	// Restore into an empty database: every table has the same rows.
	empty := dbtest.NewEmpty(t)
	r, _ = storage.Open(bytes.NewReader(file), pass)
	if _, _, err := storage.Restore(t.Context(), empty, r, migrations.FS); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for _, tb := range m.Tables {
		var src, dst int
		q := "SELECT count(*) FROM " + tb.Name
		if err := e.Pool.QueryRow(t.Context(), q).Scan(&src); err != nil {
			t.Fatal(err)
		}
		if err := empty.QueryRow(t.Context(), q).Scan(&dst); err != nil {
			t.Fatal(err)
		}
		// Taking the backup itself writes audit rows after the snapshot.
		if int64(dst) != counts[tb.Name] || (src != dst && tb.Name != "audit.events") {
			t.Fatalf("%s: source %d, backup %d, restored %d", tb.Name, src, counts[tb.Name], dst)
		}
	}
	var name string
	if err := empty.QueryRow(t.Context(), `SELECT name FROM content.classrooms`).Scan(&name); err != nil || name != "Secret classroom 42" {
		t.Fatalf("restored classroom %q, %v", name, err)
	}
	// Sequences continue after the restored rows, and a normal start still works.
	if err := db.Migrate(t.Context(), empty, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Migrate(t.Context(), empty); err != nil {
		t.Fatal(err)
	}
	if _, err := empty.Exec(t.Context(), `INSERT INTO audit.events (action, target_type, target_id) VALUES ('x', 'y', 'z')`); err != nil {
		t.Fatalf("insert after restore: %v", err)
	}
	// Only into an empty database.
	r, _ = storage.Open(bytes.NewReader(file), pass)
	if _, _, err := storage.Restore(t.Context(), empty, r, migrations.FS); err == nil || !strings.Contains(err.Error(), "isn't empty") {
		t.Fatalf("restore into a used database: %v", err)
	}
	if count(t, e, `SELECT count(*) FROM audit.events WHERE action IN ('backup_started', 'backup_finished', 'backup_downloaded')`) != 3 {
		t.Fatal("backup actions not audited")
	}
}

func TestDownloadLinksAreSingleUseAndPersonal(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	other := e.NewUser(auth.RoleAdmin)
	b := makeBackup(t, e, admin)
	var link struct{ URL string }
	admin.Call("POST", "/api/admin/backups/"+b.ID+"/link", nil, 201, &link)
	if code, _, _ := other.Get(link.URL, nil); code != 404 {
		t.Fatalf("someone else's link: %d", code)
	}
	if code, _, _ := admin.Get(link.URL, nil); code != 200 {
		t.Fatalf("first use: %d", code)
	}
	if code, _, _ := admin.Get(link.URL, nil); code != 404 {
		t.Fatalf("second use: %d", code)
	}
	admin.Call("POST", "/api/admin/backups/"+b.ID+"/link", nil, 201, &link)
	if _, err := e.Pool.Exec(t.Context(), `UPDATE storage.download_links SET expires_at = now() - interval '1 minute' WHERE used_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := admin.Get(link.URL, nil); code != 404 {
		t.Fatalf("expired link: %d", code)
	}
	// Deleting keeps the history but removes the file.
	admin.Call("DELETE", "/api/admin/backups/"+b.ID, nil, 204, nil)
	if l := backups(t, admin, ""); len(l) != 1 || l[0].OnServer {
		t.Fatalf("after delete: %+v", l)
	}
	admin.Call("POST", "/api/admin/backups/"+b.ID+"/link", nil, 410, nil)
	admin.Call("DELETE", "/api/admin/backups/"+b.ID, nil, 410, nil)
	// Only admins and managers with "Backups" (PL-NFR-02).
	e.NewUser(auth.RoleTeacher).Call("GET", "/api/admin/backups", nil, 403, nil)
}

func TestSpaceIsCheckedFirstAndLimits(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	free := uint64(10 << 30)
	e.App.Storage.SetDisk(func(string) (uint64, uint64, error) { return free, 100 << 30, nil })

	var l storage.Limits
	admin.Call("PUT", "/api/admin/storage/limits", storage.Limits{RecordingLimitBytes: 6 << 30, BackupSpaceBytes: 2 << 30}, 200, &l)
	admin.Call("PUT", "/api/admin/storage/limits", storage.Limits{RecordingLimitBytes: 11 << 30, BackupSpaceBytes: 2 << 30}, 422, nil)
	admin.Call("PUT", "/api/admin/storage/limits", storage.Limits{RecordingLimitBytes: 6 << 30, BackupSpaceBytes: 1}, 422, nil)
	var o storage.Overview
	admin.Call("GET", "/api/admin/storage", nil, 200, &o)
	if o.Limits.RecordingLimitBytes != 6<<30 || len(o.Fixed) == 0 {
		t.Fatalf("overview limits = %+v", o.Limits)
	}
	if count(t, e, `SELECT count(*) FROM audit.events WHERE action='storage_limits_changed'`) != 1 {
		t.Fatal("limit change not audited")
	}

	// Less than twice the database free: refused, saying how much is needed (PL-NFR-05).
	free = 1 << 20
	_, raw := admin.Do("POST", "/api/admin/backups", map[string]string{"passphrase": pass})
	if !strings.Contains(string(raw), "no_space") || !strings.Contains(string(raw), "needs about") {
		t.Fatalf("no space: %s", raw)
	}
	if len(backups(t, admin, "")) != 0 {
		t.Fatal("a refused backup was recorded")
	}
}

func TestOldBackupsArePrunedToTheBackupSpace(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	first := makeBackup(t, e, admin)
	if _, err := e.Pool.Exec(t.Context(), `UPDATE storage.limits SET backup_space_bytes = $1`, first.SizeBytes+first.SizeBytes/2); err != nil {
		t.Fatal(err)
	}
	second := makeBackup(t, e, admin)
	list := backups(t, admin, "")
	if len(list) != 2 || list[0].ID != second.ID || !list[0].OnServer || list[1].OnServer {
		t.Fatalf("after pruning: %+v", list)
	}
	// The newest stays even when it alone is over the space.
	if _, err := e.Pool.Exec(t.Context(), `UPDATE storage.limits SET backup_space_bytes = 1`); err != nil {
		t.Fatal(err)
	}
	third := makeBackup(t, e, admin)
	list = backups(t, admin, "")
	if !list[0].OnServer || list[0].ID != third.ID || list[1].OnServer {
		t.Fatalf("newest pruned: %+v", list)
	}
	// Old backups can also be cleaned up by date (PL-FR-06).
	var f storage.Freed
	tomorrow := time.Now().UTC().AddDate(0, 0, 2).Format(time.DateOnly)
	admin.Call("POST", "/api/admin/storage/cleanup/preview", map[string]string{"area": "backups", "before": tomorrow}, 422, nil)
	admin.Call("POST", "/api/admin/storage/cleanup/preview", map[string]string{"area": "backups", "before": time.Now().UTC().Format(time.DateOnly)}, 200, &f)
	if f.Items != 0 {
		t.Fatalf("today's backups offered: %+v", f)
	}
	if _, err := e.Pool.Exec(t.Context(), `UPDATE storage.backups SET started_at = started_at - interval '3 days'`); err != nil {
		t.Fatal(err)
	}
	admin.Call("POST", "/api/admin/storage/cleanup/preview", map[string]string{"area": "backups", "before": time.Now().UTC().Format(time.DateOnly)}, 200, &f)
	var done storage.Freed
	admin.Call("POST", "/api/admin/storage/cleanup", map[string]string{"area": "backups", "before": time.Now().UTC().Format(time.DateOnly)}, 200, &done)
	if f.Items != 1 || done != f {
		t.Fatalf("preview %+v, cleaned %+v", f, done)
	}
}

func TestOverviewShowsAreasAndGrowth(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	var o storage.Overview
	admin.Call("GET", "/api/admin/storage", nil, 200, &o)
	if o.DBBytes == 0 || o.Areas["accounts"] == 0 || o.Areas["classrooms"] == 0 || o.DiskTotal == 0 || len(o.Growth) != 1 {
		t.Fatalf("overview = %+v", o)
	}
	if o.Nightly.Configured {
		t.Fatal("no nightly job is configured")
	}
	first := o.TakenAt
	// Within the hour the same figures come back; refresh measures again.
	admin.Call("GET", "/api/admin/storage", nil, 200, &o)
	if !o.TakenAt.Equal(first) {
		t.Fatal("re-measured within the hour")
	}
	if _, err := e.Pool.Exec(t.Context(), `UPDATE storage.snapshots SET taken_at = taken_at - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	admin.Call("POST", "/api/admin/storage/refresh", nil, 200, &o)
	if count(t, e, `SELECT count(*) FROM storage.snapshots`) != 2 {
		t.Fatal("refresh didn't measure")
	}
}

func TestNightlyJobIsReported(t *testing.T) {
	dir := t.TempDir()
	e := apptest.New(t, apptest.WithNightlyDir(dir))
	admin := e.NewUser(auth.RoleAdmin)
	for _, d := range []string{"daily", "status"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	nightly := filepath.Join(dir, "daily", "quiz-20261001.dump.age")
	if err := os.WriteFile(nightly, []byte("age-encryption.org/v1 …"), 0o600); err != nil {
		t.Fatal(err)
	}
	ran := time.Now().Add(-72 * time.Hour)
	_ = os.WriteFile(filepath.Join(dir, "status", "last-run"), []byte("failed "+time.Now().UTC().Format(time.RFC3339)), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "status", "last-success"), nil, 0o644)
	_ = os.Chtimes(filepath.Join(dir, "status", "last-success"), ran, ran)

	var o storage.Overview
	admin.Call("POST", "/api/admin/storage/refresh", nil, 200, &o)
	if !o.Nightly.Configured || o.Nightly.LastRunOK || !o.Nightly.Stale || o.Nightly.LastRunAt == nil {
		t.Fatalf("nightly = %+v", o.Nightly)
	}
	list := backups(t, admin, "?kind=nightly")
	if len(list) != 1 || !list[0].OnServer {
		t.Fatalf("nightly backups = %+v", list)
	}
	// The nightly job owns its files, but they can be downloaded.
	admin.Call("DELETE", "/api/admin/backups/"+list[0].ID, nil, 400, nil)
	if body := download(t, admin, list[0].ID); !bytes.HasPrefix(body, []byte("age-encryption.org/v1")) {
		t.Fatalf("nightly download = %q", body)
	}
	// The script's rotation deleted it: still in the history, no longer on the server.
	_ = os.Remove(nightly)
	if _, err := e.Pool.Exec(t.Context(), `UPDATE storage.snapshots SET taken_at = taken_at - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	admin.Call("POST", "/api/admin/storage/refresh", nil, 200, &o)
	if list = backups(t, admin, "?kind=nightly"); len(list) != 1 || list[0].OnServer {
		t.Fatalf("after rotation = %+v", list)
	}
}

func TestBackupCutShortByARestart(t *testing.T) {
	e := apptest.New(t)
	admin := e.NewUser(auth.RoleAdmin)
	dir := t.TempDir()
	file := filepath.Join(dir, "qp-x.qpbackup.age")
	_ = os.WriteFile(file+".part", []byte("half"), 0o600)
	if _, err := e.Pool.Exec(t.Context(), `INSERT INTO storage.backups (kind, file, created_by) VALUES ('console', $1, $2)`, file, admin.User.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.App.Storage.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	list := backups(t, admin, "")
	if len(list) != 1 || list[0].Status != "failed" || list[0].Error == "" || list[0].OnServer {
		t.Fatalf("after restart = %+v", list)
	}
	if _, err := os.Stat(file + ".part"); !os.IsNotExist(err) {
		t.Fatal("partial file left behind")
	}
}
