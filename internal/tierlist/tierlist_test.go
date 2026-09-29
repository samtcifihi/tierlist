package tierlist

import (
	"errors"
	"math"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/samtcifihi/tierlist/internal/bayeselo"
	"github.com/samtcifihi/tierlist/internal/tier"
)

func mustNew(t *testing.T, name string, entries ...string) *List {
	t.Helper()
	l, err := New(name)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if _, err := l.AddEntry(e); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func mustRecord(t *testing.T, l *List, a, b int, ans Answer) {
	t.Helper()
	if err := l.Record(a, b, ans); err != nil {
		t.Fatal(err)
	}
}

// play asks count questions chosen by NextPair and answers them the way
// the model says a user would, given true ratings by entry ID.
func play(t *testing.T, l *List, truth map[int]float64, count int, rng *rand.Rand) {
	t.Helper()
	for range count {
		a, b, err := l.NextPair(rng)
		if err != nil {
			t.Fatal(err)
		}
		better, same, _ := bayeselo.Probabilities(truth[a]-truth[b], 120)
		ans := SecondBetter
		switch u := rng.Float64(); {
		case u < better:
			ans = FirstBetter
		case u < better+same:
			ans = AboutSame
		}
		mustRecord(t, l, a, b, ans)
	}
}

func TestEditing(t *testing.T) {
	if _, err := New("  "); err == nil {
		t.Error("New with a blank name: want an error")
	}
	l := mustNew(t, "  Films  ", "Alien", "Brazil")
	if l.Name != "Films" {
		t.Errorf("name %q, want %q", l.Name, "Films")
	}
	if id, err := l.AddEntry(" Casablanca "); err != nil || id != 3 || l.Entries[2].Name != "Casablanca" {
		t.Errorf("AddEntry gave ID %d, name %q, error %v", id, l.Entries[2].Name, err)
	}
	if _, err := l.AddEntry(""); err == nil {
		t.Error("AddEntry with a blank name: want an error")
	}
	if err := l.RenameEntry(2, "Brazil (1985)"); err != nil || l.Entries[1].Name != "Brazil (1985)" {
		t.Errorf("RenameEntry: %v, name now %q", err, l.Entries[1].Name)
	}
	if l.RenameEntry(9, "x") == nil || l.RenameEntry(1, " ") == nil {
		t.Error("RenameEntry of a missing entry or to a blank name: want errors")
	}
	for _, c := range []Comparison{{1, 9, FirstBetter}, {9, 1, FirstBetter}, {1, 1, AboutSame}, {1, 2, "maybe"}} {
		if l.Record(c.A, c.B, c.Answer) == nil {
			t.Errorf("Record(%v): want an error", c)
		}
	}
	if len(l.Comparisons) != 0 {
		t.Errorf("rejected comparisons were recorded: %v", l.Comparisons)
	}
	if err := l.SetFocus([]int{3, 1, 3}); err != nil || !slices.Equal(l.Focus, []int{3, 1}) {
		t.Errorf("SetFocus: %v, focus now %v", err, l.Focus)
	}
	if l.SetFocus([]int{9}) == nil || !slices.Equal(l.Focus, []int{3, 1}) {
		t.Errorf("SetFocus with a missing entry: want an error and no change, focus now %v", l.Focus)
	}
	if err := l.SetFocus(nil); err != nil || l.Focus != nil {
		t.Errorf("SetFocus(nil): %v, focus now %v", err, l.Focus)
	}
}

func TestFit(t *testing.T) {
	l := mustNew(t, "Films", "Alien", "Brazil", "Casablanca")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 3, 2, SecondBetter)
	fit, err := l.Fit()
	if err != nil {
		t.Fatal(err)
	}
	if !(fit.Rating(1) > fit.Rating(2)) || !(fit.Rating(2) > fit.Rating(3)) {
		t.Errorf("ratings %g, %g, %g; want decreasing", fit.Rating(1), fit.Rating(2), fit.Rating(3))
	}
	if !math.IsNaN(fit.Rating(9)) || !math.IsNaN(fit.SD(9)) || !(fit.SD(1) > 0) || !(fit.DrawElo() > 0) {
		t.Errorf("Rating(9) %g, SD(9) %g, SD(1) %g, draw setting %g",
			fit.Rating(9), fit.SD(9), fit.SD(1), fit.DrawElo())
	}
	// The fit's results are saved with the list.
	if l.Entries[0].Rating != fit.Rating(1) || l.DrawElo != fit.DrawElo() {
		t.Error("the fit did not update the saved ratings and draw setting")
	}
	// The fit is kept until the entries or comparisons change.
	if again, _ := l.Fit(); again != fit {
		t.Error("a second Fit with no changes recomputed")
	}
	l.RenameEntry(1, "Aliens")
	l.SetFocus([]int{1})
	if again, _ := l.Fit(); again != fit {
		t.Error("renaming or focusing recomputed the fit")
	}
	mustRecord(t, l, 1, 3, AboutSame)
	if again, _ := l.Fit(); again == fit {
		t.Error("recording an answer kept the old fit")
	}
	fit, _ = l.Fit()
	l.AddEntry("Dune")
	if again, _ := l.Fit(); again == fit || math.IsNaN(again.Rating(4)) {
		t.Error("adding an entry kept the old fit")
	}
}

