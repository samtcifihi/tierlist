package bayeselo

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func near(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// The gradient and Hessian must match central differences of the
// log-posterior and of the gradient.
func TestDerivatives(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	const n = 6
	cs := make([]Comparison, 40)
	for k := range cs {
		a, b := rng.IntN(n), rng.IntN(n-1)
		if b >= a {
			b++
		}
		cs[k] = Comparison{A: a, B: b, Outcome: Outcome(rng.IntN(3))}
	}
	p, err := newProblem(n, cs)
	if err != nil {
		t.Fatal(err)
	}
	const m, e = n + 1, 1e-6
	g, h := make([]float64, m), make([]float64, m*m)
	gp, gm := make([]float64, m), make([]float64, m)
	scratch := make([]float64, m*m)
	for range 5 {
		x := make([]float64, m)
		for k := range n {
			x[k] = 4*rng.Float64() - 2
		}
		x[n] = 0.1 + 1.4*rng.Float64()
		p.eval(x, g, h)
		for k := range m {
			xp, xm := slices.Clone(x), slices.Clone(x)
			xp[k] += e
			xm[k] -= e
			if num := (p.eval(xp, nil, nil) - p.eval(xm, nil, nil)) / (2 * e); !near(num, g[k], 1e-6) {
				t.Errorf("x = %v: gradient %d is %g, central difference %g", x, k, g[k], num)
			}
			p.eval(xp, gp, scratch)
			p.eval(xm, gm, scratch)
			for r := range m {
				if num := (gp[r] - gm[r]) / (2 * e); !near(num, h[r*m+k], 1e-6) {
					t.Errorf("x = %v: Hessian (%d, %d) is %g, central difference %g", x, r, k, h[r*m+k], num)
				}
			}
		}
	}
}

// drawTerms must match the direct formula where that is accurate, and stay
// finite where it is not.
func TestDrawTermsAccuracy(t *testing.T) {
	for _, d := range []float64{-3, -0.5, 0, 0.2, 2} {
		for _, th := range []float64{0.01, 0.3, 1, 4} {
			direct := math.Log(sigmoid(th-d) - sigmoid(-d-th))
			if got := drawTerms(d, th).ll; !near(got, direct, 1e-9) {
				t.Errorf("d = %g, t = %g: log P(draw) = %g, direct formula %g", d, th, got, direct)
			}
		}
	}
	// Here the direct formula rounds to log(0).
	if ll := drawTerms(40, 0.5).ll; math.IsInf(ll, 0) || math.IsNaN(ll) || ll > -38 {
		t.Errorf("log P(draw) at d = 40, t = 0.5 is %g, want about -39.5", ll)
	}
}
