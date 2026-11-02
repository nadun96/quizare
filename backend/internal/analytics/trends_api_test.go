package analytics_test

import (
	"context"
	"testing"

	"github.com/nadun96/quizplatform/internal/analytics"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
)

// Long-term trends (D-119): the roll-up job keeps month-by-month figures
// per classroom and per student, recomputing only what changed, and the
// teacher's and the student's views read them.
func TestTrends(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sess := f.session(t)
	f.take(t, sess, "S1", "Ann", "o1", "100") // 100%
	f.take(t, sess, "S2", "Ben", "o1", "99")  // 50%
	f.take(t, sess, "S3", "Cat", "o3", "")    // 0%
	f.process(t)
	// Ben's quiz was three months ago.
	if _, err := f.e.Pool.Exec(ctx, `UPDATE live.attempts SET submitted_at = (date_trunc('month', now() AT TIME ZONE 'UTC') - interval '3 months' + interval '5 days') AT TIME ZONE 'UTC'
		WHERE user_id = (SELECT id FROM auth.users WHERE email = 's2@example.com')`); err != nil {
		t.Fatal(err)
	}
	// And all of it a while ago, as the roll-up's next run will see it.
	if _, err := f.e.Pool.Exec(ctx, `UPDATE eval.results SET updated_at = now() - interval '10 minutes'`); err != nil {
		t.Fatal(err)
	}
	n, err := f.e.App.Analytics.Rollup(ctx)
	if err != nil || n != 2 {
		t.Fatalf("first roll-up = %d, %v", n, err)
	}

	var cls []content.Classroom
	var page struct {
		Classrooms []content.Classroom `json:"classrooms"`
	}
	f.teacher.Call("GET", "/api/teacher/classrooms", nil, 200, &page)
	cls = page.Classrooms
	path := "/api/teacher/classrooms/" + cls[0].ID + "/trends"
	var tr analytics.ClassroomTrends
	f.teacher.Call("GET", path, nil, 200, &tr)
	if len(tr.Months) != 12 || tr.Updated == nil {
		t.Fatalf("months = %d, updated %v", len(tr.Months), tr.Updated)
	}
	now, back := tr.Months[11], tr.Months[8]
	if now.Quizzes != 2 || *now.Avg != 50 || now.Enrolled != 3 || *now.Taking != 67 || *now.PassRate != 50 || now.Sessions != 1 {
		t.Fatalf("this month = %+v", now)
	}
	if back.Quizzes != 1 || *back.Avg != 50 || back.Active != 1 || back.Enrolled != 1 || *back.Taking != 100 || tr.Months[10].Avg != nil {
		t.Fatalf("three months ago = %+v, last month %+v", back, tr.Months[10])
	}
	if len(tr.Students) != 3 || tr.Students[1].Name != "Ben" || tr.Students[1].Recent != nil || *tr.Students[1].Earlier != 50 || tr.Students[0].ActiveMonths != 1 {
		t.Fatalf("students = %+v", tr.Students)
	}

	// Nothing changed: only the open classroom's current month is looked at again.
	if n, _ := f.e.App.Analytics.Rollup(ctx); n != 1 {
		t.Fatalf("idle roll-up = %d", n)
	}
	// A mark changes: the month is recomputed; the past month's members stay as counted.
	if _, err := f.e.Pool.Exec(ctx, `UPDATE eval.results SET pct = 80, updated_at = now() WHERE user_id = (SELECT id FROM auth.users WHERE email = 's1@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Pool.Exec(ctx, `UPDATE content.enrolments SET status = 'removed' WHERE user_id = (SELECT id FROM auth.users WHERE email = 's3@example.com')`); err != nil {
		t.Fatal(err)
	}
	// And Ann finishes a tutorial in the classroom.
	var topic string
	f.e.Pool.QueryRow(ctx, `SELECT id FROM content.topics LIMIT 1`).Scan(&topic)
	var tu struct {
		ID string `json:"id"`
	}
	f.teacher.Call("POST", "/api/teacher/topics/"+topic+"/tutorials", map[string]any{"title": "Cells"}, 201, &tu)
	if _, err := f.e.Pool.Exec(ctx, `INSERT INTO tutorial.runs (tutorial_id, user_id, finished_at) SELECT $1, id, now() FROM auth.users WHERE email = 's1@example.com'`, tu.ID); err != nil {
		t.Fatal(err)
	}
	f.e.App.Analytics.Rollup(ctx)
	f.teacher.Call("GET", path+"?months=24", nil, 200, &tr)
	if tr.Months[23].Tutorials != 1 || tr.Months[23].Quizzes != 2 {
		t.Fatalf("with a tutorial: %+v", tr.Months[23])
	}
	if len(tr.Months) != 24 || *tr.Months[23].Avg != 40 || tr.Months[23].Enrolled != 2 || tr.Months[20].Enrolled != 1 {
		t.Fatalf("after changes: this month %+v, three months ago %+v", tr.Months[23], tr.Months[20])
	}

	// One student's months, beside the class's.
	ben := tr.Students[1].UserID
	var st analytics.StudentTrends
	f.teacher.Call("GET", "/api/teacher/classrooms/"+cls[0].ID+"/students/"+ben+"/trends", nil, 200, &st)
	if st.Name != "Ben" || st.Months[8].Quizzes != 1 || *st.Months[8].ClassAvg != 50 || st.Months[11].Quizzes != 0 {
		t.Fatalf("Ben = %+v", st)
	}
	other := f.e.NewUser(auth.RoleTeacher)
	other.Call("GET", path, nil, 404, nil)
	other.Call("GET", "/api/teacher/classrooms/"+cls[0].ID+"/students/"+ben+"/trends", nil, 404, nil)
	f.teacher.Call("GET", "/api/teacher/classrooms/"+cls[0].ID+"/students/"+f.teacher.User.ID+"/trends", nil, 404, nil)

	// The student's own.
	ann := f.e.Client()
	ann.Call("POST", "/api/auth/login", map[string]string{"email": "s1@example.com", "password": "password123"}, 200, nil)
	ann.Call("GET", "/api/my/trends", nil, 200, &st)
	if st.Name != "Ann" || st.Months[11].Quizzes != 1 || *st.Months[11].Avg != 80 || st.Months[11].Active != 1 || st.Months[11].Tutorials != 1 {
		t.Fatalf("Ann's own = %+v", st.Months[11])
	}
}
