package oversight_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/meet"
	"github.com/nadun96/quizplatform/internal/oversight"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/poll"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

const csvQuiz = `question_code,type,question_text,options,correct_answer,marks
Q1,SINGLE,Capital of France?,Paris|Rome|Madrid,Paris,1
`

type fx struct {
	e       *apptest.Env
	admin   *apptest.Client
	teacher *apptest.Client
	topicID string
	quizID  string
}

func setup(t *testing.T) *fx {
	t.Helper()
	e := apptest.New(t)
	f := &fx{e: e, admin: e.NewUser(auth.RoleAdmin), teacher: e.NewUser(auth.RoleTeacher)}
	var c content.Classroom
	f.teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 7"}, 201, &c)
	var m content.Module
	f.teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	f.teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q struct{ ID string }
	f.teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Science"}, 201, &q)
	f.teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(csvQuiz))
	f.teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	f.topicID, f.quizID = topic.ID, q.ID
	return f
}

func (f *fx) session(want int) live.Session {
	var s live.Session
	f.teacher.Call("POST", "/api/teacher/quizzes/"+f.quizID+"/sessions", map[string]any{}, want, &s)
	return s
}

func (f *fx) poll(t *testing.T) poll.Poll {
	var p poll.Poll
	f.teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "Lunch"}, 201, &p)
	f.teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/questions", map[string]any{"type": "SINGLE", "text": "Rice or bread?",
		"body": map[string]any{"options": []map[string]string{{"text": "Rice"}, {"text": "Bread"}}}}, 201, nil)
	return p
}

type liveList struct {
	Activities []oversight.Activity `json:"activities"`
	Pauses     []oversight.Pause    `json:"pauses"`
}

func (f *fx) list() liveList {
	var l liveList
	f.admin.Call("GET", "/api/admin/live-activity", nil, 200, &l)
	return l
}

func find(l liveList, kind, id string) *oversight.Activity {
	for _, a := range l.Activities {
		if a.Kind == kind && a.ID == id {
			return &a
		}
	}
	return nil
}

