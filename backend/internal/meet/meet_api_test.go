package meet_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nadun96/quizplatform/internal/app"
	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/meet"
	"github.com/nadun96/quizplatform/internal/platform/config"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

func send(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func create(t *testing.T, c *apptest.Client, body map[string]any) meet.Meeting {
	t.Helper()
	var m meet.Meeting
	c.Call("POST", "/api/teacher/meetings", body, 201, &m)
	return m
}

func TestMeetingSignalling(t *testing.T) {
	e := apptest.New(t, func(c *config.Config, _ *app.Options) { c.MeetSTUN = []string{"stun:stun.example.org:3478"} })
	host := e.NewUser(auth.RoleTeacher)
	ann, ben := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)

	// Checked input; students don't host.
	host.Call("POST", "/api/teacher/meetings", map[string]any{"title": "", "max_people": 6}, 422, nil)
	host.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Too big", "max_people": 40}, 422, nil)
	ann.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Mine"}, 403, nil)
	m := create(t, host, map[string]any{"title": "  Revision   chat ", "max_people": 3})
	if m.Title != "Revision chat" || len(m.Code) < 6 || m.Status != "open" || m.MaxPeople != 3 {
		t.Fatalf("meeting = %+v", m)
	}

	// Anyone signed in looks it up by code, with the STUN servers.
	var acc meet.Access
	ann.Call("GET", "/api/meetings/"+m.Code, nil, 200, &acc)
	if acc.Host || acc.Meeting.ID != m.ID || len(acc.ICE) != 1 || !strings.HasPrefix(acc.ICE[0].URLs[0], "stun:") {
		t.Fatalf("access = %+v", acc)
	}
	e.Client().Call("GET", "/api/meetings/"+m.Code, nil, 401, nil)
	ann.Call("GET", "/api/meetings/NOPE2345", nil, 404, nil)

	// Joining: a welcome with the people already there; others hear about it.
	hc := host.Dial("/ws/meet/" + m.Code)
	hw := apptest.ReadUntil(t, hc, "welcome", 3*time.Second)
	hostPeer := hw["you"].(map[string]any)["peer_id"].(string)
	if !hw["you"].(map[string]any)["host"].(bool) || len(hw["peers"].([]any)) != 0 {
		t.Fatalf("host welcome = %v", hw)
	}
	ac := ann.Dial("/ws/meet/" + m.Code)
	aw := apptest.ReadUntil(t, ac, "welcome", 3*time.Second)
	annPeer := aw["you"].(map[string]any)["peer_id"].(string)
	if peers := aw["peers"].([]any); len(peers) != 1 || peers[0].(map[string]any)["peer_id"] != hostPeer {
		t.Fatalf("ann sees %v", aw["peers"])
	}
	if j := apptest.ReadUntil(t, hc, "joined", 2*time.Second); j["peer"].(map[string]any)["peer_id"] != annPeer {
		t.Fatalf("joined = %v", j)
	}

	// Offers, answers and candidates are relayed to one person, marked with the sender.
	send(t, ac, map[string]any{"type": "signal", "to": hostPeer, "data": map[string]any{"sdp": map[string]any{"type": "offer", "sdp": "v=0"}}})
	sig := apptest.ReadUntil(t, hc, "signal", 2*time.Second)
	if sig["from"] != annPeer || sig["data"].(map[string]any)["sdp"].(map[string]any)["type"] != "offer" {
		t.Fatalf("signal = %v", sig)
	}
	// Mic, camera and hand state goes to the others.
	send(t, ac, map[string]any{"type": "state", "mic": true, "cam": false, "hand": true})
	if st := apptest.ReadUntil(t, hc, "state", 2*time.Second); st["peer_id"] != annPeer || st["hand"] != true || st["cam"] != false {
		t.Fatalf("state = %v", st)
	}

	// Full: the third place is taken, the fourth is refused.
	bc := ben.Dial("/ws/meet/" + m.Code)
	benPeer := apptest.ReadUntil(t, bc, "welcome", 3*time.Second)["you"].(map[string]any)["peer_id"].(string)
	cat := e.NewUser(auth.RoleStudent)
	cc := cat.Dial("/ws/meet/" + m.Code)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, err := cc.Read(ctx)
	cancel()
	if websocket.CloseStatus(err) != 4009 {
		t.Fatalf("full meeting: %v", err)
	}

	// Only the host removes people.
	send(t, ac, map[string]any{"type": "remove", "peer_id": benPeer})
	send(t, hc, map[string]any{"type": "remove", "peer_id": benPeer})
	apptest.ReadUntil(t, bc, "removed", 2*time.Second)
	if l := apptest.ReadUntil(t, ac, "left", 2*time.Second); l["peer_id"] != benPeer {
		t.Fatalf("left = %v", l)
	}

	// A second tab replaces the first.
	ac2 := ann.Dial("/ws/meet/" + m.Code)
	apptest.ReadUntil(t, ac2, "welcome", 3*time.Second)
	apptest.ReadUntil(t, ac, "replaced", 2*time.Second)

	// Attendance: who came, how often, and who is still here.
	var att struct{ Attendance []meet.AttendanceRow }
	host.Call("GET", "/api/teacher/meetings/"+m.ID+"/attendance", nil, 200, &att)
	byName := map[string]meet.AttendanceRow{}
	for _, r := range att.Attendance {
		byName[r.UserID] = r
	}
	if len(att.Attendance) != 3 || byName[ann.User.ID].Joins != 2 || !byName[ann.User.ID].Online || byName[ben.User.ID].Online {
		t.Fatalf("attendance = %+v", att.Attendance)
	}
	e.NewUser(auth.RoleTeacher).Call("GET", "/api/teacher/meetings/"+m.ID+"/attendance", nil, 404, nil)

	// The list shows it with how many are in.
	var list struct{ Meetings []meet.Meeting }
	host.Call("GET", "/api/teacher/meetings", nil, 200, &list)
	if len(list.Meetings) != 1 || list.Meetings[0].Online != 2 {
		t.Fatalf("list = %+v", list.Meetings)
	}

	// The host ends it from the call: everyone is told, and nobody gets back in.
	send(t, hc, map[string]any{"type": "end"})
	apptest.ReadUntil(t, ac2, "ended", 2*time.Second)
	ann.Call("GET", "/api/meetings/"+m.Code, nil, 410, nil)
	host.Call("GET", "/api/teacher/meetings", nil, 200, &list)
	if list.Meetings[0].Status != "ended" || list.Meetings[0].EndedAt == nil {
		t.Fatalf("ended = %+v", list.Meetings[0])
	}
}

