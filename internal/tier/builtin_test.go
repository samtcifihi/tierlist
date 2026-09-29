package tier

import (
	"math"
	"math/big"
	"slices"
	"testing"
)

func TestStars(t *testing.T) {
	tests := []struct {
		opts  StarOptions
		title string
		tiers []string
	}{
		{StarOptions{Max: 5}, "0–5 stars", []string{"5", "4", "3", "2", "1", "0"}},
		{StarOptions{Max: 3, SkipZero: true}, "1–3 stars", []string{"3", "2", "1"}},
		{StarOptions{Max: 3, Divisions: 1}, "0–3 stars", []string{"3", "2", "1", "0"}},
		{StarOptions{Max: 5, SkipZero: true, Divisions: 2}, "1–5 stars in steps of 1/2",
			[]string{"5", "4½", "4", "3½", "3", "2½", "2", "1½", "1"}},
		{StarOptions{Max: 3, Divisions: 3}, "0–3 stars in steps of 1/3",
			[]string{"3", "2⅔", "2⅓", "2", "1⅔", "1⅓", "1", "⅔", "⅓", "0"}},
		{StarOptions{Max: 3, SkipZero: true, Divisions: 4}, "1–3 stars in steps of 1/4",
			[]string{"3", "2¾", "2½", "2¼", "2", "1¾", "1½", "1¼", "1"}},
		// Sevenths have no single-character forms, so all are written out.
		{StarOptions{Max: 3, SkipZero: true, Divisions: 7}, "1–3 stars in steps of 1/7",
			[]string{"3", "2 6/7", "2 5/7", "2 4/7", "2 3/7", "2 2/7", "2 1/7", "2",
				"1 6/7", "1 5/7", "1 4/7", "1 3/7", "1 2/7", "1 1/7", "1"}},
	}
	for _, tt := range tests {
		tmpl := mustStars(t, tt.opts, TopClosed)
		if tmpl.Name != tt.title {
			t.Errorf("Stars(%+v) is named %q, want %q", tt.opts, tmpl.Name, tt.title)
		}
		if !slices.Equal(tmpl.Tiers, tt.tiers) {
			t.Errorf("Stars(%+v) tiers = %q, want %q", tt.opts, tmpl.Tiers, tt.tiers)
		}
		// The top and bottom tiers cover 1/(2*(n-1)) and the others 1/(n-1).
		n := len(tmpl.Tiers)
		bounds := append(append([]*big.Rat{big.NewRat(0, 1)}, tmpl.Cutoffs...), big.NewRat(1, 1))
		for k := range n {
			width := new(big.Rat).Sub(bounds[k+1], bounds[k])
			want := big.NewRat(1, int64(n-1))
			if k == 0 || k == n-1 {
				want = big.NewRat(1, int64(2*(n-1)))
			}
			if width.Cmp(want) != 0 {
				t.Errorf("Stars(%+v): tier %d from the bottom covers %s, want %s", tt.opts, k, width.RatString(), want.RatString())
			}
		}
	}
}

// widths returns the sizes of a template's tiers, bottom first, each
// worked out exactly before it is rounded.
func widths(tmpl Template) []float64 {
	bounds := append(append([]*big.Rat{big.NewRat(0, 1)}, tmpl.Cutoffs...), big.NewRat(1, 1))
	w := make([]float64, len(bounds)-1)
	for k := range w {
		w[k] = ratFloat(new(big.Rat).Sub(bounds[k+1], bounds[k]))
	}
	return w
}