func TestAdminSeesAndEndsLiveActivity(t *testing.T) {
	f := setup(t)
	sess := f.session(201)
	p := f.poll(t)
	f.teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/status", map[string]string{"status": "open"}, 200, nil)
	var m meet.Meeting
	f.teacher.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Office hour"}, 201, &m)
	// A live presentation, made directly: presenting needs uploaded slides.
	ctx := context.Background()
	var presID string
	if err := f.e.Pool.QueryRow(ctx, `INSERT INTO presentation.presentations (topic_id, teacher_id, title, version) VALUES ($1, $2, 'Cells', 1) RETURNING id`,
		f.topicID, f.teacher.User.ID).Scan(&presID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Pool.Exec(ctx, `INSERT INTO presentation.lives (presentation_id, code, started_by) VALUES ($1, 'CELLS234', $2)`, presID, f.teacher.User.ID); err != nil {
		t.Fatal(err)
	}

	// Everything live, with its teacher and classroom.
	l := f.list()
	for _, x := range []struct{ kind, id, title string }{
		{oversight.KindSession, sess.ID, sess.Title}, {oversight.KindPoll, p.ID, "Lunch"},
		{oversight.KindMeeting, m.ID, "Office hour"}, {oversight.KindPresentation, presID, "Cells"},
	} {
		a := find(l, x.kind, x.id)
		if a == nil || a.Title != x.title || a.TeacherID != f.teacher.User.ID || a.TeacherEmail != f.teacher.User.Email {
			t.Fatalf("%s: %+v in %+v", x.kind, a, l.Activities)
		}
	}
	if a := find(l, oversight.KindSession, sess.ID); a.Classroom != "Grade 7" {
		t.Fatalf("session classroom = %q", a.Classroom)
	}

	// Teachers and students don't see it; managers need the feature.
	f.teacher.Call("GET", "/api/admin/live-activity", nil, 403, nil)
	mgr := f.e.NewUser(auth.RoleTeacher)
	f.admin.Call("POST", "/api/admin/managers", map[string]any{"user_id": mgr.User.ID, "features": []string{"usage"}}, 201, nil)
	mgr.Call("GET", "/api/admin/live-activity", nil, 404, nil) // features they lack are hidden
	f.admin.Call("PUT", "/api/admin/managers/"+mgr.User.ID+"/features", map[string]any{"features": []string{"live_activity"}}, 200, nil)
	mgr.Call("GET", "/api/admin/live-activity", nil, 200, nil)

	// Ending needs a reason, which the teacher is told.
	f.admin.Call("POST", "/api/admin/live-activity/quiz_session/"+sess.ID+"/end", map[string]string{"reason": " "}, 422, nil)
	f.admin.Call("POST", "/api/admin/live-activity/unknown/"+sess.ID+"/end", map[string]string{"reason": "x"}, 404, nil)
	f.e.TakeJobs("email")
	for _, x := range []struct{ kind, id string }{
		{"quiz_session", sess.ID}, {"poll", p.ID}, {"meeting", m.ID}, {"presentation", presID},
	} {
		mgr.Call("POST", "/api/admin/live-activity/"+x.kind+"/"+x.id+"/end", map[string]string{"reason": "Server overloaded"}, 204, nil)
		// Once ended, it's gone from the list and can't be ended again.
		mgr.Call("POST", "/api/admin/live-activity/"+x.kind+"/"+x.id+"/end", map[string]string{"reason": "again"}, 404, nil)
	}
	if l := f.list(); len(l.Activities) != 0 {
		t.Fatalf("still live: %+v", l.Activities)
	}
	var got live.Dashboard
	f.teacher.Call("GET", "/api/teacher/sessions/"+sess.ID, nil, 200, &got)
	if got.Session.Status != live.SessionEnded {
		t.Fatalf("session status = %s", got.Session.Status)
	}
	var gotPoll poll.Poll
	f.teacher.Call("GET", "/api/teacher/polls/"+p.ID, nil, 200, &gotPoll)
	if gotPoll.Status != "closed" {
		t.Fatalf("poll status = %s", gotPoll.Status)
	}
	jobs := f.e.TakeJobs("email")
	if len(jobs) != 4 {
		t.Fatalf("emails = %d", len(jobs))
	}
	var msg mail.Message
	_ = json.Unmarshal(jobs[0], &msg)
	if msg.To != f.teacher.User.Email || !strings.Contains(msg.Body, "Server overloaded") {
		t.Fatalf("email = %+v", msg)
	}
	var n int
	if err := f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM audit.events WHERE action='activity_force_ended' AND actor_id=$1 AND details->>'reason'='Server overloaded'`, mgr.User.ID).Scan(&n); err != nil || n != 4 {
		t.Fatalf("audit = %d, %v", n, err)
	}
}

func TestPausingNewActivities(t *testing.T) {
	f := setup(t)
	other := f.e.NewUser(auth.RoleTeacher)
	p := f.poll(t)
	running := f.session(201)

	// One teacher paused: they start nothing new; others carry on, and
	// what's already running does too.
	var pause oversight.Pause
	f.admin.Call("POST", "/api/admin/live-activity/pauses", map[string]string{"teacher_email": strings.ToUpper(f.teacher.User.Email), "reason": "Too many sessions"}, 200, &pause)
	if pause.TeacherID == nil || *pause.TeacherID != f.teacher.User.ID || pause.Reason != "Too many sessions" {
		t.Fatalf("pause = %+v", pause)
	}
	f.admin.Call("POST", "/api/admin/live-activity/pauses", map[string]string{"teacher_email": "nobody@example.com"}, 422, nil)
	var bad struct{ Code, Message string }
	f.session(409)
	f.teacher.Call("POST", "/api/teacher/quizzes/"+f.quizID+"/sessions", map[string]any{}, 409, &bad)
	if bad.Code != "activities_paused" || !strings.Contains(bad.Message, "Too many sessions") {
		t.Fatalf("paused = %+v", bad)
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/status", map[string]string{"status": "open"}, 409, nil)
	f.teacher.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Chat"}, 409, nil)
	other.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Chat"}, 201, nil)
	f.teacher.Call("GET", "/api/teacher/sessions/"+running.ID, nil, 200, nil)

	// Pausing again changes the reason; everyone paused as well.
	f.admin.Call("POST", "/api/admin/live-activity/pauses", map[string]string{"teacher_id": f.teacher.User.ID, "reason": "Exams"}, 200, &pause)
	var all oversight.Pause
	f.admin.Call("POST", "/api/admin/live-activity/pauses", map[string]string{"reason": ""}, 200, &all)
	f.admin.Call("POST", "/api/admin/live-activity/pauses", map[string]string{"reason": "Maintenance"}, 200, &all)
	if l := f.list(); len(l.Pauses) != 2 || l.Pauses[0].TeacherID != nil || l.Pauses[0].Reason != "Maintenance" || l.Pauses[1].Reason != "Exams" {
		t.Fatalf("pauses = %+v", l.Pauses)
	}
	other.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Chat"}, 409, &bad)
	if !strings.Contains(bad.Message, "Maintenance") {
		t.Fatalf("everyone = %+v", bad)
	}
	// The teacher's own pause says why first.
	f.teacher.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Chat"}, 409, &bad)
	if !strings.Contains(bad.Message, "Exams") {
		t.Fatalf("own = %+v", bad)
	}

	// Resuming.
	f.admin.Call("DELETE", "/api/admin/live-activity/pauses/"+all.ID, nil, 204, nil)
	f.admin.Call("DELETE", "/api/admin/live-activity/pauses/"+all.ID, nil, 404, nil)
	other.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Chat"}, 201, nil)
	f.teacher.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Chat"}, 409, nil)
	f.admin.Call("DELETE", "/api/admin/live-activity/pauses/"+pause.ID, nil, 204, nil)
	f.teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/status", map[string]string{"status": "open"}, 200, nil)
	f.session(201)

	var n int
	if err := f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit.events WHERE action IN ('activities_paused', 'activities_resumed')`).Scan(&n); err != nil || n != 6 {
		t.Fatalf("audit = %d, %v", n, err)
	}
}
