package pairing

import (
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/samtcifihi/tierlist/internal/bayeselo"
)

func fit(t testing.TB, n int, h []bayeselo.Comparison) *bayeselo.Result {
	t.Helper()
	res, err := bayeselo.Fit(n, h, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func next(t testing.TB, res *bayeselo.Result, h []bayeselo.Comparison, opts Options, rng *rand.Rand) [2]int {
	t.Helper()
	a, b, err := Next(res, h, opts, rng)
	if err != nil {
		t.Fatal(err)
	}
	return [2]int{a, b}
}

func cmp(a, b int) bayeselo.Comparison { return bayeselo.Comparison{A: a, B: b} }

// answer answers a comparison of entries a and b the way the model says a
// user would, given their true ratings.
func answer(truth []float64, drawElo float64, a, b int, rng *rand.Rand) bayeselo.Outcome {
	better, same, _ := bayeselo.Probabilities(truth[a]-truth[b], drawElo)
	switch u := rng.Float64(); {
	case u < better:
		return bayeselo.AWins
	case u < better+same:
		return bayeselo.Draw
	}
	return bayeselo.BWins
}

// play asks count questions chosen by Next, answers them from the true
// ratings, and returns h with them appended.
func play(t testing.TB, truth []float64, h []bayeselo.Comparison, opts Options, count int, rng *rand.Rand) []bayeselo.Comparison {
	t.Helper()
	var res *bayeselo.Result
	for range count {
		var err error
		if res, err = bayeselo.Fit(len(truth), h, res); err != nil {
			t.Fatal(err)
		}
		p := next(t, res, h, opts, rng)
		h = append(h, bayeselo.Comparison{A: p[0], B: p[1], Outcome: answer(truth, 120, p[0], p[1], rng)})
	}
	return h
}

func randomTruth(n int, rng *rand.Rand) []float64 {
	truth := make([]float64, n)
	for i := range truth {
		truth[i] = 250 * rng.NormFloat64()
	}
	return truth
}

func TestNextRejects(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 1))
	for _, n := range []int{0, 1} {
		if _, _, err := Next(fit(t, n, nil), nil, Options{}, rng); err == nil {
			t.Errorf("%d entries: want an error", n)
		}
	}
	res := fit(t, 3, nil)
	for _, h := range [][]bayeselo.Comparison{{cmp(0, 3)}, {cmp(-1, 0)}, {cmp(1, 1)}} {
		if _, _, err := Next(res, h, Options{}, rng); err == nil {
			t.Errorf("history %v: want an error", h)
		}
	}
	for _, opts := range []Options{{Focus: []int{3}}, {Hidden: []int{-1}}, {Hidden: []int{0, 1}}, {Focus: []int{2}, Hidden: []int{2}},
		{Skip: [][2]int{{0, 3}}}, {Skip: [][2]int{{0, 1}, {1, 2}, {2, 0}}}} {
		if _, _, err := Next(res, nil, opts, rng); err == nil {
			t.Errorf("%+v: want an error", opts)
		}
	}
}

func TestFavour(t *testing.T) {
	// With no answers every pair scores the same, so favouring entry 2 puts
	// it in every pair picked, and favouring 2 and 3 picks that pair.
	res := fit(t, 5, nil)
	for s := range 20 {
		rng := rand.New(rand.NewPCG(uint64(s), 3))
		if p := next(t, res, nil, Options{Favour: []int{2}}, rng); p[0] != 2 && p[1] != 2 {
			t.Errorf("favouring 2, picked %v", p)
		}
		if p := next(t, res, nil, Options{Favour: []int{2, 3}}, rng); key(p[0], p[1]) != [2]int{2, 3} {
			t.Errorf("favouring 2 and 3, picked %v", p)
		}
	}
	if _, _, err := Next(res, nil, Options{Favour: []int{5}}, rand.New(rand.NewPCG(1, 1))); err == nil {
		t.Error("favouring a missing entry: want an error")
	}
}

