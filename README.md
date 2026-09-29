# tierlist

A program that sorts a list from pairwise user judgments and outputs the result as a tierlist.

The user compares entries two at a time. The program stores those results, turns them into ratings with Bayes Elo, and then (given display options the user chooses) lumps nearby ratings into tiers.

This project uses [just](https://github.com/casey/just), not make.

## Using it

Run `just run` (or build it with `just build` and start `bin/tierlist`, `bin\tierlist.exe` on Windows). The program opens its pages in your browser at <http://127.0.0.1:7317/>. It keeps running in its window until you press **Quit** on a page, close the window, or press Ctrl+C. Starting it again while it is running just opens the running copy, so two copies never write the same lists. If a tab is already open, `just serve` starts the program without opening another; refresh the tab once the program says it is running.

- **Your lists:** the start page lists your saved tier lists, creates new ones and deletes old ones. Deleting a list first asks in a dialog, since it can't be undone.
- **Rate:** shows two entries and asks which is better, with the next 4 pairs stacked smaller above them. Click an entry or "About the same", or use the keys: <kbd>←</kbd> or <kbd>1</kbd> for the first, <kbd>↓</kbd> or <kbd>2</kbd> for about the same, <kbd>→</kbd> or <kbd>3</kbd> for the second, and <kbd>U</kbd> to undo the last answer. <kbd>4</kbd>, <kbd>5</kbd> and <kbd>6</kbd> ignore the first entry, the pair or the second entry instead of answering (see [Ignoring](#ignoring)). Below, a quiet line says what percentile the two entries of the latest answer are now at, and **Start top mode** narrows the pairs to the best entries (see [Top mode](#top-mode)).
- **Entries:** add entries (one per line, so a list can be pasted in), rename them, remove and restore them, and choose entries for focus mode. Ignored entries are marked, and **Reset ignores** asks about everything again. The list itself can be renamed, reset or deleted at the bottom. Resetting deletes every answer but keeps the entries, whose ratings start over at 1500; like deleting, it asks first in a dialog, since it can't be undone. Entries are listed best first with their rating and its uncertainty, in points: every entry starts at 1500, and an entry 100 points above another is expected to score 2:1 against it (see [Shown ratings](#shown-ratings)). **CSV, to copy** opens a box with the same list as CSV, to paste into a spreadsheet: a header row, then each entry's name, rating, CI width and number of answers. The CI width is the full width of the rating's 95% interval, which reaches 1.96 times its ± uncertainty either side, in points; removed entries are left out.
- **Tier list:** choose the display options and see the tier list, colored from blue at the top to red at the bottom, with a plain-text version to copy. Each tier shows how many entries landed in it, as in "10★ (2)".
- **Answers:** every answer so far in a table, newest first and numbered in the order given: the entry judged better, `>`, and the other entry, or both entries with `≈` between them for "about the same". Under "Filter by entry", tick entries and choose whether to see the answers involving any of them or only the answers between two of them (which needs at least two ticked). An entry's count of answers on the Entries page leads to its answers too. Answers about removed entries are shown and marked, since they still count.

Options: `-dir FOLDER` keeps lists somewhere else, `-port N` uses another port, and `-no-browser` doesn't open a browser.

## Platform

- **Language:** Go.
- **Interface:** a local web UI. The Go program serves its pages on `127.0.0.1` and opens them in the default browser. Listening only on `127.0.0.1` keeps it off the network and avoids the Windows Firewall prompt. It also turns away requests addressed to any other host name, which defeats DNS rebinding, and changes that come from another website's page.
- **Packaging:** the HTML, CSS and JavaScript are embedded in the program, so it ships as a single executable.
- **Frontend:** server-rendered HTML with as little JavaScript as practical. No Node/npm build step.
- **Target:** Windows first. The code should stay portable; it is also developed and tested on Linux.

## Entries

Entries are text. Optional pictures for entries would be nice but are not required.

Removing an entry hides it: it leaves the tier list, focus mode and new comparisons, but its answers still count toward the other entries' ratings. (If A beat X and X beat B, that still says A is better than B.) A removed entry can be restored.

## Rating

Sorting uses **Bayes Elo**.

### Prior

Every real entry starts with a prior of one win and one loss against a single dummy entry. The dummy exists only for the prior; it is not part of the user-facing list and is not shown on the tierlist.

The dummy's rating is fixed at 0, so every rating is relative to it. The pages show ratings on a scale of their own, where the dummy, and so any entry nobody has compared yet, is at 1500 (see [Shown ratings](#shown-ratings)).

### Comparisons

The program presents two real entries and the user chooses one of:

- the first is better
- the second is better
- they are about the same (a draw)

Each answer is a result the Bayes Elo calculation uses. The finished comparison record is stored.

**Undo** takes back the most recent answer and asks that question again. It can be repeated, and works across sessions, since answers are saved in order.

#### Ignoring

Instead of answering, the user can **ignore** the first entry, the pair, or the second entry, for example an entry they don't recognize, or a pair they can't call but don't want to answer "about the same" either. An ignored pair isn't asked again, and an ignored entry isn't asked about at all, until the user presses **Reset ignores** on the Entries page; the rating page offers it too once everything left to ask is ignored. Ignores are saved with the list.

Ignoring isn't an answer: it changes no rating, and an ignored entry stays in the tier list with the rating its answers so far give it. It is left out of the levels readout, though, since the user may not know it.

Undo takes back ignores as well as answers, newest first, and asks the pair again, the same way round. Ignores made before the program was last started can't be undone one by one, only reset together.

### Model

For entries A and B with ratings `rA` and `rB`, and a **draw setting** `θ ≥ 0`, all in Elo:

- P(A is better) = `f(rA - rB - θ)`
- P(B is better) = `f(rB - rA - θ)`
- P(about the same) = the rest

where `f(x) = 1 / (1 + 10^(-x/400))` is the usual Elo curve. The larger `θ`, the more room there is for "about the same": between two equally rated entries its probability is `1 - 2 f(-θ)`. Which entry was shown first makes no difference.

The curve fixes the scale in Elo: without draws, every 400 Elo of gap multiplies the odds of the higher entry being called better by 10, so 2:1 is about 120 Elo (`400 log10(2)`) and 3:1 about 191. With draws, the odds at a given gap also depend on `θ` and on how "about the same" is counted. With `θ` at 120, for example, "better" comes up twice as often as "worse" at a gap of about 91 Elo, while an expected score of 2:1, counting "about the same" as half, takes about 135. So the pages don't show Elo, but points on a scale that keeps an expected score of 2:1 at 100 points (see [Shown ratings](#shown-ratings)).

### Draw setting

The draw setting says how close two entries have to be for the user to call them about the same. The pages show it as a percentage: how often two equally rated entries are called about the same, `1 - 2 f(-θ)`.

Two ways to picture `θ` itself, both exact in the model:

- It is the rating gap at which "better" becomes a coin flip. An entry rated `θ` above another is called better only half the time; the other half splits between "about the same" and the reverse.
- It is the width of the "too close to call" zone. Summed over every rating gap, the probability of "about the same" comes to exactly `2θ`, so each entry is roughly about the same as anything rated within `θ` of it either way.

With `θ` at about 120 Elo (a draw setting of 33%), for example:

| Gap in Elo | Gap in points | Better | About the same | Worse |
| --- | --- | --- | --- | --- |
| 0 | 0 | 33% | 33% | 33% |
| 120 | 89 | 50% | 30% | 20% |
| 240 | 178 | 67% | 22% | 11% |
| 400 | 297 | 83% | 12% | 5% |

Seen the other way round, a list whose ratings spread over `R` Elo has room for about `R / (2θ)` levels the user can tell apart. Exactly, the number of **levels** is 1 divided by the probability that two randomly chosen entries of the list would be called about the same. (The rule of thumb gets rough when `R` is not much bigger than `θ`.)

The draw setting is estimated from the user's answers, along with the ratings, rather than chosen. A number of levels given up front would not pin it down: converting it needs the list's spread in Elo, which only the answers reveal, and a list may cover only part of its domain, so the same user says "about the same" more often in a list of close favorites than in a broad one. The answers measure `θ` directly, separately for each list, including how readily the user answers "about the same". Pairs are chosen to be close, so the share of "about the same" answers runs above the rate for random pairs; the model allows for that by judging each answer against its pair's rating gap.

The draw setting has a prior of its own: one win, one loss and one draw between two equally rated entries. That prior alone puts `θ` at `400 log10(2)` (about 120 Elo, a draw setting of 33%), where "about the same" is exactly as likely as either side being better. It also keeps `θ` above 0 when the user has never answered "about the same", and finite when every answer has been.

The entries' prior games against the dummy use the plain Elo curve (`θ = 0`), so they say nothing about the draw setting.

**Levels readout.** Once every entry has been compared at least 3 times, the program shows how many levels the user is telling apart in the list, as defined above, from the fitted ratings and draw setting. (Before that, the ratings have not spread out yet and the number would be misleadingly low.)

### Shown ratings

The pages show ratings in **points** rather than Elo, on a scale pinned to the expected score:

- A gap of 100 points means the higher entry's **expected score** is twice the lower's: counting "about the same" as half a win for each, it would score 2 to the other's 1 in the long run. In the model, the expected score of an entry `d` Elo above another is `½ f(d - θ) + ½ f(d + θ)`.
- An entry nobody has compared yet shows 1500, where the dummy would.

So a rating of `r` Elo shows as `1500 + 100 r / g` points, where `g` is the gap in Elo that gives an expected score of 2:1 at the current draw setting:

```
g = 400 log10((c + √(c² + 8)) / 2),  where c = cosh(θ ln(10) / 400)
```

`g` is about 120 Elo without draws (`400 log10(2)`), about 135 at the prior's draw setting of 33%, and about 157 at 50%. The ± uncertainty and the draw-margin are in points too.

Other gaps mean nearly the same at any draw setting up to 50%: 3:1 takes about 155 to 158 points and 10:1 about 300 to 330. Without draws, every further 100 points doubles the odds again, so 200 points is 4:1 and 300 is 8:1. Draws make the odds grow a little faster beyond 100 points: at 33%, 200 points is about 4.1:1 and 300 about 8.5:1.

Because the scale follows the draw setting, the shown ratings all stretch or shrink a little when it moves, even for entries whose answers haven't changed. The order of the entries never depends on the scale.

### Fitting

The ratings and the draw setting are their most probable values given the answers and the priors (the maximum of the posterior). The log-posterior is strictly concave, so there is exactly one maximum; the program finds it with Newton's method. The ratings' uncertainty, used for choosing pairs and for ± error bars, is the inverse of the negative Hessian of the log-posterior at the maximum.

Because the maximum is unique, saved ratings only speed up the next fit: refitting the stored comparisons gives the same result. The fit depends only on the stored comparisons, not their order, so it can always be redone from scratch, for example after a change to the model, and the result is what that change would always have given. A refit cannot change which pairs were asked, since those were chosen with the old model, but that does not bias the ratings.

### Choosing the next pair

By default, the program scores every candidate pair (two entries that are neither removed nor ignored, in a pair that isn't ignored) and presents the one with the highest score, breaking ties at random. The score is the expected information the comparison would give Bayes Elo, adjusted as described below.

**Expected information.** For entries A and B, pairs are ranked by

```
sensitivity(A, B) × uncertainty(A - B)
```

- **Sensitivity** is how strongly the answer depends on the exact rating gap, which is highest where the answer is hardest to predict. While the draw setting is below 50% (`θ` below `400 log10(3)`, about 191 Elo), that is when the two ratings are equal. Above it, equally rated entries are called about the same more often than not, so the most informative pairs are instead roughly `θ` apart, where "better" and "about the same" are about equally likely. Precisely, sensitivity is the Fisher information of one comparison's outcome (win, draw or loss) with respect to the rating difference, under the Bayes Elo model at the current ratings.
- **Uncertainty** is the variance of the difference between the two ratings, `Var(A) + Var(B) - 2 Cov(A, B)`, from Bayes Elo's estimate of the ratings' covariance (the same estimate its ± error bars come from).

Under the usual Gaussian approximation of the ratings, the expected information gain of a comparison is `½ ln(1 + sensitivity × uncertainty)`, so ranking by the product is ranking by expected information. It favors pairs whose answer is hard to predict and whose rating gap is not yet confidently known. Sensitivity alone is not enough: two entries with many comparisons behind them and nearly equal ratings may be a coin flip, but another answer would barely change either rating.

**Adjustments.** The score is the expected information gain, multiplied by these factors:

- **Repeats:** ½ for each earlier comparison of the same pair. The same pair is also not presented twice in a row unless no other pair is available; repeats are otherwise allowed. Each comparison already lowers the pair's uncertainty, but the model treats every answer as independent, while a user who remembers an earlier answer gives less new information than the model expects.
- **Uncompared entries:** 10 for a pair that includes an entry with no comparisons yet, so every entry gets compared at least once. (Such entries already score high, since only the prior constrains them.) In practice, uncompared entries pair up with each other first, so every entry of a new list has been compared once after about half as many answers as there are entries.
- **Sides:** the two entries are shown in random order, so a habit of picking one side does not skew the ratings.
- **Top mode:** 1.5 for each entry in the pair that top mode favours (see below).

The factors are tuning details and may change.

#### Focus mode

The user can choose a set of entries to focus on, for example entries added after a lot of rating has already been done. Until the user switches back to the default mode, only pairs that include at least one of those entries are scored and presented. Focus mode is saved with the list, so it stays on across sessions.

#### Top mode

To sort out the best entries, the user can start **top mode** for a percentage `p`, from the rating page. An entry's **percentile** is its place in the list as the tier list orders it: 100 for the best entry, 0 for the worst, evenly spaced between, as tier templates place entries. Top mode then:

- favours the top `p` percent (entries at or above the `100 - p`th percentile): pairs score 1.5 times higher for each of them, on top of the usual adjustments, so they come up more often without anything else being ruled out; and
- asks only about the top `2p` percent: both entries of every pair are at or above the `100 - 2p`th percentile, which from `p = 50` up is every entry. The two best entries always count, so there is always a pair, and so does any entry with no answers yet, since its rating says nothing so far.

As answers move entries up and down, the sets move with them, and a pair lined up in the stack that no longer fits is dropped. Top mode and focus mode are never on together: starting one leaves the other. Top mode is saved with the list.

In simulations, top mode on 20% sorts out the top better: with 30 entries and 90 answers, it left a fifth fewer of the true top 6 outside the fitted top 6 than the default mode did, and put slightly fewer pairs among them in the wrong order (with 50 entries and 250 answers, an eighth fewer and a little fewer). Nearly all of that comes from asking only about the top `2p` percent. Favouring more strongly would seem to help more, but it doesn't: it spends the answers on whichever entries look best so far and misses better ones rated low early. A factor of 3 kept only about half the gain, and 9 lost all of it, so the favouring is kept mild.

The rating page also shows, quietly, the percentiles the two entries of the latest answer are now at.

#### Coming up

The rating page also shows the next 4 pairs, small enough to take in at a glance, stacked above the pair being asked with the next one nearest and the later ones fading. A pair, once shown, stays lined up until it is asked, so the stack can be trusted: after each answer it moves down one, and one new pair joins at the top. Undo puts the answered pair back in front.

Lined-up pairs are chosen before the answers to the pairs ahead of them are known. Each one counts as asked with its answer still unknown: it narrows the ratings' uncertainty by the information it is expected to carry (a rank-one update of their covariance) and counts toward repeats and the new-entry bonus, so the next pair looks elsewhere. The ratings themselves wait for real answers. Choosing 4 pairs ahead costs little: in simulated sessions with 20 entries and 60 answers, 16.6% of pairs ended up in the wrong order, against 16.2% when each pair is chosen just before it is asked and 19.0% with random pairs.

Adding, removing or restoring entries, and switching focus mode or top mode on or off, choose the pairs coming up again. The pair being asked stays, unless it no longer fits.

### Session state

Rating is meant to span multiple sessions. The program must save and reload:

- every stored decision that affects Bayes Elo (the pairwise outcomes, at the level of detail the algorithm actually uses)
- any already-computed Bayes Elo quantities that still have future value (ratings and any other derived state worth not throwing away)

Loading that state must be enough to continue rating where a previous session left off.

#### Saved files

Each tier list is saved as one JSON file, by default in a `tierlist` folder in the user's configuration folder (`%AppData%\tierlist` on Windows). The file name comes from the list's name: its letters and digits in lower case, joined by hyphens, with a number added if the name is taken. Renaming the list later does not rename the file.

A list file holds:

- the list's name
- the entries, each with a name and a stable ID, so answers keep pointing at the right entry as the list changes
- every answer, in order: the IDs of the entry shown first (`a`) and second (`b`), and the answer (`"a"`, `"b"` or `"same"`)
- the focus set, while focus mode is on
- the top mode percentage, while top mode is on
- the ignored entries and pairs, until they are reset
- the display options, with the draw-margin in the points the pages show
- each entry's last fitted rating and the last draw setting, in Elo as the model has them (ratings relative to the dummy at 0, not the points the pages show), which only speed up the next fit

For example:

```json
{
  "format": "tierlist",
  "version": 1,
  "name": "Films",
  "entries": [
    {"id":1,"name":"Alien","rating":115.86410113563915},
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

Here Brazil was shown first, and the user answered that the second entry, Alien, was better.

A file is written to a temporary file first and then renamed into place, so a crash cannot leave a half-written list. Deleting a list deletes its file for good; a file that can't be opened can be deleted from the start page too. The program refuses to save a list that would not load again, and refuses to load a file from a newer format version rather than silently dropping what it does not understand.

## Display

Display is a separate step from rating. The user selects display options; those options do not change stored comparison results.

The display options are the tier template and its options (including the interval convention), the draw-margin, and the group-placement settings, all described below. They are saved with the list. A new list starts with 0–10 stars, the top tier closed, a draw-margin of 0, the middle-entry rule, and the higher tier when a group's middle entries fall in different tiers.

### Ranked list

Behind the scenes the program lists every real entry from best to worst by Bayes Elo and notes each entry's Bayes Elo rating. That ordered list is the only input the display algorithm uses.

The display algorithm does not care by how much one entry beat another, except for the draw-margin rule below.

Positions on that list are mapped evenly onto the closed interval `[0, 1]`, where `0` is the worst entry and `1` is the best: with `n` entries, the `k`-th best is at `(n-k)/(n-1)`.

A tier list needs at least two entries. With one there is no span between worst and best, and not much point in a tier list anyway, so with fewer than two entries the program shows an error asking the user to add more.

### Draw-margin and groups

The user chooses a **draw-margin**, in the points the pages show ratings in (see [Shown ratings](#shown-ratings)).

Entries whose ratings differ by less than or equal to the draw-margin are lumped into the same group, recursively. Adjacent merges chain: with a draw-margin of `1`, entries rated `1`, `2`, and `3` all belong to one group.

On a list already ordered by rating, groups are contiguous blocks.

Whatever the user chooses, the program groups with a draw-margin of at least ten times the precision the ratings are computed to: `10 × 400 × 10⁻⁸ / ln(10)`, about 0.00002 Elo, far less than the pages can show. Ratings that are equal in theory can come out a rounding error apart (about 10⁻¹³ Elo), and without this floor a draw-margin of 0 would let rounding split them across tiers. This is not the ± uncertainty of the ratings, which is far larger and a matter for the draw-margin the user picks.

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

The user does not enter cut-offs or ranges; they are generated, in one of three ways the user chooses (the **tier sizes**):

- **Nearest star** (the default): the top and bottom tiers each cover `1/(2*(n-1))` of `[0, 1]` and every other tier covers `1/(n-1)`, so the cut-offs are at `(2k-1)/(2*(n-1))` for `k = 1, ..., n-1`. Each entry gets the star rating nearest its position.
- **Geometric**: each tier is a fixed factor (φ, the golden ratio, 1.618033988749895 to the precision of a 64-bit float, unless the user picks another number above 0) times the size of the one before it, counting from the best tier, or from the worst if the user chooses. Above 1 the tiers grow away from where the counting starts, so by default the best tier is the smallest; below 1 they shrink; exactly 1 makes every tier the same size. The sizes are scaled to add up to 1, so any number of tiers, with any factor, covers `[0, 1]` exactly with the same ratio between every pair of neighbours.
- **Beta**: two parameters, α and β, above 0. Cut `[0, 1]` into `n` equal parts; each tier gets the share of a Beta(α, β) distribution that lies over its part, so each cut-off `k/n` moves to the distribution's CDF at `k/n`. The distribution lives on `(0, 1)`, but the tiers still cover all of `[0, 1]`, ends included. Beta(1, 1) is the uniform distribution and gives tiers all exactly the same size (unlike nearest-star tiers, whose ends are half size); Beta(2, 2) makes the middle tiers bigger and the end tiers smaller, and Beta(½, ½) the other way round. A larger α makes the tiers near the top bigger, a larger β those near the bottom. When α = β the tiers are exactly symmetric.

The factor, α and β can be written as decimals or fractions, such as `1.618` or `1/2`. Extreme settings can make some tiers too small to hold any position but the end of `[0, 1]` they touch; they are kept, tiny and in order, rather than refused.

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
- `internal/pairing`: choosing the next pair to compare.
- `internal/tier`: tier templates (stars, Hogwarts, custom) and placing a ranked list into tiers.
- `internal/tierlist`: one tier list (entries, answers, focus and display options), saving and loading it, and tying it to the packages above.
- `internal/web`: the pages, served from HTML templates, a style sheet and a small script embedded in the program.
- `cmd/tierlist`: the program itself, which starts the server and opens the browser.

## Build

Recipes live in the `justfile`. Use `just`, not `make`.

- `just run` runs the program; add options after it, as in `just run -port 7400`.
- `just serve` runs the program without opening a browser; it takes options too.
- `just build` builds the program into `bin/`.
- `just test` runs the tests.
- `just vet` runs `go vet`.
- `just check` runs both.
- `just fmt` formats the Go code.

On Windows, the justfile runs recipes with PowerShell (`set windows-shell`); elsewhere `just` uses `sh`.
