package archive_test

import (
	"context"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/archive"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/meet"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/poll"
	"github.com/nadun96/quizplatform/internal/quiz"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

const quizCSV = "question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,A?,A|B,A,2\n"

type fx struct {
	e       *apptest.Env
	teacher *apptest.Client
	class   content.Classroom
	topic   content.Topic
	quizID  string
}

func setup(t *testing.T) *fx {
	t.Helper()
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	f := &fx{e: e, teacher: teacher}
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Science"}, 201, &f.class)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+f.class.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &f.topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/quizzes", map[string]any{"title": "Cells quiz"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(quizCSV))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	f.quizID = q.ID
	return f
}

func (f *fx) session(t *testing.T, title string, end bool) live.Session {
	t.Helper()
	var s live.Session
	f.teacher.Call("POST", "/api/teacher/quizzes/"+f.quizID+"/sessions", map[string]any{"title": title}, 201, &s)
	if end {
		f.teacher.Call("POST", "/api/teacher/sessions/"+s.ID+"/end", nil, 204, nil)
	}
	return s
}

func (f *fx) list(t *testing.T, query string) []archive.Item {
	t.Helper()
	var out struct{ Items []archive.Item }
	f.teacher.Call("GET", "/api/teacher/archive?"+query, nil, 200, &out)
	return out.Items
}

func titles(items []archive.Item) map[string]bool {
	m := map[string]bool{}
	for _, it := range items {
		m[it.Title] = true
	}
	return m
}

// D-149: all four kinds go past, archived and away.
func TestArchiveEveryKind(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := f.session(t, "Quiz one", true)
	f.session(t, "Still running", false)

	var p, draft poll.Poll
	f.teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "Poll one", "classroom_id": f.class.ID}, 201, &p)
	f.teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "Draft poll"}, 201, &draft)
	if _, err := f.e.Pool.Exec(ctx, `UPDATE poll.polls SET status = 'closed', closed_at = now() WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	var m meet.Meeting
	f.teacher.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Meeting one", "classroom_id": f.class.ID}, 201, &m)
	f.teacher.Call("POST", "/api/teacher/meetings/"+m.ID+"/end", nil, 204, nil)
	var slides struct{ ID string }
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/presentations", map[string]any{"title": "Slides one"}, 201, &slides)
	var liveID string
	if err := f.e.Pool.QueryRow(ctx, `INSERT INTO presentation.lives (presentation_id, code, ended_at) VALUES ($1, 'ARCHV1', now()) RETURNING id`, slides.ID).Scan(&liveID); err != nil {
		t.Fatal(err)
	}

	past := titles(f.list(t, ""))
	for _, want := range []string{"Quiz one", "Poll one", "Meeting one", "Slides one"} {
		if !past[want] {
			t.Fatalf("%s isn't in the past list: %v", want, past)
		}
	}
	if past["Still running"] || past["Draft poll"] {
		t.Fatalf("what hasn't finished is listed: %v", past)
	}

	// The everyday lists lose them when they're archived.
	kinds := map[string]string{"quiz": s.ID, "poll": p.ID, "meeting": m.ID, "presentation": liveID}
	for kind, id := range kinds {
		f.teacher.Call("DELETE", "/api/teacher/archive/"+kind+"/"+id, nil, 409, nil) // archive first
		f.teacher.Call("POST", "/api/teacher/archive/"+kind+"/"+id, nil, 204, nil)
		f.teacher.Call("POST", "/api/teacher/archive/"+kind+"/"+id, nil, 204, nil) // twice is harmless
	}
	if got := f.list(t, ""); len(got) != 0 {
		t.Fatalf("still in the past list: %+v", got)
	}
	if got := titles(f.list(t, "view=archived")); len(got) != 4 {
		t.Fatalf("archived: %v", got)
	}
	var sessions struct{ Sessions []live.Session }
	f.teacher.Call("GET", "/api/teacher/quizzes/"+f.quizID+"/sessions", nil, 200, &sessions)
	if len(sessions.Sessions) != 1 || sessions.Sessions[0].Title != "Still running" {
		t.Fatalf("the quiz's sessions: %+v", sessions.Sessions)
	}
	var polls struct{ Polls []poll.Poll }
	f.teacher.Call("GET", "/api/teacher/polls", nil, 200, &polls)
	if len(polls.Polls) != 1 || polls.Polls[0].Title != "Draft poll" {
		t.Fatalf("the polls: %+v", polls.Polls)
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/status", map[string]string{"status": "open"}, 409, nil)
	var meetings struct{ Meetings []meet.Meeting }
	f.teacher.Call("GET", "/api/teacher/meetings", nil, 200, &meetings)
	if len(meetings.Meetings) != 0 {
		t.Fatalf("the meetings: %+v", meetings.Meetings)
	}

	// Restored, then archived again and deleted for good.
	f.teacher.Call("POST", "/api/teacher/archive/quiz/"+s.ID+"/restore", nil, 204, nil)
	if got := titles(f.list(t, "")); !got["Quiz one"] {
		t.Fatalf("not back: %v", got)
	}
	f.teacher.Call("POST", "/api/teacher/archive/quiz/"+s.ID, nil, 204, nil)
	for kind, id := range kinds {
		f.teacher.Call("DELETE", "/api/teacher/archive/"+kind+"/"+id, nil, 204, nil)
		f.teacher.Call("DELETE", "/api/teacher/archive/"+kind+"/"+id, nil, 404, nil)
	}
	if got := f.list(t, "view=archived"); len(got) != 0 {
		t.Fatalf("left: %+v", got)
	}
	f.teacher.Call("GET", "/api/teacher/sessions/"+s.ID, nil, 404, nil) // not from memory either
}

// D-149: search and filters work across the kinds.
func TestArchiveSearchAndFilter(t *testing.T) {
	f := setup(t)
	f.session(t, "Algebra revision", true)
	var other content.Classroom
	f.teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "History"}, 201, &other)
	var m meet.Meeting
	f.teacher.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Parents evening", "classroom_id": other.ID}, 201, &m)
	f.teacher.Call("POST", "/api/teacher/meetings/"+m.ID+"/end", nil, 204, nil)

	cases := map[string]string{
		"q=algebra":                  "Algebra revision",
		"q=cells":                    "Algebra revision", // the quiz it ran
		"q=history":                  "Parents evening",  // the classroom
		"kind=meeting":               "Parents evening",
		"kind=quiz":                  "Algebra revision",
		"classroom_id=" + other.ID:   "Parents evening",
		"classroom_id=" + f.class.ID: "Algebra revision",
	}
	for query, want := range cases {
		if got := f.list(t, query); len(got) != 1 || got[0].Title != want {
			t.Fatalf("%s: %+v", query, got)
		}
	}
	today := time.Now().UTC().Format("2006-01-02")
	if got := f.list(t, "from="+today+"&to="+today); len(got) != 2 {
		t.Fatalf("today: %+v", got)
	}
	if got := f.list(t, "from=2001-01-01&to=2001-01-31"); len(got) != 0 {
		t.Fatalf("long ago: %+v", got)
	}
	f.teacher.Call("GET", "/api/teacher/archive?from=soon", nil, 422, nil)
}

// D-149: what's still running, or someone else's, can't be touched.
func TestArchiveRules(t *testing.T) {
	f := setup(t)
	running := f.session(t, "Still running", false)
	f.teacher.Call("POST", "/api/teacher/archive/quiz/"+running.ID, nil, 409, nil)

	ended := f.session(t, "Finished", true)
	stranger := f.e.NewUser(auth.RoleTeacher)
	stranger.Call("POST", "/api/teacher/archive/quiz/"+ended.ID, nil, 404, nil)
	var out struct{ Items []archive.Item }
	stranger.Call("GET", "/api/teacher/archive", nil, 200, &out)
	if len(out.Items) != 0 {
		t.Fatalf("a stranger sees %+v", out.Items)
	}
	f.teacher.Call("POST", "/api/teacher/archive/nonsense/"+ended.ID, nil, 404, nil)
	f.teacher.Call("POST", "/api/teacher/archive/quiz/not-an-id", nil, 404, nil)
	f.e.NewUser(auth.RoleStudent).Call("GET", "/api/teacher/archive", nil, 403, nil)

	// A co-teacher's classroom sessions are theirs to archive too.
	co := f.e.NewUser(auth.RoleTeacher)
	if _, err := f.e.Pool.Exec(context.Background(), `INSERT INTO content.classroom_teachers (classroom_id, user_id, status, invited_by, accepted_at) VALUES ($1, $2, 'active', $3, now())`, f.class.ID, co.User.ID, f.teacher.User.ID); err != nil {
		t.Fatal(err)
	}
	co.Call("POST", "/api/teacher/archive/quiz/"+ended.ID, nil, 204, nil)
}
