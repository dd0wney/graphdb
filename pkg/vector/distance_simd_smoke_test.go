package vector

import "testing"

// TestSIMDSmokeAddMatchesScalar de-risks the simd/archsimd path: an 8-wide
// float32 add via the experimental intrinsics must equal the scalar result.
// On non-amd64 or without GOEXPERIMENT=simd, simdSmokeSupported is false and
// the test is skipped (the kernel only exists on amd64 under the experiment).
func TestSIMDSmokeAddMatchesScalar(t *testing.T) {
	if !simdSmokeSupported {
		t.Skip("simd/archsimd unavailable: needs amd64 + GOEXPERIMENT=simd")
	}
	a := []float32{1, 2, 3, 4, 5, 6, 7, 8}
	b := []float32{8, 7, 6, 5, 4, 3, 2, 1}

	got := simdSmokeAdd8(a, b)

	for i := range a {
		want := a[i] + b[i]
		if got[i] != want {
			t.Fatalf("lane %d: simd=%v scalar=%v", i, got[i], want)
		}
	}
}