func TestGeometricTiers(t *testing.T) {
	// 0–5 stars is 6 tiers. With a factor of 2 from the best tier, they are
	// 1, 2, 4, 8, 16 and 32 parts of 63, best first.
	tmpl := mustStars(t, StarOptions{Max: 5, Sizes: Sizes{Kind: GeometricTiers, Factor: 2}}, TopClosed)
	want := []float64{32, 16, 8, 4, 2, 1} // bottom first
	for k, w := range widths(tmpl) {
		if math.Abs(w-want[k]/63) > 1e-15 {
			t.Errorf("tier %d from the bottom covers %g, want %g", k, w, want[k]/63)
		}
	}
	// From the worst tier the other way round, and a factor below 1 runs
	// the other way too: 1/2 from the best is 2 from the worst.
	a := mustStars(t, StarOptions{Max: 5, Sizes: Sizes{Kind: GeometricTiers, Factor: 2, FromWorst: true}}, TopClosed)
	b := mustStars(t, StarOptions{Max: 5, Sizes: Sizes{Kind: GeometricTiers, Factor: 0.5}}, TopClosed)
	for k := range a.Cutoffs {
		if math.Abs(ratFloat(a.Cutoffs[k])-ratFloat(b.Cutoffs[k])) > 1e-15 || math.Abs(widths(a)[k]-want[5-k]/63) > 1e-15 {
			t.Errorf("cut-off %d: %v from the worst, %v with factor 1/2", k, a.Cutoffs[k].FloatString(6), b.Cutoffs[k].FloatString(6))
		}
	}
	// Any number of tiers and any factor still covers [0, 1], with the same
	// factor between neighbours; a factor of 1 makes every tier the same.
	for _, factor := range []float64{1.618, 1, 0.8, 3} {
		for _, stars := range []int{3, 10, 40} {
			w := widths(mustStars(t, StarOptions{Max: stars, Divisions: 2, Sizes: Sizes{Kind: GeometricTiers, Factor: factor}}, BottomClosed))
			for k := 1; k < len(w); k++ {
				if r := w[k-1] / w[k]; math.Abs(r-factor) > 1e-9*factor {
					t.Fatalf("factor %g, %d stars: tiers %d and %d from the bottom differ by %g", factor, stars, k-1, k, r)
				}
			}
		}
	}
	for _, bad := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := Stars(StarOptions{Max: 5, Sizes: Sizes{Kind: GeometricTiers, Factor: bad}}, TopClosed); err == nil {
			t.Errorf("factor %g: want an error", bad)
		}
	}
	// However steep, the tiers still cover [0, 1] in order (New checks
	// that); ones too small for a float64 become tiny but keep their place.
	// With a factor of 1e10 from the best, the best entry is alone at the
	// top and the rest fall to the bottom tier, which is nearly all of it.
	steep := mustStars(t, StarOptions{Max: 50, Sizes: Sizes{Kind: GeometricTiers, Factor: 1e10}}, TopClosed)
	placed, err := Place([]float64{5, 4, 3, 2, 1}, steep, Options{})
	if err != nil || !slices.Equal(placed, []int{0, 50, 50, 50, 50}) {
		t.Errorf("five entries on a factor of 1e10: tiers %v, %v", placed, err)
	}
}

func TestBetaTiers(t *testing.T) {
	even := mustStars(t, StarOptions{Max: 5}, TopClosed)
	beta := func(a, b float64) Template {
		t.Helper()
		return mustStars(t, StarOptions{Max: 5, Sizes: Sizes{Kind: BetaTiers, Alpha: a, Beta: b}}, TopClosed)
	}
	// Beta(1, 1) is uniform, so it gives the six tiers of 0–5 stars the
	// same size exactly: cut-offs at 1/6, 2/6, ... 5/6.
	for k, c := range beta(1, 1).Cutoffs {
		if want := big.NewRat(int64(k+1), 6); c.Cmp(want) != 0 {
			t.Errorf("Beta(1, 1) cut-off %d is %s, want %s", k, c.RatString(), want.RatString())
		}
	}
	// Each cut-off is the distribution's CDF at the equal one: for Beta(2, 2)
	// that is 3x² - 2x³, so 2/27 at 1/6. Symmetric parameters give
	// cut-offs exactly symmetric about 1/2, and middle tiers bigger than
	// equal ones, where Beta(½, ½) makes the end tiers bigger instead.
	b22, half := beta(2, 2), beta(0.5, 0.5)
	if c := ratFloat(b22.Cutoffs[0]); math.Abs(c-2.0/27) > 1e-15 {
		t.Errorf("Beta(2, 2) first cut-off %g, want 2/27", c)
	}
	for _, tmpl := range []Template{b22, half} {
		n := len(tmpl.Cutoffs)
		for k, c := range tmpl.Cutoffs {
			if sum := new(big.Rat).Add(c, tmpl.Cutoffs[n-1-k]); sum.Cmp(big.NewRat(1, 1)) != 0 {
				t.Errorf("cut-offs %d and %d add up to %s, not 1", k, n-1-k, sum.RatString())
			}
		}
	}
	equal := 1.0 / 6
	w22, wh := widths(b22), widths(half)
	if !(w22[0] < equal && w22[2] > equal && wh[0] > equal && wh[2] < equal) {
		t.Errorf("tier sizes, bottom first: Beta(2, 2) %v, Beta(½, ½) %v", w22, wh)
	}
	// A larger α makes the tiers near the top bigger, a larger β those near
	// the bottom.
	if top := widths(beta(5, 2)); !(top[5] > equal && top[0] < equal) {
		t.Errorf("Beta(5, 2) tier sizes, bottom first: %v", top)
	}
	// Unlike the nearest-star sizes, the end tiers aren't halved.
	if w := widths(beta(1, 1)); w[0] == widths(even)[0] {
		t.Errorf("Beta(1, 1) bottom tier %g, the same as the nearest-star one", w[0])
	}
	for _, bad := range [][2]float64{{0, 1}, {1, -2}, {math.NaN(), 1}, {1, 2e6}} {
		if _, err := Stars(StarOptions{Max: 5, Sizes: Sizes{Kind: BetaTiers, Alpha: bad[0], Beta: bad[1]}}, TopClosed); err == nil {
			t.Errorf("Beta(%g, %g): want an error", bad[0], bad[1])
		}
	}
	// Even very peaked distributions give a template, with the outer tiers
	// tiny but in order, and still symmetric.
	peaked := mustStars(t, StarOptions{Max: 10, Sizes: Sizes{Kind: BetaTiers, Alpha: 2000, Beta: 2000}}, TopClosed)
	for k, c := range peaked.Cutoffs {
		if sum := new(big.Rat).Add(c, peaked.Cutoffs[len(peaked.Cutoffs)-1-k]); sum.Cmp(big.NewRat(1, 1)) != 0 {
			t.Errorf("Beta(2000, 2000): cut-offs %d from each end add up to %s", k, sum.FloatString(20))
		}
	}
}

