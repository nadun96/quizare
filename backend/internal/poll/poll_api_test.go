package poll_test

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/poll"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

type fixture struct {
	e       *apptest.Env
	teacher *apptest.Client
	poll    poll.Poll
	qs      []poll.Question
}

func setup(t *testing.T, settings map[string]any, questions ...map[string]any) *fixture {
	t.Helper()
	e := apptest.New(t)
	f := &fixture{e: e, teacher: e.NewUser(auth.RoleTeacher)}
	body := map[string]any{"title": "Lesson check-in"}
	if settings != nil {
		body["settings"] = settings
	}
	f.teacher.Call("POST", "/api/teacher/polls", body, 201, &f.poll)
	for _, q := range questions {
		var out poll.Question
		f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/questions", q, 201, &out)
		f.qs = append(f.qs, out)
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/status", map[string]string{"status": "open"}, 200, &f.poll)
	return f
}

func settings(identity, pacing, show string) map[string]any {
	return map[string]any{"identity": identity, "audience": "anyone", "pacing": pacing, "show_results": show, "allow_edit": true}
}

var (
	singleQ = map[string]any{"type": "SINGLE", "text": "How was today?", "body": map[string]any{"options": []map[string]string{{"text": "Great"}, {"text": "OK"}, {"text": "Hard"}}}}
	cloudQ  = map[string]any{"type": "WORD_CLOUD", "text": "One word for today", "body": map[string]any{"max_entries": 2}}
	essayQ  = map[string]any{"type": "ESSAY", "text": "What should we revise?"}
	audioQ  = map[string]any{"type": "AUDIO", "text": "Say hello", "body": map[string]any{"max_seconds": 20}}
	fileQ   = map[string]any{"type": "FILE", "text": "Upload your diagram", "body": map[string]any{"accept": []string{"image"}, "max_mb": 1}}
)

// participant joins and returns a client that sends its token.
func (f *fixture) anon(t *testing.T) *apptest.Client {
	t.Helper()
	c := f.e.Client()
	var j poll.JoinResult
	c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/join", nil, 200, &j)
	if j.Token == "" || j.Identified {
		t.Fatalf("anonymous join: %+v", j)
	}
	c.Headers = map[string]string{poll.TokenHeader: j.Token}
	return c
}

func (f *fixture) answer(c *apptest.Client, q poll.Question, v map[string]any, want int) {
	f.e.T.Helper()
	c.Call("PUT", "/api/polls/"+f.poll.JoinCode+"/answers/"+q.ID, map[string]any{"value": v}, want, nil)
}

func (f *fixture) view(c *apptest.Client) poll.PublicPoll {
	f.e.T.Helper()
	var v poll.PublicPoll
	c.Call("GET", "/api/polls/"+f.poll.JoinCode, nil, 200, &v)
	return v
}

func (f *fixture) results() poll.TeacherResults {
	f.e.T.Helper()
	var r poll.TeacherResults
	f.teacher.Call("GET", "/api/teacher/polls/"+f.poll.ID+"/results", nil, 200, &r)
	return r
}

func TestAnonymousPoll(t *testing.T) {
	f := setup(t, nil, singleQ, cloudQ)
	a, b := f.anon(t), f.anon(t)
	if v := f.view(a); !v.Joined || v.Results != nil || len(v.Questions) != 2 {
		t.Fatalf("before answering: %+v", v)
	}
	f.answer(a, f.qs[0], map[string]any{"selected": []string{"o1"}}, 200)
	f.answer(b, f.qs[0], map[string]any{"selected": []string{"o1"}}, 200)
	f.answer(a, f.qs[1], map[string]any{"words": []string{"Fun!", "fast"}}, 200)
	f.answer(b, f.qs[1], map[string]any{"words": []string{"fun"}}, 200)

	v := f.view(a)
	if v.Answers[f.qs[0].ID].Selected[0] != "o1" {
		t.Fatalf("own answer not returned: %+v", v.Answers)
	}
	r := v.Results[f.qs[0].ID]
	if r.Counts["o1"] != 2 || r.Responses != 2 {
		t.Fatalf("results after answering: %+v", r)
	}
	if w := v.Results[f.qs[1].ID].Words; len(w) != 2 || w[0].Word != "fun" || w[0].Count != 2 {
		t.Fatalf("word cloud: %+v", w)
	}
	// A logged-in student joining an anonymous poll is still not identified.
	s := f.e.NewUser(auth.RoleStudent)
	var j poll.JoinResult
	s.Call("POST", "/api/polls/"+f.poll.JoinCode+"/join", map[string]bool{"identify": true}, 200, &j)
	if j.Identified || j.Token == "" {
		t.Fatalf("anonymous poll identified a user: %+v", j)
	}
	var withUser int
	_ = f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM poll.participants WHERE user_id IS NOT NULL`).Scan(&withUser)
	if withUser != 0 {
		t.Fatal("an anonymous poll stored a user id")
	}
	// Without the token nobody can answer as that participant.
	f.answer(f.e.Client(), f.qs[0], map[string]any{"selected": []string{"o2"}}, 401)
	// Clearing an answer removes it.
	f.answer(a, f.qs[0], map[string]any{}, 200)
	if got := f.results().Results[f.qs[0].ID].Counts["o1"]; got != 1 {
		t.Fatalf("after clearing: %d", got)
	}
	// Bad answers are rejected with a reason.
	code, body := a.Do("PUT", "/api/polls/"+f.poll.JoinCode+"/answers/"+f.qs[1].ID, map[string]any{"value": map[string]any{"words": []string{"a", "b", "c"}}})
	if code != 422 || !strings.Contains(string(body), "at most 2") {
		t.Fatalf("too many words: %d %s", code, body)
	}
}

func TestIdentifiedPoll(t *testing.T) {
	f := setup(t, settings("identified", "self", "live"), essayQ)
	c, _ := f.e.Client().Do("POST", "/api/polls/"+f.poll.JoinCode+"/join", nil)
	if c != 401 {
		t.Fatalf("anonymous join of an identified poll: %d", c)
	}
	s := f.e.NewUser(auth.RoleStudent)
	var j poll.JoinResult
	s.Call("POST", "/api/polls/"+f.poll.JoinCode+"/join", nil, 200, &j)
	if !j.Identified || j.Token != "" {
		t.Fatalf("identified join: %+v", j)
	}
	f.answer(s, f.qs[0], map[string]any{"text": "Fractions please"}, 200)
	texts := f.results().Results[f.qs[0].ID].Texts
	if len(texts) != 1 || texts[0].Name != s.User.Name {
		t.Fatalf("teacher should see the name: %+v", texts)
	}
	// Participants never see names, even in an identified poll.
	other := f.e.NewUser(auth.RoleStudent)
	other.Call("POST", "/api/polls/"+f.poll.JoinCode+"/join", nil, 200, nil)
	pub := f.view(other).Results[f.qs[0].ID].Texts
	if len(pub) != 1 || pub[0].Name != "" || pub[0].ParticipantID != "" {
		t.Fatalf("participant view leaked identity: %+v", pub)
	}
	// Identity can't be switched once people have joined.
	code, _ := f.teacher.Do("PUT", "/api/teacher/polls/"+f.poll.ID, map[string]any{"settings": settings("anonymous", "self", "live")})
	if code != 422 {
		t.Fatalf("identity change after joining: %d", code)
	}
}

func TestOptionalIdentity(t *testing.T) {
	f := setup(t, settings("optional", "self", "live"), singleQ)
	anon := f.anon(t) // no login: anonymous
	f.answer(anon, f.qs[0], map[string]any{"selected": []string{"o2"}}, 200)
	s := f.e.NewUser(auth.RoleStudent)
	var j poll.JoinResult
	s.Call("POST", "/api/polls/"+f.poll.JoinCode+"/join", map[string]bool{"identify": true}, 200, &j)
	if !j.Identified {
		t.Fatalf("chose to be identified: %+v", j)
	}
	s2 := f.e.NewUser(auth.RoleStudent)
	s2.Call("POST", "/api/polls/"+f.poll.JoinCode+"/join", map[string]bool{"identify": false}, 200, &j)
	if j.Identified || j.Token == "" {
		t.Fatalf("logged in but chose anonymous: %+v", j)
	}
	if f.results().Participants != 3 {
		t.Fatalf("participants: %d", f.results().Participants)
	}
}

func TestClassroomAudience(t *testing.T) {
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	code, _ := teacher.Do("POST", "/api/teacher/polls", map[string]any{"title": "x", "classroom_id": c.ID, "settings": map[string]any{"identity": "anonymous", "audience": "classroom", "pacing": "self", "show_results": "live"}})
	if code != 422 {
		t.Fatalf("anonymous classroom poll accepted: %d", code)
	}
	var p poll.Poll
	teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "x", "classroom_id": c.ID, "settings": map[string]any{"identity": "identified", "audience": "classroom", "pacing": "self", "show_results": "live"}}, 201, &p)
	teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/questions", singleQ, 201, nil)
	teacher.Call("POST", "/api/teacher/polls/"+p.ID+"/status", map[string]string{"status": "open"}, 200, nil)
	outsider := e.NewUser(auth.RoleStudent)
	if code, _ := outsider.Do("POST", "/api/polls/"+p.JoinCode+"/join", nil); code != 403 {
		t.Fatalf("outsider joined a classroom poll: %d", code)
	}
	member := e.NewUser(auth.RoleStudent)
	if _, err := e.App.Content.EnsureEnrolled(context.Background(), c.ID, member.User.ID, "S1"); err != nil {
		t.Fatal(err)
	}
	member.Call("POST", "/api/polls/"+p.JoinCode+"/join", nil, 200, nil)
	// Someone else's classroom can't be used.
	other := e.NewUser(auth.RoleTeacher)
	if code, _ := other.Do("POST", "/api/teacher/polls", map[string]any{"title": "x", "classroom_id": c.ID}); code != 404 {
		t.Fatalf("foreign classroom: %d", code)
	}
}

func TestPresenterPacing(t *testing.T) {
	f := setup(t, settings("anonymous", "presenter", "presenter"), singleQ, cloudQ)
	a := f.anon(t)
	v := f.view(a)
	if len(v.Questions) != 1 || v.Questions[0].ID != f.qs[0].ID || v.Total != 2 {
		t.Fatalf("presenter pacing shows one question: %+v", v.Questions)
	}
	f.answer(a, f.qs[1], map[string]any{"words": []string{"early"}}, 409) // not the current question
	f.answer(a, f.qs[0], map[string]any{"selected": []string{"o3"}}, 200)
	if f.view(a).Results != nil {
		t.Fatal("results shown before the presenter revealed them")
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/present", map[string]any{"index": 0, "revealed": true}, 200, nil)
	if r := f.view(a).Results[f.qs[0].ID]; r.Counts["o3"] != 1 {
		t.Fatalf("revealed results: %+v", r)
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/present", map[string]any{"index": 1, "revealed": false}, 200, nil)
	v = f.view(a)
	if v.Questions[0].ID != f.qs[1].ID || v.Results != nil {
		t.Fatalf("moved to question 2: %+v", v)
	}
	f.answer(a, f.qs[1], map[string]any{"words": []string{"now"}}, 200)
	if code, _ := f.teacher.Do("POST", "/api/teacher/polls/"+f.poll.ID+"/present", map[string]any{"index": 5}); code != 422 {
		t.Fatalf("present out of range: %d", code)
	}
}

func TestVisibilityEditingAndClosing(t *testing.T) {
	f := setup(t, map[string]any{"identity": "anonymous", "audience": "anyone", "pacing": "self", "show_results": "never", "allow_edit": false}, singleQ)
	a := f.anon(t)
	f.answer(a, f.qs[0], map[string]any{"selected": []string{"o1"}}, 200)
	if f.view(a).Results != nil {
		t.Fatal("results shown with show_results=never")
	}
	f.answer(a, f.qs[0], map[string]any{"selected": []string{"o2"}}, 409) // editing off
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/status", map[string]string{"status": "closed"}, 200, nil)
	b := f.e.Client()
	if code, _ := b.Do("POST", "/api/polls/"+f.poll.JoinCode+"/join", nil); code != 409 {
		t.Fatalf("join after closing: %d", code)
	}
	// Draft polls aren't visible to participants.
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/status", map[string]string{"status": "draft"}, 200, nil)
	if code, _ := b.Do("GET", "/api/polls/"+f.poll.JoinCode, nil); code != 409 {
		t.Fatalf("draft poll visible: %d", code)
	}
	// Another teacher can't see or control it.
	other := f.e.NewUser(auth.RoleTeacher)
	if code, _ := other.Do("GET", "/api/teacher/polls/"+f.poll.ID+"/results", nil); code != 404 {
		t.Fatalf("foreign results: %d", code)
	}
}

func TestLiveResultsOverWebSocket(t *testing.T) {
	f := setup(t, settings("anonymous", "self", "live"), singleQ)
	presenter := f.teacher.Dial("/ws/teacher/polls/" + f.poll.ID)
	first := apptest.ReadUntil(t, presenter, "results", 3*time.Second)
	if first["participants"].(float64) != 0 {
		t.Fatalf("initial: %v", first["participants"])
	}
	a := f.anon(t)
	audience := a.Dial("/ws/polls/" + f.poll.JoinCode)
	apptest.ReadUntil(t, audience, "update", 3*time.Second)
	f.answer(a, f.qs[0], map[string]any{"selected": []string{"o2"}}, 200)
	f.e.App.Poll.Flush(context.Background())

	msg := apptest.ReadUntil(t, presenter, "results", 3*time.Second)
	raw, _ := json.Marshal(msg)
	var res poll.TeacherResults
	_ = json.Unmarshal(raw, &res)
	if res.Results[f.qs[0].ID].Counts["o2"] != 1 || res.Participants != 1 {
		t.Fatalf("presenter live results: %s", raw)
	}
	up := apptest.ReadUntil(t, audience, "update", 3*time.Second)
	raw, _ = json.Marshal(up)
	var pub poll.PublicPoll
	_ = json.Unmarshal(raw, &pub)
	if pub.Results[f.qs[0].ID].Counts["o2"] != 1 || pub.Answers != nil {
		t.Fatalf("audience update: %s", raw)
	}
	// Moving to the next state reaches participants too.
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/status", map[string]string{"status": "closed"}, 200, nil)
	f.e.App.Poll.Flush(context.Background())
	for {
		up = apptest.ReadUntil(t, audience, "update", 3*time.Second)
		if up["status"] == "closed" {
			break
		}
	}
}

// A 1×1 PNG.
var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

func TestFileAnswers(t *testing.T) {
	f := setup(t, nil, fileQ, audioQ)
	a := f.anon(t)
	upload := func(q poll.Question, name, ct string, body []byte) (int, string) {
		a.Headers["X-File-Name"] = url.QueryEscape(name)
		code, out := a.Raw("POST", "/api/polls/"+f.poll.JoinCode+"/files/"+q.ID, ct, body)
		delete(a.Headers, "X-File-Name")
		return code, string(out)
	}
	if code, out := upload(f.qs[0], "page.html", "image/png", []byte("<html><script>alert(1)</script></html>")); code != 422 {
		t.Fatalf("HTML disguised as PNG: %d %s", code, out)
	}
	if code, out := upload(f.qs[0], "big.png", "image/png", append(png, make([]byte, 1<<20)...)); code != 422 || !strings.Contains(out, "larger") {
		t.Fatalf("over the 1 MB limit: %d %s", code, out)
	}
	code, out := upload(f.qs[0], "diagram.png", "image/png", png)
	if code != 200 {
		t.Fatalf("upload: %d %s", code, out)
	}
	// A JSON answer can't fake a file.
	f.answer(a, f.qs[0], map[string]any{"file": map[string]string{"id": "x"}}, 422)

	r := f.results().Results[f.qs[0].ID]
	if len(r.Files) != 1 || r.Files[0].File.Name != "diagram.png" || r.Files[0].File.ContentType != "image/png" {
		t.Fatalf("teacher file list: %+v", r.Files)
	}
	if pub := f.view(a).Results[f.qs[0].ID]; pub.Files != nil || pub.Responses != 1 {
		t.Fatalf("participants see only the count: %+v", pub)
	}
	code, body := f.teacher.Do("GET", "/api/teacher/polls/"+f.poll.ID+"/files/"+r.Files[0].File.ID, nil)
	if code != 200 || string(body) != string(png) {
		t.Fatalf("download: %d", code)
	}
	other := f.e.NewUser(auth.RoleTeacher)
	if code, _ := other.Do("GET", "/api/teacher/polls/"+f.poll.ID+"/files/"+r.Files[0].File.ID, nil); code != 404 {
		t.Fatalf("someone else downloaded a file: %d", code)
	}
	// Re-uploading replaces; clearing the answer deletes the file.
	upload(f.qs[0], "v2.png", "image/png", png)
	var n int
	_ = f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM poll.files`).Scan(&n)
	if n != 1 {
		t.Fatalf("files after re-upload: %d", n)
	}
	f.answer(a, f.qs[0], map[string]any{}, 200)
	_ = f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM poll.files`).Scan(&n)
	if n != 0 {
		t.Fatalf("files after clearing: %d", n)
	}
	// Audio: WebM bytes labelled by the browser as audio.
	webm := append([]byte{0x1a, 0x45, 0xdf, 0xa3}, []byte("\x9fB\x86\x81\x01B\xf7\x81\x01B\xf2\x81\x04B\xf3\x81\x08B\x82\x84webm")...)
	if code, out := upload(f.qs[1], "recording.webm", "audio/webm;codecs=opus", webm); code != 200 {
		t.Fatalf("audio upload: %d %s", code, out)
	}
}

func TestModerationAndExport(t *testing.T) {
	f := setup(t, settings("anonymous", "self", "live"), cloudQ, essayQ)
	a, b := f.anon(t), f.anon(t)
	f.answer(a, f.qs[0], map[string]any{"words": []string{"great", "rude"}}, 200)
	f.answer(b, f.qs[0], map[string]any{"words": []string{"great"}}, 200)
	f.answer(a, f.qs[1], map[string]any{"text": "=HYPERLINK(\"http://evil\")"}, 200)
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/moderation", map[string]any{"question_id": f.qs[0].ID, "word": "Rude", "hidden": true}, 204, nil)
	for _, w := range f.view(a).Results[f.qs[0].ID].Words {
		if w.Word == "rude" {
			t.Fatal("hidden word still shared")
		}
	}
	texts := f.results().Results[f.qs[1].ID].Texts
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/moderation", map[string]any{"question_id": f.qs[1].ID, "participant_id": texts[0].ParticipantID, "hidden": true}, 204, nil)
	if pub := f.view(b).Results[f.qs[1].ID]; len(pub.Texts) != 0 {
		t.Fatalf("hidden answer still shared: %+v", pub.Texts)
	}
	if tv := f.results().Results[f.qs[1].ID].Texts; len(tv) != 1 || !tv[0].Hidden {
		t.Fatalf("teacher keeps hidden answers, flagged: %+v", tv)
	}
	code, csv := f.teacher.Do("GET", "/api/teacher/polls/"+f.poll.ID+"/export.csv", nil)
	if code != 200 || !strings.Contains(string(csv), `[hidden] =HYPERLINK`) || strings.Contains(string(csv), "\n1,,,great; rude,=") {
		t.Fatalf("export: %d\n%s", code, csv)
	}
	// Reset clears every answer and participant but keeps the questions.
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/reset", nil, 204, nil)
	r := f.results()
	if r.Participants != 0 || r.Results[f.qs[0].ID].Responses != 0 || len(r.Poll.Questions) != 2 {
		t.Fatalf("after reset: %+v", r)
	}
}

func TestQuestionManagement(t *testing.T) {
	f := setup(t, nil, singleQ, cloudQ, essayQ)
	// Reorder, update, delete; positions stay dense.
	ids := []string{f.qs[2].ID, f.qs[0].ID, f.qs[1].ID}
	f.teacher.Call("PUT", "/api/teacher/polls/"+f.poll.ID+"/questions/order", map[string]any{"ids": ids}, 204, nil)
	f.teacher.Call("DELETE", "/api/teacher/poll-questions/"+f.qs[0].ID, nil, 204, nil)
	var p poll.Poll
	f.teacher.Call("GET", "/api/teacher/polls/"+f.poll.ID, nil, 200, &p)
	if len(p.Questions) != 2 || p.Questions[0].ID != f.qs[2].ID || p.Questions[1].Position != 1 {
		t.Fatalf("after reorder and delete: %+v", p.Questions)
	}
	// The type can't change once answered.
	a := f.anon(t)
	f.answer(a, f.qs[1], map[string]any{"words": []string{"x"}}, 200)
	if code, _ := f.teacher.Do("PUT", "/api/teacher/poll-questions/"+f.qs[1].ID, essayQ); code != 409 {
		t.Fatalf("type change with answers: %d", code)
	}
	if code, body := f.teacher.Do("POST", "/api/teacher/polls/"+f.poll.ID+"/questions", map[string]any{"type": "LIKERT", "text": "x", "body": map[string]any{"rows": []map[string]string{{"text": "a"}}, "scale": []string{"a", "b"}}}); code != 422 {
		t.Fatalf("bad Likert: %d %s", code, body)
	}
	// Every type can be created through the API.
	for _, typ := range poll.AllTypes {
		body := map[string]any{"type": typ, "text": "Question [[1]]", "body": map[string]any{
			"options": []map[string]string{{"text": "a"}, {"text": "b"}}, "left": []map[string]string{{"text": "a"}, {"text": "b"}},
			"right": []map[string]string{{"text": "1"}, {"text": "2"}}, "rows": []map[string]string{{"text": "r"}},
			"columns": []map[string]string{{"text": "c1"}, {"text": "c2"}}}}
		if code, out := f.teacher.Do("POST", "/api/teacher/polls/"+f.poll.ID+"/questions", body); code != 201 {
			t.Errorf("%s: %d %s", typ, code, out)
		}
	}
}
