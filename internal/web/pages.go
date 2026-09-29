package web

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/samtcifihi/tierlist/internal/tier"
	"github.com/samtcifihi/tierlist/internal/tierlist"
)

type libraryView struct {
	view
	Dir   string
	Lists []listSummary
}

type listSummary struct {
	Name, URL, File, Modified, Err string
	Entries, Answers               int
}

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	sums, err := tierlist.Lists(s.dir)
	if err != nil {
		s.message(w, http.StatusInternalServerError, "Your lists can't be read", err.Error())
		return
	}
	v := libraryView{view: s.view(r, "library", nil), Dir: s.dir}
	for _, sm := range sums {
		file := filepath.Base(sm.Path)
		item := listSummary{
			Name: sm.Name, URL: listURL(strings.TrimSuffix(file, ".json")), File: file,
			Entries: sm.Entries, Answers: sm.Comparisons, Modified: sm.Modified.Format("2 Jan 2006, 15:04"),
		}
		if sm.Err != nil {
			item.Err = sm.Err.Error()
		}
		v.Lists = append(v.Lists, item)
	}
	s.render(w, http.StatusOK, "library", v)
}

func (s *Server) createList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, _, err := tierlist.Create(s.dir, r.FormValue("name"))
	if err != nil {
		back(w, r, "/", "", err.Error())
		return
	}
	back(w, r, listURL(strings.TrimSuffix(filepath.Base(path), ".json"))+"/entries", "", "")
}

func (s *Server) listHome(w http.ResponseWriter, r *http.Request, ol *openList) {
	page := "/entries"
	if len(ol.list.Shown()) >= 2 {
		page = "/rate"
	}
	http.Redirect(w, r, listURL(ol.key)+page, http.StatusSeeOther)
}

func (s *Server) renameList(w http.ResponseWriter, r *http.Request, ol *openList) {
	entries := listURL(ol.key) + "/entries"
	if err := ol.list.SetName(r.FormValue("name")); err != nil {
		back(w, r, entries, "", err.Error())
		return
	}
	if !s.saved(w, ol) {
		return
	}
	back(w, r, entries, "Renamed the list.", "")
}

// saved saves the list, showing an error page and reporting false if that
// fails.
func (s *Server) saved(w http.ResponseWriter, ol *openList) bool {
	if err := s.save(ol); err != nil {
		s.message(w, http.StatusInternalServerError, "The change could not be saved", err.Error())
		return false
	}
	return true
}

type rateView struct {
	view
	Ready         bool
	First, Second tierlist.Entry
	Token         int    // the number of answers the page was made with
	Undo          string // the answer Undo would take back
	Answers       int
	DrawElo       string
	Levels        string
	Focus         []string // names of the entries in focus mode
}

func (s *Server) rate(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	names := entryNames(l)
	v := rateView{view: s.view(r, "rate", ol), Answers: len(l.Comparisons), Token: len(l.Comparisons)}
	for _, id := range l.Focus {
		v.Focus = append(v.Focus, names[id])
	}
	if k := len(l.Comparisons); k > 0 {
		v.Undo = describe(l.Comparisons[k-1], names)
	}
	if len(l.Shown()) >= 2 {
		if !pairShowable(l, ol.pair) {
			a, b, err := l.NextPair(s.rng)
			if err != nil {
				s.message(w, http.StatusInternalServerError, "No pair to compare", err.Error())
				return
			}
			ol.pair = [2]int{a, b}
		}
		fit, err := l.Fit()
		if err != nil {
			s.message(w, http.StatusInternalServerError, "The ratings can't be worked out", err.Error())
			return
		}
		v.Ready = true
		v.First, v.Second = entryByID(l, ol.pair[0]), entryByID(l, ol.pair[1])
		v.DrawElo = fmt.Sprintf("%.0f", fit.DrawElo())
		v.Levels = levelsText(l)
	}
	s.render(w, http.StatusOK, "rate", v)
}

