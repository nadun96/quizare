package tutorial_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/llm"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/tutorial"
)

// seconds reads a student's time and leaves on a page straight from the table.
func (f *fx) visit(t *testing.T, runID, pageID string) (seconds, leaves, allowed int, locked bool) {
	t.Helper()
	err := f.e.Pool.QueryRow(context.Background(), `SELECT seconds, leaves, allowed_leaves, locked_at IS NOT NULL FROM tutorial.visits WHERE run_id=$1 AND page_id=$2`,
		runID, pageID).Scan(&seconds, &leaves, &allowed, &locked)
	if err != nil {
		t.Fatal(err)
	}
	return
}

// rewind moves a visit's last report into the past, as if time had passed.
func (f *fx) rewind(t *testing.T, pageID string, secs int) {
	t.Helper()
	if _, err := f.e.Pool.Exec(context.Background(), `UPDATE tutorial.visits SET last_at = now() - make_interval(secs => $2) WHERE page_id=$1`, pageID, secs); err != nil {
		t.Fatal(err)
	}
}

func runID(t *testing.T, f *fx, tutorialID string, n int) string {
	t.Helper()
	var pr tutorial.Progress
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tutorialID+"/progress", nil, 200, &pr)
	return pr.Students[n].RunID
}

func TestLeavingAndTime(t *testing.T) {
	f := setup(t)
	tu, qs := f.tutorial(t, "Leaving")
	link := map[string]any{"type": "link", "url": "https://en.wikipedia.org/wiki/Cell", "text": "Wikipedia"}
	if c := code(t, f.teacher, "POST", "/api/teacher/tutorials/"+tu.ID+"/pages", map[string]any{"title": "x", "leaving": "never"}, 422); c != "leaving" {
		t.Fatal(c)
	}
	read := f.page(t, tu.ID, map[string]any{"title": "Read", "blocks": []map[string]any{{"type": "text", "text": "Read me"}}, "lock_after": 3})
	if read.Leaving != "allowed" || read.LockAfter != 0 {
		t.Fatalf("defaults = %+v", read)
	}
	rec := f.page(t, tu.ID, map[string]any{"blocks": []map[string]any{link}, "question_ids": []string{qs["Q1"].ID}, "leaving": "recorded"})
	blocked := f.page(t, tu.ID, map[string]any{"question_ids": []string{qs["Q3"].ID}, "checkpoint": "answered", "leaving": "blocked", "lock_after": 2})
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/status", map[string]any{"status": "published"}, 200, nil)
	s := f.student(t)
	base := "/api/tutorials/" + tu.ID

	var v tutorial.RunView
	s.Call("POST", base+"/open", nil, 200, fresh(&v))
	if v.Page.Leaving != "allowed" || v.Page.Locked {
		t.Fatalf("page 0 = %+v", v.Page)
	}
	// The reader's byline (D-107).
	if v.Tutorial.Teacher != f.teacher.User.Name || v.Tutorial.Classroom == "" || v.Tutorial.Topic == "" || v.Tutorial.PublishedAt == nil {
		t.Fatalf("byline = %+v", v.Tutorial)
	}
	run := runID(t, f, tu.ID, 0)

	// Time: a report adds no more than has passed since the last one, and at most a minute.
	s.Call("POST", base+"/time", map[string]int{"page": 0, "seconds": 30}, 204, nil)
	if secs, _, _, _ := f.visit(t, run, read.ID); secs > 3 {
		t.Fatalf("right after arriving: %d seconds", secs)
	}
	f.rewind(t, read.ID, 40)
	s.Call("POST", base+"/time", map[string]int{"page": 0, "seconds": 30}, 204, nil)
	f.rewind(t, read.ID, 600)
	s.Call("POST", base+"/time", map[string]int{"page": 0, "seconds": 500}, 204, nil)
	if secs, _, _, _ := f.visit(t, run, read.ID); secs < 90 || secs > 93 {
		t.Fatalf("after two reports: %d seconds, want 30 + 60", secs)
	}
	s.Call("POST", base+"/time", map[string]int{"page": 2, "seconds": 30}, 204, nil) // not the page they're on: ignored

	// Leaving where it's allowed.
	s.Call("POST", base+"/leave", map[string]any{"page": 0, "away_seconds": 20}, 200, fresh(&v))
	if _, leaves, allowed, _ := f.visit(t, run, read.ID); leaves != 0 || allowed != 1 {
		t.Fatalf("allowed leave: %d / %d", leaves, allowed)
	}

	// Recorded: through the page's own link it counts as allowed; otherwise it's recorded.
	s.Call("POST", base+"/go", map[string]int{"to": 1}, 200, nil)
	s.Call("POST", base+"/leave", map[string]any{"page": 1, "away_seconds": 15, "via_link": true}, 200, nil)
	s.Call("POST", base+"/leave", map[string]any{"page": 1, "away_seconds": 5}, 200, fresh(&v))
	if _, leaves, allowed, locked := f.visit(t, run, rec.ID); leaves != 1 || allowed != 1 || locked || v.Page.Leaves != 1 {
		t.Fatalf("recorded: %d / %d locked=%v", leaves, allowed, locked)
	}
	s.Call("POST", base+"/answers", map[string]any{"page": 1, "responses": map[string]any{qs["Q1"].ID: pick("o1")}}, 200, nil)
	s.Call("POST", base+"/go", map[string]int{"to": 2}, 200, nil)

	// Not allowed: a claimed link doesn't count on a page without one; the second leave locks it.
	s.Call("POST", base+"/leave", map[string]any{"page": 2, "away_seconds": 5, "via_link": true}, 200, fresh(&v))
	if v.Page.Leaves != 1 || v.Page.Locked || v.Page.LockAfter != 2 {
		t.Fatalf("first leave: %+v", v.Page)
	}
	s.Call("POST", base+"/leave", map[string]any{"page": 2, "away_seconds": 5}, 200, fresh(&v))
	if !v.Page.Locked || !v.Page.Questions[0].Locked || v.Page.Passed {
		t.Fatalf("locked: %+v", v.Page)
	}
	if c := code(t, s, "POST", base+"/answers", map[string]any{"page": 2, "responses": map[string]any{qs["Q3"].ID: pick("o1")}}, 409); c != "page_locked" {
		t.Fatal(c)
	}
	code(t, s, "POST", base+"/finish", nil, 409)
	s.Call("POST", base+"/go", map[string]int{"to": 1}, 200, nil) // back is still fine
	s.Call("POST", base+"/go", map[string]int{"to": 2}, 200, nil)

	// The teacher sees it all, and unlocks the page.
	var pr tutorial.Progress
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID+"/progress", nil, 200, &pr)
	if st := pr.Students[0]; st.Leaves != 3 || st.Locked != 1 || st.Seconds < 90 {
		t.Fatalf("progress = %+v", st)
	}
	var d tutorial.RunDetail
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID+"/runs/"+run, nil, 200, &d)
	if len(d.Pages) != 3 || !d.Pages[2].Locked || d.Pages[2].Leaves != 2 || d.Pages[1].AllowedLeave != 1 || d.Pages[0].AwaySeconds != 20 {
		t.Fatalf("run pages = %+v", d.Pages)
	}
	f.e.NewUser(auth.RoleTeacher).Call("POST", "/api/teacher/tutorials/"+tu.ID+"/runs/"+run+"/unlock", map[string]string{"page_id": blocked.ID}, 404, nil)
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/runs/"+run+"/unlock", map[string]string{"page_id": blocked.ID}, 200, &d)
	if d.Pages[2].Locked || d.Pages[2].Leaves != 2 {
		t.Fatalf("unlocked = %+v", d.Pages[2])
	}
	// Earlier leaves no longer count toward the lock.
	s.Call("POST", base+"/leave", map[string]any{"page": 2, "away_seconds": 5}, 200, fresh(&v))
	if v.Page.Locked || v.Page.Leaves != 1 {
		t.Fatalf("after unlocking: %+v", v.Page)
	}
	s.Call("POST", base+"/answers", map[string]any{"page": 2, "responses": map[string]any{qs["Q3"].ID: pick("o1")}}, 200, nil)
	s.Call("POST", base+"/finish", nil, 200, nil)

	// Finished: nothing more is recorded.
	s.Call("POST", base+"/leave", map[string]any{"page": 2, "away_seconds": 5}, 200, nil)
	if _, leaves, _, _ := f.visit(t, run, blocked.ID); leaves != 3 {
		t.Fatalf("after finishing: %d leaves", leaves)
	}
	f.e.NewUser(auth.RoleStudent).Call("POST", base+"/leave", map[string]any{"page": 0}, 404, nil)
}

