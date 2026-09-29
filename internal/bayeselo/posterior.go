package bayeselo

import (
	"errors"
	"math"
)

// sigmoid is the logistic function: the Elo curve in natural units.
func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// dsigmoid is the derivative of sigmoid.
func dsigmoid(x float64) float64 { return sigmoid(x) * sigmoid(-x) }

// softplus returns log(1 + e^x) without overflow.
func softplus(x float64) float64 {
	if x > 0 {
		return x + math.Log1p(math.Exp(-x))
	}
	return math.Log1p(math.Exp(x))
}

// terms is one comparison's log-probability and its first and second
// derivatives with respect to d, the first entry's rating minus the
// second's, and t, the draw setting, all in natural units.
type terms struct {
	ll         float64
	d, t       float64
	dd, dt, tt float64
}

// add adds k times o to a.
func (a *terms) add(o terms, k float64) {
	a.ll += k * o.ll
	a.d += k * o.d
	a.t += k * o.t
	a.dd += k * o.dd
	a.dt += k * o.dt
	a.tt += k * o.tt
}

// winTerms is for the first entry being better: log sigmoid(d-t).
func winTerms(d, t float64) terms {
	x := d - t
	q := sigmoid(-x) // the chance it would not have been
	h := dsigmoid(x)
	return terms{ll: -softplus(-x), d: q, t: -q, dd: -h, dt: h, tt: -h}
}

// lossTerms is for the second entry being better: log sigmoid(-d-t).
func lossTerms(d, t float64) terms {
	w := winTerms(-d, t)
	return terms{ll: w.ll, d: -w.d, t: w.t, dd: w.dd, dt: -w.dt, tt: w.tt}
}

// drawTerms is for the entries being about the same:
// log(sigmoid(t-d) - sigmoid(-d-t)). It is computed in the equivalent form
// d + log(2 sinh t) - softplus(d-t) - softplus(d+t), which stays accurate
// when a draw is very unlikely. t must be positive.
func drawTerms(d, t float64) terms {
	a, b := sigmoid(d-t), sigmoid(d+t)
	ha, hb := dsigmoid(d-t), dsigmoid(d+t)
	sh := math.Sinh(t)
	return terms{
		ll: d + t + math.Log(-math.Expm1(-2*t)) - softplus(d-t) - softplus(d+t),
		d:  1 - a - b,
		t:  1/math.Tanh(t) + a - b,
		dd: -ha - hb,
		dt: ha - hb,
		tt: -1/(sh*sh) - ha - hb,
	}
}

// eval returns the log-posterior, up to a constant, at x: the ratings
// followed by the draw setting, in natural units. If g and h are not nil,
// it also stores the gradient in g and the Hessian in h, (n+1)×(n+1)
// row-major.
func (p *problem) eval(x, g, h []float64) float64 {
	n, m := p.n, p.n+1
	t := x[n]
	if !(t > 0) {
		return math.Inf(-1)
	}
	if g != nil {
		clear(g)
		clear(h)
	}

	// The draw setting's prior: one win, one loss and one draw between
	// two equally rated entries.
	tp := winTerms(0, t)
	tp.add(lossTerms(0, t), 1)
	tp.add(drawTerms(0, t), 1)
	ll := tp.ll
	if g != nil {
		g[n] += tp.t
		h[n*m+n] += tp.tt
	}

	// Each entry's prior: one win and one loss against the dummy at 0, on
	// the plain Elo curve.
	for i, s := range x[:n] {
		ll -= softplus(-s) + softplus(s)
		if g != nil {
			g[i] += sigmoid(-s) - sigmoid(s)
			h[i*m+i] -= 2 * dsigmoid(s)
		}
	}

	for _, pr := range p.pairs {
		i, j := pr.i, pr.j
		d := x[i] - x[j]
		var tm terms
		if pr.wins > 0 {
			tm.add(winTerms(d, t), pr.wins)
		}
		if pr.losses > 0 {
			tm.add(lossTerms(d, t), pr.losses)
		}
		if pr.draws > 0 {
			tm.add(drawTerms(d, t), pr.draws)
		}
		ll += tm.ll
		if g != nil {
			// d = x[i] - x[j], so its derivatives flip sign for x[j].
			g[i] += tm.d
			g[j] -= tm.d
			g[n] += tm.t
			h[i*m+i] += tm.dd
			h[j*m+j] += tm.dd
			h[i*m+j] -= tm.dd
			h[j*m+i] -= tm.dd
			h[i*m+n] += tm.dt
			h[n*m+i] += tm.dt
			h[j*m+n] -= tm.dt
			h[n*m+j] -= tm.dt
			h[n*m+n] += tm.tt
		}
	}
	return ll
}

const (
	maxIterations = 100
	// stepTolerance, in natural units (about 2e-6 Elo), ends the search
	// once a Newton step is this small; the step itself then estimates the
	// remaining error.
	stepTolerance = 1e-8
)

// Precision bounds how far, in Elo, a fitted rating can be from the exact
// maximum of the posterior. In practice ratings that are equal in theory
// come out within about 1e-13 Elo of each other.
const Precision = stepTolerance / eloToNat

// maximize moves x to the maximum of the log-posterior with Newton's
// method and returns the inverse of the negative Hessian there,
// (n+1)×(n+1) row-major.
func (p *problem) maximize(x []float64) ([]float64, error) {
	m := p.n + 1
	g := make([]float64, m)
	h := make([]float64, m*m)
	step := make([]float64, m)
	trial := make([]float64, m)
	ll := p.eval(x, g, h)
	if math.IsInf(ll, -1) || math.IsNaN(ll) {
		return nil, errors.New("bayeselo: bad starting point")
	}
	for range maxIterations {
		// Factor -H, which is positive definite because the log-posterior
		// is strictly concave, and solve -H step = g.
		for k := range h {
			h[k] = -h[k]
		}
		if !cholesky(h, m) {
			return nil, errors.New("bayeselo: the Hessian is not negative definite")
		}
		copy(step, g)
		cholSolve(h, m, step)

		small := true
		for _, s := range step {
			if math.Abs(s) > stepTolerance {
				small = false
				break
			}
		}
		if small {
			for k := range x {
				x[k] += step[k]
			}
			return cholInverse(h, m), nil
		}

		// Back off from the full step until the log-posterior rises enough.
		// The slack allows for rounding once the gains are tiny.
		gain := 0.0
		for k := range g {
			gain += g[k] * step[k]
		}
		slack := 1e-12 * (1 + math.Abs(ll))
		for alpha := 1.0; ; alpha /= 2 {
			if alpha < 1e-10 {
				return nil, errors.New("bayeselo: line search failed")
			}
			for k := range x {
				trial[k] = x[k] + alpha*step[k]
			}
			if p.eval(trial, nil, nil) >= ll+1e-4*alpha*gain-slack {
				break
			}
		}
		copy(x, trial)
		ll = p.eval(x, g, h)
	}
	return nil, errors.New("bayeselo: did not converge")
}
