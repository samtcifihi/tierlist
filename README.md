# tierlist

A program that sorts a list from pairwise user judgments and outputs the result as a tierlist.

The user compares entries two at a time. The program stores those results, turns them into ratings with Bayes Elo, and then (given display options the user chooses) lumps nearby ratings into tiers.

This project uses [just](https://github.com/casey/just), not make.

## Open questions

These are not decided yet. Do not treat any language or platform as implied by this repository.

- Programming language
- Target platform

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

How the program chooses which pair to present next is an implementation detail left for later; the requirement is that rating proceeds by these three-way pairwise judgments.

### Session state

Rating is meant to span multiple sessions. The program must save and reload:

- every stored decision that affects Bayes Elo (the pairwise outcomes, at the level of detail the algorithm actually uses)
- any already-computed Bayes Elo quantities that still have future value (ratings and any other derived state worth not throwing away)

Loading that state must be enough to continue rating where a previous session left off.

## Display

Display is a separate step from rating. The user selects display options; those options do not change stored comparison results.

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

## Tier templates

A tier template is an ordered list of tiers. Each tier has:

- a name
- a range inside the closed interval `[0, 1]`

Together the tiers must cover `[0, 1]` exactly: no gaps, no overlaps. The program must check that a template is well-formed before using it.

### Built-in templates

Default templates should include:

- **0–5 stars** — six named star ratings from 0 through 5
- **0–10 stars** — eleven named star ratings from 0 through 10
- **Hogwarts** — a bespoke House-themed template; the names and cut-offs will be specified later

Until that last template is specified, keep a reserved place for it rather than inventing its contents.

### Custom templates

When creating a custom template, the user gives the **cut-offs** where one tier becomes the next, not a hand-written range for every tier.

Cut-offs may be entered as decimals or as fractions.

The user also chooses one of two interval conventions:

1. The **top** tier is a closed interval; every other tier is half-open of the form `[a, b)`.
2. The **bottom** tier is a closed interval; every other tier is half-open of the form `(a, b]`.

Those two conventions, plus the requirement that the cut-offs partition `[0, 1]`, are what the well-formedness check should enforce for custom templates.

## Build

Recipes live in the `justfile`. Use `just`, not `make`.

Until a language and target platform are chosen, the justfile only lists itself.
