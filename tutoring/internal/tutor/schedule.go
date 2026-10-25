package tutor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // time zones on every system, Windows and small containers included

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/platform"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Scheduling (D-84, TS-FR-85 to TS-FR-89). A schedule is entered as a local
// date and time in a time zone, so "Tuesdays at 15:30" stays 15:30 across a
// change of daylight saving time. A repeating schedule makes a series, and
// every occurrence is an ordinary session that can be moved or cancelled
// alone. A scheduled session opens for joining a while before it starts
// (students wait in the waiting room); it never starts by itself.
// Reminders and notices of changes go out by email through the platform,
// which knows everyone's address; the platform's pages show what's next.

const (
	maxOccurrences = 100
	maxSpan        = 366 * 24 * time.Hour
	localLayout    = "2006-01-02T15:04"
)

// Repeat is how a schedule repeats: Freq "" (once), "daily" or "weekly",
// every Interval days or weeks, on Days (weekly; 0 is Sunday), until a date
// (inclusive) or Count times.
type Repeat struct {
	Freq     string `json:"freq"`
	Interval int    `json:"interval,omitempty"`
	Days     []int  `json:"days,omitempty"`
	Until    string `json:"until,omitempty"` // YYYY-MM-DD
	Count    int    `json:"count,omitempty"`
}

// ScheduleInput is a new scheduled session or series.
type ScheduleInput struct {
	ClassroomID  string `json:"classroom_id"`
	Title        string `json:"title"`
	AdmitMode    string `json:"admit_mode"`
	Plan         *Plan  `json:"plan,omitempty"`
	Start        string `json:"start"` // local, 2006-01-02T15:04
	TimeZone     string `json:"time_zone"`
	DurationMin  int    `json:"duration_min"`
	OpenEarlyMin *int   `json:"open_early_min,omitempty"` // default 10
	Reminders    *[]int `json:"reminders,omitempty"`      // minutes before; default 24 h and 15 min
	Repeat       Repeat `json:"repeat"`
}

var defaultReminders = []int{1440, 15}

func checkTiming(f map[string]string, duration, openEarly int, reminders []int) {
	if duration < 5 || duration > 600 {
		f["duration_min"] = "5 minutes to 10 hours"
	}
	if openEarly < 0 || openEarly > 120 {
		f["open_early_min"] = "0 to 120 minutes"
	}
	seen := map[int]bool{}
	for _, m := range reminders {
		if m < 1 || m > 7*1440 || seen[m] {
			f["reminders"] = "up to 4 different times, from 1 minute to 7 days before"
		}
		seen[m] = true
	}
	if len(reminders) > 4 {
		f["reminders"] = "up to 4 different times, from 1 minute to 7 days before"
	}
}

