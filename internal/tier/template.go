// Package tier holds tier templates and places a ranked list of entries
// into tiers, as described in the README's Display and Tier templates
// sections.
package tier

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// Convention decides which tier gets a position lying exactly on the
// cut-off between two tiers.
type Convention int

const (
	// TopClosed makes the top tier closed and every other tier [a, b),
	// so a position on a cut-off belongs to the higher tier.
	TopClosed Convention = iota
	// BottomClosed makes the bottom tier closed and every other tier
	// (a, b], so a position on a cut-off belongs to the lower tier.
	BottomClosed
)

// A Template is an ordered list of named tiers and the cut-offs between
// them. Positions run from 0 (the worst entry) to 1 (the best).
type Template struct {
	Name string
	// Tiers holds the tier names, best first.
	Tiers []string
	// Cutoffs holds the len(Tiers)-1 positions where one tier becomes
	// the next, in increasing order: the first separates the bottom tier
	// from the one above it.
	Cutoffs    []*big.Rat
	Convention Convention
}

// New returns the template with the given tiers (best first), cut-offs
// (increasing) and convention, or an error if it is not well-formed.
func New(name string, tiers []string, cutoffs []*big.Rat, c Convention) (Template, error) {
	t := Template{Name: name, Tiers: tiers, Cutoffs: cutoffs, Convention: c}
	if err := t.Validate(); err != nil {
		return Template{}, err
	}
	return t, nil
}

// Validate reports whether t is well-formed: every tier is named, and
// under t's convention the cut-offs split [0, 1] into len(t.Tiers)
// non-empty ranges with no gaps or overlaps.
func (t Template) Validate() error {
	if t.Convention != TopClosed && t.Convention != BottomClosed {
		return fmt.Errorf("unknown interval convention %d", t.Convention)
	}
	if len(t.Tiers) == 0 {
		return errors.New("a template needs at least one tier")
	}
	for i, name := range t.Tiers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("tier %d has no name", i+1)
		}
	}
	if len(t.Cutoffs) != len(t.Tiers)-1 {
		return fmt.Errorf("%d tiers need %d cut-offs, not %d", len(t.Tiers), len(t.Tiers)-1, len(t.Cutoffs))
	}
	one := big.NewRat(1, 1)
	for i, c := range t.Cutoffs {
		switch {
		case c == nil:
			return fmt.Errorf("cut-off %d is missing", i+1)
		case c.Sign() < 0 || c.Cmp(one) > 0:
			return fmt.Errorf("cut-off %s is outside [0, 1]", c.RatString())
		case i > 0 && c.Cmp(t.Cutoffs[i-1]) <= 0:
			return fmt.Errorf("cut-offs must increase, but %s follows %s", c.RatString(), t.Cutoffs[i-1].RatString())
		}
	}
	// A cut-off at 0 or 1 is fine unless it leaves a tier empty.
	if n := len(t.Cutoffs); n > 0 {
		if t.Convention == TopClosed && t.Cutoffs[0].Sign() == 0 {
			return errors.New("a cut-off at 0 leaves the bottom tier empty when the top tier is the closed one")
		}
		if t.Convention == BottomClosed && t.Cutoffs[n-1].Cmp(one) == 0 {
			return errors.New("a cut-off at 1 leaves the top tier empty when the bottom tier is the closed one")
		}
	}
	return nil
}

// TierAt returns the index in t.Tiers of the tier holding position p,
// which must lie in [0, 1]. t must be well-formed.
func (t Template) TierAt(p *big.Rat) int {
	// Each cut-off below p, or on p when the top tier is closed, puts one
	// more tier beneath p's tier.
	below := 0
	for _, c := range t.Cutoffs {
		if cmp := c.Cmp(p); cmp < 0 || (cmp == 0 && t.Convention == TopClosed) {
			below++
		}
	}
	return len(t.Tiers) - 1 - below
}

// Shares returns the share of [0, 1] that each tier covers, worst tier
// first. t must be well-formed.
func (t Template) Shares() []float64 {
	shares := make([]float64, len(t.Tiers))
	below := new(big.Rat)
	for k := range shares {
		above := big.NewRat(1, 1)
		if k < len(t.Cutoffs) {
			above = t.Cutoffs[k]
		}
		shares[k], _ = new(big.Rat).Sub(above, below).Float64()
		below = above
	}
	return shares
}

var (
	fractionRE = regexp.MustCompile(`^(\d+)\s*/\s*(\d+)$`)
	decimalRE  = regexp.MustCompile(`^(\d*)(?:\.(\d*))?$`)
)

// ParseCutoff parses a cut-off written as a decimal ("0.25", ".25") or a
// fraction ("1/4"). It does not check that the cut-off lies in [0, 1].
// Unlike big.Rat's SetString, it reads every number in base 10, so
// "010/31" is ten 31sts rather than octal.
func ParseCutoff(s string) (*big.Rat, error) {
	s = strings.TrimSpace(s)
	if m := fractionRE.FindStringSubmatch(s); m != nil {
		num, _ := new(big.Int).SetString(m[1], 10)
		den, _ := new(big.Int).SetString(m[2], 10)
		if den.Sign() == 0 {
			return nil, fmt.Errorf("cut-off %q divides by zero", s)
		}
		return new(big.Rat).SetFrac(num, den), nil
	}
	if m := decimalRE.FindStringSubmatch(s); m != nil && m[1]+m[2] != "" {
		num, _ := new(big.Int).SetString(m[1]+m[2], 10)
		den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(m[2]))), nil)
		return new(big.Rat).SetFrac(num, den), nil
	}
	return nil, fmt.Errorf("cut-off %q is not a decimal like 0.25 or a fraction like 1/4", s)
}
