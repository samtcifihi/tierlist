// Package bayeselo fits Bayes Elo ratings to pairwise comparisons, as
// described in the README's Rating section.
//
// For entries A and B, the model gives
//
//	P(A better)       = f(rA - rB - θ)
//	P(B better)       = f(rB - rA - θ)
//	P(about the same) = the rest
//
// where f(x) = 1/(1 + 10^(-x/400)) is the Elo curve and θ ≥ 0 is the draw
// setting. Every entry has a prior of one win and one loss against a dummy
// entry fixed at 0, on the plain Elo curve. θ has a prior of one win, one
// loss and one draw between two equally rated entries, and is otherwise
// estimated from the comparisons. Fit finds the most probable ratings and
// θ, of which there is exactly one set because the log-posterior is
// strictly concave, and the ratings' covariance from the curvature there.
package bayeselo

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

// Outcome is the answer to one comparison.
type Outcome int

const (
	AWins Outcome = iota // A is better
	BWins                // B is better
	Draw                 // they are about the same
)

// A Comparison is one answer about entries A and B, given as indices from
// 0 to n-1. Which entry was shown first does not matter to the model.
type Comparison struct {
	A, B    int
	Outcome Outcome
}

// Result holds fitted Bayes Elo quantities.
type Result struct {
	// Ratings holds each entry's rating in Elo, relative to the dummy
	// entry, whose rating is fixed at 0.
	Ratings []float64
	// DrawElo is the draw setting θ, in Elo.
	DrawElo float64

	cov []float64 // covariance of Ratings in Elo², n×n row-major
}

// Cov returns the covariance of the ratings of entries i and j, in Elo².
// It is only available on results returned by Fit.
func (r *Result) Cov(i, j int) float64 { return r.cov[i*len(r.Ratings)+j] }

// SD returns the standard deviation of entry i's rating, in Elo.
func (r *Result) SD(i int) float64 { return math.Sqrt(r.Cov(i, i)) }

// DiffVar returns the variance of the difference between the ratings of
// entries i and j, in Elo².
func (r *Result) DiffVar(i, j int) float64 {
	return r.Cov(i, i) + r.Cov(j, j) - 2*r.Cov(i, j)
}

// Information returns the Fisher information that one comparison's answer
// carries about the rating gap diff (Elo), with draw setting drawElo, per
// Elo². It is highest where the answer is hardest to predict: at a gap of
// 0 while drawElo is below 400·log10(3), about 191, where equally rated
// entries are called about the same half the time, and near a gap of
// drawElo above that.
func Information(diff, drawElo float64) float64 {
	d, t := diff*eloToNat, drawElo*eloToNat
	// The expected negative second derivative of the log-probability of
	// each answer, weighted by its probability, simplifies to this.
	nat := dsigmoid(d-t)*sigmoid(d+t) + dsigmoid(d+t)*sigmoid(t-d)
	return nat * eloToNat * eloToNat
}

// Gain returns the expected information, in nats, that comparing entries i
// and j would add to the ratings under their Gaussian approximation:
// ½ ln(1 + Information × DiffVar). It is only available on results
// returned by Fit.
func (r *Result) Gain(i, j int) float64 {
	v := max(r.DiffVar(i, j), 0) // rounding could leave it a hair below 0
	return math.Log1p(Information(r.Ratings[i]-r.Ratings[j], r.DrawElo)*v) / 2
}

// Levels returns how many levels of quality the user tells apart among the
// entries, by the model: 1 divided by the probability that two randomly
// chosen entries would be judged about the same. It returns 0 for fewer
// than two entries.
func (r *Result) Levels() float64 {
	n := len(r.Ratings)
	if n < 2 {
		return 0
	}
	same := 0.0
	for i := range n {
		for j := i + 1; j < n; j++ {
			_, s, _ := Probabilities(r.Ratings[i]-r.Ratings[j], r.DrawElo)
			same += s
		}
	}
	return float64(n*(n-1)/2) / same
}

