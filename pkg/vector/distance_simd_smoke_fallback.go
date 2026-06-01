//go:build !amd64 || !goexperiment.simd

package vector

// simdSmokeSupported is false when archsimd is unavailable (non-amd64, or
// amd64 without GOEXPERIMENT=simd). The smoke test skips in that case.
const simdSmokeSupported = false

// simdSmokeAdd8 is never called when simdSmokeSupported is false; it exists
// only so the package compiles on every target.
func simdSmokeAdd8(a, b []float32) [8]float32 {
	var out [8]float32
	for i := range out {
		out[i] = a[i] + b[i]
	}
	return out
}
