package analytics_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/analytics"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/poll"
)

func liveView(t *testing.T, f *fx, token string) (int, analytics.LiveView, string) {
	t.Helper()
	c := f.e.Client()
	code, body := c.Do("GET", "/api/public/live/"+token, nil)
	var v analytics.LiveView
	if code == 200 {
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatal(err)
		}
	}
	return code, v, string(body)
}

func (f *fx) link(t *testing.T, body map[string]any) analytics.ShareLink {
	t.Helper()
	var l analytics.ShareLink
	f.teacher.Call("POST", "/api/teacher/share-links", body, 201, &l)
	return l
}

func TestLiveSessionLink(t *testing.T) {
	f := setup(t)
	var sess live.Session
	f.teacher.Call("POST", "/api/teacher/quizzes/"+f.quizID+"/sessions", map[string]any{"settings": map[string]any{"team_mode": "manual"}}, 201, &sess)
	f.take(t, sess, "S1", "Alice Perera", "o1", "100") // 2/2
	f.take(t, sess, "S2", "Bimal Silva", "", "100")    // 1/2
	f.process(t)
	var red live.TeamInfo
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/teams", map[string]any{"name": "Red"}, 201, &red)
	var tv live.TeamsView
	f.teacher.Call("GET", "/api/teacher/sessions/"+sess.ID+"/teams", nil, 200, &tv)
	ids := []string{tv.Unassigned[0].AttemptID, tv.Unassigned[1].AttemptID}
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/teams/members", map[string]any{"attempt_ids": ids, "team_id": red.ID}, 204, nil)

	l := f.link(t, map[string]any{"scope": "live_session", "target_id": sess.ID, "identify": "student_id"})
	if l.Token == "" || len(l.Views) != 2 {
		t.Fatalf("link: %+v", l)
	}
	code, v, raw := liveView(t, f, l.Token)
	if code != 200 || v.Kind != "session" || v.Unit != "percent" || v.Joined != 2 || v.Finished == nil || *v.Finished != 2 {
		t.Fatalf("live view: %d %s", code, raw)
	}
	if len(v.People) != 2 || v.People[0].Label != "S1" || v.People[0].Score != 100 || v.People[1].Score != 50 || v.People[0].Team != "Red" {
		t.Fatalf("people: %+v", v.People)
	}
	if len(v.Teams) != 1 || v.Teams[0].Name != "Red" || v.Teams[0].Score != 75 {
		t.Fatalf("teams: %+v", v.Teams)
	}
	if strings.Contains(raw, "Alice") || strings.Contains(raw, "example.com") || strings.Contains(raw, "attempt") {
		t.Fatalf("live view leaks identities: %s", raw)
	}
	// Anonymous links number students; a results page doesn't open a live link.
	anon := f.link(t, map[string]any{"scope": "live_session", "target_id": sess.ID, "views": []string{"leaderboard"}})
	if _, v, _ := liveView(t, f, anon.Token); v.People[0].Label != "Student 1" || v.Teams != nil {
		t.Fatalf("anonymous, people only: %+v %+v", v.People, v.Teams)
	}
	if code, _ := f.e.Client().Do("GET", "/api/public/results/"+anon.Token, nil); code != 404 {
		t.Fatalf("live token on the results page: %d", code)
	}
	// Bad requests and someone else's session.
	for _, bad := range []map[string]any{
		{"scope": "live_session", "target_id": sess.ID, "views": []string{"pass_rate"}},
		{"scope": "live_session", "target_id": sess.ID, "identify": "nickname"},
	} {
		if code, _ := f.teacher.Do("POST", "/api/teacher/share-links", bad); code != 422 {
			t.Errorf("bad link %v: %d", bad, code)
		}
	}
	other := f.e.NewUser(auth.RoleTeacher)
	if code, _ := other.Do("POST", "/api/teacher/share-links", map[string]any{"scope": "live_session", "target_id": sess.ID}); code != 404 {
		t.Fatalf("foreign session: %d", code)
	}
	// Revoked links stop working.
	f.teacher.Call("DELETE", "/api/teacher/share-links/"+l.ID, nil, 204, nil)
	if code, _, _ := liveView(t, f, l.Token); code != 410 {
		t.Fatalf("revoked: %d", code)
	}
	if code, _, _ := liveView(t, f, "not-a-token"); code != 404 {
		t.Fatalf("unknown token: %d", code)
	}
}

func TestLivePollLink(t *testing.T) {
	f := setup(t)
	var p poll.Poll
	f.teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "Capitals", "settings": map[string]any{
		"identity": "anonymous", "audience": "anyone", "pacing": "self", "show_results": "live", "allow_edit": true,
		"scoring": true, "leaderboard": "presenter", "groups": "self", "group_acceptance": "all", "group_calc": "sum"}}, 201, &p)
	var q poll.Question
	f.teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/questions", map[string]any{"type": "SINGLE", "text": "Capital of France?",
		"body": map[string]any{"options": []map[string]string{{"text": "Paris"}, {"text": "Rome"}}}, "key": map[string]any{"correct": []string{"o1"}}}, 201, &q)
	var g poll.Group
	f.teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/groups", map[string]any{"name": "Owls"}, 201, &g)
	f.teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/status", map[string]string{"status": "open"}, 200, nil)
	for _, x := range []struct{ nick, pick string }{{"Ada", "o1"}, {"Bo", "o2"}} {
		c := f.e.Client()
		var j poll.JoinResult
		c.Call("POST", "/api/polls/"+p.JoinCode+"/join", map[string]any{"nickname": x.nick, "group_id": g.ID}, 200, &j)
		c.Headers = map[string]string{poll.TokenHeader: j.Token}
		c.Call("PUT", "/api/polls/"+p.JoinCode+"/answers/"+q.ID, map[string]any{"value": map[string]any{"selected": []string{x.pick}}}, 200, nil)
	}
	l := f.link(t, map[string]any{"scope": "live_poll", "target_id": p.ID, "label": "Room 4"})
	if l.Identify != "nickname" {
		t.Fatalf("poll links default to nicknames: %+v", l)
	}
	code, v, raw := liveView(t, f, l.Token)
	if code != 200 || v.Kind != "poll" || v.Unit != "points" || v.Subtitle != "Room 4" || v.Joined != 2 {
		t.Fatalf("poll view: %d %s", code, raw)
	}
	if len(v.People) != 2 || v.People[0].Label != "Ada" || v.People[0].Score != 100 || len(v.Teams) != 1 || v.Teams[0].Score != 100 {
		t.Fatalf("poll boards: %s", raw)
	}
	anon := f.link(t, map[string]any{"scope": "live_poll", "target_id": p.ID, "identify": "anonymous"})
	if _, v, raw := liveView(t, f, anon.Token); v.People[0].Label != "Participant 1" || strings.Contains(raw, "Ada") {
		t.Fatalf("anonymous poll link: %s", raw)
	}
	if code, _ := f.teacher.Do("POST", "/api/teacher/share-links", map[string]any{"scope": "live_poll", "target_id": p.ID, "identify": "student_id"}); code != 422 {
		t.Fatalf("student ids on a poll: %d", code)
	}
	// Deleting the poll ends its links (once the 2-second cache expires).
	f.teacher.Call("DELETE", "/api/teacher/polls/"+p.ID, nil, 204, nil)
	later := time.Now().Add(5 * time.Second)
	f.e.App.Analytics.SetClock(func() time.Time { return later })
	if code, _, _ := liveView(t, f, anon.Token); code != 410 {
		t.Fatalf("deleted poll: %d", code)
	}
}
