package presentation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/presentation"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

type fx struct {
	e       *apptest.Env
	teacher *apptest.Client
	topic   content.Topic
	class   content.Classroom
}

func setup(t *testing.T) *fx {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 9"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "Biology"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "Cells"}, 201, &topic)
	return &fx{e: e, teacher: teacher, topic: topic, class: c}
}

func (f *fx) student() *apptest.Client {
	s := f.e.NewUser(auth.RoleStudent)
	s.Call("POST", "/api/enrolments", map[string]string{"join_code": f.class.JoinCode}, 200, nil)
	return s
}

func slideJPEG(t *testing.T, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, h/2, color.RGBA{200, 50, 50, 255})
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func slideForm(t *testing.T, img []byte, text string) (string, []byte) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile("image", "slide.jpg")
	fw.Write(img)
	mw.WriteField("text", text)
	mw.Close()
	return mw.FormDataContentType(), b.Bytes()
}

func status(t *testing.T, c *apptest.Client, method, path, ctype string, body []byte, want int) string {
	t.Helper()
	got, raw := c.Raw(method, path, ctype, body)
	if got != want {
		t.Fatalf("%s %s: %d, want %d: %s", method, path, got, want, raw)
	}
	var e struct {
		Code   string
		Fields map[string]string
	}
	_ = json.Unmarshal(raw, &e)
	for k := range e.Fields {
		return k
	}
	return e.Code
}

const pdf = "%PDF-1.4\n% a test file\n%%EOF\n"

// upload sends a PDF of n slides and finishes it.
func (f *fx) upload(t *testing.T, id string, n int) presentation.Presentation {
	t.Helper()
	var v struct{ Version int }
	f.teacher.Call("POST", "/api/teacher/presentations/"+id+"/uploads", map[string]any{"name": "Cells.pdf", "slides": n}, 201, &v)
	base := fmt.Sprintf("/api/teacher/presentations/%s/uploads/%d", id, v.Version)
	status(t, f.teacher, "PUT", base+"/pdf", "application/pdf", []byte(pdf), 204)
	for i := 1; i <= n; i++ {
		ct, body := slideForm(t, slideJPEG(t, 160, 90), fmt.Sprintf("Slide %d text", i))
		status(t, f.teacher, "PUT", fmt.Sprintf("%s/slides/%d", base, i), ct, body, 204)
	}
	var p presentation.Presentation
	f.teacher.Call("POST", base+"/finish", nil, 200, &p)
	return p
}

