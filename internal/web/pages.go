package web

import (
	"cmp"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
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
	Delete                         confirm
}

// A confirm is a dialog that asks before deleting something, since that
// can't be undone.
type confirm struct {
	ID     string // the dialog's element ID
	Action string // where the form posts to
	Title  string
	Text   string
	Button string // what the button that goes ahead says
}

// deleteDialog returns the dialog that asks before deleting the list at
// url.
func deleteDialog(id, url, name string, entries, answers int) confirm {
	text := "The list is empty."
	if entries > 0 || answers > 0 {
		text = fmt.Sprintf("Its %s and %s will be deleted with it.",
			plural(entries, "entry", "entries"), plural(answers, "answer", "answers"))
	}
	return confirm{ID: id, Action: url + "/delete", Title: "Delete “" + name + "”?", Text: text, Button: "Delete"}
}

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	sums, err := tierlist.Lists(s.dir)
	if err != nil {
		s.message(w, http.StatusInternalServerError, "Your lists can't be read", err.Error())
		return
	}
	v := libraryView{view: s.view(r, "library", nil), Dir: s.dir}
	for k, sm := range sums {
		file := filepath.Base(sm.Path)
		item := listSummary{
			Name: sm.Name, URL: listURL(strings.TrimSuffix(file, ".json")), File: file,
			Entries: sm.Entries, Answers: sm.Comparisons, Modified: sm.Modified.Format("2 Jan 2006, 15:04"),
		}
		id := fmt.Sprintf("delete-%d", k+1)
		item.Delete = deleteDialog(id, item.URL, item.Name, item.Entries, item.Answers)
		if sm.Err != nil {
			item.Err = sm.Err.Error()
			item.Delete = confirm{ID: id, Action: item.URL + "/delete", Title: "Delete " + file + "?",
				Text: "This file can't be opened as a tier list.", Button: "Delete"}
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

// deleteList deletes a list's file. It does not need to open the list
// first, so a damaged file can be deleted too.
func (s *Server) deleteList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.PathValue("list")
	path, _, err := s.file(key)
	if err != nil {
		s.message(w, http.StatusNotFound, "No such list", "There is no list at this address.")
		return
	}
	name := filepath.Base(path)
	if l, err := tierlist.Load(path); err == nil {
		name = l.Name
	}
	if err := os.Remove(path); err != nil {
		back(w, r, "/", "", fmt.Sprintf("“%s” could not be deleted: %v", name, err))
		return
	}
	delete(s.lists, key)
	back(w, r, "/", "Deleted “"+name+"”.", "")
}

// resetList deletes every answer in the list, keeping its entries.
func (s *Server) resetList(w http.ResponseWriter, r *http.Request, ol *openList) {
	n := len(ol.list.Comparisons)
	ol.list.Reset()
	if !s.saved(w, ol) {
		return
	}
	ol.queue, ol.done = nil, nil
	back(w, r, listURL(ol.key)+"/entries",
		fmt.Sprintf("Reset the list, deleting %s. Every entry starts again at 1500.", plural(n, "answer", "answers")), "")
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
	NothingLeft   bool // every pair that could be asked is ignored
	First, Second tierlist.Entry
	Upcoming      []upcomingPair // the pairs coming up, the last one next
	Token         int            // see token
	Undo          string         // the answer or ignore Undo would take back
	Last          string         // where the entries of the latest answer now stand
	TopMode       string         // top mode in words, while it is on
	Answers       int
	Draw          string // the draw setting, as a percentage
	Levels        string
	Focus         []string // names of the entries in focus mode
}

// token counts the answers and ignores in the list. A form from the rating
// page carries the count the page was made with, so that a double click, or
// a form sent again with the Back button, is noticed and dropped.
func token(l *tierlist.List) int {
	return len(l.Comparisons) + len(l.IgnoredEntries) + len(l.IgnoredPairs)
}

func (s *Server) rate(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	names := entryNames(l)
	v := rateView{view: s.view(r, "rate", ol), Answers: len(l.Comparisons), Token: token(l)}
	for _, id := range l.Focus {
		v.Focus = append(v.Focus, names[id])
	}
	if d, ok := lastIgnore(ol); ok {
		v.Undo = describeIgnore(d, names)
	} else if k := len(l.Comparisons); k > 0 {
		v.Undo = describe(l.Comparisons[k-1], names)
	}
	if len(l.Comparisons) > 0 {
		v.Last = lastMoves(l, names, ol.standingBefore())
	}
	if l.Top > 0 {
		v.TopMode = topText(l.Top)
	}
	if len(l.Shown()) >= 2 {
		err := s.fillQueue(ol)
		if errors.Is(err, tierlist.ErrNoPair) {
			v.NothingLeft = true
			s.render(w, http.StatusOK, "rate", v)
			return
		}
		if err != nil {
			s.message(w, http.StatusInternalServerError, "No pair to compare", err.Error())
			return
		}
		fit, err := l.Fit()
		if err != nil {
			s.message(w, http.StatusInternalServerError, "The ratings can't be worked out", err.Error())
			return
		}
		v.Ready = true
		v.First, v.Second = entryByID(l, ol.queue[0][0]), entryByID(l, ol.queue[0][1])
		// The furthest pair first, so that the page can stack them with the
		// next one nearest the pair being asked.
		for k := len(ol.queue) - 1; k >= 1; k-- {
			v.Upcoming = append(v.Upcoming, upcomingPair{First: names[ol.queue[k][0]], Second: names[ol.queue[k][1]]})
		}
		v.Draw = drawText(fit)
		v.Levels = levelsText(l)
	}
	s.render(w, http.StatusOK, "rate", v)
}

type upcomingPair struct{ First, Second string }

// fillQueue drops the pairs the rating page can no longer ask from the
// list's queue, keeping the rest in order, and tops it up to the pair to
// ask and the pairs coming up.
func (s *Server) fillQueue(ol *openList) error {
	var q [][2]int
	for _, p := range ol.queue {
		if ol.list.CanAsk(p[0], p[1]) {
			q = append(q, p)
		}
	}
	if need := 1 + upcoming - len(q); need > 0 {
		more, err := ol.list.NextPairs(q, need, s.rng)
		if err != nil {
			return err
		}
		q = append(q, more...)
	}
	ol.queue = q
	return nil
}

// pairForm reads the pair a rating page form was about, and the token it
// was made with.
func pairForm(r *http.Request) (a, b, n int, ok bool) {
	n, errN := strconv.Atoi(r.FormValue("n"))
	a, errA := strconv.Atoi(r.FormValue("a"))
	b, errB := strconv.Atoi(r.FormValue("b"))
	return a, b, n, errN == nil && errA == nil && errB == nil
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	rate := listURL(ol.key) + "/rate"
	a, b, n, ok := pairForm(r)
	if !ok {
		http.Error(w, "incomplete answer", http.StatusBadRequest)
		return
	}
	if n != token(l) {
		back(w, r, rate, "That answer came from an out-of-date page, so it wasn't recorded.", "")
		return
	}
	// Where the entries stand now, from the fit the rating page just used,
	// is where they stood before this answer.
	before, errBefore := l.TopPercents()
	if err := l.Record(a, b, tierlist.Answer(r.FormValue("answer"))); err != nil {
		back(w, r, rate, "", err.Error())
		return
	}
	if !s.saved(w, ol) {
		return
	}
	if errBefore == nil {
		ol.standing = standing{key: standingKey(l), before: before}
	}
	ol.done = append(ol.done, done{a: a, b: b})
	ol.asked(a, b)
	back(w, r, rate, "", "")
}

// ignore sets aside the pair shown, or one of its entries, until the
// ignores are reset.
func (s *Server) ignore(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	rate := listURL(ol.key) + "/rate"
	a, b, n, ok := pairForm(r)
	what := r.FormValue("ignore")
	if !ok || (what != "a" && what != "b" && what != "pair") {
		http.Error(w, "incomplete request to ignore", http.StatusBadRequest)
		return
	}
	if n != token(l) {
		back(w, r, rate, "That came from an out-of-date page, so nothing changed.", "")
		return
	}
	if !l.CanAsk(a, b) {
		back(w, r, rate, "", "that pair can't be asked about now")
		return
	}
	d := done{ignore: what, a: a, b: b}
	switch what {
	case "a":
		l.IgnoreEntry(a)
	case "b":
		l.IgnoreEntry(b)
	default:
		l.IgnorePair(a, b)
	}
	if !s.saved(w, ol) {
		return
	}
	ol.done = append(ol.done, d)
	ol.asked(a, b)
	back(w, r, rate, "Not asking about "+ignored(d, entryNames(l))+" until you reset ignores on the Entries page.", "")
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	rate := listURL(ol.key) + "/rate"
	if n, err := strconv.Atoi(r.FormValue("n")); err != nil || n != token(l) {
		back(w, r, rate, "That undo came from an out-of-date page, so nothing was undone.", "")
		return
	}
	names := entryNames(l)
	var a, b int
	var msg string
	if d, ok := lastIgnore(ol); ok {
		switch d.ignore {
		case "a":
			l.UnignoreEntry(d.a)
		case "b":
			l.UnignoreEntry(d.b)
		default:
			l.UnignorePair(d.a, d.b)
		}
		a, b, msg = d.a, d.b, "Took back "+describeIgnore(d, names)+". Answer it now."
	} else {
		c, ok := l.Undo()
		if !ok {
			back(w, r, rate, "", "")
			return
		}
		a, b, msg = c.A, c.B, "Took back "+describe(c, names)+". Answer it again."
	}
	if !s.saved(w, ol) {
		return
	}
	if k := len(ol.done); k > 0 {
		ol.done = ol.done[:k-1]
	}
	// Ask the same question again, the same way round, before the ones
	// that were coming up.
	ol.queue = append([][2]int{{a, b}}, ol.queue...)
	ol.queue = ol.queue[:min(len(ol.queue), 1+upcoming)]
	back(w, r, rate, msg, "")
}

// topText describes top mode for the top p percent.
func topText(p float64) string {
	pct := func(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) + "%" }
	if 2*p >= 100 {
		return "Top mode: favouring the top " + pct(p) + " of the list."
	}
	return "Top mode: favouring the top " + pct(p) + " of the list, and asking only about the top " + pct(2*p) + "."
}

