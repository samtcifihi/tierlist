package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/samtcifihi/tierlist/internal/tierlist"
)

// An exportView shows a list's saved file, to copy into another copy of
// the program.
type exportView struct {
	view
	Name string // the list's name, or its file's if it can't be read
	File string // the file's name
	Path string // where the file is
	Text string // what the file holds
	Rows int    // lines for its text box
}

// exportList shows a list's file in a box to copy, for Import on the start
// page of another copy of the program. It shows a file that can't be opened
// as a list too, so that its text can still be rescued.
func (s *Server) exportList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, _, err := s.file(r.PathValue("list"))
	if err != nil {
		s.message(w, http.StatusNotFound, "No such list", "There is no list at this address.")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		s.message(w, http.StatusInternalServerError, "This list can't be read", err.Error())
		return
	}
	v := exportView{view: s.view(r, "export", nil), Name: filepath.Base(path), File: filepath.Base(path), Path: path,
		Text: string(data), Rows: min(strings.Count(string(data), "\n")+2, 30)}
	if l, err := tierlist.Load(path); err == nil {
		v.Name = l.Name
	}
	v.Title = "Export " + v.Name
	s.render(w, http.StatusOK, "export", v)
}

// importList saves a list pasted as the text of its file, as Export shows
// it, as a new list, which never replaces one (see tierlist.Import), or
// into the list the form chooses (see importInto). If the text isn't a
// list, the start page shows it again with the reason.
func (s *Server) importList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, into := r.FormValue("data"), r.FormValue("into")
	if into != "" {
		s.importInto(w, r, text, into)
		return
	}
	_, l, err := tierlist.Import(s.dir, []byte(text))
	if err != nil {
		s.showLibrary(w, r, text, into, sentence("that can't be imported: "+err.Error()), http.StatusBadRequest)
		return
	}
	back(w, r, "/", "Imported “"+l.Name+"”.", "")
}

// importInto adds the entries of the list in text, with their answers, to
// the list saved under key, which keeps its own settings (see
// tierlist.List.ImportEntries), and shows its entries.
func (s *Server) importInto(w http.ResponseWriter, r *http.Request, text, key string) {
	ol, err := s.open(key)
	if err != nil {
		s.showLibrary(w, r, text, "", sentence("there is no list to import into: "+err.Error()), http.StatusBadRequest)
		return
	}
	other, err := tierlist.Parse([]byte(text))
	if err != nil {
		s.showLibrary(w, r, text, key, sentence("that can't be imported: "+err.Error()), http.StatusBadRequest)
		return
	}
	renamed, k := ol.list.ImportEntries(other)
	if !s.saved(w, ol) {
		return
	}
	ol.replan()
	msg := fmt.Sprintf("Imported %s and %s from “%s”.", plural(len(other.Entries), "entry", "entries"),
		plural(len(other.Comparisons), "answer", "answers"), other.Name)
	if renamed > 0 {
		msg += fmt.Sprintf(" %s whose %s taken got “ (%d)” added.", plural(renamed, "entry", "entries"), agree(renamed, "name was", "names were"), k)
	}
	back(w, r, listURL(ol.key)+"/entries", msg, "")
}
