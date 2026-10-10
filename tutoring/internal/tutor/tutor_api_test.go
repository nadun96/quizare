package tutor_test

import (
	"encoding/csv"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// Tutoring sessions, phase 1 (D-59).

func TestMain(m *testing.M) { testenv.Main(m) }

type fixture struct {
	e         *testenv.Env
	teacher   *testenv.Person
	classroom string
	sess      tutor.Session
}

func setup(t *testing.T, admit string) *fixture {
	t.Helper()
	e := testenv.New(t)
	f := &fixture{e: e, teacher: e.NewPerson("teacher", "Ms Perera")}
	f.classroom = e.NewClassroom(f.teacher)
	f.teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": f.classroom, "title": "Algebra help", "admit_mode": admit}, 201, &f.sess)
	return f
}

func (f *fixture) student(name string) *testenv.Person {
	s := f.e.NewPerson("student", name)
	f.e.Platform.Enrol(f.classroom, s.ID)
	return s
}

func (f *fixture) join(p *testenv.Person, want int) tutor.View {
	var v tutor.View
	p.Call("POST", "/api/join/"+f.sess.JoinCode, nil, want, &v)
	return v
}

func (f *fixture) path(rest string) string { return "/api/sessions/" + f.sess.ID + rest }

// grant is what one LiveKit token allows.
type grant struct {
	Room      string
	Sources   []string
	Subscribe bool
}

func readGrant(t *testing.T, f *fixture, p *testenv.Person, raw string) grant {
	t.Helper()
	var c struct {
		jwt.RegisteredClaims
		Video struct {
			Room              string   `json:"room"`
			RoomJoin          bool     `json:"roomJoin"`
			CanPublish        bool     `json:"canPublish"`
			CanSubscribe      bool     `json:"canSubscribe"`
			CanPublishData    bool     `json:"canPublishData"`
			CanPublishSources []string `json:"canPublishSources"`
		} `json:"video"`
	}
	if _, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return f.e.LKSecret, nil }); err != nil {
		t.Fatal(err)
	}
	if c.Subject != p.ID || !c.Video.RoomJoin || c.Video.CanPublishData || c.Issuer != "devkey" {
		t.Fatalf("token claims = %+v", c)
	}
	if ttl := c.ExpiresAt.Sub(time.Now()); ttl > media.TokenTTL || ttl < media.TokenTTL-time.Minute {
		t.Fatalf("token lives %v", ttl)
	}
	if c.Video.CanPublish != (len(c.Video.CanPublishSources) > 0) {
		t.Fatalf("canPublish %v with sources %v", c.Video.CanPublish, c.Video.CanPublishSources)
	}
	slices.Sort(c.Video.CanPublishSources)
	return grant{Room: c.Video.Room, Sources: c.Video.CanPublishSources, Subscribe: c.Video.CanSubscribe}
}

// grants reads p's tokens: the broadcast, and backstage if they have one.
func grants(t *testing.T, f *fixture, p *testenv.Person) (grant, *grant) {
	t.Helper()
	var out struct{ Main, Stage, URL string }
	p.Call("POST", f.path("/media-token"), nil, 200, &out)
	main := readGrant(t, f, p, out.Main)
	if main.Room != f.sess.ID || !main.Subscribe {
		t.Fatalf("main grant = %+v", main)
	}
	if out.Stage == "" {
		return main, nil
	}
	st := readGrant(t, f, p, out.Stage)
	if st.Room != f.sess.ID+"-stage" {
		t.Fatalf("stage grant = %+v", st)
	}
	return main, &st
}

// sources is what p may publish to everyone.
func sources(t *testing.T, f *fixture, p *testenv.Person) []string {
	t.Helper()
	m, _ := grants(t, f, p)
	return m.Sources
}

// backstage is what p may publish for the teachers only (nil: no backstage token).
func backstage(t *testing.T, f *fixture, p *testenv.Person) []string {
	t.Helper()
	_, st := grants(t, f, p)
	if st == nil {
		return nil
	}
	if st.Subscribe != (p.Role == "teacher") {
		t.Fatalf("%s may watch backstage: %v", p.Name, st.Subscribe)
	}
	return st.Sources
}