// occurrences lists a schedule's start times.
func occurrences(start time.Time, r Repeat) ([]time.Time, error) {
	loc := start.Location()
	f := map[string]string{}
	if r.Interval == 0 {
		r.Interval = 1
	}
	if r.Interval < 1 || r.Interval > 12 {
		f["repeat.interval"] = "every 1 to 12"
	}
	var until time.Time
	if r.Until != "" {
		u, err := time.ParseInLocation("2006-01-02", r.Until, loc)
		if err != nil || u.Before(time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)) {
			f["repeat.until"] = "a date on or after the first"
		}
		until = u.AddDate(0, 0, 1) // the whole day
	}
	if r.Count < 0 || r.Count > maxOccurrences {
		f["repeat.count"] = fmt.Sprintf("1 to %d times", maxOccurrences)
	}
	switch r.Freq {
	case "":
		if len(f) > 0 {
			return nil, web.Invalid(f)
		}
		return []time.Time{start}, nil
	case "daily", "weekly":
		if r.Until == "" && r.Count == 0 {
			f["repeat.until"] = "say until when, or how many times"
		}
	default:
		f["repeat.freq"] = "once, daily or weekly"
	}
	days := slices.Clone(r.Days)
	if r.Freq == "weekly" {
		if len(days) == 0 {
			days = []int{int(start.Weekday())}
		}
		for _, d := range days {
			if d < 0 || d > 6 {
				f["repeat.days"] = "days of the week, 0 (Sunday) to 6"
			}
		}
		slices.Sort(days)
		days = slices.Compact(days)
	}
	if len(f) > 0 {
		return nil, web.Invalid(f)
	}
	at := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, start.Hour(), start.Minute(), 0, 0, loc)
	}
	limit := start.Add(maxSpan)
	var out []time.Time
	done := func(t time.Time) bool {
		return (!until.IsZero() && !t.Before(until)) || (r.Count > 0 && len(out) >= r.Count) || len(out) >= maxOccurrences || t.After(limit)
	}
	if r.Freq == "daily" {
		for i := 0; ; i++ {
			t := at(start.Year(), start.Month(), start.Day()+i*r.Interval)
			if done(t) {
				break
			}
			out = append(out, t)
		}
		return out, nil
	}
	// Weekly: the weeks (Sunday first) of the start, every Interval weeks.
	week := start.AddDate(0, 0, -int(start.Weekday()))
	for w := 0; ; w++ {
		base := week.AddDate(0, 0, 7*w*r.Interval)
		stop := false
		for _, d := range days {
			day := base.AddDate(0, 0, d)
			t := at(day.Year(), day.Month(), day.Day())
			if t.Before(start) {
				continue
			}
			if done(t) {
				stop = true
				break
			}
			out = append(out, t)
		}
		if stop || base.After(limit) {
			break
		}
	}
	return out, nil
}

func loadZone(name string) (*time.Location, error) {
	if name == "" {
		return nil, web.Invalid(map[string]string{"time_zone": "choose a time zone"})
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, web.Invalid(map[string]string{"time_zone": "not a known time zone"})
	}
	return loc, nil
}

// Scheduled is what scheduling made: the series (when it repeats) and its sessions.
type Scheduled struct {
	SeriesID *string   `json:"series_id,omitempty"`
	Sessions []Session `json:"sessions"`
}

// Schedule makes a scheduled session, or a series of them (TS-FR-85).
func (s *Service) Schedule(ctx context.Context, p authn.Person, in ScheduleInput) (Scheduled, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.AdmitMode == "" {
		in.AdmitMode = "auto"
	}
	openEarly := 10
	if in.OpenEarlyMin != nil {
		openEarly = *in.OpenEarlyMin
	}
	reminders := defaultReminders
	if in.Reminders != nil {
		reminders = *in.Reminders
	}
	f := map[string]string{}
	if n := len([]rune(in.Title)); n < 1 || n > 200 {
		f["title"] = "give the session a title of up to 200 characters"
	}
	if in.AdmitMode != "auto" && in.AdmitMode != "manual" {
		f["admit_mode"] = "admit automatically or manually"
	}
	checkTiming(f, in.DurationMin, openEarly, reminders)
	loc, err := loadZone(in.TimeZone)
	if err != nil {
		return Scheduled{}, err
	}
	start, err := time.ParseInLocation(localLayout, in.Start, loc)
	if err != nil {
		f["start"] = "a date and time"
	} else if start.Before(s.now().Add(-5 * time.Minute)) {
		f["start"] = "a time that hasn't passed"
	}
	if len(f) > 0 {
		return Scheduled{}, web.Invalid(f)
	}
	times, err := occurrences(start, in.Repeat)
	if err != nil {
		return Scheduled{}, err
	}
	plan := DefaultPlan
	if in.Plan != nil {
		b, err := s.budget(ctx)
		if err != nil {
			return Scheduled{}, err
		}
		if err := in.Plan.clean(b); err != nil {
			return Scheduled{}, err
		}
		plan = *in.Plan
	}
	planRaw, _ := json.Marshal(plan)
	a, err := s.platform.Access(ctx, in.ClassroomID, p.ID)
	if err != nil {
		return Scheduled{}, err
	}
	if a.Role != "teacher" {
		return Scheduled{}, web.NewError(http.StatusForbidden, "forbidden", "only the classroom's teacher can schedule its sessions")
	}
	if a.Archived {
		return Scheduled{}, web.Conflict("this classroom is archived")
	}
	var out Scheduled
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if in.Repeat.Freq != "" {
			rule, _ := json.Marshal(in.Repeat)
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO tutoring.series (teacher_id, classroom_id, title, time_zone, rule) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
				p.ID, a.ClassroomID, in.Title, loc.String(), rule).Scan(&id); err != nil {
				return err
			}
			out.SeriesID = &id
		}
		for _, t := range times {
			sess, err := s.insertScheduled(ctx, tx, p, a, in, planRaw, out.SeriesID, t, loc, openEarly, reminders)
			if err != nil {
				return err
			}
			out.Sessions = append(out.Sessions, sess)
		}
		return nil
	})
	return out, err
}

