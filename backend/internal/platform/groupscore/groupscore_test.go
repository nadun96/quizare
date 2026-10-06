package groupscore

import (
	"slices"
	"testing"
)

func TestCombine(t *testing.T) {
	cases := []struct {
		acc, calc string
		scores    []float64
		members   int
		want      float64
	}{
		{"all", "sum", []float64{2, 1}, 3, 3},
		{"all", "average", []float64{2, 1}, 3, 1},
		{"all", "max", []float64{1, 2}, 2, 2},
		{"all", "min", []float64{1, 2}, 3, 0},
		{"all", "min", []float64{1, 2}, 2, 1},
		{"best", "average", []float64{1, 2}, 2, 2},
		{"first", "max", []float64{1, 2}, 2, 1},
		{"captain", "sum", []float64{0.5}, 4, 0.5},
		{"all", "average", nil, 3, 0},
	}
	for _, c := range cases {
		if got := Combine(c.acc, c.calc, c.scores, c.members); got != c.want {
			t.Errorf("%s/%s %v of %d = %g, want %g", c.acc, c.calc, c.scores, c.members, got, c.want)
		}
	}
}

func TestMaxFactorAndRanks(t *testing.T) {
	if MaxFactor("all", "sum", 4) != 4 || MaxFactor("all", "average", 4) != 1 || MaxFactor("first", "sum", 4) != 1 || MaxFactor("all", "sum", 0) != 1 {
		t.Fatal("max factor")
	}
	if got := Ranks([]float64{9, 7, 7, 3}); !slices.Equal(got, []int{1, 2, 2, 4}) {
		t.Fatalf("ranks %v", got)
	}
}
