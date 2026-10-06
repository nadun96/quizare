// Package groupscore combines members' marks into a group's mark (V2-07,
// D-43, D-44). Polls and live sessions share these rules.
package groupscore

import "math"

// Combine turns the marks a group's members earned on one question into the
// group's mark. scores must be in answer order (earliest first) for "first";
// for "captain" pass only the captain's mark. Average and lowest count
// members who didn't answer as 0, so a group can't lift its mark by letting
// only its strongest member answer.
func Combine(acceptance, calc string, scores []float64, members int) float64 {
	if len(scores) == 0 {
		return 0
	}
	switch acceptance {
	case "first", "captain":
		return scores[0]
	case "best":
		calc = "max"
	}
	sum, hi, lo := 0.0, scores[0], scores[0]
	for _, x := range scores {
		sum += x
		hi = math.Max(hi, x)
		lo = math.Min(lo, x)
	}
	switch calc {
	case "average":
		return Round(sum / float64(max(members, len(scores))))
	case "max":
		return hi
	case "min":
		if len(scores) < members {
			return 0
		}
		return lo
	}
	return Round(sum)
}

// MaxFactor is how many times one question's maximum a group can earn:
// a total of everyone's marks scales with the group's size.
func MaxFactor(acceptance, calc string, members int) int {
	if acceptance == "all" && calc == "sum" {
		return max(members, 1)
	}
	return 1
}

// Round keeps two decimals.
func Round(x float64) float64 { return math.Round(x*100) / 100 }

// Ranks gives 1-based ranks for scores sorted from high to low; equal
// scores share a rank (1, 2, 2, 4).
func Ranks(sorted []float64) []int {
	out := make([]int, len(sorted))
	for i, x := range sorted {
		if i > 0 && x == sorted[i-1] {
			out[i] = out[i-1]
		} else {
			out[i] = i + 1
		}
	}
	return out
}
