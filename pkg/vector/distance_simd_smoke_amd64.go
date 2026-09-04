//go:build amd64 && goexperiment.simd

package vector

import "simd/archsimd"

// simdSmokeSupported reports that the real archsimd kernel is compiled in.
const simdSmokeSupported = true

// simdSmokeAdd8 adds two 8-element float32 slices using a single AVX2 vector
// add. Throwaway: deleted when Phase 1b lands the real distance kernel.
//
// The slice-taking loader and store are named LoadFloat32x8 and Store as of
// Go 1.27; Go 1.26 called them LoadFloat32x8Slice and StoreSlice, and used
// the bare names for the *[8]float32 forms. archsimd is experimental, so
// expect this file to need the same kind of rename on the next Go minor.
func simdSmokeAdd8(a, b []float32) [8]float32 {
	va := archsimd.LoadFloat32x8(a)
	vb := archsimd.LoadFloat32x8(b)
	var out [8]float32
	va.Add(vb).Store(out[:])
	return out
}
