package tier

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
)

// StarOptions are the options for a generated star template.
type StarOptions struct {
	// Max is the number of stars in the top tier, at least 3.
	Max int
	// SkipZero makes the bottom tier 1 star instead of 0.
	SkipZero bool
	// Divisions splits each star into steps of 1/Divisions, so 2 gives
	// half-stars. 0 (the default) or 1 means whole stars only.
	Divisions int
	// Sizes chooses how big the tiers are.
	Sizes Sizes
}

// SizeKind is a way of sizing the tiers of a star template.
type SizeKind int

const (
	// EvenTiers each cover 1/(n-1) of [0, 1], except the top and bottom
	// tiers, which cover half that, so that an entry gets the star rating
	// nearest its position.
	EvenTiers SizeKind = iota
	// GeometricTiers grow or shrink by a constant factor from one tier to
	// the next.
	GeometricTiers
	// BetaTiers each get the share of a Beta distribution that falls on
	// their part of [0, 1] cut into n equal parts.
	BetaTiers
)

// Sizes chooses how big the tiers of a star template are.
type Sizes struct {
	Kind SizeKind
	// Factor, for GeometricTiers, is each tier's size divided by the
	// size of the one before it, counting from the best tier, or from the
	// worst with FromWorst. It must be above 0.
	Factor    float64
	FromWorst bool
	// Alpha and Beta, for BetaTiers, are the parameters of the Beta
	// distribution, both above 0. A larger Alpha makes the tiers near the
	// top bigger, a larger Beta those near the bottom, and 1 and 1 give
	// tiers all the same size.
	Alpha, Beta float64
}

// errTooSmall reports sizes so extreme that neighbouring cut-offs can't be
// told apart.
var errTooSmall = errors.New("these tier sizes are too extreme to work out; try values closer to even tiers")

// maxStarTiers guards against absurd option values allocating huge
// templates.
const maxStarTiers = 1000

// Stars generates a star template. With even sizes and n tiers, the top
// and bottom tiers each cover 1/(2*(n-1)) of [0, 1] and every other tier
// covers 1/(n-1): an entry gets the star rating nearest its position, and
// the convention decides which way an exact half rounds. o.Sizes can size
// the tiers otherwise.
func Stars(o StarOptions, c Convention) (Template, error) {
	if o.Max < 3 {
		return Template{}, fmt.Errorf("a star template needs a maximum of at least 3 stars, not %d", o.Max)
	}
	if o.Divisions < 0 {
		return Template{}, fmt.Errorf("stars cannot be divided into %d parts", o.Divisions)
	}
	d := max(o.Divisions, 1)
	lowest := 0
	if o.SkipZero {
		lowest = 1
	}
	if o.Max > maxStarTiers || d > maxStarTiers || (o.Max-lowest)*d+1 > maxStarTiers {
		return Template{}, fmt.Errorf("a star template can have at most %d tiers", maxStarTiers)
	}
	// Tier i, best first, is (top-i)/d stars.
	top := o.Max * d
	n := top - lowest*d + 1
	name := starNamer(d)
	tiers := make([]string, n)
	for i := range tiers {
		tiers[i] = name(top - i)
	}
	cutoffs, err := o.Sizes.cutoffs(n)
	if err != nil {
		return Template{}, err
	}
	title := fmt.Sprintf("%d–%d stars", lowest, o.Max)
	if d > 1 {
		title += fmt.Sprintf(" in steps of 1/%d", d)
	}
	return New(title, tiers, cutoffs, c)
}

// cutoffs returns the n-1 increasing cut-offs of n tiers of these sizes.
func (s Sizes) cutoffs(n int) ([]*big.Rat, error) {
	even := make([]*big.Rat, n-1)
	for k := range even {
		even[k] = big.NewRat(int64(2*k+1), int64(2*(n-1)))
	}
	switch s.Kind {
	case EvenTiers:
		return even, nil
	case GeometricTiers:
		return s.geometric(n)
	case BetaTiers:
		equal := make([]*big.Rat, n-1)
		for k := range equal {
			equal[k] = big.NewRat(int64(k+1), int64(n))
		}
		return s.beta(equal)
	}
	return nil, fmt.Errorf("unknown kind of tier sizes %d", s.Kind)
}

