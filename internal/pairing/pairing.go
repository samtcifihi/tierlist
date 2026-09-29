// Package pairing chooses the next pair of entries to compare, as
// described in the README's "Choosing the next pair" section.
package pairing

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"

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

// Options narrow down the pairs Next considers.
type Options struct {
	// Focus, if not empty, limits the pairs to those that include at least
	// one of these entries.
	Focus []int
	// Hidden entries are never picked, though their comparisons still
	// count.
	Hidden []int
	// Skip holds pairs of entries never to pick, either way round, though
	// their comparisons still count.
	Skip [][2]int
}

// ErrNoPair is returned when no pair of entries can be picked.
var ErrNoPair = errors.New("pairing: there is no pair of entries to compare")

// Next returns the next two entries to compare, in the order to show them.
// res is the Bayes Elo fit of history, the comparisons so far in the order
// they were made. rng breaks ties and picks the sides.
func Next(res *bayeselo.Result, history []bayeselo.Comparison, opts Options, rng *rand.Rand) (first, second int, err error) {
	n := len(res.Ratings)
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
	hidden := make([]bool, n)
	for _, i := range opts.Hidden {
		if i < 0 || i >= n {
			return 0, 0, fmt.Errorf("pairing: cannot hide entry %d, outside 0 to %d", i, n-1)
		}
		hidden[i] = true
	}
	var focused []bool
	if len(opts.Focus) > 0 {
		focused = make([]bool, n)
		for _, i := range opts.Focus {
			if i < 0 || i >= n {
				return 0, 0, fmt.Errorf("pairing: cannot focus on entry %d, outside 0 to %d", i, n-1)
			}
			focused[i] = true
		}
	}
	skip := make(map[[2]int]bool, len(opts.Skip))
	for _, p := range opts.Skip {
		if p[0] < 0 || p[0] >= n || p[1] < 0 || p[1] >= n {
			return 0, 0, fmt.Errorf("pairing: cannot skip the pair of entries %d and %d, outside 0 to %d", p[0], p[1], n-1)
		}
		skip[key(p[0], p[1])] = true
	}
	candidate := func(i, j int) bool {
		return !hidden[i] && !hidden[j] && !skip[key(i, j)] && (focused == nil || focused[i] || focused[j])
	}
	last := [2]int{-1, -1}
	if len(history) > 0 {
		c := history[len(history)-1]
		last = key(c.A, c.B)
	}

	// Keep the best-scoring pair, choosing uniformly among ties. The last
	// pair is skipped, and only picked if it is the only candidate.
	pick, best, ties := last, 0.0, 0
	for i := range n {
		for j := i + 1; j < n; j++ {
			p := [2]int{i, j}
			if p == last || !candidate(i, j) {
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
	if ties == 0 && (last[0] < 0 || !candidate(last[0], last[1])) {
		return 0, 0, ErrNoPair
	}
	if rng.IntN(2) == 0 {
		return pick[0], pick[1], nil
	}
	return pick[1], pick[0], nil
}

// Queue returns k more pairs to ask after pending, the pairs already lined
// up to be asked, in order; each pair is in the order to show it. Pending
// and newly chosen pairs count as asked with their answers not yet known:
// each narrows the ratings' uncertainty as much as it is expected to (see
// bayeselo.Result.Anticipate) and counts toward repeats and the new-entry
// bonus, so the next pick looks elsewhere. res is the Bayes Elo fit of
// history. With no pending pairs, the first pick is the one Next would
// make.
func Queue(res *bayeselo.Result, history []bayeselo.Comparison, pending [][2]int, k int, opts Options, rng *rand.Rand) ([][2]int, error) {
	n := len(res.Ratings)
	h := slices.Clone(history)
	for _, p := range pending {
		if p[0] < 0 || p[0] >= n || p[1] < 0 || p[1] >= n || p[0] == p[1] {
			return nil, fmt.Errorf("pairing: pending pair of entries %d and %d", p[0], p[1])
		}
		res = res.Anticipate(p[0], p[1])
		h = append(h, bayeselo.Comparison{A: p[0], B: p[1]})
	}
	out := make([][2]int, 0, k)
	for len(out) < k {
		a, b, err := Next(res, h, opts, rng)
		if err != nil {
			return nil, err
		}
		out = append(out, [2]int{a, b})
		if len(out) < k {
			res = res.Anticipate(a, b)
			h = append(h, bayeselo.Comparison{A: a, B: b})
		}
	}
	return out, nil
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
