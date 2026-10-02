package web

import (
	"cmp"
	"encoding/json"
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
	"reflect"
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

func TestExportImport(t *testing.T) {
	dir := t.TempDir()
	_, c := start(t, dir)
	base := c.newList("Films", `[{"name": "Alien", "url": "example.com/alien", "description": "Sci-fi horror"}, "Brazil"]`)
	c.answer(base, 1, 2, 0, "a")
	// The start page offers each list for export, and the export page
	// shows its file as it is saved.
	if _, body := c.get("/"); !strings.Contains(body, `href="/lists/films/export">Export</a>`) || !strings.Contains(body, "<summary>Import a list</summary>") {
		t.Errorf("start page:\n%s", body)
	}
	status, body := c.get(base + "/export")
	saved, _ := os.ReadFile(filepath.Join(dir, "films.json"))
	m := regexp.MustCompile(`(?s)<textarea class="file-text"[^>]*>(.*?)</textarea>`).FindStringSubmatch(body)
	if status != http.StatusOK || m == nil || html.UnescapeString(m[1]) != string(saved) || !strings.Contains(body, "<h1>Export “Films”</h1>") {
		t.Fatalf("export page: %d\n%s", status, body)
	}
	// Importing that text adds a copy, next to the list, with a number.
	if status, loc := c.post("/lists/import", url.Values{"data": {html.UnescapeString(m[1])}}); status != http.StatusSeeOther || !strings.Contains(loc, "msg=Imported") {
		t.Errorf("importing: %d, %s", status, loc)
	}
	copied := c.load("films-2")
	if copied.Name != "Films (2)" || len(copied.Comparisons) != 1 || copied.Entries[0].URL != "https://example.com/alien" {
		t.Errorf("imported %+v", copied)
	}
	if c.load("films").Name != "Films" {
		t.Error("importing changed the original list")
	}
	// Text that isn't a list file changes nothing, and comes back to fix.
	req, _ := http.NewRequest("POST", c.srv.URL+"/lists/import", strings.NewReader(url.Values{"data": {`{"format":"tierlist"`}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, _, body = c.do(req)
	if status != http.StatusBadRequest || !strings.Contains(body, "That can&#39;t be imported: not a readable tier list") ||
		!strings.Contains(body, `{&#34;format&#34;:&#34;tierlist&#34;</textarea>`) || !strings.Contains(body, "<details class=\"disclosure import\" open>") {
		t.Errorf("importing a broken file: %d\n%s", status, body)
	}
	if sums, _ := tierlist.Lists(dir); len(sums) != 2 {
		t.Errorf("%d lists after a failed import; want 2", len(sums))
	}
	// A file that can't be opened as a list can still be exported, to
	// rescue its text; a missing one can't.
	os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"name": "Half a list`), 0o644)
	if status, body := c.get("/lists/broken/export"); status != http.StatusOK || !strings.Contains(body, `{&#34;name&#34;: &#34;Half a list</textarea>`) {
		t.Errorf("exporting a damaged file: %d\n%s", status, body)
	}
	for _, path := range []string{"/lists/missing/export", "/lists/..%2Ffilms/export"} {
		if status, _ := c.get(path); status != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, status)
		}
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

func TestEntryDetails(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", `[{"name": "Alien", "url": "example.com/alien", "description": "Sci-fi horror, 1979"}, "Brazil"]`)
	entry := func(name string) tierlist.Entry {
		t.Helper()
		for _, e := range c.load("films").Entries {
			if e.Name == name {
				return e
			}
		}
		t.Fatalf("no entry %q", name)
		return tierlist.Entry{}
	}
	if e := entry("Alien"); e.URL != "https://example.com/alien" || e.Description != "Sci-fi horror, 1979" {
		t.Errorf("Alien added as %+v", e)
	}
	if e := entry("Brazil"); e.URL != "" || e.Description != "" {
		t.Errorf("Brazil added as %+v", e)
	}
	c.answer(base, 1, 2, 0, "a")
	add := func(text string) (int, string) {
		t.Helper()
		return c.post(base+"/entries", url.Values{"names": {text}})
	}
	// A name already in the list, in any case, updates that entry: an
	// empty link given clears it, and it keeps its ID and answers.
	if _, loc := add(`[{"name": "alien", "url": "", "description": "Chestbursting"}]`); !strings.Contains(loc, "Added+0+entries.+Updated+1+entry+already+in+the+list.") {
		t.Errorf("updating Alien: %s", loc)
	}
	if e := entry("Alien"); e.ID != 1 || e.URL != "" || e.Description != "Chestbursting" || len(c.load("films").Comparisons) != 1 {
		t.Errorf("Alien updated to %+v", e)
	}
	// A description left out stays, and a name alone changes nothing.
	add(`{"name": "Alien", "url": "https://example.org/alien"}`)
	if e := entry("Alien"); e.URL != "https://example.org/alien" || e.Description != "Chestbursting" {
		t.Errorf("Alien given only a link: %+v", e)
	}
	if _, loc := add("ALIEN"); !strings.Contains(loc, "Skipped+1+name+already+in+the+list.") {
		t.Errorf("adding Alien alone again: %s", loc)
	}
	if e := entry("Alien"); e.URL != "https://example.org/alien" || e.Description != "Chestbursting" {
		t.Errorf("Alien alone again: %+v", e)
	}
	if _, loc := add(`["Brazil", {"name": "Casablanca", "url": "example.com/c"}]`); !strings.Contains(loc, "Added+1+entry.+Skipped+1+name+already+in+the+list.") {
		t.Errorf("adding Brazil again and Casablanca: %s", loc)
	}
	// JSON that can't be read changes nothing, and the page shows the text
	// again, to fix.
	bad := "[\n  \"Dune\",\n  {\"name\": \"Kill Bill\", \"url\": \"Vol. 1\"}\n]"
	status, _, body := func() (int, http.Header, string) {
		req, _ := http.NewRequest("POST", c.srv.URL+base+"/entries", strings.NewReader(url.Values{"names": {bad}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return c.do(req)
	}()
	if status != http.StatusBadRequest || !strings.Contains(body, "Entry 2: &#34;Vol. 1&#34; isn&#39;t a web address") ||
		!strings.Contains(html.UnescapeString(body), ">"+bad+"</textarea>") || len(c.load("films").Entries) != 3 {
		t.Errorf("bad JSON: %d, %d entries\n%s", status, len(c.load("films").Entries), body)
	}
	// The entries page links each entry to its page and shows what it's about.
	_, body = c.get(base + "/entries")
	for _, want := range []string{`<a class="entry-link" href="https://example.org/alien" target="_blank" rel="noopener noreferrer"`,
		`<span class="muted small entry-desc" title="Chestbursting">Chestbursting</span>`, `href="https://example.com/c"`} {
		if !strings.Contains(body, want) {
			t.Errorf("entries page lacks %q", want)
		}
	}
	// The rating page shows what each entry of the pair is about, cut short
	// if long, with its link in the corner of its box.
	long := strings.Repeat("Very long indeed. ", 30)
	add(`[{"name": "Alien", "description": "` + long + `"}, {"name": "Brazil", "description": "Dystopia"}, {"name": "Casablanca", "url": "", "description": "Wartime romance"}]`)
	a, b, _, body := c.question(base)
	names := map[int]string{1: "Alien", 2: "Brazil", 3: "Casablanca"}
	descs := map[int]string{1: strings.TrimSpace(long), 2: "Dystopia", 3: "Wartime romance"}
	for _, id := range []int{a, b} {
		shown := descs[id]
		if id == 1 {
			shown = curtail(shown, 300)
		}
		if want := `<span class="choice-desc" title="` + descs[id] + `">` + shown + `</span>`; !strings.Contains(body, want) {
			t.Errorf("rating page lacks %q", want)
		}
	}
	if links := strings.Count(body, `class="entry-link"`); (a == 1 || b == 1) != (links == 1) {
		t.Errorf("asking %s vs %s, the page has %d links", names[a], names[b], links)
	}
	// The tier list links an entry's chip to its page, and says what it's
	// about on hover.
	_, body = c.get(base + "/tiers")
	for _, want := range []string{`<a class="chip" href="https://example.org/alien" target="_blank" rel="noopener noreferrer" title="` + strings.TrimSpace(long) + `">Alien</a>`,
		`<span class="chip" title="Dystopia">Brazil</span>`} {
		if !strings.Contains(body, want) {
			t.Errorf("tier list page lacks %q", want)
		}
	}
}

func TestCurtail(t *testing.T) {
	for _, c := range []struct {
		s    string
		n    int
		want string
	}{
		{"Short enough", 20, "Short enough"},
		{"Exactly twenty chars", 20, "Exactly twenty chars"},
		{"Cut at a space near the end, please", 30, "Cut at a space near the end…"},
		{"Averyveryverylongwordwithnospaces", 10, "Averyvery…"},
		{"Ünïcödé counts characters, not bytes", 10, "Ünïcödé…"},
		{"Ünïcödé counts characters, not bytes", 12, "Ünïcödé cou…"},
	} {
		if got := curtail(c.s, c.n); got != c.want || len([]rune(got)) > c.n {
			t.Errorf("curtail(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
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
	// The page then says where the two entries now stand, the winner in
	// the top third and the loser at the bottom, and how far the answer
	// moved them from where the list's order had them before.
	names := map[int]string{1: "Alien", 2: "Brazil", 3: "Casablanca"}
	was := map[int]int{1: 34, 2: 67, 3: 100}
	want := fmt.Sprintf("%s is now in the top 34%% (%s), %s in the top 100%% (%s).", names[a], signed(was[a]-34), names[b], signed(was[b]-100))
	// (The page writes + as &#43;.)
	if _, _, _, body := c.question(base); !strings.Contains(html.UnescapeString(body), want) {
		t.Errorf("no %q for the last answer:\n%s", want, body)
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

var shownRatings = regexp.MustCompile(`value="([^"]+)" required aria-label="Name"[^<]*>\s*</form>[^\x00]*?</div>\s*<span class="rating"[^>]*>(\d+) <span class="muted">± (\d+)</span>`)

func TestLevelsReadout(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Letters", "A", "B", "C", "D")
	warning := regexp.MustCompile(`levels? apart <span class="warn" role="img" aria-label="([^"]*)" title="[^"]*">`)
	readout := func(page string) (shown bool, warn string) {
		t.Helper()
		_, body := c.get(base + "/" + page)
		body = html.UnescapeString(body)
		if m := warning.FindStringSubmatch(body); m != nil {
			return true, m[1]
		}
		return strings.Contains(body, "levels apart") || strings.Contains(body, "level apart"), ""
	}
	// Shown from the start, but marked highly unreliable with no answers.
	for _, page := range []string{"rate", "tiers"} {
		if shown, warn := readout(page); !shown || warn != "Highly unreliable: the entries have 0.0 answers each on average, and this settles down once they have about 3." {
			t.Errorf("%s page with no answers: shown %v, warning %q", page, shown, warn)
		}
	}
	// Four answers among four entries make 2 each: unreliable.
	n := 0
	for _, p := range [][2]int{{1, 2}, {3, 4}, {1, 3}, {2, 4}} {
		c.answer(base, p[0], p[1], n, "a")
		n++
	}
	if shown, warn := readout("rate"); !shown || !strings.HasPrefix(warn, "Unreliable: the entries have 2.0 answers each") {
		t.Errorf("with 2 answers each: shown %v, warning %q", shown, warn)
	}
	// Two more make 3 each, enough for no warning.
	for _, p := range [][2]int{{1, 4}, {2, 3}} {
		c.answer(base, p[0], p[1], n, "a")
		n++
	}
	if shown, warn := readout("tiers"); !shown || warn != "" {
		t.Errorf("with 3 answers each: shown %v, warning %q", shown, warn)
	}
}

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

func TestEntriesJSON(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Kill Bill, Vol. 1", `The "Thing"`, "Zardoz", "Heat & Dust")
	c.post(base+"/entries", url.Values{"names": {`[{"name": "Alien", "url": "example.com/alien", "description": "Sci-fi <horror>"}]`}})
	c.answer(base, 1, 2, 0, "a")
	c.answer(base, 2, 3, 1, "same")
	c.answer(base, 3, 4, 2, "a")
	c.answer(base, 1, 3, 3, "a")
	c.post(base+"/entries/4/remove", nil)
	_, body := c.get(base + "/entries")
	const open = `aria-label="The entries as JSON">`
	i := strings.Index(body, open)
	if i < 0 || !strings.Contains(body, "<summary>JSON, to copy</summary>") {
		t.Fatalf("no JSON on the entries page:\n%s", body)
	}
	text, _, _ := strings.Cut(body[i+len(open):], "</textarea>")
	text = html.UnescapeString(text)
	// One entry a line, with & and < as they are.
	if !strings.HasPrefix(text, "[\n  {\"name\":\"Alien\",") || !strings.HasSuffix(text, "}\n]") || !strings.Contains(text, `"name":"Heat & Dust"`) ||
		!strings.Contains(text, `"description":"Sci-fi <horror>"`) || strings.Count(text, "\n") != 5 {
		t.Errorf("JSON:\n%s", text)
	}
	var entries []entryJSON
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("reading the JSON back: %v\n%s", err, text)
	}
	// The shown entries, best first, with the ratings and ± uncertainties
	// (one standard deviation) the page shows, the answer counts, and the
	// links and descriptions. Zardoz is removed, so it is left out.
	l := c.load("films")
	fit, err := l.Fit()
	if err != nil {
		t.Fatal(err)
	}
	want := l.Shown()
	slices.SortStableFunc(want, func(a, b tierlist.Entry) int { return cmp.Compare(fit.Rating(b.ID), fit.Rating(a.ID)) })
	if len(entries) != len(want) || len(want) != 4 || want[0].Name != "Alien" {
		t.Fatalf("JSON has %d entries for %v:\n%s", len(entries), want, text)
	}
	counts := l.Counts()
	for k, e := range want {
		got := entries[k]
		if got.Name != e.Name || fmt.Sprint(got.Rating) != fmt.Sprintf("%.0f", fit.Points(e.ID)) || fmt.Sprint(got.CIWidth) != fmt.Sprintf("%.0f", fit.PointsSD(e.ID)) ||
			got.Answers != counts[e.ID] || got.URL != e.URL || got.Description != e.Description || !strings.Contains(body, fmt.Sprintf("± %d<", got.CIWidth)) {
			t.Errorf("entry %d: %+v, want %s with rating %.1f ± %.1f and %d answers", k+1, got, e.Name, fit.Points(e.ID), fit.PointsSD(e.ID), counts[e.ID])
		}
	}
	// Pasted into another list, it adds the entries with their details,
	// leaving the ratings and answers behind.
	other := c.newList("Copy")
	if _, loc := c.post(other+"/entries", url.Values{"names": {text}}); !strings.Contains(loc, "Added+4+entries.") {
		t.Errorf("adding the JSON to another list: %s", loc)
	}
	copied := c.load("copy")
	if len(copied.Entries) != 4 || copied.Entries[0].URL != "https://example.com/alien" || copied.Entries[0].Description != "Sci-fi <horror>" || len(copied.Comparisons) != 0 {
		t.Errorf("copied entries %+v", copied.Entries)
	}
	// A list with no entries has no JSON.
	c.newList("Empty")
	if _, body := c.get("/lists/empty/entries"); strings.Contains(body, "JSON, to copy") {
		t.Errorf("JSON for an empty list:\n%s", body)
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
	// Three entries stand at 5/6, 1/2 and 1/6, so on 0–10 stars they get
	// 8, 5 and 2 stars: a list this short has nothing in the top or
	// bottom twentieth.
	status, body := c.get(base + "/tiers")
	if status != http.StatusOK || !strings.Contains(body, `<div class="tier" style="--hue: 250">
      <div class="tier-label">10★ (0)</div>`) ||
		!strings.Contains(body, "10★ (0):\n9★ (0):\n8★ (1): Alien\n7★ (0):\n6★ (0):\n5★ (1): Brazil\n4★ (0):\n3★ (0):\n2★ (1): Casablanca\n1★ (0):\n0★ (0):") {
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
	if status, _ := display("owl-newt", nil); status != http.StatusSeeOther {
		t.Errorf("choosing OWL/NEWT: %d", status)
	}
	// Outstanding is the top 31st, beyond the best of three entries, at
	// 5/6. Switching back to stars from here finds them as they were: 0–5.
	if _, body := c.get(base + "/tiers"); !strings.Contains(body, "Outstanding (0):\nExceeds Expectations (0):\nAcceptable (1): Alien\n") ||
		!strings.Contains(body, `name="maxStars" min="3" value="5"`) || !strings.Contains(body, `value="owl-newt" checked> OWL/NEWT`) {
		t.Errorf("OWL/NEWT tier list:\n%s", body)
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

func TestSigned(t *testing.T) {
	for d, want := range map[int]string{8: "+8", -8: "-8", 0: "±0", 100: "+100", -1: "-1"} {
		if got := signed(d); got != want {
			t.Errorf("signed(%d) = %q, want %q", d, got, want)
		}
	}
}

func TestLastMoves(t *testing.T) {
	l, err := tierlist.New("Films")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Alien", "Brazil", "Casablanca"} {
		l.AddEntry(name)
	}
	names := map[int]string{1: "Alien", 2: "Brazil", 3: "Casablanca"}
	if got := lastMoves(l, names, nil); got != "" {
		t.Errorf("with no answers: %q", got)
	}
	// Alien beat Brazil, then Casablanca beat Alien: Casablanca rises from
	// the top 66.7% to the top 33.3%, both rounded up.
	l.Record(1, 2, tierlist.FirstBetter)
	l.Record(3, 1, tierlist.FirstBetter)
	before := map[int]float64{1: 100.0 / 3, 3: 200.0 / 3, 2: 100}
	for _, c := range []struct {
		before map[int]float64
		want   string
	}{
		{before, "Casablanca is now in the top 34% (+33), Alien in the top 67% (-33)."},
		{nil, "Casablanca is now in the top 34%, Alien in the top 67%."},
		{map[int]float64{1: 200.0 / 3, 3: 100.0 / 3}, "Casablanca is now in the top 34% (±0), Alien in the top 67% (±0)."},
	} {
		if got := lastMoves(l, names, c.before); got != c.want {
			t.Errorf("lastMoves with %v = %q, want %q", c.before, got, c.want)
		}
	}
	// A removed entry is left out.
	l.RemoveEntry(1)
	if got := lastMoves(l, names, before); got != "Casablanca is now in the top 50% (+17)." {
		t.Errorf("with Alien removed: %q", got)
	}
}

// Where the latest answer moved its entries from is worked out for the
// entries as they are now, so adding one after the answer counts it.
func TestMovesAfterAddingEntries(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Letters", "A", "B")
	c.answer(base, 1, 2, 0, "a")
	if _, _, _, body := c.question(base); !strings.Contains(body, "A is now in the top 50% (±0), B in the top 100% (±0).") {
		t.Errorf("after A beat B:\n%s", body)
	}
	// Before the answer, C would have tied with A and B and come last, so
	// the answer moved B down from the top 67%.
	c.post(base+"/entries", url.Values{"names": {"C"}})
	if _, _, _, body := c.question(base); !strings.Contains(body, "A is now in the top 34% (±0), B in the top 100% (-33).") {
		t.Errorf("after adding C:\n%s", body)
	}
}

// The rating page says how far the latest answer moved its entries the same
// way whether it noted where they stood as the answer came in, or works it
// out again after an Undo or a restart.
func TestMovesAfterUndoAndRestart(t *testing.T) {
	dir := t.TempDir()
	_, c := start(t, dir)
	base := c.newList("Letters", "A", "B", "C", "D", "E")
	n := 0
	answer := func(a, b int) {
		t.Helper()
		if _, loc := c.answer(base, a, b, n, "a"); loc != base+"/rate" {
			t.Fatalf("answering %d over %d: %s", a, b, loc)
		}
		n++
	}
	line := regexp.MustCompile(`<p class="muted small" title="In brackets[^"]*">([^<]*)</p>`)
	moves := func(c *client) string {
		t.Helper()
		_, _, _, body := c.question(base)
		m := line.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no moves on the rating page:\n%s", body)
		}
		return html.UnescapeString(m[1])
	}
	answer(1, 2)
	answer(1, 2)
	answer(4, 3)
	third := moves(c)
	if !regexp.MustCompile(`^D is now in the top \d+% \(\+\d+\), C in the top \d+% \(-\d+\)\.$`).MatchString(third) {
		t.Errorf("after D beat C: %q", third)
	}
	answer(5, 1)
	if got := moves(c); !regexp.MustCompile(`^E is now in the top \d+% \(\+\d+\), A in the top \d+% \(-\d+\)\.$`).MatchString(got) {
		t.Errorf("after E beat A: %q", got)
	}
	c.post(base+"/undo", url.Values{"n": {strconv.Itoa(n)}})
	n--
	if got := moves(c); got != third {
		t.Errorf("after taking back E over A: %q, want %q as before", got, third)
	}
	_, c2 := start(t, dir)
	if got := moves(c2); got != third {
		t.Errorf("after a restart: %q, want %q", got, third)
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

func TestNamedTiers(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Letters", "A", "B", "C", "D", "E", "F", "G", "H")
	for k := 1; k < 8; k++ {
		c.answer(base, k, k+1, k-1, "a")
	}
	// display sends the form with named tiers, and returns the status and
	// either where it redirects to or the page it shows.
	display := func(extra url.Values) (int, string) {
		t.Helper()
		form := mergeForm(url.Values{"kind": {"named"}, "namedTiers": {"Top\n Good\n\nOkay\nWeak "}, "named-sizes": {"even"}})
		for k, v := range extra {
			form[k] = v
		}
		req, _ := http.NewRequest("POST", c.srv.URL+base+"/display", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		status, h, body := c.do(req)
		return status, cmp.Or(h.Get("Location"), body)
	}
	plain := func() string {
		t.Helper()
		_, body := c.get(base + "/tiers")
		m := regexp.MustCompile(`(?s)<textarea id="plain"[^>]*>(.*?)</textarea>`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no tier list:\n%s", body)
		}
		return html.UnescapeString(m[1])
	}
	// Four tiers sized as the nearest tier, as if they stood at 1, 2/3,
	// 1/3 and 0: cut-offs at 1/6, 1/2 and 5/6, so shares of 1/6, 1/3, 1/3
	// and 1/6. The eight entries stand at 15/16, 13/16, ..., 1/16, so the
	// tiers get 1, 3, 3 and 1 of them: their shares of eight, rounded.
	if status, loc := display(nil); status != http.StatusSeeOther || strings.Contains(loc, "err=") {
		t.Fatalf("choosing named tiers: %d, %s", status, loc)
	}
	if tm := c.load("letters").Display.Template; tm.Kind != "named" || !slices.Equal(tm.Tiers, []string{"Top", "Good", "Okay", "Weak"}) || tm.Sizes != "" {
		t.Errorf("saved template %+v", tm)
	}
	if got := plain(); got != "Top (1): A\nGood (3): B, C, D\nOkay (3): E, F, G\nWeak (1): H" {
		t.Errorf("nearest-tier tier list:\n%s", got)
	}
	_, body := c.get(base + "/tiers")
	for _, want := range []string{`value="named" checked> Named tiers`, `<option value="even" selected>Nearest tier</option>`,
		"Top\nGood\nOkay\nWeak</textarea>", `<title>Weak: 16.7% of the list</title>`} {
		if !strings.Contains(body, want) {
			t.Errorf("tier list page lacks %q", want)
		}
	}
	// Beta(1/2, 1/2) over four equal parts puts the cut-offs at 1/3, 1/2
	// and 2/3, so the end tiers get more entries.
	display(url.Values{"named-sizes": {"beta"}, "named-alpha": {"1/2"}, "named-beta": {"1/2"}})
	if got := plain(); got != "Top (3): A, B, C\nGood (1): D\nOkay (1): E\nWeak (3): F, G, H" {
		t.Errorf("Beta(1/2, 1/2) tier list:\n%s", got)
	}
	if _, body := c.get(base + "/tiers"); !strings.Contains(body, "The Beta(0.5, 0.5) density.") || strings.Contains(body, "★") {
		t.Errorf("named Beta tiers on the page:\n%s", body)
	}
	// Geometric sizes, each tier twice the one before from the best: 1, 2,
	// 4 and 8 fifteenths.
	display(url.Values{"named-sizes": {"geometric"}, "named-factor": {"2"}, "named-from": {"best"}})
	if got := plain(); got != "Top (1): A\nGood (1): B\nOkay (2): C, D\nWeak (4): E, F, G, H" {
		t.Errorf("geometric tier list:\n%s", got)
	}
	// A custom template has names of its own, and the named tiers keep
	// theirs, to come back to.
	c.post(base+"/display", mergeForm(url.Values{"kind": {"custom"}, "customTiers": {"Hot\nNot"}, "customCutoffs": {"1/2"},
		"namedTiers": {"Top\nGood\nOkay\nWeak"}, "named-sizes": {"geometric"}, "named-factor": {"2"}, "named-from": {"best"}}))
	_, body = c.get(base + "/tiers")
	for _, want := range []string{`name="customTiers" rows="6" placeholder="S&#10;A&#10;B&#10;C">Hot` + "\n" + `Not</textarea>`,
		`name="namedTiers" rows="6" placeholder="S&#10;A&#10;B&#10;C">Top` + "\n" + `Good` + "\n" + `Okay` + "\n" + `Weak</textarea>`,
		`value="custom" checked> Custom`, `name="named-factor" value="2"`} {
		if !strings.Contains(body, want) {
			t.Errorf("tier list page with a custom template lacks %q", want)
		}
	}
	for _, bad := range []url.Values{
		{"namedTiers": {" \n "}},
		{"named-sizes": {"beta"}, "named-alpha": {"0"}, "named-beta": {"1"}},
		{"named-sizes": {"geometric"}, "named-factor": {"none"}},
		{"named-sizes": {"cubes"}},
	} {
		if status, body := display(bad); status != http.StatusBadRequest || !strings.Contains(body, `class="note error"`) {
			t.Errorf("named tiers with %v: %d", bad, status)
		}
	}
	if tm := c.load("letters").Display.Template; tm.Kind != "custom" {
		t.Errorf("a bad form changed the saved template to %+v", tm)
	}
}

// formValues reads the display form on a tier list page as a browser would
// send it, with every field's value as the page shows it.
func formValues(t *testing.T, body string) url.Values {
	t.Helper()
	i := strings.Index(body, `class="display"`)
	if i < 0 {
		t.Fatalf("no display form:\n%s", body)
	}
	form := body[i : i+strings.Index(body[i:], "</form>")]
	attr := func(attrs, name string) string {
		if m := regexp.MustCompile(`\b` + name + `="([^"]*)"`).FindStringSubmatch(attrs); m != nil {
			return html.UnescapeString(m[1])
		}
		return ""
	}
	v := url.Values{}
	for _, m := range regexp.MustCompile(`<input ([^>]*)>`).FindAllStringSubmatch(form, -1) {
		switch name := attr(m[1], "name"); attr(m[1], "type") {
		case "radio", "checkbox":
			if strings.Contains(m[1]+" ", " checked ") {
				v.Add(name, cmp.Or(attr(m[1], "value"), "on"))
			}
		default:
			v.Add(name, attr(m[1], "value"))
		}
	}
	for _, m := range regexp.MustCompile(`(?s)<select name="([^"]+)">(.*?)</select>`).FindAllStringSubmatch(form, -1) {
		options := regexp.MustCompile(`<option value="([^"]*)"( selected)?>`).FindAllStringSubmatch(m[2], -1)
		chosen := options[0][1]
		for _, o := range options {
			if o[2] != "" {
				chosen = o[1]
			}
		}
		v.Add(m[1], chosen)
	}
	for _, m := range regexp.MustCompile(`(?s)<textarea name="([^"]+)"[^>]*>(.*?)</textarea>`).FindAllStringSubmatch(form, -1) {
		v.Add(m[1], html.UnescapeString(m[2]))
	}
	return v
}

// Each kind of template keeps the options it last had, so that switching
// to another kind and back finds them as they were.
func TestRememberedTemplates(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Letters", "A", "B", "C", "D")
	// submit sends the form as the page has it, with the given changes.
	submit := func(changes url.Values) {
		t.Helper()
		_, body := c.get(base + "/tiers")
		form := formValues(t, body)
		for k, v := range changes {
			form[k] = v
		}
		if status, loc := c.post(base+"/display", form); status != http.StatusSeeOther || strings.Contains(loc, "err=") {
			_, page := c.get(base + "/tiers")
			t.Fatalf("submitting %v: %d, %s\n%s", changes, status, loc, page)
		}
	}
	// A new list has the defaults, and named tiers and custom templates
	// wait to be filled in.
	_, body := c.get(base + "/tiers")
	for _, want := range []string{`name="alpha" value="2"`, `name="named-alpha" value="2"`, `name="customName" value="Custom"`,
		`value="named" data-wait>`, `value="custom" data-wait>`} {
		if !strings.Contains(body, want) {
			t.Errorf("a new list's tier list page lacks %q", want)
		}
	}
	submit(url.Values{"sizes": {"beta"}, "alpha": {"5"}, "beta": {"2"}})
	// Off to a custom template for a while...
	submit(url.Values{"kind": {"custom"}, "customName": {"Halves"}, "customTiers": {"Hot\nNot"}, "customCutoffs": {"1/2"}})
	d := c.load("letters").Display
	if stars, ok := d.Remembered("stars"); d.Template.Kind != "custom" || !ok || stars.Sizes != "beta" || stars.Alpha != "5" || stars.Beta != "2" {
		t.Errorf("with a custom template in use: %+v", d)
	}
	// ...and back to stars, which are Beta(5, 2) again, with nothing else
	// done than choosing them.
	submit(url.Values{"kind": {"stars"}})
	if tm := c.load("letters").Display.Template; tm.Kind != "stars" || tm.Sizes != "beta" || tm.Alpha != "5" || tm.Beta != "2" {
		t.Errorf("back to stars: %+v", tm)
	}
	// So is the custom template, which now applies as soon as it is chosen.
	_, body = c.get(base + "/tiers")
	if !strings.Contains(body, `value="custom">`) || !strings.Contains(body, "Hot\nNot</textarea>") {
		t.Errorf("stars in use, with a custom template kept:\n%s", body)
	}
	submit(url.Values{"kind": {"custom"}})
	if tm := c.load("letters").Display.Template; tm.Kind != "custom" || tm.Name != "Halves" || !slices.Equal(tm.Cutoffs, []string{"1/2"}) {
		t.Errorf("back to the custom template: %+v", tm)
	}
	// Options changed for a kind not chosen are kept if they make a usable
	// template, and otherwise the ones it had stay.
	submit(url.Values{"kind": {"stars"}, "namedTiers": {"S\nA\nB"}, "named-sizes": {"geometric"}, "named-factor": {"3"},
		"customCutoffs": {"not a cut-off"}})
	d = c.load("letters").Display
	named, okNamed := d.Remembered("named")
	custom, okCustom := d.Remembered("custom")
	if !okNamed || !slices.Equal(named.Tiers, []string{"S", "A", "B"}) || named.Factor != "3" || !okCustom || !slices.Equal(custom.Cutoffs, []string{"1/2"}) {
		t.Errorf("kept options: named %+v, custom %+v", named, custom)
	}
	// Another list starts with the defaults.
	other := c.newList("Others", "X", "Y")
	if _, body := c.get(other + "/tiers"); !strings.Contains(body, `name="alpha" value="2"`) || !strings.Contains(body, `value="custom" data-wait>`) {
		t.Errorf("a second list doesn't start with the defaults:\n%s", body)
	}
	if d := c.load("others").Display; len(d.Others) != 0 {
		t.Errorf("a new list keeps options: %+v", d.Others)
	}
}

// exported returns the text of the file of the list at base, as its
// export page shows it.
func (c *client) exported(base string) string {
	c.t.Helper()
	_, body := c.get(base + "/export")
	m := regexp.MustCompile(`(?s)<textarea class="file-text"[^>]*>(.*?)</textarea>`).FindStringSubmatch(body)
	if m == nil {
		c.t.Fatalf("export page:\n%s", body)
	}
	return html.UnescapeString(m[1])
}

// A list can be imported into another, which gains its entries and their
// answers and keeps its own settings.
func TestImportInto(t *testing.T) {
	_, c := start(t, t.TempDir())
	films := c.newList("Films", "Alien", "Brazil (1)")
	c.answer(films, 1, 2, 0, "a")
	c.post(films+"/question", url.Values{"question": {"Which would you rather watch?"}})
	home := c.newList("Home", `[{"name": "alien", "description": "Sci-fi horror"}, "Dune", "Eraserhead (2)"]`)
	c.answer(home, 1, 2, 0, "b")
	c.answer(home, 2, 3, 1, "same")
	text := c.exported(home)

	if _, body := c.get("/"); !strings.Contains(body, `<option value="films">into Films</option>`) || !strings.Contains(body, `<option value="home">into Home</option>`) {
		t.Errorf("no lists to import into on the start page:\n%s", body)
	}
	// alien is taken, in any case, and names end in (1) and (2) already.
	status, loc := c.post("/lists/import", url.Values{"data": {text}, "into": {"films"}})
	if status != http.StatusSeeOther || !strings.HasPrefix(loc, "/lists/films/entries?msg=") ||
		!strings.Contains(loc, url.QueryEscape("Imported 3 entries and 2 answers from “Home”. 1 entry whose name was taken got “ (3)” added.")) {
		t.Errorf("importing Home into Films: %d, %s", status, loc)
	}
	l := c.load("films")
	var names []string
	for _, e := range l.Entries {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"Alien", "Brazil (1)", "alien (3)", "Dune", "Eraserhead (2)"}) || l.Entries[2].Description != "Sci-fi horror" ||
		!slices.Equal(l.Comparisons, []tierlist.Comparison{{A: 3, B: 4, Answer: "b"}, {A: 4, B: 5, Answer: "same"}, {A: 1, B: 2, Answer: "a"}}) ||
		l.Asks() != "Which would you rather watch?" {
		t.Errorf("Films after the import: %q, %v, asks %q", names, l.Comparisons, l.Asks())
	}
	if len(c.load("home").Entries) != 3 {
		t.Error("the imported list changed")
	}
	// Text that isn't a list changes nothing, and the start page shows it
	// again, still to go into the same list.
	status, body := func() (int, string) {
		req, _ := http.NewRequest("POST", c.srv.URL+"/lists/import", strings.NewReader(url.Values{"data": {"{}"}, "into": {"films"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		status, _, body := c.do(req)
		return status, body
	}()
	if status != http.StatusBadRequest || !strings.Contains(body, "That can&#39;t be imported") || !strings.Contains(body, `<option value="films" selected>into Films</option>`) ||
		len(c.load("films").Entries) != 5 {
		t.Errorf("importing {} into Films: %d\n%s", status, body)
	}
	if status, _ := c.post("/lists/import", url.Values{"data": {text}, "into": {"nowhere"}}); status != http.StatusBadRequest {
		t.Errorf("importing into a missing list: %d", status)
	}
	// As a new list, as before.
	if _, loc := c.post("/lists/import", url.Values{"data": {text}, "into": {""}}); !strings.Contains(loc, "Imported+%E2%80%9CHome+%282%29%E2%80%9D") {
		t.Errorf("importing Home as a new list: %s", loc)
	}
}

// The answers about ticked entries can be forgotten, after a dialog says
// how many there are.
func TestForgetAnswersPage(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil", "Casablanca", "Dune")
	c.answer(base, 1, 2, 0, "a")
	c.answer(base, 2, 3, 1, "a")
	c.answer(base, 3, 4, 2, "a")
	c.answer(base, 1, 4, 3, "b")
	_, body := c.get(base + "/entries?ask=forget&focus=2&focus=9")
	for _, want := range []string{`<dialog id="forget" class="confirm" aria-labelledby="forget-title" open data-modal>`,
		"Forget the answers about Brazil?", "Every answer about Brazil goes, 2 answers in all, whatever it was compared with, and its rating starts again at 1500.",
		`<input type="hidden" name="id" value="2">`,
		`value="2" aria-label="Tick Brazil" checked>`, "Forget 2 answers</button>"} {
		if !strings.Contains(body, want) {
			t.Errorf("forget dialog lacks %q", want)
		}
	}
	if strings.Contains(body, `aria-label="Tick Alien" checked`) {
		t.Error("Alien is ticked too")
	}
	for query, want := range map[string]string{"ask=forget": "Tick the entries whose answers to forget first.", "ask=forget&focus=9": "Tick the entries whose answers to forget first."} {
		if _, body := c.get(base + "/entries?" + query); !strings.Contains(body, want) || strings.Contains(body, `<dialog id="forget"`) {
			t.Errorf("%s: no %q", query, want)
		}
	}
	status, loc := c.post(base+"/entries/forget", url.Values{"id": {"2"}})
	if status != http.StatusSeeOther || !strings.Contains(loc, "msg=Forgot+2+answers+about+Brazil%2C+which+starts+at+1500+again.") {
		t.Errorf("forgetting Brazil's answers: %d, %s", status, loc)
	}
	if got := c.load("films").Comparisons; !slices.Equal(got, []tierlist.Comparison{{A: 3, B: 4, Answer: "a"}, {A: 1, B: 4, Answer: "b"}}) {
		t.Errorf("answers left: %v", got)
	}
	if _, body := c.get(base + "/entries?ask=forget&focus=1&focus=3"); !strings.Contains(body, "Every answer about Alien and Casablanca goes, 2 answers in all, whatever they were compared with, and their ratings start again at 1500.") {
		t.Errorf("forgetting two entries' answers:\n%s", body)
	}
	if _, body := c.get(base + "/entries?ask=forget&focus=2"); !strings.Contains(body, "Brazil has no answers to forget.") {
		t.Errorf("forgetting again:\n%s", body)
	}
	for _, form := range []url.Values{{}, {"id": {"x"}}, {"id": {"9"}}} {
		if _, loc := c.post(base+"/entries/forget", form); !strings.Contains(loc, "err=") || len(c.load("films").Comparisons) != 2 {
			t.Errorf("forgetting %v: %s", form, loc)
		}
	}
}

// Ticked entries can be merged, keeping the details the dialog chooses.
func TestMergeEntriesPage(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", `[{"name": "Alien", "description": "Sci-fi"}, "Brazil",
		{"name": "Alien (1)", "url": "https://example.com/alien", "description": "Horror"}, "Dune"]`)
	c.answer(base, 1, 2, 0, "a")
	c.answer(base, 3, 2, 1, "a")
	c.answer(base, 1, 3, 2, "same")
	// The defaults: the name without a number, the link there is, and
	// the description from the entry whose name has no number.
	_, body := c.get(base + "/entries?ask=merge&focus=3&focus=1")
	for _, want := range []string{`<dialog id="merge" class="confirm merge" aria-labelledby="merge-title" open data-modal>`,
		"Merge Alien and Alien (1)?", "apart from the 1 answer between them, which go",
		`<input type="radio" name="name" value="1" checked> <span class="merge-value">Alien</span>`,
		`<input type="radio" name="name" value="3"> <span class="merge-value">Alien (1)</span>`,
		`<input type="radio" name="url" value="1"> <span class="merge-value"><span class="muted">none</span></span>`,
		`<input type="radio" name="url" value="3" checked> <span class="merge-value">https://example.com/alien</span>`,
		`<input type="radio" name="description" value="1" checked> <span class="merge-value">Sci-fi</span>`,
		`<input type="radio" name="description" value="3"> <span class="merge-value">Horror</span>`} {
		if !strings.Contains(body, want) {
			t.Errorf("merge dialog lacks %q", want)
		}
	}
	// A detail all the entries share is shown, not asked.
	if _, body := c.get(base + "/entries?ask=merge&focus=2&focus=4"); !strings.Contains(body, `<input type="hidden" name="url" value="2">`) ||
		!strings.Contains(body, `<p class="merge-value"><span class="muted">none</span></p>`) || strings.Contains(body, "between them") {
		t.Errorf("merging Brazil and Dune:\n%s", body)
	}
	if _, body := c.get(base + "/entries?ask=merge&focus=1"); !strings.Contains(body, "Tick two or more entries to merge first.") {
		t.Errorf("merging one entry:\n%s", body)
	}
	// A removed entry can't be ticked, and so can't be merged.
	c.post(base+"/entries/4/remove", nil)
	if _, body := c.get(base + "/entries?ask=merge&focus=2&focus=4"); !strings.Contains(body, "Tick two or more entries to merge first.") {
		t.Errorf("merging a removed entry:\n%s", body)
	}
	c.post(base+"/entries/4/restore", nil)
	// Choosing a value from an entry not being merged changes nothing.
	if _, loc := c.post(base+"/entries/merge", url.Values{"id": {"1", "3"}, "name": {"2"}, "url": {"3"}, "description": {"1"}}); !strings.Contains(loc, "err=Choose+which+name") ||
		len(c.load("films").Entries) != 4 {
		t.Errorf("merging with Brazil's name: %s", loc)
	}
	status, loc := c.post(base+"/entries/merge", url.Values{"id": {"1", "3"}, "name": {"1"}, "url": {"3"}, "description": {"3"}})
	if status != http.StatusSeeOther || !strings.Contains(loc, "msg=Merged+Alien+and+Alien+%281%29+into+Alien.+The+1+answer+between+them+went.") {
		t.Errorf("merging the Aliens: %d, %s", status, loc)
	}
	l := c.load("films")
	for i := range l.Entries {
		l.Entries[i].Rating = 0 // only the last fit's, to speed up the next
	}
	if want := []tierlist.Entry{{ID: 1, Name: "Alien", URL: "https://example.com/alien", Description: "Horror"}, {ID: 2, Name: "Brazil"}, {ID: 4, Name: "Dune"}}; !reflect.DeepEqual(l.Entries, want) ||
		!slices.Equal(l.Comparisons, []tierlist.Comparison{{A: 1, B: 2, Answer: "a"}, {A: 1, B: 2, Answer: "a"}}) {
		t.Errorf("merged list: %+v, %v", l.Entries, l.Comparisons)
	}
	if status, _ := c.get(base + "/rate"); status != http.StatusOK {
		t.Errorf("rating page after the merge: %d", status)
	}
}

// A list can ask its own question about each pair on the rating page.
func TestQuestion(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil")
	asks := func() string {
		t.Helper()
		_, body := c.get(base + "/rate")
		m := regexp.MustCompile(`<h1 class="question">(.*?)</h1>`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no question on the rating page:\n%s", body)
		}
		return m[1]
	}
	if got := asks(); got != "Which is better?" {
		t.Errorf("a new list asks %q", got)
	}
	if _, body := c.get(base + "/entries"); !strings.Contains(body, `<input name="question" class="wide" value="" placeholder="Which is better?"`) {
		t.Errorf("no question field on the entries page:\n%s", body)
	}
	status, loc := c.post(base+"/question", url.Values{"question": {"  Which is funnier?  "}})
	if status != http.StatusSeeOther || !strings.Contains(loc, "/entries?msg=The+rating+page+now+asks+%E2%80%9CWhich+is+funnier%3F%E2%80%9D") {
		t.Errorf("setting the question: %d, %s", status, loc)
	}
	if got := asks(); got != "Which is funnier?" || c.load("films").Question != "Which is funnier?" {
		t.Errorf("asks %q, saved %q", got, c.load("films").Question)
	}
	if _, body := c.get(base + "/entries"); !strings.Contains(body, `<input name="question" class="wide" value="Which is funnier?"`) {
		t.Errorf("the entries page doesn't show the question:\n%s", body)
	}
	// It is text, not HTML.
	c.post(base+"/question", url.Values{"question": {"Which is <b>odder</b> & why?"}})
	if got := asks(); got != "Which is &lt;b&gt;odder&lt;/b&gt; &amp; why?" {
		t.Errorf("asks %q", got)
	}
	// Left empty, it asks the default question again.
	c.post(base+"/question", url.Values{"question": {""}})
	if got := asks(); got != "Which is better?" || c.load("films").Question != "" {
		t.Errorf("with the question cleared: asks %q, saved %q", got, c.load("films").Question)
	}
}

// plainTiers returns the tier list of the list at base, as its plain text.
func (c *client) plainTiers(base string) string {
	c.t.Helper()
	_, body := c.get(base + "/tiers")
	m := regexp.MustCompile(`(?s)<textarea id="plain"[^>]*>(.*?)</textarea>`).FindStringSubmatch(body)
	if m == nil {
		c.t.Fatalf("no tier list:\n%s", body)
	}
	return html.UnescapeString(m[1])
}

// Rating-proportional tiers split the range of ratings, from the lowest to
// the highest, rather than the entries.
func TestRatingProportionalTiers(t *testing.T) {
	_, c := start(t, t.TempDir())
	base := c.newList("Films", "Alien", "Brazil", "Casablanca", "Dune")
	// Alien beats the rest, which are all about the same as each other.
	for n, p := range [][2]int{{1, 2}, {1, 3}, {1, 4}} {
		c.answer(base, p[0], p[1], n, "a")
	}
	for n, p := range [][2]int{{2, 3}, {3, 4}, {4, 2}} {
		c.answer(base, p[0], p[1], 3+n, "same")
	}
	thirds := func(proportional string) url.Values {
		return mergeForm(url.Values{"kind": {"custom"}, "customName": {"Thirds"}, "customTiers": {"Top\nMiddle\nBottom"},
			"customCutoffs": {"1/3, 2/3"}, "proportional": {proportional}})
	}
	apply := func(proportional string) {
		t.Helper()
		if status, loc := c.post(base+"/display", thirds(proportional)); status != http.StatusSeeOther || strings.Contains(loc, "err=") {
			t.Fatalf("applying %s-proportional tiers: %d, %s", proportional, status, loc)
		}
	}
	// By rank, the three tied entries are one group, at 2/3, 1/3 and 0,
	// and go where the middle one would.
	apply("entry")
	if got := c.plainTiers(base); !strings.HasPrefix(got, "Top (1): Alien\nMiddle (3): ") || !strings.HasSuffix(got, "\nBottom (0):") {
		t.Errorf("entry-proportional tier list:\n%s", got)
	}
	// By rating, they share the lowest rating.
	apply("rating")
	if got := c.plainTiers(base); !strings.HasPrefix(got, "Top (1): Alien\nMiddle (0):\nBottom (3): ") {
		t.Errorf("rating-proportional tier list:\n%s", got)
	}
	_, body := c.get(base + "/tiers")
	for _, want := range []string{`<option value="rating" selected>rating-proportional</option>`,
		`<title>Top: 33.3% of the rating range</title>`, "the tier&#39;s share of the range of ratings, from the lowest to the highest"} {
		if !strings.Contains(body, want) {
			t.Errorf("rating-proportional page lacks %q", want)
		}
	}
	if d := c.load("films").Display; d.Proportional != "rating" {
		t.Errorf("saved as %+v", d)
	}
	// The chart shows a change not yet applied as such.
	if status, chart := c.get(base + "/chart?" + thirds("entry").Encode()); status != http.StatusOK ||
		!strings.Contains(chart, "Not applied yet") || !strings.Contains(chart, "<title>Top: 33.3% of the list</title>") {
		t.Errorf("chart for entry-proportional tiers, not applied: %d\n%s", status, chart)
	}
	if _, chart := c.get(base + "/chart?" + thirds("rating").Encode()); strings.Contains(chart, "Not applied yet") {
		t.Errorf("chart for the tiers as they are:\n%s", chart)
	}
	// Entry-proportional, the default, is left out of the file.
	apply("entry")
	if d := c.load("films").Display; d.Proportional != "" {
		t.Errorf("entry-proportional saved as %q", d.Proportional)
	}
	if status, _ := c.post(base+"/display", thirds("sideways")); status != http.StatusBadRequest {
		t.Errorf("sideways-proportional tiers: %d", status)
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
	for _, want := range []string{`<option value="even" selected>Nearest tier</option>`, `name="factor" value="1.618033988749895"`, `<div class="for-beta" data-wait>`,
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
