// Package web serves the program's pages: the library of saved lists and,
// for each list, pages to rate it, edit its entries, see its tier list and
// look back over its answers.
// The pages are plain HTML forms rendered on the server; a little
// JavaScript adds keyboard shortcuts and opens the dialogs that ask before
// deleting or resetting.
package web

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/samtcifihi/tierlist/internal/tierlist"
)

//go:embed templates static
var files embed.FS

// A Server serves the lists saved in one folder. Requests take turns on one
// lock, which is plenty for one person's browser.
type Server struct {
	dir   string
	pages map[string]*template.Template

	mu    sync.Mutex
	lists map[string]*openList
	rng   *rand.Rand

	quit     chan struct{}
	quitOnce sync.Once
}

// An openList is a list read from its file, with the pair its rating page
// is showing.
type openList struct {
	key  string // the file name without .json
	path string
	list *tierlist.List
	// The file's modification time and size when last read or written, to
	// notice changes made outside the program.
	modTime time.Time
	size    int64
	// queue holds the pairs to ask, as entry IDs in the order to show
	// them: the pair on the rating page, then the ones shown coming up.
	// Once shown, a pair stays lined up until it is asked.
	queue [][2]int
	// done holds what the rating page did while the program has been
	// running, newest last, so that Undo can take back answers and ignores
	// in order. Answers from before are still in the list itself.
	done []done
}

// A done is an answer, or an ignore of the pair shown or one of its
// entries.
type done struct {
	ignore string // what was ignored: "a", "b" or "pair"; "" for an answer
	a, b   int    // the pair shown, by entry ID
}

// upcoming is how many pairs the rating page shows coming up after the
// one it asks.
const upcoming = 4

// asked takes the pair a, b off the front of the queue, once it has been
// answered or ignored.
func (ol *openList) asked(a, b int) {
	if len(ol.queue) > 0 && ol.queue[0] == [2]int{a, b} {
		ol.queue = ol.queue[1:]
	}
}

// replan keeps the pair being asked but forgets the ones coming up, so
// that they are chosen again for a list whose entries or focus changed.
func (ol *openList) replan() {
	if len(ol.queue) > 1 {
		ol.queue = ol.queue[:1]
	}
}

var errNotFound = errors.New("no such list")

// New returns a server for the lists in dir.
func New(dir string) (*Server, error) {
	s := &Server{
		dir:   dir,
		pages: make(map[string]*template.Template),
		lists: make(map[string]*openList),
		rng:   rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1)),
		quit:  make(chan struct{}),
	}
	funcs := template.FuncMap{
		"join":   func(xs []string) string { return strings.Join(xs, ", ") },
		"plural": plural, // as in {{plural .Answers "answer" "answers"}}
	}
	for _, page := range []string{"library", "rate", "entries", "tiers", "answers", "message"} {
		t, err := template.New("").Funcs(funcs).ParseFS(files, "templates/layout.html", "templates/"+page+".html")
		if err != nil {
			return nil, err
		}
		s.pages[page] = t
	}
	return s, nil
}

// Quit returns a channel that is closed when the user presses Quit.
func (s *Server) Quit() <-chan struct{} { return s.quit }

// Handler returns the server's HTTP handler.
func (s *Server) Handler() http.Handler {
	static, err := fs.Sub(files, "static")
	if err != nil {
		panic(err) // the folder is embedded, so this cannot happen
	}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", revalidate(http.FileServerFS(static))))
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "tierlist") })
	mux.HandleFunc("GET /{$}", s.library)
	mux.HandleFunc("POST /lists", s.createList)
	mux.HandleFunc("GET /lists/{list}", s.withList(s.listHome))
	mux.HandleFunc("POST /lists/{list}/rename", s.withList(s.renameList))
	mux.HandleFunc("POST /lists/{list}/delete", s.deleteList)
	mux.HandleFunc("POST /lists/{list}/reset", s.withList(s.resetList))
	mux.HandleFunc("GET /lists/{list}/rate", s.withList(s.rate))
	mux.HandleFunc("POST /lists/{list}/answer", s.withList(s.answer))
	mux.HandleFunc("POST /lists/{list}/undo", s.withList(s.undo))
	mux.HandleFunc("POST /lists/{list}/ignore", s.withList(s.ignore))
	mux.HandleFunc("POST /lists/{list}/ignores/reset", s.withList(s.resetIgnores))
	mux.HandleFunc("GET /lists/{list}/entries", s.withList(s.entries))
	mux.HandleFunc("POST /lists/{list}/entries", s.withList(s.addEntries))
	mux.HandleFunc("POST /lists/{list}/entries/{id}/rename", s.withList(s.renameEntry))
	mux.HandleFunc("POST /lists/{list}/entries/{id}/remove", s.withList(s.removeEntry))
	mux.HandleFunc("POST /lists/{list}/entries/{id}/restore", s.withList(s.restoreEntry))
	mux.HandleFunc("POST /lists/{list}/focus", s.withList(s.setFocus))
	mux.HandleFunc("POST /lists/{list}/top", s.withList(s.setTop))
	mux.HandleFunc("GET /lists/{list}/tiers", s.withList(s.tiers))
	mux.HandleFunc("POST /lists/{list}/display", s.withList(s.setDisplay))
	mux.HandleFunc("GET /lists/{list}/chart", s.withList(s.chart))
	mux.HandleFunc("GET /lists/{list}/answers", s.withList(s.answers))
	mux.HandleFunc("POST /quit", s.quitNow)
	return guard(mux)
}

