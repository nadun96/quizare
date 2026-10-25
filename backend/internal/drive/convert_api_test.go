package drive_test

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/drive"
)

// D-78: PowerPoint becomes a PDF through the teacher's Drive, and the copy
// made there is deleted.
func TestSlidesToPDF(t *testing.T) {
	f := setup(t)
	path := "/api/teacher/drive/slides-pdf"
	f.teacher.Headers = map[string]string{"X-File-Name": url.QueryEscape("Cells and organelles.pptx")}
	if code, _ := f.teacher.Raw("POST", path, "application/octet-stream", []byte("PK fake pptx")); code != 409 && code != 404 && code != 403 {
		t.Fatalf("without Drive connected: %d", code)
	}
	f.connect(t, f.teacher)

	code, body := f.teacher.Raw("POST", path, "application/octet-stream", []byte("PK fake pptx"))
	if code != 200 || !bytes.HasPrefix(body, []byte("%PDF-")) || !strings.Contains(string(body), "Cells and organelles") {
		t.Fatalf("converted: %d %q", code, body)
	}
	if len(f.g.Converted) != 1 || len(f.g.Files("")) != 0 {
		t.Fatalf("the copy in Drive should be gone: converted %v, left %d", f.g.Converted, len(f.g.Files("")))
	}
	f.teacher.Headers = map[string]string{"X-File-Name": "notes.docx"}
	if code, raw := f.teacher.Raw("POST", path, "application/octet-stream", []byte("PK")); code != 422 || !strings.Contains(string(raw), "PowerPoint") {
		t.Fatalf("not a presentation: %d %s", code, raw)
	}

	// Big decks: Drive's export call refuses, the export link answers.
	f.g.ExportLimit = 10
	f.teacher.Headers = map[string]string{"X-File-Name": "big.ppt"}
	if code, body := f.teacher.Raw("POST", path, "application/octet-stream", []byte("old ppt")); code != 200 || !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatalf("through the export link: %d %q", code, body)
	}
	f.g.ExportLimit = 0
	f.teacher.Headers = nil

	// Picked in Drive: Google Slides exported, a PDF as it is, PowerPoint converted.
	slides := f.g.Add("Lesson 4", drive.MimeSlides, []byte("%PDF-1.7 slides"))
	pdf := f.g.Add("Handout.pdf", drive.MimePDF, []byte("%PDF-1.5 handout"))
	pptx := f.g.Add("Old deck.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", []byte("PK pptx"))
	doc := f.g.Add("Essay", "application/vnd.google-apps.document", []byte("words"))
	for id, want := range map[string]string{slides: "%PDF-1.7 slides", pdf: "%PDF-1.5 handout", pptx: "% converted from Old deck"} {
		code, body := f.teacher.Raw("POST", "/api/teacher/drive/files/"+id+"/slides-pdf", "application/json", nil)
		if code != 200 || !strings.Contains(string(body), want) {
			t.Fatalf("%s: %d %q", id, code, body)
		}
	}
	if code, _ := f.teacher.Raw("POST", "/api/teacher/drive/files/"+doc+"/slides-pdf", "application/json", nil); code != 422 {
		t.Fatalf("a document isn't a presentation: %d", code)
	}
	if code, _ := f.e.NewUser(auth.RoleTeacher).Raw("POST", "/api/teacher/drive/files/"+slides+"/slides-pdf", "application/json", nil); code == 200 {
		t.Fatal("another teacher's Drive")
	}
}

// D-78: a presentation's viewing records saved to Drive, as session results are.
func TestPresentationToDrive(t *testing.T) {
	f := setup(t)
	f.connect(t, f.teacher)
	var c content.Classroom
	f.teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m content.Module
	f.teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	f.teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var p struct{ ID string }
	f.teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/presentations", map[string]any{"title": "Cells"}, 201, &p)
	var e drive.Export
	f.teacher.Call("POST", "/api/teacher/presentations/"+p.ID+"/drive-export", nil, 201, &e)
	if e.Kind != "presentation" || e.Status != "done" || e.Name != "Cells – viewing" || e.WebLink == "" {
		t.Fatalf("export = %+v", e)
	}
	f.e.NewUser(auth.RoleTeacher).Call("POST", "/api/teacher/presentations/"+p.ID+"/drive-export", nil, 404, nil)
}
