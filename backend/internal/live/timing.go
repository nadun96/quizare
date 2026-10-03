package live

import (
	"math/rand/v2"
	"time"

	"github.com/nadun96/quizplatform/internal/quiz"
)

// Grace allows for network jitter when validating saves against deadlines
// and when expiring attempts (ADR-07: now() <= deadline + grace).
const Grace = 2 * time.Second

func ptrTime(t time.Time) *time.Time { return &t }
func ptrInt64(v int64) *int64        { return &v }

// admit starts the pre-quiz countdown (FR-SS-05).
func admit(a *Attempt, now time.Time, countdownSec int) {
	a.State = StateAdmitted
	a.CountdownDeadline = ptrTime(now.Add(time.Duration(countdownSec) * time.Second))
}

// start begins the quiz. quizLimitSec is the effective quiz limit (0 = none),
// extensionSec the session-wide plus student extensions, and qLimitSec the
// first question's limit (0 = none).
func start(a *Attempt, now time.Time, quizLimitSec, extensionSec, qLimitSec int) {
	a.State = StateInProgress
	a.StartedAt = ptrTime(now)
	a.CountdownDeadline = nil
	a.Current = 0
	a.QuizDeadline = nil
	if quizLimitSec > 0 {
		a.QuizDeadline = ptrTime(now.Add(time.Duration(quizLimitSec+extensionSec) * time.Second))
	}
	setQuestionDeadline(a, now, qLimitSec)
}

func setQuestionDeadline(a *Attempt, now time.Time, qLimitSec int) {
	a.QuestionDeadline = nil
	if qLimitSec > 0 {
		a.QuestionDeadline = ptrTime(now.Add(time.Duration(qLimitSec) * time.Second))
	}
}

// advance moves to the next question; it reports false when there is none,
// meaning the attempt should be submitted.
func advance(a *Attempt, now time.Time, total int, nextLimitSec func(index int) int) bool {
	if a.Current+1 >= total {
		return false
	}
	a.Current++
	setQuestionDeadline(a, now, nextLimitSec(a.Current))
	return true
}

func submit(a *Attempt, now time.Time) {
	a.State = StateSubmitted
	a.SubmittedAt = ptrTime(now)
	a.CountdownDeadline, a.QuizDeadline, a.QuestionDeadline = nil, nil, nil
	a.QuizRemainingMs, a.QuestionRemainingMs, a.PausedAt = nil, nil, nil
}

// pause stores remaining time instead of deadlines so that paused time never
// counts, even across a restart (ADR-05 rule, BA "paused time does not count").
func pause(a *Attempt, now time.Time) {
	if a.State != StateInProgress {
		return
	}
	a.State = StatePaused
	a.PausedAt = ptrTime(now)
	if a.QuizDeadline != nil {
		a.QuizRemainingMs = ptrInt64(max(0, a.QuizDeadline.Sub(now).Milliseconds()))
		a.QuizDeadline = nil
	}
	if a.QuestionDeadline != nil {
		a.QuestionRemainingMs = ptrInt64(max(0, a.QuestionDeadline.Sub(now).Milliseconds()))
		a.QuestionDeadline = nil
	}
}

func resume(a *Attempt, now time.Time) {
	if a.State != StatePaused {
		return
	}
	a.State = StateInProgress
	a.PausedAt = nil
	if a.QuizRemainingMs != nil {
		a.QuizDeadline = ptrTime(now.Add(time.Duration(*a.QuizRemainingMs) * time.Millisecond))
		a.QuizRemainingMs = nil
	}
	if a.QuestionRemainingMs != nil {
		a.QuestionDeadline = ptrTime(now.Add(time.Duration(*a.QuestionRemainingMs) * time.Millisecond))
		a.QuestionRemainingMs = nil
	}
}

