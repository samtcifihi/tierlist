// Package tierlist holds one tier list: its entries, the user's answers,
// focus mode and display options. It saves and loads lists as JSON files,
// and connects them to rating (bayeselo), choosing pairs (pairing) and
// placing entries into tiers (tier).
package tierlist

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

// Answer is the user's answer to a comparison.
type Answer string

const (
	FirstBetter  Answer = "a"    // the entry shown first is better
	SecondBetter Answer = "b"    // the entry shown second is better
	AboutSame    Answer = "same" // they are about the same
)

// An Entry is one item being rated.
type Entry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// URL, if any, is a web page about the entry, and Description says a
	// little about it; the pages show both with the entry's name.
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
	// Removed entries are left out of the tier list, focus mode and new
	// comparisons, but their answers still count toward the other entries'
	// ratings.
	Removed bool `json:"removed,omitempty"`
	// Rating is the entry's rating from the last fit, in Elo. It is saved
	// only to speed up the next fit.
	Rating float64 `json:"rating,omitempty"`
}

// A Comparison is one answer about entries A (shown first) and B (shown
// second), given by ID.
type Comparison struct {
	A      int    `json:"a"`
	B      int    `json:"b"`
	Answer Answer `json:"answer"`
}

// A List is one tier list. Change its entries, comparisons and focus only
// through its methods, which keep its cached fit up to date. A List is not
// safe for concurrent use.
type List struct {
	Name string `json:"name"`
	// Question is what the rating page asks about each pair, such as
	// "Which is funnier?"; empty means DefaultQuestion.
	Question    string       `json:"question,omitempty"`
	Entries     []Entry      `json:"entries"`
	Comparisons []Comparison `json:"comparisons"`
	// Focus holds the IDs of the entries in focus mode; empty means the
	// default mode. It is saved, so focus mode lasts across sessions.
	Focus []int `json:"focus,omitempty"`
	// Top, above 0, turns on top mode: pairs favour the entries in the
	// top Top percent of the list and include only entries in the top
	// 2·Top percent (see TopSets). It is saved too, and never on together
	// with focus mode.
	Top float64 `json:"top,omitempty"`
	// IgnoredEntries and IgnoredPairs, by ID, are left out of the pairs
	// asked until the ignores are reset. Their answers still count, and
	// ignored entries stay in the tier list.
	IgnoredEntries []int    `json:"ignoredEntries,omitempty"`
	IgnoredPairs   [][2]int `json:"ignoredPairs,omitempty"`
	Display        Display  `json:"display"`
	// DrawElo is the draw setting from the last fit, in Elo. It is saved
	// only to speed up the next fit.
	DrawElo float64 `json:"drawElo,omitempty"`

	fit *Fit // nil when out of date
}

// New returns an empty list called name, with the default display options.
func New(name string) (*List, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("a list needs a name")
	}
	return &List{Name: name, Entries: []Entry{}, Comparisons: []Comparison{}, Display: DefaultDisplay()}, nil
}

// DefaultQuestion is what the rating page asks about each pair of a list
// that has no question of its own.
const DefaultQuestion = "Which is better?"

// Asks returns what the rating page asks about each pair: the list's own
// question, or DefaultQuestion.
func (l *List) Asks() string {
	if l.Question == "" {
		return DefaultQuestion
	}
	return l.Question
}

// SetQuestion sets what the rating page asks about each pair; an empty
// question means DefaultQuestion.
func (l *List) SetQuestion(q string) {
	l.Question = strings.TrimSpace(q)
}

// SetName renames the list.
func (l *List) SetName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a list needs a name")
	}
	l.Name = name
	return nil
}

// AddEntry adds an entry called name and returns its ID.
func (l *List) AddEntry(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("an entry needs a name")
	}
	id := 1
	for _, e := range l.Entries {
		id = max(id, e.ID+1)
	}
	l.Entries = append(l.Entries, Entry{ID: id, Name: name})
	l.fit = nil
	return id, nil
}

// RenameEntry renames the entry with ID id.
func (l *List) RenameEntry(id int, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("an entry needs a name")
	}
	e, err := l.entry(id)
	if err != nil {
		return err
	}
	e.Name = name
	return nil
}

// SetEntryDetails sets the URL and description of the entry with ID id;
// empty ones mean none.
func (l *List) SetEntryDetails(id int, url, description string) error {
	e, err := l.entry(id)
	if err != nil {
		return err
	}
	e.URL, e.Description = strings.TrimSpace(url), strings.TrimSpace(description)
	return nil
}

