package analytics_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/analytics"
	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/quiz"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

const csvQuiz = `question_code,type,question_text,options,correct_answer,marks
Q1,SINGLE,Capital of France?,Paris|Rome|Madrid,Paris,1
Q2,BLANK_TEXT,Water boils at [[1]] C,,1=100,1
`

type fx struct {
	e       *apptest.Env
	teacher *apptest.Client
	quizID  string
}

func setup(t *testing.T) *fx {
	t.Helper()
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C", "settings": map[string]any{"student_id_required": true}}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Science"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(csvQuiz))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	return &fx{e: e, teacher: teacher, quizID: q.ID}
}

func (f *fx) session(t *testing.T) live.Session {
	var s live.Session
	f.teacher.Call("POST", "/api/teacher/quizzes/"+f.quizID+"/sessions", map[string]any{}, 201, &s)
	return s
}

// take: q1 is an option id or "", q2 a blank answer or "".
func (f *fx) take(t *testing.T, sess live.Session, number, name, q1, q2 string) {
	t.Helper()
	s := f.e.Client()
	var u auth.User
	s.Call("POST", "/api/auth/register", map[string]string{"email": strings.ToLower(number) + "@example.com", "name": name, "password": "password123", "role": "student"}, 201, &u)
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{"student_number": number}, 200, &st)
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	for _, ans := range []*quiz.Response{{Selected: []string{q1}}, {Blanks: map[string]string{"1": q2}}} {
		if (ans.Selected != nil && ans.Selected[0] != "") || (ans.Blanks != nil && ans.Blanks["1"] != "") {
			s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": ans}, 200, nil)
		}
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
	}
}