func TestUploadAndPublish(t *testing.T) {
	f := setup(t)
	var p presentation.Presentation
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/presentations", map[string]any{"title": " "}, 422, nil)
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/presentations", map[string]any{"title": "Inside  a cell"}, 201, &p)
	if p.Title != "Inside a cell" || p.Status != "draft" || p.Slides != 0 || !p.AllowJump || p.AllowDownload {
		t.Fatalf("created = %+v", p)
	}
	path := "/api/teacher/presentations/" + p.ID
	if c := status(t, f.teacher, "POST", path+"/status", "application/json", []byte(`{"status":"published"}`), 409); c != "presentation_empty" {
		t.Fatal(c)
	}

	// Uploading: checked as it comes in, and only swapped in once whole.
	f.teacher.Call("POST", path+"/uploads", map[string]any{"name": "deck.pptx", "slides": 2}, 422, nil)
	f.teacher.Call("POST", path+"/uploads", map[string]any{"name": "deck.pdf", "slides": 301}, 422, nil)
	var v struct{ Version int }
	f.teacher.Call("POST", path+"/uploads", map[string]any{"name": "C:\\Users\\me\\deck.pdf", "slides": 2}, 201, &v)
	up := fmt.Sprintf("%s/uploads/%d", path, v.Version)
	if c := status(t, f.teacher, "PUT", up+"/pdf", "application/pdf", []byte("<html>"), 422); c != "file" {
		t.Fatal(c)
	}
	status(t, f.teacher, "PUT", up+"/pdf", "application/pdf", []byte(pdf), 204)
	ct, body := slideForm(t, []byte("not a picture"), "")
	if c := status(t, f.teacher, "PUT", up+"/slides/1", ct, body, 422); c != "image" {
		t.Fatal(c)
	}
	ct, body = slideForm(t, slideJPEG(t, 160, 90), "Cells   are small")
	status(t, f.teacher, "PUT", up+"/slides/3", ct, body, 404)
	status(t, f.teacher, "PUT", up+"/slides/1", ct, body, 204)
	if c := status(t, f.teacher, "POST", up+"/finish", "application/json", nil, 409); c != "upload_incomplete" {
		t.Fatal(c)
	}
	status(t, f.teacher, "PUT", up+"/slides/2", ct, body, 204)
	f.teacher.Call("POST", up+"/finish", nil, 200, &p)
	if p.Slides != 2 || p.Version != v.Version || p.FileName != "deck.pdf" || p.FileBytes != len(pdf) {
		t.Fatalf("finished = %+v", p)
	}
	status(t, f.teacher, "POST", up+"/finish", "application/json", nil, 409) // done already
	var sl struct{ Slides []presentation.Slide }
	f.teacher.Call("GET", path+"/slides", nil, 200, &sl)
	if len(sl.Slides) != 2 || sl.Slides[0].Width != 160 || sl.Slides[0].Text != "Cells are small" {
		t.Fatalf("slides = %+v", sl.Slides)
	}
	code, h, img := f.teacher.Get(fmt.Sprintf("%s/slides/1?v=%d", path, p.Version), nil)
	if code != 200 || h.Get("Content-Type") != "image/jpeg" || len(img) == 0 || h.Get("Cache-Control") != "private, max-age=86400, immutable" {
		t.Fatalf("image: %d %v", code, h)
	}

	// Someone else's: invisible.
	other := f.e.NewUser(auth.RoleTeacher)
	other.Call("GET", path, nil, 404, nil)
	other.Call("POST", path+"/uploads", map[string]any{"name": "x.pdf", "slides": 1}, 404, nil)
	if code, _, _ := other.Get(path+"/slides/1", nil); code != 404 {
		t.Fatalf("other teacher's image: %d", code)
	}

	f.teacher.Call("POST", path+"/status", map[string]string{"status": "published"}, 200, &p)
	if p.Status != "published" || p.PublishedAt == nil {
		t.Fatalf("published = %+v", p)
	}
	var list struct{ Presentations []presentation.Presentation }
	f.teacher.Call("GET", "/api/teacher/topics/"+f.topic.ID+"/presentations", nil, 200, &list)
	if len(list.Presentations) != 1 {
		t.Fatalf("list = %+v", list)
	}
	f.teacher.Call("DELETE", path, nil, 204, nil)
	f.teacher.Call("GET", path, nil, 404, nil)
}