// pairShowable reports whether the rating page can keep showing pair: two
// different entries that are not removed, including a focused one in focus
// mode.
func pairShowable(l *tierlist.List, pair [2]int) bool {
	shown := make(map[int]bool)
	for _, e := range l.Shown() {
		shown[e.ID] = true
	}
	a, b := pair[0], pair[1]
	return shown[a] && shown[b] && a != b &&
		(len(l.Focus) == 0 || slices.Contains(l.Focus, a) || slices.Contains(l.Focus, b))
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	rate := listURL(ol.key) + "/rate"
	n, errN := strconv.Atoi(r.FormValue("n"))
	a, errA := strconv.Atoi(r.FormValue("a"))
	b, errB := strconv.Atoi(r.FormValue("b"))
	if errN != nil || errA != nil || errB != nil {
		http.Error(w, "incomplete answer", http.StatusBadRequest)
		return
	}
	// An answer from a page made before the latest answer (a double click,
	// or the Back button) is dropped rather than recorded twice.
	if n != len(l.Comparisons) {
		back(w, r, rate, "That answer came from an out-of-date page, so it was ignored.", "")
		return
	}
	if err := l.Record(a, b, tierlist.Answer(r.FormValue("answer"))); err != nil {
		back(w, r, rate, "", err.Error())
		return
	}
	if !s.saved(w, ol) {
		return
	}
	ol.pair = [2]int{}
	back(w, r, rate, "", "")
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	rate := listURL(ol.key) + "/rate"
	if n, err := strconv.Atoi(r.FormValue("n")); err != nil || n != len(l.Comparisons) {
		back(w, r, rate, "That undo came from an out-of-date page, so it was ignored.", "")
		return
	}
	names := entryNames(l)
	c, ok := l.Undo()
	if !ok {
		back(w, r, rate, "", "")
		return
	}
	if !s.saved(w, ol) {
		return
	}
	// Ask the same question again, the same way round.
	ol.pair = [2]int{c.A, c.B}
	back(w, r, rate, "Took back "+describe(c, names)+". Answer it again.", "")
}

// describe puts an answer into words.
func describe(c tierlist.Comparison, names map[int]string) string {
	switch c.Answer {
	case tierlist.FirstBetter:
		return fmt.Sprintf("%s over %s", names[c.A], names[c.B])
	case tierlist.SecondBetter:
		return fmt.Sprintf("%s over %s", names[c.B], names[c.A])
	}
	return fmt.Sprintf("%s and %s about the same", names[c.A], names[c.B])
}

func levelsText(l *tierlist.List) string {
	levels, ready, err := l.Levels()
	switch {
	case err != nil:
		return ""
	case !ready:
		return fmt.Sprintf("the levels readout appears once every entry has %d answers", tierlist.LevelsAfter)
	}
	return fmt.Sprintf("you're telling about %.0f levels apart", levels)
}

type entriesView struct {
	view
	Shown   []entryRow
	Removed []entryRow
	Focus   bool
}

type entryRow struct {
	ID      int
	Name    string
	Rating  string
	SD      string
	Answers int
	Focused bool
}

func (s *Server) entries(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	fit, err := l.Fit()
	if err != nil {
		s.message(w, http.StatusInternalServerError, "The ratings can't be worked out", err.Error())
		return
	}
	counts := l.Counts()
	v := entriesView{view: s.view(r, "entries", ol), Focus: len(l.Focus) > 0}
	for _, e := range l.Entries {
		row := entryRow{
			ID: e.ID, Name: e.Name, Answers: counts[e.ID], Focused: slices.Contains(l.Focus, e.ID),
			Rating: shownRating(fit.Rating(e.ID)), SD: fmt.Sprintf("± %.0f", fit.SD(e.ID)),
		}
		if e.Removed {
			v.Removed = append(v.Removed, row)
		} else {
			v.Shown = append(v.Shown, row)
		}
	}
	slices.SortStableFunc(v.Shown, func(a, b entryRow) int { return cmp.Compare(fit.Rating(b.ID), fit.Rating(a.ID)) })
	s.render(w, http.StatusOK, "entries", v)
}

