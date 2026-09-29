package tierlist

import (
	"errors"
	"math"
	"math/rand/v2"
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
	if !math.IsNaN(fit.Rating(9)) || !math.IsNaN(fit.SD(9)) || !(fit.SD(1) > 0) || !(fit.DrawElo() > 0) || !(fit.Levels() > 0) {
		t.Errorf("Rating(9) %g, SD(9) %g, SD(1) %g, draw setting %g, levels %g",
			fit.Rating(9), fit.SD(9), fit.SD(1), fit.DrawElo(), fit.Levels())
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

func TestTiers(t *testing.T) {
	l := mustNew(t, "Films", "Alien", "Brazil", "Casablanca")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 2, 3, FirstBetter)
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

func TestDisplayOptions(t *testing.T) {
	good := []Display{
		DefaultDisplay(),
		{Template: Template{Kind: "stars", MaxStars: 10, SkipZero: true, Divisions: 2}, Convention: "bottom-closed", DrawMargin: 25, GroupRule: "alternate", Prefer: "lower"},
		{Template: Template{Kind: "hogwarts"}, Convention: "top-closed", GroupRule: "middle-entry", Prefer: "higher"},
		{Template: Template{Kind: "custom", Name: "Thirds", Tiers: []string{"Top", "Middle", "Bottom"}, Cutoffs: []string{"1/3", "0.666"}},
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