// setTop turns top mode on for the percentage given as top, or off with
// do=clear.
func (s *Server) setTop(w http.ResponseWriter, r *http.Request, ol *openList) {
	rate := listURL(ol.key) + "/rate"
	p := 0.0
	if r.FormValue("do") != "clear" {
		v, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("top")), 64)
		if err != nil || !(v > 0 && v <= 100) {
			back(w, r, rate, "", "top mode needs a percentage above 0 and at most 100")
			return
		}
		p = v
	}
	if err := ol.list.SetTop(p); err != nil {
		back(w, r, rate, "", err.Error())
		return
	}
	if !s.saved(w, ol) {
		return
	}
	ol.replan()
	if p == 0 {
		back(w, r, rate, "Top mode is off.", "")
		return
	}
	back(w, r, rate, "", "")
}

// lastMoves says where the entries of the latest answer now stand, as a
// share of the list from the top, and how many percentage points that
// answer moved them, + for up the list and - for down, as in "Alien is now
// in the top 9% (+8), Brazil in the top 50% (-16).". Shares are rounded
// up, so an entry in the top 8.3% is in the top 9%, as it is in top mode's
// terms, and the moves are between the rounded shares. Removed entries
// are left out, and so are the moves when before is nil.
func lastMoves(l *tierlist.List, names map[int]string, before map[int]float64) string {
	k := len(l.Comparisons)
	if k == 0 {
		return ""
	}
	now, err := l.TopPercents()
	if err != nil {
		return ""
	}
	c := l.Comparisons[k-1]
	var parts []string
	for _, id := range []int{c.A, c.B} {
		x, ok := now[id]
		if !ok {
			continue
		}
		part := fmt.Sprintf("in the top %d%%", roundUp(x))
		if b, ok := before[id]; ok {
			part += " (" + signed(roundUp(b)-roundUp(x)) + ")"
		}
		if len(parts) == 0 {
			part = names[id] + " is now " + part
		} else {
			part = names[id] + " " + part
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ") + "."
}

func roundUp(x float64) int { return int(math.Ceil(x)) }

// signed writes a move up or down the list: "+8", "-8", or "±0".
func signed(d int) string {
	switch {
	case d > 0:
		return "+" + strconv.Itoa(d)
	case d < 0:
		return strconv.Itoa(d)
	}
	return "±0"
}

// lastIgnore returns the latest thing the rating page did, if that was an
// ignore that Undo would take back.
func lastIgnore(ol *openList) (done, bool) {
	if k := len(ol.done); k > 0 && ol.done[k-1].ignore != "" {
		return ol.done[k-1], true
	}
	return done{}, false
}

// ignored names what an ignore set aside.
func ignored(d done, names map[int]string) string {
	switch d.ignore {
	case "a":
		return names[d.a]
	case "b":
		return names[d.b]
	}
	return names[d.a] + " vs " + names[d.b]
}

// describeIgnore puts an ignore into words for Undo.
func describeIgnore(d done, names map[int]string) string {
	return "ignoring " + ignored(d, names)
}

func (s *Server) resetIgnores(w http.ResponseWriter, r *http.Request, ol *openList) {
	from := listURL(ol.key) + "/entries"
	if r.FormValue("from") == "rate" {
		from = listURL(ol.key) + "/rate"
	}
	ol.list.ResetIgnores()
	if !s.saved(w, ol) {
		return
	}
	// The ignores are gone, so Undo can no longer take them back.
	ol.done = slices.DeleteFunc(ol.done, func(d done) bool { return d.ignore != "" })
	ol.replan()
	back(w, r, from, "Every entry and pair can be asked about again.", "")
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

// drawText shows the draw setting as how often equally rated entries are
// called about the same.
func drawText(fit *tierlist.Fit) string {
	return fmt.Sprintf("%.0f%%", 100*fit.SameChance())
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
	Shown    []entryRow
	Removed  []entryRow
	Focus    bool
	Ignoring string // what is ignored, in words, or ""
	CSV      string // the shown entries as CSV, to copy
	CSVRows  int    // lines for its text box, with room for a scroll bar
	Answers  int
	Reset    confirm
	Delete   confirm
}

type entryRow struct {
	ID      int
	Name    string
	Rating  string
	SD      string
	Answers int
	Focused bool
	Ignored bool
}

func (s *Server) entries(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	fit, err := l.Fit()
	if err != nil {
		s.message(w, http.StatusInternalServerError, "The ratings can't be worked out", err.Error())
		return
	}
	counts := l.Counts()
	v := entriesView{view: s.view(r, "entries", ol), Focus: len(l.Focus) > 0, Answers: len(l.Comparisons),
		Reset: confirm{ID: "reset-list", Action: listURL(ol.key) + "/reset", Title: "Reset “" + l.Name + "”?",
			Text:   fmt.Sprintf("Its %s will be deleted. The entries stay, and their ratings start over at 1500.", plural(len(l.Comparisons), "answer", "answers")),
			Button: "Reset"},
		Delete: deleteDialog("delete-list", listURL(ol.key), l.Name, len(l.Shown()), len(l.Comparisons))}
	for _, e := range l.Entries {
		row := entryRow{
			ID: e.ID, Name: e.Name, Answers: counts[e.ID], Focused: slices.Contains(l.Focus, e.ID), Ignored: l.EntryIgnored(e.ID),
			Rating: fmt.Sprintf("%.0f", fit.Points(e.ID)), SD: fmt.Sprintf("± %.0f", fit.PointsSD(e.ID)),
		}
		if e.Removed {
			v.Removed = append(v.Removed, row)
		} else {
			v.Shown = append(v.Shown, row)
		}
	}
	slices.SortStableFunc(v.Shown, func(a, b entryRow) int { return cmp.Compare(fit.Rating(b.ID), fit.Rating(a.ID)) })
	v.Ignoring = ignoring(l)
	v.CSV, v.CSVRows = entriesCSV(v.Shown, fit), min(len(v.Shown)+2, 20)
	s.render(w, http.StatusOK, "entries", v)
}

// entriesCSV writes the rows as CSV under a header row: each entry's name,
// its rating and the ± uncertainty of it (one standard deviation), in
// points as the page shows them, and its number of answers.
func entriesCSV(rows []entryRow, fit *tierlist.Fit) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Write([]string{"entry name", "rating", "CI width", "number of answers"})
	for _, r := range rows {
		w.Write([]string{r.Name, r.Rating, fmt.Sprintf("%.0f", fit.PointsSD(r.ID)), strconv.Itoa(r.Answers)})
	}
	w.Flush() // writing to a strings.Builder cannot fail
	return b.String()
}

// ignoring says what the list ignores, as in "Ignoring Alien and Brazil,
// plus 2 pairs.", or returns "" if nothing is ignored.
func ignoring(l *tierlist.List) string {
	names := entryNames(l)
	var ignored []string
	for _, id := range l.IgnoredEntries {
		ignored = append(ignored, names[id])
	}
	pairs := plural(len(l.IgnoredPairs), "pair", "pairs")
	switch {
	case len(ignored) > 0 && len(l.IgnoredPairs) > 0:
		return "Ignoring " + list(ignored, "and") + ", plus " + pairs + "."
	case len(ignored) > 0:
		return "Ignoring " + list(ignored, "and") + "."
	case len(l.IgnoredPairs) > 0:
		return "Ignoring " + pairs + "."
	}
	return ""
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
	if added > 0 {
		if !s.saved(w, ol) {
			return
		}
		ol.replan()
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
		ol.replan()
		return fmt.Sprintf("Removed %s. Its answers still count, and you can restore it at the bottom of the page.",
			entryNames(l)[id]), nil
	})(w, r, ol)
}

func (s *Server) restoreEntry(w http.ResponseWriter, r *http.Request, ol *openList) {
	s.entryAction(func(l *tierlist.List, id int, r *http.Request) (string, error) {
		if err := l.RestoreEntry(id); err != nil {
			return "", err
		}
		ol.replan()
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
	ol.replan()
	if len(ids) == 0 {
		back(w, r, from, "Focus mode is off.", "")
		return
	}
	back(w, r, base+"/rate", "Focus mode is on: every pair includes one of the ticked entries.", "")
}

type tiersView struct {
	view
	Form     displayForm
	Chart    *chartView // the saved template's tier sizes
	Rows     []tierRow
	Text     string // the tier list as plain text
	TextRows int    // lines for its text box, with room for a scroll bar
	Empty    string // why there is no tier list
	Draw     string // the draw setting, as a percentage
	Levels   string
}

type tierRow struct {
	Name    string
	Hue     int
	Entries []string
}

// Label is the tier's name with how many entries it holds.
func (r tierRow) Label() string { return fmt.Sprintf("%s (%d)", r.Name, len(r.Entries)) }

// displayForm holds the display options as the form shows them.
type displayForm struct {
	Kind          string
	MaxStars      string
	SkipZero      bool
	Divisions     string
	Sizes         string
	Factor        string
	From          string
	Alpha         string
	Beta          string
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
		Kind: d.Template.Kind, MaxStars: "10", Divisions: "1", CustomName: "Custom",
		Sizes: "even", Factor: strconv.FormatFloat(math.Phi, 'g', -1, 64), From: "best", Alpha: "2", Beta: "2",
		Convention: d.Convention, DrawMargin: strconv.FormatFloat(d.DrawMargin, 'f', -1, 64),
		GroupRule: d.GroupRule, Prefer: d.Prefer,
	}
	switch t := d.Template; t.Kind {
	case "stars":
		f.MaxStars, f.SkipZero, f.Divisions = strconv.Itoa(t.MaxStars), t.SkipZero, strconv.Itoa(max(t.Divisions, 1))
		f.Sizes = cmp.Or(t.Sizes, "even")
		f.Factor, f.From, f.Alpha, f.Beta = cmp.Or(t.Factor, f.Factor), cmp.Or(t.From, f.From), cmp.Or(t.Alpha, f.Alpha), cmp.Or(t.Beta, f.Beta)
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
	if shape, err := l.Display.Shape(); err == nil {
		chart := shapeChart(shape, l.Display.Template.Kind == "stars")
		v.Chart = &chart
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
			lines = append(lines, strings.TrimSpace(tr.Label()+": "+strings.Join(tr.Entries, ", ")))
		}
		v.Text, v.TextRows = strings.Join(lines, "\n"), len(lines)+1
		fit, err := l.Fit()
		if err != nil {
			s.message(w, http.StatusInternalServerError, "The ratings can't be worked out", err.Error())
			return
		}
		v.Draw = drawText(fit)
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
	f := readDisplayForm(r)
	d := tierlist.Display{Convention: f.Convention, GroupRule: f.GroupRule, Prefer: f.Prefer}
	margin, err := strconv.ParseFloat(f.DrawMargin, 64)
	if err != nil || !(margin >= 0) || math.IsInf(margin, 1) {
		return d, f, errors.New("the draw-margin must be a number, 0 or more")
	}
	d.DrawMargin = margin
	if d.Template, err = f.template(); err != nil {
		return d, f, err
	}
	if err := d.Check(); err != nil {
		return d, f, err
	}
	return d, f, nil
}

// readDisplayForm reads the display options form as it was filled in.
func readDisplayForm(r *http.Request) displayForm {
	return displayForm{
		Kind: r.FormValue("kind"), MaxStars: strings.TrimSpace(r.FormValue("maxStars")),
		SkipZero: r.FormValue("skipZero") != "", Divisions: strings.TrimSpace(r.FormValue("divisions")),
		Sizes: cmp.Or(r.FormValue("sizes"), "even"), Factor: strings.TrimSpace(r.FormValue("factor")),
		From: cmp.Or(r.FormValue("from"), "best"), Alpha: strings.TrimSpace(r.FormValue("alpha")), Beta: strings.TrimSpace(r.FormValue("beta")),
		CustomName: strings.TrimSpace(r.FormValue("customName")), CustomTiers: r.FormValue("customTiers"),
		CustomCutoffs: r.FormValue("customCutoffs"), Convention: r.FormValue("convention"),
		DrawMargin: strings.TrimSpace(r.FormValue("drawMargin")), GroupRule: r.FormValue("groupRule"),
		Prefer: r.FormValue("prefer"),
	}
}

// template reads the template the form chooses. It does not check that
// the template can be built; Display.Check does.
func (f displayForm) template() (tierlist.Template, error) {
	switch f.Kind {
	case "stars":
		maxStars, err1 := strconv.Atoi(f.MaxStars)
		divisions, err2 := strconv.Atoi(cmp.Or(f.Divisions, "1"))
		if err1 != nil || err2 != nil {
			return tierlist.Template{}, errors.New("the number of stars and the parts per star must be whole numbers")
		}
		if divisions == 1 {
			divisions = 0
		}
		t := tierlist.Template{Kind: "stars", MaxStars: maxStars, SkipZero: f.SkipZero, Divisions: divisions}
		switch f.Sizes {
		case "even":
		case "geometric":
			t.Sizes, t.Factor, t.From = f.Sizes, f.Factor, f.From
		case "beta":
			t.Sizes, t.Alpha, t.Beta = f.Sizes, f.Alpha, f.Beta
		default:
			return tierlist.Template{}, errors.New("choose how big the tiers are")
		}
		return t, nil
	case "owl-newt":
		return tierlist.Template{Kind: "owl-newt"}, nil
	case "custom":
		var tiers []string
		for _, line := range strings.Split(f.CustomTiers, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				tiers = append(tiers, line)
			}
		}
		if len(tiers) == 0 {
			return tierlist.Template{}, errors.New("enter the tier names, best first, one per line")
		}
		cutoffs, err := sortedCutoffs(f.CustomCutoffs)
		if err != nil {
			return tierlist.Template{}, err
		}
		return tierlist.Template{Kind: "custom", Name: cmp.Or(f.CustomName, "Custom"), Tiers: tiers, Cutoffs: cutoffs}, nil
	}
	return tierlist.Template{}, errors.New("choose a template")
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
