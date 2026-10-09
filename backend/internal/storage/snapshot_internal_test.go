package storage

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

// A backup reads one snapshot without locking writers out (PL-NFR-01):
// while a backup is held open mid-stream, a write still goes through.
func TestBackupDoesNotBlockWrites(t *testing.T) {
	pool := dbtest.New(t)
	ctx := t.Context()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := dump(ctx, conn.Conn(), pw, "", nil)
		pw.CloseWithError(err)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond) // nothing reads yet: the backup waits with its snapshot open
	wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := pool.Exec(wctx, `INSERT INTO audit.events (action, target_type, target_id) VALUES ('during', 'backup', 'x')`); err != nil {
		t.Fatalf("write during a backup: %v", err)
	}
	if _, err := pool.Exec(wctx, `UPDATE auth.policy SET require_teacher_approval = true`); err != nil {
		t.Fatalf("update during a backup: %v", err)
	}
	if _, err := io.Copy(io.Discard, pr); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
