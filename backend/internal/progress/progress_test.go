package progress

import (
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t.Add(10 * time.Hour)
}

func badge(p Progress, id string) Badge {
	for _, b := range p.Badges {
		if b.ID == id {
			return b
		}
	}
	panic(id)
}

func TestPointsAndLevels(t *testing.T) {
	if QuizXP(85, true) != 115 || QuizXP(40, false) != 50 || QuizXP(130, true) != 130 || QuizXP(-5, false) != 10 {
		t.Fatal("quiz points")
	}
	for xp, want := range map[int]int{0: 1, 99: 1, 100: 2, 299: 2, 300: 3, 600: 4, 1000: 5} {
		if got := LevelOf(xp); got != want {
			t.Fatalf("LevelOf(%d) = %d, want %d", xp, got, want)
		}
	}
	p := Compute([]Activity{{Title: "A", At: day("2026-10-05"), Pct: 90, Passed: true}}, []Activity{{Title: "T", At: day("2026-10-06")}}, day("2026-10-07"))
	if p.XP != 120+60 || p.Level != 2 || p.LevelXP != 100 || p.NextLevelXP != 300 || p.Quizzes != 1 || p.Tutorials != 1 {
		t.Fatalf("progress = %+v", p)
	}
	if len(p.Recent) != 2 || p.Recent[0].Kind != "tutorial" || p.Recent[1].XP != 120 {
		t.Fatalf("recent = %+v", p.Recent)
	}
}

func TestWeeklyStreak(t *testing.T) {
	// Weeks starting 28 Sep, 5 Oct and 12 Oct, a gap, then 26 Oct.
	q := []Activity{{At: day("2026-09-30")}, {At: day("2026-10-05")}, {At: day("2026-10-18")}, {At: day("2026-10-26")}}
	p := Compute(q, nil, day("2026-10-28"))
	if p.Streak != 1 || !p.ThisWeek || p.BestStreak != 3 {
		t.Fatalf("streak = %d this week %v best %d", p.Streak, p.ThisWeek, p.BestStreak)
	}
	// Nothing yet this week: last week's streak still stands until the week ends.
	p = Compute(q[:3], nil, day("2026-10-21"))
	if p.Streak != 3 || p.ThisWeek {
		t.Fatalf("streak through this week = %d", p.Streak)
	}
	// A whole week missed breaks it.
	if p = Compute(q[:3], nil, day("2026-10-28")); p.Streak != 0 {
		t.Fatalf("broken streak = %d", p.Streak)
	}
}

func TestBadges(t *testing.T) {
	q := []Activity{
		{At: day("2026-10-01"), Pct: 30, Passed: false},
		{At: day("2026-10-02"), Pct: 70, Passed: true},
		{At: day("2026-10-03"), Pct: 100, Passed: true},
		{At: day("2026-10-04"), Pct: 60, Passed: true},
	}
	p := Compute(q, nil, day("2026-10-05"))
	for id, earned := range map[string]bool{"first_quiz": true, "comeback": true, "full_marks": true, "on_a_roll": true, "ten_quizzes": false, "tutorial": false, "four_weeks": false} {
		if b := badge(p, id); b.Earned != earned {
			t.Fatalf("%s earned = %v", id, b.Earned)
		}
	}
	if b := badge(p, "on_a_roll"); !b.EarnedAt.Equal(day("2026-10-04")) {
		t.Fatalf("on a roll earned at %v", b.EarnedAt)
	}
	if b := badge(p, "ten_quizzes"); b.Have != 4 || b.Need != 10 {
		t.Fatalf("ten quizzes = %+v", b)
	}
	// No quizzes at all: nothing earned, nothing broken.
	empty := Compute(nil, nil, day("2026-10-05"))
	if empty.XP != 0 || empty.Level != 1 || empty.Streak != 0 || badge(empty, "first_quiz").Earned || len(empty.Recent) != 0 {
		t.Fatalf("empty = %+v", empty)
	}
}
