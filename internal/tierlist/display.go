package tierlist

import (
	"fmt"
	"math/big"

	"github.com/samtcifihi/tierlist/internal/tier"
)

// Display holds a list's display options (see the README's Display
// section).
type Display struct {
	Template Template `json:"template"`
	// Convention is "top-closed" or "bottom-closed".
	Convention string `json:"convention"`
	// DrawMargin groups entries whose ratings differ by at most this many
	// Elo.
	DrawMargin float64 `json:"drawMargin"`
	// GroupRule is "middle-entry" or "alternate".
	GroupRule string `json:"groupRule"`
	// Prefer is "higher" or "lower".
	Prefer string `json:"prefer"`
}

// Template chooses the tier template.
type Template struct {
	// Kind is "stars", "hogwarts" or "custom".
	Kind string `json:"kind"`

	// For stars; see tier.StarOptions.
	MaxStars  int  `json:"maxStars,omitempty"`
	SkipZero  bool `json:"skipZero,omitempty"`
	Divisions int  `json:"divisions,omitempty"`

	// For a custom template: its name, its tier names (best first) and its
	// cut-offs (increasing) as the user wrote them.
	Name    string   `json:"name,omitempty"`
	Tiers   []string `json:"tiers,omitempty"`
	Cutoffs []string `json:"cutoffs,omitempty"`
}

// DefaultDisplay returns the display options of a new list: 0–5 stars with
// the top tier closed, a draw-margin of 0, the middle-entry rule, and the
// higher tier when a group's middle entries fall in different tiers.
func DefaultDisplay() Display {
	return Display{
		Template:   Template{Kind: "stars", MaxStars: 5},
		Convention: "top-closed",
		GroupRule:  "middle-entry",
		Prefer:     "higher",
	}
}

// Check reports whether the options describe a usable template and
// placement.
func (d Display) Check() error {
	if _, err := d.template(); err != nil {
		return err
	}
	_, err := d.options()
	return err
}

// template builds the tier template the options describe.
func (d Display) template() (tier.Template, error) {
	var c tier.Convention
	switch d.Convention {
	case "top-closed":
		c = tier.TopClosed
	case "bottom-closed":
		c = tier.BottomClosed
	default:
		return tier.Template{}, fmt.Errorf("unknown interval convention %q", d.Convention)
	}
	t := d.Template
	switch t.Kind {
	case "stars":
		return tier.Stars(tier.StarOptions{Max: t.MaxStars, SkipZero: t.SkipZero, Divisions: t.Divisions}, c)
	case "hogwarts":
		return tier.Hogwarts(c)
	case "custom":
		cutoffs := make([]*big.Rat, len(t.Cutoffs))
		for i, s := range t.Cutoffs {
			r, err := tier.ParseCutoff(s)
			if err != nil {
				return tier.Template{}, err
			}
			cutoffs[i] = r
		}
		return tier.New(t.Name, t.Tiers, cutoffs, c)
	}
	return tier.Template{}, fmt.Errorf("unknown template kind %q", t.Kind)
}

// options returns the placement options.
func (d Display) options() (tier.Options, error) {
	o := tier.Options{DrawMargin: d.DrawMargin}
	switch d.GroupRule {
	case "middle-entry":
		o.Rule = tier.MiddleEntry
	case "alternate":
		o.Rule = tier.Alternate
	default:
		return o, fmt.Errorf("unknown group rule %q", d.GroupRule)
	}
	switch d.Prefer {
	case "higher":
		o.Prefer = tier.Higher
	case "lower":
		o.Prefer = tier.Lower
	default:
		return o, fmt.Errorf("unknown preference %q", d.Prefer)
	}
	if !(o.DrawMargin >= 0) {
		return o, fmt.Errorf("draw-margin %v is below 0", o.DrawMargin)
	}
	return o, nil
}
