package live_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/live"
)

func (f *fixture) teams(t *testing.T) live.TeamsView {
	t.Helper()
	var v live.TeamsView
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/teams", nil, 200, &v)
	return v
}

func TestRandomTeamsAndCaptains(t *testing.T) {
	f := setup(t, nil, nil, map[string]any{"team_mode": "random", "team_acceptance": "captain"})
	var gv live.TeamsView
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams/generate", map[string]any{"from": "random", "count": 2}, 200, &gv)
	if len(gv.Teams) != 2 || gv.Teams[0].Name != "Team 1" || gv.Mode != "random" || gv.Acceptance != "captain" {
		t.Fatalf("generated: %+v", gv)
	}
	var states []live.StudentState
	for i := 0; i < 3; i++ {
		_, st := f.join(t)
		states = append(states, st)
	}
	v := f.teams(t)
	if n0, n1 := len(v.Teams[0].List), len(v.Teams[1].List); n0+n1 != 3 || n0 < 1 || n1 < 1 {
		t.Fatalf("joiners spread over teams: %d %d", n0, n1)
	}
	for _, tm := range v.Teams {
		n := 0
		for _, m := range tm.List {
			if m.Captain {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s has %d captains: %+v", tm.Name, n, tm.List)
		}
	}
	// The first to join a team becomes its captain.
	if states[0].Team == nil || states[0].Team.Name == "" || !states[0].Captain {
		t.Fatalf("student state shows the team: %+v", states[0].Team)
	}
	d := dashboard(t, f)
	if len(d.Teams) != 2 || d.Rows[0].TeamID == nil {
		t.Fatalf("dashboard teams: %+v", d.Teams)
	}

	// Move everyone into team 1, hand over the captaincy, then delete team 1.
	ids := []string{states[0].AttemptID, states[1].AttemptID, states[2].AttemptID}
	t1 := v.Teams[0].ID
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams/members", map[string]any{"attempt_ids": ids, "team_id": t1}, 204, nil)
	v = f.teams(t)
	if len(v.Teams[0].List) != 3 {
		t.Fatalf("all in team 1: %+v", v.Teams)
	}
	other := ""
	for _, m := range v.Teams[0].List {
		if !m.Captain {
			other = m.AttemptID
		}
	}
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams/members", map[string]any{"attempt_ids": []string{other}, "captain": true}, 204, nil)
	captains := 0
	for _, m := range f.teams(t).Teams[0].List {
		if m.Captain {
			captains++
			if m.AttemptID != other {
				t.Fatal("captaincy not handed over")
			}
		}
	}
	if captains != 1 {
		t.Fatalf("%d captains", captains)
	}
	if code, _ := f.teacher.Do("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams/members", map[string]any{"attempt_ids": ids, "captain": true}); code != 422 {
		t.Fatalf("several captains at once: %d", code)
	}
	f.teacher.Call("PATCH", "/api/teacher/session-teams/"+t1, map[string]any{"name": "Owls", "color": 4}, 200, nil)
	if code, _ := f.teacher.Do("PATCH", "/api/teacher/session-teams/"+v.Teams[1].ID, map[string]any{"name": "owls"}); code != 422 {
		t.Fatalf("duplicate team name: %d", code)
	}
	f.teacher.Call("DELETE", "/api/teacher/session-teams/"+t1, nil, 204, nil)
	v = f.teams(t)
	if len(v.Teams) != 1 || len(v.Unassigned) != 3 || v.Unassigned[0].Captain {
		t.Fatalf("after delete: %+v", v)
	}
	// Another teacher can't touch these teams.
	other2 := f.e.NewUser(auth.RoleTeacher)
	if code, _ := other2.Do("GET", "/api/teacher/sessions/"+f.sess.ID+"/teams", nil); code != 404 {
		t.Fatalf("foreign teacher: %d", code)
	}
}

func TestSelfChosenAndCategoryTeams(t *testing.T) {
	f := setup(t, nil, nil, map[string]any{"team_mode": "self"})
	var red live.TeamInfo
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams", map[string]any{"name": "Red"}, 201, &red)
	s := f.e.NewUser(auth.RoleStudent)
	var p live.JoinPreview
	s.Call("GET", "/api/join/sessions/"+f.sess.JoinCode, nil, 200, &p)
	if p.TeamMode != "self" || len(p.Teams) != 1 || p.Teams[0].Name != "Red" {
		t.Fatalf("preview lists teams: %+v", p)
	}
	if code, _ := s.Do("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}); code != 422 {
		t.Fatalf("join without a team: %d", code)
	}
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{"team_id": red.ID}, 200, &st)
	if st.Team == nil || st.Team.ID != red.ID {
		t.Fatalf("joined Red: %+v", st.Team)
	}

	// Categories: one team per category; students follow their category.
	var cat content.Category
	f.teacher.Call("POST", "/api/teacher/classrooms/"+f.sess.ClassroomID+"/categories", map[string]any{"name": "Seniors", "color": 7}, 201, &cat)
	var list struct{ Enrolments []content.Enrolment }
	f.teacher.Call("GET", "/api/teacher/classrooms/"+f.sess.ClassroomID+"/enrolments", nil, 200, &list)
	f.teacher.Call("POST", "/api/teacher/categories/"+cat.ID+"/members", map[string]any{"enrolment_ids": []string{list.Enrolments[0].ID}, "assigned": true}, 200, nil)
	var gv live.TeamsView
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams/generate", map[string]any{"from": "categories", "reassign": true}, 200, &gv)
	if len(gv.Teams) != 2 || gv.Teams[1].Name != "Seniors" || gv.Teams[1].Color != 7 || len(gv.Teams[1].List) != 1 || len(gv.Teams[0].List) != 0 {
		t.Fatalf("category teams: %+v", gv.Teams)
	}
}

func dashboard(t *testing.T, f *fixture) live.Dashboard {
	t.Helper()
	var d live.Dashboard
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID, nil, 200, &d)
	return d
}
