package web

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/samtcifihi/tierlist/internal/tierlist"
)

// The entries page can forget the answers about the ticked entries, or
// merge them into one entry. Each asks first, in a dialog the page opens
// when it is shown with ?ask=forget or ?ask=merge and the ticked entries,
// as the buttons below the entries send them.

// A forgetDialog asks before forgetting the answers about some entries.
type forgetDialog struct {
	IDs     []int
	Names   string // the entries, in words
	Answers int    // how many answers would go
	Text    string // what forgetting does
}

// A mergeDialog asks which details to keep before merging some entries.
type mergeDialog struct {
	IDs     []int
	Names   string // the entries, in words
	Answers int    // how many answers between them would go
	Fields  []mergeField
}

// A mergeField is a detail of the merged entry, with the values to choose
// from.
type mergeField struct {
	Key, Label string
	Options    []mergeOption
}

// A mergeOption is a value to choose for a detail, and an entry that has
// it, by ID.
type mergeOption struct {
	Entry   int
	Value   string
	Checked bool
}

// askDialog fills in the dialog that q asks for, if any, about the entries
// it ticks, and ticks them again on the page; or, if the ticked entries
// don't suit, says why.
func askDialog(v *entriesView, l *tierlist.List, q url.Values) {
	ask := q.Get("ask")
	if ask != "forget" && ask != "merge" {
		return
	}
	want := make(map[int]bool)
	for _, s := range q["focus"] {
		if id, err := strconv.Atoi(s); err == nil {
			want[id] = true
		}
	}
	var ticked []tierlist.Entry
	var ids []int
	var names []string
	for _, e := range l.Entries {
		if want[e.ID] && !e.Removed {
			ticked = append(ticked, e)
			ids = append(ids, e.ID)
			names = append(names, e.Name)
		}
	}
	for i := range v.Shown {
		v.Shown[i].Ticked = slices.Contains(ids, v.Shown[i].ID)
	}
	about, between := 0, 0
	for _, c := range l.Comparisons {
		a, b := slices.Contains(ids, c.A), slices.Contains(ids, c.B)
		if a || b {
			about++
		}
		if a && b {
			between++
		}
	}
	switch {
	case ask == "forget" && len(ids) == 0:
		v.Error = "Tick the entries whose answers to forget first."
	case ask == "forget" && about == 0:
		v.Error = fmt.Sprintf("%s %s no answers to forget.", list(names, "and"), agree(len(names), "has", "have"))
	case ask == "forget":
		they, were, their := "they", "were", "their ratings start"
		if len(names) == 1 {
			they, were, their = "it", "was", "its rating starts"
		}
		v.Forget = &forgetDialog{IDs: ids, Names: list(names, "and"), Answers: about,
			Text: fmt.Sprintf("Every answer about %s goes, %s in all, whatever %s %s compared with, and %s again at 1500. The ratings of the entries %s %s compared with change to match.",
				list(names, "and"), plural(about, "answer", "answers"), they, were, their, they, were)}
	case len(ids) < 2:
		v.Error = "Tick two or more entries to merge first."
	default:
		v.Merge = &mergeDialog{IDs: ids, Names: list(names, "and"), Answers: between, Fields: mergeFields(ticked)}
	}
}

// agree returns one, as in "has", for a single thing and many, as in
// "have", for n things.
func agree(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// mergeDetails are the details of an entry that merging chooses among.
var mergeDetails = []struct {
	key, label string
	value      func(tierlist.Entry) string
}{
	{"name", "Name", func(e tierlist.Entry) string { return e.Name }},
	{"url", "Link", func(e tierlist.Entry) string { return e.URL }},
	{"description", "Description", func(e tierlist.Entry) string { return e.Description }},
}

// mergeFields lists, for each detail of the entries being merged, the
// different values they have, with the one tierlist.MergeDefault picks
// chosen.
func mergeFields(entries []tierlist.Entry) []mergeField {
	var fields []mergeField
	for _, d := range mergeDetails {
		f := mergeField{Key: d.key, Label: d.label}
		chosen := d.value(entries[tierlist.MergeDefault(entries, d.value)])
		for _, e := range entries {
			value := d.value(e)
			if !slices.ContainsFunc(f.Options, func(o mergeOption) bool { return o.Value == value }) {
				f.Options = append(f.Options, mergeOption{Entry: e.ID, Value: value, Checked: value == chosen})
			}
		}
		fields = append(fields, f)
	}
	return fields
}

// formEntries reads the entries a form names by ID, as "id" fields.
func formEntries(r *http.Request, l *tierlist.List) ([]int, []string, error) {
	var ids []int
	var names []string
	for _, s := range r.Form["id"] {
		id, err := strconv.Atoi(s)
		if err != nil {
			return nil, nil, fmt.Errorf("%q isn't an entry", s)
		}
		e := entryByID(l, id)
		if e.ID == 0 {
			return nil, nil, fmt.Errorf("there is no entry %d", id)
		}
		ids, names = append(ids, id), append(names, e.Name)
	}
	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("no entries were chosen")
	}
	return ids, names, nil
}

// forgetAnswers deletes every answer about the entries the form names.
func (s *Server) forgetAnswers(w http.ResponseWriter, r *http.Request, ol *openList) {
	entries := listURL(ol.key) + "/entries"
	r.ParseForm()
	ids, names, err := formEntries(r, ol.list)
	var n int
	if err == nil {
		n, err = ol.list.ForgetAnswers(ids)
	}
	if err != nil {
		back(w, r, entries, "", sentence(err.Error()))
		return
	}
	if !s.saved(w, ol) {
		return
	}
	// What Undo would take back may be gone, and the pairs coming up were
	// chosen for the old answers.
	ol.done = nil
	ol.replan()
	back(w, r, entries, fmt.Sprintf("Forgot %s about %s, which %s at 1500 again.",
		plural(n, "answer", "answers"), list(names, "and"), agree(len(names), "starts", "start")), "")
}

// mergeEntries merges the entries the form names into one, with the name,
// link and description it chooses, each given as the ID of an entry that
// has it.
func (s *Server) mergeEntries(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	entries := listURL(ol.key) + "/entries"
	r.ParseForm()
	ids, names, err := formEntries(r, l)
	if err != nil {
		back(w, r, entries, "", sentence(err.Error()))
		return
	}
	var chosen []string
	for _, d := range mergeDetails {
		id, err := strconv.Atoi(r.FormValue(d.key))
		if err != nil || !slices.Contains(ids, id) {
			back(w, r, entries, "", "Choose which name, link and description to keep.")
			return
		}
		chosen = append(chosen, d.value(entryByID(l, id)))
	}
	n, err := l.MergeEntries(ids, chosen[0], chosen[1], chosen[2])
	if err != nil {
		back(w, r, entries, "", sentence(err.Error()))
		return
	}
	if !s.saved(w, ol) {
		return
	}
	// Undo can't take back what the merged entries were in, and the pairs
	// coming up may name entries that are gone.
	ol.done = nil
	ol.replan()
	msg := fmt.Sprintf("Merged %s into %s.", list(names, "and"), chosen[0])
	if n > 0 {
		msg += fmt.Sprintf(" The %s between them went.", plural(n, "answer", "answers"))
	}
	back(w, r, entries, msg, "")
}
