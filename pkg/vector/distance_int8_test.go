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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, scale, norm := quantizeInt8(tt.v)

			if len(q) != len(tt.v) {
				t.Fatalf("len(q)=%d, want %d", len(q), len(tt.v))
			}
			if math.Abs(float64(scale-tt.wantScale)) > 1e-9 {
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
				if qi < -127 || qi > 127 {
					t.Errorf("q[%d]=%d out of [-127,127]", i, qi)
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