// guard turns away requests that did not come from the program's own
// pages. The Host must be the local address, which defeats DNS rebinding,
// and requests that change something must come from the same origin, which
// stops other websites from submitting forms to the program.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, "tierlist only answers requests for this computer", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			http.Error(w, "tierlist only accepts changes from its own pages", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether the browser says a request came from one of
// the program's own pages. Requests from outside a browser send neither
// header and are allowed.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	}
	return true
}

// revalidate makes browsers check for newer static files each time, which
// is cheap on the same computer and avoids stale styles after an upgrade.
func revalidate(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

// withList wraps a handler for one list's pages: it holds the lock and
// opens the list named in the path.
func (s *Server) withList(h func(http.ResponseWriter, *http.Request, *openList)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		ol, err := s.open(r.PathValue("list"))
		switch {
		case errors.Is(err, errNotFound):
			s.message(w, http.StatusNotFound, "No such list", "There is no list at this address.")
		case err != nil:
			s.message(w, http.StatusInternalServerError, "This list can't be opened", err.Error())
		default:
			h(w, r, ol)
		}
	}
}

// file returns the path of the file saved under key, which must be a file
// directly in the folder, and its details.
func (s *Server) file(key string) (string, os.FileInfo, error) {
	if key == "" || strings.ContainsAny(key, `/\`) || !filepath.IsLocal(key) {
		return "", nil, errNotFound
	}
	path := filepath.Join(s.dir, key+".json")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, errNotFound
	}
	return path, info, nil
}

// open returns the list saved under key, reading its file again if it has
// changed since the program last read or wrote it.
func (s *Server) open(key string) (*openList, error) {
	path, info, err := s.file(key)
	if err != nil {
		return nil, err
	}
	if ol := s.lists[key]; ol != nil && ol.modTime.Equal(info.ModTime()) && ol.size == info.Size() {
		return ol, nil
	}
	l, err := tierlist.Load(path)
	if err != nil {
		return nil, err
	}
	ol := &openList{key: key, path: path, list: l, modTime: info.ModTime(), size: info.Size()}
	s.lists[key] = ol
	return ol, nil
}

// save writes a changed list to its file. If that fails it forgets the
// list, so that the next page shows what is really saved.
func (s *Server) save(ol *openList) error {
	err := ol.list.Save(ol.path)
	var info os.FileInfo
	if err == nil {
		info, err = os.Stat(ol.path)
	}
	if err != nil {
		delete(s.lists, ol.key)
		return err
	}
	ol.modTime, ol.size = info.ModTime(), info.Size()
	return nil
}

// view holds what every page's layout needs.
type view struct {
	Title   string
	Page    string
	List    *listHeader
	Message string
	Error   string
}

type listHeader struct {
	Name string
	URL  string
}

func (s *Server) view(r *http.Request, page string, ol *openList) view {
	v := view{Page: page, Message: r.URL.Query().Get("msg"), Error: r.URL.Query().Get("err")}
	if ol != nil {
		v.Title = ol.list.Name
		v.List = &listHeader{Name: ol.list.Name, URL: listURL(ol.key)}
	}
	return v
}

func listURL(key string) string { return "/lists/" + url.PathEscape(key) }

func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	var b bytes.Buffer
	if err := s.pages[page].ExecuteTemplate(&b, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	b.WriteTo(w)
}

type messageView struct {
	view
	Detail string
}

// message shows a page with just a heading and a sentence.
func (s *Server) message(w http.ResponseWriter, status int, title, detail string) {
	s.render(w, status, "message", messageView{view: view{Title: title, Page: "message"}, Detail: detail})
}

// back sends the browser to target after a form, with a message or an
// error to show there.
func back(w http.ResponseWriter, r *http.Request, target, msg, errText string) {
	q := url.Values{}
	if msg != "" {
		q.Set("msg", msg)
	}
	if errText != "" {
		q.Set("err", sentence(errText))
	}
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// sentence turns an error message into a sentence for the page.
func sentence(msg string) string {
	r, n := utf8.DecodeRuneInString(msg)
	msg = string(unicode.ToUpper(r)) + msg[n:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}

func (s *Server) quitNow(w http.ResponseWriter, r *http.Request) {
	s.message(w, http.StatusOK, "tierlist has stopped", "Everything is saved. You can close this tab.")
	s.quitOnce.Do(func() { close(s.quit) })
}
