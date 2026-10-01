package tierlist

import (
	"reflect"
	"slices"
	"testing"
)

func TestForgetAnswers(t *testing.T) {
	l := mustNew(t, "Films", "Alien", "Brazil", "Casablanca", "Dune")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 2, 3, FirstBetter)
	mustRecord(t, l, 3, 4, FirstBetter)
	mustRecord(t, l, 1, 4, SecondBetter)
	mustRecord(t, l, 4, 2, AboutSame)
	// Every answer about Brazil goes, whatever it was compared with; the
	// rest stay, in order.
	n, err := l.ForgetAnswers([]int{2})
	want := []Comparison{{3, 4, FirstBetter}, {1, 4, SecondBetter}}
	if err != nil || n != 3 || !slices.Equal(l.Comparisons, want) {
		t.Errorf("forgetting Brazil: %d, %v; answers %v, want %v", n, err, l.Comparisons, want)
	}
	if fit, err := l.Fit(); err != nil || fit.Points(2) != 1500 {
		t.Errorf("Brazil rated %v after forgetting its answers (%v)", fit.Points(2), err)
	}
	if n, err := l.ForgetAnswers([]int{1, 9}); err == nil || n != 0 || len(l.Comparisons) != 2 {
		t.Errorf("forgetting a missing entry: %d, %v, %d answers left", n, err, len(l.Comparisons))
	}
	if n, err := l.ForgetAnswers([]int{1, 3, 4}); err != nil || n != 2 || len(l.Comparisons) != 0 {
		t.Errorf("forgetting the rest: %d, %v, %v", n, err, l.Comparisons)
	}
}

func TestMergeEntries(t *testing.T) {
	l := mustNew(t, "Films", "Alien", "Brazil", "Alien (1)", "Dune")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 3, 2, FirstBetter)
	mustRecord(t, l, 1, 3, AboutSame) // between the two Aliens: deleted
	mustRecord(t, l, 4, 3, FirstBetter)
	mustRecord(t, l, 2, 4, SecondBetter)
	l.Focus = []int{3, 4}
	l.IgnoredEntries = []int{3}
	l.IgnoredPairs = [][2]int{{1, 3}, {3, 2}, {1, 2}}

	n, err := l.MergeEntries([]int{3, 1}, " Alien ", "https://example.com/alien", "Sci-fi horror")
	if err != nil || n != 1 {
		t.Fatalf("merging the Aliens: %d, %v", n, err)
	}
	// Entry 1, the older, stays, with the details given; entry 3's answers
	// and the rest become entry 1's.
	wantEntries := []Entry{{ID: 1, Name: "Alien", URL: "https://example.com/alien", Description: "Sci-fi horror"}, {ID: 2, Name: "Brazil"}, {ID: 4, Name: "Dune"}}
	wantAnswers := []Comparison{{1, 2, FirstBetter}, {1, 2, FirstBetter}, {4, 1, FirstBetter}, {2, 4, SecondBetter}}
	if !reflect.DeepEqual(l.Entries, wantEntries) || !slices.Equal(l.Comparisons, wantAnswers) {
		t.Errorf("entries %+v\nanswers %v", l.Entries, l.Comparisons)
	}
	if !slices.Equal(l.Focus, []int{1, 4}) || !slices.Equal(l.IgnoredEntries, []int{1}) || !reflect.DeepEqual(l.IgnoredPairs, [][2]int{{1, 2}}) {
		t.Errorf("focus %v, ignored entries %v, ignored pairs %v", l.Focus, l.IgnoredEntries, l.IgnoredPairs)
	}
	if err := l.validate(); err != nil {
		t.Errorf("merged list: %v", err)
	}

	// Nothing changes if the merge can't be done.
	before := slices.Clone(l.Entries)
	for _, bad := range []struct {
		ids  []int
		name string
	}{{[]int{1}, "Alien"}, {[]int{1, 1}, "Alien"}, {[]int{1, 9}, "Alien"}, {[]int{1, 2}, " "}} {
		if _, err := l.MergeEntries(bad.ids, bad.name, "", ""); err == nil || !reflect.DeepEqual(l.Entries, before) {
			t.Errorf("merging %v as %q: error %v, entries %+v", bad.ids, bad.name, err, l.Entries)
		}
	}

	// The merged entry is removed only if all the entries were.
	l = mustNew(t, "Films", "A", "B", "C", "D")
	l.Entries[0].Removed, l.Entries[1].Removed, l.Entries[2].Removed = true, true, true
	l.MergeEntries([]int{1, 2}, "A", "", "")
	l.MergeEntries([]int{3, 4}, "C", "", "")
	if !l.Entries[0].Removed || l.Entries[1].Removed {
		t.Errorf("entries %+v", l.Entries)
	}
}

