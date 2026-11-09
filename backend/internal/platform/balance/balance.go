// Package balance shares people out among groups at random, evenly, with
// each group getting as even a share of every gender as it can (D-162):
// 8 girls among 80 students in 8 groups is one girl a group. A group can
// have a most; who doesn't fit stays out, for the teacher to place.
package balance

import "math/rand/v2"

// Person is someone to place, with their gender ("" if not given).
type Person struct {
	ID     string
	Gender string
}

// Group is a group's size so far, and how many of each gender it has.
type Group struct {
	Size    int
	Genders map[string]int
}

// Share places people in groups: the answer is, for each person in order,
// the index of their group, or -1 when every group is full. max 0 is no
// limit. With byGender, each gender is shared out on its own, the smallest
// first, so the fewest are spread the widest.
func Share(people []Person, groups []Group, max int, byGender bool, rng *rand.Rand) []int {
	out := make([]int, len(people))
	for i := range out {
		out[i] = -1
	}
	if len(groups) == 0 {
		return out
	}
	gs := make([]Group, len(groups))
	for i, g := range groups {
		gs[i] = Group{Size: g.Size, Genders: map[string]int{}}
		for k, v := range g.Genders {
			gs[i].Genders[k] = v
		}
	}
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	// Strata: by gender, or everyone together.
	strata := map[string][]int{}
	for i, p := range people {
		key := ""
		if byGender {
			key = p.Gender
		}
		strata[key] = append(strata[key], i)
	}
	keys := make([]string, 0, len(strata))
	for k := range strata {
		keys = append(keys, k)
	}
	// Smallest stratum first, "" (not given) last; ties by name, so it's stable.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && less(keys[j], keys[j-1], strata); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	for _, k := range keys {
		idx := strata[k]
		rng.Shuffle(len(idx), func(a, b int) { idx[a], idx[b] = idx[b], idx[a] })
		for _, p := range idx {
			best := -1
			// Fewest of this gender, then smallest, then a random tie-break by order.
			start := rng.IntN(len(gs))
			for n := 0; n < len(gs); n++ {
				i := (start + n) % len(gs)
				if max > 0 && gs[i].Size >= max {
					continue
				}
				if best < 0 || gs[i].Genders[k] < gs[best].Genders[k] || (gs[i].Genders[k] == gs[best].Genders[k] && gs[i].Size < gs[best].Size) {
					best = i
				}
			}
			if best < 0 {
				continue // all full
			}
			out[p] = best
			gs[best].Size++
			gs[best].Genders[k]++
		}
	}
	return out
}

func less(a, b string, strata map[string][]int) bool {
	if (a == "") != (b == "") {
		return b == ""
	}
	if len(strata[a]) != len(strata[b]) {
		return len(strata[a]) < len(strata[b])
	}
	return a < b
}