func TestViewing(t *testing.T) {
	f := setup(t)
	var p presentation.Presentation
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/presentations", map[string]any{"title": "Cells"}, 201, &p)
	p = f.upload(t, p.ID, 3)
	tpath := "/api/teacher/presentations/" + p.ID
	f.teacher.Call("PUT", tpath, map[string]any{"allow_jump": false}, 200, nil)
	s := f.student()
	path := "/api/presentations/" + p.ID

	s.Call("POST", path+"/open", nil, 404, nil) // a draft
	f.teacher.Call("POST", tpath+"/status", map[string]string{"status": "published"}, 200, nil)
	f.e.NewUser(auth.RoleStudent).Call("POST", path+"/open", nil, 404, nil)
	var mine struct{ Presentations []presentation.Mine }
	s.Call("GET", "/api/my/presentations", nil, 200, &mine)
	if len(mine.Presentations) != 1 || mine.Presentations[0].Slides != 3 || mine.Presentations[0].Started {
		t.Fatalf("mine = %+v", mine)
	}

	var v presentation.View
	s.Call("POST", path+"/open", nil, 200, &v)
	if v.Current != 1 || len(v.Slides) != 3 || v.Presentation.AllowJump || v.Finished || v.Slides[2].Text != "Slide 3 text" {
		t.Fatalf("open = %+v", v)
	}
	// In order: the next slide may load ahead; the one after can't be seen yet.
	img := func(n, want int) {
		t.Helper()
		if code, _, _ := s.Get(fmt.Sprintf("%s/slides/%d?v=%d", path, n, p.Version), nil); code != want {
			t.Fatalf("slide %d: %d, want %d", n, code, want)
		}
	}
	img(1, 200)
	img(2, 200)
	img(3, 404)
	if c := status(t, s, "POST", path+"/go", "application/json", []byte(`{"to":3}`), 409); c != "in_order" {
		t.Fatal(c)
	}

	// Time: at most what has passed, and a minute a report.
	rewind := func(secs int) {
		if _, err := f.e.Pool.Exec(context.Background(), `UPDATE presentation.views SET last_at = now() - make_interval(secs => $1)`, secs); err != nil {
			t.Fatal(err)
		}
	}
	rewind(40)
	s.Call("POST", path+"/time", map[string]int{"slide": 1, "seconds": 30}, 204, nil)
	rewind(600)
	s.Call("POST", path+"/time", map[string]int{"slide": 1, "seconds": 500}, 204, nil)
	s.Call("POST", path+"/time", map[string]int{"slide": 2, "seconds": 30}, 204, nil) // not where they are: ignored
	s.Call("POST", path+"/go", map[string]int{"to": 2}, 200, &v)
	s.Call("POST", path+"/go", map[string]int{"to": 3}, 200, &v)
	if !v.Finished || v.Reached != 3 {
		t.Fatalf("at the end = %+v", v)
	}
	s.Call("POST", path+"/go", map[string]int{"to": 1}, 200, &v) // back is fine
	s.Call("POST", path+"/open", nil, 200, &v)
	if v.Current != 1 || !v.Finished {
		t.Fatalf("resume = %+v", v)
	}

	// The teacher's report.
	var rep presentation.Report
	f.teacher.Call("GET", tpath+"/report", nil, 200, &rep)
	st := rep.Students[0]
	if len(rep.Students) != 1 || !st.Finished || st.Seen != 3 || st.Seconds < 90 || st.Seconds > 93 {
		t.Fatalf("students = %+v", rep.Students)
	}
	if rep.Slides[0].Viewers != 1 || rep.Slides[0].MedianSeconds < 90 || rep.Slides[2].Viewers != 1 {
		t.Fatalf("slides = %+v", rep.Slides)
	}
	var d presentation.ViewerDetail
	f.teacher.Call("GET", tpath+"/viewers/"+st.ViewerID, nil, 200, &d)
	if len(d.Slides) != 3 || d.Slides[0].Seconds < 90 || d.Slides[1].FirstAt == nil {
		t.Fatalf("detail = %+v", d)
	}
	f.e.NewUser(auth.RoleTeacher).Call("GET", tpath+"/report", nil, 404, nil)

	// Downloading only when allowed.
	if code, _, _ := s.Get(path+"/pdf", nil); code != 404 {
		t.Fatalf("download not allowed: %d", code)
	}
	f.teacher.Call("PUT", tpath, map[string]any{"allow_download": true}, 200, nil)
	code, h, body := s.Get(path+"/pdf", nil)
	if code != 200 || h.Get("Content-Type") != "application/pdf" || string(body) != pdf {
		t.Fatalf("download: %d %v", code, h)
	}

	// A shorter file replaces it: old slides go, the student stays within it.
	s.Call("POST", path+"/go", map[string]int{"to": 3}, 200, nil)
	old := p.Version
	p = f.upload(t, p.ID, 2)
	if code, _, _ := s.Get(fmt.Sprintf("%s/slides/1?v=%d", path, old), nil); code != 404 {
		t.Fatalf("old slide: %d", code)
	}
	s.Call("POST", path+"/open", nil, 200, &v)
	if v.Current != 2 || len(v.Slides) != 2 || v.Presentation.Version != p.Version {
		t.Fatalf("after replacing = %+v", v)
	}

	// Not open yet.
	f.teacher.Call("PUT", tpath, map[string]any{"opens_at": time.Now().Add(time.Hour)}, 200, nil)
	if c := status(t, s, "POST", path+"/open", "application/json", nil, 403); c != "presentation_not_open" {
		t.Fatal(c)
	}
}
