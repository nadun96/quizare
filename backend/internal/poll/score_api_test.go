package poll_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/poll"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func scored(pacing, showAnswers, leaderboard string, speed bool) map[string]any {
	return map[string]any{"identity": "anonymous", "audience": "anyone", "pacing": pacing, "show_results": "presenter",
		"allow_edit": true, "scoring": true, "speed_bonus": speed, "show_answers": showAnswers, "leaderboard": leaderboard}
}

func keyed(q map[string]any, key map[string]any, extra ...any) map[string]any {
	out := map[string]any{"key": key}
	for k, v := range q {
		out[k] = v
	}
	for i := 0; i+1 < len(extra); i += 2 {
		out[extra[i].(string)] = extra[i+1]
	}
	return out
}

var multiQ = map[string]any{"type": "MULTI", "text": "Pick the primes", "body": map[string]any{"options": []map[string]string{{"text": "2"}, {"text": "3"}, {"text": "4"}}}}

func (f *fixture) join(t *testing.T, nick string) *player {
	t.Helper()
	c := f.e.Client()
	var j poll.JoinResult
	c.Call("POST", "/api/polls/"+f.poll.JoinCode+"/join", map[string]any{"nickname": nick}, 200, &j)
	c.Headers = map[string]string{poll.TokenHeader: j.Token}
	return &player{c, f}
}

// player is a participant's client in a scored poll.
type player struct {
	c *apptest.Client
	f *fixture
}

func (p *player) answer(q poll.Question, v map[string]any) poll.AnswerResult {
	var res poll.AnswerResult
	p.c.Call("PUT", "/api/polls/"+p.f.poll.JoinCode+"/answers/"+q.ID, map[string]any{"value": v}, 200, &res)
	return res
}

func (p *player) raw() string {
	_, b := p.c.Do("GET", "/api/polls/"+p.f.poll.JoinCode, nil)
	return string(b)
}

func (p *player) view() poll.PublicPoll {
	var v poll.PublicPoll
	p.c.Call("GET", "/api/polls/"+p.f.poll.JoinCode, nil, 200, &v)
	return v
}

