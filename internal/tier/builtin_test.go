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

func TestHogwarts(t *testing.T) {
	h := mustHogwarts(t, TopClosed)
	wantTiers := []string{"Outstanding", "Exceeds Expectations", "Acceptable", "Poor", "Dreadful", "Troll"}
	if !slices.Equal(h.Tiers, wantTiers) {
		t.Errorf("tiers = %q, want %q", h.Tiers, wantTiers)
	}
	wantCutoffs := rats("16/31", "21/31", "25/31", "28/31", "30/31")
	if !slices.EqualFunc(h.Cutoffs, wantCutoffs, func(a, b *big.Rat) bool { return a.Cmp(b) == 0 }) {
		t.Errorf("cut-offs = %v, want %v", h.Cutoffs, wantCutoffs)
	}
	if _, err := Hogwarts(Convention(-1)); err == nil {
		t.Error("Hogwarts with an unknown convention: want an error")
	}
}