func TestCreatingAndListingSessions(t *testing.T) {
	e := testenv.New(t)
	teacher := e.NewPerson("teacher", "Ms Perera")
	other := e.NewPerson("teacher", "Mr Silva")
	classroom := e.NewClassroom(teacher)

	// Only the classroom's teacher (TS-FR-01).
	other.Call("POST", "/api/sessions", map[string]any{"classroom_id": classroom, "title": "Mine"}, 403, nil)
	teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": classroom, "title": " "}, 422, nil)
	teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": classroom, "title": "x", "admit_mode": "sometimes"}, 422, nil)
	e.Platform.Archived[classroom] = true
	teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": classroom, "title": "Old"}, 409, nil)
	e.Platform.Archived[classroom] = false

	var s tutor.Session
	teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": classroom, "title": "Session 0"}, 201, &s)
	if len(s.JoinCode) != 8 || s.Status != "open" || s.AdmitMode != "auto" || s.ChatMode != "to_teacher" || s.TeacherName != "Ms Perera" || !strings.HasPrefix(s.ClassroomName, "Room") {
		t.Fatalf("session = %+v", s)
	}
	for i := 1; i < 7; i++ {
		teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": classroom, "title": fmt.Sprintf("Session %d", i)}, 201, nil)
	}
	// Paginated, newest first, without overlap (TS-FR-91).
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		var out struct {
			Sessions []tutor.Session
			Total    int
		}
		teacher.Call("GET", fmt.Sprintf("/api/sessions?size=3&page=%d", page), nil, 200, &out)
		if out.Total != 7 || len(out.Sessions) != map[int]int{1: 3, 2: 3, 3: 1}[page] {
			t.Fatalf("page %d: %d of %d", page, len(out.Sessions), out.Total)
		}
		for _, x := range out.Sessions {
			if seen[x.ID] {
				t.Fatalf("%s on two pages", x.Title)
			}
			seen[x.ID] = true
		}
	}
	var out struct {
		Sessions []tutor.Session
		Total    int
	}
	teacher.Call("GET", "/api/sessions?q=session%206&classroom_id="+classroom, nil, 200, &out)
	if out.Total != 1 || out.Sessions[0].Title != "Session 6" {
		t.Fatalf("search = %+v", out)
	}
	other.Call("GET", "/api/sessions", nil, 200, &out)
	if out.Total != 0 {
		t.Fatal("another teacher sees these sessions")
	}
	other.Call("GET", "/api/sessions/"+s.ID, nil, 404, nil)
	other.Call("POST", "/api/sessions/"+s.ID+"/start", nil, 404, nil)
}

func TestTokensAreChecked(t *testing.T) {
	e := testenv.New(t)
	if rawGet(t, e, "/api/sessions", "garbage") != 401 {
		t.Fatal("garbage token accepted")
	}
	// A token signed with another key, or for another audience, is refused.
	p := e.NewPerson("teacher", "T")
	for _, tok := range []string{
		mustSign(t, []byte("another key of the same length 32"), "quiz-platform", "tutoring", p.ID),
		mustSign(t, testenv.Secret, "quiz-platform", "quiz-platform", p.ID),
		mustSign(t, testenv.Secret, "tutoring", "tutoring", p.ID),
	} {
		code := rawGet(t, e, "/api/sessions", tok)
		if code != 401 {
			t.Fatalf("bad token accepted: %d", code)
		}
	}
	if rawGet(t, e, "/api/sessions", "") != 401 {
		t.Fatal("no token accepted")
	}
}

