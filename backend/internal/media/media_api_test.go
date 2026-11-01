package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/media"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func picture(t *testing.T, w, h int, alpha uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = alpha
	}
	img.Set(0, 0, color.NRGBA{255, 0, 0, alpha})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Teachers put pictures in text; they're redrawn, served to anyone with the
// id, and cleared away once nothing uses them (D-113).
func TestImages(t *testing.T) {
	e := apptest.New(t)
	teacher, student := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleStudent)

	// A wide photo comes back at most 1600 px wide, as a JPEG.
	code, body := teacher.Raw("POST", "/api/teacher/images", "image/png", picture(t, 2400, 600, 255))
	var img media.Image
	if code != 201 || json.Unmarshal(body, &img) != nil || img.Width != 1600 || img.Height != 400 || img.URL != "/api/images/"+img.ID {
		t.Fatalf("upload = %d %s", code, body)
	}
	// Transparency keeps PNG.
	code, body = teacher.Raw("POST", "/api/teacher/images", "image/png", picture(t, 40, 40, 128))
	var clear media.Image
	if code != 201 || json.Unmarshal(body, &clear) != nil {
		t.Fatalf("transparent = %d %s", code, body)
	}

	// Anyone with the id sees it, even logged out; it's cached for good.
	status, h, data := e.Client().Get(img.URL, nil)
	if status != 200 || h.Get("Content-Type") != "image/jpeg" || !strings.Contains(h.Get("Cache-Control"), "immutable") || h.Get("X-Content-Type-Options") != "nosniff" || len(data) == 0 {
		t.Fatalf("get = %d %v", status, h)
	}
	if status, h, _ := e.Client().Get(clear.URL, nil); status != 200 || h.Get("Content-Type") != "image/png" {
		t.Fatalf("png = %d %v", status, h)
	}
	if status, _, _ := e.Client().Get("/api/images/00000000-0000-0000-0000-000000000000", nil); status != 404 {
		t.Fatalf("missing = %d", status)
	}

	// Not pictures, too big, or not a teacher: refused.
	if code, body := teacher.Raw("POST", "/api/teacher/images", "image/svg+xml", []byte(`<svg onload="alert(1)"/>`)); code != 422 || !strings.Contains(string(body), "PNG, JPEG") {
		t.Fatalf("svg = %d %s", code, body)
	}
	if code, _ := teacher.Raw("POST", "/api/teacher/images", "image/png", make([]byte, media.MaxUpload+10)); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big = %d", code)
	}
	if code, _ := student.Raw("POST", "/api/teacher/images", "image/png", picture(t, 10, 10, 255)); code != 403 {
		t.Fatalf("student = %d", code)
	}

	// The clean-up keeps a picture a question uses and deletes the other.
	ctx := context.Background()
	if _, err := e.Pool.Exec(ctx, `UPDATE media.images SET created_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	var c struct{ ID, JoinCode string }
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m, tp, q struct{ ID string }
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &tp)
	teacher.Call("POST", "/api/teacher/topics/"+tp.ID+"/quizzes", map[string]any{"title": "Q"}, 201, &q)
	if code, body := teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv",
		[]byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,What is ![A cell]("+img.URL+")?,A|B,A,1\n")); code >= 300 {
		t.Fatalf("import = %d %s", code, body)
	}
	n, _, err := e.App.Media.PreviewUnused(ctx, time.Now().Add(-time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("unused = %d %v", n, err)
	}
	if n, _, err := e.App.Media.DeleteUnused(ctx, time.Now().Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("deleted = %d %v", n, err)
	}
	if status, _, _ := e.Client().Get(img.URL, nil); status != 200 {
		t.Fatal("the used picture went")
	}
	if status, _, _ := e.Client().Get(clear.URL, nil); status != 404 {
		t.Fatal("the unused picture stayed")
	}
}
