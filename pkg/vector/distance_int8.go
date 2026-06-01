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