func TestRemoveRestoreUndo(t *testing.T) {
	l := mustNew(t, "Films", "Alien", "Brazil", "Casablanca", "Dune")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 2, 3, FirstBetter)
	mustRecord(t, l, 3, 4, AboutSame)
	l.SetFocus([]int{2, 4})
	before, _ := l.Fit()

	// Removing Brazil hides it but keeps its answers, so the fit is the
	// same and Alien still sits above Casablanca through it.
	if err := l.RemoveEntry(2); err != nil {
		t.Fatal(err)
	}
	if after, _ := l.Fit(); after != before || len(l.Comparisons) != 3 {
		t.Error("removing an entry changed the fit or dropped answers")
	}
	if !slices.Equal(l.Focus, []int{4}) {
		t.Errorf("focus after removing entry 2 is %v, want [4]", l.Focus)
	}
	var shown []int
	for _, e := range l.Shown() {
		shown = append(shown, e.ID)
	}
	if !slices.Equal(shown, []int{1, 3, 4}) {
		t.Errorf("shown entries %v, want [1 3 4]", shown)
	}
	rows, err := l.Tiers()
	if err != nil {
		t.Fatal(err)
	}
	var placed []int
	for _, r := range rows {
		placed = append(placed, r.Entries...)
	}
	if !slices.Equal(placed, []int{1, 3, 4}) && !slices.Equal(placed, []int{1, 4, 3}) {
		t.Errorf("tier list holds %v; want entries 1, 3 and 4 with 1 first", placed)
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		a, b, err := l.NextPair(rng)
		if err != nil || a == 2 || b == 2 {
			t.Fatalf("NextPair gave %d, %d, %v with entry 2 removed", a, b, err)
		}
	}
	if l.Record(2, 1, FirstBetter) == nil || l.SetFocus([]int{2}) == nil {
		t.Error("recording or focusing on a removed entry: want errors")
	}
	if l.RemoveEntry(9) == nil || l.RestoreEntry(9) == nil {
		t.Error("removing or restoring a missing entry: want errors")
	}
	if err := l.RestoreEntry(2); err != nil || len(l.Shown()) != 4 {
		t.Errorf("RestoreEntry: %v, %d shown", err, len(l.Shown()))
	}

	// Undo takes back answers newest first, until there are none.
	if c, ok := l.Undo(); !ok || c != (Comparison{3, 4, AboutSame}) || len(l.Comparisons) != 2 {
		t.Errorf("Undo gave %v, %v, leaving %d answers", c, ok, len(l.Comparisons))
	}
	if again, _ := l.Fit(); again == before {
		t.Error("undo kept the old fit")
	}
	l.Undo()
	l.Undo()
	if _, ok := l.Undo(); ok {
		t.Error("Undo with no answers left reported one")
	}

	if err := l.SetName("  Movies "); err != nil || l.Name != "Movies" || l.SetName(" ") == nil {
		t.Errorf("SetName: %v, name %q", err, l.Name)
	}
}

