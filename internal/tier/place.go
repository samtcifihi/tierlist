package tier

import (
	"errors"
	"fmt"
	"math"
	"math/big"
)

// ErrTooFewEntries is returned when asked to place fewer than two entries:
// with one there is no span between the worst and best entry, and not
// much point in a tier list.
var ErrTooFewEntries = errors.New("a tier list needs at least two entries")

// GroupRule chooses how a group of entries within the draw-margin of each
// other is placed.
type GroupRule int

const (
	// MiddleEntry places the whole group where its middle entry would go
	// alone. When a group has two middle entries in different tiers,
	// Options.Prefer picks the tier.
	MiddleEntry GroupRule = iota
	// Alternate places the whole group in the highest tier any member
	// would go alone, or the lowest if Options.Prefer is Lower.
	Alternate
)

// Direction picks between a higher and a lower tier.
type Direction int

const (
	Higher Direction = iota
	Lower
)

// Options are the display options that decide how entries are grouped
// and placed.
type Options struct {
	// DrawMargin groups neighboring entries whose ratings differ by at
	// most this much; groups chain through their members.
	DrawMargin float64
	Rule       GroupRule
	Prefer     Direction
}

func (o Options) validate() error {
	if !(o.DrawMargin >= 0) { // also rejects NaN
		return fmt.Errorf("the draw-margin must be 0 or more, not %v", o.DrawMargin)
	}
	if o.Rule != MiddleEntry && o.Rule != Alternate {
		return fmt.Errorf("unknown group rule %d", o.Rule)
	}
	if o.Prefer != Higher && o.Prefer != Lower {
		return fmt.Errorf("unknown direction %d", o.Prefer)
	}
	return nil
}

// Place assigns each entry of a ranked list to a tier of t. ratings holds
// the entries' Bayes Elo ratings from best to worst. The result holds, in
// the same order, the index in t.Tiers of each entry's tier.
func Place(ratings []float64, t Template, o Options) ([]int, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := o.validate(); err != nil {
		return nil, err
	}
	n := len(ratings)
	if n < 2 {
		return nil, ErrTooFewEntries
	}
	for i, r := range ratings {
		if math.IsNaN(r) || math.IsInf(r, 0) {
			return nil, fmt.Errorf("entry %d has rating %v", i+1, r)
		}
		if i > 0 && r > ratings[i-1] {
			return nil, errors.New("ratings must be sorted from best to worst")
		}
	}

	// alone[i] is the tier entry i would go in by its position alone.
	alone := make([]int, n)
	for i := range alone {
		alone[i] = t.TierAt(big.NewRat(int64(n-1-i), int64(n-1)))
	}

	placed := make([]int, n)
	for start := 0; start < n; {
		end := start + 1 // the group is entries start to end-1
		for end < n && ratings[end-1]-ratings[end] <= o.DrawMargin {
			end++
		}
		tier := o.groupTier(alone[start:end])
		for i := start; i < end; i++ {
			placed[i] = tier
		}
		start = end
	}
	return placed, nil
}

// groupTier returns the tier for a group whose members, best first, would
// go alone in the tiers listed in alone.
func (o Options) groupTier(alone []int) int {
	// Tier indices grow downwards, so the higher of two tiers is the
	// smaller index.
	pick := func(a, b int) int {
		if o.Prefer == Higher {
			return min(a, b)
		}
		return max(a, b)
	}
	if o.Rule == Alternate {
		// Members are best first, so the extremes are at the ends.
		return pick(alone[0], alone[len(alone)-1])
	}
	// With an odd number of members, both indices are the middle one.
	m := len(alone)
	return pick(alone[(m-1)/2], alone[m/2])
}