func TestImportEntries(t *testing.T) {
	l := mustNew(t, "Films", "Alien", "Brazil (1)", "Casablanca")
	mustRecord(t, l, 1, 2, FirstBetter)
	l.SetQuestion("Which would you rather watch?")
	l.Display.Template = Template{Kind: "owl-newt"}
	display := l.Display

	other := mustNew(t, "Films at home", "alien", "Dune", "Eraserhead (2)", "Casablanca", "Fargo")
	other.Entries[0].URL, other.Entries[0].Description, other.Entries[0].Rating = "https://example.com/alien", "Sci-fi horror", 120
	mustRecord(t, other, 1, 2, SecondBetter)
	mustRecord(t, other, 4, 3, AboutSame)
	other.Entries[4].Removed = true
	other.SetQuestion("Which is better?")
	other.Focus, other.IgnoredEntries, other.IgnoredPairs = []int{2}, []int{3}, [][2]int{{1, 4}}

	// Alien and Casablanca are taken, in any case; the names end in (1)
	// and (2) already, so they get (3).
	renamed, k := l.ImportEntries(other)
	if renamed != 2 || k != 3 {
		t.Errorf("renamed %d with (%d), want 2 with (3)", renamed, k)
	}
	want := []Entry{{ID: 1, Name: "Alien"}, {ID: 2, Name: "Brazil (1)"}, {ID: 3, Name: "Casablanca"},
		{ID: 4, Name: "alien (3)", URL: "https://example.com/alien", Description: "Sci-fi horror"}, {ID: 5, Name: "Dune"},
		{ID: 6, Name: "Eraserhead (2)"}, {ID: 7, Name: "Casablanca (3)"}, {ID: 8, Name: "Fargo", Removed: true}}
	if !reflect.DeepEqual(l.Entries, want) {
		t.Errorf("entries\n%+v\nwant\n%+v", l.Entries, want)
	}
	// The imported answers come first, so the list's own latest answer is
	// still the latest; the rest of the import is left behind.
	if wantAnswers := []Comparison{{4, 5, SecondBetter}, {7, 6, AboutSame}, {1, 2, FirstBetter}}; !slices.Equal(l.Comparisons, wantAnswers) {
		t.Errorf("answers %v, want %v", l.Comparisons, wantAnswers)
	}
	if l.Asks() != "Which would you rather watch?" || !reflect.DeepEqual(l.Display, display) || len(l.Focus)+len(l.IgnoredEntries)+len(l.IgnoredPairs) != 0 {
		t.Errorf("settings now: question %q, display %+v, focus %v, ignores %v %v", l.Asks(), l.Display, l.Focus, l.IgnoredEntries, l.IgnoredPairs)
	}
	if err := l.validate(); err != nil {
		t.Errorf("list after the import: %v", err)
	}

	// With no names taken, nothing is renamed.
	l = mustNew(t, "Films", "Alien")
	if renamed, k := l.ImportEntries(mustNew(t, "More", "Brazil (1)")); renamed != 0 || k != 0 || l.Entries[1].Name != "Brazil (1)" {
		t.Errorf("renamed %d with (%d): %+v", renamed, k, l.Entries)
	}
}

func TestNumbered(t *testing.T) {
	for name, want := range map[string]bool{
		"Alien (1)": true, "Solaris (1972)": true, "Alien (0)": false, "Alien (01)": false,
		"Alien(1)": false, "Alien (1) ": false, "Alien (-1)": false, "(1)": false, "Alien": false,
	} {
		if got := Numbered(name); got != want {
			t.Errorf("Numbered(%q) = %v", name, got)
		}
	}
	for _, c := range []struct {
		names []string
		want  int
	}{
		{nil, 1},
		{[]string{"A (1)", "B (2)", "C (4)", "D (01)", "E (0)"}, 3},
		{[]string{"A (2)"}, 1},
		{[]string{"A (1)", "A (99999999999999999999999)"}, 2},
	} {
		if got := freeNumber(c.names); got != c.want {
			t.Errorf("freeNumber(%q) = %d, want %d", c.names, got, c.want)
		}
	}
}

func TestMergeDefault(t *testing.T) {
	name := func(e Entry) string { return e.Name }
	url := func(e Entry) string { return e.URL }
	tests := []struct {
		about   string
		entries []Entry
		value   func(Entry) string
		want    int
	}{
		{"a name without a number", []Entry{{Name: "Alien (2)"}, {Name: "Alien"}}, name, 1},
		{"a link before none", []Entry{{Name: "Alien"}, {Name: "Alien (2)", URL: "https://b"}}, url, 1},
		{"a link from a name without a number", []Entry{{Name: "Alien (2)", URL: "https://a"}, {Name: "Alien", URL: "https://b"}}, url, 1},
		{"alphabetical", []Entry{{Name: "Alien", URL: "https://b"}, {Name: "Aliens", URL: "https://a"}}, url, 1},
		{"alphabetical, ignoring case first", []Entry{{Name: "b"}, {Name: "A"}, {Name: "a"}}, name, 1},
		{"alphabetical, ignoring case", []Entry{{Name: "a"}, {Name: "B"}}, name, 0},
		{"all empty: the first", []Entry{{Name: "A"}, {Name: "B"}}, url, 0},
		{"names that all end in numbers", []Entry{{Name: "Solaris (2002)"}, {Name: "Solaris (1972)"}}, name, 1},
	}
	for _, tt := range tests {
		if got := MergeDefault(tt.entries, tt.value); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.about, got, tt.want)
		}
	}
}
