package tier

import (
	"fmt"
	"math/big"
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
}

// maxStarTiers guards against absurd option values allocating huge
// templates.
const maxStarTiers = 1000

// Stars generates a star template. With n tiers, the top and bottom tiers
// each cover 1/(2*(n-1)) of [0, 1] and every other tier covers 1/(n-1):
// an entry gets the star rating nearest its position, and the convention
// decides which way an exact half rounds.
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
	cutoffs := make([]*big.Rat, n-1)
	for k := range cutoffs {
		cutoffs[k] = big.NewRat(int64(2*k+1), int64(2*(n-1)))
	}
	title := fmt.Sprintf("%d–%d stars", lowest, o.Max)
	if d > 1 {
		title += fmt.Sprintf(" in steps of 1/%d", d)
	}
	return New(title, tiers, cutoffs, c)
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
