package main

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestEnsureDatabase(t *testing.T) {
	url := os.Getenv("QP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("needs the shared test server (go run ./cmd/testdb)")
	}
	ctx := context.Background()
	const name = "qp_ensure_test"
	t.Cleanup(func() {
		if c, err := pgx.Connect(ctx, url); err == nil {
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name)
			c.Close(ctx)
		}
	})
	for i := 0; i < 2; i++ { // the second call finds it and does nothing
		if err := ensureDatabase(ctx, url, name); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if err := ensureDatabase(ctx, url, "bad; DROP DATABASE postgres"); err == nil {
		t.Fatal("an unsafe name was accepted")
	}
}
