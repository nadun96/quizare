package loadtest

import (
	"context"
	"net"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Ops a virtual student or the teacher times, in the order a report lists them.
var Ops = []string{"login", "join", "connect", "start", "save", "advance", "submit", "admit"}

// Latency is one operation's times in milliseconds.
type Latency struct {
	N   int     `json:"n"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

// recorder keeps a step's timings and failures.
type recorder struct {
	mu     sync.Mutex
	times  map[string][]float64
	errs   map[string]int
	reason map[string]string // the first failure of each kind, for the report
}

func newRecorder() *recorder {
	return &recorder{times: map[string][]float64{}, errs: map[string]int{}, reason: map[string]string{}}
}

func (r *recorder) ok(op string, d time.Duration) {
	r.mu.Lock()
	r.times[op] = append(r.times[op], float64(d.Microseconds())/1000)
	r.mu.Unlock()
}

func (r *recorder) fail(op string, why string) {
	r.mu.Lock()
	r.errs[op]++
	if _, seen := r.reason[op]; !seen {
		r.reason[op] = why
	}
	r.mu.Unlock()
}

// retry counts a request the server asked to try again (busy, or the
// per-network limit); it isn't a failure unless the retries run out.
func (r *recorder) retry(kind string) {
	r.mu.Lock()
	r.errs[kind]++
	r.mu.Unlock()
}

func (r *recorder) latencies() map[string]Latency {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]Latency{}
	for op, xs := range r.times {
		out[op] = summarise(xs)
	}
	return out
}

func (r *recorder) failures() (map[string]int, map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	errs, why := map[string]int{}, map[string]string{}
	for k, v := range r.errs {
		errs[k] = v
	}
	for k, v := range r.reason {
		why[k] = v
	}
	return errs, why
}

// summarise gives the median, 95th and 99th percentiles (nearest rank) and the maximum.
func summarise(xs []float64) Latency {
	if len(xs) == 0 {
		return Latency{}
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	at := func(p float64) float64 {
		i := int(p*float64(len(s))+0.999999) - 1
		return round1(s[max(0, min(i, len(s)-1))])
	}
	return Latency{N: len(s), P50: at(0.50), P95: at(0.95), P99: at(0.99), Max: round1(s[len(s)-1])}
}

func round1(x float64) float64 { return float64(int64(x*10+0.5)) / 10 }

// meter counts the bytes the virtual students' connections carry: what the
// server sends them ("down") and what they send ("up").
type meter struct{ down, up atomic.Int64 }

type countedConn struct {
	net.Conn
	m *meter
}

func (c countedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.m.down.Add(int64(n))
	return n, err
}

func (c countedConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.m.up.Add(int64(n))
	return n, err
}

func (m *meter) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	c, err := (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return countedConn{Conn: c, m: m}, nil
}

// Sample is the server's state at one moment of a step.
type Sample struct {
	T          float64 `json:"t"`           // seconds since the step began
	CPU        float64 `json:"cpu"`         // the whole machine, % of all cores; -1 unknown
	MemFreeMB  float64 `json:"mem_free_mb"` // available memory; -1 unknown
	Load1      float64 `json:"load1"`       // -1 unknown
	HeapMB     float64 `json:"heap_mb"`
	Goroutines int     `json:"goroutines"`
	DBBusy     int32   `json:"db_busy"`  // connections in use
	DBWaits    int64   `json:"db_waits"` // requests that waited for a connection since the last sample
	DownMbps   float64 `json:"down_mbps"`
	UpMbps     float64 `json:"up_mbps"`
	Active     int64   `json:"active"` // students connected
	Saves      int64   `json:"saves"`  // answers saved since the last sample
}

// sampler takes a Sample every second until stopped.
type sampler struct {
	pool    *pgxpool.Pool
	m       *meter
	active  *atomic.Int64
	saves   *atomic.Int64
	start   time.Time
	mu      sync.Mutex
	samples []Sample
}

func (s *sampler) run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	cpu := newCPUMeter()
	// The meter counts from the test's start: each step measures from its own.
	lastDown, lastUp, lastSaves := s.m.down.Load(), s.m.up.Load(), s.saves.Load()
	var lastWaits int64
	if s.pool != nil {
		lastWaits = s.pool.Stat().EmptyAcquireCount()
	}
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			secs := now.Sub(last).Seconds()
			last = now
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			down, up, saves := s.m.down.Load(), s.m.up.Load(), s.saves.Load()
			x := Sample{
				T: round1(now.Sub(s.start).Seconds()), CPU: cpu.percent(), MemFreeMB: memAvailableMB(), Load1: load1(),
				HeapMB: round1(float64(ms.HeapAlloc) / (1 << 20)), Goroutines: runtime.NumGoroutine(),
				DownMbps: round1(float64(down-lastDown) * 8 / 1e6 / secs), UpMbps: round1(float64(up-lastUp) * 8 / 1e6 / secs),
				Active: s.active.Load(), Saves: saves - lastSaves,
			}
			if s.pool != nil {
				st := s.pool.Stat()
				x.DBBusy, x.DBWaits = st.AcquiredConns(), st.EmptyAcquireCount()-lastWaits
				lastWaits = st.EmptyAcquireCount()
			}
			lastDown, lastUp, lastSaves = down, up, saves
			s.mu.Lock()
			s.samples = append(s.samples, x)
			s.mu.Unlock()
		}
	}
}

func (s *sampler) all() []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.samples)
}

func (s *sampler) latest() *Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.samples) == 0 {
		return nil
	}
	x := s.samples[len(s.samples)-1]
	return &x
}