func TestAnalytics(t *testing.T) {
	f := setup(t)
	tu, qs := f.tutorial(t, "Figures")
	read := f.page(t, tu.ID, map[string]any{"title": "Read", "blocks": []map[string]any{{"type": "text", "text": "Read me"}}})
	f.page(t, tu.ID, map[string]any{"question_ids": []string{qs["Q1"].ID, qs["Q2"].ID}})
	f.page(t, tu.ID, map[string]any{"question_ids": []string{qs["Q3"].ID}, "checkpoint": "correct", "max_tries": 1, "leaving": "recorded"})
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/status", map[string]any{"status": "published"}, 200, nil)
	base := "/api/tutorials/" + tu.ID

	// A reads for long and gets everything right; B skims and gets it all wrong.
	work := func(s *apptest.Client, choice string, finish bool) {
		s.Call("POST", base+"/open", nil, 200, nil)
		s.Call("POST", base+"/go", map[string]int{"to": 1}, 200, nil)
		s.Call("POST", base+"/answers", map[string]any{"page": 1, "responses": map[string]any{qs["Q1"].ID: pick(choice), qs["Q2"].ID: pick(choice)}}, 200, nil)
		s.Call("POST", base+"/go", map[string]int{"to": 2}, 200, nil)
		s.Call("POST", base+"/answers", map[string]any{"page": 2, "responses": map[string]any{qs["Q3"].ID: pick(choice)}}, 200, nil)
		s.Call("POST", base+"/leave", map[string]any{"page": 2}, 200, nil)
		if finish {
			s.Call("POST", base+"/finish", nil, 200, nil)
		}
	}
	a, b := f.student(t), f.student(t)
	work(a, "o1", true)
	work(b, "o2", false)
	if _, err := f.e.Pool.Exec(context.Background(), `UPDATE tutorial.visits v SET seconds = CASE WHEN r.user_id=$2 THEN 100 ELSE 10 END
		FROM tutorial.runs r WHERE r.id=v.run_id AND v.page_id=$1`, read.ID, a.User.ID); err != nil {
		t.Fatal(err)
	}

	var an tutorial.Analytics
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID+"/analytics", nil, 200, &an)
	if an.Started != 2 || an.Finished != 1 || len(an.Pages) != 3 || len(an.Students) != 2 {
		t.Fatalf("analytics = %+v", an)
	}
	p0, p1, p2 := an.Pages[0], an.Pages[1], an.Pages[2]
	if !p0.Reading || p0.Reached != 2 || p0.MedianSeconds == nil || *p0.MedianSeconds != 55 {
		t.Fatalf("reading page = %+v", p0)
	}
	if sp := p0.Then; sp == nil || sp.Questions != 3 || sp.LongerN != 1 || *sp.Longer != 100 || sp.ShorterN != 1 || *sp.Shorter != 0 {
		t.Fatalf("then = %+v", p0.Then)
	}
	if p1.Reading || len(p1.Questions) != 2 || p1.Questions[0].Answered != 2 || p1.Questions[0].Correct != 1 || *p1.Questions[0].ScorePct != 50 {
		t.Fatalf("questions page = %+v", p1)
	}
	if p2.StoppedHere != 1 || p2.OutOfTries != 1 || p2.Leaves != 2 || p2.LeftStudents != 2 {
		t.Fatalf("last page = %+v", p2)
	}
	f.e.NewUser(auth.RoleTeacher).Call("GET", "/api/teacher/tutorials/"+tu.ID+"/analytics", nil, 404, nil)
}

