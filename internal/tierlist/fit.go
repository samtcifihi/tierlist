package tierlist

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/samtcifihi/tierlist/internal/bayeselo"
	"github.com/samtcifihi/tierlist/internal/pairing"
	"github.com/samtcifihi/tierlist/internal/tier"
)

// A Fit is the Bayes Elo fit of a list's comparisons, looked up by entry
// ID.
type Fit struct {
	res   *bayeselo.Result
	index map[int]int // entry ID to index in res
	scale float64     // shown points per Elo
}

// Rating returns the rating of the entry with ID id, in Elo, or NaN if
// there is no such entry.
func (f *Fit) Rating(id int) float64 {
	if i, ok := f.index[id]; ok {
		return f.res.Ratings[i]
	}
	return math.NaN()
}

// SD returns the standard deviation of the entry's rating, in Elo, or NaN
// if there is no such entry.
func (f *Fit) SD(id int) float64 {
	if i, ok := f.index[id]; ok {
		return f.res.SD(i)
	}
	return math.NaN()
}

// DrawElo returns the fitted draw setting, in Elo.
func (f *Fit) DrawElo() float64 { return f.res.DrawElo }

// The pages show ratings in points rather than Elo. An entry not yet
// compared shows CenterPoints, as the dummy entry would, and a gap of
// TwoToOnePoints means the higher entry's expected score is twice the
// lower's (see bayeselo.ExpectedScore), whatever the draw setting.
const (
	CenterPoints   = 1500
	TwoToOnePoints = 100
)

// PointsPerElo returns how many shown points one Elo makes at the fitted
// draw setting.
func (f *Fit) PointsPerElo() float64 { return f.scale }

// Points returns the rating of the entry with ID id as the pages show it,
// or NaN if there is no such entry.
func (f *Fit) Points(id int) float64 { return CenterPoints + f.scale*f.Rating(id) }

// PointsSD returns the standard deviation of the entry's rating in shown
// points, or NaN if there is no such entry.
func (f *Fit) PointsSD(id int) float64 { return f.scale * f.SD(id) }

// SameChance returns the draw setting as the pages show it: the chance
// that two equally rated entries are judged about the same.
func (f *Fit) SameChance() float64 {
	_, same, _ := bayeselo.Probabilities(0, f.res.DrawElo)
	return same
}

// drawMarginElo converts a draw-margin in shown points to Elo, raising it
// to at least MinDrawMargin.
func (f *Fit) drawMarginElo(points float64) float64 {
	return max(points/f.scale, MinDrawMargin)
}

// LevelsAfter is how many answers every shown entry needs before the
// levels readout means much; before that, the ratings have not spread out.
const LevelsAfter = 3

// Levels returns how many levels of quality the user tells apart among the
// shown entries (see bayeselo.Result.Levels), and whether every shown entry
// has at least LevelsAfter answers yet.
func (l *List) Levels() (levels float64, ready bool, err error) {
	fit, err := l.Fit()
	if err != nil {
		return 0, false, err
	}
	counts := l.Counts()
	shown := l.Shown()
	ready = len(shown) >= 2
	ratings := make([]float64, len(shown))
	for k, e := range shown {
		ratings[k] = fit.Rating(e.ID)
		ready = ready && counts[e.ID] >= LevelsAfter
	}
	return (&bayeselo.Result{Ratings: ratings, DrawElo: fit.DrawElo()}).Levels(), ready, nil
}

// Fit returns the Bayes Elo fit of the list's comparisons, redoing it if
// the entries or comparisons have changed since the last one. It also
// updates the ratings and draw setting saved with the list.
func (l *List) Fit() (*Fit, error) {
	if l.fit != nil {
		return l.fit, nil
	}
	n := len(l.Entries)
	index := make(map[int]int, n)
	start := &bayeselo.Result{Ratings: make([]float64, n), DrawElo: l.DrawElo}
	for i, e := range l.Entries {
		index[e.ID] = i
		start.Ratings[i] = e.Rating
	}
	res, err := bayeselo.Fit(n, l.history(index), start)
	if err != nil {
		return nil, err
	}
	for i := range l.Entries {
		l.Entries[i].Rating = res.Ratings[i]
	}
	l.DrawElo = res.DrawElo
	scale := TwoToOnePoints / bayeselo.ScoreGap(2, res.DrawElo)
	l.fit = &Fit{res: res, index: index, scale: scale}
	return l.fit, nil
}

// history returns the comparisons in order, by entry index.
func (l *List) history(index map[int]int) []bayeselo.Comparison {
	cs := make([]bayeselo.Comparison, len(l.Comparisons))
	for k, c := range l.Comparisons {
		o := bayeselo.Draw
		switch c.Answer {
		case FirstBetter:
			o = bayeselo.AWins
		case SecondBetter:
			o = bayeselo.BWins
		}
		cs[k] = bayeselo.Comparison{A: index[c.A], B: index[c.B], Outcome: o}
	}
	return cs
}

// NextPair returns the IDs of the next two entries to compare, in the
// order to show them, honoring focus mode and leaving out removed entries.
// rng breaks ties and picks the sides.
func (l *List) NextPair(rng *rand.Rand) (first, second int, err error) {
	fit, err := l.Fit()
	if err != nil {
		return 0, 0, err
	}
	var opts pairing.Options
	for _, id := range l.Focus {
		opts.Focus = append(opts.Focus, fit.index[id])
	}
	for i, e := range l.Entries {
		if e.Removed {
			opts.Hidden = append(opts.Hidden, i)
		}
	}
	a, b, err := pairing.Next(fit.res, l.history(fit.index), opts, rng)
	if err != nil {
		return 0, 0, err
	}
	return l.Entries[a].ID, l.Entries[b].ID, nil
}

// A Row is one tier of the displayed list.
type Row struct {
	Name    string
	Entries []int // entry IDs, best first
}

// Tiers places the shown entries into tiers using the list's display
// options, best tier first. With fewer than two shown entries it returns
// tier.ErrTooFewEntries.
func (l *List) Tiers() ([]Row, error) {
	tmpl, err := l.Display.template()
	if err != nil {
		return nil, err
	}
	opts, err := l.Display.options()
	if err != nil {
		return nil, err
	}
	fit, err := l.Fit()
	if err != nil {
		return nil, err
	}
	opts.DrawMargin = fit.drawMarginElo(opts.DrawMargin)
	// Rank the shown entries best first; equal ratings keep the list's
	// order.
	var order []int
	for i, e := range l.Entries {
		if !e.Removed {
			order = append(order, i)
		}
	}
	r := fit.res.Ratings
	slices.SortStableFunc(order, func(i, j int) int { return cmp.Compare(r[j], r[i]) })
	ratings := make([]float64, len(order))
	for k, i := range order {
		ratings[k] = r[i]
	}
	placed, err := tier.Place(ratings, tmpl, opts)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, len(tmpl.Tiers))
	for t, name := range tmpl.Tiers {
		rows[t].Name = name
	}
	for k, i := range order {
		rows[placed[k]].Entries = append(rows[placed[k]].Entries, l.Entries[i].ID)
	}
	return rows, nil
}
