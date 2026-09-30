# Formats

This page says exactly what text tierlist takes in and gives out, so that entries and lists can be written or read elsewhere: by hand, by another program, or by a language model (see [Making a list with a language model](#making-a-list-with-a-language-model)).

| Format | Where | Direction |
|---|---|---|
| [Entries to add](#entries-to-add) | the box under **Add entries** on a list's **Entries** tab | in |
| [Entries as JSON](#entries-as-json) | **JSON, to copy**, lower down the **Entries** tab | out |
| [List files](#list-files) | **Export** and **Import a list** on the start page, and the files in the lists folder | in and out |
| [Tier list as text](#tier-list-as-text) | **Plain text, to copy** on the **Tier list** tab | out |

Everything is UTF-8 text, and the JSON is standard JSON ([RFC 8259](https://www.rfc-editor.org/rfc/rfc8259)): keys and strings in straight double quotes (`"`), no comments, and no comma after the last item of a list or object.

The simplest way to make a list from entries written elsewhere is to create it on the start page, open its **Entries** tab, and paste the entries into the box. A [list file](#list-files) makes the list in one step, with its name and display options, but it is stricter.

## Making a list with a language model

A language model can write a list of entries in the JSON for [entries to add](#entries-to-add). Give it this page with a prompt such as:

> Make a list of the 60 best-known science fiction films for tierlist, a program that ranks things by asking which of two is better. Write it in the "Entries to add" JSON format, following the rules for language models, in tierlist's format documentation at ADDRESS. Reply with the JSON only, in one code block.

Put this page's address in place of ADDRESS, or, for a model that can't open it, write "below" instead and paste this page after the prompt.

Then create the list on the start page, open its **Entries** tab, paste what is inside the code block into the box (without the ```` ``` ```` lines around it), and press **Add**.

### Rules for language models

A model writing entries for tierlist should:

1. Put the entries in a single JSON array, with nothing else in its code block: no comments, no trailing commas, and no `...` standing for entries left out.
2. Write each entry as an object with `"name"`, and with `"url"` and `"description"` where they help. No other keys.
3. Give each entry the name people know it by, with nothing added, such as a number or a rank, on one line, and short (under about 60 characters), since it goes on buttons and in the tiers. Names must all differ, ignoring case. Where two entries would share a name, add what tells them apart, such as a year: `"Solaris (1972)"` and `"Solaris (2002)"`.
4. Give a `"url"` only for a page that certainly exists, such as the entry's Wikipedia article, written in full from `https://`. Leave the key out rather than guess an address.
5. Keep a `"description"` to one short sentence (under about 150 characters) that helps the user recall the entry: what it is, and when and by whom it was made. Don't judge it: the user rates the entries, and a verdict would sway them. No line breaks.
6. Keep the entries to one kind of thing, so that any two of them can be compared.
7. Write straight double quotes (`"`) around every key and string. Inside a string, write a double quote as `\"`, or use typographic quotes (“ ” ‘ ’), which are fine there.
8. If a long list might be cut off, split it into several complete arrays, each in its own code block. They can be pasted one after another: adding is cumulative, and a name given again updates its entry rather than adding another.

For example:

<!-- check: entries -->
```json
[
  {"name": "Alien", "url": "https://en.wikipedia.org/wiki/Alien_(film)", "description": "Ridley Scott's horror film about a creature loose on a cargo ship, 1979."},
  {"name": "Blade Runner", "url": "https://en.wikipedia.org/wiki/Blade_Runner", "description": "Ridley Scott's neo-noir about a hunter of artificial humans, 1982."},
  {"name": "Solaris (1972)", "description": "Andrei Tarkovsky's film of the Stanisław Lem novel about a mysterious ocean planet."}
]
```

**How many entries.** Every answer counts for both entries in it, and the ratings settle once the entries have about 3 answers each, so a list of n entries takes about 1.5 × n answers at least (150 for 100 entries), and more for a fine ranking.

**A whole list in one step.** To have the model set up the list's name, its question and its tiers too, ask for a [list file](#list-files) instead, as in "…as a tierlist list file that asks "Which would you rather watch?", with named tiers S, A, B, C and D", and paste it into **Import a list** on the start page. The rules above apply to its entries, and the list file's own rules to the rest.

## Entries to add

On a list's **Entries** tab, the box under **Add entries** takes entries to add, and **Add** adds them.

The box is read as JSON if its text, after any spaces and blank lines at the start, begins with `{`, or with `[` followed (after any spaces or line breaks) by `{`, `"` or `]`. Anything else is read as plain titles, one per line. So `[REC]` is read as a title; but so is JSON with anything in front of it, such as a sentence or a Markdown code fence (```` ```json ````), which then adds each of its lines as an entry.

### Plain titles

Each line is one entry's name: all of it, commas, brackets and quotes included. Spaces at the start and end of a line are dropped, and blank lines are skipped. Plain titles can't give links or descriptions; JSON can.

<!-- check: plain -->
```text
Alien
Kill Bill, Vol. 1
[REC]
```

### JSON

The text is one JSON value, with nothing but spaces and line breaks before or after it: a list (array) of entries, or a single entry. Each entry is an object, or a string, which is just its name. An entry object's keys, which are case-sensitive:

| Key | | Value |
|---|---|---|
| `name` | required | The entry's name, a string that isn't empty. |
| `url` | optional | A web page about the entry, as a string (see below). `""` means none. |
| `description` | optional | A few words about the entry, as a string. `""` means none. |
| `rating`, `ciWidth`, `answers` | ignored | What [entries as JSON](#entries-as-json) gives besides the above; they are skipped, so that its text can be pasted in as it is. |

Any other key is refused, to catch misspellings, and so is a `name`, `url` or `description` that isn't a string. Spaces at the start and end of the three strings are dropped.

A `url` must be a web address: `https://` or `http://` and then the site, with no spaces. One written without `https://`, as in `en.wikipedia.org/wiki/Alien_(film)`, gets it added, as long as the site's name ends in a dot and at least two letters (`.org`, `.com`, …). Anything else, such as `ftp://…` or `javascript:…`, is refused.

In a string, a double quote is written `\"` and a backslash `\\`, and a line break can't be typed as it is. Other characters, such as é, ’ or –, can be.

<!-- check: entries -->
```json
[
  {"name": "Alien", "url": "https://en.wikipedia.org/wiki/Alien_(film)", "description": "Sci-fi horror, 1979"},
  {"name": "Brazil", "description": "Terry Gilliam's bureaucratic dystopia, 1985"},
  "Casablanca"
]
```

A single entry needn't be in a list:

<!-- check: entries -->
```json
{"name": "Alien", "description": ""}
```

### What adding does

- Names are matched, ignoring case, with the entries in the list, apart from removed ones. A name not in the list adds an entry.
- A name already in the list updates that entry instead of adding another: a `url` or `description` given replaces the old one, even `""`, which clears it, and one left out stays as it was. So a plain title, or a name alone, changes nothing about an entry already there. The entry keeps its answers and rating.
- A name given twice in one paste adds its entry once; the second updates it.
- A name that matches only a removed entry adds a new entry; to get the removed one's answers back, restore it on the **Entries** tab instead.
- It's all or nothing: if any of the text can't be read, nothing is added, and the page shows the text again with where the trouble is, as a line and column of the JSON, or an entry's number, counting from 1.
- Afterwards the page says what it did, as in "Added 3 entries. Updated 1 entry already in the list. Skipped 2 names already in the list." A skipped name was already in the list, with nothing to change.

## Entries as JSON

**JSON, to copy**, lower down a list's **Entries** tab once it has entries, gives them as a JSON array, best first, one entry a line:

<!-- check: entries-copy -->
```json
[
  {"name":"Alien","rating":1811,"ciWidth":142,"answers":5,"url":"https://en.wikipedia.org/wiki/Alien_(film)","description":"Sci-fi horror, 1979"},
  {"name":"Casablanca","rating":1635,"ciWidth":107,"answers":8},
  {"name":"Brazil","rating":1442,"ciWidth":103,"answers":8,"description":"Terry Gilliam's bureaucratic dystopia, 1985"}
]
```

| Key | Value |
|---|---|
| `name` | The entry's name. |
| `rating` | Its rating in points, a whole number, as the page shows it. Every entry starts at 1500, and an entry 100 points above another is expected to score 2:1 against it. |
| `ciWidth` | The uncertainty of the rating in points, a whole number: one standard deviation, the ± figure the page shows after the rating. |
| `answers` | How many answers the entry is in, "about the same" included. |
| `url` | Its link; left out if it has none. |
| `description` | Its description; left out if it has none. |

The keys come in this order. Removed entries are left out, and ignored ones are in. Characters such as `&` and `<` are written as they are, not escaped.

Pasted into the box for [entries to add](#entries-to-add), of the same list or another, the text adds or updates the entries with their links and descriptions; `rating`, `ciWidth` and `answers` are skipped. To move the answers and ratings too, move the whole [list file](#list-files).

## List files

Each list is one JSON file in the lists folder: a `tierlist` folder in the user's configuration folder (`%AppData%\tierlist` on Windows, `~/Library/Application Support/tierlist` on macOS, `~/.config/tierlist`, or `$XDG_CONFIG_HOME/tierlist`, on Linux), unless the program is started with `-dir FOLDER`. **Export** on the start page shows a list's file, to copy, and **Import a list** takes the text of one, pasted in, and saves it as a new list.

Importing:

- The whole text is checked first. If anything is wrong, nothing is saved, and the start page shows the text again with the reason.
- An import never replaces a list. If there is a list of the same name already, ignoring case, the new list's name gets a number, as in "Films (2)".
- Ratings aren't read from the file: they are worked out from the answers (`comparisons`), and the `rating` and `drawElo` values the program saves only make that quicker. So a list with no answers starts with every entry at 1500, whatever the file says.

For the whole file:

- It is one JSON object, with nothing after it but spaces and line breaks, and no byte order mark before it.
- A key not described here, anywhere in the file, makes it unreadable: the program refuses what it doesn't know rather than lose it. Write the keys as shown; the program matches them ignoring case, but always writes them this way.
- The keys of an object may come in any order. The program writes them in the order below, with one entry or answer a line.
- IDs and other whole numbers are written without a decimal point: `3`, not `3.0`.
- The numbers of tier sizes and cut-offs are strings, as in `"1/2"` or `"0.25"`, kept as they were typed.

### The list

| Key | | Value |
|---|---|---|
| `format` | required | `"tierlist"` |
| `version` | required | `1`. A file with a higher version, from a newer tierlist, is refused. |
| `name` | required | The list's name, a string that isn't empty. |
| `question` | optional | What the **Rate** tab asks about each pair, as in `"Which is funnier?"`. Left out, or `""`, it asks "Which is better?". |
| `entries` | optional | The entries, as a list of [entry objects](#entry-objects); none if left out. |
| `comparisons` | optional | The answers, oldest first, as a list of [answer objects](#answer-objects); none if left out. |
| `focus` | optional | Focus mode's entries, while it is on: a list of entry IDs, each once, none of them removed. |
| `top` | optional | Top mode's percentage, while it is on: a number above 0 and at most 100. Not together with `focus`. |
| `ignoredEntries` | optional | The entries left out of the pairs asked, as a list of entry IDs. |
| `ignoredPairs` | optional | The pairs left out of the pairs asked, as a list of pairs of entry IDs, as in `[[1, 2], [3, 5]]`, each of two different entries. |
| `display` | required | The [display options](#display-options). |
| `drawElo` | optional | The draw setting from the last fit, in Elo, a number 0 or more. It only speeds up the next fit; leave it out of a new file. |

### Entry objects

| Key | | Value |
|---|---|---|
| `id` | required | A whole number from 1 up, different for each entry. Answers and the other lists refer to entries by it. IDs needn't be in order or without gaps, but 1, 2, 3, … is simplest. |
| `name` | required | The entry's name, a string that isn't empty. |
| `url` | optional | A web page about the entry. |
| `description` | optional | A few words about the entry. |
| `removed` | optional | `true` for a removed entry, which is out of the tier list and new pairs, though its answers still count. |
| `rating` | optional | The entry's rating from the last fit, in Elo. It only speeds up the next fit; leave it out of a new file. |

The program checks only that the IDs differ and that names aren't empty, but a file should keep to the rules the **Entries** tab does:

- names differ, ignoring case, and each is on one line;
- no spaces at the start or end of a name, link or description;
- each `url` is a full web address, from `https://` or `http://`. A file's links are used as they are written, unlike those in the box for entries to add, which gets `https://` added and refuses what isn't a web address.

### Answer objects

| Key | Value |
|---|---|
| `a` | The ID of the entry shown first. |
| `b` | The ID of the entry shown second, not the same as `a`. |
| `answer` | `"a"` if the first was better, `"b"` if the second was, or `"same"` for about the same. |

Both IDs must be entries in the file; removed entries count. The order of the answers doesn't change the ratings. It is the order the **Answers** tab numbers them in, and **Undo** takes back the last.

### Display options

| Key | | Value |
|---|---|---|
| `template` | required | The tier template, as a [template object](#template-objects). |
| `others` | optional | The options last used for the other kinds of template, to come back to: a list of template objects, at most one each of `"stars"`, `"named"` and `"custom"`, and none of the kind in `template`. Leave it out of a new file. |
| `convention` | required | `"top-closed"`, which puts an entry exactly on a cut-off in the tier above it, or `"bottom-closed"`, which puts it in the tier below. |
| `drawMargin` | optional | Entries whose ratings are at most this many points apart are grouped together, and each group goes into one tier: a number 0 or more, 0 if left out. |
| `proportional` | optional | What a tier's share of `[0, 1]` is a share of: `"entry"` for the entries, counted by rank (the default, also meant when left out), or `"rating"` for the range of ratings, from the lowest to the highest. |
| `groupRule` | required | How a group is placed: `"middle-entry"` (its middle entry decides) or `"alternate"` (its highest or lowest entry decides). |
| `prefer` | required | `"higher"` or `"lower"`: which way a group goes when its middle entries fall in different tiers, or, with `"alternate"`, whether its highest or lowest entry decides. |

A new list's display options are:

<!-- check: display -->
```json
{"template": {"kind": "stars", "maxStars": 10}, "convention": "top-closed", "drawMargin": 0, "groupRule": "middle-entry", "prefer": "higher"}
```

### Template objects

A template object's `kind` is required: `"stars"`, `"named"`, `"owl-newt"` or `"custom"`. Its other keys depend on the kind, and those of other kinds should be left out.

- **Stars:** `maxStars` (required), the stars of the top tier, a whole number, 3 or more; `skipZero` (optional), `true` to start at 1 star rather than 0; `divisions` (optional), the parts each star is split into, 0 or 1 for whole stars, 2 for half stars and so on; and the [tier sizes](#tier-sizes). There can be at most 1000 tiers.
- **Named:** `tiers` (required), the tier names, best first, as a list of 1 to 1000 strings, none empty; and the [tier sizes](#tier-sizes).
- **OWL/NEWT:** no other keys. (`"hogwarts"`, its old name, is read as `"owl-newt"`.)
- **Custom:** `name` (optional), the template's name; `tiers` (required), the tier names, best first, none empty; and `cutoffs`, where one tier gives way to the next, from the bottom up: one fewer than the tiers (so none, or left out, for a single tier), as increasing numbers from 0 to 1, each a string holding a decimal, as in `"0.25"`, or a fraction, as in `"1/4"`. With `"top-closed"` a cut-off can't be 0, and with `"bottom-closed"` it can't be 1, since either would leave a tier empty.

<!-- check: template -->
```json
{"kind": "named", "tiers": ["S", "A", "B", "C", "D"], "sizes": "beta", "alpha": "2", "beta": "2"}
```

<!-- check: template -->
```json
{"kind": "custom", "name": "Thirds", "tiers": ["Good", "Fine", "Bad"], "cutoffs": ["1/3", "2/3"]}
```

### Tier sizes

Stars and named tiers are sized the same ways:

| Key | | Value |
|---|---|---|
| `sizes` | optional | Left out for the default, which the form calls **nearest tier**: the tiers stand evenly spaced from worst to best, and each entry gets the nearest, so the top and bottom tiers are half the size of the others. (Don't write `"even"`, the form's name for it, which a file doesn't take.) `"geometric"` makes each tier a fixed multiple of the size of the one before; `"beta"` sizes the tiers by a Beta distribution. |
| `factor` | with `"geometric"` | The multiple, a number above 0, as a string, as in `"1.618033988749895"` or `"3/2"`. |
| `from` | optional, with `"geometric"` | `"best"` (the default) to count from the best tier, or `"worst"`. |
| `alpha`, `beta` | with `"beta"` | The distribution's α and β: numbers above 0 and at most 1000000, as strings, as in `"2"` or `"1/2"`. |

Sizes too extreme to work out are refused.

<!-- check: template -->
```json
{"kind": "stars", "maxStars": 5, "skipZero": true, "divisions": 2, "sizes": "geometric", "factor": "1.618033988749895", "from": "worst"}
```

### A whole file

A new list of three entries with its own question and named tiers, as a language model might write it:

<!-- check: list -->
```json
{
  "format": "tierlist",
  "version": 1,
  "name": "Science fiction films",
  "question": "Which would you rather watch?",
  "entries": [
    {"id": 1, "name": "Alien", "url": "https://en.wikipedia.org/wiki/Alien_(film)", "description": "Ridley Scott's horror film about a creature loose on a cargo ship, 1979."},
    {"id": 2, "name": "Blade Runner", "url": "https://en.wikipedia.org/wiki/Blade_Runner", "description": "Ridley Scott's neo-noir about a hunter of artificial humans, 1982."},
    {"id": 3, "name": "Solaris (1972)", "description": "Andrei Tarkovsky's film of the Stanisław Lem novel about a mysterious ocean planet."}
  ],
  "display": {
    "template": {"kind": "named", "tiers": ["S", "A", "B", "C", "D"]},
    "convention": "top-closed",
    "drawMargin": 0,
    "groupRule": "middle-entry",
    "prefer": "higher"
  }
}
```

A list as the program saves it, after one answer (Brazil was shown first, and the second entry, Alien, was called better):

<!-- check: list -->
```json
{
  "format": "tierlist",
  "version": 1,
  "name": "Films",
  "entries": [
    {"id":1,"name":"Alien","url":"https://en.wikipedia.org/wiki/Alien_(film)","description":"Sci-fi horror, 1979","rating":115.86410113563915},
    {"id":2,"name":"Brazil","rating":-115.86410113563912}
  ],
  "comparisons": [
    {"a":2,"b":1,"answer":"b"}
  ],
  "display": {
    "template": {
      "kind": "stars",
      "maxStars": 10
    },
    "convention": "top-closed",
    "drawMargin": 0,
    "groupRule": "middle-entry",
    "prefer": "higher"
  },
  "drawElo": 102.09813849908167
}
```

## Tier list as text

**Plain text, to copy**, below the tier list on the **Tier list** tab, gives the tier list for people to read; the program doesn't read it back. Each tier is a line, best first: the tier's name (star tiers with a ★), the number of entries in it in parentheses, a colon, and its entries, best first, separated by a comma and a space. A tier with no entries ends at the colon. Since names can hold commas themselves, the entries of a line can't always be told apart; for a program, [entries as JSON](#entries-as-json) or the [list file](#list-files) is the better choice.

```text
S (2): Alien, Blade Runner
A (0):
B (1): Solaris (1972)
```
