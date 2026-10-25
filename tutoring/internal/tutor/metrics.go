package tutor

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Monitoring (D-82, TS-NFR-41). Each check of the resource monitor also
// reads LiveKit's Prometheus counters, when LIVEKIT_METRICS_URL is set, for
// the media server's measured upload, download and CPU; LiveKit reports
// nothing per room, so each session's figures stay the estimates of D-81
// (its CPU is the measured CPU shared out by upload). A row a minute, for
// all of tutoring and for each live session, is kept 7 days.

// MetricsKeep is how long the minute rows are kept.
const MetricsKeep = 7 * 24 * time.Hour

// Measured is the media server's own figures, from two readings of its counters.
type Measured struct {
	UpMbps   float64   `json:"up_mbps"`
	DownMbps float64   `json:"down_mbps"`
	CPU      float64   `json:"cpu"` // cores
	At       time.Time `json:"at"`
}

type metricsState struct {
	url        string
	client     *http.Client
	last       media.Counters
	measured   *Measured
	failing    bool
	minute     time.Time
	acc        map[string]*bucket // session id, "" for all of tutoring
	reconnects map[string]int     // each live session's, since it went live (or the service started)
}

// bucket gathers one minute's checks.
type bucket struct {
	up, down, cpu                    float64
	n, cpuN                          int
	tracks, participants, reconnects int
	measured                         bool
}

// SetMetrics points the monitor at LiveKit's Prometheus endpoint ("" for none).
func (s *Service) SetMetrics(url string) {
	s.mon.mu.Lock()
	defer s.mon.mu.Unlock()
	s.mon.met.url = url
	s.mon.met.client = &http.Client{Timeout: 2 * time.Second}
}

// observe is CheckLoad's monitoring part: reconnects, the measured
// figures, and the minute's samples. s.mon.mu is held.
func (s *Service) observe(ctx context.Context, live []Session, loads map[string]Load) {
	m := &s.mon.met
	if m.acc == nil {
		m.acc, m.reconnects = map[string]*bucket{}, map[string]int{}
	}
	isLive := map[string]bool{}
	for _, sess := range live {
		isLive[sess.ID] = true
	}
	back := s.hub.takeReconnects(isLive)
	for id := range m.reconnects {
		if !isLive[id] {
			delete(m.reconnects, id)
		}
	}
	for id, l := range loads {
		m.reconnects[id] += back[id]
		l.Reconnects = m.reconnects[id]
		loads[id] = l
	}

	if m.url != "" {
		c, err := media.Scrape(ctx, m.client, m.url)
		switch {
		case err != nil:
			if !m.failing {
				s.log.Warn("media server metrics", "err", err)
			}
			m.failing, m.measured, m.last = true, nil, media.Counters{}
		default:
			m.failing = false
			dt := c.At.Sub(m.last.At).Seconds()
			// A restart of LiveKit resets its counters: start again from this reading.
			if !m.last.At.IsZero() && dt >= 0.5 && c.BytesOut >= m.last.BytesOut && c.BytesIn >= m.last.BytesIn && c.CPUSeconds >= m.last.CPUSeconds {
				m.measured = &Measured{
					UpMbps:   round2((c.BytesOut - m.last.BytesOut) * 8 / dt / 1e6),
					DownMbps: round2((c.BytesIn - m.last.BytesIn) * 8 / dt / 1e6),
					CPU:      round2((c.CPUSeconds - m.last.CPUSeconds) / dt),
					At:       c.At,
				}
			}
			m.last = c
		}
	}

	// A new minute: the last one's rows go to the database.
	minute := s.now().Truncate(time.Minute)
	if !m.minute.IsZero() && minute.After(m.minute) {
		if err := s.flushMetrics(ctx, m.minute); err != nil {
			s.log.Warn("saving metrics", "err", err)
		}
	}
	m.minute = minute

	add := func(key string, up, down float64, cpu *float64, tracks, participants, reconnects int, measured bool) {
		b := m.acc[key]
		if b == nil {
			b = &bucket{}
			m.acc[key] = b
		}
		b.up += up
		b.down += down
		b.n++
		if cpu != nil {
			b.cpu += *cpu
			b.cpuN++
		}
		b.tracks, b.participants = max(b.tracks, tracks), max(b.participants, participants)
		b.reconnects += reconnects
		b.measured = b.measured || measured
	}
	var t Load
	for id, l := range loads {
		add(id, l.Mbps, l.DownMbps, nil, l.Tracks, l.Participants, back[id], false)
		t.Mbps += l.Mbps
		t.DownMbps += l.DownMbps
		t.Tracks += l.Tracks
		t.Participants += l.Participants
		t.Reconnects += back[id]
	}
	switch ms := m.measured; {
	case len(loads) == 0:
		// Nothing live: no row, so the history shows the time nothing ran as a gap.
	case ms != nil:
		add("", ms.UpMbps, ms.DownMbps, &ms.CPU, t.Tracks, t.Participants, t.Reconnects, true)
	default:
		add("", t.Mbps, t.DownMbps, nil, t.Tracks, t.Participants, t.Reconnects, false)
	}
}