func TestReset(t *testing.T) {
	l := mustNew(t, "Films", "Alien", "Brazil", "Casablanca")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 2, 3, AboutSame)
	l.RemoveEntry(3)
	l.SetFocus([]int{1})
	l.Display.DrawMargin = 20
	if fit, _ := l.Fit(); fit.Rating(1) == 0 {
		t.Fatal("the answers did not move the ratings")
	}
	l.Reset()
	if len(l.Comparisons) != 0 || len(l.Entries) != 3 || !l.Entries[2].Removed ||
		!slices.Equal(l.Focus, []int{1}) || l.Display.DrawMargin != 20 {
		t.Errorf("after Reset: %+v", l)
	}
	// Rating starts over from the priors alone.
	fit, err := l.Fit()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2, 3} {
		if fit.Rating(id) != 0 || fit.Points(id) != CenterPoints {
			t.Errorf("entry %d rated %g after Reset", id, fit.Rating(id))
		}
	}
	if math.Abs(fit.SameChance()-1.0/3) > 1e-9 {
		t.Errorf("draw setting %g after Reset, want the prior's", fit.SameChance())
	}
	if _, ok := l.Undo(); ok {
		t.Error("Undo after Reset took something back")
	}
}

func TestCountsAndLevels(t *testing.T) {
	l := mustNew(t, "Letters", "A", "B", "C")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 2, 3, FirstBetter)
	if c := l.Counts(); c[1] != 1 || c[2] != 2 || c[3] != 1 {
		t.Errorf("counts %v", c)
	}
	if _, ready, err := l.Levels(); err != nil || ready {
		t.Errorf("after two answers: ready %v, error %v; want not ready", ready, err)
	}
	for range 2 {
		mustRecord(t, l, 1, 2, FirstBetter)
		mustRecord(t, l, 2, 3, FirstBetter)
		mustRecord(t, l, 1, 3, FirstBetter)
	}
	levels, ready, err := l.Levels()
	if err != nil || !ready || !(levels > 1) {
		t.Errorf("after eight answers: %g levels, ready %v, error %v", levels, ready, err)
	}
	// A removed entry no longer counts, even without enough answers.
	l.AddEntry("D")
	if _, ready, _ := l.Levels(); ready {
		t.Error("a new entry with no answers should hold the readout back")
	}
	// Neither does an ignored one, which may be unknown to the user.
	l.IgnoreEntry(4)
	if _, ready, _ := l.Levels(); !ready {
		t.Error("an ignored entry should not hold the readout back")
	}
	l.UnignoreEntry(4)
	l.RemoveEntry(4)
	if _, ready, _ := l.Levels(); !ready {
		t.Error("a removed entry should not hold the readout back")
	}
}

func TestPercentiles(t *testing.T) {
	l := mustNew(t, "Letters", "A", "B", "C", "D", "E")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 2, 3, FirstBetter)
	mustRecord(t, l, 3, 4, FirstBetter)
	l.RemoveEntry(5)
	pct, err := l.Percentiles()
	if err != nil || !reflect.DeepEqual(pct, map[int]float64{1: 100, 2: 100 * 2.0 / 3, 3: 100.0 / 3, 4: 0}) {
		t.Errorf("percentiles %v, %v", pct, err)
	}
	if pct, _ := mustNew(t, "One", "A").Percentiles(); pct[1] != 100 {
		t.Errorf("a lone entry is at the %gth percentile", pct[1])
	}
}