// RemoveEntry removes the entry with ID id from the tier list, focus mode
// and new comparisons. Its answers still count toward the other entries'
// ratings, and RestoreEntry brings it back.
func (l *List) RemoveEntry(id int) error {
	e, err := l.entry(id)
	if err != nil {
		return err
	}
	e.Removed = true
	if l.Focus = slices.DeleteFunc(l.Focus, func(f int) bool { return f == id }); len(l.Focus) == 0 {
		l.Focus = nil
	}
	return nil
}

// RestoreEntry brings back a removed entry.
func (l *List) RestoreEntry(id int) error {
	e, err := l.entry(id)
	if err != nil {
		return err
	}
	e.Removed = false
	return nil
}

// Shown returns the entries that have not been removed, in list order.
func (l *List) Shown() []Entry {
	var shown []Entry
	for _, e := range l.Entries {
		if !e.Removed {
			shown = append(shown, e)
		}
	}
	return shown
}

// Record adds the answer to a comparison of entries a (shown first) and b
// (shown second), neither of which may have been removed.
func (l *List) Record(a, b int, answer Answer) error {
	c := Comparison{A: a, B: b, Answer: answer}
	if err := checkComparison(c, l.ids()); err != nil {
		return err
	}
	for _, id := range []int{a, b} {
		if e, _ := l.entry(id); e.Removed {
			return fmt.Errorf("entry %d has been removed", id)
		}
	}
	l.Comparisons = append(l.Comparisons, c)
	l.fit = nil
	return nil
}

// Undo takes back the most recent answer and returns it. It reports false
// if there are no answers.
func (l *List) Undo() (Comparison, bool) {
	k := len(l.Comparisons)
	if k == 0 {
		return Comparison{}, false
	}
	c := l.Comparisons[k-1]
	l.Comparisons = l.Comparisons[:k-1]
	l.fit = nil
	return c, true
}

// Reset deletes every answer, so that rating starts over, keeping the
// entries (removed ones stay removed), ignores, focus mode and display
// options.
func (l *List) Reset() {
	l.Comparisons = []Comparison{}
	for i := range l.Entries {
		l.Entries[i].Rating = 0
	}
	l.DrawElo = 0
	l.fit = nil
}

// Counts returns how many answers involve each entry, by ID.
func (l *List) Counts() map[int]int {
	counts := make(map[int]int, len(l.Entries))
	for _, c := range l.Comparisons {
		counts[c.A]++
		counts[c.B]++
	}
	return counts
}

// SetFocus turns on focus mode for the entries with the given IDs, or
// returns to the default mode if there are none. Removed entries cannot be
// focused on.
func (l *List) SetFocus(ids []int) error {
	if len(ids) > 0 {
		l.Top = 0
	}
	var focus []int
	for _, id := range ids {
		e, err := l.entry(id)
		if err != nil {
			return err
		}
		if e.Removed {
			return fmt.Errorf("entry %d has been removed", id)
		}
		if !slices.Contains(focus, id) {
			focus = append(focus, id)
		}
	}
	l.Focus = focus
	return nil
}

// SetTop turns on top mode for the top p percent of the list, turning off
// focus mode, or turns it off with p = 0.
func (l *List) SetTop(p float64) error {
	if !(p >= 0 && p <= 100) {
		return fmt.Errorf("top mode needs a percentage above 0 and at most 100, not %v", p)
	}
	if p > 0 {
		l.Focus = nil
	}
	l.Top = p
	return nil
}

// IgnoreEntry leaves the entry with ID id out of the pairs asked until
// ResetIgnores.
func (l *List) IgnoreEntry(id int) error {
	if _, err := l.entry(id); err != nil {
		return err
	}
	if !l.EntryIgnored(id) {
		l.IgnoredEntries = append(l.IgnoredEntries, id)
	}
	return nil
}

// IgnorePair leaves the pair of entries a and b, either way round, out of
// the pairs asked until ResetIgnores.
func (l *List) IgnorePair(a, b int) error {
	if err := checkPair(a, b, l.ids()); err != nil {
		return err
	}
	if !l.PairIgnored(a, b) {
		l.IgnoredPairs = append(l.IgnoredPairs, [2]int{a, b})
	}
	return nil
}

// UnignoreEntry takes back IgnoreEntry(id).
func (l *List) UnignoreEntry(id int) {
	l.IgnoredEntries = slices.DeleteFunc(l.IgnoredEntries, func(x int) bool { return x == id })
}

// UnignorePair takes back IgnorePair(a, b), given either way round.
func (l *List) UnignorePair(a, b int) {
	l.IgnoredPairs = slices.DeleteFunc(l.IgnoredPairs, func(p [2]int) bool { return samePair(p, a, b) })
}

// ResetIgnores lets every entry and pair be asked about again.
func (l *List) ResetIgnores() {
	l.IgnoredEntries, l.IgnoredPairs = nil, nil
}

