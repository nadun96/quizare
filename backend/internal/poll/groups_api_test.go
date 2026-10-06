package poll_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/poll"
)

func grouped(groups, acceptance, calc string) map[string]any {
	st := scored("self", "after_close", "everyone", false)
	st["groups"], st["group_acceptance"], st["group_calc"] = groups, acceptance, calc
	return st
}

var capitalQ = keyed(singleQ, map[string]any{"correct": []string{"o2"}})

func (f *fixture) joinGroup(t *testing.T, nick, group string) *player {
	t.Helper()
	c := f.e.Client()
	var j poll.JoinResult
	c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/join", map[string]any{"nickname": nick, "group_id": group}, 200, &j)
	c.Headers = map[string]string{poll.TokenHeader: j.Token}
	return &player{c, f}
}

func (f *fixture) groups() poll.GroupsView {
	f.e.T.Helper()
	var v poll.GroupsView
	f.teacher.Call("GET", "/api/teacher/polls/"+f.poll.ID+"/groups", nil, 200, &v)
	return v
}

func (f *fixture) settle(t *testing.T, st map[string]any) {
	t.Helper()
	f.teacher.Call("PUT", "/api/teacher/polls/"+f.poll.ID, map[string]any{"settings": st}, 200, nil)
}

func groupScores(lb []poll.GroupRank) string {
	var b []string
	for _, g := range lb {
		b = append(b, fmt.Sprintf("%d:%s=%g", g.Rank, g.Name, g.Score))
	}
	return strings.Join(b, " ")
}

func TestSelfChosenGroupsAndCalculation(t *testing.T) {
	f := setup(t, grouped("self", "all", "average"), capitalQ)
	var red, blue poll.Group
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups", map[string]any{"name": "Red"}, 201, &red)
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups", map[string]any{"name": " blue  team ", "color": 5}, 201, &blue)
	if blue.Name != "blue team" || blue.Color != 5 || red.Color != 1 {
		t.Fatalf("groups: %+v %+v", red, blue)
	}
	if code, _ := f.teacher.Do("POST", "/api/teacher/polls/"+f.poll.ID+"/groups", map[string]any{"name": "RED"}); code != 422 {
		t.Fatalf("duplicate name: %d", code)
	}
	// A group must be chosen, and it must be one of this poll's.
	if code, _ := f.e.Client().Do("POST", "/api/polls/"+f.poll.JoinCode+"/join", map[string]any{}); code != 422 {
		t.Fatalf("join without a group: %d", code)
	}
	if code, _ := f.e.Client().Do("POST", "/api/polls/"+f.poll.JoinCode+"/join", map[string]any{"group_id": f.poll.ID}); code != 422 {
		t.Fatalf("join with a foreign group: %d", code)
	}
	a, b, c := f.joinGroup(t, "A", red.ID), f.joinGroup(t, "B", red.ID), f.joinGroup(t, "C", blue.ID)
	a.answer(f.qs[0], map[string]any{"selected": []string{"o2"}}) // right: 100
	b.answer(f.qs[0], map[string]any{"selected": []string{"o1"}}) // wrong: 0
	c.answer(f.qs[0], map[string]any{"selected": []string{"o2"}}) // right: 100

	v := a.view()
	if v.MyGroup != red.ID || len(v.Groups) != 2 || v.Groups[0].Members != 2 || v.GroupMode != "self" {
		t.Fatalf("participant group view: %+v %+v", v.MyGroup, v.Groups)
	}
	if got := groupScores(v.GroupLeaderboard); got != "1:blue team=100 2:Red=50" {
		t.Fatalf("average: %s", got)
	}
	f.settle(t, grouped("self", "all", "sum"))
	if got := groupScores(f.results().GroupLeaderboard); got != "1:Red=100 1:blue team=100" {
		t.Fatalf("sum (tie): %s", got)
	}
	f.settle(t, grouped("self", "all", "min"))
	if got := groupScores(f.results().GroupLeaderboard); got != "1:blue team=100 2:Red=0" {
		t.Fatalf("lowest: %s", got)
	}
	f.settle(t, grouped("self", "best", "sum"))
	if got := groupScores(f.results().GroupLeaderboard); got != "1:Red=100 1:blue team=100" {
		t.Fatalf("best: %s", got)
	}
	// Average counts members who didn't answer as 0.
	f.joinGroup(t, "D", blue.ID)
	f.settle(t, grouped("self", "all", "average"))
	if got := groupScores(f.results().GroupLeaderboard); got != "1:Red=50 1:blue team=50" {
		t.Fatalf("average with a silent member: %s", got)
	}

	// Changing group is allowed only before answering.
	var j poll.JoinResult
	a.c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/join", map[string]any{"group_id": blue.ID}, 200, &j)
	if a.view().MyGroup != red.ID {
		t.Fatal("moved group after answering")
	}
	// Deleting a group leaves its members ungrouped.
	f.teacher.Call("DELETE", "/api/teacher/poll-groups/"+red.ID, nil, 204, nil)
	gv := f.groups()
	if len(gv.Groups) != 1 || len(gv.Ungrouped) != 2 || gv.Ungrouped[0].Captain {
		t.Fatalf("after deleting a group: %+v", gv)
	}
	if s := c.raw(); strings.Contains(s, "participant_id") {
		t.Fatalf("participant ids leaked: %s", s)
	}
}

