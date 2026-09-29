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

The dummy's rating is fixed at 0, so every rating is relative to it.

### Comparisons

The program presents two real entries and the user chooses one of:

- the first is better
- the second is better
- they are about the same (a draw)

Each answer is a result the Bayes Elo calculation uses. The finished comparison record is stored.

### Model

For entries A and B with ratings `rA` and `rB`, and a **draw setting** `θ ≥ 0`, all in Elo:

- P(A is better) = `f(rA - rB - θ)`
- P(B is better) = `f(rB - rA - θ)`
- P(about the same) = the rest

where `f(x) = 1 / (1 + 10^(-x/400))` is the usual Elo curve. The larger `θ`, the more room there is for "about the same": between two equally rated entries its probability is `1 - 2 f(-θ)`. Which entry was shown first makes no difference.

### Draw setting

The draw setting is estimated from the user's answers, along with the ratings. It has a prior of its own: one win, one loss and one draw between two equally rated entries. That prior alone puts `θ` at `400 log10(2)` (about 120 Elo), where "about the same" is exactly as likely as either side being better. It also keeps `θ` above 0 when the user has never answered "about the same", and finite when every answer has been.

The entries' prior games against the dummy use the plain Elo curve (`θ = 0`), so they say nothing about the draw setting.

### Fitting

The ratings and the draw setting are their most probable values given the answers and the priors (the maximum of the posterior). The log-posterior is strictly concave, so there is exactly one maximum; the program finds it with Newton's method. The ratings' uncertainty, used for choosing pairs and for ± error bars, is the inverse of the negative Hessian of the log-posterior at the maximum.

Because the maximum is unique, saved ratings only speed up the next fit: refitting the stored comparisons gives the same result.

### Choosing the next pair

By default, the program scores every candidate pair and presents the one with the highest score, breaking ties at random. The score is the expected information the comparison would give Bayes Elo, adjusted as described below.

**Expected information.** For entries A and B, pairs are ranked by

```
closeness(A, B) × uncertainty(A - B)
```

- **Closeness** is how close to a coin flip the comparison is expected to be: highest when the two ratings are equal, falling as they move apart. Precisely, it is the Fisher information of one comparison's outcome (win, draw or loss) with respect to the rating difference, under the Bayes Elo model at the current ratings.
- **Uncertainty** is the variance of the difference between the two ratings, `Var(A) + Var(B) - 2 Cov(A, B)`, from Bayes Elo's estimate of the ratings' covariance (the same estimate its ± error bars come from).

Under the usual Gaussian approximation of the ratings, the expected information gain of a comparison is `½ ln(1 + closeness × uncertainty)`, so ranking by the product is ranking by expected information. It favors entries whose ratings are close but not yet confidently known. Closeness alone is not enough: two entries with many comparisons behind them and nearly equal ratings are a coin flip, but another answer would barely change either rating.

**Adjustments.**

- **Repeats:** each earlier comparison of the same pair multiplies its score by a penalty factor (for example ½), and the same pair is not presented twice in a row unless no other pair is available. Repeats are otherwise allowed. Each comparison already lowers the pair's uncertainty, but the model treats every answer as independent, while a user who remembers an earlier answer gives less new information than the model expects.
- **Uncompared entries:** a pair that includes an entry with no comparisons yet gets a large bonus factor, so every entry gets compared at least once. (Such entries already score high, since only the prior constrains them.)
- **Sides:** the two entries are shown in random order, so a habit of picking one side does not skew the ratings.

The exact penalty and bonus factors are tuning details.

#### Focus mode

The user can choose a set of entries to focus on, for example entries added after a lot of rating has already been done. Until the user switches back to the default mode, only pairs that include at least one of those entries are scored and presented.

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

Positions on that list are mapped evenly onto the closed interval `[0, 1]`, where `0` is the worst entry and `1` is the best: with `n` entries, the `k`-th best is at `(n-k)/(n-1)`.

A tier list needs at least two entries. With one there is no span between worst and best, and not much point in a tier list anyway, so with fewer than two entries the program shows an error asking the user to add more.

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

- **Maximum stars:** a whole number, at least 3, so there is always at least one tier between the top and bottom tiers.
- **Include 0:** by default the lowest tier is 0 stars. The user can skip 0 instead, so the lowest tier is 1 star. This holds with divisions too: 1–5 stars in half-stars is 9 tiers.
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

## Code layout

- `internal/bayeselo`: fitting ratings, the draw setting and the ratings' uncertainty to the comparisons.
- `internal/tier`: tier templates (stars, Hogwarts, custom) and placing a ranked list into tiers.

## Build

Recipes live in the `justfile`. Use `just`, not `make`.

- `just test` runs the tests.
- `just vet` runs `go vet`.
- `just check` runs both.
- `just fmt` formats the Go code.

On Windows, the justfile runs recipes with PowerShell (`set windows-shell`); elsewhere `just` uses `sh`.