// Reference values from scipy.special.betainc.
func TestBetaCDF(t *testing.T) {
	for _, c := range [][4]float64{
		{0.05, 2, 2, 0.007250000000000001},
		{0.35, 2, 2, 0.28174999999999994},
		{0.5, 0.5, 0.5, 0.5000000000000001},
		{0.05, 0.5, 0.5, 0.14356629312870628},
		{0.95, 0.5, 0.5, 0.8564337068712936},
		{0.25, 2, 5, 0.466064453125},
		{0.75, 2, 5, 0.995361328125},
		{0.15, 0.01, 0.3, 0.9544750168396194},
		{0.45, 50, 50, 0.1586521989370985},
		{0.3, 200, 300, 1.049698524329301e-06},
		{0.999, 3, 0.2, 0.6685414839010846},
		{0.001, 0.2, 3, 0.33145851609891525},
		{0.6, 1, 1, 0.6},
		{0.1, 1, 3, 0.271},
		{0.9, 7.5, 1.25, 0.5598232691710877},
	} {
		if got := betaCDF(c[0], c[1], c[2]); math.Abs(got-c[3]) > 1e-12*c[3] {
			t.Errorf("betaCDF(%g, %g, %g) = %.17g, want %.17g", c[0], c[1], c[2], got, c[3])
		}
	}
}

// Reference values from scipy.stats.beta.pdf.
func TestBetaPDF(t *testing.T) {
	for _, c := range []struct{ x, a, b, want, tol float64 }{
		{0.3, 2, 2, 1.26, 1e-14},
		{0.5, 2, 2, 1.5, 1e-14},
		{0.25, 0.5, 0.5, 0.7351051938957226, 1e-14},
		{0.001, 0.5, 0.5, 10.070879119947092, 1e-14},
		{0.8, 5, 2, 2.4576, 1e-14},
		{0.5, 2000, 2000, 50.45949662334133, 1e-11},
		{0.51, 2000, 2000, 22.678356346730215, 1e-11},
		{0.02, 2, 50, 18.951687433105086, 1e-13},
		{0.5, 1e6, 1e6, 1128.379026048125, 1e-8},
		{0.5005, 1e6, 1e6, 415.1076530860914, 1e-8},
		{0.9, 7.5, 1.25, 3.9612879580556313, 1e-13},
		{0.1, 1, 3, 2.43, 1e-14},
		{1e-9, 0.1, 0.1, 6385738.951130342, 1e-13},
		{0.6, 1, 1, 1, 1e-15},
		{0.999, 3, 0.2, 66.18124050207096, 1e-13},
		{0, 2, 2, 0, 0},
		{1, 0.5, 0.5, 0, 0},
		{-0.5, 1, 1, 0, 0},
	} {
		if got := BetaPDF(c.x, c.a, c.b); math.Abs(got-c.want) > c.tol*c.want {
			t.Errorf("BetaPDF(%g, %g, %g) = %.17g, want %.17g", c.x, c.a, c.b, got, c.want)
		}
	}
	// Each Beta tier's share is the density's area over its equal part. (The
	// midpoint sums need densities without sharp ends to match closely.)
	for _, ab := range [][2]float64{{2, 5}, {3, 3}, {1, 4}, {7.5, 2.5}} {
		tmpl := mustStars(t, StarOptions{Max: 5, Sizes: Sizes{Kind: BetaTiers, Alpha: ab[0], Beta: ab[1]}}, TopClosed)
		const steps = 20000
		for k, share := range tmpl.Shares() {
			area := 0.0
			for i := range steps {
				area += BetaPDF((float64(k)+(float64(i)+0.5)/steps)/6, ab[0], ab[1]) / (6 * steps)
			}
			if math.Abs(area-share) > 1e-8 {
				t.Errorf("Beta(%g, %g), tier %d from the bottom: area %.12f, share %.12f", ab[0], ab[1], k, area, share)
			}
		}
	}
}

