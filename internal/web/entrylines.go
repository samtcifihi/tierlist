package web

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// An entryLine is one line of the box for adding entries: a title, then a
// URL and a description if the line gives them. given counts how many of
// the three it gives, so that adding a title already in the list changes
// only what the line says: a URL or description given, even empty, replaces
// the old one, and one left off the end of the line is kept.
type entryLine struct {
	name, url, description string
	given                  int
}

// parseEntryLines reads the box for adding entries: one entry a line, as
// CSV, with a title, then optionally a URL and a description (see
// splitEntryLine). It skips blank lines and reports the first line it
// can't read, by number.
func parseEntryLines(text string) ([]entryLine, error) {
	var out []entryLine
	for n, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields, err := splitEntryLine(line)
		if err == nil && fields[0] == "" {
			err = errors.New("it has no title")
		}
		e := entryLine{name: fields[0], given: len(fields)}
		if err == nil && e.given > 1 {
			e.url, err = webAddress(fields[1])
		}
		if err != nil {
			return nil, fmt.Errorf("line %d, %q: %w", n+1, strings.TrimSpace(line), err)
		}
		if e.given > 2 {
			e.description = fields[2]
		}
		out = append(out, e)
	}
	return out, nil
}

// splitEntryLine splits a line into a title, a URL and a description,
// separated by commas, as far as the line goes. The title and URL are CSV
// fields: one with a comma in it goes in quotes, with "" for a quote
// inside. The description is the rest of the line, commas and all; in
// quotes, the quotes are taken off.
func splitEntryLine(line string) ([]string, error) {
	var fields []string
	rest := line
	for len(fields) < 2 {
		f, after, more, err := csvField(rest)
		if err != nil {
			return []string{""}, err
		}
		fields = append(fields, f)
		if !more {
			return fields, nil
		}
		rest = after
	}
	d := strings.TrimSpace(rest)
	if f, after, more, err := csvField(d); err == nil && !more && after == "" && strings.HasPrefix(d, `"`) {
		d = f // the whole description was one quoted field
	}
	return append(fields, d), nil
}

// csvField reads one field from the start of s: in quotes, with "" for a
// quote inside, or else up to the next comma. It returns the field without
// the spaces around it, what follows the comma after it, and whether there
// was a comma.
func csvField(s string) (field, rest string, more bool, err error) {
	s = strings.TrimLeft(s, " \t")
	if strings.HasPrefix(s, `"`) {
		field, rest, more, quoted, err := quotedField(s)
		if err != nil || quoted {
			return field, rest, more, err
		}
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		return strings.TrimSpace(s[:i]), s[i+1:], true, nil
	}
	return strings.TrimSpace(s), "", false, nil
}

// quotedField reads a field in quotes from the start of s, as csvField
// does. It reports quoted false if more than spaces follow the closing
// quote before the next comma, as in "Weird Al" Yankovic, which isn't a
// quoted field after all.
func quotedField(s string) (field, rest string, more, quoted bool, err error) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		if s[i] != '"' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '"' {
			b.WriteByte('"')
			i++
			continue
		}
		after := strings.TrimLeft(s[i+1:], " \t\r")
		switch {
		case after == "":
			return b.String(), "", false, true, nil
		case after[0] == ',':
			return b.String(), after[1:], true, true, nil
		}
		return "", "", false, false, nil
	}
	return "", "", false, false, errors.New("a quote isn't closed")
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
		return "", fmt.Errorf(`%q isn't a web address (a title with a comma in it needs quotes, as in "Kill Bill, Vol. 1")`, s)
	}
	return full, nil
}
