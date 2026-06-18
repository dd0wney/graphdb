package search

import (
	"math"
	"math/rand"
)

// --- Linear algebra helpers ---

func lsaRandMatrix(rng *rand.Rand, rows, cols int) [][]float32 {
	m := make([][]float32, rows)
	for i := range m {
		m[i] = make([]float32, cols)
		for j := range m[i] {
			m[i][j] = float32(rng.NormFloat64())
		}
	}
	return m
}

// The matrix-helper functions below use single-letter capitalized
// parameter names (X, Y, Q, B, G, A) to mirror the linear-algebra
// notation in the surrounding comments and any LSA / Jacobi / QR
// reference. gocritic's captLocal rule wants lowercase, but renaming
// here would obscure correspondence with the math. //nolint:gocritic
// directives on each function suppress the rule with this rationale.

// lsaSparseMulDense: Y = X × A   (D×l = sparse(D×T) × dense(T×l))
//
//nolint:gocritic // captLocal: matrix notation, see comment above
func lsaSparseMulDense(X []sparseRow, A [][]float32, D, l int) [][]float32 {
	Y := make([][]float32, D)
	for d := range Y {
		Y[d] = make([]float32, l)
		yd := Y[d]
		for k, tidx := range X[d].idx {
			v := X[d].val[k]
			ar := A[tidx]
			for j := 0; j < l; j++ {
				yd[j] += v * ar[j]
			}
		}
	}
	return Y
}

// lsaSparseTMulDense: Z = X^T × Y   (T×l = sparse(D×T)^T × dense(D×l))
//
//nolint:gocritic // captLocal: matrix notation, see comment above lsaSparseMulDense
func lsaSparseTMulDense(X []sparseRow, Y [][]float32, T, l int) [][]float32 {
	Z := make([][]float32, T)
	for t := range Z {
		Z[t] = make([]float32, l)
	}
	for d, row := range X {
		yd := Y[d]
		for k, tidx := range row.idx {
			v := row.val[k]
			zt := Z[tidx]
			for j := 0; j < l; j++ {
				zt[j] += v * yd[j]
			}
		}
	}
	return Z
}

// lsaQR orthonormalizes columns of Y (D×l) via modified Gram-Schmidt.
//
//nolint:gocritic // captLocal: matrix notation, see comment above lsaSparseMulDense
func lsaQR(Y [][]float32, D, l int) [][]float32 {
	cols := make([][]float32, l)
	for j := range cols {
		cols[j] = make([]float32, D)
		for d := 0; d < D; d++ {
			cols[j][d] = Y[d][j]
		}
	}
	for j := 0; j < l; j++ {
		for prev := 0; prev < j; prev++ {
			dot := float32(0)
			for d := 0; d < D; d++ {
				dot += cols[j][d] * cols[prev][d]
			}
			for d := 0; d < D; d++ {
				cols[j][d] -= dot * cols[prev][d]
			}
		}
		norm := float32(0)
		for d := 0; d < D; d++ {
			norm += cols[j][d] * cols[j][d]
		}
		if norm > 1e-12 {
			inv := float32(1 / math.Sqrt(float64(norm)))
			for d := 0; d < D; d++ {
				cols[j][d] *= inv
			}
		}
	}
	Q := make([][]float32, D)
	for d := range Q {
		Q[d] = make([]float32, l)
		for j := 0; j < l; j++ {
			Q[d][j] = cols[j][d]
		}
	}
	return Q
}

// lsaLeftMul: B = Q^T × X   (l×T = dense(D×l)^T × sparse(D×T))
//
//nolint:gocritic // captLocal: matrix notation, see comment above lsaSparseMulDense
func lsaLeftMul(Q [][]float32, X []sparseRow, D, l, T int) [][]float32 {
	B := make([][]float32, l)
	for i := range B {
		B[i] = make([]float32, T)
	}
	for d, row := range X {
		qd := Q[d]
		for k, tidx := range row.idx {
			v := row.val[k]
			for i := 0; i < l; i++ {
				B[i][tidx] += qd[i] * v
			}
		}
	}
	return B
}

// lsaGram: G = B × B^T   (l×l symmetric; eigenvalues = squared singular values of B)
//
//nolint:gocritic // captLocal: matrix notation, see comment above lsaSparseMulDense
func lsaGram(B [][]float32, l, T int) [][]float32 {
	G := make([][]float32, l)
	for i := range G {
		G[i] = make([]float32, l)
	}
	for i := 0; i < l; i++ {
		bi := B[i]
		for j := i; j < l; j++ {
			bj := B[j]
			dot := float32(0)
			for t := 0; t < T; t++ {
				dot += bi[t] * bj[t]
			}
			G[i][j] = dot
			G[j][i] = dot
		}
	}
	return G
}

// lsaJacobi computes eigendecomposition of symmetric G (n×n) via cyclic Jacobi sweeps.
// Returns (eigenvalues, V) where columns of V are eigenvectors.
//
//nolint:gocritic // captLocal: matrix notation, see comment above lsaSparseMulDense
func lsaJacobi(G [][]float32, n int) ([]float32, [][]float32) {
	A := make([][]float32, n)
	for i := range A {
		A[i] = make([]float32, n)
		copy(A[i], G[i])
	}
	V := make([][]float32, n)
	for i := range V {
		V[i] = make([]float32, n)
		V[i][i] = 1
	}

	const maxSweeps = 50
	const tol = float32(1e-6)

	for sweep := 0; sweep < maxSweeps; sweep++ {
		maxOff := float32(0)
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				v := A[i][j]
				if v < 0 {
					v = -v
				}
				if v > maxOff {
					maxOff = v
				}
			}
		}
		if maxOff < tol {
			break
		}
		for p := 0; p < n-1; p++ {
			for q := p + 1; q < n; q++ {
				apq := A[p][q]
				if apq < 0 {
					apq = -apq
				}
				if apq < 1e-9 {
					continue
				}
				tau := (A[q][q] - A[p][p]) / (2 * A[p][q])
				var t float32
				if tau >= 0 {
					t = 1 / (tau + float32(math.Sqrt(float64(1+tau*tau))))
				} else {
					t = 1 / (tau - float32(math.Sqrt(float64(1+tau*tau))))
				}
				c := float32(1 / math.Sqrt(float64(1+t*t)))
				s := t * c
				App, Aqq, Apq := A[p][p], A[q][q], A[p][q]
				A[p][p] = App - t*Apq
				A[q][q] = Aqq + t*Apq
				A[p][q] = 0
				A[q][p] = 0
				for r := 0; r < n; r++ {
					if r == p || r == q {
						continue
					}
					Apr := A[p][r]
					Aqr := A[q][r]
					A[p][r] = c*Apr - s*Aqr
					A[r][p] = A[p][r]
					A[q][r] = s*Apr + c*Aqr
					A[r][q] = A[q][r]
				}
				for r := 0; r < n; r++ {
					Vrp := V[r][p]
					Vrq := V[r][q]
					V[r][p] = c*Vrp - s*Vrq
					V[r][q] = s*Vrp + c*Vrq
				}
			}
		}
	}

	eigenVals := make([]float32, n)
	for i := range eigenVals {
		eigenVals[i] = A[i][i]
	}
	return eigenVals, V
}