func TestJoiningAdmittingStartingAndEnding(t *testing.T) {
	f := setup(t, "manual")
	ann, bob, eve := f.student("Ann"), f.student("Bob"), f.e.NewPerson("student", "Eve")

	// Not in the classroom: refused with the platform's reason (TS-FR-02).
	code, body := eve.Do("POST", "/api/join/"+f.sess.JoinCode, nil)
	if code != 403 || !strings.Contains(string(body), "not_enrolled") {
		t.Fatalf("not enrolled: %d %s", code, body)
	}
	f.e.NewPerson("student", "x").Call("POST", "/api/join/NOPE1234", nil, 404, nil)

	// Admit manually: students wait (TS-FR-03).
	v := f.join(ann, 200)
	if v.Me.State != "waiting" || v.Me.Role != "student" || v.Session.JoinCode != "" || v.Participants != nil {
		t.Fatalf("waiting view = %+v", v)
	}
	f.join(bob, 200)
	tv := f.join(f.teacher, 200)
	if tv.Me.Role != "teacher" || tv.Me.State != "admitted" || tv.Waiting != 2 || len(tv.Participants) != 3 {
		t.Fatalf("teacher view = %+v", tv)
	}
	ann.Call("POST", f.path("/media-token"), nil, 403, nil)
	var n struct{ Changed int }
	f.teacher.Call("POST", f.path("/admit"), map[string]any{"user_ids": []string{ann.ID}, "admit": true}, 200, &n)
	f.teacher.Call("POST", f.path("/admit"), map[string]any{"user_ids": []string{bob.ID}, "admit": false}, 200, &n)
	if n.Changed != 1 {
		t.Fatalf("refused %d", n.Changed)
	}
	code, body = bob.Do("POST", "/api/join/"+f.sess.JoinCode, nil)
	if code != 403 || !strings.Contains(string(body), "refused") {
		t.Fatalf("refused student rejoins: %d %s", code, body)
	}
	// Before the start, students wait for it; the teacher can get ready.
	code, body = ann.Do("POST", f.path("/media-token"), nil)
	if code != 409 || !strings.Contains(string(body), "not_live") {
		t.Fatalf("before start: %d %s", code, body)
	}
	if got := sources(t, f, f.teacher); !slices.Equal(got, []string{"camera", "microphone", "screen_share", "screen_share_audio"}) {
		t.Fatalf("teacher sources = %v", got)
	}
	if _, st := grants(t, f, f.teacher); st == nil || !st.Subscribe || len(st.Sources) != 0 { // watches backstage, publishes nothing there
		t.Fatalf("teacher backstage = %+v", st)
	}
	ann.Call("POST", f.path("/start"), nil, 404, nil) // only the teacher
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	f.teacher.Call("POST", f.path("/start"), nil, 409, nil)
	// A student may publish nothing by default (TS-FR-20), and gets no backstage token.
	if got := sources(t, f, ann); len(got) != 0 {
		t.Fatalf("student sources = %v", got)
	}
	if got := backstage(t, f, ann); got != nil {
		t.Fatalf("student backstage = %v", got)
	}

	// Locked: nobody new, though those already in may come back (TS-FR-61).
	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"locked": true}, 204, nil)
	cara := f.student("Cara")
	code, body = cara.Do("POST", "/api/join/"+f.sess.JoinCode, nil)
	if code != 403 || !strings.Contains(string(body), "session_locked") {
		t.Fatalf("locked: %d %s", code, body)
	}
	f.join(ann, 200)
	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"locked": false, "admit_mode": "auto"}, 204, nil)
	if v := f.join(cara, 200); v.Me.State != "admitted" {
		t.Fatalf("auto admit: %+v", v.Me)
	}

	// Removed: out, and can't come back (TS-FR-60).
	f.teacher.Call("DELETE", f.path("/participants/"+cara.ID), nil, 204, nil)
	if calls := f.e.LiveKit.Calls("RemoveParticipant"); len(calls) != 2 || calls[0].Body["identity"] != cara.ID {
		t.Fatalf("media remove = %+v", calls)
	}
	code, body = cara.Do("POST", "/api/join/"+f.sess.JoinCode, nil)
	if code != 403 || !strings.Contains(string(body), "removed") {
		t.Fatalf("removed rejoins: %d %s", code, body)
	}

	// Ending stops everything (TS-FR-04).
	f.teacher.Call("POST", f.path("/end"), nil, 204, nil)
	if calls := f.e.LiveKit.Calls("DeleteRoom"); len(calls) != 2 || calls[0].Body["room"] != f.sess.ID || calls[1].Body["room"] != f.sess.ID+"-stage" {
		t.Fatalf("media end = %+v", calls)
	}
	ann.Call("POST", "/api/join/"+f.sess.JoinCode, nil, 410, nil)
	ann.Call("POST", f.path("/media-token"), nil, 410, nil)
}

