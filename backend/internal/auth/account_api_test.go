package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
)

func picture(t *testing.T, w, h int, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var b bytes.Buffer
	if format == "png" {
		_ = png.Encode(&b, img)
	} else {
		_ = jpeg.Encode(&b, img, nil)
	}
	return b.Bytes()
}

// withExif puts an APP1 Exif segment holding a fake GPS tag after the JPEG's SOI.
func withExif(j []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), []byte("GPSLatitude 6.9271 N")...)
	seg := append([]byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
	return append(append(append([]byte{}, j[:2]...), seg...), j[2:]...)
}

func avatarGet(c *apptest.Client, id, query string) (int, http.Header, []byte) {
	return c.Get("/api/auth/users/"+id+"/avatar"+query, nil)
}

func TestAvatarUploadPrivacyAndLimits(t *testing.T) {
	e := apptest.New(t)
	teacher, student, stranger := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	if _, err := e.App.Content.EnsureEnrolled(context.Background(), c.ID, student.User.ID, ""); err != nil {
		t.Fatal(err)
	}

	// Uploads are re-encoded: a 600×400 photo with a GPS tag becomes a clean 256×256 JPEG.
	code, body := student.Raw("PUT", "/api/auth/me/avatar", "image/jpeg", withExif(picture(t, 600, 400, "jpeg")))
	if code != 200 {
		t.Fatalf("upload: %d %s", code, body)
	}
	var out struct{ Avatar string }
	_ = json.Unmarshal(body, &out)
	var me auth.User
	student.Call("GET", "/api/auth/me", nil, 200, &me)
	if out.Avatar == "" || me.Avatar != out.Avatar {
		t.Fatalf("version on /me: %q vs %q", me.Avatar, out.Avatar)
	}
	code, hdr, img := avatarGet(student, student.User.ID, "?v="+me.Avatar)
	if code != 200 || hdr.Get("Content-Type") != "image/jpeg" || !strings.Contains(hdr.Get("Cache-Control"), "immutable") || hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("own picture: %d %v", code, hdr)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil || format != "jpeg" || cfg.Width != 256 || cfg.Height != 256 {
		t.Fatalf("stored picture: %v %s %dx%d", err, format, cfg.Width, cfg.Height)
	}
	if bytes.Contains(img, []byte("Exif")) || bytes.Contains(img, []byte("GPS")) {
		t.Fatal("metadata survived")
	}
	// Who can see it: the teacher of the student's class, not other students.
	if code, _, _ := avatarGet(teacher, student.User.ID, ""); code != 200 {
		t.Fatalf("teacher: %d", code)
	}
	if code, _, _ := avatarGet(stranger, student.User.ID, ""); code != 404 {
		t.Fatalf("stranger: %d", code)
	}
	if code, _, _ := avatarGet(e.Client(), student.User.ID, ""); code != 401 {
		t.Fatalf("logged out: %d", code)
	}
	// The teacher's list carries the version so the picture can be cached.
	var list struct{ Enrolments []content.Enrolment }
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/enrolments", nil, 200, &list)
	if list.Enrolments[0].StudentAvatar != me.Avatar {
		t.Fatalf("enrolment avatar: %+v", list.Enrolments[0])
	}
	// Unchanged pictures are revalidated cheaply.
	if code, _, _ := teacher.Get("/api/auth/users/"+student.User.ID+"/avatar", map[string]string{"If-None-Match": `"` + me.Avatar + `"`}); code != 304 {
		t.Fatalf("If-None-Match: %d", code)
	}

	// Limits: 512 KB, real images only, no huge dimensions.
	big := append(picture(t, 10, 10, "png"), make([]byte, 520<<10)...)
	if code, _ := student.Raw("PUT", "/api/auth/me/avatar", "image/png", big); code != 413 {
		t.Fatalf("over 512 KB: %d", code)
	}
	if code, body := student.Raw("PUT", "/api/auth/me/avatar", "image/png", []byte("<svg onload=alert(1)>")); code != 422 || !strings.Contains(string(body), "PNG, JPEG") {
		t.Fatalf("not an image: %d %s", code, body)
	}
	if code, _ := student.Raw("PUT", "/api/auth/me/avatar", "image/png", picture(t, 9000, 2, "png")); code != 422 {
		t.Fatalf("too wide: %d", code)
	}
	// A PNG works too, and removing the picture clears it everywhere.
	if code, _ := student.Raw("PUT", "/api/auth/me/avatar", "image/png", picture(t, 300, 300, "png")); code != 200 {
		t.Fatalf("png: %d", code)
	}
	student.Call("DELETE", "/api/auth/me/avatar", nil, 204, nil)
	me = auth.User{} // "avatar" is omitted when empty
	student.Call("GET", "/api/auth/me", nil, 200, &me)
	if me.Avatar != "" {
		t.Fatal("avatar version not cleared")
	}
	if code, _, _ := avatarGet(teacher, student.User.ID, ""); code != 404 {
		t.Fatalf("after delete: %d", code)
	}
}

func TestChangePassword(t *testing.T) {
	e := apptest.New(t)
	s := e.NewUser(auth.RoleTeacher)
	email := s.User.Email
	// A second device signed in to the same account.
	other := e.Client()
	other.Call("POST", "/api/auth/login", map[string]string{"email": email, "password": "password123"}, 200, nil)

	for _, bad := range []struct {
		cur, next, field string
	}{
		{"wrong-password", "brand new pass 1", "current_password"},
		{"password123", "short", "new_password"},
		{"password123", "password123", "new_password"},
	} {
		code, body := s.Do("POST", "/api/auth/me/password", map[string]string{"current_password": bad.cur, "new_password": bad.next})
		if code != 422 || !strings.Contains(string(body), bad.field) {
			t.Fatalf("%+v: %d %s", bad, code, body)
		}
	}
	e.TakeJobs("email")
	s.Call("POST", "/api/auth/me/password", map[string]string{"current_password": "password123", "new_password": "brand new pass 1"}, 204, nil)
	// This device stays signed in; the other is signed out.
	s.Call("GET", "/api/auth/me", nil, 200, nil)
	if code, _ := other.Do("GET", "/api/auth/me", nil); code != 401 {
		t.Fatalf("other device still signed in: %d", code)
	}
	if code, _ := e.Client().Do("POST", "/api/auth/login", map[string]string{"email": email, "password": "password123"}); code != 401 {
		t.Fatalf("old password: %d", code)
	}
	e.Client().Call("POST", "/api/auth/login", map[string]string{"email": email, "password": "brand new pass 1"}, 200, nil)
	// The owner is told by email.
	found := false
	for _, raw := range e.TakeJobs("email") {
		if strings.Contains(string(raw), "Your password was changed") && strings.Contains(string(raw), email) {
			found = true
		}
	}
	if !found {
		t.Fatal("no password-changed email")
	}
	// Guessing the current password is throttled.
	limited := false
	for i := 0; i < 15 && !limited; i++ {
		code, _ := s.Do("POST", "/api/auth/me/password", map[string]string{"current_password": "guess", "new_password": "another new pass"})
		limited = code == 429
	}
	if !limited {
		t.Fatal("password guesses are not rate-limited")
	}
	if code, _ := e.Client().Do("POST", "/api/auth/me/password", map[string]string{"current_password": "x", "new_password": "yyyyyyyyyy"}); code != 401 {
		t.Fatalf("logged out: %d", code)
	}
}
