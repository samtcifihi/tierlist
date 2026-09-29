package bayeselo

import "math"

// cholesky replaces the lower triangle of the symmetric m×m matrix a
// (row-major) with its Cholesky factor L, where a = L Lᵀ. It reports false
// if a is not positive definite.
func cholesky(a []float64, m int) bool {
	for j := range m {
		rj := a[j*m : j*m+m]
		d := rj[j]
		for k := range j {
			d -= rj[k] * rj[k]
		}
		if !(d > 0) {
			return false
		}
		d = math.Sqrt(d)
		rj[j] = d
		for i := j + 1; i < m; i++ {
			ri := a[i*m : i*m+m]
			s := ri[j]
			for k := range j {
				s -= ri[k] * rj[k]
			}
			ri[j] = s / d
		}
	}
	return true
}

// cholSolve solves L Lᵀ x = b in place, given L from cholesky.
func cholSolve(l []float64, m int, b []float64) {
	for i := range m { // L y = b
		s := b[i]
		for k := range i {
			s -= l[i*m+k] * b[k]
		}
		b[i] = s / l[i*m+i]
	}
	for i := m - 1; i >= 0; i-- { // Lᵀ x = y
		s := b[i]
		for k := i + 1; k < m; k++ {
			s -= l[k*m+i] * b[k]
		}
		b[i] = s / l[i*m+i]
	}
}

// cholInverse returns the inverse of L Lᵀ, m×m row-major, given L from
// cholesky.
func cholInverse(l []float64, m int) []float64 {
	// w = L⁻¹, lower triangular, built a column at a time.
	w := make([]float64, m*m)
	col := make([]float64, m)
	for j := range m {
		col[j] = 1 / l[j*m+j]
		for i := j + 1; i < m; i++ {
			s := 0.0
			for k := j; k < i; k++ {
				s += l[i*m+k] * col[k]
			}
			col[i] = -s / l[i*m+i]
		}
		for i := j; i < m; i++ {
			w[i*m+j] = col[i]
		}
	}
	// (L Lᵀ)⁻¹ = wᵀ w. Fill the lower triangle, then mirror it.
	inv := make([]float64, m*m)
	for k := range m {
		wk := w[k*m : k*m+k+1]
		for i, wki := range wk {
			row := inv[i*m : i*m+i+1]
			for j := range row {
				row[j] += wki * wk[j]
			}
		}
	}
	for i := range m {
		for j := range i {
			inv[j*m+i] = inv[i*m+j]
		}
	}
	return inv
}
