// Package pairing chooses the next pair of entries to compare, as
// described in the README's "Choosing the next pair" section.
package pairing

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/samtcifihi/tierlist/internal/bayeselo"
)

const (
	// RepeatPenalty multiplies a pair's score once for each earlier
	// comparison of the same two entries.
	RepeatPenalty = 0.5
	// NewEntryBonus multiplies the score of a pair that includes an entry
	// with no comparisons yet.
	NewEntryBonus = 10.0
	// tieTolerance counts scores this close, relatively, as tied, so that
	// rounding does not decide between pairs that are really equal.
	tieTolerance = 1e-9
)

// Next returns the next two entries to compare, in the order to show them.
// res is the Bayes Elo fit of history, the comparisons so far in the order
// they were made. If focus is not empty, only pairs that include at least
// one of its entries are considered. rng breaks ties and picks the sides.
func Next(res *bayeselo.Result, history []bayeselo.Comparison, focus []int, rng *rand.Rand) (first, second int, err error) {
	n := len(res.Ratings)
	if n < 2 {
		return 0, 0, errors.New("pairing: there must be at least two entries")
	}
	perEntry := make([]int, n)
	perPair := make(map[[2]int]int)
	for k, c := range history {
		if c.A < 0 || c.A >= n || c.B < 0 || c.B >= n || c.A == c.B {
			return 0, 0, fmt.Errorf("pairing: comparison %d is between entries %d and %d", k+1, c.A, c.B)
		}
		perEntry[c.A]++
		perEntry[c.B]++
		perPair[key(c.A, c.B)]++
	}
	var focused []bool
	if len(focus) > 0 {
		focused = make([]bool, n)
		for _, i := range focus {
			if i < 0 || i >= n {
				return 0, 0, fmt.Errorf("pairing: cannot focus on entry %d, outside 0 to %d", i, n-1)
			}
			focused[i] = true
		}
	}
	last := [2]int{-1, -1}
	if len(history) > 0 {
		c := history[len(history)-1]
		last = key(c.A, c.B)
	}

	// Keep the best-scoring pair, choosing uniformly among ties. The last
	// pair is skipped; if it is the only candidate, pick keeps it.
	pick, best, ties := last, 0.0, 0
	for i := range n {
		for j := i + 1; j < n; j++ {
			p := [2]int{i, j}
			if p == last || (focused != nil && !focused[i] && !focused[j]) {
				continue
			}
			s := score(res, i, j, perPair[p], perEntry[i] == 0 || perEntry[j] == 0)
			switch {
			case ties == 0 || s > best*(1+tieTolerance):
				pick, best, ties = p, s, 1
			case s >= best*(1-tieTolerance):
				ties++
				if rng.IntN(ties) == 0 {
					pick = p
				}
			}
		}
	}
	if rng.IntN(2) == 0 {
		return pick[0], pick[1], nil
	}
	return pick[1], pick[0], nil
}

// score is a candidate pair's expected information gain, adjusted for
// earlier comparisons of the pair and for including an uncompared entry.
func score(res *bayeselo.Result, i, j, repeats int, uncompared bool) float64 {
	s := res.Gain(i, j) * math.Pow(RepeatPenalty, float64(repeats))
	if uncompared {
		s *= NewEntryBonus
	}
	return s
}

// key orders a pair of entries so that it can be looked up either way
// round.
func key(a, b int) [2]int {
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}
}