// geometric returns the cut-offs of n tiers whose sizes, from the best
// tier (or the worst with FromWorst), each are Factor times the one
// before, scaled to add up to 1.
func (s Sizes) geometric(n int) ([]*big.Rat, error) {
	if !(s.Factor > 0) || math.IsInf(s.Factor, 1) {
		return nil, fmt.Errorf("the factor between tier sizes must be a number above 0, not %v", s.Factor)
	}
	// Tier j from the bottom has size Factor^e, where e counts the tiers
	// before it; the sizes are worked out relative to the biggest, so that
	// no power overflows.
	sizes := make([]float64, n)
	for j := range sizes {
		e := n - 1 - j
		if s.FromWorst {
			e = j
		}
		sizes[j] = float64(e) * math.Log(s.Factor)
	}
	biggest := slices.Max(sizes)
	for j := range sizes {
		sizes[j] = math.Exp(sizes[j] - biggest)
	}
	// The share of the tiers below each cut-off, and above it, each summed
	// from the end the sequence starts at so that small tiers keep their
	// precision.
	below, above := make([]float64, n-1), make([]float64, n-1)
	sum := 0.0
	for j := range below {
		sum += sizes[j]
		below[j] = sum
	}
	total := sum + sizes[n-1]
	sum = 0
	for j := n - 1; j >= 1; j-- {
		sum += sizes[j]
		above[j-1] = sum
	}
	for j := range below {
		below[j] /= total
		above[j] /= total
	}
	return fromShares(below, above)
}

// beta returns the cut-offs of tiers that each get the share of a
// Beta(Alpha, Beta) distribution lying over their part of [0, 1] cut into
// equal parts, whose cut-offs are equal: the distribution's CDF at each
// of those. Beta(1, 1) is uniform, so it gives equal tiers exactly; a
// symmetric distribution gives cut-offs exactly symmetric about 1/2.
func (s Sizes) beta(equal []*big.Rat) ([]*big.Rat, error) {
	for _, p := range []float64{s.Alpha, s.Beta} {
		if !(p > 0) || p > maxBetaParam {
			return nil, fmt.Errorf("the Beta distribution's α and β must be numbers above 0 and at most %g, not %v", float64(maxBetaParam), p)
		}
	}
	if s.Alpha == 1 && s.Beta == 1 {
		return equal, nil
	}
	half := big.NewRat(1, 2)
	below, above := make([]float64, len(equal)), make([]float64, len(equal))
	for k, e := range equal {
		if s.Alpha == s.Beta && e.Cmp(half) == 0 {
			below[k], above[k] = 0.5, 0.5
			continue
		}
		// 1 - e is the cut-off k places from the other end, exactly.
		below[k] = betaCDF(ratFloat(e), s.Alpha, s.Beta)
		above[k] = betaCDF(ratFloat(equal[len(equal)-1-k]), s.Beta, s.Alpha)
	}
	return fromShares(below, above)
}

// fromShares turns the share of [0, 1] below and above each cut-off into
// exact cut-offs, using whichever share is at most 1/2, which is the one
// worked out accurately: a cut-off near 1 is 1 minus its share above.
// A share too small for a float64, which only extreme sizes give, leaves
// a tier of 2^-1100 or so: tiny, but still holding the end of [0, 1] it
// touches.
func fromShares(below, above []float64) ([]*big.Rat, error) {
	one := big.NewRat(1, 1)
	eps := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), 1100))
	times := func(k int) *big.Rat { return new(big.Rat).Mul(eps, big.NewRat(int64(k), 1)) }
	n := len(below)
	cutoffs := make([]*big.Rat, n)
	for k := range cutoffs {
		switch {
		case !(below[k] >= 0) || !(above[k] >= 0):
			return nil, errTooSmall
		case below[k] <= 0.5 && below[k] > 0:
			cutoffs[k] = new(big.Rat).SetFloat64(below[k])
		case below[k] <= 0.5:
			cutoffs[k] = times(k + 1)
		case above[k] > 0:
			cutoffs[k] = new(big.Rat).Sub(one, new(big.Rat).SetFloat64(above[k]))
		default:
			cutoffs[k] = new(big.Rat).Sub(one, times(n-k))
		}
		if k > 0 && cutoffs[k].Cmp(cutoffs[k-1]) <= 0 {
			return nil, errTooSmall
		}
	}
	return cutoffs, nil
}

