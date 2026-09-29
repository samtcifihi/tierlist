package tierlist

import (
	"bytes"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFileFormat(t *testing.T) {
	l := mustNew(t, "Films & shows", "Alien", "Brazil")
	mustRecord(t, l, 1, 2, FirstBetter)
	mustRecord(t, l, 2, 1, AboutSame)
	l.AddEntry("Casablanca")
	l.RemoveEntry(3)
	l.SetFocus([]int{2})
	l.IgnoreEntry(1)
	l.IgnorePair(2, 1)
	got, err := l.encode()
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "format": "tierlist",
  "version": 1,
  "name": "Films & shows",
  "entries": [
    {"id":1,"name":"Alien"},
    {"id":2,"name":"Brazil"},
    {"id":3,"name":"Casablanca","removed":true}
  ],
  "comparisons": [
    {"a":1,"b":2,"answer":"a"},
    {"a":2,"b":1,"answer":"same"}
  ],
  "focus": [2],
  "ignoredEntries": [1],
  "ignoredPairs": [[2,1]],
  "display": {
    "template": {
      "kind": "stars",
      "maxStars": 10
    },
    "convention": "top-closed",
    "drawMargin": 0,
    "groupRule": "middle-entry",
    "prefer": "higher"
  }
}
`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// An empty list keeps its arrays.
	empty, _ := mustNew(t, "Empty").encode()
	if !bytes.Contains(empty, []byte(`"entries": [],`)) || !bytes.Contains(empty, []byte(`"comparisons": [],`)) {
		t.Errorf("empty list encoded as\n%s", empty)
	}
}

// Saving and loading must give back the same list, which then carries on
// exactly where the saved one would have.
func TestSaveAndLoad(t *testing.T) {
	l := mustNew(t, "Letters", "A", "B", "C", "D", "E", "F", "G", "H")
	truth := map[int]float64{1: 350, 2: 240, 3: 180, 4: 60, 5: 0, 6: -90, 7: -200, 8: -310}
	play(t, l, truth, 25, rand.New(rand.NewPCG(2, 2)))
	l.SetFocus([]int{7, 8})
	l.Display = Display{
		Template:   Template{Kind: "custom", Name: "Thirds", Tiers: []string{"Top", "Middle", "Bottom"}, Cutoffs: []string{"1/3", "0.666"}},
		Convention: "bottom-closed", DrawMargin: 15, GroupRule: "alternate", Prefer: "lower",
	}
	fit, err := l.Fit()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "letters.json")
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	l2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := l.encode()
	b, _ := l2.encode()
	if !bytes.Equal(a, b) {
		t.Fatalf("saved\n%s\nloaded\n%s", a, b)
	}

	fit2, err := l2.Fit()
	if err != nil {
		t.Fatal(err)
	}
	for id := range truth {
		if math.Abs(fit.Rating(id)-fit2.Rating(id)) > 1e-6 || math.Abs(fit.SD(id)-fit2.SD(id)) > 1e-6 {
			t.Errorf("entry %d: %g ± %g before saving, %g ± %g after", id, fit.Rating(id), fit.SD(id), fit2.Rating(id), fit2.SD(id))
		}
	}
	for k := range 5 {
		a1, b1, err1 := l.NextPair(rand.New(rand.NewPCG(uint64(k), 3)))
		a2, b2, err2 := l2.NextPair(rand.New(rand.NewPCG(uint64(k), 3)))
		if a1 != a2 || b1 != b2 || err1 != nil || err2 != nil {
			t.Errorf("next pair %d, %d (%v) before saving, %d, %d (%v) after", a1, b1, err1, a2, b2, err2)
		}
		if a1 != 7 && a1 != 8 && b1 != 7 && b1 != 8 {
			t.Errorf("focus mode was lost: next pair %d, %d", a1, b1)
		}
	}
	rows1, err1 := l.Tiers()
	rows2, err2 := l2.Tiers()
	if !reflect.DeepEqual(rows1, rows2) || err1 != nil || err2 != nil {
		t.Errorf("tiers %v (%v) before saving, %v (%v) after", rows1, err1, rows2, err2)
	}

	// Saving again replaces the file and leaves nothing else behind.
	mustRecord(t, l2, 1, 8, FirstBetter)
	if err := l2.Save(path); err != nil {
		t.Fatal(err)
	}
	if l3, err := Load(path); err != nil || len(l3.Comparisons) != 26 {
		t.Errorf("after saving again: %v", err)
	}
	if des, _ := os.ReadDir(dir); len(des) != 1 {
		t.Errorf("files left in the folder: %v", des)
	}
}

func TestSaveNamedTiers(t *testing.T) {
	l := mustNew(t, "Letters", "A", "B")
	l.Display.Template = Template{Kind: "named", Tiers: []string{"S", "A", "B", "C"}, Sizes: "beta", Alpha: "1/2", Beta: "2"}
	path := filepath.Join(t.TempDir(), "letters.json")
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte(`"kind": "named"`)) {
		t.Errorf("saved named tiers:\n%s", data)
	}
	if l2, err := Load(path); err != nil || !reflect.DeepEqual(l2.Display, l.Display) {
		t.Errorf("loaded display %+v, %v; want %+v", l2.Display, err, l.Display)
	}
}

func TestSaveRefusesInvalidList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "films.json")
	l := mustNew(t, "Films", "Alien", "Brazil")
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	l.Comparisons = append(l.Comparisons, Comparison{A: 1, B: 9, Answer: FirstBetter}) // bypassing Record
	if err := l.Save(path); err == nil {
		t.Error("saving a comparison with a missing entry: want an error")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("a refused save changed the file")
	}
	l.Comparisons = l.Comparisons[:0]
	l.Entries[0].Rating = math.NaN()
	if err := l.Save(path); err == nil {
		t.Error("saving a rating that is not a number: want an error")
	}
	if des, _ := os.ReadDir(dir); len(des) != 1 {
		t.Errorf("files left in the folder: %v", des)
	}
}

func TestLoadRejects(t *testing.T) {
	const valid = `{"format":"tierlist","version":1,"name":"L",` +
		`"entries":[{"id":1,"name":"A"},{"id":2,"name":"B"}],` +
		`"comparisons":[{"a":1,"b":2,"answer":"a"}],` +
		`"display":{"template":{"kind":"stars","maxStars":5},"convention":"top-closed","drawMargin":0,"groupRule":"middle-entry","prefer":"higher"}}`
	if _, err := decode([]byte(valid)); err != nil {
		t.Fatalf("the valid file does not load: %v", err)
	}
	tests := []struct{ name, old, new, wantErr string }{
		{"not JSON", valid, "{", "not a readable tier list"},
		{"trailing data", valid, valid + "{}", "unexpected data"},
		{"unknown field", `"name":"L"`, `"name":"L","colour":"red"`, "unknown field"},
		{"not a tier list", `"format":"tierlist"`, `"format":"shopping"`, "not a tier list file"},
		{"newer version", `"version":1`, `"version":2`, "newer version"},
		{"no version", `"version":1,`, ``, "unknown format version"},
		{"no name", `"name":"L"`, `"name":" "`, "no name"},
		{"missing entry", `"b":2`, `"b":3`, "comparison 1: there is no entry 3"},
		{"self-comparison", `"b":2`, `"b":1`, "compared with itself"},
		{"unknown answer", `"answer":"a"`, `"answer":"maybe"`, `unknown answer "maybe"`},
		{"duplicate ID", `{"id":2`, `{"id":1`, "two entries have ID 1"},
		{"ID 0", `{"id":2`, `{"id":0`, "IDs start at 1"},
		{"blank entry name", `"name":"B"`, `"name":""`, "entry 2 has no name"},
		{"focus on a missing entry", `"display"`, `"focus":[5],"display"`, "focus: entry 5"},
		{"focus on a removed entry", `{"id":2,"name":"B"}],`, `{"id":2,"name":"B","removed":true}],"focus":[2],`, "focus: entry 2"},
		{"negative draw setting", `"display"`, `"drawElo":-1,"display"`, "draw setting"},
		{"ignoring a missing entry", `"display"`, `"ignoredEntries":[7],"display"`, "ignored: there is no entry 7"},
		{"top mode above 100%", `"display"`, `"top":150,"display"`, "top mode"},
		{"top mode and focus mode", `"display"`, `"focus":[1],"top":20,"display"`, "both on"},
		{"ignoring a pair of one entry", `"display"`, `"ignoredPairs":[[1,1]],"display"`, "ignored pair: entry 1 is compared with itself"},
		{"bad template", `"maxStars":5`, `"maxStars":2`, "display:"},
		{"bad convention", `"top-closed"`, `"sideways"`, "display:"},
		{"bad group rule", `"middle-entry"`, `"widest"`, "display:"},
	}
	for _, tt := range tests {
		_, err := decode([]byte(strings.Replace(valid, tt.old, tt.new, 1)))
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: error %v, want one mentioning %q", tt.name, err, tt.wantErr)
		}
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("loading a missing file: want an error")
	}
	// Leaving out the entries and comparisons means there are none. The
	// OWL/NEWT template was once called Hogwarts, and lists saved then
	// still load.
	bare := `{"format":"tierlist","version":1,"name":"L",` +
		`"display":{"template":{"kind":"hogwarts"},"convention":"top-closed","drawMargin":0,"groupRule":"middle-entry","prefer":"higher"}}`
	if l, err := decode([]byte(bare)); err != nil || l.Entries == nil || l.Comparisons == nil || l.Display.Template.Kind != "owl-newt" {
		t.Errorf("a list without entries or comparisons, saved with the Hogwarts template: %v, %v", l, err)
	}
}

func TestCreateAndLists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lists") // Create makes it
	if sums, err := Lists(dir); sums != nil || err != nil {
		t.Errorf("a missing folder: %v, %v; want no lists", sums, err)
	}
	for _, c := range []struct{ name, file string }{
		{"Films", "films.json"},
		{"Films", "films-2.json"},
		{"films!", "films-3.json"},
		{"CON", "con-list.json"},
	} {
		path, l, err := Create(dir, c.name)
		if err != nil || filepath.Base(path) != c.file || l.Name != c.name {
			t.Errorf("Create(%q) = %s, %v, want file %s", c.name, path, err, c.file)
		}
	}
	if _, _, err := Create(dir, "  "); err == nil {
		t.Error("Create with a blank name: want an error")
	}
	l, _ := Load(filepath.Join(dir, "films.json"))
	l.AddEntry("Alien")
	l.AddEntry("Brazil")
	l.AddEntry("Casablanca")
	mustRecord(t, l, 1, 2, FirstBetter)
	l.RemoveEntry(3)
	l.Save(filepath.Join(dir, "films.json"))
	for name, data := range map[string]string{
		"broken.json":          "{",
		"notes.txt":            "not a list",
		"films.json.tmp-12345": "left over from a crash",
	} {
		os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644)
	}
	os.Mkdir(filepath.Join(dir, "folder.json"), 0o755)

	sums, err := Lists(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range sums {
		switch {
		case s.Err != nil:
			got = append(got, "error:"+filepath.Base(s.Path))
		default:
			got = append(got, s.Name+":"+filepath.Base(s.Path))
		}
		if s.Modified.IsZero() {
			t.Errorf("%s has no modification time", s.Path)
		}
	}
	want := []string{"error:broken.json", "CON:con-list.json", "Films:films-2.json", "Films:films.json", "films!:films-3.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lists gave %v, want %v", got, want)
	}
	if s := sums[3]; s.Entries != 2 || s.Comparisons != 1 {
		t.Errorf("Films has %d entries and %d comparisons, want 2 and 1", s.Entries, s.Comparisons)
	}
}

func TestFileName(t *testing.T) {
	tests := map[string]string{
		"Films":                        "films",
		"  Best films, ever!!  ":       "best-films-ever",
		"Crème brûlée":                 "crème-brûlée",
		"日本 映画":                        "日本-映画",
		"!!!":                          "list",
		"CON":                          "con-list",
		"com1":                         "com1-list",
		"LPT9":                         "lpt9-list",
		"Console":                      "console",
		"com10":                        "com10",
		strings.Repeat("a", 60):        strings.Repeat("a", 50),
		strings.Repeat("b", 49) + " c": strings.Repeat("b", 49),
	}
	for in, want := range tests {
		if got := fileName(in); got != want {
			t.Errorf("fileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultDir(t *testing.T) {
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user configuration folder here:", err)
	}
	if dir, err := DefaultDir(); err != nil || dir != filepath.Join(base, "tierlist") {
		t.Errorf("DefaultDir() = %q, %v", dir, err)
	}
}
