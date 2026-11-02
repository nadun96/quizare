package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/platform/db"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/migrations"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func TestMigrateIsIdempotent(t *testing.T) {
	pool := dbtest.New(t) // already migrated once
	ctx := context.Background()
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no migrations recorded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit.events(action,target_type,target_id) VALUES ('x','y','z')`); err != nil {
		t.Fatalf("audit table missing: %v", err)
	}
}

func TestOpenWaitGivesUp(t *testing.T) {
	start := time.Now()
	// Nothing listens on port 1: every attempt fails, so OpenWait must stop at the deadline.
	_, err := db.OpenWait(context.Background(), "postgres://x:y@127.0.0.1:1/x?sslmode=disable&connect_timeout=1", 1, 2*time.Second)
	if err == nil {
		t.Fatal("expected an error")
	}
	if d := time.Since(start); d < time.Second || d > 10*time.Second {
		t.Fatalf("gave up after %v, want about 2 s", d)
	}
}

func TestOpenWaitSucceedsImmediately(t *testing.T) {
	pool := dbtest.New(t)
	url := pool.Config().ConnString()
	p, err := db.OpenWait(context.Background(), url, 2, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
}

// Every foreign key has an index that starts with it (D-119): without one,
// deleting a user or a classroom, or listing by owner, reads the whole
// table. The exceptions are single-row settings tables.
func TestForeignKeysAreIndexed(t *testing.T) {
	pool := dbtest.New(t)
	rows, err := pool.Query(context.Background(), `
		SELECT c.conrelid::regclass::text || '.' || a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		WHERE c.contype = 'f' AND NOT EXISTS (
			SELECT 1 FROM pg_index i WHERE i.indrelid = c.conrelid AND i.indkey[0] = c.conkey[1])
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	small := map[string]bool{"auth.managers.granted_by": true, "mailing.server.updated_by": true, "llm.local.updated_by": true}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		if !small[k] {
			t.Errorf("foreign key %s has no index", k)
		}
	}
}