func TestClassroomMeeting(t *testing.T) {
	e := apptest.New(t)
	host := e.NewUser(auth.RoleTeacher)
	in, out := e.NewUser(auth.RoleStudent), e.NewUser(auth.RoleStudent)
	var c content.Classroom
	host.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 9"}, 201, &c)
	in.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)

	// Someone else's classroom is refused.
	other := e.NewUser(auth.RoleTeacher)
	other.Call("POST", "/api/teacher/meetings", map[string]any{"title": "Sneaky", "classroom_id": c.ID}, 422, nil)

	m := create(t, host, map[string]any{"title": "Grade 9 catch-up", "classroom_id": c.ID})
	if m.MaxPeople != 6 || m.ClassroomID == nil || *m.ClassroomID != c.ID {
		t.Fatalf("meeting = %+v", m)
	}
	in.Call("GET", "/api/meetings/"+m.Code, nil, 200, nil)
	host.Call("GET", "/api/meetings/"+m.Code, nil, 200, nil)
	out.Call("GET", "/api/meetings/"+m.Code, nil, 403, nil)

	// Ending over HTTP; only the host can.
	other.Call("POST", "/api/teacher/meetings/"+m.ID+"/end", nil, 404, nil)
	host.Call("POST", "/api/teacher/meetings/"+m.ID+"/end", nil, 204, nil)
	in.Call("GET", "/api/meetings/"+m.Code, nil, 410, nil)
}

func TestTURNCredentials(t *testing.T) {
	secret := "a-shared-coturn-secret"
	file := filepath.Join(t.TempDir(), "turn.secret")
	if err := os.WriteFile(file, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := apptest.New(t, func(c *config.Config, _ *app.Options) {
		c.MeetSTUN = nil
		c.MeetTURNURLs = []string{"turn:turn.example.org:3478", "turns:turn.example.org:5349"}
		c.MeetTURNSecretFile = file
	})
	host := e.NewUser(auth.RoleTeacher)
	m := create(t, host, map[string]any{"title": "Relay"})
	var acc meet.Access
	host.Call("GET", "/api/meetings/"+m.Code, nil, 200, &acc)
	if !acc.Host || len(acc.ICE) != 1 || len(acc.ICE[0].URLs) != 2 {
		t.Fatalf("ice = %+v", acc.ICE)
	}
	ice := acc.ICE[0]
	if !strings.HasSuffix(ice.Username, ":"+host.User.ID) {
		t.Fatalf("username %q", ice.Username)
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(ice.Username))
	if ice.Credential != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("the credential isn't coturn's HMAC of the username")
	}
}

func TestTURNNeedsSecret(t *testing.T) {
	if _, err := meet.NewService(nil, nil, nil, "", meet.Options{TURNURLs: []string{"turn:x"}}, nil); err == nil {
		t.Fatal("TURN without a secret file")
	}
	short := filepath.Join(t.TempDir(), "s")
	_ = os.WriteFile(short, []byte("short"), 0o600)
	if _, err := meet.NewService(nil, nil, nil, "", meet.Options{TURNURLs: []string{"turn:x"}, TURNSecretFile: short}, nil); err == nil {
		t.Fatal("a short TURN secret")
	}
}
