package tutorlink_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/tutorlink"
)

// The platform's side of tutoring (ADR-23, TS-NFR-51, TS-NFR-52, D-59).

func TestMain(m *testing.M) { dbtest.Main(m) }

var secret = bytes.Repeat([]byte{9}, 32)

func serviceToken(t *testing.T, key []byte, ttl time.Duration, iss, aud string) string {
	t.Helper()
	now := time.Now()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: iss, Audience: jwt.ClaimStrings{aud},
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl))}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func access(t *testing.T, c *apptest.Client, token, classroomID, userID string) (int, string) {
	t.Helper()
	c.Headers = map[string]string{"Authorization": "Bearer " + token}
	body, _ := json.Marshal(map[string]string{"classroom_id": classroomID, "user_id": userID})
	code, out := c.Raw("POST", "/internal/tutoring/access", "application/json", body)
	c.Headers = nil
	return code, string(out)
}

func TestTutoringIsOffWithoutConfiguration(t *testing.T) {
	e := apptest.New(t)
	s := e.NewUser(auth.RoleStudent)
	var cfg map[string]any
	s.Call("GET", "/api/tutoring/config", nil, 200, &cfg)
	if cfg["enabled"] != false || cfg["url"] != nil {
		t.Fatalf("config = %v", cfg)
	}
	s.Call("POST", "/api/tutoring/token", nil, 404, nil)
	if code, _ := access(t, e.Client(), serviceToken(t, secret, time.Minute, "tutoring", "quiz-platform"), "x", "y"); code != 404 {
		t.Fatalf("internal API with tutoring off: %d", code)
	}
}

func TestUserTokensNameThePerson(t *testing.T) {
	e := apptest.New(t, apptest.WithTutoring("https://tutor.example.edu/", secret))
	e.Client().Call("POST", "/api/tutoring/token", nil, 401, nil)
	s := e.NewUser(auth.RoleStudent)
	var cfg map[string]any
	s.Call("GET", "/api/tutoring/config", nil, 200, &cfg)
	if cfg["enabled"] != true || cfg["url"] != "https://tutor.example.edu" {
		t.Fatalf("config = %v", cfg)
	}
	var out struct {
		Token     string
		ExpiresAt time.Time `json:"expires_at"`
	}
	s.Call("POST", "/api/tutoring/token", nil, 200, &out)
	var c tutorlink.UserClaims
	if _, err := jwt.ParseWithClaims(out.Token, &c, func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience("tutoring"), jwt.WithIssuer("quiz-platform")); err != nil {
		t.Fatal(err)
	}
	if c.Subject != s.User.ID || c.Role != "student" || c.Name != s.User.Name {
		t.Fatalf("claims = %+v", c)
	}
	if ttl := c.ExpiresAt.Sub(c.IssuedAt.Time); ttl != 5*time.Minute {
		t.Fatalf("token lives %v", ttl)
	}
	// A person's token is no use against the platform's internal API.
	if code, _ := access(t, e.Client(), out.Token, "x", s.User.ID); code != 404 {
		t.Fatalf("user token on the internal API: %d", code)
	}
}

func TestInternalAccessAPI(t *testing.T) {
	e := apptest.New(t, apptest.WithTutoring("http://localhost:8090", secret))
	teacher := e.NewUser(auth.RoleTeacher)
	var open, closed content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Open"}, 201, &open)
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Closed", "settings": map[string]any{"auto_enrol_on_join": false}}, 201, &closed)
	student := e.NewUser(auth.RoleStudent)
	c := e.Client()
	good := serviceToken(t, secret, time.Minute, "tutoring", "quiz-platform")

	code, out := access(t, c, good, open.ID, teacher.User.ID)
	if code != 200 || !strings.Contains(out, `"role":"teacher"`) {
		t.Fatalf("teacher: %d %s", code, out)
	}
	// Joining enrols, as for quizzes (BR-02), where the classroom allows it.
	code, out = access(t, c, good, open.ID, student.User.ID)
	if code != 200 || !strings.Contains(out, `"role":"student"`) || !strings.Contains(out, `"classroom_name":"Open"`) {
		t.Fatalf("student: %d %s", code, out)
	}
	code, out = access(t, c, good, closed.ID, student.User.ID)
	if code != 403 || !strings.Contains(out, "not_enrolled") {
		t.Fatalf("not enrolled: %d %s", code, out)
	}
	if code, _ = access(t, c, good, "not-a-uuid", student.User.ID); code != 404 {
		t.Fatalf("unknown classroom: %d", code)
	}

	// Refused: no token, a wrong key, a wrong issuer or audience, a long-lived token, an expired one.
	for name, tok := range map[string]string{
		"none":         "",
		"wrong key":    serviceToken(t, bytes.Repeat([]byte{1}, 32), time.Minute, "tutoring", "quiz-platform"),
		"wrong issuer": serviceToken(t, secret, time.Minute, "quiz-platform", "quiz-platform"),
		"wrong aud":    serviceToken(t, secret, time.Minute, "tutoring", "tutoring"),
		"too long":     serviceToken(t, secret, time.Hour, "tutoring", "quiz-platform"),
		"expired":      serviceToken(t, secret, -time.Minute, "tutoring", "quiz-platform"),
	} {
		if code, _ := access(t, c, tok, open.ID, teacher.User.ID); code != 404 {
			t.Errorf("%s: %d", name, code)
		}
	}
	// Not reachable under /api, where the public proxy forwards.
	if code, _ := access(t, c, good, open.ID, teacher.User.ID); code != 200 {
		t.Fatal("sanity")
	}
	c.Headers = map[string]string{"Authorization": "Bearer " + good}
	if code, _ := c.Raw("POST", "/api/internal/tutoring/access", "application/json", []byte(`{}`)); code == 200 {
		t.Fatal("internal API under /api")
	}
}