func (s *Service) insertScheduled(ctx context.Context, tx pgx.Tx, p authn.Person, a platform.Access, in ScheduleInput, planRaw []byte,
	seriesID *string, at time.Time, loc *time.Location, openEarly int, reminders []int) (Session, error) {
	for range 5 {
		sess, err := scanSession(tx.QueryRow(ctx, `INSERT INTO tutoring.sessions (teacher_id, teacher_name, classroom_id, classroom_name, title, join_code, admit_mode,
				scheduled_at, plan, series_id, duration_min, time_zone, open_early_min, reminders)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT (join_code) DO NOTHING RETURNING `+sessionCols,
			p.ID, p.Name, a.ClassroomID, a.ClassroomName, in.Title, newCode(), in.AdmitMode, at.UTC(), planRaw, seriesID, in.DurationMin, loc.String(), openEarly, reminders))
		if errors.Is(err, web.ErrNotFound) {
			continue // the join code was taken
		}
		if err != nil {
			return sess, err
		}
		return sess, s.skipPastReminders(ctx, tx, sess)
	}
	return Session{}, errors.New("could not find a free join code")
}

// skipPastReminders marks reminders whose time has already passed as sent:
// a session scheduled for in an hour gets no "in 24 hours" email.
func (s *Service) skipPastReminders(ctx context.Context, q interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}, sess Session) error {
	if sess.ScheduledAt == nil {
		return nil
	}
	_, err := q.Exec(ctx, `INSERT INTO tutoring.reminders_sent (session_id, minutes, for_at)
		SELECT $1, m, $2::timestamptz FROM unnest($3::int[]) m WHERE $2::timestamptz - make_interval(mins => m) <= $4 ON CONFLICT DO NOTHING`,
		sess.ID, *sess.ScheduledAt, sess.Reminders, s.now())
	return err
}

// ---------- moving and cancelling ----------

// TimingInput moves a session or changes its timing (the lead teacher's);
// fields left out stay as they are.
type TimingInput struct {
	Start        *string `json:"start,omitempty"` // local, in TimeZone
	TimeZone     *string `json:"time_zone,omitempty"`
	DurationMin  *int    `json:"duration_min,omitempty"`
	OpenEarlyMin *int    `json:"open_early_min,omitempty"`
	Reminders    *[]int  `json:"reminders,omitempty"`
}

