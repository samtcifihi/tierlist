package web

import (
	"cmp"
	"encoding/csv"
	"fmt"
	"html"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/samtcifihi/tierlist/internal/tierlist"
)

// client talks to a test server without following redirects, so tests can
// check where each form sends the browser.
type client struct {
	t   *testing.T
	srv *httptest.Server
	dir string
}

func start(t *testing.T, dir string) (*Server, *client) {
	t.Helper()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, &client{t: t, srv: srv, dir: dir}
}

func (c *client) do(req *http.Request) (int, http.Header, string) {
	c.t.Helper()
	hc := *c.srv.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(body)
}

func (c *client) get(path string) (int, string) {
	c.t.Helper()
	req, _ := http.NewRequest("GET", c.srv.URL+path, nil)
	status, _, body := c.do(req)
	return status, body
}

// post submits a form and returns the status and where it redirects to.
func (c *client) post(path string, form url.Values) (int, string) {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, h, _ := c.do(req)
	return status, h.Get("Location")
}

// load reads a list straight from its file.
func (c *client) load(key string) *tierlist.List {
	c.t.Helper()
	l, err := tierlist.Load(filepath.Join(c.dir, key+".json"))
	if err != nil {
		c.t.Fatal(err)
	}
	return l
}

// newList creates a list with the given entries through the pages.
func (c *client) newList(name string, entries ...string) string {
	c.t.Helper()
	status, loc := c.post("/lists", url.Values{"name": {name}})
	if status != http.StatusSeeOther || !strings.HasSuffix(loc, "/entries") {
		c.t.Fatalf("creating %q: %d, %s", name, status, loc)
	}
	base := strings.TrimSuffix(loc, "/entries")
	if len(entries) > 0 {
		c.post(base+"/entries", url.Values{"names": {strings.Join(entries, "\n")}})
	}
	return base
}

var hidden = regexp.MustCompile(`name="(a|b|n)" value="(\d+)"`)

// question reads the pair and token from the rating page.
func (c *client) question(base string) (a, b, n int, body string) {
	c.t.Helper()
	status, body := c.get(base + "/rate")
	if status != http.StatusOK {
		c.t.Fatalf("rating page: %d\n%s", status, body)
	}
	vals := map[string]int{}
	for _, m := range hidden.FindAllStringSubmatch(body, -1) {
		v, _ := strconv.Atoi(m[2])
		if _, seen := vals[m[1]]; !seen {
			vals[m[1]] = v
		}
	}
	return vals["a"], vals["b"], vals["n"], body
}

func (c *client) answer(base string, a, b, n int, ans string) (int, string) {
	c.t.Helper()
	return c.post(base+"/answer", url.Values{"a": {strconv.Itoa(a)}, "b": {strconv.Itoa(b)}, "n": {strconv.Itoa(n)}, "answer": {ans}})
}

func TestLibrary(t *testing.T) {
	_, c := start(t, t.TempDir())
	if status, body := c.get("/"); status != http.StatusOK || !strings.Contains(body, "No lists yet") {
		t.Fatalf("empty library: %d\n%s", status, body)
	}
	c.newList("Films & shows", "Alien")
	_, body := c.get("/")
	if !strings.Contains(body, `href="/lists/films-shows"`) || !strings.Contains(body, "Films &amp; shows") ||
		!strings.Contains(body, "1 entry · 0 answers") {
		t.Errorf("library does not show the new list:\n%s", body)
	}
	if status, loc := c.post("/lists", url.Values{"name": {"  "}}); status != http.StatusSeeOther || !strings.Contains(loc, "err=") {
		t.Errorf("creating a list with a blank name: %d, %s", status, loc)
	}
	os.WriteFile(filepath.Join(c.dir, "broken.json"), []byte("{"), 0o644)
	if _, body := c.get("/"); !strings.Contains(body, "broken.json") || !strings.Contains(body, "can't be opened") {
		t.Errorf("library does not flag the damaged file:\n%s", body)
	}
	if status, body := c.get("/lists/broken/rate"); status != http.StatusInternalServerError || !strings.Contains(body, "can&#39;t be opened") {
		t.Errorf("opening the damaged file: %d\n%s", status, body)
	}
}

func TestEntries(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films")
	status, loc := c.post(base+"/entries", url.Values{"names": {"Alien\r\nBrazil\n\n  alien \nCasablanca\n"}})
	if status != http.StatusSeeOther || !strings.Contains(loc, "Added+3+entries.+Skipped+1+name") {
		t.Errorf("adding entries: %d, %s", status, loc)
	}
	c.post(base+"/entries/2/rename", url.Values{"name": {"Brazil (1985)"}})
	c.post(base+"/entries/3/remove", nil)
	c.post(base+"/rename", url.Values{"name": {"Movies"}})
	l := c.load("films")
	if l.Name != "Movies" || len(l.Entries) != 3 || l.Entries[1].Name != "Brazil (1985)" || !l.Entries[2].Removed {
		t.Errorf("saved list: %+v", l)
	}
	_, body := c.get(base + "/entries")
	removed := body[strings.Index(body, "<h2>Removed</h2>"):]
	if !strings.Contains(body, `value="Brazil (1985)"`) || !strings.Contains(removed, "Casablanca") {
		t.Errorf("entries page:\n%s", body)
	}
	if status, loc := c.post(base+"/entries/3/restore", nil); status != http.StatusSeeOther || !strings.Contains(loc, "Restored+Casablanca") {
		t.Errorf("restoring: %d, %s", status, loc)
	}
	if status, loc := c.post(base+"/entries/9/remove", nil); status != http.StatusSeeOther || !strings.Contains(loc, "err=") {
		t.Errorf("removing a missing entry: %d, %s", status, loc)
	}
}