func TestScoredPollLeaderboard(t *testing.T) {
	f := setup(t, scored("self", "after_answer", "everyone", false),
		keyed(singleQ, map[string]any{"correct": []string{"o2"}}),
		keyed(multiQ, map[string]any{"correct": []string{"o1", "o2"}}, "points", 200),
		essayQ)
	if f.qs[0].Key == nil || f.qs[1].Points != 200 || f.qs[2].Points != 100 {
		t.Fatalf("teacher sees keys and points: %+v", f.qs)
	}
	ann, ben, cat := f.join(t, "Ann"), f.join(t, "  Ben\x07  "), f.join(t, "")

	// Nobody sees a key before answering.
	if s := ann.raw(); strings.Contains(s, `"key":{`) || strings.Contains(s, `"keys"`) {
		t.Fatalf("answer key leaked: %s", s)
	}
	r := ann.answer(f.qs[0], map[string]any{"selected": []string{"o2"}})
	if r.Key == nil || r.Score == nil || r.Score.Points != 100 || !r.Score.Correct {
		t.Fatalf("after answering, key and score come back: %+v", r)
	}
	ann.answer(f.qs[1], map[string]any{"selected": []string{"o1", "o3"}}) // one right, one wrong: 0
	// Having seen the key, Ann can't change her answer.
	if code, body := ann.c.Do("PUT", "/api/polls/"+f.poll.JoinCode+"/answers/"+f.qs[0].ID, map[string]any{"value": map[string]any{}}); code != 409 || !strings.Contains(string(body), "answer_revealed") {
		t.Fatalf("changing a revealed answer: %d %s", code, body)
	}
	ben.answer(f.qs[0], map[string]any{"selected": []string{"o1"}})
	ben.answer(f.qs[1], map[string]any{"selected": []string{"o1", "o2"}})
	ben.answer(f.qs[2], map[string]any{"text": "fractions"})

	// The shared update never carries "after answering" keys.
	f.e.App.Poll.Flush(context.Background())
	v := ben.view()
	if _, ok := v.Keys[f.qs[1].ID]; !ok || v.Scores[f.qs[1].ID].Points != 200 {
		t.Fatalf("own view has own keys and scores: %+v %+v", v.Keys, v.Scores)
	}
	if s := cat.raw(); strings.Contains(s, `"keys"`) {
		t.Fatalf("a participant who hasn't answered sees keys: %s", s)
	}
	if len(v.Leaderboard) != 3 || v.Leaderboard[0].Name != "Ben" || v.Leaderboard[0].Score != 200 ||
		v.Leaderboard[1].Name != "Ann" || v.Leaderboard[2].Name != "Participant 3" || v.Leaderboard[2].Rank != 3 {
		t.Fatalf("leaderboard: %+v", v.Leaderboard)
	}
	if v.Me == nil || v.Me.Rank != 1 || v.MeKey != v.Leaderboard[0].Key || v.Leaderboard[0].ParticipantID != "" {
		t.Fatalf("own rank, without participant ids: %+v %s", v.Me, v.MeKey)
	}
	tr := f.results()
	if len(tr.Leaderboard) != 3 || tr.Leaderboard[0].ParticipantID == "" || tr.Leaderboard[0].Nickname != "Ben" {
		t.Fatalf("teacher leaderboard: %+v", tr.Leaderboard)
	}

	// Correcting a key rescores answers already given: now Ann wins.
	q0 := keyed(singleQ, map[string]any{"correct": []string{"o2"}}, "points", 500)
	f.teacher.Call("PUT", "/api/teacher/poll-questions/"+f.qs[0].ID, q0, 200, nil)
	if lb := ann.view().Leaderboard; lb[0].Name != "Ann" || lb[0].Score != 500 {
		t.Fatalf("after changing points: %+v", lb)
	}
	// The teacher can rename a participant.
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/moderation", map[string]any{"participant_id": tr.Leaderboard[0].ParticipantID, "nickname": "B."}, 204, nil)
	if lb := ann.view().Leaderboard; lb[1].Name != "B." {
		t.Fatalf("renamed: %+v", lb)
	}
	// Hidden responses don't count.
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/moderation", map[string]any{"question_id": f.qs[1].ID, "participant_id": tr.Leaderboard[0].ParticipantID, "hidden": true}, 204, nil)
	if lb := ann.view().Leaderboard; lb[1].Score != 0 {
		t.Fatalf("hidden answer still scored: %+v", lb)
	}
}

func TestScoringValidation(t *testing.T) {
	f := setup(t, nil, singleQ)
	bad := []map[string]any{
		keyed(singleQ, map[string]any{"correct": []string{"o9"}}),
		keyed(essayQ, map[string]any{"accepted": []string{"x"}}),
		keyed(singleQ, map[string]any{"correct": []string{"o1"}}, "points", 20000),
		keyed(singleQ, map[string]any{"correct": []string{"o1"}}, "time_limit_sec", 2),
	}
	for i, q := range bad {
		if code, body := f.teacher.Do("POST", "/api/teacher/polls/"+f.poll.ID+"/questions", q); code != 422 {
			t.Errorf("bad question %d: %d %s", i, code, body)
		}
	}
	for i, st := range []map[string]any{
		{"identity": "anonymous", "audience": "anyone", "pacing": "self", "show_results": "live", "names": "name"},
		{"identity": "anonymous", "audience": "anyone", "pacing": "self", "show_results": "live", "show_answers": "presenter"},
		{"identity": "anonymous", "audience": "anyone", "pacing": "self", "show_results": "live", "leaderboard": "all"},
	} {
		if code, body := f.teacher.Do("POST", "/api/teacher/polls", map[string]any{"title": "x", "settings": st}); code != 422 {
			t.Errorf("bad settings %d: %d %s", i, code, body)
		}
	}
	// Older clients that omit the new settings get the defaults.
	var p poll.Poll
	f.teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "x", "settings": settings("anonymous", "presenter", "live")}, 201, &p)
	if p.Leaderboard != "presenter" || p.ShowAnswers != "presenter" || p.Names != "nickname" || p.Scoring {
		t.Fatalf("defaults: %+v", p.Settings)
	}
}

