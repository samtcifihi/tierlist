package tierlist

import (
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/samtcifihi/tierlist/internal/bayeselo"
	"github.com/samtcifihi/tierlist/internal/tier"
)

// MinDrawMargin is the smallest draw-margin used when placing entries, in
// Elo: ten times the precision of the ratings, so that entries whose
// ratings are equal in theory always group, whichever way rounding tips
// them.
const MinDrawMargin = 10 * bayeselo.Precision

// Display holds a list's display options (see the README's Display
// section).
type Display struct {
	Template Template `json:"template"`
	// Convention is "top-closed" or "bottom-closed".
	Convention string `json:"convention"`
	// DrawMargin groups entries whose ratings differ by at most this many
	// points, as the pages show ratings (see Fit.Points).
	DrawMargin float64 `json:"drawMargin"`
	// GroupRule is "middle-entry" or "alternate".
	GroupRule string `json:"groupRule"`
	// Prefer is "higher" or "lower".
	Prefer string `json:"prefer"`
}

// Template chooses the tier template.
type Template struct {
	// Kind is "stars", "owl-newt" or "custom".
	Kind string `json:"kind"`

	// For stars; see tier.StarOptions.
	MaxStars  int  `json:"maxStars,omitempty"`
	SkipZero  bool `json:"skipZero,omitempty"`
	Divisions int  `json:"divisions,omitempty"`
	// Sizes is how big the star tiers are: "" for even tiers, "geometric"
	// or "beta" (see tier.Sizes), with the numbers as the user wrote them,
	// as decimals or fractions.
	Sizes  string `json:"sizes,omitempty"`
	Factor string `json:"factor,omitempty"` // geometric: each tier's size over the one before's
	From   string `json:"from,omitempty"`   // geometric: counting from the "best" (default) or "worst" tier
	Alpha  string `json:"alpha,omitempty"`  // beta
	Beta   string `json:"beta,omitempty"`   // beta

	// For a custom template: its name, its tier names (best first) and its
	// cut-offs (increasing) as the user wrote them.
	Name    string   `json:"name,omitempty"`
	Tiers   []string `json:"tiers,omitempty"`
	Cutoffs []string `json:"cutoffs,omitempty"`
}

// DefaultDisplay returns the display options of a new list: 0–10 stars with
// the top tier closed, a draw-margin of 0, the middle-entry rule, and the
// higher tier when a group's middle entries fall in different tiers.
func DefaultDisplay() Display {
	return Display{
		Template:   Template{Kind: "stars", MaxStars: 10},
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

// A Shape is how a display's tier template shares out [0, 1] among its
// tiers, for drawing: cut [0, 1] into one equal part per tier, worst tier
// first, and each tier's share is the area over its part.
type Shape struct {
	// Tiers and Shares hold the tier names and the share of [0, 1] each
	// tier covers, worst tier first.
	Tiers  []string
	Shares []float64
	// Alpha and Beta, with Beta tier sizes, are the parameters of the Beta
	// distribution whose density the shares come from; otherwise both
	// are 0.
	Alpha, Beta float64
}

// Shape returns the shape of the tier template the options describe.
func (d Display) Shape() (Shape, error) {
	t, err := d.template()
	if err != nil {
		return Shape{}, err
	}
	s := Shape{Tiers: slices.Clone(t.Tiers), Shares: t.Shares()}
	slices.Reverse(s.Tiers)
	if d.Template.Kind == "stars" && d.Template.Sizes == "beta" {
		sizes, err := d.Template.sizes()
		if err != nil {
			return Shape{}, err
		}
		s.Alpha, s.Beta = sizes.Alpha, sizes.Beta
	}
	return s, nil
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
		sizes, err := t.sizes()
		if err != nil {
			return tier.Template{}, err
		}
		return tier.Stars(tier.StarOptions{Max: t.MaxStars, SkipZero: t.SkipZero, Divisions: t.Divisions, Sizes: sizes}, c)
	case "owl-newt":
		return tier.OWLNEWT(c)
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

// sizes reads how big the star tiers are.
func (t Template) sizes() (tier.Sizes, error) {
	switch t.Sizes {
	case "":
		return tier.Sizes{}, nil
	case "geometric":
		f, err := number(t.Factor, "the factor between tier sizes")
		if err != nil {
			return tier.Sizes{}, err
		}
		if t.From != "" && t.From != "best" && t.From != "worst" {
			return tier.Sizes{}, fmt.Errorf("geometric tiers count from the best or the worst tier, not %q", t.From)
		}
		return tier.Sizes{Kind: tier.GeometricTiers, Factor: f, FromWorst: t.From == "worst"}, nil
	case "beta":
		a, err := number(t.Alpha, "α")
		if err != nil {
			return tier.Sizes{}, err
		}
		b, err := number(t.Beta, "β")
		if err != nil {
			return tier.Sizes{}, err
		}
		return tier.Sizes{Kind: tier.BetaTiers, Alpha: a, Beta: b}, nil
	}
	return tier.Sizes{}, fmt.Errorf("unknown tier sizes %q", t.Sizes)
}

// number reads a number written as a decimal or a fraction, such as 1.618
// or 1/2.
func number(s, what string) (float64, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
	if !ok {
		return 0, fmt.Errorf("%s must be a number, such as 1.5 or 3/2, not %q", what, s)
	}
	f, _ := r.Float64()
	return f, nil
}

// options returns the placement options, with the draw-margin still in
// shown points.
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
