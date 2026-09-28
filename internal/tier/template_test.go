package tier

import (
	"math/big"
	"testing"
)

func rat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic("bad rational " + s)
	}
	return r
}

func rats(ss ...string) []*big.Rat {
	rs := make([]*big.Rat, len(ss))
	for i, s := range ss {
		rs[i] = rat(s)
	}
	return rs
}

func mustNew(t *testing.T, name string, tiers []string, cutoffs []*big.Rat, c Convention) Template {
	t.Helper()
	tmpl, err := New(name, tiers, cutoffs, c)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func mustStars(t *testing.T, o StarOptions, c Convention) Template {
	t.Helper()
	tmpl, err := Stars(o, c)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func mustHogwarts(t *testing.T, c Convention) Template {
	t.Helper()
	tmpl, err := Hogwarts(c)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func TestParseCutoff(t *testing.T) {
	good := []struct{ in, want string }{
		{"1/3", "1/3"},
		{"0.25", "1/4"},
		{".5", "1/2"},
		{"1.", "1"},
		{"0", "0"},
		{"1", "1"},
		{"0.10", "1/10"},
		{" 2 / 6 ", "1/3"},
		{"010/31", "10/31"}, // base 10, not octal
		{"3/2", "3/2"},      // out of range, but that is for Validate to catch
	}
	for _, tt := range good {
		got, err := ParseCutoff(tt.in)
		if err != nil {
			t.Errorf("ParseCutoff(%q): %v", tt.in, err)
		} else if got.Cmp(rat(tt.want)) != 0 {
			t.Errorf("ParseCutoff(%q) = %s, want %s", tt.in, got.RatString(), tt.want)
		}
	}
	for _, in := range []string{"", " ", ".", "/", "abc", "1/0", "-0.5", "+0.5", "0x10", "1e-1", "1_0", "1/2/3", "0.5.5", "½", "1/", "/3", "1,5"} {
		if got, err := ParseCutoff(in); err == nil {
			t.Errorf("ParseCutoff(%q) = %s, want an error", in, got.RatString())
		}
	}
}

func TestValidate(t *testing.T) {
	three := []string{"top", "middle", "bottom"}
	tests := []struct {
		name    string
		tmpl    Template
		wantErr bool
	}{
		{"top closed", Template{Tiers: three, Cutoffs: rats("1/3", "2/3")}, false},
		{"bottom closed", Template{Tiers: three, Cutoffs: rats("1/3", "2/3"), Convention: BottomClosed}, false},
		{"one tier", Template{Tiers: []string{"all"}}, false},
		{"no tiers", Template{}, true},
		{"blank name", Template{Tiers: []string{"top", " ", "bottom"}, Cutoffs: rats("1/3", "2/3")}, true},
		{"too few cut-offs", Template{Tiers: three, Cutoffs: rats("1/2")}, true},
		{"too many cut-offs", Template{Tiers: three, Cutoffs: rats("1/4", "1/2", "3/4")}, true},
		{"missing cut-off", Template{Tiers: three, Cutoffs: []*big.Rat{rat("1/3"), nil}}, true},
		{"decreasing", Template{Tiers: three, Cutoffs: rats("2/3", "1/3")}, true},
		{"repeated", Template{Tiers: three, Cutoffs: rats("1/2", "1/2")}, true},
		{"below 0", Template{Tiers: three, Cutoffs: rats("-1/3", "2/3")}, true},
		{"above 1", Template{Tiers: three, Cutoffs: rats("1/3", "4/3")}, true},
		// A cut-off at 0 or 1 makes a one-point tier under one convention
		// and an empty tier under the other.
		{"0, top closed", Template{Tiers: three, Cutoffs: rats("0", "1/2")}, true},
		{"0, bottom closed", Template{Tiers: three, Cutoffs: rats("0", "1/2"), Convention: BottomClosed}, false},
		{"1, top closed", Template{Tiers: three, Cutoffs: rats("1/2", "1")}, false},
		{"1, bottom closed", Template{Tiers: three, Cutoffs: rats("1/2", "1"), Convention: BottomClosed}, true},
		{"unknown convention", Template{Tiers: three, Cutoffs: rats("1/3", "2/3"), Convention: 2}, true},
	}
	for _, tt := range tests {
		if err := tt.tmpl.Validate(); (err != nil) != tt.wantErr {
			t.Errorf("%s: Validate() = %v, want error: %v", tt.name, err, tt.wantErr)
		}
	}
}

// inTier reports whether position p lies in tier i (best first) of t,
// using each tier's range as the README defines it.
func inTier(t Template, i int, p *big.Rat) bool {
	n := len(t.Tiers)
	k := n - 1 - i // counting up from the bottom tier
	lo, hi := big.NewRat(0, 1), big.NewRat(1, 1)
	if k > 0 {
		lo = t.Cutoffs[k-1]
	}
	if k < n-1 {
		hi = t.Cutoffs[k]
	}
	aboveLo, belowHi := lo.Cmp(p) <= 0, p.Cmp(hi) <= 0
	switch {
	case t.Convention == TopClosed && k < n-1: // [a, b) except the top tier
		belowHi = p.Cmp(hi) < 0
	case t.Convention == BottomClosed && k > 0: // (a, b] except the bottom tier
		aboveLo = lo.Cmp(p) < 0
	}
	return aboveLo && belowHi
}

func TestTierAtMatchesIntervals(t *testing.T) {
	var templates []Template
	for _, c := range []Convention{TopClosed, BottomClosed} {
		for _, o := range []StarOptions{{Max: 5}, {Max: 10}, {Max: 5, SkipZero: true, Divisions: 2}, {Max: 3, Divisions: 3}} {
			templates = append(templates, mustStars(t, o, c))
		}
		templates = append(templates, mustHogwarts(t, c))
	}
	// One-point tiers at either end.
	templates = append(templates,
		mustNew(t, "best only", []string{"best", "rest"}, rats("1"), TopClosed),
		mustNew(t, "worst only", []string{"rest", "worst"}, rats("0"), BottomClosed),
	)

	// Every cut-off above has a denominator of at most 62, so this tries
	// every cut-off as well as positions between them.
	for _, tmpl := range templates {
		for den := int64(1); den <= 62; den++ {
			for num := int64(0); num <= den; num++ {
				p := big.NewRat(num, den)
				got := tmpl.TierAt(p)
				for i := range tmpl.Tiers {
					if inTier(tmpl, i, p) != (i == got) {
						t.Fatalf("%s, convention %d: TierAt(%s) = %d, but tier %d's range disagrees",
							tmpl.Name, tmpl.Convention, p.RatString(), got, i)
					}
				}
			}
		}
	}
}

func TestTierAtOnCutoff(t *testing.T) {
	p := big.NewRat(30, 31) // between Outstanding and Exceeds Expectations
	for c, want := range map[Convention]string{TopClosed: "Outstanding", BottomClosed: "Exceeds Expectations"} {
		h := mustHogwarts(t, c)
		if got := h.Tiers[h.TierAt(p)]; got != want {
			t.Errorf("convention %d: position 30/31 is in %q, want %q", c, got, want)
		}
	}
}
