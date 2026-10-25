package tutor_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/platform"
	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// Scheduling (D-84, TS-FR-85 to TS-FR-89): series, joining early, moving and
// cancelling with notices, reminders, calendars and feeds.

func waitNotices(t *testing.T, e *testenv.Env, n int) []platform.Notice {
	t.Helper()
	var got []platform.Notice
	for range 60 {
		got = append(got, e.Platform.TakeNotices()...)
		if len(got) >= n {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("notices = %+v, want %d", got, n)
	return nil
}

func TestScheduling(t *testing.T) {
	f := setup(t, "auto")
	ann, zed := f.student("Ann"), f.e.NewPerson("student", "Zed")
	const zone = "Asia/Colombo"
	loc, _ := time.LoadLocation(zone)
	// Next week's Monday, 15:30 in Colombo.
	now := time.Now().In(loc)
	monday := now.AddDate(0, 0, 7+int(time.Monday-now.Weekday()))
	start := monday.Format("2006-01-02") + "T15:30"

	// Checked: the rule, the zone, the time, who schedules.
	in := tutor.ScheduleInput{ClassroomID: f.classroom, Title: "Waves", Start: start, TimeZone: zone, DurationMin: 45,
		Repeat: tutor.Repeat{Freq: "weekly", Days: []int{1, 3}, Count: 5}}
	var bad apiError
	for name, change := range map[string]func(*tutor.ScheduleInput){
		"no end":     func(i *tutor.ScheduleInput) { i.Repeat = tutor.Repeat{Freq: "weekly"} },
		"bad zone":   func(i *tutor.ScheduleInput) { i.TimeZone = "Mars/Olympus" },
		"past":       func(i *tutor.ScheduleInput) { i.Start = "2020-01-01T10:00" },
		"too short":  func(i *tutor.ScheduleInput) { i.DurationMin = 2 },
		"reminders":  func(i *tutor.ScheduleInput) { r := []int{15, 15}; i.Reminders = &r },
		"open early": func(i *tutor.ScheduleInput) { o := 500; i.OpenEarlyMin = &o },
	} {
		x := in
		change(&x)
		code, raw := f.teacher.Do("POST", "/api/schedule", x)
		if code != 422 {
			t.Fatalf("%s: %d %s", name, code, raw)
		}
	}
	ann.Call("POST", "/api/schedule", in, 403, &bad)
	var made tutor.Scheduled
	f.teacher.Call("POST", "/api/schedule", in, 201, &made)
	if made.SeriesID == nil || len(made.Sessions) != 5 {
		t.Fatalf("scheduled = %+v", made)
	}
	for i, s := range made.Sessions {
		local := s.ScheduledAt.In(loc)
		if local.Format("15:04") != "15:30" || (local.Weekday() != time.Monday && local.Weekday() != time.Wednesday) ||
			*s.SeriesID != *made.SeriesID || s.DurationMin != 45 || s.TimeZone != zone || s.OpenEarlyMin != 10 || len(s.Reminders) != 2 || s.Status != "open" {
			t.Fatalf("occurrence %d = %+v", i, s)
		}
	}
	first, third := made.Sessions[0], made.Sessions[2]
	if first.JoinCode == third.JoinCode {
		t.Fatal("occurrences share a join code")
	}

	// Calendars (TS-FR-86): the teacher's and an enrolled student's; nobody else's.
	cal := func(p *testenv.Person) []tutor.CalendarEntry {
		var out struct{ Sessions []tutor.CalendarEntry }
		from, to := time.Now().UTC().Format(time.RFC3339), time.Now().AddDate(0, 0, 40).UTC().Format(time.RFC3339)
		p.Call("GET", "/api/calendar?from="+from+"&to="+to, nil, 200, &out)
		return out.Sessions
	}
	if got := cal(f.teacher); len(got) != 5 || got[0].Role != "teacher" || got[0].OpensAt == nil || !got[0].OpensAt.Equal(first.ScheduledAt.Add(-10*time.Minute)) {
		t.Fatalf("teacher's calendar = %+v", got)
	}
	if got := cal(ann); len(got) != 5 || got[0].Role != "student" || got[0].JoinCode != first.JoinCode {
		t.Fatalf("Ann's calendar = %+v", got)
	}
	if got := cal(zed); len(got) != 0 {
		t.Fatalf("a stranger's calendar = %+v", got)
	}
	f.teacher.Call("GET", "/api/calendar?from=2027-01-01T00:00:00Z&to=2027-06-01T00:00:00Z", nil, 422, nil)

	// Joining early (TS-FR-89): students wait until it opens; teachers needn't.
	code, raw := ann.Do("POST", "/api/join/"+first.JoinCode, nil)
	if code != 409 || !strings.Contains(string(raw), "not_open_yet") || !strings.Contains(string(raw), "15:20") {
		t.Fatalf("Ann joining a week early: %d %s", code, raw)
	}
	f.teacher.Call("POST", "/api/join/"+first.JoinCode, nil, 200, nil)

	// Moving one (TS-FR-85): only it moves, everyone is told, and it opens.
	soon := time.Now().Add(20 * time.Minute).In(loc).Format("2006-01-02T15:04")
	var moved tutor.Session
	ann.Call("PUT", "/api/sessions/"+first.ID+"/timing", tutor.TimingInput{Start: &soon}, 404, nil)
	f.teacher.Call("PUT", "/api/sessions/"+first.ID+"/timing", tutor.TimingInput{Start: &soon}, 200, &moved)
	if moved.ScheduledAt.In(loc).Format("2006-01-02T15:04") != soon || moved.SeriesID == nil {
		t.Fatalf("moved = %+v", moved)
	}
	n := waitNotices(t, f.e, 1)
	if len(n) != 1 || n[0].Subject != "Tutoring moved: Waves" || !n[0].Students || n[0].ClassroomID != f.classroom ||
		!strings.Contains(n[0].Body, "now ") || !strings.Contains(n[0].Body, f.e.Origin+"/tutor/"+first.JoinCode) {
		t.Fatalf("move notice = %+v", n)
	}
	early := 25
	f.teacher.Call("PUT", "/api/sessions/"+first.ID+"/timing", tutor.TimingInput{OpenEarlyMin: &early}, 200, nil)
	ann.Call("POST", "/api/join/"+first.JoinCode, nil, 200, nil) // opens 25 minutes before: now
	f.e.Platform.TakeNotices()                                   // (no notice: the time didn't change)

	// Reminders (TS-FR-87): the 24-hour one had passed when it was moved; the
	// 15-minute one falls due 5 minutes from now, once.
	if err := f.e.Service.SendReminders(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := f.e.Platform.TakeNotices(); len(got) != 0 {
		t.Fatalf("early reminders = %+v", got)
	}
	f.e.Service.SetClock(func() time.Time { return time.Now().Add(6 * time.Minute) })
	defer f.e.Service.SetClock(time.Now)
	for range 2 {
		if err := f.e.Service.SendReminders(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	n = f.e.Platform.TakeNotices()
	if len(n) != 1 || !strings.HasPrefix(n[0].Subject, "Tutoring in 1") || !strings.Contains(n[0].Subject, "minutes: Waves") || !strings.Contains(n[0].Body, "You can join from 25 minutes before") {
		t.Fatalf("reminder = %+v", n)
	}
	f.e.Service.SetClock(time.Now)
	// Turned off for one session.
	none := []int{}
	f.teacher.Call("PUT", "/api/sessions/"+third.ID+"/timing", tutor.TimingInput{Reminders: &none}, 200, &moved)
	if len(moved.Reminders) != 0 {
		t.Fatalf("reminders off = %+v", moved.Reminders)
	}

	// Cancelling (TS-FR-85): one, then the rest of the series from the third.
	second := made.Sessions[1]
	var res struct{ Cancelled int }
	f.teacher.Call("POST", "/api/sessions/"+second.ID+"/cancel", nil, 200, &res)
	if res.Cancelled != 1 {
		t.Fatalf("cancelled = %d", res.Cancelled)
	}
	if n := waitNotices(t, f.e, 1); n[0].Subject != "Tutoring cancelled: Waves" || strings.Count(n[0].Body, "(Asia/Colombo)") != 1 {
		t.Fatalf("cancel notice = %+v", n)
	}
	code, raw = ann.Do("POST", "/api/join/"+second.JoinCode, nil)
	if code != 410 || !strings.Contains(string(raw), "session_cancelled") {
		t.Fatalf("joining a cancelled one: %d %s", code, raw)
	}
	f.teacher.Call("POST", "/api/sessions/"+second.ID+"/cancel", nil, 409, nil)
	f.teacher.Call("PUT", "/api/sessions/"+second.ID+"/timing", tutor.TimingInput{Start: &soon}, 409, nil)
	f.teacher.Call("POST", "/api/sessions/"+third.ID+"/cancel", map[string]bool{"series": true}, 200, &res)
	if res.Cancelled != 3 {
		t.Fatalf("rest of the series = %d", res.Cancelled)
	}
	if n := waitNotices(t, f.e, 1); strings.Count(n[0].Body, "(Asia/Colombo)") != 3 {
		t.Fatalf("series cancel notice = %+v", n)
	}

	// Calendar files (TS-FR-88): the series, cancelled ones marked.
	code, raw = ann.Do("GET", "/api/sessions/"+third.ID+"/calendar.ics", nil)
	ics := string(raw)
	if code != 200 || strings.Count(ics, "BEGIN:VEVENT") != 5 || strings.Count(ics, "STATUS:CANCELLED") != 4 ||
		!strings.Contains(ics, "SUMMARY:Waves") || !strings.HasPrefix(ics, "BEGIN:VCALENDAR\r\n") {
		t.Fatalf("ics: %d\n%s", code, ics)
	}
	if code, _ := zed.Do("GET", "/api/sessions/"+third.ID+"/calendar.ics", nil); code != 404 {
		t.Fatalf("a stranger's ics: %d", code)
	}

	// The private feed: made, read without a login, revoked.
	var feed struct {
		Active bool
		Path   string
	}
	ann.Call("GET", "/api/my/feed", nil, 200, &feed)
	if feed.Active {
		t.Fatal("a feed before making one")
	}
	ann.Call("POST", "/api/my/feed", nil, 201, &feed)
	get := func(path string) (int, string) {
		resp, err := http.Get(f.e.Server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	code, body := get(feed.Path)
	if code != 200 || strings.Count(body, "BEGIN:VEVENT") != 5 {
		t.Fatalf("feed: %d %s", code, body)
	}
	ann.Call("GET", "/api/my/feed", nil, 200, &feed)
	if !feed.Active {
		t.Fatal("feed not active")
	}
	if code, _ := get("/feeds/not-a-token.ics"); code != 404 {
		t.Fatalf("a made-up feed: %d", code)
	}
	ann.Call("DELETE", "/api/my/feed", nil, 204, nil)
	if code, _ := get(feed.Path); code != 404 {
		t.Fatalf("revoked feed: %d", code)
	}
}
