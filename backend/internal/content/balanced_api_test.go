package content_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
)

// D-162: a student's gender, set by them and corrected by their teacher, and
// random groups with each gender spread evenly and at most so many a group.
func TestGenderAndRandomGroups(t *testing.T) {
	e := apptest.New(t)
	teacher, other := e.NewUser(auth.RoleTeacher), e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var girls, boys []*apptest.Client
	ens := map[string]string{}
	for i := range 8 {
		s := e.NewUser(auth.RoleStudent)
		en, err := e.App.Content.EnsureEnrolled(context.Background(), c.ID, s.User.ID, fmt.Sprintf("S%d", i))
		if err != nil {
			t.Fatal(err)
		}
		ens[s.User.ID] = en.ID
		if i < 2 {
			girls = append(girls, s)
		} else {
			boys = append(boys, s)
		}
	}
	// The student sets theirs.
	girls[0].Call("PUT", "/api/auth/me/gender", map[string]string{"gender": "other"}, 422, nil)
	girls[0].Call("PUT", "/api/auth/me/gender", map[string]string{"gender": "female"}, 200, nil)
	var g struct{ Gender string }
	girls[0].Call("GET", "/api/auth/me/gender", nil, 200, &g)
	if g.Gender != "female" {
		t.Fatalf("gender = %q", g.Gender)
	}
	// The teacher sets the rest.
	teacher.Call("PATCH", "/api/teacher/enrolments/"+ens[girls[1].User.ID], map[string]string{"gender": "female"}, 200, nil)
	for _, b := range boys {
		teacher.Call("PATCH", "/api/teacher/enrolments/"+ens[b.User.ID], map[string]string{"gender": "male"}, 200, nil)
	}
	other.Call("PATCH", "/api/teacher/enrolments/"+ens[boys[0].User.ID], map[string]string{"gender": "female"}, 404, nil)
	teacher.Call("PATCH", "/api/teacher/enrolments/"+ens[boys[0].User.ID], map[string]string{"gender": "x"}, 422, nil)
	var list struct{ Enrolments []content.Enrolment }
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/enrolments", nil, 200, &list)
	female := 0
	for _, en := range list.Enrolments {
		if en.Gender == "female" {
			female++
		}
	}
	if female != 2 {
		t.Fatalf("girls in the list: %d", female)
	}

	// Two groups: one girl and three boys each.
	url := "/api/teacher/classrooms/" + c.ID + "/groups/random"
	teacher.Call("POST", url, map[string]any{"count": 1}, 422, nil)
	other.Call("POST", url, map[string]any{"count": 2}, 404, nil)
	var res content.RandomGroupsResult
	teacher.Call("POST", url, map[string]any{"count": 2, "balance": true, "prefix": "Team"}, 201, &res)
	if len(res.Groups) != 2 || res.Placed != 8 || res.Left != 0 {
		t.Fatalf("made = %+v", res)
	}
	type view struct{ Groups []content.ClassGroup }
	var v view
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/groups", nil, 200, &v)
	for _, grp := range v.Groups {
		f := 0
		for _, p := range grp.People {
			for _, gl := range girls {
				if p.UserID == gl.User.ID {
					f++
				}
			}
		}
		if !strings.HasPrefix(grp.Name, "Team ") || len(grp.People) != 4 || f != 1 {
			t.Fatalf("%s: %d people, %d girls", grp.Name, len(grp.People), f)
		}
	}
	// At most 3 a group: two left for the teacher; names don't clash.
	teacher.Call("POST", url, map[string]any{"count": 2, "max": 3, "prefix": "Team"}, 201, &res)
	if res.Placed != 6 || res.Left != 2 {
		t.Fatalf("with a most: %+v", res)
	}
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/groups", nil, 200, &v)
	if len(v.Groups) != 4 || v.Groups[2].Name != "Team 3" {
		t.Fatalf("groups: %d, third %q", len(v.Groups), v.Groups[2].Name)
	}
}

// D-162: a classroom's student ID pattern: IDs must fit it; old ones are flagged.
func TestStudentIDPattern(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C", "settings": map[string]any{"student_id_editable": true}}, 201, &c)
	old := e.NewUser(auth.RoleStudent)
	old.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "abc"}, 200, nil)

	url := "/api/teacher/classrooms/" + c.ID + "/student-ids"
	teacher.Call("PUT", url, map[string]string{"pattern": "MGT/(", "hint": ""}, 422, nil)
	teacher.Call("PUT", url, map[string]string{"pattern": `MGT/\d{4}/\d{3}`, "hint": "MGT/24/1"}, 422, nil)
	var set struct {
		Pattern     string
		NotMatching int `json:"not_matching"`
	}
	teacher.Call("PUT", url, map[string]string{"pattern": `MGT/\d{4}/\d{3}`, "hint": "MGT/2024/447"}, 200, &set)
	if set.NotMatching != 1 {
		t.Fatalf("not matching: %d", set.NotMatching)
	}
	var rule content.StudentIDRule
	teacher.Call("GET", url, nil, 200, &rule)
	if rule.Hint != "MGT/2024/447" {
		t.Fatalf("rule = %+v", rule)
	}

	s := e.NewUser(auth.RoleStudent)
	code, body := s.Do("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "MGT/2024/44"})
	if code != 422 || !strings.Contains(string(body), "MGT/2024/447") {
		t.Fatalf("a wrong ID: %d %s", code, body)
	}
	// The whole ID must fit, not just part of it.
	if code, _ := s.Do("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "xMGT/2024/447"}); code != 422 {
		t.Fatalf("a partly right ID: %d", code)
	}
	var en content.Enrolment
	s.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "MGT/2024/447"}, 200, &en)
	s.Call("PUT", "/api/my/classrooms/"+c.ID+"/student-number", map[string]string{"student_number": "nope"}, 422, nil)
	teacher.Call("PATCH", "/api/teacher/enrolments/"+en.ID, map[string]string{"student_number": "nope"}, 422, nil)
	teacher.Call("PATCH", "/api/teacher/enrolments/"+en.ID, map[string]string{"student_number": "MGT/2024/448"}, 200, nil)

	var list struct{ Enrolments []content.Enrolment }
	teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/enrolments", nil, 200, &list)
	for _, x := range list.Enrolments {
		if x.NumberBad != (x.UserID == old.User.ID) {
			t.Fatalf("flags: %+v", list.Enrolments)
		}
	}
	// No pattern: anything goes again.
	teacher.Call("PUT", url, map[string]string{"pattern": ""}, 200, nil)
	s.Call("PUT", "/api/my/classrooms/"+c.ID+"/student-number", map[string]string{"student_number": "nope"}, 200, nil)
}