func TestSpeedBonusTimeLimitAndReveal(t *testing.T) {
	f := setup(t, scored("presenter", "presenter", "presenter", true),
		keyed(singleQ, map[string]any{"correct": []string{"o1"}}, "time_limit_sec", 20),
		keyed(singleQ, map[string]any{"correct": []string{"o3"}}))
	clk := &clock{t: time.Now().Truncate(time.Second)}
	f.e.App.Poll.SetClock(clk.now)
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/present", map[string]any{"index": 0}, 200, nil)
	fast, slow, late := f.join(t, "fast"), f.join(t, "slow"), f.join(t, "late")

	if v := fast.view(); v.QuestionStartedAt == nil || *v.QuestionStartedAt != clk.now().UnixMilli() || v.Leaderboard != nil {
		t.Fatalf("clock and hidden leaderboard: %+v", v)
	}
	r := fast.answer(f.qs[0], map[string]any{"selected": []string{"o1"}})
	if r.Key != nil || r.Score != nil {
		t.Fatalf("presenter-revealed key returned early: %+v", r)
	}
	clk.add(10 * time.Second)
	slow.answer(f.qs[0], map[string]any{"selected": []string{"o1"}})
	clk.add(13 * time.Second) // past 20s + grace
	if code, body := late.c.Do("PUT", "/api/polls/"+f.poll.JoinCode+"/answers/"+f.qs[0].ID, map[string]any{"value": map[string]any{"selected": []string{"o1"}}}); code != 409 || !strings.Contains(string(body), "time_up") {
		t.Fatalf("late answer: %d %s", code, body)
	}
	lb := f.results().Leaderboard
	if lb[0].Nickname != "fast" || lb[0].Score != 100 || lb[1].Score != 75 || lb[2].Score != 0 {
		t.Fatalf("speed bonus: %+v", lb)
	}

	// Re-presenting the same question doesn't restart its clock.
	if s := slow.raw(); strings.Contains(s, `"keys"`) {
		t.Fatalf("key before reveal: %s", s)
	}
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/present", map[string]any{"index": 0, "revealed": true, "answers_revealed": true}, 200, nil)
	if code, _ := late.c.Do("PUT", "/api/polls/"+f.poll.JoinCode+"/answers/"+f.qs[0].ID, map[string]any{"value": map[string]any{"selected": []string{"o1"}}}); code != 409 {
		t.Fatalf("answering after the reveal: %d", code)
	}
	v := slow.view()
	if k, ok := v.Keys[f.qs[0].ID]; !ok || k.Correct[0] != "o1" || v.Scores[f.qs[0].ID].Points != 75 || !v.AnswersRevealed {
		t.Fatalf("revealed key: %+v", v)
	}
	if *v.QuestionStartedAt != clk.now().Add(-23*time.Second).UnixMilli() {
		t.Fatal("revealing restarted the clock")
	}
	// The next question hides the key again; untimed questions get no bonus.
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/present", map[string]any{"index": 1}, 200, nil)
	clk.add(30 * time.Second)
	late.answer(f.qs[1], map[string]any{"selected": []string{"o3"}})
	if v := late.view(); v.Keys != nil || v.AnswersRevealed {
		t.Fatalf("next question: %+v", v)
	}
	// late (0 + 100 untimed) ties fast (100); the earlier finisher is listed first.
	if lb := f.results().Leaderboard; lb[1].Nickname != "late" || lb[1].Score != 100 || lb[1].Rank != 1 || lb[2].Rank != 3 {
		t.Fatalf("untimed question: %+v", lb)
	}
	// Closing shows every key.
	f.teacher.Call("POST", "/api/teacher/polls/"+f.poll.ID+"/status", map[string]string{"status": "closed"}, 200, nil)
	if v := fast.view(); len(v.Keys) != 1 || v.Keys[f.qs[1].ID].Correct[0] != "o3" { // presenter pacing still shows only the current question
		t.Fatalf("keys after closing: %+v", v.Keys)
	}
}
