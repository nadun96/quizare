package presentation

import (
	"testing"
	"time"
)

// Live time adds up across short intervals: fractions of a second carry
// over to the next interval on the same slide instead of being lost.
func TestLiveTimeCarries(t *testing.T) {
	h := &liveHub{}
	start := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	m := &member{viewerID: "v", n: 3, visible: true, since: start}
	var total time.Duration
	now := start
	for range 6 { // six intervals of 0.6 s: 3.6 s in all
		now = now.Add(600 * time.Millisecond)
		for _, x := range h.close(m, now) {
			if x.n != 3 || x.d%time.Second != 0 {
				t.Fatalf("recorded %+v", x)
			}
			total += x.d
		}
	}
	if total != 3*time.Second || m.carry[3] != 600*time.Millisecond {
		t.Fatalf("total %v, carried %v", total, m.carry[3])
	}

	// A hidden tab counts nothing; a long silence counts a minute at most.
	m.visible = false
	if xs := h.close(m, now.Add(time.Hour)); xs != nil {
		t.Fatalf("hidden: %+v", xs)
	}
	m.visible = true
	xs := h.close(m, now.Add(2*time.Hour))
	if len(xs) != 1 || xs[0].d != maxInterval {
		t.Fatalf("capped: %+v", xs)
	}
	// Presenters are never counted.
	p := &member{presenter: true, n: 1, visible: true, since: start}
	if xs := h.close(p, start.Add(time.Minute)); xs != nil {
		t.Fatalf("presenter: %+v", xs)
	}
}