func TestRating(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil", "Casablanca")
	a, b, n, body := c.question(base)
	if n != 0 || a == b || a < 1 || a > 3 || b < 1 || b > 3 || !strings.Contains(body, `data-keys="ArrowLeft 1"`) {
		t.Fatalf("first question: %d vs %d, token %d\n%s", a, b, n, body)
	}
	// With no answers, the draw setting is its prior's: equally rated
	// entries are called about the same a third of the time.
	if !strings.Contains(body, "draw setting 33%") {
		t.Errorf("no draw setting of 33%% on the first question:\n%s", body)
	}
	if status, loc := c.answer(base, a, b, n, "a"); status != http.StatusSeeOther || loc != base+"/rate" {
		t.Fatalf("answering: %d, %s", status, loc)
	}
	// The page then says where the two entries now stand: the winner at
	// the top, the loser at the bottom, the third entry between them.
	names := map[int]string{1: "Alien", 2: "Brazil", 3: "Casablanca"}
	if _, _, _, body := c.question(base); !strings.Contains(body, names[a]+" is now at the 100th percentile, "+names[b]+" at the 0th.") {
		t.Errorf("no percentiles for the last answer:\n%s", body)
	}
	// The same form again, say from a double click, is ignored.
	if _, loc := c.answer(base, a, b, n, "a"); !strings.Contains(loc, "out-of-date") {
		t.Errorf("answering twice: %s", loc)
	}
	l := c.load("films")
	if len(l.Comparisons) != 1 || l.Comparisons[0] != (tierlist.Comparison{A: a, B: b, Answer: tierlist.FirstBetter}) {
		t.Fatalf("saved answers: %v", l.Comparisons)
	}

	// Undo takes the answer back and asks the same question again.
	_, _, n, body = c.question(base)
	if n != 1 || !strings.Contains(body, "Undo:") {
		t.Fatalf("second question: token %d\n%s", n, body)
	}
	if _, loc := c.post(base+"/undo", url.Values{"n": {"1"}}); !strings.Contains(loc, "Took+back") {
		t.Errorf("undo: %s", loc)
	}
	if l := c.load("films"); len(l.Comparisons) != 0 {
		t.Errorf("after undo, %d answers are saved", len(l.Comparisons))
	}
	if a2, b2, n2, _ := c.question(base); a2 != a || b2 != b || n2 != 0 {
		t.Errorf("after undo, asked %d vs %d (token %d), want %d vs %d again", a2, b2, n2, a, b)
	}
	// A removed entry cannot be answered about, and with only one entry
	// left there is nothing to ask.
	c.post(base+"/entries/"+strconv.Itoa(a)+"/remove", nil)
	if _, loc := c.answer(base, a, b, 0, "b"); !strings.Contains(loc, "err=") {
		t.Errorf("answering about a removed entry: %s", loc)
	}
	c.post(base+"/entries/"+strconv.Itoa(b)+"/remove", nil)
	if _, body := c.get(base + "/rate"); !strings.Contains(body, "Add at least two entries") {
		t.Errorf("with one entry left, the rating page says:\n%s", body)
	}
}

var shownRatings = regexp.MustCompile(`value="([^"]+)" required aria-label="Name"[^<]*>\s*</form>\s*<span class="rating"[^>]*>(\d+) <span class="muted">± (\d+)</span>`)

func TestRatingsShownInPoints(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil", "Casablanca")
	c.answer(base, 1, 2, 0, "a")
	_, body := c.get(base + "/entries")
	type rating struct{ points, sd int }
	shown := map[string]rating{}
	for _, m := range shownRatings.FindAllStringSubmatch(body, -1) {
		points, _ := strconv.Atoi(m[2])
		sd, _ := strconv.Atoi(m[3])
		shown[m[1]] = rating{points, sd}
	}
	// Alien beat Brazil, so they sit either side of Casablanca, which has no
	// answers yet and so shows the center, 1500.
	if shown["Casablanca"].points != 1500 || !(shown["Alien"].points > 1500) || !(shown["Brazil"].points < 1500) ||
		shown["Alien"].points+shown["Brazil"].points != 3000 {
		t.Errorf("shown ratings %v; want Casablanca at 1500 with Alien and Brazil either side", shown)
	}
	// Ratings and their uncertainties are in points, not Elo.
	fit, err := c.load("films").Fit()
	if err != nil {
		t.Fatal(err)
	}
	for id, name := range map[int]string{1: "Alien", 2: "Brazil", 3: "Casablanca"} {
		want := rating{int(math.Round(fit.Points(id))), int(math.Round(fit.PointsSD(id)))}
		if shown[name] != want {
			t.Errorf("%s shows as %v, want %v", name, shown[name], want)
		}
	}
}

func TestEntriesCSV(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Kill Bill, Vol. 1", `The "Thing"`, "Zardoz", "Heat")
	c.answer(base, 1, 2, 0, "a")
	c.answer(base, 2, 3, 1, "same")
	c.answer(base, 3, 4, 2, "a")
	c.answer(base, 1, 3, 3, "a")
	c.post(base+"/entries/4/remove", nil)
	_, body := c.get(base + "/entries")
	const open = `aria-label="The entries as CSV">`
	i := strings.Index(body, open)
	if i < 0 || !strings.Contains(body, "<summary>CSV, to copy</summary>") {
		t.Fatalf("no CSV on the entries page:\n%s", body)
	}
	text, _, _ := strings.Cut(body[i+len(open):], "</textarea>")
	text = html.UnescapeString(text)
	// Names with commas and quotes are quoted, as CSV has it.
	if !strings.HasPrefix(text, "entry name,rating,CI width,number of answers\n") ||
		!strings.Contains(text, "\n\"Kill Bill, Vol. 1\",") || !strings.Contains(text, "\n\"The \"\"Thing\"\"\",") {
		t.Errorf("CSV:\n%s", text)
	}
	records, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatalf("reading the CSV back: %v\n%s", err, text)
	}
	// The shown entries, best first, with the ratings and ± uncertainties
	// (one standard deviation) the page shows, and the answer counts.
	// Zardoz is removed, so it is left out.
	l := c.load("films")
	fit, err := l.Fit()
	if err != nil {
		t.Fatal(err)
	}
	want := l.Shown()
	slices.SortStableFunc(want, func(a, b tierlist.Entry) int { return cmp.Compare(fit.Rating(b.ID), fit.Rating(a.ID)) })
	if len(records) != 1+len(want) || len(want) != 4 || want[0].Name != "Alien" {
		t.Fatalf("CSV has %d records for %v:\n%s", len(records), want, text)
	}
	counts := l.Counts()
	for k, e := range want {
		rec := records[k+1]
		if rec[0] != e.Name || rec[1] != fmt.Sprintf("%.0f", fit.Points(e.ID)) || rec[2] != fmt.Sprintf("%.0f", fit.PointsSD(e.ID)) ||
			rec[3] != strconv.Itoa(counts[e.ID]) || !strings.Contains(body, "± "+rec[2]+"<") {
			t.Errorf("row %d: %q, want %s with rating %.1f ± %.1f and %d answers", k+1, rec, e.Name, fit.Points(e.ID), fit.PointsSD(e.ID), counts[e.ID])
		}
	}
	// A list with no entries has no CSV.
	c.newList("Empty")
	if _, body := c.get("/lists/empty/entries"); strings.Contains(body, "CSV") {
		t.Errorf("CSV for an empty list:\n%s", body)
	}
}

