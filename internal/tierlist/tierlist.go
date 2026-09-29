// Package tierlist holds one tier list: its entries, the user's answers,
// focus mode and display options. It saves and loads lists as JSON files,
// and connects them to rating (bayeselo), choosing pairs (pairing) and
// placing entries into tiers (tier).
package tierlist

import (
	"errors"
	"fmt"
	"math"
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
	for i := range l.Entries {
		if l.Entries[i].ID == id {
			l.Entries[i].Name = name
			return nil
		}
	}
	return fmt.Errorf("there is no entry %d", id)
}

// Record adds the answer to a comparison of entries a (shown first) and b
// (shown second).
func (l *List) Record(a, b int, answer Answer) error {
	c := Comparison{A: a, B: b, Answer: answer}
	if err := checkComparison(c, l.ids()); err != nil {
		return err
	}
	l.Comparisons = append(l.Comparisons, c)
	l.fit = nil
	return nil
}

// SetFocus turns on focus mode for the entries with the given IDs, or
// returns to the default mode if there are none.
func (l *List) SetFocus(ids []int) error {
	known := l.ids()
	var focus []int
	seen := make(map[int]bool)
	for _, id := range ids {
		if !known[id] {
			return fmt.Errorf("there is no entry %d", id)
		}
		if !seen[id] {
			seen[id] = true
			focus = append(focus, id)
		}
	}
	l.Focus = focus
	return nil
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
		if !known[id] || seen[id] {
			return fmt.Errorf("focus: entry %d is missing or listed twice", id)
		}
		seen[id] = true
	}
	if !(l.DrawElo >= 0) || math.IsInf(l.DrawElo, 1) {
		return fmt.Errorf("draw setting %v", l.DrawElo)
	}
	if _, err := l.Display.template(); err != nil {
		return fmt.Errorf("display: %w", err)
	}
	if _, err := l.Display.options(); err != nil {
		return fmt.Errorf("display: %w", err)
	}
	return nil
}