// ratingCenter is added to every rating the pages show, so that the dummy
// entry, and any entry not yet compared, shows as 1500. Internally ratings
// are relative to the dummy at 0. Differences between ratings, such as the
// uncertainty, the draw setting and the draw-margin, are not shifted.
const ratingCenter = 1500

// shownRating formats a rating for the pages, as a whole number.
func shownRating(x float64) string {
	return fmt.Sprintf("%.0f", math.Round(ratingCenter+x))
}

func (s *Server) addEntries(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	have := make(map[string]bool)
	for _, e := range l.Shown() {
		have[strings.ToLower(e.Name)] = true
	}
	added, skipped := 0, 0
	for _, line := range strings.Split(r.FormValue("names"), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if have[strings.ToLower(name)] {
			skipped++
			continue
		}
		have[strings.ToLower(name)] = true
		if _, err := l.AddEntry(name); err != nil {
			back(w, r, listURL(ol.key)+"/entries", "", err.Error())
			return
		}
		added++
	}
	if added > 0 && !s.saved(w, ol) {
		return
	}
	msg := fmt.Sprintf("Added %s.", plural(added, "entry", "entries"))
	if skipped > 0 {
		msg += fmt.Sprintf(" Skipped %s already in the list.", plural(skipped, "name", "names"))
	}
	back(w, r, listURL(ol.key)+"/entries", msg, "")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// entryAction handles a form about one entry: it applies change to the
// entry named in the path, saves, and returns to the entries page.
func (s *Server) entryAction(change func(l *tierlist.List, id int, r *http.Request) (string, error)) func(http.ResponseWriter, *http.Request, *openList) {
	return func(w http.ResponseWriter, r *http.Request, ol *openList) {
		entries := listURL(ol.key) + "/entries"
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "no such entry", http.StatusNotFound)
			return
		}
		msg, err := change(ol.list, id, r)
		if err != nil {
			back(w, r, entries, "", err.Error())
			return
		}
		if !s.saved(w, ol) {
			return
		}
		back(w, r, entries, msg, "")
	}
}

func (s *Server) renameEntry(w http.ResponseWriter, r *http.Request, ol *openList) {
	s.entryAction(func(l *tierlist.List, id int, r *http.Request) (string, error) {
		return "", l.RenameEntry(id, r.FormValue("name"))
	})(w, r, ol)
}

func (s *Server) removeEntry(w http.ResponseWriter, r *http.Request, ol *openList) {
	s.entryAction(func(l *tierlist.List, id int, r *http.Request) (string, error) {
		if err := l.RemoveEntry(id); err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed %s. Its answers still count, and you can restore it at the bottom of the page.",
			entryNames(l)[id]), nil
	})(w, r, ol)
}

func (s *Server) restoreEntry(w http.ResponseWriter, r *http.Request, ol *openList) {
	s.entryAction(func(l *tierlist.List, id int, r *http.Request) (string, error) {
		if err := l.RestoreEntry(id); err != nil {
			return "", err
		}
		return fmt.Sprintf("Restored %s.", entryNames(l)[id]), nil
	})(w, r, ol)
}