func TestSkip(t *testing.T) {
	res := fit(t, 3, nil)
	// With 0–1 and 0–2 skipped, 1–2 is all that is left, even straight
	// after it was asked.
	opts := Options{Skip: [][2]int{{1, 0}, {0, 2}}}
	for _, h := range [][]bayeselo.Comparison{nil, {cmp(2, 1)}} {
		for s := range 5 {
			if p := next(t, res, h, opts, rand.New(rand.NewPCG(uint64(s), 2))); key(p[0], p[1]) != [2]int{1, 2} {
				t.Errorf("history %v: picked %v", h, p)
			}
		}
	}
	// With every pair skipped, there is nothing to ask.
	opts.Skip = append(opts.Skip, [2]int{2, 1})
	if _, _, err := Next(res, nil, opts, rand.New(rand.NewPCG(1, 2))); !errors.Is(err, ErrNoPair) {
		t.Errorf("every pair skipped: error %v, want ErrNoPair", err)
	}
}

// With no answers yet every pair scores the same, so every pair should come
// up, about equally often, in both orders.
func TestFirstPairIsRandom(t *testing.T) {
	res := fit(t, 4, nil)
	rng := rand.New(rand.NewPCG(2, 2))
	seen := make(map[[2]int]int)
	for range 1200 {
		seen[next(t, res, nil, Options{}, rng)]++
	}
	if len(seen) != 12 {
		t.Fatalf("saw %d ordered pairs, want all 12: %v", len(seen), seen)
	}
	for p, k := range seen {
		if k < 50 || k > 150 {
			t.Errorf("pair %v came up %d times out of 1200, want about 100", p, k)
		}
	}
}

// Next must pick a pair with the highest score, never the pair just asked.
func TestPicksBestScore(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 3))
	for trial := range 20 {
		truth := randomTruth(8, rng)
		h := play(t, truth, nil, Options{}, 10+trial, rng)
		res := fit(t, len(truth), h)
		perEntry := make([]int, len(truth))
		perPair := make(map[[2]int]int)
		for _, c := range h {
			perEntry[c.A]++
			perEntry[c.B]++
			perPair[key(c.A, c.B)]++
		}
		last := key(h[len(h)-1].A, h[len(h)-1].B)
		best := 0.0
		for i := range truth {
			for j := i + 1; j < len(truth); j++ {
				if p := [2]int{i, j}; p != last {
					best = max(best, score(res, i, j, perPair[p], perEntry[i] == 0 || perEntry[j] == 0, 0))
				}
			}
		}
		p := next(t, res, h, Options{}, rng)
		k := key(p[0], p[1])
		got := score(res, k[0], k[1], perPair[k], perEntry[k[0]] == 0 || perEntry[k[1]] == 0, 0)
		if k == last || got < best*(1-tieTolerance) {
			t.Errorf("trial %d: picked %v scoring %g; best score is %g, last pair %v", trial, k, got, best, last)
		}
	}
}

