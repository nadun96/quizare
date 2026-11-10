package poll_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/poll"
)

// jpeg is the smallest thing that starts like a JPEG.
var jpeg = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0, 0xFF, 0xD9}

func (p *player) putSlide(id string, n int, img []byte, want int) {
	p.f.e.T.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("image", "slide.jpg")
	_, _ = fw.Write(img)
	_ = w.WriteField("text", "Slide text")
	_ = w.Close()
	code, body := p.c.Raw("PUT", "/api/polls/"+p.f.poll.JoinCode+"/presentations/"+id+"/slides/"+itoa(n), w.FormDataContentType(), buf.Bytes())
	if code != want {
		p.f.e.T.Fatalf("slide %d: %d %s", n, code, body)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// submit uploads a presentation of n slides and says it's done.
func (p *player) submit(title string, n int) poll.Submission {
	p.f.e.T.Helper()
	var x poll.Submission
	p.c.Call("POST", "/api/polls/"+p.f.poll.JoinCode+"/presentations", map[string]any{"title": title, "file_name": title + ".pdf", "slides": n}, 201, &x)
	for i := 1; i <= n; i++ {
		p.putSlide(x.ID, i, jpeg, 204)
	}
	p.c.Call("POST", "/api/polls/"+p.f.poll.JoinCode+"/presentations/"+x.ID+"/finish", nil, 200, &x)
	return x
}

// D-165: a poll asks for presentations, one a poll group; the leader uploads,
// the teacher lets one present, the presenter turns the slides, everyone
// follows; the teacher marks it and the audience rates it.
func TestPollPresentationsByGroup(t *testing.T) {
	f := setup(t, grouped("self", "all", "average"), capitalQ)
	var red, blue poll.Group
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups", map[string]any{"name": "Red"}, 201, &red)
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups", map[string]any{"name": "Blue"}, 201, &blue)
	ann := f.joinGroup(t, "Ann", red.ID) // first in: Red's captain
	ben := f.joinGroup(t, "Ben", red.ID)
	cat := f.joinGroup(t, "Cat", blue.ID)
	url := "/api/teacher/polls/" + f.poll.ID + "/presentations"

	// Not asked for yet.
	ann.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/presentations", map[string]any{"title": "x", "slides": 1}, 409, nil)
	f.teacher.Call("PUT", url, map[string]any{"mode": "class_group"}, 422, nil) // no classroom
	f.teacher.Call("PUT", url, map[string]any{"mode": "poll_group", "upload_by": "leader", "out_of": 20, "rating": true, "instructions": "5 minutes each"}, 200, nil)
	f.e.NewUser(auth.RoleTeacher).Call("PUT", url, map[string]any{"mode": "student"}, 404, nil)
	if v := f.view(ann.c); v.Presentations != "poll_group" {
		t.Fatalf("view: %+v", v.Presentations)
	}
	var mine poll.MyPresentations
	ben.c.Call("GET", "/api/polls/"+f.poll.JoinCode+"/presentations", nil, 200, &mine)
	if len(mine.Owners) != 1 || mine.Owners[0].Name != "Red" || mine.CanUpload[red.ID] || mine.Instructions != "5 minutes each" {
		t.Fatalf("Ben's view: %+v", mine)
	}
	// Only the leader uploads.
	ben.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/presentations", map[string]any{"title": "Ours", "slides": 2}, 403, nil)
	ann.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/presentations", map[string]any{"title": "Ours", "slides": 0}, 422, nil)
	var x poll.Submission
	ann.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/presentations", map[string]any{"title": "Volcanoes", "file_name": "volcanoes.pdf", "slides": 3}, 201, &x)
	ann.putSlide(x.ID, 1, []byte("not a picture"), 422)
	ann.putSlide(x.ID, 4, jpeg, 422)
	ann.putSlide(x.ID, 1, jpeg, 204)
	ann.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/presentations/"+x.ID+"/finish", nil, 422, nil) // 1 of 3
	ann.putSlide(x.ID, 2, jpeg, 204)
	ann.putSlide(x.ID, 3, jpeg, 204)
	ann.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/presentations/"+x.ID+"/finish", nil, 200, &x)
	if !x.Ready || x.Slides != 3 {
		t.Fatalf("ready: %+v", x)
	}
	// Members see their own slides; others don't, until it's on the screen.
	if code, _, _ := ben.c.Get("/api/polls/"+f.poll.JoinCode+"/presentations/"+x.ID+"/slides/1", nil); code != 200 {
		t.Fatalf("a member's own slide: %d", code)
	}
	if code, _, _ := cat.c.Get("/api/polls/"+f.poll.JoinCode+"/presentations/"+x.ID+"/slides/1", nil); code != 404 {
		t.Fatalf("someone else's slide before presenting: %d", code)
	}
	cat.f.e.T.Helper()
	var blueSub poll.Submission
	cat.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/presentations", map[string]any{"title": "Rivers", "slides": 1}, 201, &blueSub)

	// The teacher's list.
	var tv struct {
		Settings    poll.PresSettings
		Submissions []poll.Submission
		Stage       *poll.Stage
	}
	f.teacher.Call("GET", url, nil, 200, &tv)
	if len(tv.Submissions) != 2 || tv.Submissions[0].Owner.Name != "Red" || tv.Submissions[0].UploadedBy == "" || tv.Stage != nil {
		t.Fatalf("teacher list: %+v", tv)
	}
	f.teacher.Call("PUT", "/api/teacher/polls/"+f.poll.ID+"/stage", map[string]any{"submission_id": blueSub.ID}, 409, nil) // not all in

	// Let Red present: Ann controls the slides; everyone follows.
	audience := cat.c.Dial("/ws/polls/" + f.poll.JoinCode)
	apptest.ReadUntil(t, audience, "update", 3*time.Second)
	f.teacher.Call("PUT", "/api/teacher/polls/"+f.poll.ID+"/stage", map[string]any{"submission_id": x.ID}, 204, nil)
	f.e.App.Poll.Flush(context.Background())
	var u poll.PublicPoll
	raw, _ := json.Marshal(apptest.ReadUntil(t, audience, "update", 3*time.Second))
	if err := json.Unmarshal(raw, &u); err != nil || u.Stage == nil || u.Stage.SubmissionID != x.ID || u.Stage.Slide != 1 || u.Stage.Owner != "Red" {
		t.Fatalf("stage pushed: %+v %v", u.Stage, err)
	}
	if v := f.view(ann.c); v.Stage.PresenterKey != v.MeKey || v.Stage.PresenterKey == "" {
		t.Fatalf("Ann presents: %+v / %s", v.Stage, v.MeKey)
	}
	if code, _, _ := cat.c.Get("/api/polls/"+f.poll.JoinCode+"/presentations/"+x.ID+"/slides/2", nil); code != 200 {
		t.Fatalf("the audience sees what's on the screen: %d", code)
	}
	ben.c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/stage/slide", map[string]int{"n": 2}, 403, nil) // not the presenter
	ann.c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/stage/slide", map[string]int{"n": 4}, 422, nil)
	ann.c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/stage/slide", map[string]int{"n": 2}, 204, nil)
	if v := f.view(cat.c); v.Stage.Slide != 2 {
		t.Fatalf("followed: %d", v.Stage.Slide)
	}
	// The teacher takes over at any time, and can hand it to Ben.
	f.teacher.Call("PUT", "/api/teacher/polls/"+f.poll.ID+"/stage/slide", map[string]int{"n": 3}, 204, nil)
	var benID string
	for _, g := range f.groups().Groups {
		for _, m := range g.Members {
			if m.Nickname == "Ben" {
				benID = m.ParticipantID
			}
		}
	}
	f.teacher.Call("PUT", "/api/teacher/polls/"+f.poll.ID+"/stage", map[string]any{"submission_id": x.ID, "presenter": benID}, 204, nil)
	ben.c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/stage/slide", map[string]int{"n": 2}, 204, nil)
	ann.c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/stage/slide", map[string]int{"n": 1}, 403, nil)
	// Not replaced while it's on the screen.
	ann.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/presentations", map[string]any{"title": "Volcanoes 2", "slides": 2}, 409, nil)

	// Ratings: the audience, not its own group.
	ann.c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/presentations/"+x.ID+"/rating", map[string]int{"stars": 5}, 403, nil)
	cat.c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/presentations/"+x.ID+"/rating", map[string]int{"stars": 6}, 422, nil)
	cat.c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/presentations/"+x.ID+"/rating", map[string]int{"stars": 4}, 204, nil)
	cat.c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/presentations/"+blueSub.ID+"/rating", map[string]int{"stars": 4}, 409, nil) // not presented

	f.teacher.Call("DELETE", "/api/teacher/polls/"+f.poll.ID+"/stage", nil, 204, nil)
	if v := f.view(cat.c); v.Stage != nil {
		t.Fatal("stage cleared")
	}
	if code, _, _ := cat.c.Get("/api/polls/"+f.poll.JoinCode+"/presentations/"+x.ID+"/slides/1", nil); code != 404 {
		t.Fatalf("off the screen again: %d", code)
	}

	// Marks.
	mark := func(id string, m any, want int) {
		t.Helper()
		f.teacher.Call("PUT", "/api/teacher/poll-submissions/"+id+"/mark", map[string]any{"mark": m, "comment": "Clear"}, want, nil)
	}
	mark(x.ID, 21, 422)
	mark(x.ID, 17.5, 204)
	f.teacher.Call("GET", url, nil, 200, &tv)
	got := tv.Submissions[0]
	if got.Mark == nil || *got.Mark != 17.5 || got.Rating == nil || *got.Rating != 4 || got.Ratings != 1 || got.PresentedAt == nil {
		t.Fatalf("marked: %+v", got)
	}
	if code, _, _ := f.teacher.Get("/api/teacher/poll-submissions/"+x.ID+"/slides/3", nil); code != 200 {
		t.Fatalf("teacher slide: %d", code)
	}
	f.teacher.Call("DELETE", "/api/teacher/poll-submissions/"+blueSub.ID, nil, 204, nil)
}

// D-165: one presentation a student; PowerPoint needs Drive.
func TestPollPresentationsByStudent(t *testing.T) {
	f := setup(t, settings("anonymous", "self", "live"), singleQ)
	f.teacher.Call("PUT", "/api/teacher/polls/"+f.poll.ID+"/presentations", map[string]any{"mode": "student"}, 200, nil)
	ann, ben := f.join(t, "Ann"), f.join(t, "Ben")
	a := ann.submit("Mine", 2)
	b := ben.submit("Also mine", 1)
	if a.ID == b.ID {
		t.Fatal("one each")
	}
	// Uploading again replaces it.
	again := ann.submit("Mine, better", 1)
	if again.ID != a.ID || again.Slides != 1 {
		t.Fatalf("replaced: %+v", again)
	}
	f.teacher.Call("PUT", "/api/teacher/poll-submissions/"+a.ID+"/mark", map[string]any{"mark": 1}, 422, nil) // not marked out of anything
	code, _ := ann.c.Raw("POST", "/api/polls/"+f.poll.JoinCode+"/presentations/convert", "application/octet-stream", []byte("PK"))
	if code == 200 {
		t.Fatal("converted without Drive")
	}
}
