package web

import (
	"slices"
	"strings"
	"testing"
)

func TestParseEntries(t *testing.T) {
	good := []struct {
		text string
		want []entryInput
	}{
		// One title a line, commas and all.
		{"Alien\r\n\r\n  Kill Bill, Vol. 1  \n[REC]\n", []entryInput{{name: "Alien"}, {name: "Kill Bill, Vol. 1"}, {name: "[REC]"}}},
		// A bracket that doesn't start a list of entries is part of a title.
		{"[7] Days\n[REC]", []entryInput{{name: "[7] Days"}, {name: "[REC]"}}},
		{"  \n ", nil},
		// JSON: a list of objects or names, or one object.
		{`[{"name": "Alien", "url": "example.com/alien", "description": "Sci-fi horror, 1979"}, "Brazil"]`,
			[]entryInput{{name: "Alien", url: "https://example.com/alien", description: "Sci-fi horror, 1979", hasURL: true, hasDescription: true}, {name: "Brazil"}}},
		{`{"name": " Alien ", "description": ""}`, []entryInput{{name: "Alien", hasDescription: true}}},
		{"\n  [\n  ]\n", []entryInput{}},
		{`["Kill Bill, Vol. 1", "Films & \"shows\""]`, []entryInput{{name: "Kill Bill, Vol. 1"}, {name: `Films & "shows"`}}},
		// What the entries box writes besides the details is left behind.
		{`[{"name":"Jaws","rating":1816,"ciWidth":141,"answers":5,"url":"https://example.com/jaws"}]`,
			[]entryInput{{name: "Jaws", url: "https://example.com/jaws", hasURL: true}}},
		// Space after the JSON is fine, as a text box sends it.
		{"[\"Alien\"]\r\n  \r\n", []entryInput{{name: "Alien"}}},
	}
	for _, c := range good {
		got, err := parseEntries(c.text)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("parseEntries(%q) = %+v, %v; want %+v", c.text, got, err, c.want)
		}
	}
	for _, c := range []struct{ text, err string }{
		{`[{"name": "Alien",}]`, "the JSON can't be read at line 1, column 19: invalid character '}'"},
		{"[\n  {\"name\": \"Alien\"},\n  {\"name\": Brazil}\n]", "the JSON can't be read at line 3, column 12"},
		{`[{"name": "Alien"}`, "the JSON stops before it is finished"},
		{`[{"name": "Alien"}] [{"name": "Brazil"}]`, "there is more after the JSON ends, at line 1, column 21; put all the entries in one list"},
		// Whatever follows the JSON, even what can't start a value.
		{`[{"name": "Alien"}]]`, "there is more after the JSON ends, at line 1, column 20"},
		{`{"name": "Alien"}}`, "there is more after the JSON ends, at line 1, column 18"},
		{"[\"Alien\"]\n\n  , \"Brazil\"", "there is more after the JSON ends, at line 3, column 3"},
		{`[{"name": "Alien"}] and more`, "there is more after the JSON ends"},
		// Columns count characters, not bytes.
		{`["Amélie", {"name": x}]`, "the JSON can't be read at line 1, column 21: invalid character 'x'"},
		// Curly quotes, as a word processor writes them.
		{`[{"name": “Alien”}]`, "the JSON can't be read at line 1, column 11: it has a curly quote, “, where JSON needs a straight one (\")"},
		{"[\n  {‘name’: \"Alien\"}\n]", "line 2, column 4: it has a curly quote, ‘"},
		{`[{"name": "Alien", "descripton": "typo"}]`, `entry 1: unknown key "descripton"`},
		{`["Alien", {"url": "example.com"}]`, `entry 2: it has no "name"`},
		{`[{"name": 7}]`, `entry 1: "name" must be text`},
		{`[{"name": "Alien", "url": "Vol. 1"}]`, `entry 1: "Vol. 1" isn't a web address`},
		{`[{"name": "Alien", "url": "javascript:alert(1)"}]`, "isn't a web address"},
		{`[{"name": "Alien", "url": "ftp://example.com"}]`, "isn't a web address"},
		{`[{"name": "Alien"}, 7]`, "entry 2: an entry is an object"},
	} {
		if _, err := parseEntries(c.text); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("parseEntries(%q): error %v, want one with %q", c.text, err, c.err)
		}
	}
}

func TestWebAddress(t *testing.T) {
	for in, want := range map[string]string{
		"":                                       "",
		"example.com":                            "https://example.com",
		"example.com/alien":                      "https://example.com/alien",
		"http://localhost:8080/x":                "http://localhost:8080/x",
		"https://en.wikipedia.org/wiki/A_(film)": "https://en.wikipedia.org/wiki/A_(film)",
	} {
		if got, err := webAddress(in); err != nil || got != want {
			t.Errorf("webAddress(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"Vol.1", "Vol. 1", "ftp://example.com", "https://exa mple.com", "localhost"} {
		if _, err := webAddress(bad); err == nil {
			t.Errorf("webAddress(%q): want an error", bad)
		}
	}
}