func TestFocus(t *testing.T) {
	dir := t.TempDir()
	_, c := start(t, dir)
	base := c.newList("Letters", "A", "B", "C", "D", "E")
	if _, loc := c.post(base+"/focus", url.Values{"focus": {"5"}}); !strings.Contains(loc, "/rate?msg=Focus+mode+is+on") {
		t.Errorf("focusing: %s", loc)
	}
	check := func(c *client, rounds int) {
		t.Helper()
		for range rounds {
			a, b, n, body := c.question(base)
			if a != 5 && b != 5 {
				t.Fatalf("in focus mode on E, asked %d vs %d", a, b)
			}
			if !strings.Contains(body, "Focus mode: every pair includes E") {
				t.Fatalf("no focus banner:\n%s", body)
			}
			c.answer(base, a, b, n, "same")
		}
	}
	check(c, 6)
	// Focus mode is saved, so a fresh start keeps it.
	_, c2 := start(t, dir)
	check(c2, 3)
	if _, loc := c2.post(base+"/focus", url.Values{"do": {"clear"}, "from": {"rate"}}); !strings.Contains(loc, "/rate?msg=Focus+mode+is+off") {
		t.Errorf("leaving focus mode: %s", loc)
	}
	if l := c2.load("letters"); l.Focus != nil {
		t.Errorf("focus after leaving: %v", l.Focus)
	}
	if _, loc := c2.post(base+"/focus", nil); !strings.Contains(loc, "err=Tick") {
		t.Errorf("focusing on nothing: %s", loc)
	}
}

