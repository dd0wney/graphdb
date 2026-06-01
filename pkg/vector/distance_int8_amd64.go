//go:build amd64 && goexperiment.simd

package vector

import "simd/archsimd"

// dotInt8 computes Σ a[i]*b[i] using AVX2: each 16-int8 chunk is sign-extended
// to int16 (VPMOVSXBW) and fed to DotProductPairs (VPMADDWD), which multiplies
// and pairwise-adds into int32 lanes accumulated with VPADDD. All three are
// AVX2 (256-bit) ops — no AVX-512 needed (baseline is Haswell+). int16 widening
// (not the saturating uint8×int8 VPMADDUBSW) avoids overflow: max product
// 16129, pair-sum 32258, well within int32. True VNNI (VPDPBUSD) is not exposed
// by go1.26 archsimd.
func dotInt8(a, b []int8) int32 {
	acc := archsimd.BroadcastInt32x8(0)
	n := len(a)
	i := 0
	for ; i+16 <= n; i += 16 {
		va := archsimd.LoadInt8x16Slice(a[i:]).ExtendToInt16()
		vb := archsimd.LoadInt8x16Slice(b[i:]).ExtendToInt16()
		acc = acc.Add(va.DotProductPairs(vb))
	}

	var parts [8]int32
	acc.StoreSlice(parts[:])
	var dot int32
	for _, p := range parts {
		dot += p
	}

	// Scalar remainder for the tail (len not a multiple of 16).
	for ; i < n; i++ {
		dot += int32(a[i]) * int32(b[i])
	}
	return dot
}