func TestStarsRejects(t *testing.T) {
	bad := []StarOptions{
		{Max: 2},
		{Max: 0},
		{Max: -3},
		{Max: 5, Divisions: -1},
		{Max: 1000},               // 1001 tiers
		{Max: 100, Divisions: 11}, // 1101 tiers
		{Max: 5, Divisions: math.MaxInt},
		{Max: math.MaxInt},
	}
	for _, o := range bad {
		if tmpl, err := Stars(o, TopClosed); err == nil {
			t.Errorf("Stars(%+v) made %d tiers, want an error", o, len(tmpl.Tiers))
		}
	}
	if _, err := Stars(StarOptions{Max: 999}, TopClosed); err != nil {
		t.Errorf("Stars with exactly 1000 tiers: %v", err)
	}
	if _, err := Stars(StarOptions{Max: 5}, Convention(7)); err == nil {
		t.Error("Stars with an unknown convention: want an error")
	}
}

func TestStarNamer(t *testing.T) {
	tests := []struct {
		d, units int
		want     string
	}{
		{1, 4, "4"},
		{2, 1, "½"},
		{6, 3, "½"},
		{6, 4, "⅔"},
		{8, 6, "¾"},
		{8, 15, "1⅞"},
		{10, 1, "1/10"}, // 3/10 has no glyph, so tenths are written out
		{10, 5, "1/2"},
		{10, 25, "2 1/2"},
	}
	for _, tt := range tests {
		if got := starNamer(tt.d)(tt.units); got != tt.want {
			t.Errorf("starNamer(%d)(%d) = %q, want %q", tt.d, tt.units, got, tt.want)
		}
	}
}

func TestHalfStarsEqualDoubledWholeStars(t *testing.T) {
	half := mustStars(t, StarOptions{Max: 5, Divisions: 2}, TopClosed)
	whole := mustStars(t, StarOptions{Max: 10}, TopClosed)
	if len(half.Tiers) != 11 || len(whole.Tiers) != 11 {
		t.Fatalf("got %d and %d tiers, want 11 each", len(half.Tiers), len(whole.Tiers))
	}
	for i := range half.Cutoffs {
		if half.Cutoffs[i].Cmp(whole.Cutoffs[i]) != 0 {
			t.Errorf("cut-off %d: %s with half-stars, %s with whole stars",
				i, half.Cutoffs[i].RatString(), whole.Cutoffs[i].RatString())
		}
	}
}

func TestOWLNEWT(t *testing.T) {
	h := mustOWLNEWT(t, TopClosed)
	wantTiers := []string{"Outstanding", "Exceeds Expectations", "Acceptable", "Poor", "Dreadful", "Troll"}
	if !slices.Equal(h.Tiers, wantTiers) {
		t.Errorf("tiers = %q, want %q", h.Tiers, wantTiers)
	}
	wantCutoffs := rats("16/31", "21/31", "25/31", "28/31", "30/31")
	if !slices.EqualFunc(h.Cutoffs, wantCutoffs, func(a, b *big.Rat) bool { return a.Cmp(b) == 0 }) {
		t.Errorf("cut-offs = %v, want %v", h.Cutoffs, wantCutoffs)
	}
	if h.Name != "OWL/NEWT" {
		t.Errorf("name %q, want OWL/NEWT", h.Name)
	}
	if _, err := OWLNEWT(Convention(-1)); err == nil {
		t.Error("OWL/NEWT with an unknown convention: want an error")
	}
}
