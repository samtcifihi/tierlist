package web

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/samtcifihi/tierlist/internal/tierlist"
)

type answersView struct {
	view
	Total    int
	Rows     []answerRow
	Entries  []filterEntry // every entry, by name, to filter by
	Filtered bool
	Summary  string
}

// An answerRow is one answer, with the entry judged better first unless
// the answer was "about the same".
type answerRow struct {
	N             int // 1 for the first answer
	First, Second answerName
	Same          bool
}

// An answerName is an entry's name as the answers page shows it.
type answerName struct {
	Name    string
	Removed bool
	Chosen  bool // one of the entries the answers are filtered by
}

type filterEntry struct {
	answerName
	ID int
}

// answers shows the list's answers, newest first. Entry IDs given as entry
// in the query keep only the answers that involve at least one of them.
func (s *Server) answers(w http.ResponseWriter, r *http.Request, ol *openList) {
	l := ol.list
	chosen := make(map[int]bool)
	for _, v := range r.URL.Query()["entry"] {
		if id, err := strconv.Atoi(v); err == nil {
			chosen[id] = true
		}
	}
	v := answersView{view: s.view(r, "answers", ol), Total: len(l.Comparisons)}
	names := make(map[int]answerName, len(l.Entries))
	for _, e := range l.Entries {
		n := answerName{Name: e.Name, Removed: e.Removed, Chosen: chosen[e.ID]}
		names[e.ID] = n
		v.Entries = append(v.Entries, filterEntry{answerName: n, ID: e.ID})
	}
	slices.SortStableFunc(v.Entries, func(a, b filterEntry) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	var chosenNames []string
	for _, e := range v.Entries {
		if e.Chosen {
			chosenNames = append(chosenNames, e.Name)
		}
	}
	v.Filtered = len(chosenNames) > 0

	for k := len(l.Comparisons) - 1; k >= 0; k-- {
		c := l.Comparisons[k]
		if v.Filtered && !chosen[c.A] && !chosen[c.B] {
			continue
		}
		row := answerRow{N: k + 1, First: names[c.A], Second: names[c.B], Same: c.Answer == tierlist.AboutSame}
		if c.Answer == tierlist.SecondBetter {
			row.First, row.Second = row.Second, row.First
		}
		v.Rows = append(v.Rows, row)
	}
	switch {
	case !v.Filtered:
		v.Summary = plural(v.Total, "answer", "answers") + ", newest first."
	case len(v.Rows) == 0:
		v.Summary = fmt.Sprintf("None of the %s involve %s.", plural(v.Total, "answer", "answers"), orList(chosenNames))
	default:
		v.Summary = fmt.Sprintf("Answers involving %s: %d of %d, newest first.", orList(chosenNames), len(v.Rows), v.Total)
	}
	s.render(w, http.StatusOK, "answers", v)
}

// orList joins names as "A", "A or B", or "A, B or C".
func orList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}