func TestPermissionsHandsAndMuting(t *testing.T) {
	f := setup(t, "auto")
	ann, bob := f.student("Ann"), f.student("Bob")
	f.join(ann, 200)
	f.join(bob, 200)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	annSock := ann.Dial(f.sess.ID)
	annSock.Must("state")

	// Raise hands: the queue keeps its order (TS-FR-24).
	ann.Call("PUT", f.path("/hand"), map[string]bool{"raised": true}, 204, nil)
	time.Sleep(5 * time.Millisecond)
	bob.Call("PUT", f.path("/hand"), map[string]bool{"raised": true}, 204, nil)
	ann.Call("PUT", f.path("/hand"), map[string]bool{"raised": true}, 204, nil) // keeps its place
	f.join(f.teacher, 200)
	f.teacher.Call("PUT", f.path("/hand"), map[string]bool{"raised": true}, 403, nil) // teachers don't raise hands
	var tv tutor.View
	f.teacher.Call("GET", f.path(""), nil, 200, &tv)
	var queue []string
	for _, p := range tv.Participants {
		if p.HandAt != nil {
			queue = append(queue, p.Name)
		}
	}
	if !slices.Equal(queue, []string{"Ann", "Bob"}) {
		t.Fatalf("hand queue = %v", queue)
	}

	// Allow from the queue: grants the microphone, lowers the hand and asks (TS-FR-21, TS-FR-22, TS-FR-24).
	yes := true
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{UserIDs: []string{ann.ID}, Mic: &yes, Ask: true, LowerHand: true}, 200, nil)
	ask := annSock.Must("ask")
	if ask["data"].(map[string]any)["mic"] != true {
		t.Fatalf("ask = %v", ask)
	}
	// Allowed devices publish backstage: to the teachers, never to other students (TS-FR-25, TS-FR-37).
	if got := backstage(t, f, ann); !slices.Equal(got, []string{"microphone"}) {
		t.Fatalf("ann backstage = %v", got)
	}
	if got := sources(t, f, ann); len(got) != 0 {
		t.Fatalf("ann broadcasts %v", got)
	}
	calls := f.e.LiveKit.Calls("UpdateParticipant")
	if len(calls) != 2 {
		t.Fatalf("live permission = %+v", calls)
	}
	for _, c := range calls {
		perm := c.Body["permission"].(map[string]any)
		switch c.Body["room"] {
		case f.sess.ID + "-stage":
			if c.Body["identity"] != ann.ID || perm["canPublish"] != true || fmt.Sprint(perm["canPublishSources"]) != "[MICROPHONE]" || perm["canSubscribe"] != false || perm["canPublishData"] != false {
				t.Fatalf("backstage permission = %v", c.Body)
			}
		case f.sess.ID:
			if perm["canPublish"] != false || perm["canSubscribe"] != true {
				t.Fatalf("broadcast permission = %v", c.Body)
			}
		default:
			t.Fatalf("room %v", c.Body["room"])
		}
	}
	tv = tutor.View{} // decoding into a used struct keeps fields the reply leaves out
	f.teacher.Call("GET", f.path(""), nil, 200, &tv)
	for _, p := range tv.Participants {
		if p.Name == "Ann" && (p.HandAt != nil || !p.AllowMic) {
			t.Fatalf("ann after allowing = %+v", p)
		}
	}
	// Everyone gets the screen; then the microphone is taken back from Ann.
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{All: true, Screen: &yes}, 200, nil)
	no := false
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{UserIDs: []string{ann.ID}, Mic: &no}, 200, nil)
	if got := backstage(t, f, ann); !slices.Equal(got, []string{"screen_share", "screen_share_audio"}) {
		t.Fatalf("ann backstage = %v", got)
	}
	if got := backstage(t, f, bob); !slices.Equal(got, []string{"screen_share", "screen_share_audio"}) {
		t.Fatalf("bob backstage = %v", got)
	}
	if calls := f.e.LiveKit.Calls("UpdateParticipant"); len(calls) != 6 { // both rooms each time
		t.Fatalf("live permission calls = %d", len(calls))
	}
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{}, 422, nil)
	ann.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{All: true, Mic: &yes}, 404, nil) // only the teacher

	// Mute everyone's microphone: their microphone tracks only (TS-FR-23).
	f.e.LiveKit.Tracks[ann.ID] = []media.Track{{Sid: "TR_mic_a", Source: "MICROPHONE"}, {Sid: "TR_scr_a", Source: "SCREEN_SHARE"}}
	f.e.LiveKit.Tracks[bob.ID] = []media.Track{{Sid: "TR_mic_b", Source: "MICROPHONE", Muted: true}}
	f.teacher.Call("POST", f.path("/mute"), tutor.MuteInput{All: true, Mic: true}, 204, nil)
	muted := f.e.LiveKit.Calls("MutePublishedTrack") // the fake lists the same tracks in both rooms
	if len(muted) != 2 || muted[0].Body["trackSid"] != "TR_mic_a" || muted[1].Body["trackSid"] != "TR_mic_a" || muted[0].Body["muted"] != true {
		t.Fatalf("muted = %+v", muted)
	}
	f.teacher.Call("POST", f.path("/mute"), tutor.MuteInput{UserID: ann.ID}, 422, nil)

	// The teacher lowers a hand.
	f.teacher.Call("DELETE", f.path("/participants/"+bob.ID+"/hand"), nil, 204, nil)
	tv = tutor.View{}
	f.teacher.Call("GET", f.path(""), nil, 200, &tv)
	for _, p := range tv.Participants {
		if p.HandAt != nil {
			t.Fatalf("hand still up: %+v", p)
		}
	}
}

