package web

import (
	"slices"
	"strings"
	"testing"
)

func TestParseEntryLines(t *testing.T) {
	good := []struct {
		line string
		want entryLine
	}{
		{"Alien", entryLine{name: "Alien", given: 1}},
		{"  Alien  \r", entryLine{name: "Alien", given: 1}},
		{"Alien, https://example.com/alien", entryLine{name: "Alien", url: "https://example.com/alien", given: 2}},
		// A description may hold commas; a bare domain gets https://.
		{"Alien, example.com/alien, Sci-fi horror, 1979, with 1,000 fans",
			entryLine{name: "Alien", url: "https://example.com/alien", description: "Sci-fi horror, 1979, with 1,000 fans", given: 3}},
		{`"Kill Bill, Vol. 1", http://example.org/kb, "Revenge, in two parts"`,
			entryLine{name: "Kill Bill, Vol. 1", url: "http://example.org/kb", description: "Revenge, in two parts", given: 3}},
		// Empty fields are given, as empty; leaving them off isn't.
		{"Alien,, A film", entryLine{name: "Alien", description: "A film", given: 3}},
		{"Alien, https://example.com,", entryLine{name: "Alien", url: "https://example.com", given: 3}},
		{"Alien,", entryLine{name: "Alien", given: 2}},
		// Quotes that don't make a quoted field stay, and "" is a quote.
		{`The "Thing", example.com`, entryLine{name: `The "Thing"`, url: "https://example.com", given: 2}},
		{`"Weird Al" Yankovic, example.com`, entryLine{name: `"Weird Al" Yankovic`, url: "https://example.com", given: 2}},
		{`"""Heroes""", example.com, He said "we can be"`, entryLine{name: `"Heroes"`, url: "https://example.com", description: `He said "we can be"`, given: 3}},
		{`Alien, example.com, "A ""classic"", some say"`, entryLine{name: "Alien", url: "https://example.com", description: `A "classic", some say`, given: 3}},
		{"Local, http://localhost:8080/x", entryLine{name: "Local", url: "http://localhost:8080/x", given: 2}},
		{"Alien, https://en.wikipedia.org/wiki/Alien_(film)", entryLine{name: "Alien", url: "https://en.wikipedia.org/wiki/Alien_(film)", given: 2}},
	}
	for _, c := range good {
		got, err := parseEntryLines(c.line)
		if err != nil || len(got) != 1 || got[0] != c.want {
			t.Errorf("parseEntryLines(%q) = %+v, %v; want %+v", c.line, got, err, c.want)
		}
	}
	// Blank lines are skipped, and every other line is an entry.
	got, err := parseEntryLines("Alien\r\n\r\n  \nBrazil, example.com\n")
	if err != nil || !slices.Equal(got, []entryLine{{name: "Alien", given: 1}, {name: "Brazil", url: "https://example.com", given: 2}}) {
		t.Errorf("two lines with blanks: %+v, %v", got, err)
	}
	for _, c := range []struct{ text, err string }{
		{"Kill Bill, Vol. 1", `line 1, "Kill Bill, Vol. 1": "Vol. 1" isn't a web address`},
		{"Alien\nKill Bill, Vol.1", `line 2, "Kill Bill, Vol.1": "Vol.1" isn't a web address`},
		{"Alien, ftp://example.com", `"ftp://example.com" isn't a web address`},
		{"Alien, javascript:alert(1)", `isn't a web address`},
		{"Alien, https://exa mple.com", `isn't a web address`},
		{`"Alien, example.com`, "a quote isn't closed"},
		{", example.com", "it has no title"},
	} {
		if _, err := parseEntryLines(c.text); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("parseEntryLines(%q): error %v, want one with %q", c.text, err, c.err)
		}
	}
}
