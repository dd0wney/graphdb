//go:build amd64 && goexperiment.simd

package vector

import "simd/archsimd"

// simdSmokeSupported reports that the real archsimd kernel is compiled in.
const simdSmokeSupported = true

// simdSmokeAdd8 adds two 8-element float32 slices using a single AVX2 vector
// add. Throwaway: deleted when Phase 1b lands the real distance kernel.
func simdSmokeAdd8(a, b []float32) [8]float32 {
	va := archsimd.LoadFloat32x8Slice(a)
	vb := archsimd.LoadFloat32x8Slice(b)
	var out [8]float32
	va.Add(vb).StoreSlice(out[:])
	return out
}