func (f *fx) process(t *testing.T) {
	t.Helper()
	for _, raw := range f.e.TakeJobs("evaluate_attempt") {
		var a eval.EvaluateAttemptArgs
		json.Unmarshal(raw, &a)
		if err := f.e.App.Eval.EvaluateAttempt(context.Background(), a.AttemptID); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, raw := range f.e.TakeJobs("analytics_recompute") {
		var a analytics.RecomputeArgs
		json.Unmarshal(raw, &a)
		if !seen[a.SessionID] {
			seen[a.SessionID] = true
			if _, err := f.e.App.Analytics.Recompute(context.Background(), a.SessionID); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSessionAnalytics(t *testing.T) {
	f := setup(t)
	sess := f.session(t)
	f.take(t, sess, "S1", "Ann", "o1", "100")  // 100%
	f.take(t, sess, "S2", "Ben", "o1", "99")   // 50%
	f.take(t, sess, "S3", "Cat", "o2", "100")  // 50%
	f.take(t, sess, "S4", "Dan", "o2", "boil") // 0%
	f.take(t, sess, "S5", "Eve", "o3", "")     // 0%
	f.process(t)

	var st analytics.SessionStats
	f.teacher.Call("GET", "/api/teacher/sessions/"+sess.ID+"/analytics", nil, 200, &st)
	c := st.Class
	if c.Joined != 5 || c.Marked != 5 || c.Mean != 40 || c.Median != 50 || c.PassRate != 60 || c.CompletionRate != 100 {
		t.Fatalf("class = %+v", c)
	}
	if c.Distribution[0] != 2 || c.Distribution[5] != 2 || c.Distribution[9] != 1 {
		t.Fatalf("distribution = %v", c.Distribution)
	}
	q1 := st.Questions[0]
	if q1.Code != "Q1" || q1.Answered != 5 || q1.PctCorrect != 40 || q1.OptionCounts["o2"] != 2 || q1.OptionLabels["o2"] != "Rome" {
		t.Fatalf("Q1 = %+v", q1)
	}
	if len(q1.CommonWrong) == 0 || q1.CommonWrong[0].Answer != "Rome" || q1.CommonWrong[0].Count != 2 {
		t.Fatalf("common wrong = %+v", q1.CommonWrong)
	}
	if q1.Discrimination == nil || *q1.Discrimination <= 0 {
		t.Fatalf("discrimination = %v (top students got Q1 right, bottom did not)", q1.Discrimination)
	}
	q2 := st.Questions[1]
	if q2.Answered != 4 || q2.PctCorrect != 50 {
		t.Fatalf("Q2 = %+v", q2)
	}
	if len(st.Students) != 5 || st.Students[0].Name != "Ann" || st.Students[0].TimeTakenSec == nil || len(st.Students[0].Answers) != 2 {
		t.Fatalf("students = %+v", st.Students[0])
	}
	f.e.NewUser(auth.RoleTeacher).Call("GET", "/api/teacher/sessions/"+sess.ID+"/analytics", nil, 404, nil)
}

func TestQuizAnalyticsAcrossSessions(t *testing.T) {
	f := setup(t)
	s1, s2 := f.session(t), f.session(t)
	f.take(t, s1, "A1", "Ann", "o1", "100")
	f.take(t, s2, "B1", "Ben", "o2", "")
	f.process(t)
	var qs analytics.QuizStats
	f.teacher.Call("GET", "/api/teacher/quizzes/"+f.quizID+"/analytics", nil, 200, &qs)
	if len(qs.Comparison) != 2 || len(qs.Students) != 2 || qs.Class.Mean != 50 || qs.Class.PassRate != 50 {
		t.Fatalf("quiz stats = %+v", qs)
	}
	if qs.Questions[0].Answered != 2 || qs.Questions[0].PctCorrect != 50 {
		t.Fatalf("rolled-up Q1 = %+v", qs.Questions[0])
	}
}

func public(t *testing.T, f *fx, token string) (int, analytics.PublicView, http.Header, string) {
	t.Helper()
	resp, err := f.e.Server.Client().Get(f.e.Server.URL + "/api/public/results/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw strings.Builder
	var v analytics.PublicView
	buf := make([]byte, 1<<16)
	for {
		n, err := resp.Body.Read(buf)
		raw.Write(buf[:n])
		if err != nil {
			break
		}
	}
	json.Unmarshal([]byte(raw.String()), &v)
	return resp.StatusCode, v, resp.Header, raw.String()
}

// AC-12 and BR-13: public pages never show names or emails.
func TestShareLinksAC12(t *testing.T) {
	f := setup(t)
	sess := f.session(t)
	f.take(t, sess, "S1", "Ann Secret", "o1", "100")
	f.take(t, sess, "S2", "Ben Secret", "o2", "")
	f.process(t)

	var l analytics.ShareLink
	f.teacher.Call("POST", "/api/teacher/share-links", map[string]any{"scope": "session", "target_id": sess.ID, "views": []string{"question_pct"}}, 201, &l)
	if len(l.Token) < 20 {
		t.Fatalf("token %q", l.Token)
	}
	status, v, h, raw := public(t, f, l.Token)
	if status != 200 || len(v.Questions) != 2 || v.Students != nil || v.PassRate != nil || v.Questions[0].PctCorrect != 50 {
		t.Fatalf("question_pct view: %d %s", status, raw)
	}
	if strings.Contains(raw, "Secret") || strings.Contains(raw, "@example.com") || strings.Contains(raw, "S1") {
		t.Fatalf("personal data on a public page: %s", raw)
	}
	if !strings.Contains(h.Get("X-Robots-Tag"), "noindex") {
		t.Fatal("noindex header missing")
	}

	// Individual view: anonymous by default, student IDs when chosen, answers only when enabled.
	var anon, ids analytics.ShareLink
	f.teacher.Call("POST", "/api/teacher/share-links", map[string]any{"scope": "session", "target_id": sess.ID, "views": []string{"individual", "pass_rate"}}, 201, &anon)
	_, v, _, raw = public(t, f, anon.Token)
	if len(v.Students) != 2 || v.Students[0].Label != "Student 1" || v.Students[0].Answers != nil || v.PassRate.PassRate != 50 || strings.Contains(raw, "Secret") {
		t.Fatalf("anonymous individual view: %s", raw)
	}
	f.teacher.Call("POST", "/api/teacher/share-links", map[string]any{"scope": "session", "target_id": sess.ID, "views": []string{"individual"},
		"identify": "student_id", "show_answers": true}, 201, &ids)
	_, v, _, raw = public(t, f, ids.Token)
	if v.Students[0].Label != "S1" || len(v.Students[0].Answers) != 2 || strings.Contains(raw, "Secret") {
		t.Fatalf("student-id view: %s", raw)
	}

	// Revoke, regenerate, expire.
	f.teacher.Call("DELETE", "/api/teacher/share-links/"+anon.ID, nil, 204, nil)
	if status, _, _, _ := public(t, f, anon.Token); status != 410 {
		t.Fatalf("revoked link status %d", status)
	}
	var regen analytics.ShareLink
	f.teacher.Call("POST", "/api/teacher/share-links/"+anon.ID+"/regenerate", nil, 200, &regen)
	if status, _, _, _ := public(t, f, anon.Token); status != 404 {
		t.Fatalf("old token after regenerate: %d", status)
	}
	if status, _, _, _ := public(t, f, regen.Token); status != 200 {
		t.Fatalf("regenerated token: %d", status)
	}
	f.teacher.Call("POST", "/api/teacher/share-links", map[string]any{"scope": "session", "target_id": sess.ID, "views": []string{"pass_rate"},
		"expires_at": time.Now().Add(-time.Hour)}, 422, nil)
	if status, _, _, _ := public(t, f, "nonsense"); status != 404 {
		t.Fatal("unknown token")
	}
	var list struct{ Links []analytics.ShareLink }
	f.teacher.Call("GET", "/api/teacher/share-links?target_id="+sess.ID, nil, 200, &list)
	if len(list.Links) != 3 || list.Links[0].Token != "" {
		t.Fatalf("list = %+v (tokens must not be listed)", list.Links)
	}
	f.e.NewUser(auth.RoleTeacher).Call("POST", "/api/teacher/share-links", map[string]any{"scope": "session", "target_id": sess.ID, "views": []string{"pass_rate"}}, 404, nil)
}

func TestCSVExportNeutralisesFormulas(t *testing.T) {
	f := setup(t)
	sess := f.session(t)
	f.take(t, sess, "S1", `=HYPERLINK("http://evil","x")`, "o1", "100")
	f.process(t)
	status, raw := f.teacher.Do("GET", "/api/teacher/sessions/"+sess.ID+"/export.csv", nil)
	if status != 200 {
		t.Fatal(status)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "student_number" || rows[0][len(rows[0])-1] != "Q2" {
		t.Fatalf("header = %v", rows[0])
	}
	if !strings.HasPrefix(rows[1][1], "'=") || rows[1][3] != "2" {
		t.Fatalf("row = %v", rows[1])
	}
}
