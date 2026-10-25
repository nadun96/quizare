package tutor_test

import (
	"context"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// Monitoring (D-82, TS-NFR-41): measured figures from LiveKit's counters,
// each session's estimates, reconnects, and 7 days of history.

func TestMonitoring(t *testing.T) {
	f := setup(t, "auto")
	ctx := context.Background()
	admin := f.e.NewPerson("admin", "Admin")
	var v tutor.MonitorView
	f.teacher.Call("GET", "/api/admin/monitor", nil, 403, nil)
	admin.Call("GET", "/api/admin/monitor?range=month", nil, 422, nil)
	admin.Call("GET", "/api/admin/monitor", nil, 200, &v)
	if v.Range != "24h" || v.Step != 300 || !v.Configured || len(v.Points) != 0 || len(v.History) != 0 || len(v.Sessions) != 0 || v.Total.Measured != nil {
		t.Fatalf("empty monitor = %+v", v)
	}

	ann := f.student("Ann")
	f.join(f.teacher, 200)
	f.join(ann, 200)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	f.teacher.Dial(f.sess.ID).Must("state")
	as := ann.Dial(f.sess.ID)
	as.Must("state")
	f.e.LiveKit.Publish(f.sess.ID, f.teacher.ID, media.Track{Sid: "S1", Source: "SCREEN_SHARE"}, media.Track{Sid: "M1", Source: "MICROPHONE"})

	// Ann comes back twice: a new socket, and her page joining the video again.
	as.Conn.CloseNow()
	ann.Dial(f.sess.ID).Must("state")
	ann.Call("POST", f.path("/media-reconnected"), nil, 204, nil)
	stranger := f.e.NewPerson("student", "Zed")
	stranger.Call("POST", f.path("/media-reconnected"), nil, 404, nil)

	// Two readings of LiveKit's counters make a measurement.
	check := func() {
		t.Helper()
		if err := f.e.Service.CheckLoad(ctx); err != nil {
			t.Fatal(err)
		}
	}
	check()
	admin.Call("GET", "/api/admin/monitor", nil, 200, &v)
	if v.Total.Measured != nil || v.Total.UpMbps != 1.24 || v.Total.DownMbps != 1.24 || v.Total.Participants != 2 || v.Total.Reconnects != 2 {
		t.Fatalf("estimated only = %+v", v.Total)
	}
	time.Sleep(600 * time.Millisecond)
	f.e.Metrics.Add(250_000, 1_000_000, 0.3)
	check()
	admin.Call("GET", "/api/admin/monitor", nil, 200, &v)
	m := v.Total.Measured
	if m == nil || m.UpMbps <= 0 || m.DownMbps <= 0 || m.UpMbps < 3*m.DownMbps || m.CPU <= 0 || v.Total.UpMbps != m.UpMbps || v.Total.CPU == nil || v.Total.Estimated != 1.24 {
		t.Fatalf("measured = %+v / %+v", v.Total, m)
	}
	if len(v.Sessions) != 1 {
		t.Fatalf("sessions = %+v", v.Sessions)
	}
	s := v.Sessions[0]
	if s.Tracks != 2 || s.Participants != 2 || s.Reconnects != 2 || s.DownMbps != 1.24 || s.Mbps != 1.24 || s.CPU == nil || *s.CPU != m.CPU {
		t.Fatalf("live session = %+v", s)
	}

	// The minute's rows; rows past 7 days go.
	if _, err := f.e.Pool.Exec(ctx, `INSERT INTO tutoring.metrics (at, up_mbps, down_mbps, tracks, participants, reconnects) VALUES (now() - interval '8 days', 1, 1, 0, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := f.e.Service.FlushMetrics(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var old, rows int
	if err := f.e.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE at < now() - interval '7 days'), count(*) FROM tutoring.metrics`).Scan(&old, &rows); err != nil || old != 0 || rows != 2 {
		t.Fatalf("rows = %d, old %d, %v", rows, old, err)
	}
	admin.Call("GET", "/api/admin/monitor?range=7d", nil, 200, &v)
	if v.Step != 1800 || len(v.Points) != 1 || !v.Points[0].Measured || v.Points[0].CPU == nil || v.Points[0].Participants != 2 || v.Points[0].Reconnects != 2 {
		t.Fatalf("history = %+v", v.Points)
	}
	if len(v.History) != 1 || v.History[0].ID != f.sess.ID || v.History[0].PeakUpMbps != 1.24 || v.History[0].PeakParticipants != 2 || v.History[0].PeakTracks != 2 || v.History[0].Reconnects != 2 {
		t.Fatalf("sessions' history = %+v", v.History)
	}

	// LiveKit's metrics unreachable: back to estimates, nothing fails.
	f.e.Metrics.Down = true
	check()
	v = tutor.MonitorView{} // decoding into a used struct keeps fields the reply leaves out
	admin.Call("GET", "/api/admin/monitor", nil, 200, &v)
	if v.Total.Measured != nil || v.Total.UpMbps != 1.24 {
		t.Fatalf("metrics down = %+v", v.Total)
	}
}
