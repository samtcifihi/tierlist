package bayeselo

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// priorDrawElo is the draw setting given by its prior alone.
var priorDrawElo = 400 * math.Log10(2)

func fit(t *testing.T, n int, cs []Comparison) *Result {
	t.Helper()
	res, err := Fit(n, cs, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// simulate returns count comparisons between random pairs of entries with
// the given true ratings, answered by the model with draw setting drawElo.
func simulate(rng *rand.Rand, truth []float64, drawElo float64, count int) []Comparison {
	n := len(truth)
	cs := make([]Comparison, count)
	for k := range cs {
		a, b := rng.IntN(n), rng.IntN(n-1)
		if b >= a {
			b++
		}
		better, same, _ := Probabilities(truth[a]-truth[b], drawElo)
		o := BWins
		switch u := rng.Float64(); {
		case u < better:
			o = AWins
		case u < better+same:
			o = Draw
		}
		cs[k] = Comparison{A: a, B: b, Outcome: o}
	}
	return cs
}

func TestNoComparisons(t *testing.T) {
	res := fit(t, 3, nil)
	// Only the priors: every rating at the dummy's 0, the draw setting at
	// its prior's value, and an uncertainty of sqrt(2) natural units.
	wantSD := math.Sqrt2 / eloToNat
	for i, r := range res.Ratings {
		if r != 0 || !near(res.SD(i), wantSD, 1e-9) {
			t.Errorf("entry %d: rating %g ± %g, want 0 ± %g", i, r, res.SD(i), wantSD)
		}
	}
	if res.Cov(0, 1) != 0 {
		t.Errorf("uncompared entries have covariance %g, want 0", res.Cov(0, 1))
	}
	if !near(res.DrawElo, priorDrawElo, 1e-9) {
		t.Errorf("draw setting %g, want %g", res.DrawElo, priorDrawElo)
	}

	if res := fit(t, 0, nil); len(res.Ratings) != 0 || !near(res.DrawElo, priorDrawElo, 1e-9) {
		t.Errorf("no entries: got %v and draw setting %g", res.Ratings, res.DrawElo)
	}
}

func TestOneComparison(t *testing.T) {
	// A win puts the entries symmetrically either side of the dummy.
	r := fit(t, 2, []Comparison{{0, 1, AWins}}).Ratings
	if !(r[0] > 0) || !near(r[0], -r[1], 1e-9) {
		t.Errorf("after a win, ratings are %v, want x and -x for some x > 0", r)
	}
	// A draw leaves both at 0 and raises the draw setting.
	res := fit(t, 2, []Comparison{{0, 1, Draw}})
	if math.Abs(res.Ratings[0]) > 1e-9 || math.Abs(res.Ratings[1]) > 1e-9 || !(res.DrawElo > priorDrawElo) {
		t.Errorf("after a draw, ratings %v and draw setting %g; want 0, 0 and more than %g",
			res.Ratings, res.DrawElo, priorDrawElo)
	}
}

func TestOrderAndSidesDoNotMatter(t *testing.T) {
	a := fit(t, 3, []Comparison{{0, 1, AWins}, {1, 2, Draw}, {2, 0, BWins}, {0, 1, Draw}})
	b := fit(t, 3, []Comparison{{0, 1, Draw}, {0, 2, AWins}, {2, 1, Draw}, {1, 0, BWins}})
	if !slices.Equal(a.Ratings, b.Ratings) || a.DrawElo != b.DrawElo {
		t.Errorf("%v, %g differs from %v, %g", a.Ratings, a.DrawElo, b.Ratings, b.DrawElo)
	}
}

func TestChainOfWins(t *testing.T) {
	r := fit(t, 4, []Comparison{{0, 1, AWins}, {1, 2, AWins}, {2, 3, AWins}}).Ratings
	if !(r[0] > r[1] && r[1] > r[2] && r[2] > r[3]) {
		t.Errorf("ratings %v, want strictly decreasing", r)
	}
}

func TestMoreWinsWiderGap(t *testing.T) {
	prev := 0.0
	for _, k := range []int{1, 2, 5, 20} {
		cs := make([]Comparison, k)
		for i := range cs {
			cs[i] = Comparison{0, 1, AWins}
		}
		r := fit(t, 2, cs).Ratings
		if gap := r[0] - r[1]; !(gap > prev) {
			t.Errorf("%d wins give a gap of %g, not more than %g", k, gap, prev)
		} else {
			prev = gap
		}
	}
}

func TestUncertaintyShrinksWithComparisons(t *testing.T) {
	var cs []Comparison
	for range 5 {
		cs = append(cs, Comparison{0, 1, AWins}, Comparison{1, 0, AWins})
	}
	res := fit(t, 3, cs)
	if !(res.SD(0) < res.SD(2)) || !near(res.SD(2), math.Sqrt2/eloToNat, 1e-9) {
		t.Errorf("SDs %g (compared 10 times) and %g (never), want the first smaller and the second %g",
			res.SD(0), res.SD(2), math.Sqrt2/eloToNat)
	}
	// Entries compared with each other a lot know their difference well.
	if !(res.DiffVar(0, 1) < res.DiffVar(0, 2)) {
		t.Errorf("DiffVar(0, 1) = %g, not below DiffVar(0, 2) = %g", res.DiffVar(0, 1), res.DiffVar(0, 2))
	}
}

// With plenty of answers from known ratings and a known draw setting, the
// fit should recover both.
func TestRecoversSimulatedRatings(t *testing.T) {
	truth := []float64{-320, -210, -140, -60, -10, 30, 80, 150, 210, 270} // mean 0
	const drawElo = 150
	cs := simulate(rand.New(rand.NewPCG(1, 2)), truth, drawElo, 20000)
	res := fit(t, len(truth), cs)
	if math.Abs(res.DrawElo-drawElo) > 10 {
		t.Errorf("draw setting %g, want about %d", res.DrawElo, drawElo)
	}
	mean := 0.0
	for _, r := range res.Ratings {
		mean += r / float64(len(truth))
	}
	for i, r := range res.Ratings {
		if err := r - mean - truth[i]; math.Abs(err) > 25 || math.Abs(err) > 4*res.SD(i) {
			t.Errorf("entry %d: rating %.1f ± %.1f (centered), want about %g", i, r-mean, res.SD(i), truth[i])
		}
	}
}

func TestDrawSettingExtremes(t *testing.T) {
	// Never "about the same": the draw setting drops close to 0 but stays
	// positive.
	var cs []Comparison
	for range 50 {
		cs = append(cs, Comparison{0, 1, AWins}, Comparison{0, 1, BWins})
	}
	if d := fit(t, 2, cs).DrawElo; !(d > 0 && d < 20) {
		t.Errorf("with no draws, the draw setting is %g, want between 0 and 20", d)
	}
	// Always "about the same": it grows large but stays finite.
	for i := range cs {
		cs[i].Outcome = Draw
	}
	if d := fit(t, 2, cs).DrawElo; !(d > 500 && !math.IsInf(d, 0)) {
		t.Errorf("with only draws, the draw setting is %g, want large but finite", d)
	}
}

func TestWarmStart(t *testing.T) {
	truth := []float64{-200, -50, 0, 60, 190}
	cs := simulate(rand.New(rand.NewPCG(7, 8)), truth, 80, 300)
	cold := fit(t, len(truth), cs)
	warm, err := Fit(len(truth), cs, cold)
	if err != nil {
		t.Fatal(err)
	}
	for i := range truth {
		if !near(warm.Ratings[i], cold.Ratings[i], 1e-9) || !near(warm.SD(i), cold.SD(i), 1e-6) {
			t.Errorf("entry %d: warm start gives %g ± %g, cold %g ± %g",
				i, warm.Ratings[i], warm.SD(i), cold.Ratings[i], cold.SD(i))
		}
	}
	// Starts that do not fit are ignored.
	for _, start := range []*Result{
		{Ratings: []float64{1, 2}, DrawElo: 50},
		{Ratings: []float64{0, math.NaN(), 0, 0, 0}, DrawElo: 50},
		{Ratings: make([]float64, 5), DrawElo: -1},
	} {
		res, err := Fit(len(truth), cs, start)
		if err != nil || !near(res.Ratings[0], cold.Ratings[0], 1e-9) {
			t.Errorf("start %v: got %v, %v", start, res, err)
		}
	}
}

func TestProbabilities(t *testing.T) {
	for _, diff := range []float64{-800, -100, 0, 50, 1000} {
		for _, draw := range []float64{0, 30, 120, 400} {
			b, s, w := Probabilities(diff, draw)
			if b < 0 || s < 0 || w < 0 || !near(b+s+w, 1, 1e-12) {
				t.Errorf("Probabilities(%g, %g) = %g, %g, %g", diff, draw, b, s, w)
			}
		}
	}
	// With the prior's draw setting, equally rated entries give each
	// answer a third of the time.
	if b, s, w := Probabilities(0, priorDrawElo); !near(b, 1.0/3, 1e-12) || !near(s, 1.0/3, 1e-12) || !near(w, 1.0/3, 1e-12) {
		t.Errorf("equal ratings, prior draw setting: %g, %g, %g", b, s, w)
	}
	// Without a draw setting it is the plain Elo curve: 400 points is 10:1.
	if b, s, _ := Probabilities(400, 0); !near(b, 10.0/11, 1e-12) || s != 0 {
		t.Errorf("Probabilities(400, 0) = %g better, %g same; want 10/11, 0", b, s)
	}
}

func TestExpectedScore(t *testing.T) {
	for _, draw := range []float64{0, 30, 120, 400} {
		for _, diff := range []float64{-600, -120, 0, 45, 300} {
			better, same, _ := Probabilities(diff, draw)
			got := ExpectedScore(diff, draw)
			if !near(got, better+same/2, 1e-12) {
				t.Errorf("ExpectedScore(%g, %g) = %g, want %g", diff, draw, got, better+same/2)
			}
			if !near(got+ExpectedScore(-diff, draw), 1, 1e-12) {
				t.Errorf("ExpectedScore(±%g, %g) do not add up to 1", diff, draw)
			}
		}
	}
}

func TestScoreGap(t *testing.T) {
	// Without draws, it is the plain Elo curve.
	for _, odds := range []float64{2, 3, 10} {
		if got, want := ScoreGap(odds, 0), 400*math.Log10(odds); !near(got, want, 1e-12) {
			t.Errorf("ScoreGap(%g, 0) = %g, want %g", odds, got, want)
		}
	}
	// With the prior's draw setting, cosh θ is 5/4, and 2:1 takes
	// 400·log10((5 + √153)/8), about 134.68 Elo.
	if got, want := ScoreGap(2, priorDrawElo), 400*math.Log10((5+math.Sqrt(153))/8); !near(got, want, 1e-12) {
		t.Errorf("ScoreGap(2, prior) = %g, want %g", got, want)
	}
	for _, draw := range []float64{0, 50, priorDrawElo, 300, 1000} {
		prev := math.Inf(-1)
		for _, odds := range []float64{0.001, 0.5, 1, 2, 3, 10, 1000} {
			gap := ScoreGap(odds, draw)
			if e := ExpectedScore(gap, draw); !near(e/(1-e), odds, 1e-9) {
				t.Errorf("at ScoreGap(%g, %g) = %g the expected score is %g:1", odds, draw, gap, e/(1-e))
			}
			if back := ScoreGap(1/odds, draw); !near(back, -gap, 1e-12) {
				t.Errorf("ScoreGap(1/%g, %g) = %g, want %g", odds, draw, back, -gap)
			}
			if !(gap > prev) {
				t.Errorf("ScoreGap(%g, %g) = %g, not above %g for smaller odds", odds, draw, gap, prev)
			}
			prev = gap
		}
	}
	// Draws widen the gap.
	if !(ScoreGap(2, 0) < ScoreGap(2, 100) && ScoreGap(2, 100) < ScoreGap(2, 300)) {
		t.Errorf("ScoreGap(2, θ) for θ = 0, 100, 300: %g, %g, %g; want increasing",
			ScoreGap(2, 0), ScoreGap(2, 100), ScoreGap(2, 300))
	}
}

func TestInformation(t *testing.T) {
	// Check against the definition, the sum over answers of p'²/p, with
	// the derivatives taken numerically from Probabilities.
	const h = 1e-3
	for _, draw := range []float64{0, 40, 120, 300} {
		for _, diff := range []float64{-500, -90, 0, 30, 250, 800} {
			b0, s0, w0 := Probabilities(diff-h, draw)
			b1, s1, w1 := Probabilities(diff+h, draw)
			b, s, w := Probabilities(diff, draw)
			want := 0.0
			for _, q := range [][3]float64{{b, b0, b1}, {s, s0, s1}, {w, w0, w1}} {
				if q[0] > 0 {
					dp := (q[2] - q[1]) / (2 * h)
					want += dp * dp / q[0]
				}
			}
			if got := Information(diff, draw); !near(got, want, 1e-5) {
				t.Errorf("Information(%g, %g) = %g, want %g", diff, draw, got, want)
			}
		}
	}
	// Up to a draw setting of 400·log10(3), a gap of 0 is the most
	// informative; beyond it, a gap near the draw setting is.
	switchDraw := 400 * math.Log10(3)
	if Information(0, switchDraw-5) <= Information(20, switchDraw-5) {
		t.Error("just below the switch, a gap of 0 should beat a gap of 20")
	}
	if Information(0, switchDraw+5) >= Information(20, switchDraw+5) {
		t.Error("just above the switch, a gap of 20 should beat a gap of 0")
	}
	if Information(0, 500) >= Information(500, 500) {
		t.Error("with a draw setting of 500, a gap of 500 should beat a gap of 0")
	}
}

func TestGain(t *testing.T) {
	res := &Result{
		Ratings: []float64{0, 0, 300, 0},
		DrawElo: 100,
		cov: []float64{
			40000, 0, 0, 0,
			0, 40000, 0, 0,
			0, 0, 40000, 0,
			0, 0, 0, 10000,
		},
	}
	want := math.Log1p(Information(0, 100)*80000) / 2
	if got := res.Gain(0, 1); !near(got, want, 1e-12) {
		t.Errorf("Gain(0, 1) = %g, want %g", got, want)
	}
	// A wider gap with the same uncertainty gains less, and so does the
	// same gap with less uncertainty.
	if !(res.Gain(0, 2) < res.Gain(0, 1)) || !(res.Gain(0, 3) < res.Gain(0, 1)) {
		t.Errorf("Gain(0, 2) = %g and Gain(0, 3) = %g, want both below Gain(0, 1) = %g",
			res.Gain(0, 2), res.Gain(0, 3), res.Gain(0, 1))
	}
	// Ratings that move together know their gap better.
	res.cov[1], res.cov[4] = 30000, 30000
	if !(res.Gain(0, 1) < want) {
		t.Errorf("with covariance, Gain(0, 1) = %g, want below %g", res.Gain(0, 1), want)
	}
	// A gap known exactly gains nothing.
	res.cov[1], res.cov[4] = 40000, 40000
	if g := res.Gain(0, 1); g != 0 {
		t.Errorf("with no uncertainty in the gap, Gain(0, 1) = %g, want 0", g)
	}
}

func TestAnticipate(t *testing.T) {
	cs := simulate(rand.New(rand.NewPCG(3, 4)), []float64{-150, -20, 40, 200, 0}, 90, 12)
	res := fit(t, 5, cs)
	const i, j = 1, 3
	got := res.Anticipate(i, j)

	// It must match adding the comparison's information to the precision
	// and inverting that directly.
	n := len(res.Ratings)
	prec := slices.Clone(res.cov)
	if !cholesky(prec, n) {
		t.Fatal("covariance not positive definite")
	}
	prec = cholInverse(prec, n)
	f := Information(res.Ratings[i]-res.Ratings[j], res.DrawElo)
	prec[i*n+i] += f
	prec[j*n+j] += f
	prec[i*n+j] -= f
	prec[j*n+i] -= f
	if !cholesky(prec, n) {
		t.Fatal("new precision not positive definite")
	}
	want := cholInverse(prec, n)
	for a := range n {
		for b := range n {
			if !near(got.Cov(a, b), want[a*n+b], 1e-9) {
				t.Errorf("Cov(%d, %d) = %g, want %g", a, b, got.Cov(a, b), want[a*n+b])
			}
		}
	}

	// The ratings stay put, in a copy, and the pair's gap is known better,
	// so asking about it again would gain less.
	if !slices.Equal(got.Ratings, res.Ratings) || &got.Ratings[0] == &res.Ratings[0] || got.DrawElo != res.DrawElo {
		t.Error("Anticipate changed or shared the ratings or draw setting")
	}
	d := res.DiffVar(i, j)
	if !near(got.DiffVar(i, j), d/(1+f*d), 1e-9) || !(got.Gain(i, j) < res.Gain(i, j)) {
		t.Errorf("DiffVar %g → %g, Gain %g → %g", d, got.DiffVar(i, j), res.Gain(i, j), got.Gain(i, j))
	}
	// An entry never compared keeps its uncertainty.
	res = fit(t, 3, []Comparison{{0, 1, AWins}})
	if after := res.Anticipate(0, 1); after.SD(2) != res.SD(2) || !(after.SD(0) < res.SD(0)) {
		t.Errorf("SDs %g, %g after; %g, %g before", after.SD(0), after.SD(2), res.SD(0), res.SD(2))
	}
}

func TestLevels(t *testing.T) {
	// Equal ratings with the prior's draw setting: "about the same" a third
	// of the time, so 3 levels.
	if l := fit(t, 4, nil).Levels(); !near(l, 3, 1e-9) {
		t.Errorf("equal ratings: %g levels, want 3", l)
	}
	// Ratings spread evenly over 2400 Elo with a draw setting of 120 have
	// room for about 2400 / (2 * 120) = 10 levels.
	ratings := make([]float64, 401)
	for i := range ratings {
		ratings[i] = 6 * float64(i)
	}
	if l := (&Result{Ratings: ratings, DrawElo: 120}).Levels(); l < 10 || l > 12 {
		t.Errorf("ratings spread over 2400 Elo: %g levels, want about 10", l)
	}
	if l := (&Result{Ratings: []float64{0}, DrawElo: 120}).Levels(); l != 0 {
		t.Errorf("one entry: %g levels, want 0", l)
	}
}

func TestFitRejects(t *testing.T) {
	tests := []struct {
		n  int
		cs []Comparison
	}{
		{-1, nil},
		{2, []Comparison{{0, 0, AWins}}},
		{2, []Comparison{{0, 2, AWins}}},
		{2, []Comparison{{-1, 1, AWins}}},
		{2, []Comparison{{0, 1, Outcome(3)}}},
	}
	for _, tt := range tests {
		if _, err := Fit(tt.n, tt.cs, nil); err == nil {
			t.Errorf("Fit(%d, %v): want an error", tt.n, tt.cs)
		}
	}
}

func BenchmarkFit(b *testing.B) {
	for _, n := range []int{50, 200, 500} {
		b.Run(fmt.Sprint(n, " entries"), func(b *testing.B) {
			rng := rand.New(rand.NewPCG(9, 10))
			truth := make([]float64, n)
			for i := range truth {
				truth[i] = 200 * rng.NormFloat64()
			}
			cs := simulate(rng, truth, 100, 10*n)
			for b.Loop() {
				if _, err := Fit(n, cs, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