func TestTierListPage(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil", "Casablanca")
	c.answer(base, 1, 2, 0, "a")
	c.answer(base, 2, 3, 1, "a")
	status, body := c.get(base + "/tiers")
	if status != http.StatusOK || !strings.Contains(body, `<div class="tier" style="--hue: 250">
      <div class="tier-label">10★ (1)</div>`) ||
		!strings.Contains(body, "10★ (1): Alien\n9★ (0):\n8★ (0):\n7★ (0):\n6★ (0):\n5★ (1): Brazil\n4★ (0):\n3★ (0):\n2★ (0):\n1★ (0):\n0★ (1): Casablanca") {
		t.Fatalf("tier list page: %d\n%s", status, body)
	}
	fit, err := c.load("films").Fit()
	if err != nil {
		t.Fatal(err)
	}
	if draw := fmt.Sprintf("Draw setting %.0f%%", 100*fit.SameChance()); !strings.Contains(body, draw) {
		t.Errorf("no %q on the tier list page:\n%s", draw, body)
	}

	display := func(kind string, extra url.Values) (int, string) {
		form := url.Values{"kind": {kind}, "convention": {"top-closed"}, "drawMargin": {"0"},
			"groupRule": {"middle-entry"}, "prefer": {"higher"}, "maxStars": {"5"}, "divisions": {"1"}}
		for k, v := range extra {
			form[k] = v
		}
		return c.post(base+"/display", form)
	}
	if status, _ := display("hogwarts", nil); status != http.StatusSeeOther {
		t.Errorf("choosing Hogwarts: %d", status)
	}
	// Switching back to stars from here starts at 10 stars.
	if _, body := c.get(base + "/tiers"); !strings.Contains(body, "Outstanding (1): Alien") ||
		!strings.Contains(body, `name="maxStars" min="3" value="10"`) {
		t.Errorf("Hogwarts tier list:\n%s", body)
	}
	status, _ = display("custom", url.Values{"customName": {"Halves"}, "customTiers": {"Good\nBad"}, "customCutoffs": {"1/2"}})
	if l := c.load("films"); status != http.StatusSeeOther || l.Display.Template.Kind != "custom" ||
		!slices.Equal(l.Display.Template.Tiers, []string{"Good", "Bad"}) {
		t.Errorf("custom template: %d, %+v", status, l.Display)
	}
	// Cut-offs may come in any order, separated by commas or lines.
	display("custom", url.Values{"customTiers": {"S\nA\nB\nC"}, "customCutoffs": {"0.9, 1/4\n0.5"}})
	if l := c.load("films"); !slices.Equal(l.Display.Template.Cutoffs, []string{"1/4", "0.5", "0.9"}) {
		t.Errorf("cut-offs saved as %v", l.Display.Template.Cutoffs)
	}
	for _, bad := range []url.Values{
		{"kind": {"custom"}, "customTiers": {"Good\nBad"}, "customCutoffs": {"half"}},
		{"kind": {"custom"}, "customTiers": {""}, "customCutoffs": {""}},
		{"kind": {"stars"}, "maxStars": {"2"}},
		{"kind": {"stars"}, "drawMargin": {"-3"}},
	} {
		before := c.load("films").Display
		req, _ := http.NewRequest("POST", c.srv.URL+base+"/display", strings.NewReader(mergeForm(bad).Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		status, _, body := c.do(req)
		if status != http.StatusBadRequest || !strings.Contains(body, `class="note error"`) {
			t.Errorf("%v: %d\n%s", bad, status, body)
		}
		if after := c.load("films").Display; !slices.Equal(after.Template.Cutoffs, before.Template.Cutoffs) || after.Template.Kind != before.Template.Kind {
			t.Errorf("%v changed the saved display to %+v", bad, after)
		}
	}

	one := c.newList("Solo", "Only")
	if _, body := c.get(one + "/tiers"); !strings.Contains(body, "not much point in a tier list of one item") {
		t.Errorf("one-entry tier list:\n%s", body)
	}
	none := c.newList("Empty")
	if _, body := c.get(none + "/tiers"); !strings.Contains(body, "no entries yet") {
		t.Errorf("empty tier list:\n%s", body)
	}
}

func TestOrdinal(t *testing.T) {
	for n, want := range map[int]string{0: "0th", 1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th",
		21: "21st", 22: "22nd", 23: "23rd", 92: "92nd", 100: "100th", 101: "101st", 111: "111th"} {
		if got := ordinal(n); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestTierColors(t *testing.T) {
	for _, c := range []struct{ i, n, want int }{{0, 6, 250}, {5, 6, 0}, {2, 5, 125}, {0, 1, 250}} {
		if got := hue(c.i, c.n); got != c.want {
			t.Errorf("hue(%d, %d) = %d, want %d", c.i, c.n, got, c.want)
		}
	}
}

// mergeForm fills in the display fields a test leaves out.
func mergeForm(v url.Values) url.Values {
	form := url.Values{"convention": {"top-closed"}, "drawMargin": {"0"}, "groupRule": {"middle-entry"},
		"prefer": {"higher"}, "maxStars": {"5"}, "divisions": {"1"}}
	for k, x := range v {
		form[k] = x
	}
	return form
}

func TestGuard(t *testing.T) {
	_, c := start(t, t.TempDir())
	req, _ := http.NewRequest("GET", c.srv.URL+"/", nil)
	req.Host = "evil.example"
	if status, _, _ := c.do(req); status != http.StatusForbidden {
		t.Errorf("a request for another host: %d", status)
	}
	for _, h := range []map[string]string{
		{"Origin": "http://evil.example"},
		{"Origin": "null"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
	} {
		req, _ := http.NewRequest("POST", c.srv.URL+"/lists", strings.NewReader("name=Hacked"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range h {
			req.Header.Set(k, v)
		}
		if status, _, _ := c.do(req); status != http.StatusForbidden {
			t.Errorf("a form from another site (%v): %d", h, status)
		}
	}
	req, _ = http.NewRequest("POST", c.srv.URL+"/lists", strings.NewReader("name=Mine"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", c.srv.URL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if status, _, _ := c.do(req); status != http.StatusSeeOther {
		t.Errorf("a form from the program's own page: %d", status)
	}
	if sums, _ := tierlist.Lists(c.dir); len(sums) != 1 || sums[0].Name != "Mine" {
		t.Errorf("lists after the attempts: %v", sums)
	}
}

func TestOutsideChangesAreReloaded(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil")
	c.get(base + "/entries") // now held in memory
	l := c.load("films")
	l.AddEntry("Dune")
	if err := l.Save(filepath.Join(c.dir, "films.json")); err != nil {
		t.Fatal(err)
	}
	if _, body := c.get(base + "/entries"); !strings.Contains(body, `value="Dune"`) {
		t.Errorf("an entry added outside the program does not show:\n%s", body)
	}
}

func TestOtherRoutes(t *testing.T) {
	s, c := start(t, t.TempDir())
	for _, path := range []string{"/lists/nope/rate", "/lists/..%2Fsecret/rate", "/lists/a%5Cb/entries"} {
		if status, _ := c.get(path); status != http.StatusNotFound {
			t.Errorf("GET %s: %d", path, status)
		}
	}
	base := c.newList("Films", "Alien")
	if status, loc := c.post(base+"/entries/x/remove", nil); status != http.StatusNotFound {
		t.Errorf("removing entry x: %d, %s", status, loc)
	}
	req, _ := http.NewRequest("GET", c.srv.URL+base, nil)
	if _, h, _ := c.do(req); h.Get("Location") != base+"/entries" {
		t.Errorf("a list with one entry opens on %q", h.Get("Location"))
	}
	if status, body := c.get("/ping"); status != http.StatusOK || body != "tierlist" {
		t.Errorf("ping: %d %q", status, body)
	}
	req, _ = http.NewRequest("GET", c.srv.URL+"/static/style.css", nil)
	if status, h, _ := c.do(req); status != http.StatusOK || !strings.HasPrefix(h.Get("Content-Type"), "text/css") {
		t.Errorf("style sheet: %d, %s", status, h.Get("Content-Type"))
	}
	select {
	case <-s.Quit():
		t.Fatal("quit before being asked")
	default:
	}
	req, _ = http.NewRequest("POST", c.srv.URL+"/quit", nil)
	if status, _, body := c.do(req); status != http.StatusOK || !strings.Contains(body, "has stopped") {
		t.Errorf("quit: %d\n%s", status, body)
	}
	select {
	case <-s.Quit():
	default:
		t.Error("Quit was not signalled")
	}
}

func TestDeleteList(t *testing.T) {
	_, c := start(t, t.TempDir())
	films := c.newList("Films", "Alien", "Brazil")
	c.answer(films, 1, 2, 0, "a")
	games := c.newList("Games")
	os.WriteFile(filepath.Join(c.dir, "broken.json"), []byte("{"), 0o644)

	// Every list in the library, damaged or not, can be deleted after a
	// dialog that warns it can't be undone.
	_, body := c.get("/")
	for _, want := range []string{
		`data-dialog="delete-1"`, `<dialog id="delete-1"`, `action="/lists/broken/delete"`,
		"Delete broken.json?", "This file can&#39;t be opened as a tier list.",
		`action="/lists/films/delete"`, "Delete “Films”?", "Its 2 entries and 1 answer will be deleted with it.",
		`action="/lists/games/delete"`, "Delete “Games”?", "The list is empty.",
		"This can't be undone.", `<button formmethod="dialog" autofocus>Cancel</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("library page lacks %q:\n%s", want, body)
		}
	}
	// So can the list on its entries page.
	if _, body := c.get(games + "/entries"); !strings.Contains(body, `data-dialog="delete-list"`) ||
		!strings.Contains(body, `action="/lists/games/delete"`) {
		t.Errorf("entries page has no way to delete the list:\n%s", body)
	}

	status, loc := c.post(films+"/delete", nil)
	if u, _ := url.Parse(loc); status != http.StatusSeeOther || u.Path != "/" || u.Query().Get("msg") != "Deleted “Films”." {
		t.Errorf("deleting Films: %d, %s", status, loc)
	}
	if _, err := os.Stat(filepath.Join(c.dir, "films.json")); !os.IsNotExist(err) {
		t.Errorf("films.json after deleting: %v", err)
	}
	if status, _ := c.get(films + "/rate"); status != http.StatusNotFound {
		t.Errorf("a deleted list's page: %d", status)
	}
	if status, _ := c.post(films+"/delete", nil); status != http.StatusNotFound {
		t.Errorf("deleting it again: %d", status)
	}
	// A new list with the same name starts empty.
	c.newList("Films")
	if _, body := c.get(films + "/entries"); strings.Contains(body, `value="Alien"`) {
		t.Errorf("a new list called Films shows the deleted one's entries:\n%s", body)
	}

	if _, loc := c.post("/lists/broken/delete", nil); !strings.Contains(loc, "msg=Deleted+%E2%80%9Cbroken.json") {
		t.Errorf("deleting the damaged file: %s", loc)
	}
	// Only a list file directly in the folder can be deleted, and only from
	// the program's own pages.
	os.WriteFile(filepath.Join(filepath.Dir(c.dir), "secret.json"), []byte("{}"), 0o644)
	for _, path := range []string{"/lists/..%2Fsecret/delete", "/lists/a%5Cb/delete", "/lists//delete"} {
		if status, _ := c.post(path, nil); status != http.StatusNotFound && status != http.StatusMovedPermanently {
			t.Errorf("POST %s: %d", path, status)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.dir), "secret.json")); err != nil {
		t.Errorf("a file outside the folder was touched: %v", err)
	}
	req, _ := http.NewRequest("POST", c.srv.URL+games+"/delete", nil)
	req.Header.Set("Origin", "http://evil.example")
	if status, _, _ := c.do(req); status != http.StatusForbidden {
		t.Errorf("a deletion from another site: %d", status)
	}
	var names []string
	sums, _ := tierlist.Lists(c.dir)
	for _, s := range sums {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"Films", "Games"}) {
		t.Errorf("lists left: %v", names)
	}
}

var answerRows = regexp.MustCompile(`<tr>\s*<td class="num">(\d+)</td>\s*<td>(.*?)</td>\s*<td class="rel"[^>]*>(.*?)</td>\s*<td>(.*?)</td>\s*</tr>`)

// answersShown returns the rows of the answers table as plain text, cells
// separated by spaces.
func answersShown(body string) []string {
	tags := regexp.MustCompile(`<[^>]*>`)
	var rows []string
	for _, m := range answerRows.FindAllStringSubmatch(body, -1) {
		cells := []string{m[1]}
		for _, cell := range m[2:] {
			cells = append(cells, html.UnescapeString(tags.ReplaceAllString(cell, "")))
		}
		rows = append(rows, strings.Join(cells, " "))
	}
	return rows
}

func TestAnswersPage(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil", "Casablanca", "Dune", "Eraserhead")
	if _, body := c.get(base + "/answers"); !strings.Contains(body, "No answers yet") {
		t.Errorf("answers page with no answers:\n%s", body)
	}
	c.answer(base, 1, 2, 0, "a")    // Alien over Brazil
	c.answer(base, 3, 2, 1, "b")    // Brazil over Casablanca
	c.answer(base, 3, 4, 2, "same") // Casablanca and Dune about the same
	c.post(base+"/entries/4/remove", nil)

	_, body := c.get(base + "/answers")
	want := []string{"3 Casablanca ≈ Dune (removed)", "2 Brazil > Casablanca", "1 Alien > Brazil"}
	if got := answersShown(body); !slices.Equal(got, want) || !strings.Contains(body, "3 answers, newest first.") ||
		!strings.Contains(body, `href="/lists/films/answers" aria-current="page"`) || strings.Contains(body, "<details open") {
		t.Errorf("answers %q, want %q; page:\n%s", got, want, body)
	}

	for _, tt := range []struct {
		query   string
		rows    []string
		summary string
	}{
		{"entry=2", []string{"2 Brazil > Casablanca", "1 Alien > Brazil"}, "Answers involving Brazil: 2 of 3, newest first."},
		{"entry=4&entry=1", []string{"3 Casablanca ≈ Dune (removed)", "1 Alien > Brazil"},
			"Answers involving Alien or Dune: 2 of 3, newest first."},
		{"entry=5", nil, "None of the 3 answers involve Eraserhead."},
		{"entry=x&entry=99", want, "3 answers, newest first."},
		// Only the answers between ticked entries.
		{"entry=2&entry=3&match=only", []string{"2 Brazil > Casablanca"}, "Answers between Brazil and Casablanca: 1 of 3, newest first."},
		{"entry=4&entry=3&entry=2&match=only", []string{"3 Casablanca ≈ Dune (removed)", "2 Brazil > Casablanca"},
			"Answers among Brazil, Casablanca and Dune: 2 of 3, newest first."},
		{"entry=1&entry=5&match=only", nil, "None of the 3 answers are between Alien and Eraserhead."},
		{"entry=2&match=only", nil, "Every answer is about two entries, so tick at least two to see the answers only between them."},
		{"match=only", want, "3 answers, newest first."},
	} {
		_, body := c.get(base + "/answers?" + tt.query)
		if got := answersShown(body); !slices.Equal(got, tt.rows) || !strings.Contains(body, tt.summary) {
			t.Errorf("?%s: answers %q and no %q; page:\n%s", tt.query, got, tt.summary, body)
		}
	}
	// A filter keeps its choices ticked, shows the chosen names in bold and
	// offers a way back to every answer. Switching between the kinds of
	// filter applies at once, and ticking waits for the Filter button.
	_, body = c.get(base + "/answers?entry=2")
	for _, want := range []string{"<details open>", `value="2" checked`, `value="any" checked`, "<strong>Brazil</strong>",
		`href="/lists/films/answers">Show all</a>`, `class="filter" data-autosubmit`, `<div class="picks" data-wait>`,
		`<td class="rel" title="better than">&gt;</td>`} {
		if !strings.Contains(body, want) {
			t.Errorf("filtered page lacks %q:\n%s", want, body)
		}
	}
	_, body = c.get(base + "/answers?match=only")
	if !strings.Contains(body, "<details open>") || !strings.Contains(body, `value="only" checked`) ||
		!strings.Contains(body, `<td class="rel" title="about the same as">≈</td>`) {
		t.Errorf("switching to only between ticked entries, with none ticked:\n%s", body)
	}
	// Each entry's count of answers on the entries page leads here.
	if _, body := c.get(base + "/entries"); !strings.Contains(body, `href="/lists/films/answers?entry=2">2 answers</a>`) ||
		!strings.Contains(body, `href="/lists/films/answers?entry=1">1 answer</a>`) {
		t.Errorf("entries page does not link to each entry's answers:\n%s", body)
	}
}

var upcomingRows = regexp.MustCompile(`<li><span class="box">([^<]*)</span><span class="vs">vs</span><span class="box">([^<]*)</span></li>`)

// upcomingShown returns the pairs the rating page shows coming up, the
// furthest first, as it stacks them.
func upcomingShown(body string) [][2]string {
	var pairs [][2]string
	for _, m := range upcomingRows.FindAllStringSubmatch(body, -1) {
		pairs = append(pairs, [2]string{m[1], m[2]})
	}
	return pairs
}

func TestUpcomingPairs(t *testing.T) {
	s, c := start(t, t.TempDir())
	s.rng = rand.New(rand.NewPCG(1, 1)) // the same pairs every run
	base := c.newList("Letters", "A", "B", "C", "D", "E", "F", "G", "H")
	letter := func(id int) string { return string(rune('A' + id - 1)) }
	a, b, n, body := c.question(base)
	up := upcomingShown(body)
	if len(up) != 4 {
		t.Fatalf("%d pairs coming up, want 4:\n%s", len(up), body)
	}

	// Answering asks the next pair, nearest the bottom of the stack; the
	// rest move down and one new pair joins at the top.
	c.answer(base, a, b, n, "a")
	a2, b2, n2, body := c.question(base)
	up2 := upcomingShown(body)
	if got := [2]string{letter(a2), letter(b2)}; got != up[3] || !slices.Equal(up2[1:], up[:3]) {
		t.Errorf("before answering, coming up %v; after, asked %v with %v coming up", up, got, up2)
	}
	// Undo asks the answered pair again, with the same pairs coming up as
	// before.
	c.post(base+"/undo", url.Values{"n": {strconv.Itoa(n2)}})
	a3, b3, _, body := c.question(base)
	if a3 != a || b3 != b || !slices.Equal(upcomingShown(body), up) {
		t.Errorf("after undo, asked %d vs %d with %v coming up; want %d vs %d with %v", a3, b3, upcomingShown(body), a, b, up)
	}

	// New entries are brought into the pairs coming up straight away, while
	// the pair being asked stays.
	c.post(base+"/entries", url.Values{"names": {"I\nJ"}})
	a4, b4, _, body := c.question(base)
	seen := map[string]bool{}
	for _, p := range upcomingShown(body) {
		seen[p[0]], seen[p[1]] = true, true
	}
	if a4 != a || b4 != b || !seen["I"] || !seen["J"] {
		t.Errorf("after adding I and J: asked %d vs %d, coming up %v", a4, b4, upcomingShown(body))
	}
	// So is focus mode.
	c.post(base+"/focus", url.Values{"focus": {"1"}})
	a5, b5, _, body := c.question(base)
	if a5 != 1 && b5 != 1 {
		t.Errorf("in focus mode on A, asked %d vs %d", a5, b5)
	}
	for _, p := range upcomingShown(body) {
		if p[0] != "A" && p[1] != "A" {
			t.Errorf("in focus mode on A, %v is coming up", p)
		}
	}
	// Leaving focus mode plans the pairs coming up afresh too.
	c.post(base+"/focus", url.Values{"do": {"clear"}})
	_, _, _, body = c.question(base)
	withA := 0
	for _, p := range upcomingShown(body) {
		if p[0] == "A" || p[1] == "A" {
			withA++
		}
	}
	if withA == 4 {
		t.Errorf("after leaving focus mode, every pair coming up still includes A: %v", upcomingShown(body))
	}
}

func TestResetList(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil", "Casablanca")
	// With no answers there is nothing to reset yet.
	if _, body := c.get(base + "/entries"); !strings.Contains(body, `data-dialog="reset-list" disabled`) {
		t.Errorf("reset is offered with no answers:\n%s", body)
	}
	c.answer(base, 1, 2, 0, "a")
	c.answer(base, 2, 3, 1, "same")
	c.post(base+"/entries/3/remove", nil)

	// The button opens a dialog that says what goes and warns it can't be
	// undone.
	_, body := c.get(base + "/entries")
	for _, want := range []string{`<button type="button" data-dialog="reset-list">Reset this list…</button>`,
		`<dialog id="reset-list"`, `action="/lists/films/reset"`, "Reset “Films”?",
		"Its 2 answers will be deleted. The entries stay, and their ratings start over at 1500.",
		"This can't be undone.", `<button class="danger">Reset</button>`, `<button class="danger">Delete</button>`} {
		if !strings.Contains(body, want) {
			t.Errorf("entries page lacks %q:\n%s", want, body)
		}
	}

	status, loc := c.post(base+"/reset", nil)
	if u, _ := url.Parse(loc); status != http.StatusSeeOther || u.Path != base+"/entries" ||
		u.Query().Get("msg") != "Reset the list, deleting 2 answers. Every entry starts again at 1500." {
		t.Errorf("resetting: %d, %s", status, loc)
	}
	l := c.load("films")
	if len(l.Comparisons) != 0 || len(l.Entries) != 3 || !l.Entries[2].Removed || l.DrawElo != 0 {
		t.Errorf("saved list after reset: %+v", l)
	}
	// The ratings start over, and so does rating, with no answer to undo.
	_, body = c.get(base + "/entries")
	for _, m := range shownRatings.FindAllStringSubmatch(body, -1) {
		if m[2] != "1500" {
			t.Errorf("%s is rated %s after reset", m[1], m[2])
		}
	}
	if _, _, n, body := c.question(base); n != 0 || strings.Contains(body, "Undo:") || len(upcomingShown(body)) != 4 {
		t.Errorf("rating after reset: token %d, %d pairs coming up\n%s", n, len(upcomingShown(body)), body)
	}
	// Only the program's own pages can reset a list.
	c.answer(base, 1, 2, 0, "b")
	req, _ := http.NewRequest("POST", c.srv.URL+base+"/reset", nil)
	req.Header.Set("Origin", "http://evil.example")
	if status, _, _ := c.do(req); status != http.StatusForbidden || len(c.load("films").Comparisons) != 1 {
		t.Errorf("a reset from another site: %d", status)
	}
}

// After a reset every entry is new again, so the pairs lined up are chosen
// afresh: the pair asked and the next two cover all six entries.
func TestResetPlansAfresh(t *testing.T) {
	s, c := start(t, t.TempDir())
	s.rng = rand.New(rand.NewPCG(2, 2))
	base := c.newList("Letters", "A", "B", "C", "D", "E", "F")
	for range 8 {
		a, b, n, _ := c.question(base)
		c.answer(base, a, b, n, "a")
	}
	c.question(base) // lines up pairs chosen from these answers
	c.post(base+"/reset", nil)
	a, b, _, body := c.question(base)
	letter := func(id int) string { return string(rune('A' + id - 1)) }
	seen := map[string]bool{letter(a): true, letter(b): true}
	up := upcomingShown(body)
	for _, p := range up[len(up)-2:] {
		seen[p[0]], seen[p[1]] = true, true
	}
	if len(seen) != 6 {
		t.Errorf("after reset, asked %s vs %s with %v coming up; want the first three pairs to cover all six entries",
			letter(a), letter(b), up)
	}
}

func TestIgnoring(t *testing.T) {
	s, c := start(t, t.TempDir())
	s.rng = rand.New(rand.NewPCG(3, 3))
	base := c.newList("Letters", "A", "B", "C", "D")
	letter := func(id int) string { return string(rune('A' + id - 1)) }
	ignore := func(a, b, n int, what string) string {
		t.Helper()
		_, loc := c.post(base+"/ignore", url.Values{"a": {strconv.Itoa(a)}, "b": {strconv.Itoa(b)}, "n": {strconv.Itoa(n)}, "ignore": {what}})
		return loc
	}
	undo := func(n int) { c.post(base+"/undo", url.Values{"n": {strconv.Itoa(n)}}) }
	same := func(a, b, c, d int) bool { return (a == c && b == d) || (a == d && b == c) }

	a, b, n, body := c.question(base)
	for _, want := range []string{
		`formaction="/lists/letters/ignore" name="ignore" value="a" data-keys="4"`,
		`formaction="/lists/letters/ignore" name="ignore" value="pair" data-keys="5"`,
		`formaction="/lists/letters/ignore" name="ignore" value="b" data-keys="6"`,
		"Ignore " + letter(a) + " <kbd>4</kbd>", "Ignore this pair <kbd>5</kbd>", "Ignore " + letter(b) + " <kbd>6</kbd>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rating page lacks %q:\n%s", want, body)
		}
	}

	// Ignoring the pair moves on to another pair; Undo asks it again, the
	// same way round.
	if loc := ignore(a, b, n, "pair"); !strings.Contains(loc, "msg=Not+asking+about+"+letter(a)+"+vs+"+letter(b)) {
		t.Errorf("ignoring the pair: %s", loc)
	}
	if loc := ignore(a, b, n, "a"); !strings.Contains(loc, "out-of-date") || len(c.load("letters").IgnoredEntries) != 0 {
		t.Errorf("an ignore from an out-of-date page: %s", loc)
	}
	a1, b1, n1, body := c.question(base)
	if same(a, b, a1, b1) || !strings.Contains(body, "Undo: ignoring "+letter(a)+" vs "+letter(b)) {
		t.Errorf("after ignoring %d vs %d, asked %d vs %d:\n%s", a, b, a1, b1, body)
	}
	undo(n1)
	if a2, b2, _, _ := c.question(base); a2 != a || b2 != b || len(c.load("letters").IgnoredPairs) != 0 {
		t.Errorf("after undoing the ignore, asked %d vs %d; want %d vs %d with nothing ignored", a2, b2, a, b)
	}

	// Undo takes back answers and ignores newest first.
	c.answer(base, a, b, n, "a")
	a3, b3, n3, _ := c.question(base)
	ignore(a3, b3, n3, "b")
	a4, b4, n4, body := c.question(base)
	if a4 == b3 || b4 == b3 || !strings.Contains(body, "Undo: ignoring "+letter(b3)) {
		t.Errorf("after ignoring %s, asked %d vs %d:\n%s", letter(b3), a4, b4, body)
	}
	for _, p := range upcomingShown(body) {
		if p[0] == letter(b3) || p[1] == letter(b3) {
			t.Errorf("ignored %s is coming up: %v", letter(b3), p)
		}
	}
	undo(n4)
	a5, b5, n5, body := c.question(base)
	if a5 != a3 || b5 != b3 || c.load("letters").EntryIgnored(b3) || !strings.Contains(body, "Undo: "+letter(a)+" over "+letter(b)) {
		t.Errorf("after undoing the ignore: asked %d vs %d, want %d vs %d, with the answer next to undo:\n%s", a5, b5, a3, b3, body)
	}
	undo(n5)
	if l := c.load("letters"); len(l.Comparisons) != 0 {
		t.Errorf("the second undo left %d answers", len(l.Comparisons))
	}

	// With every pair ignored there is nothing left to ask, and Reset
	// ignores brings them all back.
	for range 3 {
		a, b, n, _ := c.question(base)
		ignore(a, b, n, "a")
	}
	if _, body := c.get(base + "/rate"); !strings.Contains(body, "Every pair left to ask is ignored") ||
		!strings.Contains(body, `action="/lists/letters/ignores/reset"`) || !strings.Contains(body, "Undo: ignoring") {
		t.Errorf("rating page with nothing left to ask:\n%s", body)
	}
	_, body = c.get(base + "/entries")
	if strings.Count(body, " · ignored") != 3 || !strings.Contains(body, "Ignoring ") || !strings.Contains(body, "<button>Reset ignores</button>") {
		t.Errorf("entries page with three entries ignored:\n%s", body)
	}
	if _, loc := c.post(base+"/ignores/reset", url.Values{"from": {"rate"}}); !strings.HasPrefix(loc, base+"/rate?msg=Every+entry") {
		t.Errorf("resetting ignores: %s", loc)
	}
	if _, _, _, body := c.question(base); len(c.load("letters").IgnoredEntries) != 0 || strings.Contains(body, "Undo: ignoring") {
		t.Errorf("after resetting ignores: %v ignored\n%s", c.load("letters").IgnoredEntries, body)
	}
}

func TestTopModePage(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Letters", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J")
	for k := 1; k < 10; k++ {
		c.answer(base, k, k+1, k-1, "a")
	}
	if _, _, _, body := c.question(base); !strings.Contains(body, `action="/lists/letters/top"`) || !strings.Contains(body, "Start top mode") {
		t.Errorf("no way to start top mode:\n%s", body)
	}
	if _, loc := c.post(base+"/top", url.Values{"top": {"20"}}); loc != base+"/rate" {
		t.Errorf("starting top mode: %s", loc)
	}
	// Every pair shown, now and coming up, is from the top 40%: A to D.
	a, b, _, body := c.question(base)
	if a > 4 || b > 4 || !strings.Contains(body, "Top mode: favouring the top 20% of the list, and asking only about the top 40%.") ||
		strings.Contains(body, "Start top mode") {
		t.Errorf("in top mode, asked %d vs %d:\n%s", a, b, body)
	}
	for _, p := range upcomingShown(body) {
		for _, name := range p {
			if name > "D" {
				t.Errorf("in top mode, %v is coming up", p)
			}
		}
	}
	for _, bad := range []string{"0", "150", "lots"} {
		if _, loc := c.post(base+"/top", url.Values{"top": {bad}}); !strings.Contains(loc, "err=") {
			t.Errorf("top mode on %q: %s", bad, loc)
		}
	}
	if _, loc := c.post(base+"/top", url.Values{"do": {"clear"}}); !strings.Contains(loc, "msg=Top+mode+is+off") || c.load("letters").Top != 0 {
		t.Errorf("leaving top mode: %s", loc)
	}
	// Focus mode turns top mode off.
	c.post(base+"/top", url.Values{"top": {"20"}})
	c.post(base+"/focus", url.Values{"focus": {"5"}})
	if l := c.load("letters"); l.Top != 0 || !slices.Equal(l.Focus, []int{5}) {
		t.Errorf("after focusing: top %v, focus %v", l.Top, l.Focus)
	}
}

func TestTierSizes(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Letters", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J")
	for k := 1; k < 10; k++ {
		c.answer(base, k, k+1, k-1, "a")
	}
	display := func(extra url.Values) (int, string) {
		form := mergeForm(url.Values{"kind": {"stars"}, "maxStars": {"3"}})
		for k, v := range extra {
			form[k] = v
		}
		req, _ := http.NewRequest("POST", c.srv.URL+base+"/display", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		status, _, body := c.do(req)
		return status, body
	}
	// New lists start with even tiers, and the form offers the other
	// kinds with their usual numbers filled in.
	_, body := c.get(base + "/tiers")
	for _, want := range []string{`<option value="even" selected>Nearest star</option>`, `name="factor" value="1.618033988749895"`, `<div class="for-beta" data-wait>`,
		`<option value="best" selected>the best tier</option>`, `name="alpha" value="2"`, `name="beta" value="2"`} {
		if !strings.Contains(body, want) {
			t.Errorf("tier list page lacks %q", want)
		}
	}
	// Ten entries on 0–3 stars, with each tier twice the size of the one
	// above: 1, 2, 4 and 8 fifteenths, best first.
	if status, _ := display(url.Values{"sizes": {"geometric"}, "factor": {"2"}, "from": {"best"}}); status != http.StatusSeeOther {
		t.Fatalf("choosing geometric tiers: %d", status)
	}
	l := c.load("letters")
	if tm := l.Display.Template; tm.Sizes != "geometric" || tm.Factor != "2" || tm.From != "best" || tm.Alpha != "" {
		t.Errorf("saved template %+v", tm)
	}
	_, body = c.get(base + "/tiers")
	if !strings.Contains(body, "3★ (1): A\n2★ (1): B\n1★ (3): C, D, E\n0★ (5): F, G, H, I, J") ||
		!strings.Contains(body, `<option value="geometric" selected>`) {
		t.Errorf("geometric tier list:\n%s", body)
	}
	// Beta(2, 2) over four equal parts makes the middle tiers bigger: its
	// CDF, 3x² - 2x³, moves the cut-offs from 1/4, 1/2 and 3/4 to 0.15625,
	// 1/2 and 0.84375, so ten entries go 2, 3, 3, 2.
	display(url.Values{"sizes": {"beta"}, "alpha": {"4/2"}, "beta": {"2.0"}})
	if tm := c.load("letters").Display.Template; tm.Sizes != "beta" || tm.Alpha != "4/2" || tm.Beta != "2.0" || tm.Factor != "" {
		t.Errorf("saved template %+v", tm)
	}
	if _, body := c.get(base + "/tiers"); !strings.Contains(body, "3★ (2): A, B\n2★ (3): C, D, E\n1★ (3): F, G, H\n0★ (2): I, J") ||
		!strings.Contains(body, `name="alpha" value="4/2"`) {
		t.Errorf("Beta(2, 2) tier list:\n%s", body)
	}
	for _, bad := range []struct {
		form url.Values
		want string
	}{
		{url.Values{"sizes": {"geometric"}, "factor": {"-1"}}, "above 0"},
		{url.Values{"sizes": {"geometric"}, "factor": {"lots"}}, "must be a number"},
		{url.Values{"sizes": {"beta"}, "alpha": {"0"}, "beta": {"2"}}, "above 0"},
		{url.Values{"sizes": {"beta"}, "alpha": {"2"}, "beta": {""}}, "β must be a number"},
		{url.Values{"sizes": {"square"}}, "choose how big the tiers are"},
	} {
		if status, body := display(bad.form); status != http.StatusBadRequest || !strings.Contains(strings.ToLower(body), strings.ToLower(bad.want)) {
			t.Errorf("%v: %d, want an error mentioning %q\n%s", bad.form, status, bad.want, body)
		}
	}
	if tm := c.load("letters").Display.Template; tm.Sizes != "beta" {
		t.Errorf("a rejected form changed the saved template to %+v", tm)
	}
}