// bankOf fills a teacher's question bank with the four questions.
func bankOf(c *apptest.Client) []quiz.Question {
	var bank quiz.Quiz
	c.Call("GET", "/api/teacher/bank", nil, 200, &bank)
	c.Raw("POST", "/api/teacher/quizzes/"+bank.ID+"/questions/import", "text/csv", []byte(questionsCSV))
	var got struct{ Questions []quiz.Question }
	c.Call("GET", "/api/teacher/quizzes/"+bank.ID+"/questions", nil, 200, &got)
	return got.Questions
}

func TestCopyAndDuplicate(t *testing.T) {
	f := setup(t)
	tu, qs := f.tutorial(t, "Copies")
	bank := bankOf(f.teacher)
	if len(bank) != 4 {
		t.Fatalf("bank = %d", len(bank))
	}

	// From the bank: codes kept where free; from the tutorial itself: renamed.
	var got struct{ Questions []quiz.Question }
	path := "/api/teacher/tutorials/" + tu.ID + "/questions/copy"
	f.teacher.Call("POST", path, map[string]any{"ids": []string{bank[2].ID, qs["Q1"].ID}}, 201, &got)
	if len(got.Questions) != 2 || got.Questions[0].Code != "Q3-copy" || got.Questions[0].QuizID != tu.QuizID || got.Questions[1].Code != "Q1-copy" {
		t.Fatalf("copied = %+v", got.Questions)
	}
	other := f.e.NewUser(auth.RoleTeacher)
	f.teacher.Call("POST", path, map[string]any{"ids": []string{}}, 422, nil)
	other.Call("POST", path, map[string]any{"ids": []string{qs["Q1"].ID}}, 404, nil)
	f.teacher.Call("POST", path, map[string]any{"ids": []string{bankOf(other)[0].ID}}, 404, nil)

	// Duplicating a page: right after it, with copies of its questions.
	first := f.page(t, tu.ID, map[string]any{"title": "Check", "blocks": []map[string]any{{"type": "text", "text": "Hi"}},
		"question_ids": []string{qs["Q1"].ID}, "checkpoint": "answered", "leaving": "blocked", "lock_after": 3})
	last := f.page(t, tu.ID, map[string]any{"title": "End", "blocks": []map[string]any{{"type": "text", "text": "Bye"}}})
	var dup tutorial.Page
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/pages/"+first.ID+"/duplicate", nil, 201, &dup)
	if dup.Title != "Check (copy)" || dup.Leaving != "blocked" || dup.LockAfter != 3 || len(dup.QuestionIDs) != 1 || dup.QuestionIDs[0] == qs["Q1"].ID {
		t.Fatalf("duplicate = %+v", dup)
	}
	var ed tutorial.Editor
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID, nil, 200, &ed)
	if len(ed.Pages) != 3 || ed.Pages[0].ID != first.ID || ed.Pages[1].ID != dup.ID || ed.Pages[2].ID != last.ID || len(ed.Questions) != 7 {
		t.Fatalf("after duplicating: %d pages, %d questions", len(ed.Pages), len(ed.Questions))
	}
	other.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/pages/"+first.ID+"/duplicate", nil, 404, nil)
}

