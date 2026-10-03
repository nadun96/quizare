package db_test

import (
	"context"
	"testing"

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
