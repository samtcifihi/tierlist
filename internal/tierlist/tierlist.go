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
	Name        string       `json:"name"`
	Entries     []Entry      `json:"entries"`
	Comparisons []Comparison `json:"comparisons"`
	// Focus holds the IDs of the entries in focus mode; empty means the
	// default mode. It is saved, so focus mode lasts across sessions.
	Focus   []int   `json:"focus,omitempty"`
	Display Display `json:"display"`
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
// entries (removed ones stay removed), focus mode and display options.
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
	switch {
	case !known[c.A]:
		return fmt.Errorf("there is no entry %d", c.A)
	case !known[c.B]:
		return fmt.Errorf("there is no entry %d", c.B)
	case c.A == c.B:
		return fmt.Errorf("entry %d is compared with itself", c.A)
	case c.Answer != FirstBetter && c.Answer != SecondBetter && c.Answer != AboutSame:
		return fmt.Errorf("unknown answer %q", c.Answer)
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
	if !(l.DrawElo >= 0) || math.IsInf(l.DrawElo, 1) {
		return fmt.Errorf("draw setting %v", l.DrawElo)
	}
	if err := l.Display.Check(); err != nil {
		return fmt.Errorf("display: %w", err)
	}
	return nil
}