func TestIgnores(t *testing.T) {
	l := mustNew(t, "Letters", "A", "B", "C", "D")
	if err := l.IgnoreEntry(1); err != nil {
		t.Fatal(err)
	}
	if err := l.IgnorePair(3, 2); err != nil {
		t.Fatal(err)
	}
	l.IgnorePair(2, 3) // the same pair the other way round
	l.IgnoreEntry(1)
	if l.IgnoreEntry(9) == nil || l.IgnorePair(2, 2) == nil || l.IgnorePair(2, 9) == nil {
		t.Error("ignoring a missing entry or a pair of one entry: want errors")
	}
	if !slices.Equal(l.IgnoredEntries, []int{1}) || !slices.Equal(l.IgnoredPairs, [][2]int{{3, 2}}) {
		t.Errorf("ignored entries %v, pairs %v", l.IgnoredEntries, l.IgnoredPairs)
	}
	for _, c := range []struct {
		a, b int
		ok   bool
	}{{1, 2, false}, {2, 3, false}, {3, 2, false}, {2, 4, true}, {4, 3, true}, {4, 4, false}, {4, 9, false}} {
		if got := l.CanAsk(c.a, c.b); got != c.ok {
			t.Errorf("CanAsk(%d, %d) = %v", c.a, c.b, got)
		}
	}
	// Only 2–4 and 3–4 are left to ask.
	rng := rand.New(rand.NewPCG(5, 5))
	pairs, err := l.NextPairs(nil, 6, rng)
	for _, p := range pairs {
		if !l.CanAsk(p[0], p[1]) {
			t.Errorf("asked about ignored %v", p)
		}
	}
	if err != nil || len(pairs) != 6 {
		t.Errorf("NextPairs: %v, %v", pairs, err)
	}
	l.IgnoreEntry(4)
	if _, err := l.NextPairs(nil, 1, rng); !errors.Is(err, ErrNoPair) {
		t.Errorf("with nothing left to ask: %v, want ErrNoPair", err)
	}

	// Ignores are saved, kept by Reset, and taken back one by one or all at
	// once.
	path := filepath.Join(t.TempDir(), "letters.json")
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	l2, err := Load(path)
	if err != nil || !slices.Equal(l2.IgnoredEntries, []int{1, 4}) || !slices.Equal(l2.IgnoredPairs, [][2]int{{3, 2}}) {
		t.Errorf("loaded ignores %v, %v (%v)", l2.IgnoredEntries, l2.IgnoredPairs, err)
	}
	l.Reset()
	l.UnignoreEntry(4)
	l.UnignorePair(2, 3)
	if !slices.Equal(l.IgnoredEntries, []int{1}) || len(l.IgnoredPairs) != 0 || !l.CanAsk(2, 3) || l.CanAsk(1, 2) {
		t.Errorf("after undoing two ignores: %v, %v", l.IgnoredEntries, l.IgnoredPairs)
	}
	l.ResetIgnores()
	if len(l.IgnoredEntries) != 0 || !l.CanAsk(1, 2) {
		t.Errorf("after ResetIgnores: %v", l.IgnoredEntries)
	}
}

