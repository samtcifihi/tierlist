package web

import (
	"io"
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
		!strings.Contains(body, "1 entries") {
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
	if status, loc := c.answer(base, a, b, n, "a"); status != http.StatusSeeOther || loc != base+"/rate" {
		t.Fatalf("answering: %d, %s", status, loc)
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

var shownRatings = regexp.MustCompile(`value="([^"]+)" required aria-label="Name"[^<]*>\s*</form>\s*<span class="rating"[^>]*>(\d+) `)

func TestRatingsShownAround1500(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil", "Casablanca")
	c.answer(base, 1, 2, 0, "a")
	_, body := c.get(base + "/entries")
	shown := map[string]int{}
	for _, m := range shownRatings.FindAllStringSubmatch(body, -1) {
		shown[m[1]], _ = strconv.Atoi(m[2])
	}
	// Alien beat Brazil, so they sit either side of Casablanca, which has no
	// answers yet and so shows the center, 1500.
	if shown["Casablanca"] != 1500 || !(shown["Alien"] > 1500) || !(shown["Brazil"] < 1500) ||
		shown["Alien"]+shown["Brazil"] != 3000 {
		t.Errorf("shown ratings %v; want Casablanca at 1500 with Alien and Brazil either side", shown)
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
      <div class="tier-label">5★</div>`) ||
		!strings.Contains(body, "5★: Alien\n4★:\n3★: Brazil\n2★:\n1★:\n0★: Casablanca") {
		t.Fatalf("tier list page: %d\n%s", status, body)
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
	if _, body := c.get(base + "/tiers"); !strings.Contains(body, "Outstanding: Alien") {
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