func TestChat(t *testing.T) {
	f := setup(t, "auto")
	ann, bob := f.student("Ann"), f.student("Bob")
	for _, p := range []*testenv.Person{ann, bob, f.teacher} {
		f.join(p, 200)
	}
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	as, bs, ts := ann.Dial(f.sess.ID), bob.Dial(f.sess.ID), f.teacher.Dial(f.sess.ID)
	for _, s := range []*testenv.Socket{as, bs, ts} {
		s.Must("state")
	}

	// Default: students to the teacher only (TS-FR-40).
	var m tutor.Message
	ann.Call("POST", f.path("/chat"), map[string]string{"text": "  How does this work?  "}, 201, &m)
	if m.Audience != "teachers" || m.Text != "How does this work?" {
		t.Fatalf("message = %+v", m)
	}
	if got := ts.Must("chat"); got["data"].(map[string]any)["text"] != "How does this work?" {
		t.Fatalf("teacher got %v", got)
	}
	as.Must("chat") // her own question, back to her
	bs.Quiet("chat", 300*time.Millisecond)
	// Students can't write privately; teachers reply privately (TS-FR-41).
	ann.Call("POST", f.path("/chat"), map[string]string{"text": "psst", "to": bob.ID}, 403, nil)
	f.teacher.Call("POST", f.path("/chat"), map[string]string{"text": "Look at slide 3", "to": ann.ID}, 201, nil)
	as.Must("chat")
	bs.Quiet("chat", 300*time.Millisecond)

	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"chat_mode": "everyone"}, 204, nil)
	var hello tutor.Message
	bob.Call("POST", f.path("/chat"), map[string]string{"text": "<b>hello</b> https://example.com"}, 201, &hello)
	if got := as.Must("chat"); got["data"].(map[string]any)["text"] != "<b>hello</b> https://example.com" {
		t.Fatalf("plain text, as sent: %v", got) // shown as text, never HTML (TS-FR-43)
	}
	ann.Call("POST", f.path("/chat"), map[string]string{"text": strings.Repeat("a", 1001)}, 422, nil)

	// Late joiners see what they may see (TS-FR-44).
	var hist struct{ Messages []tutor.Message }
	bob.Call("GET", f.path("/chat"), nil, 200, &hist)
	if len(hist.Messages) != 1 || hist.Messages[0].ID != hello.ID {
		t.Fatalf("bob's history = %+v", hist.Messages)
	}
	f.teacher.Call("GET", f.path("/chat"), nil, 200, &hist)
	if len(hist.Messages) != 3 {
		t.Fatalf("teacher's history = %d", len(hist.Messages))
	}
	ann.Call("GET", f.path("/chat"), nil, 200, &hist)
	if len(hist.Messages) != 3 { // her question, the reply to her, and Bob's message
		t.Fatalf("ann's history = %d", len(hist.Messages))
	}

	// Pin and delete (TS-FR-42, TS-FR-45).
	f.teacher.Call("PUT", f.path("/pin"), map[string]int64{"id": m.ID}, 422, nil) // not everyone's
	f.teacher.Call("PUT", f.path("/pin"), map[string]int64{"id": hello.ID}, 204, nil)
	var bv tutor.View
	bob.Call("GET", f.path(""), nil, 200, &bv)
	if bv.Pinned == nil || bv.Pinned.ID != hello.ID {
		t.Fatalf("pinned = %+v", bv.Pinned)
	}
	f.teacher.Call("DELETE", f.path(fmt.Sprintf("/chat/%d", hello.ID)), nil, 204, nil)
	if got := as.Must("chat_deleted"); got["data"].(map[string]any)["id"] != float64(hello.ID) {
		t.Fatalf("deleted = %v", got)
	}
	hist.Messages = nil
	bob.Call("GET", f.path("/chat"), nil, 200, &hist)
	if len(hist.Messages) != 0 {
		t.Fatal("deleted message still in history")
	}
	bv = tutor.View{}
	bob.Call("GET", f.path(""), nil, 200, &bv)
	if bv.Pinned != nil {
		t.Fatal("deleted message still pinned")
	}
	ann.Call("DELETE", f.path(fmt.Sprintf("/chat/%d", m.ID)), nil, 404, nil)

	// Slow mode, muting one student, announcements and off.
	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"slow_seconds": 60}, 204, nil)
	cara := f.student("Cara") // hasn't written yet; Ann wrote within the minute
	f.join(cara, 200)
	cara.Call("POST", f.path("/chat"), map[string]string{"text": "one"}, 201, nil)
	cara.Call("POST", f.path("/chat"), map[string]string{"text": "two"}, 429, nil)
	ann.Call("POST", f.path("/chat"), map[string]string{"text": "again"}, 429, nil)
	f.teacher.Call("PUT", f.path("/participants/"+bob.ID+"/chat-muted"), map[string]bool{"muted": true}, 204, nil)
	code, body := bob.Do("POST", f.path("/chat"), map[string]string{"text": "hi"})
	if code != 403 || !strings.Contains(string(body), "chat_muted") {
		t.Fatalf("muted student: %d %s", code, body)
	}
	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"chat_mode": "announcements", "slow_seconds": 0}, 204, nil)
	ann.Call("POST", f.path("/chat"), map[string]string{"text": "x"}, 403, nil)
	f.teacher.Call("POST", f.path("/chat"), map[string]string{"text": "Break for 5 minutes"}, 201, nil)
	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"chat_mode": "off"}, 204, nil)
	code, body = ann.Do("POST", f.path("/chat"), map[string]string{"text": "x"})
	if code != 403 || !strings.Contains(string(body), "chat_off") {
		t.Fatalf("chat off: %d %s", code, body)
	}
	f.teacher.Call("PUT", f.path("/settings"), map[string]any{"chat_mode": "loud"}, 422, nil)
	var log int
	if err := f.e.Pool.QueryRow(t.Context(), `SELECT count(*) FROM tutoring.log WHERE action IN ('message_deleted', 'chat_muted')`).Scan(&log); err != nil || log != 2 {
		t.Fatalf("log = %d, %v", log, err) // TS-NFR-42
	}
}