// Co-teachers are found by email; only active teachers, and only id and name (TS-FR-70).
func TestInternalTeacherLookup(t *testing.T) {
	e := apptest.New(t, apptest.WithTutoring("http://localhost:8090", secret))
	teacher := e.NewUser(auth.RoleTeacher)
	student := e.NewUser(auth.RoleStudent)
	c := e.Client()
	find := func(token, email string) (int, string) {
		c.Headers = map[string]string{"Authorization": "Bearer " + token}
		body, _ := json.Marshal(map[string]string{"email": email})
		code, out := c.Raw("POST", "/internal/tutoring/teacher", "application/json", body)
		c.Headers = nil
		return code, string(out)
	}
	good := serviceToken(t, secret, time.Minute, "tutoring", "quiz-platform")
	code, out := find(good, strings.ToUpper(teacher.User.Email))
	if code != 200 || !strings.Contains(out, teacher.User.ID) || strings.Contains(out, "email") {
		t.Fatalf("teacher: %d %s", code, out)
	}
	if code, _ := find(good, student.User.Email); code != 404 {
		t.Fatalf("student found: %d", code)
	}
	if code, _ := find(good, "nobody@example.com"); code != 404 {
		t.Fatalf("unknown found: %d", code)
	}
	if code, _ := find("", teacher.User.Email); code != 404 {
		t.Fatalf("no token: %d", code)
	}
}

// Breakout groups from classroom categories (TS-FR-50, D-79): the categories
// and their active students, for the classroom's own teacher only.
func TestInternalCategories(t *testing.T) {
	e := apptest.New(t, apptest.WithTutoring("http://localhost:8090", secret))
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var enrol []content.Enrolment
	for range 3 {
		s := e.NewUser(auth.RoleStudent)
		var en content.Enrolment
		s.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, &en)
		enrol = append(enrol, en)
	}
	var red, blue content.Category
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/categories", map[string]any{"name": "Red", "color": 1}, 201, &red)
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/categories", map[string]any{"name": "Blue", "color": 2}, 201, &blue)
	teacher.Call("POST", "/api/teacher/categories/"+red.ID+"/members", map[string]any{"enrolment_ids": []string{enrol[0].ID, enrol[1].ID}, "assigned": true}, 200, nil)
	teacher.Call("POST", "/api/teacher/categories/"+blue.ID+"/members", map[string]any{"enrolment_ids": []string{enrol[2].ID}, "assigned": true}, 200, nil)

	cl := e.Client()
	ask := func(token, teacherID string) (int, string) {
		cl.Headers = map[string]string{"Authorization": "Bearer " + token}
		body, _ := json.Marshal(map[string]string{"classroom_id": c.ID, "teacher_id": teacherID})
		code, out := cl.Raw("POST", "/internal/tutoring/categories", "application/json", body)
		cl.Headers = nil
		return code, string(out)
	}
	good := serviceToken(t, secret, time.Minute, "tutoring", "quiz-platform")
	code, out := ask(good, teacher.User.ID)
	var got struct{ Categories []tutorlink.CategoryGroup }
	json.Unmarshal([]byte(out), &got)
	if code != 200 || len(got.Categories) != 2 || got.Categories[0].Name != "Red" || len(got.Categories[0].UserIDs) != 2 || len(got.Categories[1].UserIDs) != 1 {
		t.Fatalf("categories: %d %s", code, out)
	}
	if code, _ := ask(good, e.NewUser(auth.RoleTeacher).User.ID); code != 404 {
		t.Fatalf("another teacher's classroom: %d", code)
	}
	if code, _ := ask("", teacher.User.ID); code != 404 {
		t.Fatalf("no token: %d", code)
	}
}