// EntryIgnored reports whether the entry with ID id is ignored.
func (l *List) EntryIgnored(id int) bool { return slices.Contains(l.IgnoredEntries, id) }

// PairIgnored reports whether the pair of entries a and b, either way
// round, is ignored as a pair.
func (l *List) PairIgnored(a, b int) bool {
	return slices.ContainsFunc(l.IgnoredPairs, func(p [2]int) bool { return samePair(p, a, b) })
}

// CanAsk reports whether entries a and b can be asked about now: they are
// different entries of the list, neither is removed or ignored, the pair
// isn't ignored, in focus mode one of them is in focus, and in top mode
// top mode allows both.
func (l *List) CanAsk(a, b int) bool {
	ea, errA := l.entry(a)
	eb, errB := l.entry(b)
	ok := errA == nil && errB == nil && a != b && !ea.Removed && !eb.Removed &&
		!l.EntryIgnored(a) && !l.EntryIgnored(b) && !l.PairIgnored(a, b) &&
		(len(l.Focus) == 0 || slices.Contains(l.Focus, a) || slices.Contains(l.Focus, b))
	if ok && l.Top > 0 {
		_, allowed, err := l.TopSets()
		ok = err == nil && allowed[a] && allowed[b]
	}
	return ok
}

func samePair(p [2]int, a, b int) bool { return p == [2]int{a, b} || p == [2]int{b, a} }

// entry returns the entry with ID id.
func (l *List) entry(id int) (*Entry, error) {
	for i := range l.Entries {
		if l.Entries[i].ID == id {
			return &l.Entries[i], nil
		}
	}
	return nil, fmt.Errorf("there is no entry %d", id)
}

// ids returns the set of entry IDs.
func (l *List) ids() map[int]bool {
	known := make(map[int]bool, len(l.Entries))
	for _, e := range l.Entries {
		known[e.ID] = true
	}
	return known
}

func checkComparison(c Comparison, known map[int]bool) error {
	if err := checkPair(c.A, c.B, known); err != nil {
		return err
	}
	if c.Answer != FirstBetter && c.Answer != SecondBetter && c.Answer != AboutSame {
		return fmt.Errorf("unknown answer %q", c.Answer)
	}
	return nil
}

// checkPair checks that a and b are two different known entries.
func checkPair(a, b int, known map[int]bool) error {
	switch {
	case !known[a]:
		return fmt.Errorf("there is no entry %d", a)
	case !known[b]:
		return fmt.Errorf("there is no entry %d", b)
	case a == b:
		return fmt.Errorf("entry %d is compared with itself", a)
	}
	return nil
}

// validate checks everything a saved list must satisfy.
func (l *List) validate() error {
	if strings.TrimSpace(l.Name) == "" {
		return errors.New("the list has no name")
	}
	known := make(map[int]bool, len(l.Entries))
	for k, e := range l.Entries {
		switch {
		case e.ID < 1:
			return fmt.Errorf("entry %d has ID %d; IDs start at 1", k+1, e.ID)
		case known[e.ID]:
			return fmt.Errorf("two entries have ID %d", e.ID)
		case strings.TrimSpace(e.Name) == "":
			return fmt.Errorf("entry %d has no name", e.ID)
		case math.IsNaN(e.Rating) || math.IsInf(e.Rating, 0):
			return fmt.Errorf("entry %d has rating %v", e.ID, e.Rating)
		}
		known[e.ID] = true
	}
	for k, c := range l.Comparisons {
		if err := checkComparison(c, known); err != nil {
			return fmt.Errorf("comparison %d: %w", k+1, err)
		}
	}
	seen := make(map[int]bool)
	for _, id := range l.Focus {
		if e, err := l.entry(id); err != nil || e.Removed || seen[id] {
			return fmt.Errorf("focus: entry %d is missing, removed or listed twice", id)
		}
		seen[id] = true
	}
	for _, id := range l.IgnoredEntries {
		if !known[id] {
			return fmt.Errorf("ignored: there is no entry %d", id)
		}
	}
	for _, p := range l.IgnoredPairs {
		if err := checkPair(p[0], p[1], known); err != nil {
			return fmt.Errorf("ignored pair: %w", err)
		}
	}
	switch {
	case !(l.Top >= 0 && l.Top <= 100):
		return fmt.Errorf("top mode: %v percent", l.Top)
	case l.Top > 0 && len(l.Focus) > 0:
		return errors.New("focus mode and top mode are both on")
	}
	if !(l.DrawElo >= 0) || math.IsInf(l.DrawElo, 1) {
		return fmt.Errorf("draw setting %v", l.DrawElo)
	}
	if err := l.Display.Check(); err != nil {
		return fmt.Errorf("display: %w", err)
	}
	return nil
}