// extend adds time to the quiz limit (FR-SS-09, AC-08). It works for running
// and paused attempts; attempts not yet started pick it up at start.
func extend(a *Attempt, seconds int) {
	d := time.Duration(seconds) * time.Second
	switch {
	case a.QuizDeadline != nil:
		a.QuizDeadline = ptrTime(a.QuizDeadline.Add(d))
	case a.QuizRemainingMs != nil:
		a.QuizRemainingMs = ptrInt64(*a.QuizRemainingMs + d.Milliseconds())
	}
}

// EffectiveQuestionDeadline caps the question deadline at the quiz deadline:
// the quiz limit ends the attempt even if question timers remain (BA §7).
func EffectiveQuestionDeadline(a *Attempt) *time.Time {
	switch {
	case a.QuestionDeadline == nil:
		return a.QuizDeadline
	case a.QuizDeadline == nil || a.QuestionDeadline.Before(*a.QuizDeadline):
		return a.QuestionDeadline
	default:
		return a.QuizDeadline
	}
}

// acceptsAnswers reports whether a save at now is within the deadlines.
func acceptsAnswers(a *Attempt, now time.Time) bool {
	if a.State != StateInProgress {
		return false
	}
	if d := EffectiveQuestionDeadline(a); d != nil && now.After(d.Add(Grace)) {
		return false
	}
	return true
}

// ---------- violation policy (ADR-08 refinement, FR-PR-03/05) ----------

const (
	ActionInvalidated = "invalidated"
	ActionWarned      = "warned"
	ActionLogged      = "logged"
)

// decide applies the configured violation policy to the warnings used so far.
func decide(policy string, allowedWarnings, warningsSoFar int) string {
	switch policy {
	case "log_only":
		return ActionLogged
	case "warn_then_invalidate":
		if warningsSoFar < allowedWarnings {
			return ActionWarned
		}
		return ActionInvalidated
	default: // "invalidate", the BA default
		return ActionInvalidated
	}
}

// ---------- ordering ----------

// newOrders builds a per-attempt question order and choice orders.
// MATCH right-hand items and DRAG items are always shuffled, because their
// authored order reveals the answer.
func newOrders(questions []quiz.Question, shuffleQuestions bool, shuffleOptions func(q quiz.Question) bool, rng *rand.Rand) ([]int, map[string][]string) {
	order := make([]int, len(questions))
	for i := range order {
		order[i] = i
	}
	if shuffleQuestions {
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	}
	opts := map[string][]string{}
	for _, q := range questions {
		var list []quiz.Choice
		switch {
		case q.Type == quiz.Match:
			list = q.Body.Right
		case q.Type == quiz.Drag:
			list = q.Body.Options
		case shuffleOptions(q) && (q.Type == quiz.Single || q.Type == quiz.Multi || q.Type == quiz.BlankOpt):
			list = q.Body.Options
		default:
			continue
		}
		ids := make([]string, len(list))
		for i, c := range list {
			ids[i] = c.ID
		}
		rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		if q.Type == quiz.Drag && len(q.Body.Zones) == 0 && len(ids) > 1 && sameOrder(ids, q.Key.Order) {
			ids[0], ids[1] = ids[1], ids[0] // never present an ordering question already solved
		}
		opts[q.ID] = ids
	}
	return order, opts
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// applyOrder returns the student's view of q with choices in this attempt's order.
func applyOrder(q quiz.Question, ids []string) quiz.StudentQuestion {
	v := q.StudentView()
	if len(ids) == 0 {
		return v
	}
	reorder := func(cs []quiz.Choice) []quiz.Choice {
		by := map[string]quiz.Choice{}
		for _, c := range cs {
			by[c.ID] = c
		}
		out := make([]quiz.Choice, 0, len(cs))
		for _, id := range ids {
			if c, ok := by[id]; ok {
				out = append(out, c)
			}
		}
		if len(out) != len(cs) {
			return cs
		}
		return out
	}
	if q.Type == quiz.Match {
		v.Body.Right = reorder(v.Body.Right)
	} else {
		v.Body.Options = reorder(v.Body.Options)
	}
	return v
}
