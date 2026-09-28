# tierlist

A program that sorts a list from pairwise user judgments and outputs the result as a tierlist.

The user compares entries two at a time. The program stores those results, turns them into ratings with Bayes Elo, and then (given display options the user chooses) lumps nearby ratings into tiers.

This project uses [just](https://github.com/casey/just), not make.

## Platform

- **Language:** Go.
- **Interface:** a local web UI. The Go program serves its pages on `127.0.0.1` and opens them in the default browser. Listening only on `127.0.0.1` keeps it off the network and avoids the Windows Firewall prompt.
- **Packaging:** the HTML, CSS and JavaScript are embedded in the program, so it ships as a single executable.
- **Frontend:** server-rendered HTML with as little JavaScript as practical. No Node/npm build step.
- **Target:** Windows first. The code should stay portable; it is also developed and tested on Linux.

## Entries

Entries are text. Optional pictures for entries would be nice but are not required.

## Rating

Sorting uses **Bayes Elo**.

### Prior

Every real entry starts with a prior of one win and one loss against a single dummy entry. The dummy exists only for the prior; it is not part of the user-facing list and is not shown on the tierlist.

### Comparisons

The program presents two real entries and the user chooses one of:

- the first is better
- the second is better
- they are about the same (a draw)

Each answer is a result the Bayes Elo calculation uses. The finished comparison record is stored.

### Choosing the next pair

The exact method is not decided yet. It should:

- by default, present the comparison that gives Bayes Elo the most useful information; in practice, two entries whose ratings are close but not yet confidently known
- strongly favor entries that have not been compared yet, so every entry gets compared at least once
- discourage repeating a pair that has already been compared, without forbidding it

#### Focus mode

The user can choose a set of entries to focus on, for example entries added after a lot of rating has already been done. Until the user switches back to the default mode, every pair presented includes at least one of those entries.

### Session state

Rating is meant to span multiple sessions. The program must save and reload:

- every stored decision that affects Bayes Elo (the pairwise outcomes, at the level of detail the algorithm actually uses)
- any already-computed Bayes Elo quantities that still have future value (ratings and any other derived state worth not throwing away)

Loading that state must be enough to continue rating where a previous session left off.

## Display

Display is a separate step from rating. The user selects display options; those options do not change stored comparison results.

The display options are the tier template and its options (including the interval convention), the draw-margin, and the group-placement settings, all described below.

### Ranked list

Behind the scenes the program lists every real entry from best to worst by Bayes Elo and notes each entry's Bayes Elo rating. That ordered list is the only input the display algorithm uses.

The display algorithm does not care by how much one entry beat another, except for the draw-margin rule below.

Positions on that list are mapped onto the closed interval `[0, 1]`, where `0` is the worst entry and `1` is the best. With one entry there is no span between worst and best; that edge case needs a defined convention when the program is implemented.

### Draw-margin and groups

The user chooses a **draw-margin** (in Bayes Elo rating units).

Entries whose Bayes Elo ratings differ by less than or equal to the draw-margin are lumped into the same group, recursively. Adjacent merges chain: with a draw-margin of `1`, entries rated `1`, `2`, and `3` all belong to one group.

On a list already ordered by rating, groups are contiguous blocks.

### Placing a group into a tier

Each entry, considered alone, would fall in some tier according to its `[0, 1]` position and the active tier template. Groups are then placed as follows.

**Default (middle-entry) rule**

- The middle entry of the group decides the tier for every entry in the group.
- If the group has an even number of entries, there are two middle entries.
  - If both middle entries fall in the same tier, the whole group goes there.
  - If they fall in different tiers, a user setting chooses whether the whole group goes to the **higher** tier or the **lower** tier.

**Alternate rule**

If this option is on, the middle-entry rule is skipped. The same higher-vs-lower setting then means:

- put every entry in the group in the **highest** tier any member of the group would have fallen in, or
- put every entry in the group in the **lowest** tier any member of the group would have fallen in.

### Sharing

To share the tier list as an image, the user takes a screenshot of the tier list page. As a fallback, the program also shows the tier list as plain text in a text box the user can copy from.

## Tier templates

A tier template is an ordered list of tiers. Each tier has:

- a name
- a range inside the closed interval `[0, 1]`

Together the tiers must cover `[0, 1]` exactly: no gaps, no overlaps. The program must check that a template is well-formed before using it.

Ranges are not written out tier by tier. A template is defined by its tiers in order, the **cut-offs** where one tier becomes the next, and one of two **interval conventions**:

1. The **top** tier is a closed interval; every other tier is half-open of the form `[a, b)`.
2. The **bottom** tier is a closed interval; every other tier is half-open of the form `(a, b]`.

So a position exactly on a cut-off belongs to the higher tier under the first convention and to the lower tier under the second. The user chooses the interval convention for every template, built-in or custom.

### Built-in templates

#### Stars

Star templates are generated from options, and each tier is named by its number of stars:

- **Maximum stars:** a whole number, at least 2.
- **Include 0:** by default the lowest tier is 0 stars. The user can skip 0 instead, so the lowest tier is 1 star.
- **Divisions:** by default none, meaning whole stars only. Otherwise a whole number `d`, at least 2, splits each star into steps of `1/d`; `d = 2` gives half-stars.

The tiers run from the maximum number of stars down to the lowest tier in steps of `1/d` star (`d = 1` for whole stars), so there are `n = (max - lowest) * d + 1` tiers.

The user does not enter cut-offs or ranges; they are generated. The top and bottom tiers each cover `1/(2*(n-1))` of `[0, 1]` and every other tier covers `1/(n-1)`, so the cut-offs are at `(2k-1)/(2*(n-1))` for `k = 1, ..., n-1`.

The tier-placement logic only uses the number of tiers and their ranges. Fractional stars and whether 0 is included only change the tier names: 0–5 stars with half-stars and 0–10 whole stars are both 11 tiers with the same ranges, so they place every entry the same way.

#### Hogwarts

A fixed template. The user chooses its interval convention (and, as for any template, the draw-margin and group-placement settings) but not its tiers or cut-offs. From best to worst:

| Tier | This tier and above | This tier alone |
| --- | --- | --- |
| Outstanding | top 1/31 | 1/31 |
| Exceeds Expectations | top 3/31 | 2/31 |
| Acceptable | top 6/31 | 3/31 |
| Poor | top 10/31 | 4/31 |
| Dreadful | top 15/31 | 5/31 |
| Troll | everything | the rest, 16/31 |

As positions on `[0, 1]` (`0` is the worst entry, `1` the best), the cut-offs are `16/31`, `21/31`, `25/31`, `28/31` and `30/31`.

### Custom templates

When creating a custom template, the user gives the cut-offs and chooses the interval convention.

Cut-offs may be entered as decimals or as fractions.

The interval convention, plus the requirement that the cut-offs partition `[0, 1]`, are what the well-formedness check should enforce for custom templates.

## Build

Recipes live in the `justfile`. Use `just`, not `make`.

On Windows, the justfile runs recipes with PowerShell (`set windows-shell`); elsewhere `just` uses `sh`. Go build and test recipes will be added along with the Go code.
