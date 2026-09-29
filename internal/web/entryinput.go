package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// An entryInput is one entry from the box for adding entries: its name,
// and a URL and description if the box gives them. Adding a name already
// in the list changes only what the box says: a URL or description given,
// even empty, replaces the old one, and one not given is kept.
type entryInput struct {
	name, url, description string
	hasURL, hasDescription bool
}

// jsonStart tells JSON in the box for adding entries from a title that
// merely starts with a bracket, such as [REC]: an object, or a list whose
// first item is an object or a string, or that is empty.
var jsonStart = regexp.MustCompile(`^(\{|\[\s*[{"\]])`)

// parseEntries reads the box for adding entries: one title a line, or JSON
// (see parseEntriesJSON). A line's title is all of it, commas and all.
func parseEntries(text string) ([]entryInput, error) {
	if jsonStart.MatchString(strings.TrimSpace(text)) {
		return parseEntriesJSON(text)
	}
	var out []entryInput
	for _, line := range strings.Split(text, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			out = append(out, entryInput{name: name})
		}
	}
	return out, nil
}

// ignoredKeys are what the entries box on the entries page writes besides
// the details the box for adding entries takes, so that its text can be
// added, to this list or another, as it is.
var ignoredKeys = map[string]bool{"rating": true, "ciWidth": true, "answers": true}

// parseEntriesJSON reads entries written as JSON: a list of them, or just
// one, each an object with "name" and optionally "url" and "description",
// or else just a name, as a string.
func parseEntriesJSON(text string) ([]entryInput, error) {
	var raw json.RawMessage
	dec := json.NewDecoder(strings.NewReader(text))
	if err := dec.Decode(&raw); err != nil {
		return nil, jsonError(text, err)
	}
	if rest := strings.TrimLeftFunc(text[dec.InputOffset():], unicode.IsSpace); rest != "" {
		line, column := position(text, len(text)-len(rest))
		return nil, fmt.Errorf("there is more after the JSON ends, at line %d, column %d; put all the entries in one list, as in [{...}, {...}]", line, column)
	}
	items := []json.RawMessage{raw}
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		items = nil
		if err := json.Unmarshal(raw, &items); err != nil { // raw is a list, read already
			return nil, fmt.Errorf("the JSON can't be read: %v", err)
		}
	}
	out := make([]entryInput, 0, len(items))
	for k, item := range items {
		e, err := parseEntryJSON(item)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", k+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// parseEntryJSON reads one entry: an object with "name" and optionally
// "url" and "description", or just a name, as a string.
func parseEntryJSON(item json.RawMessage) (entryInput, error) {
	var e entryInput
	if json.Unmarshal(item, &e.name) == nil {
		e.name = strings.TrimSpace(e.name)
	} else {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(item, &fields); err != nil {
			return e, errors.New(`an entry is an object, as in {"name": "Alien"}, or a name, as in "Alien"`)
		}
		for key, value := range fields {
			var s *string
			switch key {
			case "name":
				s = &e.name
			case "url":
				s, e.hasURL = &e.url, true
			case "description":
				s, e.hasDescription = &e.description, true
			default:
				if ignoredKeys[key] {
					continue
				}
				return e, fmt.Errorf(`unknown key %q; an entry has "name", and may have "url" and "description"`, key)
			}
			if err := json.Unmarshal(value, s); err != nil {
				return e, fmt.Errorf("%q must be text, in quotes", key)
			}
			*s = strings.TrimSpace(*s)
		}
	}
	if e.name == "" {
		return e, errors.New(`it has no "name"`)
	}
	var err error
	if e.url, err = webAddress(e.url); err != nil {
		return e, err
	}
	return e, nil
}

// jsonError explains a JSON mistake in text, saying where it is, as a line
// and column, if the decoder says.
func jsonError(text string, err error) error {
	var syntax *json.SyntaxError
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF):
		return errors.New("the JSON stops before it is finished; a bracket or quote may be missing")
	case !errors.As(err, &syntax) || syntax.Offset < 1 || syntax.Offset > int64(len(text)):
		return fmt.Errorf("the JSON can't be read: %v", err)
	}
	// The decoder stops just after the byte it can't read.
	i := int(syntax.Offset) - 1
	line, column := position(text, i)
	if r, _ := utf8.DecodeRuneInString(text[i:]); strings.ContainsRune("“”‘’", r) {
		return fmt.Errorf("the JSON can't be read at line %d, column %d: it has a curly quote, %c, where JSON needs a straight one (\")", line, column, r)
	}
	return fmt.Errorf("the JSON can't be read at line %d, column %d: %v", line, column, err)
}

// position gives the line and column, counting from 1, of the character
// that starts at byte i of text.
func position(text string, i int) (line, column int) {
	before := text[:i]
	return strings.Count(before, "\n") + 1, utf8.RuneCountInString(before[strings.LastIndexByte(before, '\n')+1:]) + 1
}

// bareHost is a host written without a scheme that looks like a web
// address: it ends in a dot and letters, as in example.com.
var bareHost = regexp.MustCompile(`\.[A-Za-z]{2,}\.?$`)

// webAddress checks a URL given for an entry, adding https:// to one
// written without a scheme, as in example.com/game. An empty one is fine;
// it means none.
func webAddress(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	full := s
	if !strings.Contains(s, "://") {
		full = "https://" + s
	}
	u, err := url.Parse(full)
	switch {
	case err != nil || strings.ContainsAny(s, " \t") || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "",
		full != s && !bareHost.MatchString(u.Hostname()):
		return "", fmt.Errorf("%q isn't a web address", s)
	}
	return full, nil
}
