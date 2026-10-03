package live

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/internal/quiz"
)

var t0 = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

func TestCountdownAndStart(t *testing.T) {
	a := &Attempt{State: StateWaiting}
	admit(a, t0, 60)
	if a.State != StateAdmitted || !a.CountdownDeadline.Equal(t0.Add(60*time.Second)) {
		t.Fatalf("admit: %+v", a)
	}
	start(a, t0.Add(10*time.Second), 600, 0, 30)
	if a.State != StateInProgress || a.CountdownDeadline != nil {
		t.Fatal("start did not clear countdown")
	}
	if !a.QuizDeadline.Equal(t0.Add(610*time.Second)) || !a.QuestionDeadline.Equal(t0.Add(40*time.Second)) {
		t.Fatalf("deadlines: %v %v", a.QuizDeadline, a.QuestionDeadline)
	}
}

func TestNoLimitsMeansNoDeadlines(t *testing.T) {
	a := &Attempt{}
	start(a, t0, 0, 300, 0)
	if a.QuizDeadline != nil || a.QuestionDeadline != nil || EffectiveQuestionDeadline(a) != nil {
		t.Fatal("deadlines set without limits")
	}
	if !acceptsAnswers(a, t0.Add(24*time.Hour)) {
		t.Fatal("unlimited attempt rejected a save")
	}
}

// AC-04 / AC-05 ingredients: question deadline is capped by the quiz deadline.
func TestQuizLimitCapsQuestionTimer(t *testing.T) {
	a := &Attempt{}
	start(a, t0, 20, 0, 30)
	if !EffectiveQuestionDeadline(a).Equal(t0.Add(20 * time.Second)) {
		t.Fatalf("effective question deadline = %v", EffectiveQuestionDeadline(a))
	}
	if !acceptsAnswers(a, t0.Add(21*time.Second)) {
		t.Fatal("save inside grace rejected")
	}
	if acceptsAnswers(a, t0.Add(23*time.Second)) {
		t.Fatal("save after deadline+grace accepted")
	}
}

func TestAdvance(t *testing.T) {
	a := &Attempt{}
	limits := []int{30, 0, 45}
	start(a, t0, 0, 0, limits[0])
	if !advance(a, t0.Add(5*time.Second), 3, func(i int) int { return limits[i] }) || a.Current != 1 || a.QuestionDeadline != nil {
		t.Fatalf("advance to 2nd: %+v", a)
	}
	advance(a, t0.Add(6*time.Second), 3, func(i int) int { return limits[i] })
	if !a.QuestionDeadline.Equal(t0.Add(51 * time.Second)) {
		t.Fatalf("3rd deadline %v", a.QuestionDeadline)
	}
	if advance(a, t0.Add(7*time.Second), 3, func(i int) int { return limits[i] }) {
		t.Fatal("advanced past the last question")
	}
}

// AC-07: paused time does not count against any limit.
func TestPauseResume(t *testing.T) {
	a := &Attempt{}
	start(a, t0, 600, 0, 60)
	pause(a, t0.Add(100*time.Second))
	if a.State != StatePaused || a.QuizDeadline != nil || *a.QuizRemainingMs != 500_000 || *a.QuestionRemainingMs != 0 {
		t.Fatalf("pause: %+v", a)
	}
	if acceptsAnswers(a, t0.Add(101*time.Second)) {
		t.Fatal("paused attempt accepted a save")
	}
	resume(a, t0.Add(1000*time.Second)) // paused for 900 s
	if !a.QuizDeadline.Equal(t0.Add(1500 * time.Second)) {
		t.Fatalf("resume deadline = %v", a.QuizDeadline)
	}
}

// AC-08: a student with 2 minutes left extended by 5 minutes has 7 minutes.
func TestExtendAC08(t *testing.T) {
	a := &Attempt{}
	start(a, t0, 600, 0, 0)
	now := t0.Add(480 * time.Second) // 2 minutes left
	extend(a, 300)
	if got := a.QuizDeadline.Sub(now); got != 7*time.Minute {
		t.Fatalf("remaining = %v, want 7m", got)
	}
	pause(a, now)
	extend(a, 60)
	if *a.QuizRemainingMs != 8*60*1000 {
		t.Fatalf("extend while paused: %d", *a.QuizRemainingMs)
	}
}

func TestStartIncludesExtensions(t *testing.T) {
	a := &Attempt{}
	start(a, t0, 600, 120, 0) // session-wide + student extensions given before starting
	if !a.QuizDeadline.Equal(t0.Add(720 * time.Second)) {
		t.Fatalf("deadline %v", a.QuizDeadline)
	}
}

func TestViolationPolicy(t *testing.T) {
	cases := []struct {
		policy  string
		allowed int
		so      int
		want    string
	}{
		{"invalidate", 3, 0, ActionInvalidated},
		{"log_only", 0, 99, ActionLogged},
		{"warn_then_invalidate", 1, 0, ActionWarned},
		{"warn_then_invalidate", 1, 1, ActionInvalidated},
		{"warn_then_invalidate", 2, 1, ActionWarned},
		{"", 0, 0, ActionInvalidated},
	}
	for _, c := range cases {
		if got := decide(c.policy, c.allowed, c.so); got != c.want {
			t.Errorf("decide(%s,%d,%d) = %s, want %s", c.policy, c.allowed, c.so, got, c.want)
		}
	}
}

func TestOrdersNeverRevealAnswers(t *testing.T) {
	rows, _ := quiz.ParseQuestionsCSV([]byte(quiz.Template))
	var qs []quiz.Question
	for i, r := range rows {
		q := r.Question
		q.ID = string(rune('a' + i))
		qs = append(qs, q)
	}
	for seed := uint64(0); seed < 200; seed++ {
		rng := rand.New(rand.NewPCG(seed, seed))
		order, opts := newOrders(qs, true, func(quiz.Question) bool { return false }, rng)
		if len(order) != len(qs) {
			t.Fatal("order length")
		}
		seen := map[int]bool{}
		for _, i := range order {
			seen[i] = true
		}
		if len(seen) != len(qs) {
			t.Fatal("question order is not a permutation")
		}
		for _, q := range qs {
			if q.Type == quiz.Drag && len(q.Body.Zones) == 0 && sameOrder(opts[q.ID], q.Key.Order) {
				t.Fatalf("seed %d: ordering question shown in the correct order", seed)
			}
			if q.Type == quiz.Single && opts[q.ID] != nil {
				t.Fatal("options shuffled although option_order is fixed")
			}
			if q.Type == quiz.Match && len(opts[q.ID]) != len(q.Body.Right) {
				t.Fatal("match right side not shuffled")
			}
		}
	}
	single := qs[0]
	v := applyOrder(single, []string{"o3", "o1", "o2"})
	if v.Body.Options[0].ID != "o3" || len(v.Body.Options) != 3 {
		t.Fatalf("applyOrder = %+v", v.Body.Options)
	}
}
