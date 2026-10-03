package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/llm"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/quiz"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

// fake is an in-process provider that records what it was sent.
type fake struct {
	mu     sync.Mutex
	keys   []string
	reqs   []llm.GradeRequest
	result llm.GradeResult
	err    error
}

func (f *fake) Grade(_ context.Context, apiKey, model string, r llm.GradeRequest) (llm.GradeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, apiKey+"|"+model)
	f.reqs = append(f.reqs, r)
	return f.result, f.err
}

type fx struct {
	e       *apptest.Env
	teacher *apptest.Client
	sess    live.Session
	p       *fake
}

const csvQuiz = `question_code,type,question_text,options,correct_answer,marks,evaluation
Q1,SINGLE,Capital of France?,Paris|Rome,Paris,1,KEY
Q2,ESSAY,Explain photosynthesis,,Plants turn light into glucose,5,LLM
`

func setup(t *testing.T, quizSettings map[string]any) *fx {
	t.Helper()
	p := &fake{result: llm.GradeResult{Score: 4, Rationale: "mentions light and glucose", Feedback: "Mention chlorophyll.", Confidence: 0.9}}
	e := apptest.New(t, apptest.WithProviders(map[string]llm.Provider{"anthropic": p, "openai": p, "google": p}))
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Q", "settings": quizSettings}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(csvQuiz))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess live.Session
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)
	return &fx{e: e, teacher: teacher, sess: sess, p: p}
}

func (f *fx) take(t *testing.T, essay string) string {
	t.Helper()
	s := f.e.NewUser(auth.RoleStudent)
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{}, 200, &st)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{"o1"}}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
	s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Text: essay}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
	for _, raw := range f.e.TakeJobs("evaluate_attempt") {
		var a eval.EvaluateAttemptArgs
		json.Unmarshal(raw, &a)
		if err := f.e.App.Eval.EvaluateAttempt(context.Background(), a.AttemptID); err != nil {
			t.Fatal(err)
		}
	}
	return st.AttemptID
}

// runLLM drains llm_mark jobs through the worker logic.
func (f *fx) runLLM(t *testing.T) int {
	t.Helper()
	jobs := f.e.TakeJobs("llm_mark")
	for _, raw := range jobs {
		var a llm.MarkArgs
		json.Unmarshal(raw, &a)
		if err := f.e.App.LLM.Process(context.Background(), a.LLMRequest); err != nil {
			var perm *llm.PermanentError
			if !errors.As(err, &perm) {
				t.Fatal(err)
			}
			f.e.App.Eval.LLMFailed(context.Background(), a.AttemptID, a.QuestionID, a.Mode, perm.Reason)
		}
	}
	return len(jobs)
}

func result(t *testing.T, f *fx, id string) eval.AttemptResult {
	var out struct{ Results []eval.AttemptResult }
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/results", nil, 200, &out)
	for _, r := range out.Results {
		if r.AttemptID == id {
			return r
		}
	}
	t.Fatal("no result")
	return eval.AttemptResult{}
}

// AC-11: only the last 4 characters are ever visible, to anyone.
func TestKeyPrivacyAC11(t *testing.T) {
	f := setup(t, nil)
	const secret = "sk-ant-very-secret-key-WXYZ"
	var k llm.Key
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "anthropic", "api_key": secret, "label": "Main"}, 201, &k)
	if k.Last4 != "WXYZ" || k.Model != "claude-opus-5-5" || !k.IsDefault {
		t.Fatalf("key = %+v", k)
	}
	_, raw := f.teacher.Do("GET", "/api/teacher/llm-keys", nil)
	if strings.Contains(string(raw), "very-secret") {
		t.Fatal("list leaks the key")
	}
	admin := f.e.NewUser(auth.RoleAdmin)
	for _, path := range []string{"/api/teacher/llm-keys", "/api/admin/users"} {
		_, raw := admin.Do("GET", path, nil)
		if strings.Contains(string(raw), "very-secret") {
			t.Fatalf("%s leaks the key to an admin", path)
		}
	}
	admin.Call("GET", "/api/teacher/llm-keys", nil, 403, nil)
	f.e.NewUser(auth.RoleTeacher).Call("POST", "/api/teacher/llm-keys/"+k.ID+"/test", nil, 404, nil)

	var stored []byte
	f.e.Pool.QueryRow(context.Background(), `SELECT ciphertext || wrapped_dek FROM llm.keys WHERE id=$1`, k.ID).Scan(&stored)
	if strings.Contains(string(stored), "secret") {
		t.Fatal("plaintext key in the database")
	}
	f.teacher.Call("POST", "/api/teacher/llm-keys/"+k.ID+"/test", nil, 200, &k)
	if k.LastTestOK == nil || !*k.LastTestOK || f.p.keys[0] != secret+"|claude-opus-5-5" {
		t.Fatalf("test call: %+v %v", k, f.p.keys)
	}
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "nope", "api_key": "x"}, 422, nil)
	f.teacher.Call("DELETE", "/api/teacher/llm-keys/"+k.ID, nil, 204, nil)
}