// fakeLLM is an in-process provider.
type fakeLLM struct {
	mu     sync.Mutex
	reqs   []llm.GradeRequest
	result llm.GradeResult
	err    error
}

func (p *fakeLLM) Grade(_ context.Context, _, _ string, r llm.GradeRequest) (llm.GradeResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, r)
	return p.result, p.err
}

func TestAIMarking(t *testing.T) {
	p := &fakeLLM{result: llm.GradeResult{Score: 4, Feedback: "Mention chlorophyll.", Rationale: "mostly there", Confidence: 0.9}}
	f := setupWith(t, apptest.WithProviders(map[string]llm.Provider{"anthropic": p, "openai": p, "google": p}))
	var tu tutorial.Tutorial
	f.teacher.Call("POST", "/api/teacher/topics/"+f.topic.ID+"/tutorials", map[string]any{"title": "Essays"}, 201, &tu)
	f.teacher.Raw("POST", "/api/teacher/quizzes/"+tu.QuizID+"/questions/import", "text/csv",
		[]byte("question_code,type,question_text,options,correct_answer,marks,evaluation\nE1,ESSAY,Explain photosynthesis,,Plants turn light into glucose,5,LLM\n"))
	var ed tutorial.Editor
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID, nil, 200, &ed)
	essay := ed.Questions[0].ID
	f.page(t, tu.ID, map[string]any{"question_ids": []string{essay}, "checkpoint": "answered"})
	f.teacher.Call("POST", "/api/teacher/tutorials/"+tu.ID+"/status", map[string]any{"status": "published"}, 200, nil)
	base := "/api/tutorials/" + tu.ID
	answer := func(s *apptest.Client, text string) tutorial.RunView {
		var v tutorial.RunView
		s.Call("POST", base+"/open", nil, 200, nil)
		s.Call("POST", base+"/answers", map[string]any{"page": 0, "responses": map[string]any{essay: map[string]string{"text": text}}}, 200, &v)
		return v
	}
	process := func() int {
		jobs := f.e.TakeJobs("llm_tutorial_mark")
		for _, raw := range jobs {
			var a llm.TutorialArgs
			json.Unmarshal(raw, &a)
			err := (&llm.TutorialWorker{Service: f.e.App.LLM}).Work(context.Background(), &river.Job[llm.TutorialArgs]{
				JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 4}, Args: a})
			var cancel *river.JobCancelError
			if err != nil && !errors.As(err, &cancel) {
				t.Fatal(err)
			}
		}
		return len(jobs)
	}

	// No key: the teacher marks it.
	s1 := f.student(t)
	if m := answer(s1, "Light.").Page.Questions[0].Mark; m == nil || !m.Pending || m.Marking {
		t.Fatalf("without a key: %+v", m)
	}
	if n := process(); n != 0 {
		t.Fatalf("%d jobs without a key", n)
	}

	// With a key: the AI marks it, with feedback, under a pseudonym.
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "anthropic", "api_key": "sk-ant-test-key-ABCD", "label": "Main"}, 201, nil)
	s2 := f.student(t)
	if m := answer(s2, "Plants use light to make glucose, says "+s2.User.Email).Page.Questions[0].Mark; m == nil || !m.Pending || !m.Marking {
		t.Fatalf("queued: %+v", m)
	}
	if n := process(); n != 1 {
		t.Fatalf("%d jobs", n)
	}
	if r := p.reqs[0]; r.Pseudonym == "" || r.MaxScore != 5 || r.ModelAnswer != "Plants turn light into glucose" || strings.Contains(r.Answer, s2.User.Email) {
		t.Fatalf("request = %+v", r)
	}
	var v tutorial.RunView
	s2.Call("GET", base+"/pages/0", nil, 200, fresh(&v))
	if m := v.Page.Questions[0].Mark; m.Pending || m.Marking || m.Score != 4 || m.Feedback != "Mention chlorophyll." {
		t.Fatalf("marked = %+v", m)
	}
	var d tutorial.RunDetail
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID+"/runs/"+runID(t, f, tu.ID, 1), nil, 200, &d)
	if a := d.Answers[0]; !a.AIMarked || a.AIFeedback != "Mention chlorophyll." || a.Status != "marked" {
		t.Fatalf("teacher sees = %+v", a)
	}

	// A provider that refuses: the answer goes to the teacher, flagged.
	p.err = &llm.PermanentError{Reason: "invalid key"}
	s3 := f.student(t)
	answer(s3, "Something")
	process()
	s3.Call("GET", base+"/pages/0", nil, 200, fresh(&v))
	if m := v.Page.Questions[0].Mark; !m.Pending || m.Marking {
		t.Fatalf("failed = %+v", m)
	}
	f.teacher.Call("GET", "/api/teacher/tutorials/"+tu.ID+"/runs/"+runID(t, f, tu.ID, 2), nil, 200, &d)
	if a := d.Answers[0]; !a.Flagged || a.Method != "manual" || a.FlagReason == "" {
		t.Fatalf("flagged = %+v", a)
	}
}