func TestTopMode(t *testing.T) {
	// Ten entries in a known order: each beat the next once.
	l := mustNew(t, "Letters", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J")
	for id := 1; id < 10; id++ {
		mustRecord(t, l, id, id+1, FirstBetter)
	}
	if err := l.SetTop(20); err != nil {
		t.Fatal(err)
	}
	// Their percentiles are 100, 88.9, 77.8, 66.7, 55.6 and so on, so the
	// top 20% is A and B, and pairs may only include the top 40%, A to D.
	trues := func(m map[int]bool) []int {
		var ids []int
		for id, ok := range m {
			if ok {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		return ids
	}
	favoured, allowed, err := l.TopSets()
	if err != nil || !slices.Equal(trues(favoured), []int{1, 2}) || !slices.Equal(trues(allowed), []int{1, 2, 3, 4}) {
		t.Errorf("favoured %v, allowed %v, %v", trues(favoured), trues(allowed), err)
	}
	pairs, err := l.NextPairs(nil, 12, rand.New(rand.NewPCG(9, 9)))
	for _, p := range pairs {
		if p[0] > 4 || p[1] > 4 {
			t.Errorf("top mode on 20%% asked about %v", p)
		}
	}
	if err != nil || l.CanAsk(1, 5) || !l.CanAsk(1, 4) {
		t.Errorf("NextPairs %v; CanAsk(1, 5) %v, CanAsk(1, 4) %v", err, l.CanAsk(1, 5), l.CanAsk(1, 4))
	}
	// An entry not compared yet is allowed, so that it gets placed.
	l.AddEntry("K")
	if _, allowed, _ := l.TopSets(); !allowed[11] {
		t.Error("a new entry is left out of top mode")
	}
	// From 50% up every entry is allowed.
	l.SetTop(50)
	if _, allowed, _ := l.TopSets(); len(trues(allowed)) != 11 {
		t.Errorf("top mode on 50%% allows %v", trues(allowed))
	}
	// Top mode is saved, and it and focus mode are never on together.
	path := filepath.Join(t.TempDir(), "letters.json")
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	if l2, err := Load(path); err != nil || l2.Top != 50 {
		t.Errorf("loaded top mode %v, %v", l2.Top, err)
	}
	l.SetFocus([]int{5})
	if l.Top != 0 {
		t.Error("focus mode left top mode on")
	}
	l.SetTop(10)
	if l.Focus != nil {
		t.Error("top mode left focus mode on")
	}
	for _, bad := range []float64{-1, 101, math.NaN()} {
		if l.SetTop(bad) == nil {
			t.Errorf("SetTop(%g): want an error", bad)
		}
	}
	// With two entries, both are allowed however small the percentage.
	two := mustNew(t, "Two", "A", "B")
	mustRecord(t, two, 1, 2, FirstBetter)
	two.SetTop(1)
	if _, allowed, _ := two.TopSets(); !allowed[1] || !allowed[2] || !two.CanAsk(2, 1) {
		t.Errorf("two entries on top 1%%: allowed %v", allowed)
	}
}

func TestNextPairHonorsFocus(t *testing.T) {
	l := mustNew(t, "Letters", "A", "B", "C", "D", "E", "F")
	rng := rand.New(rand.NewPCG(1, 1))
	play(t, l, map[int]float64{1: 300, 2: 200, 3: 100, 4: 0, 5: -100, 6: -200}, 10, rng)
	l.SetFocus([]int{6})
	for range 20 {
		a, b, err := l.NextPair(rng)
		if err != nil {
			t.Fatal(err)
		}
		if a != 6 && b != 6 {
			t.Fatalf("in focus mode on entry 6, NextPair gave %d and %d", a, b)
		}
		mustRecord(t, l, a, b, FirstBetter)
	}
	if _, _, err := mustNew(t, "One", "A").NextPair(rng); err == nil {
		t.Error("NextPair with one entry: want an error")
	}
}

func TestNextPairs(t *testing.T) {
	l := mustNew(t, "Letters", "A", "B", "C", "D", "E", "F")
	rng := rand.New(rand.NewPCG(4, 4))
	// Three pairs lined up at once cover all six new entries.
	pairs, err := l.NextPairs(nil, 3, rng)
	seen := map[int]bool{}
	for _, p := range pairs {
		seen[p[0]], seen[p[1]] = true, true
	}
	if err != nil || len(pairs) != 3 || len(seen) != 6 {
		t.Errorf("three pairs from six new entries: %v, %v", pairs, err)
	}
	// Pending pairs are given by entry ID, and focus mode and removed
	// entries hold for every new pair.
	l.RemoveEntry(6)
	l.SetFocus([]int{1})
	more, err := l.NextPairs([][2]int{{1, 2}}, 4, rng)
	if err != nil || len(more) != 4 {
		t.Fatalf("NextPairs: %v, %v", more, err)
	}
	for _, p := range more {
		if (p[0] != 1 && p[1] != 1) || p[0] == 6 || p[1] == 6 {
			t.Errorf("focus on 1, 6 removed: queued %v", p)
		}
	}
	if more[0] == [2]int{1, 2} || more[0] == [2]int{2, 1} {
		t.Errorf("the pending pair came straight back: %v", more)
	}
	if _, err := l.NextPairs([][2]int{{1, 9}}, 1, rng); err == nil {
		t.Error("a pending pair with a missing entry: want an error")
	}
}

func TestTiers(t *testing.T) {
	l := mustNew(t, "Films", "Alien", "Brazil", "Casablanca")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 2, 3, FirstBetter)
	if d := DefaultDisplay(); !reflect.DeepEqual(d.Template, Template{Kind: "stars", MaxStars: 10}) {
		t.Errorf("a new list's template is %+v, want 0–10 stars", d.Template)
	}
	l.Display.Template.MaxStars = 5
	rows, err := l.Tiers()
	if err != nil {
		t.Fatal(err)
	}
	// On 0–5 stars, positions 1, 1/2 and 0 give 5, 3 (1/2 is a cut-off and
	// the top tier is closed, so it rounds up) and 0 stars.
	want := []Row{{"5", []int{1}}, {"4", nil}, {"3", []int{2}}, {"2", nil}, {"1", nil}, {"0", []int{3}}}
	if !slices.EqualFunc(rows, want, func(a, b Row) bool { return a.Name == b.Name && slices.Equal(a.Entries, b.Entries) }) {
		t.Errorf("rows %v, want %v", rows, want)
	}

	l.Display.Template = Template{Kind: "custom", Name: "Halves", Tiers: []string{"Good", "Bad"}, Cutoffs: []string{"1/2"}}
	l.Display.Convention = "bottom-closed"
	rows, err = l.Tiers()
	if err != nil || len(rows) != 2 || !slices.Equal(rows[0].Entries, []int{1}) || !slices.Equal(rows[1].Entries, []int{2, 3}) {
		t.Errorf("with a custom template: rows %v, error %v", rows, err)
	}

	l.Display.Template.Kind = "sideways"
	if _, err := l.Tiers(); err == nil {
		t.Error("with an unknown template: want an error")
	}
	if _, err := mustNew(t, "One", "A").Tiers(); !errors.Is(err, tier.ErrTooFewEntries) {
		t.Errorf("one entry: error %v, want tier.ErrTooFewEntries", err)
	}
}

// Entries 2 and 3 each beat entry 1 once, so their ratings are equal in
// theory. Rounding can leave them a hair apart (about 3e-14 Elo on amd64)
// or exactly equal (on arm64). Alone they would land in 5 and 4 stars, but
// the minimum draw-margin groups them, and the split group goes to the
// higher tier.
func TestTiedEntriesStayTogether(t *testing.T) {
	l := mustNew(t, "Letters", "A", "B", "C", "D", "E")
	l.Display.Template.MaxStars = 5
	mustRecord(t, l, 2, 1, FirstBetter)
	mustRecord(t, l, 3, 1, FirstBetter)
	mustRecord(t, l, 4, 1, AboutSame)
	mustRecord(t, l, 5, 1, AboutSame)
	fit, _ := l.Fit()
	r := fit.res.Ratings // entry k is at index k-1
	if d := r[1] - r[2]; math.Abs(d) > MinDrawMargin {
		t.Fatalf("entries 2 and 3 are %g Elo apart; want a tie up to rounding", d)
	}
	// Whichever way rounding tips them, they stay together. Setting the
	// fitted ratings directly makes this the same on every machine.
	for _, gap := range []float64{0, 3e-14, -3e-14, MinDrawMargin / 2} {
		r[2] = r[1] - gap
		rows, err := l.Tiers()
		if err != nil {
			t.Fatal(err)
		}
		if rows[0].Name != "5" || len(rows[0].Entries) != 2 {
			t.Errorf("entries 2 and 3 %g Elo apart: rows %v; want both in 5 stars", gap, rows)
		}
	}
	if got := fit.drawMarginElo(0); got != MinDrawMargin {
		t.Errorf("a draw-margin of 0 is used as %g Elo, want %g", got, MinDrawMargin)
	}
	if got, want := fit.drawMarginElo(30), 30/fit.PointsPerElo(); got != want {
		t.Errorf("a draw-margin of 30 points is used as %g Elo, want %g", got, want)
	}
}

func TestPoints(t *testing.T) {
	l := mustNew(t, "Letters", "A", "B", "C")
	fit, _ := l.Fit()
	// With no answers, every entry is at the center, and the draw setting
	// is its prior's, where equally rated entries are about the same a
	// third of the time.
	if fit.Points(1) != CenterPoints || math.Abs(fit.SameChance()-1.0/3) > 1e-9 {
		t.Errorf("no answers: %g points, same chance %g; want %d, 1/3", fit.Points(1), fit.SameChance(), CenterPoints)
	}

	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 1, 2, AboutSame)
	mustRecord(t, l, 3, 2, AboutSame)
	fit, _ = l.Fit()
	k, draw := fit.PointsPerElo(), fit.DrawElo()
	// A gap of 100 points is an expected score of 2:1.
	if e := bayeselo.ExpectedScore(TwoToOnePoints/k, draw); math.Abs(e-2.0/3) > 1e-12 {
		t.Errorf("at %d points the expected score is %g, want 2/3", TwoToOnePoints, e)
	}
	// Ratings, gaps and uncertainties all scale by k, around the center.
	for _, id := range []int{1, 2, 3} {
		if got, want := fit.Points(id), CenterPoints+k*fit.Rating(id); math.Abs(got-want) > 1e-9 {
			t.Errorf("entry %d: %g points, want %g", id, got, want)
		}
		if got, want := fit.PointsSD(id), k*fit.SD(id); math.Abs(got-want) > 1e-9 || !(got > 0) {
			t.Errorf("entry %d: ± %g points, want %g", id, got, want)
		}
	}
	if _, same, _ := bayeselo.Probabilities(0, draw); fit.SameChance() != same {
		t.Errorf("same chance %g, want %g", fit.SameChance(), same)
	}
	if !math.IsNaN(fit.Points(9)) || !math.IsNaN(fit.PointsSD(9)) {
		t.Errorf("no entry 9, but %g ± %g points", fit.Points(9), fit.PointsSD(9))
	}
}

// With two entries, one in the top tier and one in the bottom, grouping
// them puts both in the top tier. The draw-margin is in shown points.
func TestDrawMarginInPoints(t *testing.T) {
	l := mustNew(t, "Pair", "A", "B")
	mustRecord(t, l, 1, 2, FirstBetter)
	fit, _ := l.Fit()
	gap := fit.Points(1) - fit.Points(2)
	for _, c := range []struct {
		margin   float64
		together bool
	}{{0.999 * gap, false}, {1.001 * gap, true}} {
		l.Display.DrawMargin = c.margin
		rows, err := l.Tiers()
		if err != nil {
			t.Fatal(err)
		}
		if together := len(rows[0].Entries) == 2; together != c.together {
			t.Errorf("entries %g points apart, draw-margin %g: rows %v", gap, c.margin, rows)
		}
	}
}

func TestDisplayOptions(t *testing.T) {
	good := []Display{
		DefaultDisplay(),
		{Template: Template{Kind: "stars", MaxStars: 10, SkipZero: true, Divisions: 2}, Convention: "bottom-closed", DrawMargin: 25, GroupRule: "alternate", Prefer: "lower"},
		{Template: Template{Kind: "hogwarts"}, Convention: "top-closed", GroupRule: "middle-entry", Prefer: "higher"},
		{Template: Template{Kind: "custom", Name: "Thirds", Tiers: []string{"Top", "Middle", "Bottom"}, Cutoffs: []string{"1/3", "0.666"}},
			Convention: "top-closed", GroupRule: "middle-entry", Prefer: "higher"},
		{Template: Template{Kind: "stars", MaxStars: 5, Sizes: "geometric", Factor: "1.618", From: "worst"},
			Convention: "top-closed", GroupRule: "middle-entry", Prefer: "higher"},
		{Template: Template{Kind: "stars", MaxStars: 5, Sizes: "beta", Alpha: "1/2", Beta: "3"},
			Convention: "top-closed", GroupRule: "middle-entry", Prefer: "higher"},
	}
	for _, d := range good {
		if _, err := d.template(); err != nil {
			t.Errorf("%+v: %v", d, err)
		}
		if _, err := d.options(); err != nil {
			t.Errorf("%+v: %v", d, err)
		}
	}
	bad := []func(*Display){
		func(d *Display) { d.Template.MaxStars = 2 },
		func(d *Display) { d.Template.Kind = "" },
		func(d *Display) {
			d.Template = Template{Kind: "custom", Name: "X", Tiers: []string{"A", "B"}, Cutoffs: []string{"half"}}
		},
		func(d *Display) { d.Template = Template{Kind: "custom", Name: "X", Tiers: []string{"A", "B"}} },
		func(d *Display) { d.Convention = "sideways" },
		func(d *Display) { d.GroupRule = "biggest" },
		func(d *Display) { d.Prefer = "" },
		func(d *Display) { d.DrawMargin = -1 },
		func(d *Display) { d.DrawMargin = math.NaN() },
		func(d *Display) { d.Template.Sizes, d.Template.Factor = "geometric", "0" },
		func(d *Display) { d.Template.Sizes, d.Template.Factor = "geometric", "golden" },
		func(d *Display) { d.Template.Sizes, d.Template.Factor, d.Template.From = "geometric", "2", "middle" },
		func(d *Display) { d.Template.Sizes, d.Template.Alpha, d.Template.Beta = "beta", "-1", "2" },
		func(d *Display) { d.Template.Sizes, d.Template.Alpha, d.Template.Beta = "beta", "2", "" },
		func(d *Display) { d.Template.Sizes = "square" },
	}
	for k, change := range bad {
		d := DefaultDisplay()
		change(&d)
		_, errT := d.template()
		_, errO := d.options()
		if errT == nil && errO == nil {
			t.Errorf("bad display %d (%+v): want an error", k, d)
		}
	}
}

func TestShape(t *testing.T) {
	// 0–10 stars, nearest star: the end tiers cover half as much.
	s, err := DefaultDisplay().Shape()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tiers) != 11 || s.Tiers[0] != "0" || s.Tiers[10] != "10" || len(s.Shares) != 11 ||
		s.Shares[0] != 0.05 || s.Shares[5] != 0.1 || s.Shares[10] != 0.05 || s.Alpha != 0 || s.Beta != 0 {
		t.Errorf("default shape %+v", s)
	}
	// The larger β makes the bottom tiers bigger.
	d := DefaultDisplay()
	d.Template = Template{Kind: "stars", MaxStars: 5, Sizes: "beta", Alpha: "1/2", Beta: "3"}
	if s, err := d.Shape(); err != nil || len(s.Shares) != 6 || s.Alpha != 0.5 || s.Beta != 3 || !(s.Shares[0] > s.Shares[5]) {
		t.Errorf("Beta(1/2, 3) shape %+v, %v", s, err)
	}
	d.Template = Template{Kind: "hogwarts"}
	if s, err := d.Shape(); err != nil || s.Tiers[0] != "Troll" || s.Shares[0] != 16.0/31 || s.Alpha != 0 {
		t.Errorf("Hogwarts shape %+v, %v", s, err)
	}
	d.Template = Template{Kind: "stars", MaxStars: 5, Sizes: "beta", Alpha: "0", Beta: "3"}
	if _, err := d.Shape(); err == nil {
		t.Error("shape of Beta(0, 3): want an error")
	}
}
