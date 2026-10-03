package auth

import (
	"context"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
)

func TestHasherRoundTrip(t *testing.T) {
	h := NewHasher(2)
	ctx := context.Background()
	enc, err := h.Hash(ctx, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := h.Verify(ctx, "correct horse", enc); !ok {
		t.Fatal("valid password rejected")
	}
	if ok, _ := h.Verify(ctx, "wrong", enc); ok {
		t.Fatal("wrong password accepted")
	}
	if want := "$argon2id$v=19$m=19456,t=2,p=1$"; enc[:len(want)] != want {
		t.Fatalf("parameters must match OWASP baseline: %s", enc)
	}
}

func TestHasherPoolIsBounded(t *testing.T) {
	h := NewHasher(1)
	h.acquireMax = 20 * time.Millisecond
	h.slots <- struct{}{} // occupy the only worker
	if _, err := h.Hash(context.Background(), "x"); err != ErrBusy {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	<-h.slots
	h.maxQueue = 0
	if _, err := h.Hash(context.Background(), "x"); err != ErrBusy {
		t.Fatalf("queue full: err = %v, want ErrBusy", err)
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(2, time.Second)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }
	if !l.Allow("k") || !l.Allow("k") || l.Allow("k") {
		t.Fatal("burst of 2 not enforced")
	}
	if !l.Allow("other") {
		t.Fatal("keys must be independent")
	}
	now = now.Add(time.Second)
	if !l.Allow("k") || l.Allow("k") {
		t.Fatal("refill of 1/s not applied")
	}
}

func TestMain(m *testing.M) { dbtest.Main(m) }

func TestSessionIdleAndAbsoluteTimeouts(t *testing.T) {
	pool := dbtest.New(t)
	rc, err := jobs.NewClient(pool, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(pool, NewHasher(1), rc, "https://x")
	ctx := context.Background()
	now := time.Now()
	s.now = func() time.Time { return now }

	u, err := s.CreateAdmin(ctx, "a@example.com", "A", "password123")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.CreateSession(ctx, u, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Authenticate(ctx, token); !ok {
		t.Fatal("fresh session rejected")
	}
	// Admin idle timeout is 30 minutes.
	now = now.Add(31 * time.Minute)
	if _, ok, _ := s.Authenticate(ctx, token); ok {
		t.Fatal("idle admin session accepted")
	}

	// Activity keeps a session alive until the absolute timeout (12h for admins).
	now = time.Now()
	token, _, _ = s.CreateSession(ctx, u, "test")
	for i := 0; i < 24; i++ {
		now = now.Add(29 * time.Minute)
		if _, ok, _ := s.Authenticate(ctx, token); !ok {
			t.Fatalf("active session expired early at step %d", i)
		}
	}
	now = now.Add(29 * time.Minute) // 12h05m since creation
	if _, ok, _ := s.Authenticate(ctx, token); ok {
		t.Fatal("session outlived the absolute timeout")
	}
}
