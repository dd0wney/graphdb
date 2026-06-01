package vector

import "math"

// quantizedVec is the int8 projection of a float32 vector: the quantized
// components, the dequantization scale, and the L2 norm of the ORIGINAL
// float32 vector (not re-derived from the int8 values, which would accumulate
// quantization error).
type quantizedVec struct {
	q     []int8
	scale float32
	norm  float32
}

// quantizeInt8 quantizes v with a symmetric per-vector scale (maxAbs/127),
// mapping the largest-magnitude component to ±127. It returns the int8 vector,
// the scale, and the L2 norm of the original v. A zero vector returns
// (zeros, 0, 0).
func quantizeInt8(v []float32) (q []int8, scale, norm float32) {
	var maxAbs, sumSq float64
	for _, x := range v {
		a := math.Abs(float64(x))
		if a > maxAbs {
			maxAbs = a
		}
		sumSq += float64(x) * float64(x)
	}

	q = make([]int8, len(v))
	norm = float32(math.Sqrt(sumSq))
	if maxAbs == 0 {
		return q, 0, 0
	}

	scale = float32(maxAbs / 127.0)
	invScale := 127.0 / maxAbs
	for i, x := range v {
		r := math.Round(float64(x) * invScale)
		// Symmetric clamp to ±127 (not -128): keeps the int8 range symmetric so
		// -128 is unused. Rounding can land on ±127 but never exceed it; the
		// clamp is defensive against float edge cases.
		if r > 127 {
			r = 127
		} else if r < -127 {
			r = -127
		}
		q[i] = int8(r)
	}
	return q, scale, norm
}

// dotInt8Scalar computes the integer dot product of two equal-length int8
// vectors, accumulating in int32. It is the portable reference kernel and the
// fallback for non-SIMD builds. int32 is safe: at dim 1536 the max magnitude
// is 1536·127·127 ≈ 24.8M, far below int32's 2.1B ceiling.
func dotInt8Scalar(a, b []int8) int32 {
	var dot int32
	for i := range a {
		dot += int32(a[i]) * int32(b[i])
	}
	return dot
}

// metricDistanceInt8 converts an int8 dot product and the stored scales/norms
// into the configured distance metric, mirroring the float32 Distance
// semantics. dotF is the approximate float32 dot product of the originals.
func metricDistanceInt8(metric DistanceMetric, dot int32, aScale, bScale, aNorm, bNorm float32) float32 {
	dotF := float32(dot) * aScale * bScale

	switch metric {
	case MetricEuclidean:
		// ||a-b||^2 = ||a||^2 + ||b||^2 - 2(a·b). The max(0,…) clamp guards a
		// slightly-negative radicand when a≈b (exact norms vs approximate dot),
		// which would otherwise NaN the sqrt and corrupt the search.
		sq := aNorm*aNorm + bNorm*bNorm - 2*dotF
		if sq < 0 {
			sq = 0
		}
		return float32(math.Sqrt(float64(sq)))
	case MetricDotProduct:
		// Negated so "closer" is smaller, matching the float32 Distance path.
		return -dotF
	case MetricCosine:
		fallthrough
	default:
		// Matches CosineSimilarity's zero-vector handling: similarity 0 → distance 1.
		if aNorm == 0 || bNorm == 0 {
			return 1.0
		}
		return 1.0 - dotF/(aNorm*bNorm)
	}
}