// FlushMetrics writes the samples gathered so far as the rows of minute at
// (the monitor does it as each minute ends), and forgets rows past 7 days.
func (s *Service) FlushMetrics(ctx context.Context, at time.Time) error {
	s.mon.mu.Lock()
	defer s.mon.mu.Unlock()
	return s.flushMetrics(ctx, at.Truncate(time.Minute))
}

func (s *Service) flushMetrics(ctx context.Context, at time.Time) error {
	acc := s.mon.met.acc
	s.mon.met.acc = map[string]*bucket{}
	batch := &pgx.Batch{}
	for key, b := range acc {
		if b.n == 0 {
			continue
		}
		var sid, cpu any
		if key != "" {
			sid = key
		}
		if b.cpuN > 0 {
			cpu = round2(b.cpu / float64(b.cpuN))
		}
		batch.Queue(`INSERT INTO tutoring.metrics (at, session_id, up_mbps, down_mbps, cpu, tracks, participants, reconnects, measured)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, at, sid, round2(b.up/float64(b.n)), round2(b.down/float64(b.n)), cpu, b.tracks, b.participants, b.reconnects, b.measured)
	}
	batch.Queue(`DELETE FROM tutoring.metrics WHERE at < $1`, at.Add(-MetricsKeep))
	return s.pool.SendBatch(ctx, batch).Close()
}

// ---------- the admin's view ----------

// LiveTotal is all of tutoring now.
type LiveTotal struct {
	UpMbps       float64   `json:"up_mbps"`
	DownMbps     float64   `json:"down_mbps"`
	CPU          *float64  `json:"cpu,omitempty"` // measured only
	Tracks       int       `json:"tracks"`
	Participants int       `json:"participants"`
	Reconnects   int       `json:"reconnects"`
	Measured     *Measured `json:"measured,omitempty"`
	Estimated    float64   `json:"estimated_up_mbps"` // the sessions' estimates added up
}

// HistoryPoint is all of tutoring over one stretch of the chart.
type HistoryPoint struct {
	At           time.Time `json:"at"`
	UpMbps       float64   `json:"up_mbps"`
	DownMbps     float64   `json:"down_mbps"`
	CPU          *float64  `json:"cpu,omitempty"`
	Participants int       `json:"participants"`
	Reconnects   int       `json:"reconnects"`
	Measured     bool      `json:"measured"`
}

// SessionHistory is one session over the range.
type SessionHistory struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	TeacherName      string    `json:"teacher_name"`
	From             time.Time `json:"from"`
	To               time.Time `json:"to"`
	PeakUpMbps       float64   `json:"peak_up_mbps"`
	AvgUpMbps        float64   `json:"avg_up_mbps"`
	PeakDownMbps     float64   `json:"peak_down_mbps"`
	PeakParticipants int       `json:"peak_participants"`
	PeakTracks       int       `json:"peak_tracks"`
	Reconnects       int       `json:"reconnects"`
}

// MonitorView is the admin's monitoring page (TS-NFR-41).
type MonitorView struct {
	Total      LiveTotal        `json:"total"`
	Sessions   []LiveUse        `json:"sessions"`
	BudgetMbps float64          `json:"budget_mbps"`
	Range      string           `json:"range"`
	Step       int              `json:"step_seconds"`
	Points     []HistoryPoint   `json:"points"`
	History    []SessionHistory `json:"history"`
	Configured bool             `json:"measuring"` // LIVEKIT_METRICS_URL is set
}

var monitorRanges = map[string]struct {
	span time.Duration
	step int // seconds per point
}{"24h": {24 * time.Hour, 300}, "7d": {MetricsKeep, 1800}}

// Monitoring returns what tutoring sends now and over the last day or week.
func (s *Service) Monitoring(ctx context.Context, p authn.Person, rng string) (MonitorView, error) {
	if err := requireAdmin(p); err != nil {
		return MonitorView{}, err
	}
	r, ok := monitorRanges[rng]
	if !ok {
		return MonitorView{}, web.Invalid(map[string]string{"range": "24h or 7d"})
	}
	b, err := s.budget(ctx)
	if err != nil {
		return MonitorView{}, err
	}
	sessions, err := s.liveUses(ctx)
	if err != nil {
		return MonitorView{}, err
	}
	v := MonitorView{Sessions: sessions, BudgetMbps: b.TotalMbps, Range: rng, Step: r.step}
	s.mon.mu.Lock()
	ms := s.mon.met.measured
	v.Configured = s.mon.met.url != ""
	s.mon.mu.Unlock()
	for _, u := range sessions {
		v.Total.UpMbps += u.Mbps
		v.Total.DownMbps += u.DownMbps
		v.Total.Tracks += u.Tracks
		v.Total.Participants += u.Participants
		v.Total.Reconnects += u.Reconnects
	}
	v.Total.Estimated = round2(v.Total.UpMbps)
	if ms != nil {
		v.Total.Measured = ms
		v.Total.UpMbps, v.Total.DownMbps, v.Total.CPU = ms.UpMbps, ms.DownMbps, &ms.CPU
		// Each session's share of the CPU, by its share of the upload (estimated).
		for i := range v.Sessions {
			if v.Total.Estimated > 0 {
				c := round2(ms.CPU * v.Sessions[i].Mbps / v.Total.Estimated)
				v.Sessions[i].CPU = &c
			}
		}
	}
	v.Total.UpMbps, v.Total.DownMbps = round2(v.Total.UpMbps), round2(v.Total.DownMbps)

	since := s.now().Add(-r.span)
	rows, _ := s.pool.Query(ctx, `SELECT to_timestamp(floor(extract(epoch FROM at) / $2) * $2), avg(up_mbps)::float8, avg(down_mbps)::float8, avg(cpu)::float8,
			max(participants), sum(reconnects)::int, bool_or(measured)
		FROM tutoring.metrics WHERE session_id IS NULL AND at >= $1 GROUP BY 1 ORDER BY 1`, since, r.step)
	if v.Points, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (HistoryPoint, error) {
		var hp HistoryPoint
		err := row.Scan(&hp.At, &hp.UpMbps, &hp.DownMbps, &hp.CPU, &hp.Participants, &hp.Reconnects, &hp.Measured)
		hp.UpMbps, hp.DownMbps = round2(hp.UpMbps), round2(hp.DownMbps)
		if hp.CPU != nil {
			c := round2(*hp.CPU)
			hp.CPU = &c
		}
		return hp, err
	}); err != nil {
		return v, err
	}
	rows, _ = s.pool.Query(ctx, `SELECT s.id, s.title, s.teacher_name, min(m.at), max(m.at), max(m.up_mbps)::float8, avg(m.up_mbps)::float8, max(m.down_mbps)::float8,
			max(m.participants), max(m.tracks), sum(m.reconnects)::int
		FROM tutoring.metrics m JOIN tutoring.sessions s ON s.id = m.session_id
		WHERE m.at >= $1 GROUP BY s.id ORDER BY max(m.at) DESC LIMIT 100`, since)
	if v.History, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SessionHistory, error) {
		var h SessionHistory
		err := row.Scan(&h.ID, &h.Title, &h.TeacherName, &h.From, &h.To, &h.PeakUpMbps, &h.AvgUpMbps, &h.PeakDownMbps, &h.PeakParticipants, &h.PeakTracks, &h.Reconnects)
		h.PeakUpMbps, h.AvgUpMbps, h.PeakDownMbps = round2(h.PeakUpMbps), round2(h.AvgUpMbps), round2(h.PeakDownMbps)
		return h, err
	}); err != nil {
		return v, err
	}
	if v.Points == nil {
		v.Points = []HistoryPoint{}
	}
	if v.History == nil {
		v.History = []SessionHistory{}
	}
	return v, nil
}

// liveUses is each live session's use now, busiest first.
func (s *Service) liveUses(ctx context.Context) ([]LiveUse, error) {
	rows, _ := s.pool.Query(ctx, `SELECT `+sessionCols+` FROM tutoring.sessions WHERE status='live'`)
	live, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) { return scanSession(r) })
	if err != nil {
		return nil, err
	}
	out := []LiveUse{}
	for _, sess := range live {
		l := s.loadOf(sess.ID)
		out = append(out, LiveUse{ID: sess.ID, Title: sess.Title, TeacherName: sess.TeacherName, Mbps: l.Mbps, Viewers: l.Viewers, AutoOff: sess.AutoOff,
			DownMbps: l.DownMbps, Tracks: l.Tracks, Participants: l.Participants, Reconnects: l.Reconnects})
	}
	slices.SortFunc(out, func(a, c LiveUse) int { return cmp.Compare(c.Mbps, a.Mbps) })
	return out, nil
}

// MediaReconnected counts the page joining the video again (TS-NFR-41).
func (s *Service) MediaReconnected(ctx context.Context, p authn.Person, id string) error {
	if _, err := s.participant(ctx, id, p.ID); err != nil {
		return err
	}
	s.hub.reconnected(id)
	return nil
}
