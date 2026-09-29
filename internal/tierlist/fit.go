package tierlist

import (
	"cmp"
	"fmt"
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
// shown entries (see bayeselo.Result.Levels), and whether every one of them
// has at least LevelsAfter answers yet. Ignored entries are left out, as
// the user may not know them.
func (l *List) Levels() (levels float64, ready bool, err error) {
	fit, err := l.Fit()
	if err != nil {
		return 0, false, err
	}
	counts := l.Counts()
	var ratings []float64
	ready = true
	for _, e := range l.Shown() {
		if l.EntryIgnored(e.ID) {
			continue
		}
		ratings = append(ratings, fit.Rating(e.ID))
		ready = ready && counts[e.ID] >= LevelsAfter
	}
	ready = ready && len(ratings) >= 2
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
// order to show them, honoring focus mode and leaving out removed and
// ignored entries and ignored pairs.
// rng breaks ties and picks the sides.
func (l *List) NextPair(rng *rand.Rand) (first, second int, err error) {
	pairs, err := l.NextPairs(nil, 1, rng)
	if err != nil {
		return 0, 0, err
	}
	return pairs[0][0], pairs[0][1], nil
}

// ErrNoPair is returned, wrapped or not, when no pair can be asked: every
// pair left is ignored, or has a removed or ignored entry, or leaves out
// the entries in focus.
var ErrNoPair = pairing.ErrNoPair

// NextPairs returns k more pairs of entry IDs to compare after pending,
// the pairs already lined up to be asked, in order (see pairing.Queue).
// Like NextPair, it honors focus mode and leaves out removed and ignored
// entries and ignored pairs; the pending pairs must be between entries of
// the list.
func (l *List) NextPairs(pending [][2]int, k int, rng *rand.Rand) ([][2]int, error) {
	fit, err := l.Fit()
	if err != nil {
		return nil, err
	}
	var opts pairing.Options
	for _, id := range l.Focus {
		opts.Focus = append(opts.Focus, fit.index[id])
	}
	favoured, allowed, err := l.TopSets()
	if err != nil {
		return nil, err
	}
	for i, e := range l.Entries {
		if e.Removed || l.EntryIgnored(e.ID) || (allowed != nil && !allowed[e.ID]) {
			opts.Hidden = append(opts.Hidden, i)
		}
		if favoured[e.ID] {
			opts.Favour = append(opts.Favour, i)
		}
	}
	for _, p := range l.IgnoredPairs {
		opts.Skip = append(opts.Skip, [2]int{fit.index[p[0]], fit.index[p[1]]})
	}
	queued := make([][2]int, len(pending))
	for k, p := range pending {
		a, okA := fit.index[p[0]]
		b, okB := fit.index[p[1]]
		if !okA || !okB {
			return nil, fmt.Errorf("pending pair of entries %d and %d, which are not both in the list", p[0], p[1])
		}
		queued[k] = [2]int{a, b}
	}
	pairs, err := pairing.Queue(fit.res, l.history(fit.index), queued, k, opts, rng)
	if err != nil {
		return nil, err
	}
	for k, p := range pairs {
		pairs[k] = [2]int{l.Entries[p[0]].ID, l.Entries[p[1]].ID}
	}
	return pairs, nil
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
	order := l.ranked(fit)
	ratings := make([]float64, len(order))
	for k, i := range order {
		ratings[k] = fit.res.Ratings[i]
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

// ranked returns the indices of the shown entries, best first. Equal
// ratings keep the list's order.
func (l *List) ranked(fit *Fit) []int {
	var order []int
	for i, e := range l.Entries {
		if !e.Removed {
			order = append(order, i)
		}
	}
	r := fit.res.Ratings
	slices.SortStableFunc(order, func(i, j int) int { return cmp.Compare(r[j], r[i]) })
	return order
}

// percentile is the percentile of the k-th best of n entries, counting
// from 0: 100 for the best, 0 for the worst, evenly spaced between, like
// the positions tier templates use.
func percentile(k, n int) float64 {
	if n < 2 {
		return 100
	}
	return 100 * float64(n-1-k) / float64(n-1)
}

// Percentiles returns each shown entry's percentile in the list, by ID
// (see percentile), in the order the tier list uses.
func (l *List) Percentiles() (map[int]float64, error) {
	fit, err := l.Fit()
	if err != nil {
		return nil, err
	}
	order := l.ranked(fit)
	out := make(map[int]float64, len(order))
	for k, i := range order {
		out[l.Entries[i].ID] = percentile(k, len(order))
	}
	return out, nil
}

// TopSets returns, by entry ID, the entries top mode favours, those at or
// above the (100 - Top)th percentile, and the ones it allows in pairs at
// all: those at or above the (100 - 2·Top)th, which is all of them from
// Top = 50 up, the two best whatever Top is, and any not compared yet,
// whose ratings mean nothing so far. With top mode off both are nil.
func (l *List) TopSets() (favoured, allowed map[int]bool, err error) {
	if l.Top <= 0 {
		return nil, nil, nil
	}
	fit, err := l.Fit()
	if err != nil {
		return nil, nil, err
	}
	counts := l.Counts()
	order := l.ranked(fit)
	favoured, allowed = make(map[int]bool), make(map[int]bool)
	for k, i := range order {
		id := l.Entries[i].ID
		p := percentile(k, len(order))
		favoured[id] = p >= 100-l.Top
		allowed[id] = p >= 100-2*l.Top || k < 2 || counts[id] == 0
	}
	return favoured, allowed, nil
}
