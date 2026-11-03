package content_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/props"
)

// D-128: a classroom's optional details, kept tidy, and the list filtered by them.
func TestClassroomProps(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	is := newClassroom(t, teacher, map[string]any{"name": "MGT/IS/2025", "props": map[string]any{
		"department": "Information Systems", "course_code": "is2201", "year": 2025, "semester": "Semester 1",
		"starts_on": "2025-01-06", "ends_on": "2025-06-30", "tags": []string{"First year", "lab", "Lab"}}})
	if is.Props.CourseCode != "IS2201" || len(is.Props.Tags) != 2 || *is.Props.StartsOn != "2025-01-06" {
		t.Fatalf("created = %+v", is.Props)
	}
	newClassroom(t, teacher, map[string]any{"name": "MGT/IS/2024", "props": map[string]any{"department": "Information Systems", "year": 2024, "tags": []string{"first year"}}})
	newClassroom(t, teacher, map[string]any{"name": "Research", "props": map[string]any{"department": "Graduate studies", "starts_on": "2099-01-01"}})
	newClassroom(t, teacher, map[string]any{"name": "Plain"})
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Bad", "props": map[string]any{"year": 12, "starts_on": "2026-02-01", "ends_on": "2026-01-01"}}, 422, nil)

	names := func(query string) []string {
		var page struct{ Classrooms []content.Classroom }
		teacher.Call("GET", "/api/teacher/classrooms?sort=name&"+query, nil, 200, &page)
		out := []string{}
		for _, c := range page.Classrooms {
			out = append(out, c.Name)
		}
		return out
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	}
	eq(names("department=information+systems"), "MGT/IS/2024", "MGT/IS/2025")
	eq(names("year=2025"), "MGT/IS/2025")
	eq(names("tag=FIRST+YEAR"), "MGT/IS/2024", "MGT/IS/2025")
	eq(names("tag=first+year&tag=lab"), "MGT/IS/2025")
	eq(names("when=upcoming"), "Research")
	eq(names("when=past"), "MGT/IS/2025")
	eq(names("q=is2201"), "MGT/IS/2025")
	eq(names("q=lab"), "MGT/IS/2025")

	var f props.Facets
	teacher.Call("GET", "/api/teacher/classroom-facets", nil, 200, &f)
	if len(f.Departments) != 2 || f.Departments[0].Value != "Information Systems" || f.Departments[0].Count != 2 || len(f.Years) != 2 || f.Years[0].Value != "2025" {
		t.Fatalf("facets = %+v", f)
	}

	// Changing the details replaces them; the rest stays.
	var up content.Classroom
	teacher.Call("PATCH", "/api/teacher/classrooms/"+is.ID, map[string]any{"props": map[string]any{"department": "IS", "tags": []string{}}}, 200, &up)
	if up.Name != "MGT/IS/2025" || up.Props.Department != "IS" || up.Props.Year != nil || len(up.Props.Tags) != 0 || up.Props.StartsOn != nil {
		t.Fatalf("updated = %+v", up.Props)
	}
	teacher.Call("PATCH", "/api/teacher/classrooms/"+is.ID, map[string]any{"name": "Renamed"}, 200, &up)
	if up.Props.Department != "IS" {
		t.Fatal("a rename cleared the details")
	}
	// Another teacher sees none of it.
	other := e.NewUser(auth.RoleTeacher)
	other.Call("GET", "/api/teacher/classroom-facets", nil, 200, &f)
	if len(f.Departments) != 0 {
		t.Fatalf("facets leak: %+v", f)
	}
}
