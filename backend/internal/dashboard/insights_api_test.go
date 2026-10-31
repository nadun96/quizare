package dashboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/dashboard"
)

// The dashboard's charts (D-105): a student's scores against the class,
// their weeks and classrooms; a teacher's sessions, active students and
// classroom participation, each against the period before.
func TestInsights(t *testing.T) {
	e := apptest.New(t)
	ctx := context.Background()
	teacher := e.NewUser(auth.RoleTeacher)
	s1, s2, s3 := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 8"}, 201, &c)
	for _, s := range []*apptest.Client{s1, s2, s3} {
		s.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)
	}
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q struct{ ID string }
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Cells"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,A?,A|B,A,1\n"))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)

	// Two released sessions: one 50 days ago, one now.
	result := func(session string, who *apptest.Client, pct float64, at time.Time) {
		var attempt string
		if err := e.Pool.QueryRow(ctx, `INSERT INTO live.attempts (session_id, user_id, state, submitted_at) VALUES ($1, $2, 'submitted', $3) RETURNING id`, session, who.User.ID, at).Scan(&attempt); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Pool.Exec(ctx, `INSERT INTO eval.results (attempt_id, session_id, user_id, score, max_score, pct, pass_mark_pct, passed, complete, invalidated)
			VALUES ($1, $2, $3, $4::numeric, 100, $4::numeric, 50, $4::numeric >= 50, true, false)`, attempt, session, who.User.ID, pct); err != nil {
			t.Fatal(err)
		}
	}
	session := func(at time.Time) string {
		var sess struct{ ID string }
		teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)
		teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/end", map[string]any{}, 204, nil)
		teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/release", nil, 204, nil)
		if _, err := e.Pool.Exec(ctx, `UPDATE live.sessions SET created_at = $2, started_at = $2 WHERE id = $1`, sess.ID, at); err != nil {
			t.Fatal(err)
		}
		return sess.ID
	}
	now := time.Now()
	old, recent := session(now.AddDate(0, 0, -50)), session(now)
	result(old, s1, 40, now.AddDate(0, 0, -50))
	result(recent, s1, 80, now)
	result(recent, s2, 60, now)

	var in dashboard.Insights
	s1.Call("GET", "/api/my/dashboard/insights?days=30", nil, 200, &in)
	if in.Days != 30 || in.KPIs["avg_score"].Value != 80 || in.KPIs["avg_score"].Prev == nil || *in.KPIs["avg_score"].Prev != 40 || in.KPIs["quizzes"].Value != 1 {
		t.Fatalf("student kpis = %+v", in.KPIs)
	}
	if len(in.Scores) != 1 || in.Scores[0].Pct != 80 || in.Scores[0].ClassAvg == nil || *in.Scores[0].ClassAvg != 70 || in.Scores[0].Classroom != "Grade 8" {
		t.Fatalf("scores = %+v", in.Scores)
	}
	if len(in.Weeks) != 12 || in.Weeks[11].Quizzes != 1 || len(in.Classrooms) != 1 || *in.Classrooms[0].Avg != 80 {
		t.Fatalf("weeks %+v classrooms %+v", in.Weeks, in.Classrooms)
	}
	s1.Call("GET", "/api/my/dashboard/insights?days=90", nil, 200, &in)
	if in.Days != 90 || len(in.Scores) != 2 || in.KPIs["avg_score"].Value != 60 || in.KPIs["avg_score"].Prev != nil {
		t.Fatalf("90 days = %+v %+v", in.KPIs, in.Scores)
	}

	teacher.Call("GET", "/api/my/dashboard/insights?days=30", nil, 200, &in)
	if in.KPIs["students"].Value != 3 || in.KPIs["active_students"].Value != 2 || *in.KPIs["active_students"].Prev != 1 || in.KPIs["sessions"].Value != 1 || in.KPIs["avg_score"].Value != 70 || in.KPIs["pass_rate"].Value != 100 {
		t.Fatalf("teacher kpis = %+v", in.KPIs)
	}
	if len(in.Sessions) != 1 || in.Sessions[0].Avg != 70 || in.Sessions[0].Results != 2 {
		t.Fatalf("sessions = %+v", in.Sessions)
	}
	if len(in.Classrooms) != 1 || in.Classrooms[0].Students != 3 || *in.Classrooms[0].Participation != 67 || in.Weeks[11].Students != 2 {
		t.Fatalf("teacher classrooms %+v weeks %+v", in.Classrooms, in.Weeks)
	}
	// Any other range is 90 days; staff have nothing here.
	teacher.Call("GET", "/api/my/dashboard/insights?days=7", nil, 200, &in)
	if in.Days != 90 {
		t.Fatalf("days = %d", in.Days)
	}
}
