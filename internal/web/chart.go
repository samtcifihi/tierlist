package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/samtcifihi/tierlist/internal/tier"
	"github.com/samtcifihi/tierlist/internal/tierlist"
)

// The tier sizes chart draws a tierlist.Shape as a density on [0, 1] (see
// the README's Tier sizes chart section). Its SVG coordinates run from 0
// to chartSize across and down, stretched to the chart's box.
const (
	chartSize = 1000.0
	// curveSamples is how many evenly spaced points draw a smooth density.
	curveSamples = 400
	// Past these many tiers, the lines between them and the areas that
	// name a tier on hover would only clutter the chart.
	maxDividers   = 60
	maxHoverParts = 200
)

type chartView struct {
	Top      string   // the density at the top edge
	OneY     string   // SVG y of density 1, the height of tiers all the same size
	OneAt    string   // the same as a percentage of the height, or "" to leave it unlabelled
	Dividers []string // SVG x of the lines between tiers
	Parts    []chartPart
	Area     string // SVG path of the area under the density
	Line     string // SVG path of the density
	Caption  string
	Preview  bool // the chart shows options that are not applied yet
}

// A chartPart is a tier's equal part of [0, 1], which names the tier and
// its share on hover.
type chartPart struct {
	X, Width string
	Title    string
}

// shapeChart lays out the chart of s. Star tiers get a ★ after their
// names.
func shapeChart(s tierlist.Shape, stars bool) chartView {
	n := len(s.Shares)
	// A step for each tier: the area over its part is its share.
	var xs, ys []float64
	peak := 1.0
	for k, share := range s.Shares {
		h := float64(n) * share
		xs = append(xs, float64(k)/float64(n), float64(k+1)/float64(n))
		ys = append(ys, h, h)
		peak = max(peak, h)
	}
	curve := s.Alpha > 0
	if curve {
		xs = curveXs(s.Alpha, s.Beta)
		ys = make([]float64, len(xs))
		for i, x := range xs {
			ys[i] = tier.BetaPDF(x, s.Alpha, s.Beta)
			// Next to an end where the density has no bound, it runs off
			// the top of the chart rather than flattening the rest.
			if (x >= 0.01 || s.Alpha >= 1) && (x <= 0.99 || s.Beta >= 1) {
				peak = max(peak, ys[i])
			}
		}
	}
	top := niceAbove(1.08 * peak)
	y := func(v float64) float64 {
		if math.IsNaN(v) {
			v = 0
		}
		return chartSize * (1 - min(v, 1.05*top)/top)
	}
	var line, area strings.Builder
	fmt.Fprintf(&area, "M%s %sL", coord(chartSize*xs[0]), coord(chartSize))
	for i, x := range xs {
		p := coord(chartSize*x) + " " + coord(y(ys[i]))
		if i == 0 {
			line.WriteString("M" + p)
		} else {
			line.WriteString("L" + p)
		}
		area.WriteString(p + " ")
	}
	fmt.Fprintf(&area, "%s %sZ", coord(chartSize*xs[len(xs)-1]), coord(chartSize))

	v := chartView{Top: strconv.FormatFloat(top, 'f', -1, 64), OneY: coord(y(1)), Line: line.String(), Area: area.String()}
	if at := 100 / top; at >= 12 {
		v.OneAt = strconv.FormatFloat(at, 'f', 1, 64)
	}
	if n <= maxDividers {
		for k := 1; k < n; k++ {
			v.Dividers = append(v.Dividers, coord(chartSize*float64(k)/float64(n)))
		}
	}
	if n <= maxHoverParts {
		for k, share := range s.Shares {
			name := s.Tiers[k]
			if stars {
				name += "★"
			}
			v.Parts = append(v.Parts, chartPart{X: coord(chartSize * float64(k) / float64(n)), Width: coord(chartSize / float64(n)),
				Title: fmt.Sprintf("%s: %.3g%% of the list", name, 100*share)})
		}
	}
	v.Caption = "Each tier gets an equal slice of [0, 1], worst on the left, and the area above the slice is the tier's share of the list, so the total area is 1. The dashed line is the height of tiers all the same size."
	if curve {
		v.Caption = fmt.Sprintf("The Beta(%s, %s) density. ", strconv.FormatFloat(s.Alpha, 'g', -1, 64), strconv.FormatFloat(s.Beta, 'g', -1, 64)) + v.Caption
	}
	return v
}

// curveXs returns where to draw a Beta(a, b) density: evenly across
// (0, 1), and closely around its peak, which can be too narrow for the
// even points to catch.
func curveXs(a, b float64) []float64 {
	xs := make([]float64, 0, curveSamples+200)
	for i := 0; i <= curveSamples; i++ {
		xs = append(xs, min(max(float64(i)/curveSamples, 1e-9), 1-1e-9))
	}
	if a > 1 && b > 1 {
		mode := (a - 1) / (a + b - 2)
		sd := math.Sqrt(a * b / ((a + b) * (a + b) * (a + b + 1)))
		for i := -80; i <= 80; i++ {
			if x := mode + float64(i)/10*sd; x > 0 && x < 1 {
				xs = append(xs, x)
			}
		}
	}
	slices.Sort(xs)
	return slices.Compact(xs)
}

// niceAbove returns the smallest of 1, 1.25, 1.5, 2, 2.5, 3, 4, 5, 6 and 8
// times a power of ten that is at least v, for the top of the chart.
func niceAbove(v float64) float64 {
	if !(v > 0) || math.IsInf(v, 1) {
		return 1.25
	}
	e := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 1.25, 1.5, 2, 2.5, 3, 4, 5, 6, 8} {
		if m*e >= v {
			return m * e
		}
	}
	return 10 * e
}

// coord writes an SVG coordinate to a tenth of a unit.
func coord(v float64) string {
	return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
}

// chart renders the tier sizes chart for the display options in the query,
// for the tier list page to show while they are being changed. Options
// that don't make a template get a 422 and the reason, as plain text.
func (s *Server) chart(w http.ResponseWriter, r *http.Request, ol *openList) {
	f := readDisplayForm(r)
	t, err := f.template()
	var shape tierlist.Shape
	if err == nil {
		shape, err = tierlist.Display{Template: t, Convention: f.Convention}.Shape()
	}
	if err != nil {
		http.Error(w, sentence(err.Error()), http.StatusUnprocessableEntity)
		return
	}
	v := shapeChart(shape, t.Kind == "stars")
	v.Preview = !sameTemplate(t, ol.list.Display.Template)
	var b bytes.Buffer
	if err := s.pages["tiers"].ExecuteTemplate(&b, "chart", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	b.WriteTo(w)
}

// sameTemplate reports whether a and b are the same template choice, as
// they would be saved.
func sameTemplate(a, b tierlist.Template) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}