// Internally, ratings and the draw setting are in natural units, where the
// Elo curve is the logistic function: nats = Elo * eloToNat.
const eloToNat = math.Ln10 / 400

// Probabilities returns the model's chances that an entry rated diff Elo
// above another is judged better, about the same, or worse, with draw
// setting drawElo ≥ 0.
func Probabilities(diff, drawElo float64) (better, same, worse float64) {
	d, t := diff*eloToNat, drawElo*eloToNat
	better, worse = sigmoid(d-t), sigmoid(-d-t)
	if t > 0 {
		same = math.Exp(drawTerms(d, t).ll)
	}
	return better, same, worse
}

// Fit returns the Bayes Elo ratings of n entries given the comparisons.
// If start holds n ratings, the search starts from its ratings and draw
// setting. The result is the same either way; a start near it is only
// faster.
func Fit(n int, cs []Comparison, start *Result) (*Result, error) {
	if n < 0 {
		return nil, fmt.Errorf("bayeselo: %d entries", n)
	}
	p, err := newProblem(n, cs)
	if err != nil {
		return nil, err
	}
	x := make([]float64, n+1) // ratings, then the draw setting, in nats
	x[n] = math.Ln2           // the draw setting's prior alone gives this
	if start != nil && len(start.Ratings) == n && usable(start) {
		for i, r := range start.Ratings {
			x[i] = r * eloToNat
		}
		x[n] = start.DrawElo * eloToNat
	}
	inv, err := p.maximize(x)
	if err != nil {
		return nil, err
	}
	res := &Result{
		Ratings: make([]float64, n),
		DrawElo: x[n] / eloToNat,
		cov:     make([]float64, n*n),
	}
	for i := range n {
		res.Ratings[i] = x[i] / eloToNat
		for j := range n {
			res.cov[i*n+j] = inv[i*(n+1)+j] / (eloToNat * eloToNat)
		}
	}
	return res, nil
}

// usable reports whether start's values can seed a search.
func usable(start *Result) bool {
	for _, r := range start.Ratings {
		if math.IsNaN(r) || math.IsInf(r, 0) {
			return false
		}
	}
	return start.DrawElo > 0 && !math.IsInf(start.DrawElo, 1)
}

// A pair sums up all comparisons between entries i and j, where i < j.
type pair struct {
	i, j                int
	wins, losses, draws float64 // i better, j better, about the same
}

type problem struct {
	n     int
	pairs []pair
}

func newProblem(n int, cs []Comparison) (*problem, error) {
	index := make(map[[2]int]int)
	var pairs []pair
	for k, c := range cs {
		switch {
		case c.A < 0 || c.A >= n || c.B < 0 || c.B >= n:
			return nil, fmt.Errorf("bayeselo: comparison %d names an entry outside 0 to %d", k+1, n-1)
		case c.A == c.B:
			return nil, fmt.Errorf("bayeselo: comparison %d compares entry %d with itself", k+1, c.A)
		case c.Outcome != AWins && c.Outcome != BWins && c.Outcome != Draw:
			return nil, fmt.Errorf("bayeselo: comparison %d has unknown outcome %d", k+1, c.Outcome)
		}
		i, j, o := c.A, c.B, c.Outcome
		if i > j {
			i, j = j, i
			switch o {
			case AWins:
				o = BWins
			case BWins:
				o = AWins
			}
		}
		key := [2]int{i, j}
		pi, ok := index[key]
		if !ok {
			pi = len(pairs)
			index[key] = pi
			pairs = append(pairs, pair{i: i, j: j})
		}
		switch o {
		case AWins:
			pairs[pi].wins++
		case BWins:
			pairs[pi].losses++
		case Draw:
			pairs[pi].draws++
		}
	}
	// A fixed order makes the result independent of the comparisons' order,
	// down to floating-point rounding.
	slices.SortFunc(pairs, func(a, b pair) int {
		return cmp.Or(cmp.Compare(a.i, b.i), cmp.Compare(a.j, b.j))
	})
	return &problem{n: n, pairs: pairs}, nil
}