// maxBetaParam bounds α and β, beyond which the tiers would be too small
// anyway and the CDF would be slow to work out.
const maxBetaParam = 1e6

func ratFloat(r *big.Rat) float64 {
	f, _ := r.Float64()
	return f
}

// betaCDF returns the regularized incomplete beta function I_x(a, b), the
// chance that a Beta(a, b) variable is at most x, for x in (0, 1).
func betaCDF(x, a, b float64) float64 {
	// The continued fraction converges quickly below (a+1)/(a+b+2); above
	// it, I_x(a, b) = 1 - I_(1-x)(b, a) turns the problem around.
	if x > (a+1)/(a+b+2) {
		return 1 - betaCDF(1-x, b, a)
	}
	lab, _ := math.Lgamma(a + b)
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	front := math.Exp(lab - la - lb + a*math.Log(x) + b*math.Log1p(-x))
	return front * betaFraction(x, a, b) / a
}

// betaFraction evaluates the continued fraction for the incomplete beta
// function by the modified Lentz method (as in Numerical Recipes, section
// 6.4).
func betaFraction(x, a, b float64) float64 {
	const tiny = 1e-300
	nonzero := func(v float64) float64 {
		if math.Abs(v) < tiny {
			return tiny
		}
		return v
	}
	c, d := 1.0, 1/nonzero(1-(a+b)*x/(a+1))
	h := d
	for m := 1.0; m <= 100000; m++ {
		num := m * (b - m) * x / ((a + 2*m - 1) * (a + 2*m))
		d = 1 / nonzero(1+num*d)
		c = nonzero(1 + num/c)
		h *= d * c
		num = -(a + m) * (a + b + m) * x / ((a + 2*m) * (a + 2*m + 1))
		d = 1 / nonzero(1+num*d)
		c = nonzero(1 + num/c)
		step := d * c
		h *= step
		if math.Abs(step-1) < 1e-15 {
			break
		}
	}
	return h
}

// fractionGlyphs spells every fraction with a denominator of 2, 3, 4, 5,
// 6 or 8 as a single character.
var fractionGlyphs = map[[2]int]string{
	{1, 2}: "½",
	{1, 3}: "⅓", {2, 3}: "⅔",
	{1, 4}: "¼", {3, 4}: "¾",
	{1, 5}: "⅕", {2, 5}: "⅖", {3, 5}: "⅗", {4, 5}: "⅘",
	{1, 6}: "⅙", {5, 6}: "⅚",
	{1, 8}: "⅛", {3, 8}: "⅜", {5, 8}: "⅝", {7, 8}: "⅞",
}

// starNamer returns a function naming units/d stars, such as "4½". When
// some fraction of a star has no single-character form (sevenths, say),
// every fraction is written out instead, as in "4 2/7", so a template
// never mixes the two styles.
func starNamer(d int) func(units int) string {
	glyphs := true
	for k := 1; k < d && glyphs; k++ {
		_, glyphs = fractionGlyphs[reduced(k, d)]
	}
	return func(units int) string {
		whole, rem := units/d, units%d
		if rem == 0 {
			return strconv.Itoa(whole)
		}
		f := reduced(rem, d)
		frac, sep := fractionGlyphs[f], ""
		if !glyphs {
			frac, sep = fmt.Sprintf("%d/%d", f[0], f[1]), " "
		}
		if whole == 0 {
			return frac
		}
		return strconv.Itoa(whole) + sep + frac
	}
}

// reduced returns num/den in lowest terms. num and den must be positive.
func reduced(num, den int) [2]int {
	a, b := num, den
	for b != 0 {
		a, b = b, a%b
	}
	return [2]int{num / a, den / a}
}

// Hogwarts returns the fixed Hogwarts template.
func Hogwarts(c Convention) (Template, error) {
	tiers := []string{"Outstanding", "Exceeds Expectations", "Acceptable", "Poor", "Dreadful", "Troll"}
	// Outstanding is the top 1/31 of [0, 1], and the tiers down to
	// Dreadful end at the top 3/31, 6/31, 10/31 and 15/31. Troll is the
	// rest. The cut-offs, increasing, are 1 minus those.
	tops := []int64{15, 10, 6, 3, 1}
	cutoffs := make([]*big.Rat, len(tops))
	for i, top := range tops {
		cutoffs[i] = big.NewRat(31-top, 31)
	}
	return New("Hogwarts", tiers, cutoffs, c)
}
