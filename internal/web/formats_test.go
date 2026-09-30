package web

import (
	"encoding/json"
	"fmt"
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

// The formats page tells people, and language models, how to write entries
// and list files, so its examples are read here by the code that reads
// them, and it must name every key that code reads or writes.

const formatsDoc = "../../docs/formats.md"

// A docExample is a code block in a Markdown file, marked with a comment
// such as <!-- check: entries --> saying what it is an example of.
type docExample struct {
	where      string // file:line
	kind, text string
}

var checkMark = regexp.MustCompile(`^<!-- check: ([a-z-]+) -->$`)

// docExamples returns the marked code blocks in the Markdown file at path.
// Every JSON block must be marked, so that none goes unchecked.
func docExamples(t *testing.T, path string) []docExample {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	var out []docExample
	kind, marked := "", 0
	for i := 0; i < len(lines); i++ {
		if m := checkMark.FindStringSubmatch(lines[i]); m != nil {
			kind, marked = m[1], i+1
			continue
		}
		if !strings.HasPrefix(lines[i], "```") {
			if kind != "" && strings.TrimSpace(lines[i]) != "" {
				t.Errorf("%s:%d: the check mark isn't followed by a code block", name, marked)
				kind = ""
			}
			continue
		}
		end := slices.Index(lines[i+1:], "```")
		if end < 0 {
			t.Fatalf("%s:%d: the code block doesn't end", name, i+1)
		}
		switch {
		case kind != "":
			out = append(out, docExample{fmt.Sprintf("%s:%d", name, i+1), kind, strings.Join(lines[i+1:i+1+end], "\n")})
		case lines[i] == "```json":
			t.Errorf("%s:%d: a JSON example with no check mark before it", name, i+1)
		}
		kind, i = "", i+1+end
	}
	return out
}

func TestFormatsExamples(t *testing.T) {
	examples := append(docExamples(t, formatsDoc), docExamples(t, "../../README.md")...)
	kinds := make(map[string]int)
	for _, ex := range examples {
		kinds[ex.kind]++
		switch ex.kind {
		case "plain", "entries":
			// The box for adding entries reads them as plain titles or as
			// JSON, as the example is meant.
			got, err := parseEntries(ex.text)
			if err != nil || len(got) == 0 || jsonStart.MatchString(strings.TrimSpace(ex.text)) != (ex.kind == "entries") {
				t.Errorf("%s: %s example read as %+v, %v", ex.where, ex.kind, got, err)
			}
			var names []string
			for _, e := range got {
				names = append(names, e.name)
			}
			if ex.kind == "plain" && !slices.Equal(names, strings.Split(ex.text, "\n")) {
				t.Errorf("%s: plain titles read as %q", ex.where, names)
			}
		case "entries-copy":
			// Just what the entries page would write for these entries, and
			// the box for adding entries takes it as it is.
			var entries []entryJSON
			dec := json.NewDecoder(strings.NewReader(ex.text))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&entries); err != nil {
				t.Errorf("%s: %v", ex.where, err)
				continue
			}
			rows := make([]entryRow, len(entries))
			for i, e := range entries {
				rows[i] = entryRow{Name: e.Name, URL: e.URL, Description: e.Description, Answers: e.Answers,
					Rating: strconv.Itoa(e.Rating), SD: "± " + strconv.Itoa(e.CIWidth)}
			}
			if want := entriesJSON(rows); ex.text != want {
				t.Errorf("%s: the entries page would write\n%s", ex.where, want)
			}
			if got, err := parseEntries(ex.text); err != nil || len(got) != len(entries) {
				t.Errorf("%s: adding the entries: %+v, %v", ex.where, got, err)
			}
		case "list", "display", "template":
			text := ex.text
			if ex.kind == "template" {
				text = `{"template": ` + text + `, "convention": "top-closed", "groupRule": "middle-entry", "prefer": "higher"}`
			}
			if ex.kind != "list" {
				text = `{"format": "tierlist", "version": 1, "name": "Example", "display": ` + text + `}`
			}
			if _, _, err := tierlist.Import(t.TempDir(), []byte(text)); err != nil {
				t.Errorf("%s: the %s example can't be imported: %v", ex.where, ex.kind, err)
			}
		default:
			t.Errorf("%s: unknown check %q", ex.where, ex.kind)
		}
	}
	// Each kind of example is there, so the marks are being found.
	for _, kind := range []string{"plain", "entries", "entries-copy", "list", "display", "template"} {
		if kinds[kind] == 0 {
			t.Errorf("no %s examples checked", kind)
		}
	}
}

// TestFormatsKeys checks that the formats page names every key of a list
// file, of the box for adding entries, and of the entries page's JSON.
func TestFormatsKeys(t *testing.T) {
	data, err := os.ReadFile(formatsDoc)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"format", "version", "name", "url", "description"}
	for key := range ignoredKeys {
		keys = append(keys, key)
	}
	for _, v := range []any{tierlist.List{}, tierlist.Entry{}, tierlist.Comparison{}, tierlist.Display{}, tierlist.Template{}, entryJSON{}} {
		rt := reflect.TypeOf(v)
		for i := range rt.NumField() {
			if key, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ","); key != "" && key != "-" {
				keys = append(keys, key)
			}
		}
	}
	for _, key := range keys {
		if !strings.Contains(string(data), "`"+key+"`") {
			t.Errorf("%s doesn't name the key %q", formatsDoc, key)
		}
	}
}