// AC-10: an essay set to LLM with a valid key gets a score and feedback, and the teacher can override it.
func TestLLMMarkingAC10(t *testing.T) {
	f := setup(t, nil)
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "openai", "model": "gpt-test", "api_key": "sk-openai-1234"}, 201, nil)
	essay := "Plants use light energy to make glucose. Email me at kid@example.com"
	id := f.take(t, essay)
	r := result(t, f, id)
	if r.Complete || r.Marks[1].Status != "pending" {
		t.Fatalf("before LLM: %+v", r.Marks[1])
	}
	if n := f.runLLM(t); n != 1 {
		t.Fatalf("llm jobs = %d", n)
	}
	r = result(t, f, id)
	m := r.Marks[1]
	if m.Status != "marked" || !m.AIMarked || *m.Score != 4 || m.AIFeedback != "Mention chlorophyll." || !r.Complete || r.Score != 5 {
		t.Fatalf("after LLM: %+v / %+v", m, r)
	}
	// NFR-05 / ADR-16: no names, emails or student IDs reach the provider.
	sent := f.p.reqs[0]
	if strings.Contains(sent.Answer, "kid@example.com") || !strings.HasPrefix(sent.Pseudonym, "student-") || sent.ModelAnswer == "" {
		t.Fatalf("request = %+v", sent)
	}
	blob, _ := json.Marshal(sent)
	var name string
	f.e.Pool.QueryRow(context.Background(), `SELECT u.name FROM auth.users u JOIN live.attempts a ON a.user_id=u.id WHERE a.id=$1`, id).Scan(&name)
	if strings.Contains(string(blob), name) {
		t.Fatal("student name sent to the provider")
	}
	f.teacher.Call("PUT", "/api/teacher/marks/"+id+"/"+m.QuestionID, map[string]any{"score": 2}, 200, nil)
	if got := result(t, f, id); got.Score != 3 {
		t.Fatalf("override = %+v", got)
	}
}

func TestPermanentFailureFallsBackToManual(t *testing.T) {
	f := setup(t, nil)
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "openai", "model": "gpt-test", "api_key": "sk-openai-1234"}, 201, nil)
	f.p.err = &llm.PermanentError{Reason: "the API key was rejected by the provider"}
	id := f.take(t, "Light makes glucose in leaves.")
	f.runLLM(t)
	m := result(t, f, id).Marks[1]
	if m.Status != "needs_manual" || !m.Flagged || !strings.Contains(m.FlagReason, "rejected") {
		t.Fatalf("mark = %+v", m)
	}
}

func TestAIFeedbackForKeyQuestionsAndRemarkAndEstimate(t *testing.T) {
	f := setup(t, map[string]any{"feedback_mode": "both"})
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "google", "model": "gemini-test", "api_key": "AIza-1234"}, 201, nil)
	var est llm.Estimate
	id := f.take(t, "Plants make glucose from light energy.")
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/llm-estimate", nil, 200, &est)
	if est.Answers != 2 || est.EstimatedTokens <= 0 {
		t.Fatalf("estimate = %+v", est)
	}
	if n := f.runLLM(t); n != 2 { // essay mark + feedback for the key-marked SINGLE
		t.Fatalf("jobs = %d", n)
	}
	r := result(t, f, id)
	if r.Marks[0].AIFeedback == "" || *r.Marks[0].Score != 1 || r.Marks[0].AIMarked {
		t.Fatalf("key question with AI feedback: %+v", r.Marks[0])
	}
	var fbReq llm.GradeRequest
	for _, q := range f.p.reqs {
		if q.FeedbackOnly {
			fbReq = q
		}
	}
	if fbReq.KnownScore != 1 || !strings.Contains(fbReq.Answer, "Paris") {
		t.Fatalf("feedback request = %+v", fbReq)
	}
	// FR-EV-09: re-run LLM marking.
	f.p.result.Score = 5
	f.teacher.Call("POST", "/api/teacher/marks/"+id+"/"+r.Marks[1].QuestionID+"/remark", nil, 202, nil)
	if got := result(t, f, id).Marks[1].Status; got != "pending" {
		t.Fatalf("status after remark request = %s", got)
	}
	f.runLLM(t)
	if got := result(t, f, id); *got.Marks[1].Score != 5 {
		t.Fatalf("remark score = %v", *got.Marks[1].Score)
	}
	f.teacher.Call("POST", "/api/teacher/marks/"+id+"/"+r.Marks[0].QuestionID+"/remark", nil, 400, nil)
}

func TestUniqueJobsPreventDoubleBilling(t *testing.T) {
	f := setup(t, nil)
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "openai", "model": "m", "api_key": "sk-openai-1234"}, 201, nil)
	id := f.take(t, "Plants use sunlight.")
	var qid string
	f.e.Pool.QueryRow(context.Background(), `SELECT question_id FROM eval.marks WHERE attempt_id=$1 AND status='pending'`, id).Scan(&qid)
	// A second enqueue of the same answer while the first is queued is ignored.
	f.teacher.Call("POST", "/api/teacher/marks/"+id+"/"+qid+"/remark", nil, 202, nil)
	var n int
	f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM river_job WHERE kind='llm_mark'`).Scan(&n)
	if n != 1 {
		t.Fatalf("llm jobs queued = %d", n)
	}
}