func TestFirstAnswerAndCaptainGroups(t *testing.T) {
	f := setup(t, grouped("manual", "first", "sum"), capitalQ, keyed(multiQ, map[string]any{"correct": []string{"o1", "o2"}}))
	ps := []*player{f.join(t, "P1"), f.join(t, "P2"), f.join(t, "P3"), f.join(t, "P4")}
	if code, _ := f.teacher.Do("POST", "/api/teacher/polls/"+f.poll.ID+"/groups/generate", map[string]any{"from": "random", "count": 1}); code != 422 {
		t.Fatalf("one random group: %d", code)
	}
	var gv poll.GroupsView
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups/generate", map[string]any{"from": "random", "count": 2}, 200, &gv)
	if len(gv.Groups) != 2 || len(gv.Groups[0].Members) != 2 || len(gv.Groups[1].Members) != 2 || len(gv.Ungrouped) != 0 {
		t.Fatalf("random groups: %+v", gv)
	}
	for _, g := range gv.Groups {
		n := 0
		for _, m := range g.Members {
			if m.Captain {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("group %s has %d captains", g.Name, n)
		}
	}
	byNick := map[string]*player{}
	for i, p := range ps {
		byNick[fmt.Sprintf("P%d", i+1)] = p
	}
	g1 := gv.Groups[0]
	first, mate := byNick[g1.Members[0].Name], byNick[g1.Members[1].Name]

	// First answer: once a member answers, teammates can't.
	first.answer(f.qs[0], map[string]any{"selected": []string{"o2"}})
	if code, body := mate.c.Do("PUT", "/api/polls/"+f.poll.JoinCode+"/answers/"+f.qs[0].ID, map[string]any{"value": map[string]any{"selected": []string{"o1"}}}); code != 409 || !strings.Contains(string(body), "group_answered") {
		t.Fatalf("teammate answering second: %d %s", code, body)
	}
	if ga := mate.view().GroupAnswers[f.qs[0].ID]; ga.By != first.view().Nickname || ga.Value.Selected[0] != "o2" {
		t.Fatalf("teammate sees the group's answer: %+v", ga)
	}
	// The first answerer may still change it; clearing frees the question.
	first.answer(f.qs[0], map[string]any{})
	mate.answer(f.qs[0], map[string]any{"selected": []string{"o2"}})
	if lb := f.results().GroupLeaderboard; lb[0].ID != g1.ID || lb[0].Score != 100 {
		t.Fatalf("first-answer score: %s", groupScores(lb))
	}

	// Captain: only the captain answers, and only the captain's answer counts.
	f.settle(t, grouped("manual", "captain", "sum"))
	var captain, crew *player
	for _, m := range g1.Members {
		if m.Captain {
			captain = byNick[m.Name]
		} else {
			crew = byNick[m.Name]
		}
	}
	if code, body := crew.c.Do("PUT", "/api/polls/"+f.poll.JoinCode+"/answers/"+f.qs[1].ID, map[string]any{"value": map[string]any{"selected": []string{"o1", "o2"}}}); code != 409 || !strings.Contains(string(body), "captain_only") {
		t.Fatalf("non-captain answering: %d %s", code, body)
	}
	captain.answer(f.qs[1], map[string]any{"selected": []string{"o1", "o2"}})
	if !captain.view().Captain || crew.view().Captain {
		t.Fatal("captain flag in the participant view")
	}
	// Teacher hands the captaincy over; the new captain's answers count.
	crewID := ""
	for _, m := range f.groups().Groups[0].Members {
		if !m.Captain {
			crewID = m.ParticipantID
		}
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups/members", map[string]any{"participant_ids": []string{crewID}, "captain": true}, 204, nil)
	if !crew.view().Captain || captain.view().Captain {
		t.Fatal("captaincy moved")
	}
	// Moving everyone into one group and out again.
	all := []string{}
	for _, g := range f.groups().Groups {
		for _, m := range g.Members {
			all = append(all, m.ParticipantID)
		}
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups/members", map[string]any{"participant_ids": all, "group_id": g1.ID}, 204, nil)
	gv = f.groups()
	if len(gv.Groups[0].Members) != 4 || len(gv.Groups[1].Members) != 0 {
		t.Fatalf("moved everyone: %+v", gv)
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/groups/members", map[string]any{"participant_ids": all[:1], "group_id": ""}, 204, nil)
	if gv = f.groups(); len(gv.Ungrouped) != 1 {
		t.Fatalf("ungrouped one: %+v", gv.Ungrouped)
	}
}

func TestGroupsFromCategories(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var ens []content.Enrolment
	students := []*apptest.Client{e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)}
	for i, s := range students {
		en, err := e.App.Content.EnsureEnrolled(context.Background(), c.ID, s.User.ID, fmt.Sprintf("S%d", i))
		if err != nil {
			t.Fatal(err)
		}
		ens = append(ens, en)
	}
	var owls, foxes content.Category
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/categories", map[string]any{"name": "Owls", "color": 3}, 201, &owls)
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/categories", map[string]any{"name": "Foxes"}, 201, &foxes)
	teacher.Call("POST", "/api/teacher/categories/"+owls.ID+"/members", map[string]any{"enrolment_ids": []string{ens[0].ID, ens[1].ID}, "assigned": true}, 200, nil)
	teacher.Call("POST", "/api/teacher/categories/"+foxes.ID+"/members", map[string]any{"enrolment_ids": []string{ens[2].ID}, "assigned": true}, 200, nil)

	st := grouped("categories", "all", "sum")
	st["identity"] = "anonymous"
	if code, _ := teacher.Do("POST", "/api/teacher/polls", map[string]any{"title": "x", "classroom_id": c.ID, "settings": st}); code != 422 {
		t.Fatalf("categories need identified participants: %d", code)
	}
	st["identity"] = "identified"
	if code, _ := teacher.Do("POST", "/api/teacher/polls", map[string]any{"title": "x", "settings": st}); code != 422 {
		t.Fatalf("categories need a classroom: %d", code)
	}
	var p poll.Poll
	teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "Team quiz", "classroom_id": c.ID, "settings": st}, 201, &p)
	f := &fixture{e: e, teacher: teacher, poll: p}
	var q poll.Question
	teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/questions", capitalQ, 201, &q)
	teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/status", map[string]string{"status": "open"}, 200, nil)

	// Someone who joined before the groups existed is placed when they are made.
	students[0].Call("POST", "/api/polls/"+p.JoinCode+"/join", map[string]bool{"identify": true}, 200, nil)
	var gv poll.GroupsView
	teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/groups/generate", map[string]any{"from": "categories"}, 200, &gv)
	if len(gv.Groups) != 2 || gv.Groups[0].Name != "Owls" || gv.Groups[0].Color != 3 || len(gv.Groups[0].Members) != 1 || !gv.Groups[0].Members[0].Captain {
		t.Fatalf("groups from categories: %+v", gv)
	}
	// Later joiners go straight to their category's group.
	for _, s := range students[1:] {
		s.Call("POST", "/api/polls/"+p.JoinCode+"/join", map[string]bool{"identify": true}, 200, nil)
	}
	gv = f.groups()
	if len(gv.Groups[0].Members) != 2 || len(gv.Groups[1].Members) != 1 || gv.Groups[0].Members[0].RealName == "" {
		t.Fatalf("joined into category groups: %+v", gv)
	}
	// Generating again doesn't duplicate groups.
	teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/groups/generate", map[string]any{"from": "categories"}, 200, &gv)
	if len(gv.Groups) != 2 {
		t.Fatalf("regenerated: %d groups", len(gv.Groups))
	}
	students[2].Call("PUT", "/api/polls/"+p.JoinCode+"/answers/"+q.ID, map[string]any{"value": map[string]any{"selected": []string{"o2"}}}, 200, nil)
	if got := groupScores(f.results().GroupLeaderboard); got != "1:Foxes=100 2:Owls=0" {
		t.Fatalf("category groups scored: %s", got)
	}
}
