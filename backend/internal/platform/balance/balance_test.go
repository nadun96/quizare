package balance

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

func people(girls, boys, unknown int) []Person {
	var ps []Person
	for i := range girls {
		ps = append(ps, Person{ID: fmt.Sprint("g", i), Gender: "female"})
	}
	for i := range boys {
		ps = append(ps, Person{ID: fmt.Sprint("b", i), Gender: "male"})
	}
	for i := range unknown {
		ps = append(ps, Person{ID: fmt.Sprint("u", i)})
	}
	return ps
}

func count(ps []Person, at []int, n int) (sizes []int, girls []int) {
	sizes, girls = make([]int, n), make([]int, n)
	for i, g := range at {
		if g < 0 {
			continue
		}
		sizes[g]++
		if ps[i].Gender == "female" {
			girls[g]++
		}
	}
	return
}

// The example: 8 girls in a batch of 80, 8 groups: one girl each, 10 each.
func TestEightGirlsInEightGroups(t *testing.T) {
	for seed := range uint64(20) {
		ps := people(8, 72, 0)
		at := Share(ps, make([]Group, 8), 0, true, rand.New(rand.NewPCG(seed, 1)))
		sizes, girls := count(ps, at, 8)
		for i := range 8 {
			if girls[i] != 1 || sizes[i] != 10 {
				t.Fatalf("seed %d: sizes %v, girls %v", seed, sizes, girls)
			}
		}
	}
}

func TestUnevenSpreadsWithinOne(t *testing.T) {
	ps := people(5, 20, 4)
	at := Share(ps, make([]Group, 4), 0, true, rand.New(rand.NewPCG(7, 7)))
	sizes, girls := count(ps, at, 4)
	minS, maxS, minG, maxG := 99, 0, 99, 0
	for i := range 4 {
		minS, maxS = min(minS, sizes[i]), max(maxS, sizes[i])
		minG, maxG = min(minG, girls[i]), max(maxG, girls[i])
	}
	if maxS-minS > 1 || maxG-minG > 1 {
		t.Fatalf("sizes %v, girls %v", sizes, girls)
	}
}

// A most per group leaves the rest out; groups already partly full count.
func TestMaxAndExisting(t *testing.T) {
	ps := people(0, 10, 0)
	at := Share(ps, []Group{{Size: 2}, {Size: 0}}, 4, false, rand.New(rand.NewPCG(1, 2)))
	sizes, _ := count(ps, at, 2)
	out := 0
	for _, g := range at {
		if g < 0 {
			out++
		}
	}
	if sizes[0] != 2 || sizes[1] != 4 || out != 4 {
		t.Fatalf("sizes %v, left out %d", sizes, out)
	}
	if got := Share(ps, nil, 0, true, nil); got[0] != -1 {
		t.Fatal("no groups: nobody placed")
	}
}
