package auth

import (
	"sync"
	"time"
)

// Limiter is an in-memory token bucket per key (ADR-13: per IP and per account).
type Limiter struct {
	mu      sync.Mutex
	burst   float64
	perSec  float64
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewLimiter(burst int, every time.Duration) *Limiter {
	return &Limiter{burst: float64(burst), perSec: 1 / every.Seconds(), buckets: map[string]*bucket{}, now: time.Now}
}

// Allow consumes one token for key if available.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) > 50_000 {
			l.sweep(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.perSec)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep drops buckets that have refilled completely.
func (l *Limiter) sweep(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.perSec >= l.burst {
			delete(l.buckets, k)
		}
	}
}