// SetTiming moves one session (TS-FR-85) or changes its reminders
// (TS-FR-87); a new time is sent to everyone concerned.
func (s *Service) SetTiming(ctx context.Context, p authn.Person, id string, in TimingInput) (Session, error) {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return sess, err
	}
	if sess.Status != "open" {
		return sess, web.Conflict("only a session that hasn't started can be moved")
	}
	zone, duration, openEarly, reminders := sess.TimeZone, sess.DurationMin, sess.OpenEarlyMin, ints(sess.Reminders)
	if in.TimeZone != nil {
		zone = *in.TimeZone
	}
	if in.DurationMin != nil {
		duration = *in.DurationMin
	}
	if in.OpenEarlyMin != nil {
		openEarly = *in.OpenEarlyMin
	}
	if in.Reminders != nil {
		reminders = *in.Reminders
	}
	f := map[string]string{}
	checkTiming(f, duration, openEarly, reminders)
	loc, err := loadZone(zone)
	if err != nil {
		return sess, err
	}
	at := sess.ScheduledAt
	if in.Start != nil {
		t, err := time.ParseInLocation(localLayout, *in.Start, loc)
		switch {
		case err != nil:
			f["start"] = "a date and time"
		case t.Before(s.now().Add(-5 * time.Minute)):
			f["start"] = "a time that hasn't passed"
		default:
			t = t.UTC()
			at = &t
		}
	}
	if len(f) > 0 {
		return sess, web.Invalid(f)
	}
	moved := at != nil && (sess.ScheduledAt == nil || !at.Equal(*sess.ScheduledAt))
	updated, err := scanSession(s.pool.QueryRow(ctx, `UPDATE tutoring.sessions SET scheduled_at=$2, time_zone=$3, duration_min=$4, open_early_min=$5, reminders=$6
		WHERE id=$1 AND status='open' RETURNING `+sessionCols, id, at, loc.String(), duration, openEarly, reminders))
	if err != nil {
		return sess, err
	}
	if err := s.skipPastReminders(ctx, s.pool, updated); err != nil {
		return updated, err
	}
	s.logAction(ctx, id, p.ID, "timing_changed", nil, in)
	if moved && sess.ScheduledAt != nil {
		s.notifyLater(updated, "Tutoring moved: "+updated.Title,
			fmt.Sprintf("%s\nwas %s\nnow %s\n\n%s", updated.Title, when(*sess.ScheduledAt, loc), when(*updated.ScheduledAt, loc), s.details(updated)))
	}
	s.hub.changed(id)
	return updated, nil
}

func ints(xs []int32) []int {
	out := make([]int, len(xs))
	for i, x := range xs {
		out[i] = int(x)
	}
	return out
}

// Cancel cancels a session that hasn't started, or, with series, it and
// the rest of its series. Everyone concerned is told; anyone waiting leaves.
func (s *Service) Cancel(ctx context.Context, p authn.Person, id string, series bool) (int, error) {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return 0, err
	}
	if sess.Status != "open" {
		return 0, web.Conflict("only a session that hasn't started can be cancelled")
	}
	q := `UPDATE tutoring.sessions SET status='cancelled', cancelled_at=$2 WHERE id=$1 AND status='open' RETURNING ` + sessionCols
	args := []any{id, s.now()}
	if series && sess.SeriesID != nil {
		q = `UPDATE tutoring.sessions SET status='cancelled', cancelled_at=$2 WHERE series_id=$3 AND status='open'
			AND (id=$1 OR scheduled_at >= coalesce($4, scheduled_at)) RETURNING ` + sessionCols
		args = append(args, *sess.SeriesID, sess.ScheduledAt)
	}
	rows, _ := s.pool.Query(ctx, q, args...)
	cancelled, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) { return scanSession(r) })
	if err != nil {
		return 0, err
	}
	var dates []string
	for _, c := range cancelled {
		s.logAction(ctx, c.ID, p.ID, "cancelled", nil, nil)
		s.hub.end(c.ID)
		if c.ScheduledAt != nil {
			loc, _ := time.LoadLocation(c.TimeZone)
			dates = append(dates, when(*c.ScheduledAt, loc))
		}
	}
	if len(dates) > 0 {
		slices.Sort(dates)
		s.notifyLater(sess, "Tutoring cancelled: "+sess.Title,
			fmt.Sprintf("%s is cancelled:\n%s\n\n%s, with %s", sess.Title, strings.Join(dates, "\n"), sess.ClassroomName, sess.TeacherName))
	}
	return len(cancelled), nil
}

// ---------- joining early (TS-FR-89) ----------

// opensAt is when a scheduled session opens for students to join.
func opensAt(sess Session) *time.Time {
	if sess.ScheduledAt == nil || sess.Status != "open" {
		return nil
	}
	t := sess.ScheduledAt.Add(-time.Duration(sess.OpenEarlyMin) * time.Minute)
	return &t
}

func (s *Service) notYetOpen(sess Session) error {
	t := opensAt(sess)
	if t == nil || !s.now().Before(*t) {
		return nil
	}
	loc, _ := time.LoadLocation(sess.TimeZone)
	return web.NewError(http.StatusConflict, "not_open_yet", fmt.Sprintf("this session opens at %s, %d minutes before it starts", when(*t, loc), sess.OpenEarlyMin))
}