// These give Next a fit with no answers, where every pair has the same
// expected gain, so only the adjustments tell pairs apart.
func TestAdjustments(t *testing.T) {
	rng := rand.New(rand.NewPCG(4, 4))
	tests := []struct {
		name  string
		n     int
		h     []bayeselo.Comparison
		opts  Options
		allow [][2]int // the pairs Next may pick
	}{
		{"repeats lose to fresh pairs, and the last pair is skipped", 4,
			[]bayeselo.Comparison{cmp(0, 1), cmp(0, 1), cmp(0, 1), cmp(2, 3)}, Options{},
			[][2]int{{0, 2}, {0, 3}, {1, 2}, {1, 3}}},
		{"the only unrepeated pair wins", 3,
			[]bayeselo.Comparison{cmp(0, 1), cmp(1, 2)}, Options{},
			[][2]int{{0, 2}}},
		{"repeats are allowed", 3,
			[]bayeselo.Comparison{cmp(0, 1), cmp(1, 2), cmp(0, 2)}, Options{},
			[][2]int{{0, 1}, {1, 2}}},
		{"the last pair again, when it is the only pair", 2,
			[]bayeselo.Comparison{cmp(0, 1)}, Options{},
			[][2]int{{0, 1}}},
		// Without the bonus, the unrepeated pair (1, 2) would tie with these.
		{"uncompared entries first", 4,
			[]bayeselo.Comparison{cmp(0, 1), cmp(0, 2)}, Options{},
			[][2]int{{0, 3}, {1, 3}, {2, 3}}},
		{"focus", 5, nil, Options{Focus: []int{4}},
			[][2]int{{0, 4}, {1, 4}, {2, 4}, {3, 4}}},
		{"focus on two entries", 5, []bayeselo.Comparison{cmp(1, 3)}, Options{Focus: []int{1, 3}},
			[][2]int{{0, 1}, {1, 2}, {1, 4}, {0, 3}, {2, 3}, {3, 4}}},
		{"hidden entries are skipped", 4, nil, Options{Hidden: []int{1, 3}},
			[][2]int{{0, 2}}},
		{"hidden and focus together", 5, nil, Options{Focus: []int{0}, Hidden: []int{2, 4}},
			[][2]int{{0, 1}, {0, 3}}},
		{"the last pair again, when hiding leaves nothing else", 4,
			[]bayeselo.Comparison{cmp(0, 3)}, Options{Hidden: []int{1, 2}},
			[][2]int{{0, 3}}},
	}
	for _, tt := range tests {
		res := fit(t, tt.n, nil)
		seen := make(map[[2]int]bool)
		for range 200 {
			p := next(t, res, tt.h, tt.opts, rng)
			k := key(p[0], p[1])
			ok := false
			for _, a := range tt.allow {
				ok = ok || k == a
			}
			if !ok {
				t.Errorf("%s: picked %v, want one of %v", tt.name, k, tt.allow)
				break
			}
			seen[p] = true
		}
		if len(seen) != 2*len(tt.allow) {
			t.Errorf("%s: saw %d ordered pairs in 200 tries, want every allowed pair both ways", tt.name, len(seen))
		}
	}
}

func TestSession(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 5))
	truth := randomTruth(12, rng)
	h := play(t, truth, nil, Options{}, 60, rng)

	// Uncompared entries pair up first, so all 12 are compared within 6
	// answers.
	seen := make(map[int]bool)
	for _, c := range h[:6] {
		seen[c.A], seen[c.B] = true, true
	}
	if len(seen) != 12 {
		t.Errorf("after 6 answers, %d of 12 entries have been compared", len(seen))
	}
	for k := 1; k < len(h); k++ {
		if key(h[k].A, h[k].B) == key(h[k-1].A, h[k-1].B) {
			t.Errorf("answers %d and %d are both about %v", k, k+1, key(h[k].A, h[k].B))
		}
	}

	// Entries added now are compared straight away.
	truth = append(truth, 100, -50)
	h2 := play(t, truth, h, Options{}, 2, rng)
	added := map[int]bool{}
	for _, c := range h2[len(h):] {
		added[c.A], added[c.B] = true, true
	}
	if !added[12] || !added[13] {
		t.Errorf("the two answers after adding entries 12 and 13 were %v", h2[len(h):])
	}

	// In focus mode every pair includes a focused entry.
	for _, c := range play(t, truth, h2, Options{Focus: []int{12, 13}}, 20, rng)[len(h2):] {
		if c.A < 12 && c.B < 12 {
			t.Errorf("in focus mode, asked about %d and %d", c.A, c.B)
		}
	}
}

