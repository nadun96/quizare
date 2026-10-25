package tutor_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/platform"
	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// Phase 3 (D-79): breakout groups, their chat, visits, help and closing.

type tokens struct {
	Main, Stage, Group string
	GroupRoom          string `json:"group_room"`
}

func (f *fixture) tokens(p *testenv.Person) tokens {
	var t tokens
	p.Call("POST", f.path("/media-token"), nil, 200, &t)
	return t
}

func (f *fixture) view(p *testenv.Person) tutor.View {
	var v tutor.View
	p.Call("GET", f.path("/"), nil, 200, &v)
	return v
}

func groupOf(v tutor.View, userID string) *tutor.Group {
	for i := range v.Groups {
		if slices.Contains(v.Groups[i].Members, userID) {
			return &v.Groups[i]
		}
	}
	return nil
}

func TestGroups(t *testing.T) {
	f := setup(t, "auto")
	ann, bob, cy, dee := f.student("Ann"), f.student("Bob"), f.student("Cy"), f.student("Dee")
	for _, s := range []*testenv.Person{f.teacher, ann, bob, cy, dee} {
		f.join(s, 200)
	}
	co := f.e.NewPerson("teacher", "Mr Silva")
	f.teacher.Call("POST", f.path("/coteachers"), map[string]string{"email": co.Email}, 201, nil)
	f.join(co, 200)

	// Forming (TS-FR-50): random by count or size; only the lead teacher.
	co.Call("PUT", f.path("/groups"), map[string]any{"mode": "random", "count": 2}, 403, nil)
	f.teacher.Call("PUT", f.path("/groups"), map[string]any{"mode": "random", "count": 0}, 422, nil)
	f.teacher.Call("PUT", f.path("/groups"), map[string]any{"mode": "sorted"}, 422, nil)
	var formed struct{ Groups []tutor.Group }
	f.teacher.Call("PUT", f.path("/groups"), map[string]any{"mode": "random", "size": 3}, 200, &formed)
	if len(formed.Groups) != 2 || formed.Groups[0].Size+formed.Groups[1].Size != 4 || formed.Groups[0].Size != 2 {
		t.Fatalf("random by size = %+v", formed.Groups)
	}
	// From the classroom's categories: a student in two goes to the first; the rest to "Others".
	f.e.Platform.Categories[f.classroom] = []platform.Category{
		{Name: "Red", UserIDs: []string{ann.ID, bob.ID}}, {Name: "Blue", UserIDs: []string{bob.ID, cy.ID}},
	}
	f.teacher.Call("PUT", f.path("/groups"), map[string]any{"mode": "categories"}, 200, &formed)
	names := []string{}
	for _, g := range formed.Groups {
		names = append(names, g.Name+":"+strconv.Itoa(g.Size))
	}
	if strings.Join(names, ",") != "Red:2,Blue:1,Others:1" {
		t.Fatalf("by categories = %v", names)
	}
	// By hand: each student once at most.
	f.teacher.Call("PUT", f.path("/groups"), map[string]any{"mode": "manual", "groups": []map[string]any{{"name": "A", "user_ids": []string{ann.ID}}, {"name": "B", "user_ids": []string{ann.ID}}}}, 422, nil)
	f.teacher.Call("PUT", f.path("/groups"), map[string]any{"mode": "manual", "groups": []map[string]any{
		{"name": "Readers", "user_ids": []string{ann.ID, bob.ID}}, {"name": "", "user_ids": []string{cy.ID}}}}, 200, &formed)
	readers, second := formed.Groups[0], formed.Groups[1]
	if readers.Name != "Readers" || second.Name != "Group 2" {
		t.Fatalf("manual = %+v", formed.Groups)
	}

	// Students see the groups' names and sizes, and who is in their own.
	v := f.view(ann)
	if len(v.Groups) != 2 || len(v.Groups[0].Members) != 0 || len(v.GroupMates) != 1 || v.GroupMates[0].Name != "Bob" || *v.Me.Group != readers.ID {
		t.Fatalf("Ann's view = groups %+v mates %+v", v.Groups, v.GroupMates)
	}

	// Opening needs the session live. Then each student gets their group's room.
	f.teacher.Call("POST", f.path("/groups/open"), nil, 409, nil)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	if tk := f.tokens(ann); tk.Group != "" {
		t.Fatal("a group token before the groups open")
	}
	as, bs, cs, ts := ann.Dial(f.sess.ID), bob.Dial(f.sess.ID), cy.Dial(f.sess.ID), f.teacher.Dial(f.sess.ID)
	for _, s := range []*testenv.Socket{as, bs, cs, ts} {
		s.Must("state")
	}
	f.teacher.Call("POST", f.path("/groups/open"), nil, 204, nil)
	as.Must("groups_opened")
	tk := f.tokens(ann)
	g := readGrant(t, f, ann, tk.Group)
	if tk.GroupRoom != f.sess.ID+"-g"+strconv.FormatInt(readers.ID, 10) || g.Room != tk.GroupRoom || !g.Subscribe || !slices.Equal(g.Sources, []string{"microphone"}) {
		t.Fatalf("Ann's group grant = %+v (%s)", g, tk.GroupRoom)
	}
	if m := readGrant(t, f, ann, tk.Main); len(m.Sources) != 0 {
		t.Fatalf("still nothing to publish in the broadcast: %+v", m)
	}
	if tk := f.tokens(dee); tk.Group != "" {
		t.Fatal("Dee isn't in a group")
	}

	// Group chat (TS-FR-46): on even with the main chat off; the group and the teachers see it.
	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"chat_mode": "off"}, 204, nil)
	ann.Call("POST", f.path("/chat"), map[string]any{"text": "In the main room"}, 403, nil)
	var m tutor.Message
	ann.Call("POST", f.path("/chat"), map[string]any{"text": "Shall we start with question 2?", "group": true}, 201, &m)
	if m.Audience != "group" || m.GroupID == nil || *m.GroupID != readers.ID {
		t.Fatalf("group message = %+v", m)
	}
	bs.Must("chat")
	ts.Must("chat")
	cs.Quiet("chat", 300*time.Millisecond)
	dee.Call("POST", f.path("/chat"), map[string]any{"text": "hi", "group": true}, 409, nil)
	var hist struct{ Messages []tutor.Message }
	bob.Call("GET", f.path("/chat"), nil, 200, &hist)
	cy.Call("GET", f.path("/chat"), nil, 200, &struct{}{})
	if len(hist.Messages) != 1 {
		t.Fatalf("Bob's history = %+v", hist.Messages)
	}
	var cyHist struct{ Messages []tutor.Message }
	cy.Call("GET", f.path("/chat"), nil, 200, &cyHist)
	if len(cyHist.Messages) != 0 {
		t.Fatalf("Cy sees another group's chat: %+v", cyHist.Messages)
	}
	f.teacher.Call("POST", f.path("/chat"), map[string]any{"text": "Good idea", "group_id": readers.ID}, 201, nil)
	as.Must("chat")
	f.teacher.Call("PUT", f.path("/groups/settings"), map[string]any{"group_chat": false}, 204, nil)
	ann.Call("POST", f.path("/chat"), map[string]any{"text": "still?", "group": true}, 403, nil)

	// Help (TS-FR-54): the teachers are told; visiting answers it and joins the group's room.
	ann.Call("PUT", f.path("/help"), map[string]bool{"on": true}, 204, nil)
	if got := ts.Must("group_help"); got["data"].(map[string]any)["name"] != "Ann" {
		t.Fatalf("help = %v", got)
	}
	f.e.Service.Flush(f.sess.ID)
	if gr := groupOf(f.view(f.teacher), ann.ID); gr == nil || gr.HelpAt == nil {
		t.Fatalf("help not shown: %+v", gr)
	}
	co.Call("PUT", f.path("/visit"), map[string]int64{"group_id": readers.ID}, 204, nil)
	ctk := f.tokens(co)
	if cg := readGrant(t, f, co, ctk.Group); cg.Room != tk.GroupRoom || len(cg.Sources) != 4 {
		t.Fatalf("visiting grant = %+v", cg)
	}
	if gr := groupOf(f.view(f.teacher), ann.ID); gr.HelpAt != nil {
		t.Fatal("visiting should answer the call for help")
	}
	co.Call("PUT", f.path("/visit"), map[string]int64{"group_id": 0}, 204, nil)
	if removed := f.e.LiveKit.Calls("RemoveParticipant"); len(removed) != 1 || removed[0].Body["room"] != tk.GroupRoom {
		t.Fatalf("leaving the group room: %+v", removed)
	}

	// Moving a student (TS-FR-52): out of the old room at once; the new token is for the new one.
	f.teacher.Call("PUT", f.path("/participants/"+bob.ID+"/group"), map[string]int64{"group_id": second.ID}, 204, nil)
	if removed := f.e.LiveKit.Calls("RemoveParticipant"); len(removed) != 1 || removed[0].Body["identity"] != bob.ID {
		t.Fatalf("moving: %+v", removed)
	}
	if bt := f.tokens(bob); !strings.HasSuffix(bt.GroupRoom, "-g"+strconv.FormatInt(second.ID, 10)) {
		t.Fatalf("Bob's new room = %s", bt.GroupRoom)
	}
	ann.Call("PUT", f.path("/my-group"), map[string]int64{"group_id": second.ID}, 403, nil) // the teacher assigns

	// More in groups (TS-FR-53): applies at once to the open groups.
	f.e.LiveKit.Calls("UpdateParticipant")
	f.teacher.Call("PUT", f.path("/groups/settings"), map[string]any{"group_camera": true}, 204, nil)
	if ups := f.e.LiveKit.Calls("UpdateParticipant"); len(ups) != 3 {
		t.Fatalf("permission updates = %d", len(ups))
	}
	if g := readGrant(t, f, ann, f.tokens(ann).Group); !slices.Equal(g.Sources, []string{"camera", "microphone"}) {
		t.Fatalf("with cameras in groups = %+v", g)
	}

	// An announcement reaches every group.
	f.teacher.Call("POST", f.path("/groups/announce"), map[string]string{"text": "Five minutes left"}, 201, nil)
	if got := cs.Must("announcement"); got["data"].(map[string]any)["text"] != "Five minutes left" {
		t.Fatalf("announcement = %v", got)
	}

	// Closing after a countdown everyone sees (TS-FR-51); the group rooms end.
	f.teacher.Call("POST", f.path("/groups/close"), map[string]int{"countdown": 400}, 422, nil)
	f.teacher.Call("POST", f.path("/groups/close"), map[string]int{"countdown": 1}, 204, nil)
	f.e.Service.Flush(f.sess.ID)
	if v := f.view(ann); !v.Session.GroupsOpen || v.Session.GroupsCloseAt == nil {
		t.Fatalf("counting down = %+v", v.Session)
	}
	as.Must("groups_closed")
	if v := f.view(ann); v.Session.GroupsOpen {
		t.Fatal("still open after the countdown")
	}
	if ends := f.e.LiveKit.Calls("DeleteRoom"); len(ends) != 2 {
		t.Fatalf("group rooms ended = %+v", ends)
	}
	if tk := f.tokens(ann); tk.Group != "" {
		t.Fatal("a group token after closing")
	}
	f.teacher.Call("POST", f.path("/groups/close"), map[string]int{"countdown": 0}, 409, nil)
}

