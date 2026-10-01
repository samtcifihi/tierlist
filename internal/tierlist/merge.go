package tierlist

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// MergeEntries merges the entries with the given IDs, two or more, into
// one: the one with the lowest ID, which takes the given name, URL and
// description. The others' answers become its answers, apart from those
// between the merged entries, which are deleted, as are pairs ignored
// between them. The merged entry is in focus, or ignored, if any of them
// was, and removed only if all of them were. It returns how many answers
// it deleted.
func (l *List) MergeEntries(ids []int, name, url, description string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("an entry needs a name")
	}
	merged := make(map[int]bool, len(ids))
	removed := true
	for _, id := range ids {
		e, err := l.entry(id)
		if err != nil {
			return 0, err
		}
		merged[id] = true
		removed = removed && e.Removed
	}
	if len(merged) < 2 {
		return 0, errors.New("merging takes two or more entries")
	}
	keep := slices.Min(ids)
	to := func(id int) int {
		if merged[id] {
			return keep
		}
		return id
	}

	n := len(l.Comparisons)
	comparisons := make([]Comparison, 0, n)
	for _, c := range l.Comparisons {
		if c.A, c.B = to(c.A), to(c.B); c.A != c.B {
			comparisons = append(comparisons, c)
		}
	}
	l.Comparisons = comparisons
	var pairs [][2]int
	for _, p := range l.IgnoredPairs {
		a, b := to(p[0]), to(p[1])
		if a != b && !slices.ContainsFunc(pairs, func(q [2]int) bool { return samePair(q, a, b) }) {
			pairs = append(pairs, [2]int{a, b})
		}
	}
	l.IgnoredPairs = pairs
	l.Focus = mapIDs(l.Focus, to)
	l.IgnoredEntries = mapIDs(l.IgnoredEntries, to)

	l.Entries = slices.DeleteFunc(l.Entries, func(e Entry) bool { return merged[e.ID] && e.ID != keep })
	e, _ := l.entry(keep)
	e.Name, e.URL, e.Description, e.Removed = name, strings.TrimSpace(url), strings.TrimSpace(description), removed
	l.fit = nil
	return n - len(l.Comparisons), nil
}

// mapIDs maps each of ids with to, keeping each result once, in order.
func mapIDs(ids []int, to func(int) int) []int {
	var out []int
	for _, id := range ids {
		if id = to(id); !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// ImportEntries adds the entries of other to l with their answers, which
// go before l's own, so that l's latest answer is still the latest. All
// else in other, such as its display options, question, focus, top mode
// and ignores, is left behind. An entry whose name l already has,
// ignoring case, gets " (k)" added to its name, with k the smallest whole
// number from 1 up that no name in either list ends with as " (k)", the
// same for all the entries renamed. It returns how many entries were
// renamed, and k.
func (l *List) ImportEntries(other *List) (renamed, k int) {
	taken := make(map[string]bool, len(l.Entries))
	var names []string
	next := 1
	for _, e := range l.Entries {
		taken[strings.ToLower(e.Name)] = true
		names = append(names, e.Name)
		next = max(next, e.ID+1)
	}
	for _, e := range other.Entries {
		names = append(names, e.Name)
		if taken[strings.ToLower(e.Name)] {
			renamed++
		}
	}
	if renamed > 0 {
		k = freeNumber(names)
	}
	id := make(map[int]int, len(other.Entries))
	for _, e := range other.Entries {
		ne := Entry{ID: next, Name: e.Name, URL: e.URL, Description: e.Description, Removed: e.Removed}
		if taken[strings.ToLower(e.Name)] {
			ne.Name = fmt.Sprintf("%s (%d)", e.Name, k)
		}
		id[e.ID] = next
		next++
		l.Entries = append(l.Entries, ne)
	}
	comparisons := make([]Comparison, 0, len(other.Comparisons)+len(l.Comparisons))
	for _, c := range other.Comparisons {
		comparisons = append(comparisons, Comparison{A: id[c.A], B: id[c.B], Answer: c.Answer})
	}
	l.Comparisons = append(comparisons, l.Comparisons...)
	l.fit = nil
	return renamed, k
}

// numberEnd matches a name that ends in " (k)", as entries renamed on
// import do, with k a whole number from 1 up, written without leading
// zeros.
var numberEnd = regexp.MustCompile(` \(([1-9][0-9]*)\)$`)

// Numbered reports whether name ends in " (k)" for a whole number k from 1
// up, as the names of entries renamed on import do.
func Numbered(name string) bool { return numberEnd.MatchString(name) }

// freeNumber returns the smallest whole number k from 1 up such that none
// of names ends in " (k)".
func freeNumber(names []string) int {
	used := make(map[int]bool)
	for _, name := range names {
		if m := numberEnd.FindStringSubmatch(name); m != nil {
			if k, err := strconv.Atoi(m[1]); err == nil {
				used[k] = true
			}
		}
	}
	k := 1
	for used[k] {
		k++
	}
	return k
}

// MergeDefault returns the index of the entry, among those being merged,
// whose value of one detail, as value gives it, the merged entry keeps
// unless the user chooses another: a value that isn't empty first, then
// one from an entry whose name isn't Numbered, then the first value
// alphabetically, then the first entry given.
func MergeDefault(entries []Entry, value func(Entry) string) int {
	rank := func(e Entry) (bool, bool) { return value(e) == "", Numbered(e.Name) }
	best := 0
	for i := 1; i < len(entries); i++ {
		a, b := entries[i], entries[best]
		aEmpty, aNumbered := rank(a)
		bEmpty, bNumbered := rank(b)
		va, vb := value(a), value(b)
		if c := cmp.Or(compareBools(aEmpty, bEmpty), compareBools(aNumbered, bNumbered),
			strings.Compare(strings.ToLower(va), strings.ToLower(vb)), strings.Compare(va, vb)); c < 0 {
			best = i
		}
	}
	return best
}

// compareBools orders false before true.
func compareBools(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	}
	return 1
}
