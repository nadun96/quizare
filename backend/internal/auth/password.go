package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters: OWASP's first recommended configuration (ADR-13).
const (
	argonMemoryKiB = 19456
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// ErrBusy means the hashing pool is saturated; callers return 503 + Retry-After.
var ErrBusy = errors.New("password hashing pool busy")

// Hasher runs Argon2id in a bounded worker pool. Without the cap, 100
// simultaneous logins would allocate ~1.9 GB (architecture §2.1, risk R1).
type Hasher struct {
	mu         sync.Mutex
	inUse      int
	workers    int
	freed      chan struct{} // closed, and replaced, whenever a slot frees or the pool grows
	waiting    atomic.Int64
	maxQueue   int64
	acquireMax time.Duration
	// memoryKiB is overridable so unit tests stay fast; production uses the constant.
	memoryKiB uint32
}

func NewHasher(workers int) *Hasher {
	if workers < 1 {
		workers = 1
	}
	return &Hasher{
		workers:    workers,
		freed:      make(chan struct{}),
		maxQueue:   200,
		acquireMax: 10 * time.Second,
		memoryKiB:  argonMemoryKiB,
	}
}

func (h *Hasher) acquire(ctx context.Context) error {
	if h.waiting.Add(1) > h.maxQueue {
		h.waiting.Add(-1)
		return ErrBusy
	}
	defer h.waiting.Add(-1)
	t := time.NewTimer(h.acquireMax)
	defer t.Stop()
	for {
		h.mu.Lock()
		if h.inUse < h.workers {
			h.inUse++
			h.mu.Unlock()
			return nil
		}
		freed := h.freed
		h.mu.Unlock()
		select {
		case <-freed:
		case <-t.C:
			return ErrBusy
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (h *Hasher) release() {
	h.mu.Lock()
	h.inUse--
	h.wake()
	h.mu.Unlock()
}

// wake lets every waiter look again; h.mu is held.
func (h *Hasher) wake() {
	close(h.freed)
	h.freed = make(chan struct{})
}

// SetWorkers changes how many passwords are checked at once (D-126).
// Checks already running finish; a smaller pool takes effect as they do.
func (h *Hasher) SetWorkers(n int) {
	h.mu.Lock()
	h.workers = max(1, n)
	h.wake()
	h.mu.Unlock()
}

// Workers is how many passwords are checked at once.
func (h *Hasher) Workers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.workers
}

// Hash returns a PHC-format Argon2id hash.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, h.memoryKiB, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memoryKiB, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify checks password against a PHC hash using the parameters stored in it.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported hash format")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.release()
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
