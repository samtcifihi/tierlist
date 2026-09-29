package bayeselo

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestCholesky(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	const m = 7
	// a = bᵀb + I is symmetric positive definite.
	b := make([]float64, m*m)
	for k := range b {
		b[k] = rng.NormFloat64()
	}
	a := make([]float64, m*m)
	for i := range m {
		for j := range m {
			for k := range m {
				a[i*m+j] += b[k*m+i] * b[k*m+j]
			}
		}
		a[i*m+i]++
	}
	l := slices.Clone(a)
	if !cholesky(l, m) {
		t.Fatal("cholesky rejected a positive definite matrix")
	}

	rhs := make([]float64, m)
	for i := range rhs {
		rhs[i] = rng.NormFloat64()
	}
	x := slices.Clone(rhs)
	cholSolve(l, m, x)
	for i := range m {
		s := 0.0
		for k := range m {
			s += a[i*m+k] * x[k]
		}
		if !near(s, rhs[i], 1e-10) {
			t.Errorf("row %d of a·x is %g, want %g", i, s, rhs[i])
		}
	}

	inv := cholInverse(l, m)
	for i := range m {
		for j := range m {
			s := 0.0
			for k := range m {
				s += a[i*m+k] * inv[k*m+j]
			}
			want := 0.0
			if i == j {
				want = 1
			}
			if !near(s, want, 1e-10) {
				t.Errorf("(a·a⁻¹)[%d][%d] = %g, want %g", i, j, s, want)
			}
		}
	}

	if cholesky([]float64{1, 2, 2, 1}, 2) {
		t.Error("cholesky accepted a matrix that is not positive definite")
	}
}
