// Package progress is a student's own progress (D-97): experience points,
// levels, a weekly streak and badges, worked out from what the platform
// already records: released quiz results and finished tutorials. Nothing
// is stored, and nobody but the student sees it: there's no class ranking.
package progress

import (
	"math"
	"sort"
	"time"
)

// Activity is one thing a student did that counts.
type Activity struct {
	Kind   string    `json:"kind"` // quiz or tutorial
	Title  string    `json:"title"`
	At     time.Time `json:"at"`
	Pct    float64   `json:"-"`
	Passed bool      `json:"-"`
	XP     int       `json:"xp"`
}

// Badge is earned once, for good.
type Badge struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Earned      bool       `json:"earned"`
	EarnedAt    *time.Time `json:"earned_at,omitempty"`
	Have        int        `json:"have"` // towards Need, while not earned
	Need        int        `json:"need"`
}

// Progress is what the student sees.
type Progress struct {
	XP          int        `json:"xp"`
	Level       int        `json:"level"`
	LevelXP     int        `json:"level_xp"`      // where this level starts
	NextLevelXP int        `json:"next_level_xp"` // where the next one starts
	Streak      int        `json:"streak_weeks"`  // weeks in a row, up to this week or last
	BestStreak  int        `json:"best_streak_weeks"`
	ThisWeek    bool       `json:"active_this_week"`
	Quizzes     int        `json:"quizzes"`
	Tutorials   int        `json:"tutorials"`
	Badges      []Badge    `json:"badges"`
	Recent      []Activity `json:"recent"` // the newest five
}

// The points: taking part, the score, passing, finishing a tutorial.
const (
	xpQuiz     = 10
	xpPassed   = 20
	xpTutorial = 60
)

// QuizXP is what a released quiz result earns: 10 for taking part, its
// percentage, and 20 for passing.
func QuizXP(pct float64, passed bool) int {
	xp := xpQuiz + int(math.Round(math.Max(0, math.Min(100, pct))))
	if passed {
		xp += xpPassed
	}
	return xp
}

// LevelStart is the XP where a level starts: 0, 100, 300, 600, 1000…
func LevelStart(level int) int { return 50 * level * (level - 1) }

// LevelOf is the level reached with xp, from 1.
func LevelOf(xp int) int {
	l := 1
	for LevelStart(l+1) <= xp {
		l++
	}
	return l
}

// week is the Monday (UTC) that starts the week of t.
func week(t time.Time) time.Time {
	t = t.UTC()
	d := (int(t.Weekday()) + 6) % 7
	return time.Date(t.Year(), t.Month(), t.Day()-d, 0, 0, 0, 0, time.UTC)
}

// Compute works out a student's progress from their quizzes and tutorials.
func Compute(quizzes, tutorials []Activity, now time.Time) Progress {
	var all []Activity
	for _, q := range quizzes {
		q.Kind, q.XP = "quiz", QuizXP(q.Pct, q.Passed)
		all = append(all, q)
	}
	for _, t := range tutorials {
		t.Kind, t.XP = "tutorial", xpTutorial
		all = append(all, t)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].At.Before(all[j].At) })

	p := Progress{Quizzes: len(quizzes), Tutorials: len(tutorials), Recent: []Activity{}}
	weeks := map[time.Time]bool{}
	for _, a := range all {
		p.XP += a.XP
		weeks[week(a.At)] = true
	}
	p.Level = LevelOf(p.XP)
	p.LevelXP, p.NextLevelXP = LevelStart(p.Level), LevelStart(p.Level+1)

	// The streak: weeks in a row with something done. It lasts through the
	// current week, so it isn't lost on a Monday morning.
	this := week(now)
	p.ThisWeek = weeks[this]
	from := this
	if !p.ThisWeek {
		from = from.AddDate(0, 0, -7)
	}
	for w := from; weeks[w]; w = w.AddDate(0, 0, -7) {
		p.Streak++
	}
	var sorted []time.Time
	for w := range weeks {
		sorted = append(sorted, w)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	run := 0
	for i, w := range sorted {
		if i > 0 && sorted[i-1].AddDate(0, 0, 7).Equal(w) {
			run++
		} else {
			run = 1
		}
		p.BestStreak = max(p.BestStreak, run)
	}

	p.Badges = badges(all, sorted)
	for i := len(all) - 1; i >= 0 && len(p.Recent) < 5; i-- {
		p.Recent = append(p.Recent, all[i])
	}
	return p
}

// badges are earned in time order; each records when.
func badges(all []Activity, weeks []time.Time) []Badge {
	out := []Badge{
		{ID: "first_quiz", Name: "First quiz", Description: "Finish a quiz and get its result.", Need: 1},
		{ID: "full_marks", Name: "Full marks", Description: "Score 100 % on a quiz.", Need: 1},
		{ID: "on_a_roll", Name: "On a roll", Description: "Pass three quizzes in a row.", Need: 3},
		{ID: "comeback", Name: "Comeback", Description: "Pass a quiz right after one you didn't pass.", Need: 1},
		{ID: "ten_quizzes", Name: "Ten quizzes", Description: "Finish ten quizzes.", Need: 10},
		{ID: "tutorial", Name: "Tutorial finisher", Description: "Finish a tutorial.", Need: 1},
		{ID: "four_weeks", Name: "Four weeks running", Description: "Do a quiz or tutorial four weeks in a row.", Need: 4},
	}
	byID := map[string]*Badge{}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	earn := func(id string, have int, at time.Time) {
		b := byID[id]
		if b.Earned {
			return
		}
		b.Have = min(have, b.Need)
		if have >= b.Need {
			t := at
			b.Earned, b.EarnedAt = true, &t
		}
	}
	quizzes, passedRun, lastFailed := 0, 0, false
	for _, a := range all {
		switch a.Kind {
		case "quiz":
			quizzes++
			earn("first_quiz", quizzes, a.At)
			earn("ten_quizzes", quizzes, a.At)
			if a.Pct >= 100 {
				earn("full_marks", 1, a.At)
			}
			if a.Passed {
				passedRun++
				if lastFailed {
					earn("comeback", 1, a.At)
				}
			} else {
				passedRun = 0
			}
			earn("on_a_roll", max(passedRun, byID["on_a_roll"].Have), a.At)
			lastFailed = !a.Passed
		case "tutorial":
			earn("tutorial", 1, a.At)
		}
	}
	run := 0
	for i, w := range weeks {
		if i > 0 && weeks[i-1].AddDate(0, 0, 7).Equal(w) {
			run++
		} else {
			run = 1
		}
		earn("four_weeks", max(run, byID["four_weeks"].Have), w.AddDate(0, 0, 6))
	}
	return out
}