func (s *Server) setFocus(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	base := listURL(ol.key)
	from := base + "/entries"
	if r.FormValue("from") == "rate" {
		from = base + "/rate"
	}
	var ids []int
	if r.FormValue("do") != "clear" {
		for _, v := range r.Form["focus"] {
			id, err := strconv.Atoi(v)
			if err != nil {
				http.Error(w, "bad entry", http.StatusBadRequest)
				return
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			back(w, r, from, "", "Tick the entries to focus on first.")
			return
		}
	}
	if err := l.SetFocus(ids); err != nil {
		back(w, r, from, "", err.Error())
		return
	}
	if !s.saved(w, ol) {
		return
	}
	if len(ids) == 0 {
		back(w, r, from, "Focus mode is off.", "")
		return
	}
	back(w, r, base+"/rate", "Focus mode is on: every pair includes one of the ticked entries.", "")
}

type tiersView struct {
	view
	Form     displayForm
	Rows     []tierRow
	Text     string // the tier list as plain text
	TextRows int    // lines for its text box, with room for a scroll bar
	Empty    string // why there is no tier list
	DrawElo  string
	Levels   string
}

type tierRow struct {
	Name    string
	Hue     int
	Entries []string
}

// displayForm holds the display options as the form shows them.
type displayForm struct {
	Kind          string
	MaxStars      string
	SkipZero      bool
	Divisions     string
	CustomName    string
	CustomTiers   string
	CustomCutoffs string
	Convention    string
	DrawMargin    string
	GroupRule     string
	Prefer        string
}

func formFor(d tierlist.Display) displayForm {
	f := displayForm{
		Kind: d.Template.Kind, MaxStars: "5", Divisions: "1", CustomName: "Custom",
		Convention: d.Convention, DrawMargin: strconv.FormatFloat(d.DrawMargin, 'f', -1, 64),
		GroupRule: d.GroupRule, Prefer: d.Prefer,
	}
	switch t := d.Template; t.Kind {
	case "stars":
		f.MaxStars, f.SkipZero, f.Divisions = strconv.Itoa(t.MaxStars), t.SkipZero, strconv.Itoa(max(t.Divisions, 1))
	case "custom":
		f.CustomName, f.CustomTiers, f.CustomCutoffs = t.Name, strings.Join(t.Tiers, "\n"), strings.Join(t.Cutoffs, "\n")
	}
	return f
}

func (s *Server) tiers(w http.ResponseWriter, r *http.Request, ol *openList) {
	s.showTiers(w, r, ol, formFor(ol.list.Display), "", http.StatusOK)
}

// showTiers renders the tier list page with the given form, which differs
// from the saved options when it is being shown again with an error.
func (s *Server) showTiers(w http.ResponseWriter, r *http.Request, ol *openList, form displayForm, formErr string, status int) {
	l := ol.list
	v := tiersView{view: s.view(r, "tiers", ol), Form: form}
	if formErr != "" {
		v.Error = formErr
	}
	rows, err := l.Tiers()
	switch {
	case errors.Is(err, tier.ErrTooFewEntries) && len(l.Shown()) == 0:
		v.Empty = "This list has no entries yet."
	case errors.Is(err, tier.ErrTooFewEntries):
		v.Empty = "There's not much point in a tier list of one item. Add some more items."
	case err != nil:
		s.message(w, http.StatusInternalServerError, "The tier list can't be worked out", err.Error())
		return
	default:
		names := entryNames(l)
		stars := l.Display.Template.Kind == "stars"
		var lines []string
		for i, row := range rows {
			tr := tierRow{Name: row.Name, Hue: hue(i, len(rows))}
			if stars {
				tr.Name += "★"
			}
			for _, id := range row.Entries {
				tr.Entries = append(tr.Entries, names[id])
			}
			v.Rows = append(v.Rows, tr)
			lines = append(lines, strings.TrimSpace(tr.Name+": "+strings.Join(tr.Entries, ", ")))
		}
		v.Text, v.TextRows = strings.Join(lines, "\n"), len(lines)+1
		fit, err := l.Fit()
		if err != nil {
			s.message(w, http.StatusInternalServerError, "The ratings can't be worked out", err.Error())
			return
		}
		v.DrawElo = fmt.Sprintf("%.0f", fit.DrawElo())
		v.Levels = levelsText(l)
	}
	s.render(w, status, "tiers", v)
}

// hue colors tier i of n (0 is the top tier), from blue at the top to red
// at the bottom.
func hue(i, n int) int {
	if n < 2 {
		return 250
	}
	return 250 * (n - 1 - i) / (n - 1)
}

func (s *Server) setDisplay(w http.ResponseWriter, r *http.Request, ol *openList) {
	d, form, err := parseDisplay(r)
	if err != nil {
		s.showTiers(w, r, ol, form, sentence(err.Error()), http.StatusBadRequest)
		return
	}
	ol.list.Display = d
	if !s.saved(w, ol) {
		return
	}
	back(w, r, listURL(ol.key)+"/tiers", "", "")
}

// parseDisplay reads the display options form.
func parseDisplay(r *http.Request) (tierlist.Display, displayForm, error) {
	f := displayForm{
		Kind: r.FormValue("kind"), MaxStars: strings.TrimSpace(r.FormValue("maxStars")),
		SkipZero: r.FormValue("skipZero") != "", Divisions: strings.TrimSpace(r.FormValue("divisions")),
		CustomName: strings.TrimSpace(r.FormValue("customName")), CustomTiers: r.FormValue("customTiers"),
		CustomCutoffs: r.FormValue("customCutoffs"), Convention: r.FormValue("convention"),
		DrawMargin: strings.TrimSpace(r.FormValue("drawMargin")), GroupRule: r.FormValue("groupRule"),
		Prefer: r.FormValue("prefer"),
	}
	d := tierlist.Display{Convention: f.Convention, GroupRule: f.GroupRule, Prefer: f.Prefer}
	margin, err := strconv.ParseFloat(f.DrawMargin, 64)
	if err != nil || !(margin >= 0) || math.IsInf(margin, 1) {
		return d, f, errors.New("the draw-margin must be a number, 0 or more")
	}
	d.DrawMargin = margin
	switch f.Kind {
	case "stars":
		maxStars, err1 := strconv.Atoi(f.MaxStars)
		divisions, err2 := strconv.Atoi(cmp.Or(f.Divisions, "1"))
		if err1 != nil || err2 != nil {
			return d, f, errors.New("the number of stars and the parts per star must be whole numbers")
		}
		if divisions == 1 {
			divisions = 0
		}
		d.Template = tierlist.Template{Kind: "stars", MaxStars: maxStars, SkipZero: f.SkipZero, Divisions: divisions}
	case "hogwarts":
		d.Template = tierlist.Template{Kind: "hogwarts"}
	case "custom":
		var tiers []string
		for _, line := range strings.Split(f.CustomTiers, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				tiers = append(tiers, line)
			}
		}
		if len(tiers) == 0 {
			return d, f, errors.New("enter the tier names, best first, one per line")
		}
		cutoffs, err := sortedCutoffs(f.CustomCutoffs)
		if err != nil {
			return d, f, err
		}
		d.Template = tierlist.Template{Kind: "custom", Name: cmp.Or(f.CustomName, "Custom"), Tiers: tiers, Cutoffs: cutoffs}
	default:
		return d, f, errors.New("choose a template")
	}
	if err := d.Check(); err != nil {
		return d, f, err
	}
	return d, f, nil
}

// sortedCutoffs reads cut-offs written one per line or separated by commas
// or semicolons, and puts them in increasing order, keeping how each was
// written.
func sortedCutoffs(text string) ([]string, error) {
	type cutoff struct {
		text  string
		value *big.Rat
	}
	var cs []cutoff
	for _, s := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == ',' || r == ';' }) {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		v, err := tier.ParseCutoff(s)
		if err != nil {
			return nil, err
		}
		cs = append(cs, cutoff{s, v})
	}
	slices.SortStableFunc(cs, func(a, b cutoff) int { return a.value.Cmp(b.value) })
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.text
	}
	return out, nil
}

func entryNames(l *tierlist.List) map[int]string {
	names := make(map[int]string, len(l.Entries))
	for _, e := range l.Entries {
		names[e.ID] = e.Name
	}
	return names
}

func entryByID(l *tierlist.List, id int) tierlist.Entry {
	for _, e := range l.Entries {
		if e.ID == id {
			return e
		}
	}
	return tierlist.Entry{}
}