// playQueued is play with the next few pairs fixed in advance: it keeps
// ahead pairs lined up after the one being asked, adding one with Queue
// after each answer.
func playQueued(t testing.TB, truth []float64, ahead, count int, rng *rand.Rand) []bayeselo.Comparison {
	t.Helper()
	var h []bayeselo.Comparison
	res := fit(t, len(truth), nil)
	var queue [][2]int
	for range count {
		more, err := Queue(res, h, queue, 1+ahead-len(queue), Options{}, rng)
		if err != nil {
			t.Fatal(err)
		}
		queue = append(queue, more...)
		p := queue[0]
		queue = queue[1:]
		h = append(h, bayeselo.Comparison{A: p[0], B: p[1], Outcome: answer(truth, 120, p[0], p[1], rng)})
		if res, err = bayeselo.Fit(len(truth), h, res); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

// Over a session, choosing pairs this way should order the entries better
// than asking about random pairs, even with the next 4 pairs fixed in
// advance so that they can be shown. (In longer simulations, fixing them
// costs a small fraction of the advantage over random pairs.)
func TestBeatsRandomPairs(t *testing.T) {
	const n, answers, sessions = 20, 120, 30
	wrong := func(res *bayeselo.Result, truth []float64) int {
		k := 0
		for i := range truth {
			for j := i + 1; j < len(truth); j++ {
				if (res.Ratings[i]-res.Ratings[j])*(truth[i]-truth[j]) <= 0 {
					k++
				}
			}
		}
		return k
	}
	smart, queued, random := 0, 0, 0
	for s := range sessions {
		rng := rand.New(rand.NewPCG(uint64(s), 6))
		truth := randomTruth(n, rng)
		smart += wrong(fit(t, n, play(t, truth, nil, Options{}, answers, rng)), truth)
		queued += wrong(fit(t, n, playQueued(t, truth, 4, answers, rng)), truth)

		var h []bayeselo.Comparison
		for range answers {
			a, b := rng.IntN(n), rng.IntN(n-1)
			if b >= a {
				b++
			}
			h = append(h, bayeselo.Comparison{A: a, B: b, Outcome: answer(truth, 120, a, b, rng)})
		}
		random += wrong(fit(t, n, h), truth)
	}
	t.Logf("pairs in the wrong order over %d sessions: %d with Next, %d with 4 pairs queued, %d with random pairs",
		sessions, smart, queued, random)
	if smart >= random || queued >= random {
		t.Errorf("Next left %d pairs in the wrong order, Queue %d, random pairs %d", smart, queued, random)
	}
}

func TestQueue(t *testing.T) {
	rng := func() *rand.Rand { return rand.New(rand.NewPCG(8, 8)) }
	res := fit(t, 12, nil)
	// Uncompared entries pair up, even before any answer comes in.
	pairs, err := Queue(res, nil, nil, 6, Options{}, rng())
	seen := map[int]bool{}
	for _, p := range pairs {
		seen[p[0]], seen[p[1]] = true, true
	}
	if err != nil || len(pairs) != 6 || len(seen) != 12 {
		t.Errorf("6 pairs from 12 new entries: %v (%v), covering %d entries", pairs, err, len(seen))
	}
	// With nothing pending, the first pair is Next's.
	first, err := Queue(res, nil, nil, 1, Options{}, rng())
	if a, b, _ := Next(res, nil, Options{}, rng()); err != nil || first[0] != [2]int{a, b} {
		t.Errorf("Queue gave %v, Next %d, %d", first, a, b)
	}
	// Pending pairs count as asked: after a pending pair of two entries
	// among three, the next pair brings in the third.
	res3 := fit(t, 3, nil)
	for range 10 {
		next, err := Queue(res3, nil, [][2]int{{0, 1}}, 1, Options{}, rng())
		if err != nil || (next[0] != [2]int{2, 0} && next[0] != [2]int{2, 1} && next[0] != [2]int{0, 2} && next[0] != [2]int{1, 2}) {
			t.Fatalf("after pending 0 and 1: %v, %v", next, err)
		}
	}
	// Focus and hidden entries hold for every pair.
	h := []bayeselo.Comparison{cmp(0, 1), cmp(2, 3)}
	res = fit(t, 12, h)
	pairs, _ = Queue(res, h, nil, 8, Options{Focus: []int{4}, Hidden: []int{5}}, rng())
	for _, p := range pairs {
		if (p[0] != 4 && p[1] != 4) || p[0] == 5 || p[1] == 5 {
			t.Errorf("focus on 4, 5 hidden: queued %v", p)
		}
	}
	// With only two entries, the one pair is all there is to ask.
	two, err := Queue(fit(t, 2, nil), nil, nil, 3, Options{}, rng())
	for _, p := range two {
		if key(p[0], p[1]) != [2]int{0, 1} {
			t.Errorf("two entries: %v", two)
		}
	}
	if err != nil || len(two) != 3 {
		t.Errorf("two entries: %v, %v", two, err)
	}
	for _, bad := range [][2]int{{0, 0}, {0, 12}, {-1, 3}} {
		if _, err := Queue(res, h, [][2]int{bad}, 1, Options{}, rng()); err == nil {
			t.Errorf("pending %v: want an error", bad)
		}
	}
}
