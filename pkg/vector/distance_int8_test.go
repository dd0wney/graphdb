package vector

import (
	"math"
	"testing"
)

func TestQuantizeInt8(t *testing.T) {
	tests := []struct {
		name      string
		v         []float32
		wantScale float32 // maxAbs/127; 0 for zero vector
	}{
		{"simple", []float32{1, -1, 0.5, -0.5}, 1.0 / 127.0},
		{"zero vector", []float32{0, 0, 0}, 0},
		{"single max", []float32{2.0}, 2.0 / 127.0},
		{"empty", []float32{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, scale, norm := quantizeInt8(tt.v)

			if len(q) != len(tt.v) {
				t.Fatalf("len(q)=%d, want %d", len(q), len(tt.v))
			}
			if math.Abs(float64(scale-tt.wantScale)) > 1e-6 {
				t.Errorf("scale=%v, want %v", scale, tt.wantScale)
			}

			var wantSumSq float64
			for _, x := range tt.v {
				wantSumSq += float64(x) * float64(x)
			}
			wantNorm := float32(math.Sqrt(wantSumSq))
			if math.Abs(float64(norm-wantNorm)) > 1e-6 {
				t.Errorf("norm=%v, want %v", norm, wantNorm)
			}

			for i, qi := range q {
				// int8 max is 127, so only -128 can violate the symmetric range.
				if qi < -127 {
					t.Errorf("q[%d]=%d below -127 (asymmetric -128 not allowed)", i, qi)
				}
			}

			// For non-zero input the largest-magnitude component maps to exactly 127.
			if scale > 0 {
				var maxQ int8
				for _, qi := range q {
					a := qi
					if a < 0 {
						a = -a
					}
					if a > maxQ {
						maxQ = a
					}
				}
				if maxQ != 127 {
					t.Errorf("max |q[i]| = %d, want 127", maxQ)
				}
			}

			if scale > 0 {
				for i, x := range tt.v {
					deq := float32(q[i]) * scale
					if math.Abs(float64(deq-x)) > float64(scale) {
						t.Errorf("deq[%d]=%v too far from %v (scale=%v)", i, deq, x, scale)
					}
				}
			}
		})
	}
}

func TestDotInt8Scalar(t *testing.T) {
	tests := []struct {
		name string
		a, b []int8
		want int32
	}{
		{"basic", []int8{1, 2, 3}, []int8{4, 5, 6}, 4 + 10 + 18},
		{"negatives", []int8{-1, 2, -3}, []int8{4, -5, 6}, -4 - 10 - 18},
		{"empty", []int8{}, []int8{}, 0},
		{"max magnitude", []int8{127, -127}, []int8{127, -127}, 127*127 + 127*127},
		{"max magnitude negative", []int8{127, -127}, []int8{-127, 127}, -127*127 - 127*127},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dotInt8Scalar(tt.a, tt.b); got != tt.want {
				t.Errorf("dotInt8Scalar=%d, want %d", got, tt.want)
			}
		})
	}
}

// dotInt8 is the build-tag dispatch; on a non-SIMD build it equals the scalar
// kernel, on a SIMD build the differential test (Task 6) checks equivalence.
func TestDotInt8DispatchMatchesScalar(t *testing.T) {
	a := []int8{1, -2, 3, -4, 5, -6, 7, -8, 9, -10, 11, -12, 13, -14, 15, -16, 17}
	b := []int8{-1, 2, -3, 4, -5, 6, -7, 8, -9, 10, -11, 12, -13, 14, -15, 16, -17}
	if got, want := dotInt8(a, b), dotInt8Scalar(a, b); got != want {
		t.Errorf("dotInt8=%d, dotInt8Scalar=%d", got, want)
	}
}

func TestMetricDistanceInt8(t *testing.T) {
	// Two simple vectors; quantize, then compare int8-derived distance to the
	// float32 reference within a tolerance that accounts for quantization.
	a := []float32{1, 2, 3, 4}
	b := []float32{4, 3, 2, 1}
	qa, sa, na := quantizeInt8(a)
	qb, sb, nb := quantizeInt8(b)
	dot := dotInt8(qa, qb)

	const tol = 0.02

	t.Run("cosine", func(t *testing.T) {
		want, _ := CosineDistance(a, b)
		got := metricDistanceInt8(MetricCosine, dot, sa, sb, na, nb)
		if math.Abs(float64(got-want)) > tol {
			t.Errorf("cosine int8=%v, float32=%v", got, want)
		}
	})
	t.Run("euclidean", func(t *testing.T) {
		want, _ := EuclideanDistance(a, b)
		got := metricDistanceInt8(MetricEuclidean, dot, sa, sb, na, nb)
		if math.Abs(float64(got-want)) > tol*float64(na+nb) {
			t.Errorf("euclidean int8=%v, float32=%v", got, want)
		}
	})
	t.Run("dot_product", func(t *testing.T) {
		raw, _ := DotProduct(a, b)
		want := -raw
		got := metricDistanceInt8(MetricDotProduct, dot, sa, sb, na, nb)
		if math.Abs(float64(got-want)) > tol*float64(na*nb) {
			t.Errorf("dot int8=%v, float32=%v", got, want)
		}
	})
}

func TestMetricDistanceInt8EuclideanClampSelf(t *testing.T) {
	// Distance from a vector to itself must be ~0 and never NaN (the clamp
	// guards a tiny-negative radicand from exact norms vs approximate dot).
	v := []float32{0.3, -0.7, 0.1, 0.9, -0.2}
	q, s, n := quantizeInt8(v)
	dot := dotInt8(q, q)
	got := metricDistanceInt8(MetricEuclidean, dot, s, s, n, n)
	if math.IsNaN(float64(got)) {
		t.Fatal("euclidean self-distance is NaN — clamp missing")
	}
	if got < 0 || got > 0.05 {
		t.Errorf("euclidean self-distance=%v, want ~0", got)
	}
}

func TestMetricDistanceInt8ZeroVector(t *testing.T) {
	// Zero vector: cosine distance 1.0, dot_product 0, matching float32 path.
	if got := metricDistanceInt8(MetricCosine, 0, 0, 0, 0, 0); got != 1.0 {
		t.Errorf("cosine zero-vector=%v, want 1.0", got)
	}
	if got := metricDistanceInt8(MetricDotProduct, 0, 0, 0, 0, 0); got != 0 {
		t.Errorf("dot zero-vector=%v, want 0", got)
	}
}
