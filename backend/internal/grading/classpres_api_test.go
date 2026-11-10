package grading_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"strconv"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/grading"
)

// jpeg is the smallest thing that starts like a JPEG.
var jpeg = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0, 0xFF, 0xD9}

func putPresSlide(t *testing.T, c *apptest.Client, id string, n int, want int) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("image", "s.jpg")
	fw.Write(jpeg)
	w.WriteField("text", "Slide text")
	w.Close()
	code, body := c.Raw("PUT", "/api/my/group-presentations/"+id+"/slides/"+strconv.Itoa(n), w.FormDataContentType(), buf.Bytes())
	if code != want {
		t.Fatalf("slide %d: %d %s", n, code, body)
	}
}

func (f *reportFx) groupID(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := f.e.Pool.QueryRow(context.Background(), `SELECT id::text FROM content.categories WHERE classroom_id = $1 AND name = $2`, f.class.ID, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func myTask(t *testing.T, c *apptest.Client, classID, taskID string) grading.MyGroupTask {
	t.Helper()
	var v grading.MyClassPresentations
	c.Call("GET", "/api/my/classrooms/"+classID+"/presentations", nil, 200, &v)
	for _, x := range v.Tasks {
		if x.ID == taskID {
			return x
		}
	}
	t.Fatalf("task %s not in %+v", taskID, v.Tasks)
	return grading.MyGroupTask{}
}

// D-166: a classroom's presentation tasks. Each group uploads one; the
// teacher allows it; a member presents it on the classroom's screen while
// classmates follow; the teacher can take over and stop; marks count in the
// class report for every member; uploads can be deleted later.
func TestClassGroupPresentations(t *testing.T) {
	f := newReportFx(t)
	ctx := context.Background()
	amaya, ben, chen, dev := f.student(t), f.student(t), f.student(t), f.student(t)
	f.classGroup(t, "Lions", amaya, ben)
	f.classGroup(t, "Tigers", chen)
	lions, tigers := f.groupID(t, "Lions"), f.groupID(t, "Tigers")
	other := f.e.NewUser(auth.RoleTeacher)
	tasksURL := "/api/teacher/classrooms/" + f.class.ID + "/presentation-tasks"

	// Tasks: checked, and only for the classroom's teachers.
	f.teacher.Call("POST", tasksURL, map[string]any{"title": " "}, 422, nil)
	f.teacher.Call("POST", tasksURL, map[string]any{"title": "Talks", "upload_by": "anyone"}, 422, nil)
	f.teacher.Call("POST", tasksURL, map[string]any{"title": "Talks", "topic_id": "00000000-0000-0000-0000-000000000000"}, 422, nil)
	other.Call("POST", tasksURL, map[string]any{"title": "Talks"}, 404, nil)
	var task grading.PresTask
	f.teacher.Call("POST", tasksURL, map[string]any{"title": "Volcano talks", "topic_id": f.topic.ID, "upload_by": "leader", "out_of": 20, "rating": true, "instructions": "5 minutes"}, 201, &task)
	if task.Topic != "T" || task.Groups != 2 || task.UploadBy != "leader" {
		t.Fatalf("task = %+v", task)
	}
	taskURL := "/api/teacher/presentation-tasks/" + task.ID
	other.Call("GET", taskURL, nil, 404, nil)

	// Leader only: Ben leads the Lions, Chen the Tigers; Amaya leads nothing.
	if _, err := f.e.Pool.Exec(ctx, `UPDATE content.enrolment_categories ec SET role = 'leader' FROM content.enrolments e
		WHERE e.id = ec.enrolment_id AND e.user_id = $1 AND ec.category_id = $2`, ben.User.ID, lions); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Pool.Exec(ctx, `UPDATE content.enrolment_categories ec SET role = 'leader' FROM content.enrolments e
		WHERE e.id = ec.enrolment_id AND e.user_id = $1 AND ec.category_id = $2`, chen.User.ID, tigers); err != nil {
		t.Fatal(err)
	}
	mt := myTask(t, amaya, f.class.ID, task.ID)
	if len(mt.MyGroups) != 1 || mt.MyGroups[0].ID != lions || mt.MyGroups[0].CanUpload {
		t.Fatalf("Amaya's view = %+v", mt.MyGroups)
	}
	if mt := myTask(t, dev, f.class.ID, task.ID); len(mt.MyGroups) != 0 {
		t.Fatalf("Dev is in no group: %+v", mt.MyGroups)
	}
	start := "/api/my/presentation-tasks/" + task.ID + "/presentations"
	amaya.Call("POST", start, map[string]any{"group_id": lions, "file_name": "talk.pdf", "slides": 2}, 403, nil)
	ben.Call("POST", start, map[string]any{"group_id": tigers, "file_name": "talk.pdf", "slides": 2}, 403, nil)
	ben.Call("POST", start, map[string]any{"group_id": lions, "file_name": "talk.pdf", "slides": 101}, 422, nil)
	var x grading.GroupPres
	ben.Call("POST", start, map[string]any{"group_id": lions, "file_name": "Lava flows.pdf", "slides": 2}, 201, &x)
	if x.Title != "Lava flows" || x.Ready {
		t.Fatalf("started = %+v", x)
	}
	putPresSlide(t, ben, x.ID, 3, 422)
	putPresSlide(t, ben, x.ID, 1, 204)
	putPresSlide(t, amaya, x.ID, 2, 403)
	ben.Call("POST", "/api/my/group-presentations/"+x.ID+"/finish", nil, 422, nil)
	putPresSlide(t, ben, x.ID, 2, 204)
	ben.Call("POST", "/api/my/group-presentations/"+x.ID+"/finish", nil, 200, &x)
	if !x.Ready || x.Allowed {
		t.Fatalf("finished = %+v", x)
	}
	// Their own slides they can see; others only when on the screen.
	amaya.Call("GET", "/api/my/group-presentations/"+x.ID+"/slides/1", nil, 200, nil)
	chen.Call("GET", "/api/my/group-presentations/"+x.ID+"/slides/1", nil, 404, nil)

	// Present: not until the teacher allows it.
	present := "/api/my/group-presentations/" + x.ID + "/present"
	amaya.Call("POST", present, nil, 403, nil)
	other.Call("PUT", "/api/teacher/group-presentations/"+x.ID+"/allowed", map[string]bool{"allowed": true}, 404, nil)
	f.teacher.Call("PUT", "/api/teacher/group-presentations/"+x.ID+"/allowed", map[string]bool{"allowed": true}, 204, nil)
	chen.Call("POST", present, nil, 404, nil) // not his group's
	var st struct{ Stage *grading.ClassStage }
	amaya.Call("POST", present, nil, 200, &st)
	if st.Stage == nil || !st.Stage.Presenting || !st.Stage.Mine || st.Stage.Slide != 1 || st.Stage.Slides != 2 || st.Stage.Group != "Lions" {
		t.Fatalf("presenting = %+v", st.Stage)
	}

	// Classmates follow; only the presenter turns.
	myStage := "/api/my/classrooms/" + f.class.ID + "/stage"
	chen.Call("GET", myStage, nil, 200, &st)
	if st.Stage == nil || st.Stage.Presenting || st.Stage.Mine || st.Stage.PresenterName != amaya.User.Name {
		t.Fatalf("Chen sees %+v", st.Stage)
	}
	chen.Call("GET", "/api/my/group-presentations/"+x.ID+"/slides/2", nil, 200, nil)
	chen.Call("PUT", myStage+"/slide", map[string]int{"n": 2}, 403, nil)
	ben.Call("PUT", myStage+"/slide", map[string]int{"n": 2}, 403, nil)
	amaya.Call("PUT", myStage+"/slide", map[string]int{"n": 3}, 422, nil)
	amaya.Call("PUT", myStage+"/slide", map[string]int{"n": 2}, 204, nil)
	other.Call("GET", myStage, nil, 404, nil)
	chen.Call("GET", myStage, nil, 200, &st)
	if st.Stage.Slide != 2 {
		t.Fatalf("Chen is on %d", st.Stage.Slide)
	}
	// Re-uploading while on the screen is refused.
	ben.Call("POST", start, map[string]any{"group_id": lions, "file_name": "v2.pdf", "slides": 1}, 409, nil)

	// Ratings: classmates, after it's shown; not its own group.
	rate := "/api/my/group-presentations/" + x.ID + "/rating"
	amaya.Call("PUT", rate, map[string]int{"stars": 5}, 403, nil)
	chen.Call("PUT", rate, map[string]int{"stars": 6}, 422, nil)
	chen.Call("PUT", rate, map[string]int{"stars": 4}, 204, nil)
	dev.Call("PUT", rate, map[string]int{"stars": 2}, 204, nil)

	// The teacher takes over on the same slide, then stops it.
	f.teacher.Call("POST", "/api/teacher/group-presentations/"+x.ID+"/show", nil, 204, nil)
	var tv grading.TaskView
	f.teacher.Call("GET", taskURL, nil, 200, &tv)
	if tv.Stage == nil || tv.Stage.PresenterName != "" || tv.Stage.Slide != 2 {
		t.Fatalf("taken over: %+v", tv.Stage)
	}
	amaya.Call("PUT", myStage+"/slide", map[string]int{"n": 1}, 403, nil)
	f.teacher.Call("PUT", "/api/teacher/classrooms/"+f.class.ID+"/stage/slide", map[string]int{"n": 1}, 204, nil)
	f.teacher.Call("DELETE", "/api/teacher/classrooms/"+f.class.ID+"/stage", nil, 204, nil)
	chen.Call("GET", myStage, nil, 200, &st)
	if st.Stage != nil {
		t.Fatalf("still on: %+v", st.Stage)
	}
	chen.Call("GET", "/api/my/group-presentations/"+x.ID+"/slides/1", nil, 404, nil)

	// One at a time: the Tigers can't present over the Lions.
	var tg grading.GroupPres
	chen.Call("POST", start, map[string]any{"group_id": tigers, "title": "Ash", "file_name": "ash.pdf", "slides": 1}, 201, &tg)
	putPresSlide(t, chen, tg.ID, 1, 204)
	chen.Call("POST", "/api/my/group-presentations/"+tg.ID+"/finish", nil, 200, nil)
	f.teacher.Call("PUT", "/api/teacher/group-presentations/"+tg.ID+"/allowed", map[string]bool{"allowed": true}, 204, nil)
	amaya.Call("POST", present, nil, 200, nil)
	chen.Call("POST", "/api/my/group-presentations/"+tg.ID+"/present", nil, 409, nil)
	amaya.Call("DELETE", myStage, nil, 204, nil)
	amaya.Call("DELETE", myStage, nil, 403, nil)
	chen.Call("POST", "/api/my/group-presentations/"+tg.ID+"/present", nil, 200, nil)
	// No longer allowed: off the screen.
	f.teacher.Call("PUT", "/api/teacher/group-presentations/"+tg.ID+"/allowed", map[string]bool{"allowed": false}, 204, nil)
	chen.Call("GET", myStage, nil, 200, &st)
	if st.Stage != nil {
		t.Fatalf("still on after permission went: %+v", st.Stage)
	}

	// Marks, in the class report for every member of the group.
	mark := "/api/teacher/group-presentations/" + x.ID + "/mark"
	f.teacher.Call("PUT", mark, map[string]any{"mark": 21}, 422, nil)
	f.teacher.Call("PUT", mark, map[string]any{"mark": 15, "comment": "Clear"}, 204, nil)
	f.teacher.Call("GET", taskURL, nil, 200, &tv)
	var lionsRow *grading.GroupPres
	for _, g := range tv.Groups {
		if g.ID == lions {
			lionsRow = g.Presentation
		}
	}
	if lionsRow == nil || lionsRow.Mark == nil || *lionsRow.Mark != 15 || lionsRow.Ratings != 2 || *lionsRow.Rating != 3 || lionsRow.PresentedAt == nil {
		t.Fatalf("Lions = %+v", lionsRow)
	}
	var r grading.ClassReport
	f.teacher.Call("GET", "/api/teacher/classrooms/"+f.class.ID+"/class-report", nil, 200, &r)
	found := false
	for _, it := range r.Items {
		found = found || (it.Kind == "presentation" && it.ID == task.ID && it.Topic == "T")
	}
	if !found {
		t.Fatalf("items: %+v", r.Items)
	}
	for _, s := range r.Students {
		m, ok := s.Marks["presentation:"+task.ID]
		switch s.UserID {
		case amaya.User.ID, ben.User.ID:
			if !ok || m.Score != 15 || m.Pct != 75 {
				t.Fatalf("%s: %+v", s.Name, m)
			}
		case chen.User.ID, dev.User.ID:
			if ok {
				t.Fatalf("%s has %+v", s.Name, m)
			}
		}
	}
	f.teacher.Call("PUT", taskURL, map[string]any{"title": "Volcano talks", "out_of": 10}, 422, nil)

	// Deleting uploads later: the slides go, the mark stays.
	f.teacher.Call("DELETE", "/api/teacher/group-presentations/"+x.ID+"/slides", nil, 204, nil)
	f.teacher.Call("GET", "/api/teacher/group-presentations/"+x.ID+"/slides/1", nil, 404, nil)
	mt = myTask(t, ben, f.class.ID, task.ID)
	if p := mt.MyGroups[0].Presentation; p == nil || !p.FilesRemoved || p.Ready || p.Allowed || p.Mark == nil {
		t.Fatalf("after removing slides: %+v", p)
	}
	amaya.Call("POST", present, nil, 403, nil)
	var removed map[string]int
	f.teacher.Call("DELETE", taskURL+"/slides", nil, 200, &removed)
	if removed["removed"] != 1 { // the Tigers'; the Lions' were gone already
		t.Fatalf("removed = %v", removed)
	}
	// Or the whole presentation, with its mark.
	f.teacher.Call("DELETE", "/api/teacher/group-presentations/"+tg.ID, nil, 204, nil)
	f.teacher.Call("GET", taskURL, nil, 200, &tv)
	for _, g := range tv.Groups {
		if g.ID == tigers && g.Presentation != nil {
			t.Fatalf("Tigers' still there: %+v", g.Presentation)
		}
	}
	// And the task.
	f.teacher.Call("DELETE", taskURL, nil, 204, nil)
	f.teacher.Call("GET", taskURL, nil, 404, nil)
	var n int
	if err := f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM grading.pres_submissions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("left: %d %v", n, err)
	}
}

// PowerPoint needs Drive; without it, students are told to upload a PDF.
func TestClassPresentationConvert(t *testing.T) {
	f := newReportFx(t)
	s := f.student(t)
	code, _ := s.Raw("POST", "/api/my/classrooms/"+f.class.ID+"/presentations/convert", "application/octet-stream", []byte("PK"))
	if code != 409 && code != 400 && code != 422 && code != 502 {
		t.Fatalf("convert = %d", code)
	}
	other := f.e.NewUser(auth.RoleStudent)
	if code, _ := other.Raw("POST", "/api/my/classrooms/"+f.class.ID+"/presentations/convert", "application/octet-stream", []byte("PK")); code != 404 {
		t.Fatalf("not enrolled: %d", code)
	}
}