// ---------- notices ----------

// SetClock replaces the service's clock (tests: reminders falling due).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetLinks gives the platform's address, for the links in emails.
func (s *Service) SetLinks(platformOrigin string) { s.origin = strings.TrimRight(platformOrigin, "/") }

// when is a time as people read it: "Mon 2 Nov 2026, 15:30 (Asia/Colombo)".
func when(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format("Mon 2 Jan 2006, 15:04") + " (" + loc.String() + ")"
}

func (s *Service) details(sess Session) string {
	return fmt.Sprintf("%s, with %s\nJoin: %s/tutor/%s", sess.ClassroomName, sess.TeacherName, s.origin, sess.JoinCode)
}

// notifyLater emails a session's people in the background: the teacher,
// co-teachers and the classroom's students.
func (s *Service) notifyLater(sess Session, subject, body string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.notify(ctx, sess, subject, body); err != nil {
			s.log.Warn("tutoring notice", "session", sess.ID, "err", err)
		}
	}()
}

func (s *Service) notify(ctx context.Context, sess Session, subject, body string) error {
	rows, _ := s.pool.Query(ctx, `SELECT user_id::text FROM tutoring.coteachers WHERE session_id=$1`, sess.ID)
	co, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	return s.platform.Notify(ctx, platform.Notice{ClassroomID: sess.ClassroomID, Students: true, UserIDs: co, Subject: subject, Body: body})
}

