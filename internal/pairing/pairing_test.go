package pairing

import (
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

func next(t testing.TB, res *bayeselo.Result, h []bayeselo.Comparison, focus []int, rng *rand.Rand) [2]int {
	t.Helper()
	a, b, err := Next(res, h, focus, rng)
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
func play(t testing.TB, truth []float64, h []bayeselo.Comparison, focus []int, count int, rng *rand.Rand) []bayeselo.Comparison {
	t.Helper()
	var res *bayeselo.Result
	for range count {
		var err error
		if res, err = bayeselo.Fit(len(truth), h, res); err != nil {
			t.Fatal(err)
		}
		p := next(t, res, h, focus, rng)
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
		if _, _, err := Next(fit(t, n, nil), nil, nil, rng); err == nil {
			t.Errorf("%d entries: want an error", n)
		}
	}
	res := fit(t, 3, nil)
	for _, h := range [][]bayeselo.Comparison{{cmp(0, 3)}, {cmp(-1, 0)}, {cmp(1, 1)}} {
		if _, _, err := Next(res, h, nil, rng); err == nil {
			t.Errorf("history %v: want an error", h)
		}
	}
	if _, _, err := Next(res, nil, []int{3}, rng); err == nil {
		t.Error("focus on a missing entry: want an error")
	}
}

// With no answers yet every pair scores the same, so every pair should come
// up, about equally often, in both orders.
func TestFirstPairIsRandom(t *testing.T) {
	res := fit(t, 4, nil)
	rng := rand.New(rand.NewPCG(2, 2))
	seen := make(map[[2]int]int)
	for range 1200 {
		seen[next(t, res, nil, nil, rng)]++
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
		h := play(t, truth, nil, nil, 10+trial, rng)
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
					best = max(best, score(res, i, j, perPair[p], perEntry[i] == 0 || perEntry[j] == 0))
				}
			}
		}
		p := next(t, res, h, nil, rng)
		k := key(p[0], p[1])
		got := score(res, k[0], k[1], perPair[k], perEntry[k[0]] == 0 || perEntry[k[1]] == 0)
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
		focus []int
		allow [][2]int // the pairs Next may pick
	}{
		{"repeats lose to fresh pairs, and the last pair is skipped", 4,
			[]bayeselo.Comparison{cmp(0, 1), cmp(0, 1), cmp(0, 1), cmp(2, 3)}, nil,
			[][2]int{{0, 2}, {0, 3}, {1, 2}, {1, 3}}},
		{"the only unrepeated pair wins", 3,
			[]bayeselo.Comparison{cmp(0, 1), cmp(1, 2)}, nil,
			[][2]int{{0, 2}}},
		{"repeats are allowed", 3,
			[]bayeselo.Comparison{cmp(0, 1), cmp(1, 2), cmp(0, 2)}, nil,
			[][2]int{{0, 1}, {1, 2}}},
		{"the last pair again, when it is the only pair", 2,
			[]bayeselo.Comparison{cmp(0, 1)}, nil,
			[][2]int{{0, 1}}},
		// Without the bonus, the unrepeated pair (1, 2) would tie with these.
		{"uncompared entries first", 4,
			[]bayeselo.Comparison{cmp(0, 1), cmp(0, 2)}, nil,
			[][2]int{{0, 3}, {1, 3}, {2, 3}}},
		{"focus", 5, nil, []int{4},
			[][2]int{{0, 4}, {1, 4}, {2, 4}, {3, 4}}},
		{"focus on two entries", 5, []bayeselo.Comparison{cmp(1, 3)}, []int{1, 3},
			[][2]int{{0, 1}, {1, 2}, {1, 4}, {0, 3}, {2, 3}, {3, 4}}},
	}
	for _, tt := range tests {
		res := fit(t, tt.n, nil)
		seen := make(map[[2]int]bool)
		for range 200 {
			p := next(t, res, tt.h, tt.focus, rng)
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
	h := play(t, truth, nil, nil, 60, rng)

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
	h2 := play(t, truth, h, nil, 2, rng)
	added := map[int]bool{}
	for _, c := range h2[len(h):] {
		added[c.A], added[c.B] = true, true
	}
	if !added[12] || !added[13] {
		t.Errorf("the two answers after adding entries 12 and 13 were %v", h2[len(h):])
	}

	// In focus mode every pair includes a focused entry.
	for _, c := range play(t, truth, h2, []int{12, 13}, 20, rng)[len(h2):] {
		if c.A < 12 && c.B < 12 {
			t.Errorf("in focus mode, asked about %d and %d", c.A, c.B)
		}
	}
}

// Over a session, choosing pairs this way should order the entries better
// than asking about random pairs.
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
	smart, random := 0, 0
	for s := range sessions {
		rng := rand.New(rand.NewPCG(uint64(s), 6))
		truth := randomTruth(n, rng)
		smart += wrong(fit(t, n, play(t, truth, nil, nil, answers, rng)), truth)

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
	t.Logf("pairs in the wrong order over %d sessions: %d with Next, %d with random pairs", sessions, smart, random)
	if smart >= random {
		t.Errorf("Next left %d pairs in the wrong order, random pairs %d", smart, random)
	}
}
