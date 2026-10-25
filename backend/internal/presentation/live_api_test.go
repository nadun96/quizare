package presentation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/presentation"
)

func send(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// audience waits for the presenter's summary to match.
func audience(t *testing.T, c *websocket.Conn, ok func(counts map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m := apptest.ReadUntil(t, c, "audience", 5*time.Second)
		if ok(m["counts"].(map[string]any)) {
			return m
		}
	}
	t.Fatal("the audience never matched")
	return nil
}

func TestLive(t *testing.T) {
	f := setup(t)
	var p presentation.Presentation
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/presentations", map[string]any{"title": "Cells"}, 201, &p)
	tpath := "/api/teacher/presentations/" + p.ID
	f.teacher.Call("POST", tpath+"/live", nil, 409, nil) // no slides yet
	p = f.upload(t, p.ID, 4)
	s := f.student()
	path := "/api/presentations/" + p.ID

	// Not live: nothing to join, and a draft stays hidden.
	if c := status(t, s, "POST", path+"/live/join", "application/json", nil, 404); c != "not_live" && c != "not_found" {
		t.Fatal(c)
	}
	var l presentation.Live
	f.teacher.Call("POST", tpath+"/live", nil, 200, &l)
	if len(l.Code) != 6 || l.Slide != 1 || l.MaxShown != 1 || !l.AllowBrowse {
		t.Fatalf("live = %+v", l)
	}
	var again presentation.Live
	f.teacher.Call("POST", tpath+"/live", nil, 200, &again)
	if again.ID != l.ID {
		t.Fatal("a second start opened another session")
	}

	// A draft being presented: the class sees it live, and joins with the code.
	var mine struct{ Presentations []presentation.Mine }
	s.Call("GET", "/api/my/presentations", nil, 200, &mine)
	if len(mine.Presentations) != 1 || !mine.Presentations[0].Live || mine.Presentations[0].Published {
		t.Fatalf("mine = %+v", mine)
	}
	var found struct{ ID string }
	s.Call("GET", "/api/presentations/code/"+l.Code, nil, 200, &found)
	if found.ID != p.ID {
		t.Fatalf("by code = %+v", found)
	}
	f.e.NewUser(auth.RoleStudent).Call("GET", "/api/presentations/code/"+l.Code, nil, 404, nil)
	var lv presentation.LiveView
	s.Call("POST", path+"/live/join", nil, 200, &lv)
	if len(lv.Slides) != 4 || lv.Live.Code != l.Code || lv.Presentation.Published {
		t.Fatalf("join = %+v", lv)
	}
	s.Call("POST", path+"/open", nil, 404, nil) // not at their own pace: it's a draft

	// Sockets: the teacher presents, the student follows.
	pres := f.teacher.Dial("/ws/presentations/" + p.ID)
	stu := s.Dial("/ws/presentations/" + p.ID)
	apptest.ReadUntil(t, pres, "live", 3*time.Second)
	apptest.ReadUntil(t, stu, "live", 3*time.Second)
	audience(t, pres, func(c map[string]any) bool { return c["joined"] == 1.0 && c["following"] == 1.0 })

	img := func(n, want int) {
		t.Helper()
		if code, _, _ := s.Get(fmt.Sprintf("%s/slides/%d?v=%d", path, n, p.Version), nil); code != want {
			t.Fatalf("slide %d: %d, want %d", n, code, want)
		}
	}
	img(1, 200)
	img(2, 404) // never ahead of the teacher
	f.teacher.Call("POST", tpath+"/live/slide", map[string]int{"slide": 9}, 422, nil)
	f.teacher.Call("POST", tpath+"/live/slide", map[string]int{"slide": 3}, 200, &l)
	m := apptest.ReadUntil(t, stu, "live", 3*time.Second)
	if live := m["live"].(map[string]any); live["slide"] != 3.0 || live["max_shown"] != 3.0 {
		t.Fatalf("pushed = %v", m)
	}
	img(2, 200)
	img(4, 404)

	// Looking back on their own, then the tab hidden: the teacher sees both.
	// Time counts between the moments the server hears from the student, so
	// the waits leave room for a busy machine delaying a message.
	time.Sleep(2500 * time.Millisecond) // on slide 3, following
	send(t, stu, map[string]any{"type": "state", "slide": 1, "following": false, "visible": true})
	audience(t, pres, func(c map[string]any) bool { return c["looking_back"] == 1.0 })
	send(t, stu, map[string]any{"type": "state", "slide": 4, "following": false, "visible": true}) // ahead: ignored
	send(t, stu, map[string]any{"type": "state", "slide": 1, "following": false, "visible": false})
	a := audience(t, pres, func(c map[string]any) bool { return c["away"] == 1.0 })
	if people := a["people"].([]any); len(people) != 1 || people[0].(map[string]any)["slide"] != 1.0 {
		t.Fatalf("people = %v", a["people"])
	}

	// Looking back can be turned off.
	f.teacher.Call("PUT", tpath+"/live", map[string]bool{"allow_browse": false}, 200, &l)
	if l.AllowBrowse {
		t.Fatal("allow_browse still on")
	}
	send(t, stu, map[string]any{"type": "state", "slide": 1, "following": true, "visible": true})
	audience(t, pres, func(c map[string]any) bool { return c["following"] == 1.0 })
	send(t, stu, map[string]any{"type": "state", "slide": 2, "following": false, "visible": true})
	time.Sleep(2500 * time.Millisecond)
	send(t, stu, map[string]any{"type": "state", "slide": 3, "following": true, "visible": true})

	// Ending: everyone is told; live time is recorded apart from their own pace.
	f.teacher.Call("POST", tpath+"/live/end", nil, 204, nil)
	apptest.ReadUntil(t, stu, "ended", 3*time.Second)
	f.teacher.Call("GET", tpath+"/live", nil, 404, nil)
	s.Call("GET", "/api/presentations/code/"+l.Code, nil, 404, nil)
	img(1, 404) // a draft, no longer live
	// The last report's time is written by the student's connection as it
	// arrives, so the report may need a moment to include it.
	var rep presentation.Report
	for deadline := time.Now().Add(5 * time.Second); ; {
		rep = presentation.Report{}
		f.teacher.Call("GET", tpath+"/report", nil, 200, &rep)
		if rep.Slides[2].LiveMedian >= 3 || time.Now().After(deadline) { // slide 3's own time, its one viewer
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	st := rep.Students[0]
	if st.LiveSeconds < 3 || st.Seen != 0 || rep.Slides[2].LiveViewers != 1 {
		t.Fatalf("report = %+v slides=%+v", st, rep.Slides)
	}
	var d presentation.ViewerDetail
	f.teacher.Call("GET", tpath+"/viewers/"+st.ViewerID, nil, 200, &d)
	if d.Slides[2].LiveSeconds < 3 || d.Slides[2].Seconds != 0 {
		t.Fatalf("detail = %+v", d.Slides)
	}
	f.e.NewUser(auth.RoleTeacher).Call("POST", tpath+"/live", nil, 404, nil)
}
