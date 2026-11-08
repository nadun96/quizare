package content_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
)

// Classroom groups (D-158): leaders and assistants, students choosing their
// own groups within the teacher's limits and dates, and leaders' own names.
func TestClassGroups(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	other := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	students := make([]*apptest.Client, 4)
	ens := make([]content.Enrolment, 4)
	for i := range students {
		students[i] = e.NewUser(auth.RoleStudent)
		en, err := e.App.Content.EnsureEnrolled(context.Background(), c.ID, students[i].User.ID, fmt.Sprintf("S%d", i))
		if err != nil {
			t.Fatal(err)
		}
		ens[i] = en
	}
	outsider := e.NewUser(auth.RoleStudent)
	base := "/api/teacher/classrooms/" + c.ID

	var red, blue content.Category
	teacher.Call("POST", base+"/categories", map[string]any{"name": "Red"}, 201, &red)
	teacher.Call("POST", base+"/categories", map[string]any{"name": "Blue"}, 201, &blue)
	teacher.Call("POST", "/api/teacher/categories/"+red.ID+"/members", map[string]any{"enrolment_ids": []string{ens[0].ID, ens[1].ID}, "assigned": true}, 200, nil)

	// Roles: one leader a group; a new leader replaces the old.
	role := func(cat, en, r string, code int) {
		t.Helper()
		teacher.Call("PUT", "/api/teacher/categories/"+cat+"/members/"+en+"/role", map[string]string{"role": r}, code, nil)
	}
	role(red.ID, ens[0].ID, "leader", 204)
	role(red.ID, ens[1].ID, "assistant", 204)
	role(red.ID, ens[2].ID, "leader", 422) // not in the group
	role(red.ID, ens[1].ID, "boss", 422)
	other.Call("PUT", "/api/teacher/categories/"+red.ID+"/members/"+ens[1].ID+"/role", map[string]string{"role": "leader"}, 404, nil)

	type view struct {
		Groups   []content.ClassGroup
		Policy   content.GroupPolicy
		Students []content.GroupMember
	}
	var v view
	teacher.Call("GET", base+"/groups", nil, 200, &v)
	if len(v.Groups) != 2 || v.Policy.SelfJoin || v.Policy.MaxPerStudent != 1 || len(v.Students) != 4 || v.Students[0].EnrolmentID == "" {
		t.Fatalf("groups = %+v", v)
	}
	var redView content.ClassGroup
	for _, g := range v.Groups {
		if g.ID == red.ID {
			redView = g
		}
	}
	if len(redView.People) != 2 || redView.People[0].UserID != students[0].User.ID || redView.People[0].Role != "leader" || redView.People[1].Role != "assistant" {
		t.Fatalf("red = %+v", redView)
	}
	role(red.ID, ens[1].ID, "leader", 204)
	teacher.Call("GET", base+"/groups", nil, 200, &v)
	for _, g := range v.Groups {
		if g.ID == red.ID && (g.People[0].UserID != students[1].User.ID || g.People[1].Role != "member") {
			t.Fatalf("new leader: %+v", g.People)
		}
	}
	other.Call("GET", base+"/groups", nil, 404, nil)

	// Choosing is off until the teacher allows it.
	students[2].Call("POST", "/api/my/groups/"+blue.ID, nil, 409, nil)
	outsider.Call("GET", "/api/my/classrooms/"+c.ID+"/groups", nil, 404, nil)
	outsider.Call("POST", "/api/my/groups/"+blue.ID, nil, 404, nil)

	one := 1
	teacher.Call("PUT", base+"/group-policy", map[string]any{"self_join": true, "max_per_student": 0}, 422, nil)
	teacher.Call("PUT", base+"/group-policy", map[string]any{"self_join": true, "max_per_student": 1, "max_size": 0}, 422, nil)
	later := time.Now().Add(time.Hour)
	teacher.Call("PUT", base+"/group-policy", map[string]any{"self_join": true, "max_per_student": 1, "opens_at": later, "closes_at": later.Add(-time.Minute)}, 422, nil)
	var pol content.GroupPolicy
	teacher.Call("PUT", base+"/group-policy", map[string]any{"self_join": true, "max_per_student": 1, "opens_at": later}, 200, &pol)
	if !pol.SelfJoin || pol.Open {
		t.Fatalf("not open yet: %+v", pol)
	}
	students[2].Call("POST", "/api/my/groups/"+blue.ID, nil, 409, nil)

	teacher.Call("PUT", base+"/group-policy", map[string]any{"self_join": true, "max_per_student": 1, "max_size": one}, 200, &pol)
	if !pol.Open {
		t.Fatalf("open now: %+v", pol)
	}
	students[2].Call("POST", "/api/my/groups/"+blue.ID, nil, 204, nil)
	students[2].Call("POST", "/api/my/groups/"+blue.ID, nil, 204, nil) // already in: nothing changes
	students[3].Call("POST", "/api/my/groups/"+blue.ID, nil, 409, nil) // full
	students[2].Call("POST", "/api/my/groups/"+red.ID, nil, 409, nil)  // one group of their own choosing
	students[0].Call("DELETE", "/api/my/groups/"+red.ID, nil, 409, nil) // the teacher put them there

	var mine content.MyGroupsView
	students[2].Call("GET", "/api/my/classrooms/"+c.ID+"/groups", nil, 200, &mine)
	if len(mine.Groups) != 2 || mine.Chosen != 1 || !mine.Policy.Open {
		t.Fatalf("mine = %+v", mine)
	}
	for _, g := range mine.Groups {
		switch g.ID {
		case blue.ID:
			if !g.Mine || !g.Self || !g.Full || len(g.Members) != 1 || g.Members[0].StudentNumber != nil || g.Members[0].EnrolmentID != "" {
				t.Fatalf("blue = %+v", g)
			}
		case red.ID:
			if g.Mine || g.Size != 2 || len(g.Members) != 0 {
				t.Fatalf("red, not theirs = %+v", g)
			}
		}
	}

	students[2].Call("DELETE", "/api/my/groups/"+blue.ID, nil, 204, nil)
	teacher.Call("PUT", base+"/group-policy", map[string]any{"self_join": true, "max_per_student": 2}, 200, nil)
	students[2].Call("POST", "/api/my/groups/"+blue.ID, nil, 204, nil)
	students[2].Call("POST", "/api/my/groups/"+red.ID, nil, 204, nil)
	teacher.Call("PUT", base+"/group-policy", map[string]any{"self_join": false, "max_per_student": 2}, 200, nil)
	students[2].Call("DELETE", "/api/my/groups/"+blue.ID, nil, 409, nil) // closed: stays

	// Only the leader names the group; names can't clash.
	students[0].Call("PUT", "/api/my/groups/"+red.ID+"/name", map[string]string{"name": "Comets"}, 403, nil)
	students[1].Call("PUT", "/api/my/groups/"+red.ID+"/name", map[string]string{"name": "blue"}, 422, nil)
	students[1].Call("PUT", "/api/my/groups/"+red.ID+"/name", map[string]string{"name": "  The   Comets "}, 204, nil)
	var cats struct{ Categories []content.Category }
	teacher.Call("GET", base+"/categories", nil, 200, &cats)
	for _, cat := range cats.Categories {
		if cat.ID == red.ID && (cat.PublicName == nil || *cat.PublicName != "The Comets" || cat.Shown() != "The Comets" || cat.Name != "Red") {
			t.Fatalf("named = %+v", cat)
		}
	}
	students[0].Call("GET", "/api/my/classrooms/"+c.ID+"/groups", nil, 200, &mine)
	for _, g := range mine.Groups {
		if g.ID == red.ID && (g.Name != "The Comets" || g.Teacher != "Red" || g.Role != "member") {
			t.Fatalf("student sees = %+v", g)
		}
	}
	other.Call("DELETE", "/api/teacher/categories/"+red.ID+"/public-name", nil, 404, nil)
	teacher.Call("DELETE", "/api/teacher/categories/"+red.ID+"/public-name", nil, 204, nil)
	teacher.Call("GET", base+"/categories", nil, 200, &cats)
	for _, cat := range cats.Categories {
		if cat.ID == red.ID && cat.PublicName != nil {
			t.Fatalf("name not cleared: %+v", cat)
		}
	}
}
