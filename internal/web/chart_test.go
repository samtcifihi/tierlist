package web

import (
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/samtcifihi/tierlist/internal/tierlist"
)

func TestNiceAbove(t *testing.T) {
	for _, c := range [][2]float64{
		{1.08, 1.25}, {1.62, 2}, {3.455, 4}, {54.5, 60}, {10, 10}, {9.9, 10}, {1000, 1000},
		{0.5, 0.5}, {7.1, 8}, {8.5, 10}, {1219, 1250}, {math.NaN(), 1.25}, {math.Inf(1), 1.25},
	} {
		if got := niceAbove(c[0]); got != c[1] {
			t.Errorf("niceAbove(%v) = %v, want %v", c[0], got, c[1])
		}
	}
}

func mustShape(t *testing.T, tmpl tierlist.Template) tierlist.Shape {
	t.Helper()
	d := tierlist.DefaultDisplay()
	d.Template = tmpl
	s, err := d.Shape()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestShapeChart(t *testing.T) {
	// 0–10 stars, nearest star: steps of 11 × 1/20 = 0.55 at the ends
	// and 11 × 1/10 = 1.1 between, under a top of 1.25.
	v := shapeChart(mustShape(t, tierlist.Template{Kind: "stars", MaxStars: 10}), true)
	if v.Top != "1.25" || v.OneY != "200" || v.OneAt != "80.0" || len(v.Dividers) != 10 || v.Dividers[0] != "90.9" || len(v.Parts) != 11 {
		t.Errorf("nearest star chart: %+v", v)
	}
	if !strings.HasPrefix(v.Line, "M0 560L90.9 560L90.9 120L181.8 120L181.8 120") || !strings.HasSuffix(v.Line, "L909.1 560L1000 560") ||
		!strings.HasPrefix(v.Area, "M0 1000L0 560 90.9 560 ") || !strings.HasSuffix(v.Area, "1000 560 1000 1000Z") {
		t.Errorf("nearest star paths:\n%s\n%s", v.Line, v.Area)
	}
	if v.Parts[0].Title != "0★: 5% of the list" || v.Parts[10].Title != "10★: 5% of the list" || v.Parts[1].X != "90.9" || v.Parts[1].Width != "90.9" ||
		!strings.HasPrefix(v.Caption, "Each tier gets an equal slice") {
		t.Errorf("nearest star parts %+v, caption %q", v.Parts, v.Caption)
	}

	// Beta(2, 2) is smooth, peaking at 1.5 in the middle, under a top of 2.
	v = shapeChart(mustShape(t, tierlist.Template{Kind: "stars", MaxStars: 10, Sizes: "beta", Alpha: "2", Beta: "2"}), true)
	if v.Top != "2" || !strings.Contains(v.Line, "L500 250L") || strings.Count(v.Line, "L") < curveSamples ||
		!strings.HasPrefix(v.Caption, "The Beta(2, 2) density. Each tier") || len(v.Parts) != 11 {
		t.Errorf("Beta(2, 2) chart: top %s, caption %q, line %.200s", v.Top, v.Caption, v.Line)
	}

	// Beta(1/2, 1/2) has no bound at either end. The top comes from the
	// density at 0.01 and 0.99, about 3.2, and the curve runs off it.
	v = shapeChart(mustShape(t, tierlist.Template{Kind: "stars", MaxStars: 10, Sizes: "beta", Alpha: "1/2", Beta: "1/2"}), true)
	if v.Top != "4" || !strings.HasPrefix(v.Line, "M0 -50L") || !strings.HasSuffix(v.Line, "L1000 -50") {
		t.Errorf("Beta(1/2, 1/2) chart: top %s, line %.60s...%s", v.Top, v.Line, v.Line[len(v.Line)-30:])
	}

	// A narrow peak still gets drawn: Beta(2000, 2000) peaks at about 50.5.
	v = shapeChart(mustShape(t, tierlist.Template{Kind: "stars", MaxStars: 10, Sizes: "beta", Alpha: "2000", Beta: "2000"}), true)
	if v.Top != "60" || v.OneAt != "" || !strings.Contains(v.Line, "L500 159") {
		t.Errorf("Beta(2000, 2000) chart: top %s, one at %q, line has no peak near 159", v.Top, v.OneAt)
	}

	// Beta(100000, 200000) peaks at about 464, near 1/3 but between the
	// evenly spaced points, which only reach about 290. The points around
	// the peak catch it, for a top of 600 rather than 400.
	v = shapeChart(mustShape(t, tierlist.Template{Kind: "stars", MaxStars: 10, Sizes: "beta", Alpha: "100000", Beta: "200000"}), true)
	if v.Top != "600" {
		t.Errorf("Beta(100000, 200000) chart: top %s, want 600", v.Top)
	}

	// OWL/NEWT: Troll holds 16/31 of the list, 6 × 16/31 ≈ 3.1 high.
	v = shapeChart(mustShape(t, tierlist.Template{Kind: "owl-newt"}), false)
	titles := []string{}
	for _, p := range v.Parts {
		titles = append(titles, p.Title)
	}
	if v.Top != "4" || !strings.HasPrefix(v.Line, "M0 225.8L166.7 225.8") || !slices.Equal(titles, []string{"Troll: 51.6% of the list",
		"Dreadful: 16.1% of the list", "Poor: 12.9% of the list", "Acceptable: 9.68% of the list",
		"Exceeds Expectations: 6.45% of the list", "Outstanding: 3.23% of the list"}) {
		t.Errorf("OWL/NEWT chart: top %s, line %.40s, titles %q", v.Top, v.Line, titles)
	}

	// Lots of tiers leave out the lines between them and the hover parts.
	v = shapeChart(mustShape(t, tierlist.Template{Kind: "stars", MaxStars: 100, Divisions: 4}), true)
	if len(v.Dividers) != 0 || len(v.Parts) != 0 || strings.Count(v.Line, "L") != 2*401-1 {
		t.Errorf("401 tiers: %d dividers, %d parts, %d line points", len(v.Dividers), len(v.Parts), strings.Count(v.Line, "L")+1)
	}
}

func TestChartPage(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Letters", "A", "B", "C")
	// The tier list page draws the saved template, and says where to fetch
	// the chart for other options while they are being changed.
	_, body := c.get(base + "/tiers")
	if !strings.Contains(body, `data-chart="/lists/letters/chart"`) || !strings.Contains(body, `<fieldset class="chart" id="chart">`) ||
		!strings.Contains(body, `<span style="bottom: 100%">1.25</span>`) || !strings.Contains(body, `<title>10★: 5% of the list</title>`) ||
		strings.Contains(body, "Not applied yet") {
		t.Errorf("tier list page chart:\n%s", body)
	}
	chart := func(form url.Values) (int, string) {
		t.Helper()
		q := mergeForm(url.Values{"kind": {"stars"}, "maxStars": {"10"}, "sizes": {"even"}})
		for k, v := range form {
			q[k] = v
		}
		req, _ := http.NewRequest("GET", c.srv.URL+base+"/chart?"+q.Encode(), nil)
		status, _, body := c.do(req)
		return status, body
	}
	// The saved options draw the same chart, as applied.
	status, body := chart(nil)
	if status != http.StatusOK || !strings.HasPrefix(body, "\n<fieldset class=\"chart\" id=\"chart\">") ||
		!strings.Contains(body, `<span style="bottom: 100%">1.25</span>`) || strings.Contains(body, "Not applied yet") || strings.Contains(body, "<html") {
		t.Errorf("chart of the saved options: %d\n%s", status, body)
	}
	// Other options are drawn as a preview.
	status, body = chart(url.Values{"sizes": {"beta"}, "alpha": {"1/2"}, "beta": {"0.5"}})
	if status != http.StatusOK || !strings.Contains(body, "<strong>Not applied yet:</strong>") || !strings.Contains(body, "The Beta(0.5, 0.5) density.") {
		t.Errorf("chart of Beta(1/2, 1/2): %d\n%s", status, body)
	}
	status, body = chart(url.Values{"kind": {"custom"}, "tierNames": {"Top\nRest"}, "customCutoffs": {"0.9"}})
	if status != http.StatusOK || !strings.Contains(body, "<title>Rest: 90% of the list</title>") || !strings.Contains(body, "Not applied yet") {
		t.Errorf("chart of a custom template: %d\n%s", status, body)
	}
	// Options that make no template get the reason, and nothing is saved.
	for _, bad := range []url.Values{
		{"sizes": {"beta"}, "alpha": {"0"}, "beta": {"2"}},
		{"sizes": {"beta"}, "alpha": {"1/"}, "beta": {"2"}},
		{"maxStars": {"2"}},
		{"kind": {"custom"}},
		{"kind": {"custom"}, "tierNames": {"A\nB"}, "customCutoffs": {"2"}},
		{"sizes": {"geometric"}, "factor": {"-1"}},
		{"convention": {"sideways"}},
	} {
		if status, body := chart(bad); status != http.StatusUnprocessableEntity || strings.Contains(body, "<") || !strings.HasSuffix(strings.TrimSpace(body), ".") {
			t.Errorf("chart of %v: %d %q", bad, status, body)
		}
	}
	if d := c.load("letters").Display; d.Template.Sizes != "" || d.Template.MaxStars != 10 {
		t.Errorf("charts changed the saved options: %+v", d)
	}
	// The draw-margin and other placement options don't matter to the
	// chart, even when they are being typed in.
	if status, body := chart(url.Values{"drawMargin": {""}, "groupRule": {"alternate"}}); status != http.StatusOK || strings.Contains(body, "Not applied yet") {
		t.Errorf("chart while the draw-margin is blank: %d\n%s", status, body)
	}
}
