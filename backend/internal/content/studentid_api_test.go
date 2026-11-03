package content_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
)

// D-134: a teacher may let students set their own ID in a classroom; IDs stay unique.
func TestStudentsSetTheirOwnID(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	c := newClassroom(t, teacher, map[string]any{"name": "MGT/IS/2025", "settings": map[string]any{"student_id_required": true}})
	ann, ben := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	ann.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "IS/25/01"}, 200, nil)
	ben.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode, "student_number": "IS/25/02"}, 200, nil)
	path := "/api/my/classrooms/" + c.ID + "/student-number"
	mine := func(s *apptest.Client) (string, bool) {
		var page struct {
			Classrooms []struct {
				StudentNumber *string `json:"student_number"`
				CanEditID     bool    `json:"can_edit_id"`
			}
		}
		s.Call("GET", "/api/my/classrooms", nil, 200, &page)
		n := ""
		if p := page.Classrooms[0].StudentNumber; p != nil {
			n = *p
		}
		return n, page.Classrooms[0].CanEditID
	}

	// Off by default: the teacher sets IDs.
	if _, can := mine(ann); can {
		t.Fatal("students may edit their ID before the teacher allows it")
	}
	ann.Call("PUT", path, map[string]string{"student_number": "IS/25/11"}, 403, nil)

	teacher.Call("PATCH", "/api/teacher/classrooms/"+c.ID, map[string]any{"settings": map[string]any{"student_id_required": true, "student_id_editable": true}}, 200, nil)
	if _, can := mine(ann); !can {
		t.Fatal("the teacher allowed it, but the student can't edit")
	}
	ann.Call("PUT", path, map[string]string{"student_number": "  IS/25/11 "}, 200, nil)
	if n, _ := mine(ann); n != "IS/25/11" {
		t.Fatalf("Ann's ID = %q", n)
	}
	ann.Call("PUT", path, map[string]string{"student_number": "IS/25/02"}, 422, nil) // Ben's
	ann.Call("PUT", path, map[string]string{"student_number": ""}, 422, nil)         // required here
	other := e.NewUser(auth.RoleStudent)
	other.Call("PUT", path, map[string]string{"student_number": "X1"}, 404, nil) // not enrolled
}