func inWords(minutes int) string {
	switch {
	case minutes%1440 == 0:
		if minutes == 1440 {
			return "24 hours"
		}
		return fmt.Sprintf("%d days", minutes/1440)
	case minutes%60 == 0:
		if minutes == 60 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", minutes/60)
	case minutes == 1:
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}

// SendReminders emails the reminders that are due (TS-FR-87); the service
// runs it every 30 seconds.
func (s *Service) SendReminders(ctx context.Context) error {
	now := s.now()
	rows, _ := s.pool.Query(ctx, `SELECT `+prefixed("s", sessionCols)+`, m FROM tutoring.sessions s, unnest(s.reminders) m
		WHERE s.status='open' AND s.scheduled_at > $1 AND s.scheduled_at - make_interval(mins => m) <= $1
		AND NOT EXISTS (SELECT 1 FROM tutoring.reminders_sent r WHERE r.session_id=s.id AND r.minutes=m AND r.for_at=s.scheduled_at)`, now)
	type due struct {
		sess    Session
		minutes int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (due, error) {
		var m int32
		sess, err := scanSessionWith(r, &m)
		return due{sess, int(m)}, err
	})
	if err != nil {
		return err
	}
	for _, d := range list {
		// Claim it first, so two copies of the service never both send it.
		tag, err := s.pool.Exec(ctx, `INSERT INTO tutoring.reminders_sent (session_id, minutes, for_at) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
			d.sess.ID, d.minutes, *d.sess.ScheduledAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		loc, _ := time.LoadLocation(d.sess.TimeZone)
		left := int(d.sess.ScheduledAt.Sub(now).Round(time.Minute).Minutes())
		body := fmt.Sprintf("%s starts %s.\nYou can join from %d minutes before.\n\n%s", d.sess.Title, when(*d.sess.ScheduledAt, loc), d.sess.OpenEarlyMin, s.details(d.sess))
		if err := s.notify(ctx, d.sess, fmt.Sprintf("Tutoring in %s: %s", inWords(max(left, 1)), d.sess.Title), body); err != nil {
			// Not sent: try again at the next sweep.
			_, _ = s.pool.Exec(ctx, `DELETE FROM tutoring.reminders_sent WHERE session_id=$1 AND minutes=$2 AND for_at=$3`, d.sess.ID, d.minutes, *d.sess.ScheduledAt)
			s.log.Warn("tutoring reminder", "session", d.sess.ID, "err", err)
		}
	}
	return nil
}

// prefixed qualifies a column list with a table alias.
func prefixed(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		c = strings.TrimSpace(c)
		if c != "" {
			parts[i] = alias + "." + c
		}
	}
	return strings.Join(parts, ", ")
}

// ---------- calendars (TS-FR-86, TS-FR-88) ----------

// CalendarEntry is one session in someone's calendar.
type CalendarEntry struct {
	ID            string     `json:"id"`
	SeriesID      *string    `json:"series_id,omitempty"`
	Title         string     `json:"title"`
	ClassroomID   string     `json:"classroom_id"`
	ClassroomName string     `json:"classroom_name"`
	TeacherName   string     `json:"teacher_name"`
	JoinCode      string     `json:"join_code"`
	Status        string     `json:"status"`
	ScheduledAt   *time.Time `json:"scheduled_at,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	DurationMin   int        `json:"duration_min"`
	TimeZone      string     `json:"time_zone"`
	OpensAt       *time.Time `json:"opens_at,omitempty"`
	Role          string     `json:"role"` // teacher, coteacher or student
}

// calendar lists the sessions someone teaches, co-teaches or may join (a
// classroom they study in) that are scheduled between from and to, and any
// that are live now.
func (s *Service) calendar(ctx context.Context, userID string, from, to time.Time) ([]CalendarEntry, error) {
	studies := []string{}
	if cs, err := s.platform.Classrooms(ctx, userID); err == nil {
		for _, c := range cs {
			if c.Role == "student" {
				studies = append(studies, c.ID)
			}
		}
	} else {
		s.log.Warn("calendar classrooms", "err", err) // teachers still see their own
	}
	rows, _ := s.pool.Query(ctx, `SELECT `+sessionCols+`,
			CASE WHEN teacher_id=$1 THEN 'teacher' WHEN EXISTS (SELECT 1 FROM tutoring.coteachers c WHERE c.session_id=tutoring.sessions.id AND c.user_id=$1) THEN 'coteacher' ELSE 'student' END
		FROM tutoring.sessions
		WHERE (teacher_id=$1 OR id IN (SELECT session_id FROM tutoring.coteachers WHERE user_id=$1) OR classroom_id::text = ANY($2))
		AND ((scheduled_at >= $3 AND scheduled_at < $4) OR status='live')
		ORDER BY coalesce(scheduled_at, started_at), id LIMIT 500`, userID, studies, from, to)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CalendarEntry, error) {
		var role string
		sess, err := scanSessionWith(r, &role)
		return CalendarEntry{ID: sess.ID, SeriesID: sess.SeriesID, Title: sess.Title, ClassroomID: sess.ClassroomID, ClassroomName: sess.ClassroomName,
			TeacherName: sess.TeacherName, JoinCode: sess.JoinCode, Status: sess.Status, ScheduledAt: sess.ScheduledAt, StartedAt: sess.StartedAt,
			DurationMin: sess.DurationMin, TimeZone: sess.TimeZone, OpensAt: opensAt(sess), Role: role}, err
	})
}

// Calendar is someone's sessions between from and to (at most 100 days).
func (s *Service) Calendar(ctx context.Context, p authn.Person, from, to time.Time) ([]CalendarEntry, error) {
	if !to.After(from) || to.Sub(from) > 100*24*time.Hour {
		return nil, web.Invalid(map[string]string{"to": "after from, and at most 100 days later"})
	}
	list, err := s.calendar(ctx, p.ID, from, to)
	if list == nil {
		list = []CalendarEntry{}
	}
	return list, err
}

// ICS writes sessions as an iCalendar file (RFC 5545), one event each,
// cancelled ones marked so calendars drop them.
func (s *Service) ICS(name string, list []CalendarEntry) string {
	var b strings.Builder
	line := func(l string) { b.WriteString(fold(l)) }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Classroom Quiz//Tutoring//EN")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	line("X-WR-CALNAME:" + icsText(name))
	stamp := s.now().UTC().Format("20060102T150405Z")
	for _, e := range list {
		if e.ScheduledAt == nil {
			continue
		}
		link := s.origin + "/tutor/" + e.JoinCode
		status := "CONFIRMED"
		if e.Status == "cancelled" {
			status = "CANCELLED"
		}
		line("BEGIN:VEVENT")
		line("UID:" + e.ID + "@tutoring")
		line("DTSTAMP:" + stamp)
		line("DTSTART:" + e.ScheduledAt.UTC().Format("20060102T150405Z"))
		line("DTEND:" + e.ScheduledAt.Add(time.Duration(e.DurationMin)*time.Minute).UTC().Format("20060102T150405Z"))
		line("SUMMARY:" + icsText(e.Title))
		line("DESCRIPTION:" + icsText(fmt.Sprintf("%s, with %s\nJoin: %s", e.ClassroomName, e.TeacherName, link)))
		line("URL:" + link)
		line("STATUS:" + status)
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return b.String()
}

func icsText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`).Replace(s)
}

// fold ends a content line, folding it at 75 octets without splitting a character.
func fold(l string) string {
	var b strings.Builder
	n := 0
	for _, r := range l {
		size := len(string(r))
		if n+size > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += size
	}
	b.WriteString("\r\n")
	return b.String()
}

// SessionICS is one session as a calendar file, for anyone who may see it.
func (s *Service) SessionICS(ctx context.Context, p authn.Person, id string) (string, string, error) {
	sess, err := s.session(ctx, id)
	if err != nil {
		return "", "", err
	}
	if sess.ScheduledAt == nil {
		return "", "", web.Conflict("this session isn't scheduled")
	}
	list, err := s.calendar(ctx, p.ID, *sess.ScheduledAt, sess.ScheduledAt.Add(time.Second))
	if err != nil {
		return "", "", err
	}
	if sess.SeriesID != nil {
		var all []CalendarEntry
		var first time.Time
		if err := s.pool.QueryRow(ctx, `SELECT min(scheduled_at) FROM tutoring.sessions WHERE series_id=$1`, *sess.SeriesID).Scan(&first); err != nil {
			return "", "", err
		}
		if all, err = s.calendar(ctx, p.ID, first, first.Add(maxSpan+24*time.Hour)); err != nil {
			return "", "", err
		}
		list = list[:0]
		for _, e := range all {
			if e.SeriesID != nil && *e.SeriesID == *sess.SeriesID {
				list = append(list, e)
			}
		}
	}
	if !slices.ContainsFunc(list, func(e CalendarEntry) bool { return e.ID == id }) {
		return "", "", web.ErrNotFound
	}
	return s.ICS(sess.Title, list), sess.JoinCode, nil
}

// ---------- the private feed ----------

func feedHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// NewFeed makes (or replaces) the caller's private calendar feed; the token
// is shown once (TS-FR-88).
func (s *Service) NewFeed(ctx context.Context, p authn.Person) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	_, err := s.pool.Exec(ctx, `INSERT INTO tutoring.feeds (user_id, token_hash, created_at) VALUES ($1,$2,$3)
		ON CONFLICT (user_id) DO UPDATE SET token_hash=EXCLUDED.token_hash, created_at=EXCLUDED.created_at`, p.ID, feedHash(token), s.now())
	return token, err
}

// Feed says whether the caller has a feed, and since when.
func (s *Service) Feed(ctx context.Context, p authn.Person) (*time.Time, error) {
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT created_at FROM tutoring.feeds WHERE user_id=$1`, p.ID).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &at, err
}

// RevokeFeed stops the caller's feed link working.
func (s *Service) RevokeFeed(ctx context.Context, p authn.Person) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM tutoring.feeds WHERE user_id=$1`, p.ID)
	return err
}

// ServeFeed is GET /feeds/{token}.ics: a month back and six months ahead.
func (s *Service) ServeFeed(w http.ResponseWriter, r *http.Request, token string) {
	var userID string
	err := s.pool.QueryRow(r.Context(), `SELECT user_id::text FROM tutoring.feeds WHERE token_hash=$1`, feedHash(strings.TrimSuffix(token, ".ics"))).Scan(&userID)
	if err != nil {
		web.WriteError(w, r, web.ErrNotFound)
		return
	}
	now := s.now()
	list, err := s.calendar(r.Context(), userID, now.AddDate(0, -1, 0), now.AddDate(0, 6, 0))
	if err != nil {
		web.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write([]byte(s.ICS("Tutoring sessions", list)))
}
