package tier

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
)

// placeNames places ratings with Place and returns the tier names,
// separated by spaces.
func placeNames(t *testing.T, ratings []float64, tmpl Template, o Options) string {
	t.Helper()
	placed, err := Place(ratings, tmpl, o)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(placed))
	for i, tier := range placed {
		names[i] = tmpl.Tiers[tier]
	}
	return strings.Join(names, " ")
}

func TestPlace(t *testing.T) {
	// Seven entries stand each in the middle of a seventh of [0, 1], at
	// 13/14, 11/14, 9/14, 1/2, 5/14, 3/14 and 1/14, so alone they would go
	// top, top, middle, middle, middle, bottom, bottom.
	three := []string{"top", "middle", "bottom"}
	thirds := mustNew(t, "thirds", three, rats("1/3", "2/3"), TopClosed)
	thirdsDown := mustNew(t, "thirds", three, rats("1/3", "2/3"), BottomClosed)
	halves := mustNew(t, "halves", []string{"top", "bottom"}, rats("1/2"), TopClosed)
	halvesDown := mustNew(t, "halves", []string{"top", "bottom"}, rats("1/2"), BottomClosed)
	spaced := []float64{700, 600, 500, 400, 300, 200, 100}
	pair := []float64{700, 601, 600, 400, 300, 200, 100}         // groups the 2nd and 3rd: top and middle
	triple := []float64{700, 602, 600, 598, 300, 200, 100}       // groups the 2nd to 4th: top, middle, middle
	quadruple := []float64{703, 702, 701, 700, 300, 200, 100}    // groups the 1st to 4th: top, top, middle, middle
	lowQuadruple := []float64{700, 600, 503, 502, 501, 500, 100} // groups the 3rd to 6th: middle × 3, bottom

	tests := []struct {
		name    string
		tmpl    Template
		ratings []float64
		opts    Options
		want    string
	}{
		{"no groups", thirds, spaced, Options{DrawMargin: 50},
			"top top middle middle middle bottom bottom"},
		{"no groups, bottom closed", thirdsDown, spaced, Options{DrawMargin: 50},
			"top top middle middle middle bottom bottom"},
		// Three entries stand at 5/6, 1/2 and 1/6: the middle one is on the
		// cut-off, so the convention decides.
		{"on a cut-off, top closed", halves, []float64{3, 2, 1}, Options{},
			"top top bottom"},
		{"on a cut-off, bottom closed", halvesDown, []float64{3, 2, 1}, Options{},
			"top bottom bottom"},
		{"odd group: middle entry decides", thirds, triple, Options{DrawMargin: 5},
			"top middle middle middle middle bottom bottom"},
		{"even group, middles split, higher", thirds, pair, Options{DrawMargin: 5},
			"top top top middle middle bottom bottom"},
		{"even group, middles split, lower", thirds, pair, Options{DrawMargin: 5, Prefer: Lower},
			"top middle middle middle middle bottom bottom"},
		{"even group, middles agree", thirds, lowQuadruple, Options{DrawMargin: 5, Prefer: Lower},
			"top top middle middle middle middle bottom"},
		{"alternate rule, highest", thirds, quadruple, Options{DrawMargin: 5, Rule: Alternate},
			"top top top top middle bottom bottom"},
		{"alternate rule, lowest", thirds, quadruple, Options{DrawMargin: 5, Rule: Alternate, Prefer: Lower},
			"middle middle middle middle middle bottom bottom"},
		// The README's example: with a draw-margin of 1, ratings 3, 2 and
		// 1 chain into one group even though 3 and 1 differ by 2.
		{"groups chain", thirds, []float64{3, 2, 1}, Options{DrawMargin: 1},
			"middle middle middle"},
		{"chain broken", thirds, []float64{4, 2, 1}, Options{DrawMargin: 1},
			"top middle middle"},
		{"equal ratings group at margin 0", thirds, []float64{5, 5, 1}, Options{},
			"top top bottom"},
		{"one big group", thirds, spaced, Options{DrawMargin: 1000},
			"middle middle middle middle middle middle middle"},
		// At 3/4 and 1/4.
		{"two entries", thirds, []float64{2, 1}, Options{},
			"top bottom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := placeNames(t, tt.ratings, tt.tmpl, tt.opts); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

// With the star cut-offs, an entry gets the star rating nearest its
// position, and the convention decides which way an exact half rounds.
func TestPlaceStars(t *testing.T) {
	// At 21/22, 19/22, ..., 1/22: the middle one, at 1/2, is halfway
	// between 2 and 3 stars of 0–5.
	eleven := []float64{11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	tests := []struct {
		name    string
		c       Convention
		ratings []float64
		want    string
	}{
		// At 11/12, 3/4, 7/12, 5/12, 1/4 and 1/12.
		{"one entry per star", TopClosed, []float64{6, 5, 4, 3, 2, 1}, "5 4 3 2 1 0"},
		{"a half rounds up", TopClosed, eleven, "5 4 4 3 3 3 2 2 1 1 0"},
		{"a half rounds down", BottomClosed, eleven, "5 4 4 3 3 2 2 2 1 1 0"},
		// At 9/10, 7/10, ..., 1/10: each halfway between two stars.
		{"all halves round up", TopClosed, []float64{5, 4, 3, 2, 1}, "5 4 3 2 1"},
		{"all halves round down", BottomClosed, []float64{5, 4, 3, 2, 1}, "4 3 2 1 0"},
	}
	for _, tt := range tests {
		stars := mustStars(t, StarOptions{Max: 5}, tt.c)
		if got := placeNames(t, tt.ratings, stars, Options{}); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestPlaceHalfStarsLikeDoubledWholeStars(t *testing.T) {
	half := mustStars(t, StarOptions{Max: 5, Divisions: 2}, TopClosed)
	whole := mustStars(t, StarOptions{Max: 10}, TopClosed)
	for n := 2; n <= 40; n++ {
		ratings := make([]float64, n)
		for i := range ratings {
			ratings[i] = float64(n - i)
		}
		a, errA := Place(ratings, half, Options{})
		b, errB := Place(ratings, whole, Options{})
		if errA != nil || errB != nil || !slices.Equal(a, b) {
			t.Errorf("%d entries: half-stars give %v (%v), whole stars %v (%v)", n, a, errA, b, errB)
		}
	}
}

// By rating, each entry stands where its rating lies between the worst
// rating and the best, so the tiers split the range of ratings rather than
// the entries.
func TestPlaceByRating(t *testing.T) {
	three := []string{"top", "middle", "bottom"}
	thirds := mustNew(t, "thirds", three, rats("1/3", "2/3"), TopClosed)
	halves := mustNew(t, "halves", []string{"top", "bottom"}, rats("1/2"), TopClosed)
	halvesDown := mustNew(t, "halves", []string{"top", "bottom"}, rats("1/2"), BottomClosed)
	thirdsDown := mustNew(t, "thirds", three, rats("1/3", "2/3"), BottomClosed)
	byRating := Options{Positions: ByRating, Tolerance: 1e-6}
	with := func(o Options, f func(*Options)) Options { f(&o); return o }

	tests := []struct {
		name    string
		tmpl    Template
		ratings []float64
		opts    Options
		want    string
	}{
		// The average gap is 225, so the range, widened by half of it at
		// each end, runs from -12.5 to 1112.5, and these stand at 0.9,
		// 0.81, 0.19, 0.14 and 0.1: nothing in the middle third, though by
		// rank the middle entry would be there.
		{"a gap in the ratings", thirds, []float64{1000, 900, 200, 150, 100}, byRating,
			"top top bottom bottom bottom"},
		{"a gap in the ratings, by rank", thirds, []float64{1000, 900, 200, 150, 100}, Options{},
			"top top middle bottom bottom"},
		// One far ahead squeezes the rest to the bottom.
		{"an outlier", thirds, []float64{2000, 300, 200, 100}, byRating,
			"top bottom bottom bottom"},
		// Exactly on the cut-off, the convention decides: the widened range
		// runs from 50 to 350, so 200 is at 1/2.
		{"on a cut-off, top closed", halves, []float64{300, 200, 100}, byRating,
			"top top bottom"},
		{"on a cut-off, bottom closed", halvesDown, []float64{300, 200, 100}, byRating,
			"top bottom bottom"},
		// So it does within the tolerance of it, either way...
		{"just below a cut-off", halves, []float64{300, 200 - 1e-9, 100}, byRating,
			"top top bottom"},
		{"just above a cut-off", halvesDown, []float64{300, 200 + 1e-9, 100}, byRating,
			"top bottom bottom"},
		// ...but not beyond it.
		{"below a cut-off", halves, []float64{300, 200 - 1e-3, 100}, byRating,
			"top bottom bottom"},
		{"just below a cut-off, no tolerance", halves, []float64{300, 200 - 1e-9, 100}, with(byRating, func(o *Options) { o.Tolerance = 0 }),
			"top bottom bottom"},
		// The widened range runs from -1 to 5, so 1 is on the cut-off at
		// 1/3, though 1/3 can't be written exactly as a float.
		{"on a cut-off of 1/3, no tolerance", thirds, []float64{4, 1, 0}, with(byRating, func(o *Options) { o.Tolerance = 0 }),
			"top middle bottom"},
		// Ratings all the same, or all within the tolerance, stand at 1/2.
		{"all the same", thirds, []float64{5, 5, 5, 5}, byRating,
			"middle middle middle middle"},
		{"all the same, on a cut-off", halves, []float64{5, 5}, byRating,
			"top top"},
		{"all the same, on a cut-off, bottom closed", halvesDown, []float64{5, 5}, byRating,
			"bottom bottom"},
		{"all within the tolerance", thirds, []float64{5 + 1e-9, 5, 5 - 1e-9}, byRating,
			"middle middle middle"},
		{"all within the tolerance, bottom closed", thirdsDown, []float64{5 + 1e-9, 5, 5 - 1e-9}, byRating,
			"middle middle middle"},
		// Groups work as by rank: the widened range runs from -150 to 1050,
		// so these stand at 0.875, 0.675, 0.658 and 0.125, and the 2nd and
		// 3rd would go top and middle alone, but go together.
		{"a group split by a cut-off, higher", thirds, []float64{900, 660, 640, 0}, with(byRating, func(o *Options) { o.DrawMargin = 30 }),
			"top top top bottom"},
		{"a group split by a cut-off, lower", thirds, []float64{900, 660, 640, 0}, with(byRating, func(o *Options) { o.DrawMargin = 30; o.Prefer = Lower }),
			"top middle middle bottom"},
		{"a group, alternate rule", thirds, []float64{900, 610, 590, 300, 0}, with(byRating, func(o *Options) { o.DrawMargin = 300; o.Rule = Alternate; o.Prefer = Lower }),
			"bottom bottom bottom bottom bottom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := placeNames(t, tt.ratings, tt.tmpl, tt.opts); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

// Evenly spaced ratings stand where their ranks do, so by rating they go
// where they would by rank, cut-offs and all, whatever rounding does to
// the positions.
func TestPlaceByRatingEvenlySpaced(t *testing.T) {
	templates := []Template{
		mustStars(t, StarOptions{Max: 5}, TopClosed),
		mustStars(t, StarOptions{Max: 5}, BottomClosed),
		mustStars(t, StarOptions{Max: 10, Divisions: 2}, TopClosed),
		mustNew(t, "thirds", []string{"a", "b", "c"}, rats("1/3", "2/3"), TopClosed),
		mustNew(t, "sevenths", []string{"a", "b", "c"}, rats("1/7", "5/7"), BottomClosed),
	}
	for _, tmpl := range templates {
		for n := 2; n <= 43; n++ {
			for _, step := range []float64{1, 0.1, 37.3} {
				ratings := make([]float64, n)
				for i := range ratings {
					ratings[i] = 1500 + step*float64(n-1-i)
				}
				byRank, err1 := Place(ratings, tmpl, Options{})
				byRating, err2 := Place(ratings, tmpl, Options{Positions: ByRating, Tolerance: 1e-6})
				if err1 != nil || err2 != nil || !slices.Equal(byRank, byRating) {
					t.Errorf("%s, convention %d, %d entries %v apart: by rank %v (%v), by rating %v (%v)",
						tmpl.Name, tmpl.Convention, n, step, byRank, err1, byRating, err2)
				}
			}
		}
	}
}

// Each tier gets its share of the entries, to within one: a tier too small
// for an entry stays empty, even at the top or the bottom.
func TestPlaceSharesOfEntries(t *testing.T) {
	templates := []Template{
		mustStars(t, StarOptions{Max: 10}, TopClosed),
		mustStars(t, StarOptions{Max: 10, Sizes: Sizes{Kind: BetaTiers, Alpha: 2, Beta: 2}}, TopClosed),
		mustStars(t, StarOptions{Max: 5, Sizes: Sizes{Kind: GeometricTiers, Factor: 1.618}}, BottomClosed),
		mustStars(t, StarOptions{Max: 50, Sizes: Sizes{Kind: GeometricTiers, Factor: 1e10}}, TopClosed),
		mustNew(t, "thirds", []string{"a", "b", "c"}, rats("1/3", "2/3"), BottomClosed),
	}
	for _, tmpl := range templates {
		shares := tmpl.Shares() // worst first
		for n := 2; n <= 60; n++ {
			ratings := make([]float64, n)
			for i := range ratings {
				ratings[i] = float64(n - i)
			}
			for _, positions := range []Positions{ByRank, ByRating} {
				placed, err := Place(ratings, tmpl, Options{Positions: positions, Tolerance: 1e-9})
				if err != nil {
					t.Fatal(err)
				}
				counts := make([]int, len(tmpl.Tiers))
				for _, tier := range placed {
					counts[tier]++
				}
				for k, count := range counts {
					share := shares[len(shares)-1-k]
					if math.Abs(float64(count)-float64(n)*share) >= 1 {
						t.Errorf("%s, %d entries, positions %d: tier %s holds %d, for a share of %.4f", tmpl.Name, n, positions, tmpl.Tiers[k], count, share)
					}
				}
			}
		}
	}
	// So with Beta(2, 2) stars, 0 and 10 stars, each 2.3% of [0, 1], stay
	// empty until a list has 22 entries.
	beta := templates[1]
	for _, n := range []int{21, 22} {
		ratings := make([]float64, n)
		for i := range ratings {
			ratings[i] = float64(n - i)
		}
		placed, _ := Place(ratings, beta, Options{})
		top, bottom := slices.Contains(placed, 0), slices.Contains(placed, 10)
		if top != (n == 22) || bottom != (n == 22) {
			t.Errorf("%d entries on Beta(2, 2) stars: tiers %v", n, placed)
		}
	}
}

func TestPlaceTooFewEntries(t *testing.T) {
	stars := mustStars(t, StarOptions{Max: 5}, TopClosed)
	for _, ratings := range [][]float64{nil, {1500}} {
		if _, err := Place(ratings, stars, Options{}); !errors.Is(err, ErrTooFewEntries) {
			t.Errorf("Place(%v) error = %v, want ErrTooFewEntries", ratings, err)
		}
	}
}

func TestPlaceRejects(t *testing.T) {
	stars := mustStars(t, StarOptions{Max: 5}, TopClosed)
	tests := []struct {
		name    string
		ratings []float64
		tmpl    Template
		opts    Options
	}{
		{"unsorted ratings", []float64{1, 2}, stars, Options{}},
		{"NaN rating", []float64{2, math.NaN()}, stars, Options{}},
		{"infinite rating", []float64{math.Inf(1), 1}, stars, Options{}},
		{"negative draw-margin", []float64{2, 1}, stars, Options{DrawMargin: -1}},
		{"NaN draw-margin", []float64{2, 1}, stars, Options{DrawMargin: math.NaN()}},
		{"unknown rule", []float64{2, 1}, stars, Options{Rule: 2}},
		{"unknown direction", []float64{2, 1}, stars, Options{Prefer: 2}},
		{"malformed template", []float64{2, 1}, Template{}, Options{}},
		{"unknown positions", []float64{2, 1}, stars, Options{Positions: 2}},
		{"negative tolerance", []float64{2, 1}, stars, Options{Positions: ByRating, Tolerance: -1}},
		{"NaN tolerance", []float64{2, 1}, stars, Options{Positions: ByRating, Tolerance: math.NaN()}},
	}
	for _, tt := range tests {
		if placed, err := Place(tt.ratings, tt.tmpl, tt.opts); err == nil {
			t.Errorf("%s: Place = %v, want an error", tt.name, placed)
		}
	}
}
