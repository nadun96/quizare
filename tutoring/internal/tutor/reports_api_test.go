package tutor_test

import (
	"context"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// Attendance reports (D-87, TS-FR-81 to TS-FR-83).

func TestAttendanceReports(t *testing.T) {
	f := setup(t, "auto")
	ann, bob := f.student("Ann"), f.student("Bob")
	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	path := "/api/classrooms/" + f.classroom + "/attendance?from=" + from + "&to=" + to

	// First session: Ann and Bob. Second: Ann only. A third never starts.
	f.join(f.teacher, 200)
	f.join(ann, 200)
	f.join(bob, 200)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	as, bs := ann.Dial(f.sess.ID), bob.Dial(f.sess.ID)
	as.Must("state")
	bs.Must("state")
	time.Sleep(1100 * time.Millisecond)
	f.teacher.Call("POST", f.path("/end"), nil, 204, nil)
	as.WaitClosed()
	bs.WaitClosed()

	var second, third tutor.Session
	f.teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": f.classroom, "title": "Second"}, 201, &second)
	f.teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": f.classroom, "title": "Never started"}, 201, &third)
	ann.Call("POST", "/api/join/"+second.JoinCode, nil, 200, nil)
	f.teacher.Call("POST", "/api/join/"+second.JoinCode, nil, 200, nil)
	f.teacher.Call("POST", "/api/sessions/"+second.ID+"/start", nil, 204, nil)
	as2 := ann.Dial(second.ID)
	as2.Must("state")
	time.Sleep(200 * time.Millisecond)

	// The classroom's report (TS-FR-81): the live one counts too.
	var rep tutor.ClassroomReport
	f.teacher.Call("GET", path, nil, 200, &rep)
	if len(rep.Sessions) != 2 || rep.Sessions[0].ID != f.sess.ID || rep.Sessions[0].Seconds < 1 || rep.Sessions[1].EndedAt != nil {
		t.Fatalf("sessions = %+v", rep.Sessions)
	}
	if len(rep.Students) != 2 || rep.Students[0].Name != "Ann" || len(rep.Students[0].Attended) != 2 || len(rep.Students[1].Attended) != 1 ||
		rep.Students[1].Attended[0] != f.sess.ID || rep.Students[1].Seconds < 1 || rep.CountsInResults {
		t.Fatalf("students = %+v", rep.Students)
	}
	// Only the classroom's teacher; a range of at most a year.
	f.e.NewPerson("teacher", "Mr Silva").Call("GET", path, nil, 404, nil)
	ann.Call("GET", path, nil, 404, nil)
	f.teacher.Call("GET", "/api/classrooms/"+f.classroom+"/attendance?from=2020-01-01T00:00:00Z&to=2022-01-01T00:00:00Z", nil, 422, nil)
	f.teacher.Call("GET", "/api/classrooms/"+f.classroom+"/attendance?from=yesterday&to="+to, nil, 422, nil)

	// Counting it in the results (TS-FR-82).
	ann.Call("PUT", "/api/classrooms/"+f.classroom+"/attendance/settings", map[string]bool{"counts_in_results": true}, 404, nil)
	f.teacher.Call("PUT", "/api/classrooms/"+f.classroom+"/attendance/settings", map[string]bool{"counts_in_results": true}, 204, nil)
	rep = tutor.ClassroomReport{}
	f.teacher.Call("GET", path, nil, 200, &rep)
	if !rep.CountsInResults {
		t.Fatal("not counted after turning it on")
	}

	// A student's own page (TS-FR-81).
	var mine struct{ Classrooms []tutor.MyClassroomAttendance }
	bob.Call("GET", "/api/my/attendance", nil, 200, &mine)
	if len(mine.Classrooms) != 1 || mine.Classrooms[0].Sessions != 2 || mine.Classrooms[0].Attended != 1 || len(mine.Classrooms[0].Recent) != 2 ||
		mine.Classrooms[0].Recent[0].Title != "Second" || mine.Classrooms[0].Recent[0].Attended || !mine.Classrooms[0].Recent[1].Attended {
		t.Fatalf("Bob's attendance = %+v", mine.Classrooms)
	}
	mine.Classrooms = nil
	ann.Call("GET", "/api/my/attendance", nil, 200, &mine)
	if len(mine.Classrooms) != 1 || mine.Classrooms[0].Attended != 2 {
		t.Fatalf("Ann's attendance = %+v", mine.Classrooms)
	}

	// Kept a year after the end (PO-16): then the session goes, with its attendance.
	f.teacher.Call("POST", "/api/sessions/"+second.ID+"/end", nil, 204, nil)
	ctx := context.Background()
	if n, err := f.e.Service.SweepRetention(ctx, tutor.DefaultRetention); err != nil || n != 0 {
		t.Fatalf("swept too early: %d %v", n, err)
	}
	f.e.Service.SetClock(func() time.Time { return time.Now().Add(366 * 24 * time.Hour) })
	defer f.e.Service.SetClock(time.Now)
	if n, err := f.e.Service.SweepRetention(ctx, tutor.DefaultRetention); err != nil || n != 2 {
		t.Fatalf("swept %d, %v", n, err)
	}
	f.e.Service.SetClock(time.Now)
	var left int
	if err := f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM tutoring.visits`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("visits left = %d, %v", left, err)
	}
	var kept int
	if err := f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM tutoring.sessions WHERE id=$1`, third.ID).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("the session that never started: %d, %v", kept, err) // kept
	}
}