func TestSocketsAndAttendance(t *testing.T) {
	f := setup(t, "manual")
	ann := f.student("Ann")
	f.join(ann, 200)
	// Waiting students get state, not chat, and are told when admitted.
	s1 := ann.Dial(f.sess.ID)
	if st := s1.Must("state"); st["data"].(map[string]any)["me"].(map[string]any)["state"] != "waiting" {
		t.Fatalf("state = %v", st)
	}
	f.teacher.Call("POST", f.path("/admit"), map[string]any{"user_ids": []string{ann.ID}, "admit": true}, 200, nil)
	for {
		st := s1.Must("state")
		if st["data"].(map[string]any)["me"].(map[string]any)["state"] == "admitted" {
			break
		}
	}
	// Not live yet: no attendance.
	var att struct{ Attendance []tutor.Attendee }
	f.teacher.Call("GET", f.path("/attendance"), nil, 200, &att)
	if len(att.Attendance) != 0 {
		t.Fatalf("attendance before start = %+v", att)
	}
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	time.Sleep(100 * time.Millisecond)
	// A second tab replaces the first (TS-FR-02), and attendance counts both visits.
	s2 := ann.Dial(f.sess.ID)
	s2.Must("state")
	if r := s1.WaitClosed(); !strings.Contains(r, "replaced") {
		t.Fatalf("first tab closed with %q", r)
	}
	f.teacher.Call("GET", f.path("/attendance"), nil, 200, &att)
	if len(att.Attendance) != 1 || att.Attendance[0].Visits != 2 || !att.Attendance[0].Online || att.Attendance[0].LastLeft != nil {
		t.Fatalf("attendance = %+v", att.Attendance)
	}
	code, csvBody := f.teacher.Do("GET", f.path("/attendance?format=csv"), nil)
	rows, err := csv.NewReader(strings.NewReader(string(csvBody))).ReadAll()
	if code != 200 || err != nil || len(rows) != 2 || rows[1][0] != "Ann" || rows[1][4] != "2" {
		t.Fatalf("csv %d %v %q", code, err, csvBody)
	}
	ann.Call("GET", f.path("/attendance"), nil, 404, nil)

	// Sockets need a valid first message and the platform's origin.
	if err := badSocket(t, f.e, f.sess.ID, "not json", f.e.Origin); err != nil {
		t.Fatal(err)
	}
	if err := badSocket(t, f.e, f.sess.ID, `{"token":"x"}`, "https://evil.example"); err != nil {
		t.Fatal(err)
	}

	// Ending closes everyone, and the visit ends.
	f.teacher.Call("POST", f.path("/end"), nil, 204, nil)
	if r := s2.WaitClosed(); !strings.Contains(r, "ended") {
		t.Fatalf("closed with %q", r)
	}
	f.teacher.Call("GET", f.path("/attendance"), nil, 200, &att)
	if att.Attendance[0].LastLeft == nil || att.Attendance[0].Online {
		t.Fatalf("after end = %+v", att.Attendance[0])
	}
}

func TestRemovedStudentIsDisconnected(t *testing.T) {
	f := setup(t, "auto")
	ann := f.student("Ann")
	f.join(ann, 200)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	s := ann.Dial(f.sess.ID)
	s.Must("state")
	f.teacher.Call("DELETE", f.path("/participants/"+ann.ID), nil, 204, nil)
	if got := s.Must("closed"); got["data"].(map[string]any)["reason"] != "removed" {
		t.Fatalf("closed = %v", got)
	}
	s.WaitClosed()
	// And can't reconnect.
	s2 := ann.Dial(f.sess.ID)
	if r := s2.WaitClosed(); !strings.Contains(r, "not in this session") {
		t.Fatalf("removed reconnect: %q", r)
	}
}