// Students choose their own group when the teacher lets them.
func TestChooseGroups(t *testing.T) {
	f := setup(t, "auto")
	ann := f.student("Ann")
	f.join(ann, 200)
	var formed struct{ Groups []tutor.Group }
	f.teacher.Call("PUT", f.path("/groups"), map[string]any{"mode": "choose", "count": 3}, 200, &formed)
	if len(formed.Groups) != 3 || formed.Groups[2].Name != "Group 3" || formed.Groups[0].Size != 0 {
		t.Fatalf("choose = %+v", formed.Groups)
	}
	ann.Call("PUT", f.path("/my-group"), map[string]int64{"group_id": formed.Groups[1].ID}, 204, nil)
	ann.Call("PUT", f.path("/my-group"), map[string]int64{"group_id": 999999}, 404, nil)
	if v := f.view(ann); v.Me.Group == nil || *v.Me.Group != formed.Groups[1].ID || v.Groups[1].Size != 1 {
		t.Fatalf("chosen = %+v", v.Me)
	}
	// Ending the session ends the group rooms with it.
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	f.teacher.Call("POST", f.path("/groups/open"), nil, 204, nil)
	f.e.LiveKit.Calls("DeleteRoom")
	f.teacher.Call("POST", f.path("/end"), nil, 204, nil)
	if ends := f.e.LiveKit.Calls("DeleteRoom"); len(ends) != 5 { // main, backstage, three groups
		t.Fatalf("rooms ended = %d", len(ends))
	}
}
