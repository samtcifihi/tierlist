package tierlist

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	formatName    = "tierlist"
	formatVersion = 1
)

// file is the saved form of a list.
type file struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	*List
}

// Load reads the list saved at path.
func Load(path string) (*List, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	l, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return l, nil
}

func decode(data []byte) (*List, error) {
	f := file{List: &List{}}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("not a readable tier list: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("not a readable tier list: unexpected data after the list")
	}
	switch {
	case f.Format != formatName:
		return nil, errors.New("not a tier list file")
	case f.Version > formatVersion:
		return nil, fmt.Errorf("saved by a newer version of the program (format %d)", f.Version)
	case f.Version < 1:
		return nil, fmt.Errorf("unknown format version %d", f.Version)
	}
	l := f.List
	if err := l.validate(); err != nil {
		return nil, err
	}
	if l.Entries == nil {
		l.Entries = []Entry{}
	}
	if l.Comparisons == nil {
		l.Comparisons = []Comparison{}
	}
	return l, nil
}

// Save writes the list to path, replacing any file there. It writes a
// temporary file first and then renames it into place, so a crash cannot
// leave a half-written list. It refuses to save a list that would not load.
func (l *List) Save(path string) error {
	data, err := l.encode()
	if err != nil {
		return fmt.Errorf("not saving %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// encode returns the list as JSON, with one entry or comparison per line
// so that the file stays easy to read.
func (l *List) encode() ([]byte, error) {
	if err := l.validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	field := func(key string, value []byte) {
		if b.Len() == 0 {
			b.WriteString("{\n")
		} else {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, "  %q: %s", key, value)
	}
	for _, f := range []struct {
		key   string
		value any
		lines bool
		skip  bool
	}{
		{key: "format", value: formatName},
		{key: "version", value: formatVersion},
		{key: "name", value: l.Name},
		{key: "entries", value: l.Entries, lines: true},
		{key: "comparisons", value: l.Comparisons, lines: true},
		{key: "focus", value: l.Focus, skip: len(l.Focus) == 0},
		{key: "ignoredEntries", value: l.IgnoredEntries, skip: len(l.IgnoredEntries) == 0},
		{key: "ignoredPairs", value: l.IgnoredPairs, skip: len(l.IgnoredPairs) == 0},
		{key: "display", value: l.Display},
		{key: "drawElo", value: l.DrawElo, skip: l.DrawElo == 0},
	} {
		if f.skip {
			continue
		}
		var data []byte
		var err error
		switch v := f.value.(type) {
		case []Entry:
			data, err = lines(v)
		case []Comparison:
			data, err = lines(v)
		case []int, [][2]int:
			data, err = marshal(v, false)
		default:
			data, err = marshal(v, true)
		}
		if err != nil {
			return nil, err
		}
		field(f.key, data)
	}
	b.WriteString("\n}\n")
	return b.Bytes(), nil
}

// lines encodes a slice as a JSON array with one compact element per line.
func lines[T any](xs []T) ([]byte, error) {
	if len(xs) == 0 {
		return []byte("[]"), nil
	}
	var b bytes.Buffer
	b.WriteString("[")
	for k, x := range xs {
		data, err := marshal(x, false)
		if err != nil {
			return nil, err
		}
		if k > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n    ")
		b.Write(data)
	}
	b.WriteString("\n  ]")
	return b.Bytes(), nil
}

// marshal encodes v without escaping HTML characters, so that names such
// as "Tom & Jerry" stay readable. With indent, nested values are indented
// to sit inside the top-level object.
func marshal(v any, indent bool) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("  ", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// DefaultDir returns the folder where lists are kept by default: tierlist
// in the user's configuration folder, which is %AppData% on Windows.
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "tierlist"), nil
}

// Create makes a new, empty list called name, saves it in dir under a file
// name made from name, and returns its path.
func Create(dir, name string) (string, *List, error) {
	l, err := New(name)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	base := fileName(l.Name)
	for k := 1; k <= 1000; k++ {
		file := base + ".json"
		if k > 1 {
			file = fmt.Sprintf("%s-%d.json", base, k)
		}
		path := filepath.Join(dir, file)
		// Claim the name first, so two lists never share a file.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		f.Close()
		if err := l.Save(path); err != nil {
			os.Remove(path)
			return "", nil, err
		}
		return path, l, nil
	}
	return "", nil, fmt.Errorf("too many lists called %q", l.Name)
}

// fileName turns a list's name into a file name without extension: its
// letters and digits in lower case, with anything else in between turned
// into single hyphens, at most 50 characters, and never a name Windows
// reserves.
func fileName(name string) string {
	var b strings.Builder
	count, gap := 0, false
	for _, r := range strings.ToLower(name) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			gap = gap || count > 0
			continue
		}
		need := 1
		if gap {
			need = 2
		}
		if count+need > 50 {
			break
		}
		if gap {
			b.WriteByte('-')
		}
		b.WriteRune(r)
		count += need
		gap = false
	}
	s := b.String()
	switch {
	case s == "":
		return "list"
	case slices.Contains([]string{"con", "prn", "aux", "nul"}, s),
		len(s) == 4 && (strings.HasPrefix(s, "com") || strings.HasPrefix(s, "lpt")) && s[3] >= '0' && s[3] <= '9':
		return s + "-list"
	}
	return s
}

// A Summary describes a saved list.
type Summary struct {
	Path        string
	Name        string
	Entries     int // not counting removed entries
	Comparisons int
	Modified    time.Time
	// Err is set if the file could not be loaded; the other fields except
	// Path and Modified are then empty.
	Err error
}

// Lists describes the lists saved in dir, sorted by name. A missing dir
// has no lists.
func Lists(dir string) ([]Summary, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, de := range des {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		s := Summary{Path: filepath.Join(dir, de.Name())}
		if info, err := de.Info(); err == nil {
			s.Modified = info.ModTime()
		}
		if l, err := Load(s.Path); err != nil {
			s.Err = err
		} else {
			s.Name, s.Entries, s.Comparisons = l.Name, len(l.Shown()), len(l.Comparisons)
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Summary) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out, nil
}
