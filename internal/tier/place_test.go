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
	// Seven entries sit at 1, 5/6, 2/3, 1/2, 1/3, 1/6 and 0, so alone they
	// would go top, top, top, middle, middle, bottom, bottom (or, with the
	// bottom tier closed, top, top, middle, middle, bottom, bottom, bottom).
	three := []string{"top", "middle", "bottom"}
	topClosed := mustNew(t, "thirds", three, rats("1/3", "2/3"), TopClosed)
	bottomClosed := mustNew(t, "thirds", three, rats("1/3", "2/3"), BottomClosed)
	spaced := []float64{700, 600, 500, 400, 300, 200, 100}
	pair := []float64{700, 600, 501, 500, 300, 200, 100}      // groups the 3rd and 4th
	triple := []float64{700, 600, 502, 500, 498, 200, 100}    // groups the 3rd to 5th
	quadruple := []float64{703, 702, 701, 700, 300, 200, 100} // groups the 1st to 4th

	tests := []struct {
		name    string
		tmpl    Template
		ratings []float64
		opts    Options
		want    string
	}{
		{"no groups", topClosed, spaced, Options{DrawMargin: 50},
			"top top top middle middle bottom bottom"},
		{"no groups, bottom closed", bottomClosed, spaced, Options{DrawMargin: 50},
			"top top middle middle bottom bottom bottom"},
		{"odd group: middle entry decides", topClosed, triple, Options{DrawMargin: 5},
			"top top middle middle middle bottom bottom"},
		{"even group, middles split, higher", topClosed, pair, Options{DrawMargin: 5},
			"top top top top middle bottom bottom"},
		{"even group, middles split, lower", topClosed, pair, Options{DrawMargin: 5, Prefer: Lower},
			"top top middle middle middle bottom bottom"},
		{"even group, middles agree", topClosed, quadruple, Options{DrawMargin: 5, Prefer: Lower},
			"top top top top middle bottom bottom"},
		{"alternate rule, highest", topClosed, quadruple, Options{DrawMargin: 5, Rule: Alternate},
			"top top top top middle bottom bottom"},
		{"alternate rule, lowest", topClosed, quadruple, Options{DrawMargin: 5, Rule: Alternate, Prefer: Lower},
			"middle middle middle middle middle bottom bottom"},
		// The README's example: with a draw-margin of 1, ratings 3, 2 and
		// 1 chain into one group even though 3 and 1 differ by 2.
		{"groups chain", topClosed, []float64{3, 2, 1}, Options{DrawMargin: 1},
			"middle middle middle"},
		{"chain broken", topClosed, []float64{4, 2, 1}, Options{DrawMargin: 1},
			"top middle middle"},
		{"equal ratings group at margin 0", topClosed, []float64{5, 5, 1}, Options{},
			"top top bottom"},
		{"one big group", topClosed, spaced, Options{DrawMargin: 1000},
			"middle middle middle middle middle middle middle"},
		{"two entries", topClosed, []float64{2, 1}, Options{},
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
	eleven := []float64{11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1} // at 1, 9/10, ..., 0
	tests := []struct {
		name    string
		c       Convention
		ratings []float64
		want    string
	}{
		{"one entry per star", TopClosed, []float64{6, 5, 4, 3, 2, 1}, "5 4 3 2 1 0"},
		{"halves round up", TopClosed, eleven, "5 5 4 4 3 3 2 2 1 1 0"},
		{"halves round down", BottomClosed, eleven, "5 4 4 3 3 2 2 1 1 0 0"},
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
		// At 1, 8/9, 1/9, 1/18 and 0: nothing in the middle third of the
		// range, though by rank the middle entry would be there.
		{"a gap in the ratings", thirds, []float64{1000, 900, 200, 150, 100}, byRating,
			"top top bottom bottom bottom"},
		{"a gap in the ratings, by rank", thirds, []float64{1000, 900, 200, 150, 100}, Options{},
			"top top middle bottom bottom"},
		// One far ahead squeezes the rest to the bottom.
		{"an outlier", thirds, []float64{2000, 300, 200, 100}, byRating,
			"top bottom bottom bottom"},
		// Exactly on the cut-off, the convention decides.
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
		// A third of the way up is on the cut-off at 1/3, though 1/3 can't
		// be written exactly as a float.
		{"on a cut-off of 1/3, no tolerance", thirds, []float64{3, 1, 0}, with(byRating, func(o *Options) { o.Tolerance = 0 }),
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
		// Groups work as by rank: at 1, 0.678, 0.656 and 0, the 2nd and
		// 3rd would go top and middle alone, and go together.
		{"a group split by a cut-off, higher", thirds, []float64{900, 610, 590, 0}, with(byRating, func(o *Options) { o.DrawMargin = 30 }),
			"top top top bottom"},
		{"a group split by a cut-off, lower", thirds, []float64{900, 610, 590, 0}, with(byRating, func(o *Options) { o.DrawMargin = 30; o.Prefer = Lower }),
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
