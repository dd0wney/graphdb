package vector

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

// TestCosineSimilarity tests cosine similarity calculation
func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
			epsilon:  0.0001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0},
			b:        []float32{0, 1},
			expected: 0.0,
			epsilon:  0.0001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
			epsilon:  0.0001,
		},
		{
			name:     "similar vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{2, 4, 6},
			expected: 1.0,
			epsilon:  0.0001,
		},
		{
			name:     "different magnitude vectors",
			a:        []float32{3, 4},
			b:        []float32{4, 3},
			expected: 0.96,
			epsilon:  0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CosineSimilarity(tt.a, tt.b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(float64(result-tt.expected)) > float64(tt.epsilon) {
				t.Errorf("CosineSimilarity(%v, %v) = %v, want %v (±%v)",
					tt.a, tt.b, result, tt.expected, tt.epsilon)
			}
		})
	}
}

// TestCosineDistance tests cosine distance calculation (1 - cosine similarity)
func TestCosineDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
			epsilon:  0.0001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0},
			b:        []float32{0, 1},
			expected: 1.0,
			epsilon:  0.0001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: 2.0,
			epsilon:  0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CosineDistance(tt.a, tt.b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(float64(result-tt.expected)) > float64(tt.epsilon) {
				t.Errorf("CosineDistance(%v, %v) = %v, want %v (±%v)",
					tt.a, tt.b, result, tt.expected, tt.epsilon)
			}
		})
	}
}

// TestEuclideanDistance tests Euclidean distance calculation
func TestEuclideanDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: 0.0,
			epsilon:  0.0001,
		},
		{
			name:     "unit distance in 1D",
			a:        []float32{0},
			b:        []float32{1},
			expected: 1.0,
			epsilon:  0.0001,
		},
		{
			name:     "3-4-5 triangle",
			a:        []float32{0, 0},
			b:        []float32{3, 4},
			expected: 5.0,
			epsilon:  0.0001,
		},
		{
			name:     "3D distance",
			a:        []float32{1, 2, 3},
			b:        []float32{4, 6, 8},
			expected: 7.0711,
			epsilon:  0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EuclideanDistance(tt.a, tt.b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(float64(result-tt.expected)) > float64(tt.epsilon) {
				t.Errorf("EuclideanDistance(%v, %v) = %v, want %v (±%v)",
					tt.a, tt.b, result, tt.expected, tt.epsilon)
			}
		})
	}
}

// TestDotProduct tests dot product calculation
func TestDotProduct(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0},
			b:        []float32{0, 1},
			expected: 0.0,
			epsilon:  0.0001,
		},
		{
			name:     "parallel vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: 14.0,
			epsilon:  0.0001,
		},
		{
			name:     "simple dot product",
			a:        []float32{1, 2, 3},
			b:        []float32{4, 5, 6},
			expected: 32.0,
			epsilon:  0.0001,
		},
		{
			name:     "negative values",
			a:        []float32{-1, 2, -3},
			b:        []float32{4, -5, 6},
			expected: -32.0,
			epsilon:  0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DotProduct(tt.a, tt.b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(float64(result-tt.expected)) > float64(tt.epsilon) {
				t.Errorf("DotProduct(%v, %v) = %v, want %v (±%v)",
					tt.a, tt.b, result, tt.expected, tt.epsilon)
			}
		})
	}
}

// TestNormalize tests vector normalization
func TestNormalize(t *testing.T) {
	tests := []struct {
		name     string
		input    []float32
		expected []float32
		epsilon  float32
	}{
		{
			name:     "unit vector",
			input:    []float32{1, 0, 0},
			expected: []float32{1, 0, 0},
			epsilon:  0.0001,
		},
		{
			name:     "scale vector",
			input:    []float32{3, 4},
			expected: []float32{0.6, 0.8},
			epsilon:  0.0001,
		},
		{
			name:     "3D vector",
			input:    []float32{1, 2, 2},
			expected: []float32{1.0 / 3.0, 2.0 / 3.0, 2.0 / 3.0},
			epsilon:  0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Normalize(tt.input)
			for i := range result {
				if math.Abs(float64(result[i]-tt.expected[i])) > float64(tt.epsilon) {
					t.Errorf("Normalize(%v)[%d] = %v, want %v (±%v)",
						tt.input, i, result[i], tt.expected[i], tt.epsilon)
				}
			}
		})
	}
}

// TestMismatchedDimensions tests error handling for mismatched vector dimensions
func TestMismatchedDimensions(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}

	// Test that mismatched dimensions return an error
	_, err := CosineSimilarity(a, b)
	if err == nil {
		t.Error("Expected error for mismatched dimensions, but got nil")
	}
	if !errors.Is(err, ErrDimensionMismatch) {
		t.Errorf("Expected ErrDimensionMismatch, got: %v", err)
	}

	_, err = EuclideanDistance(a, b)
	if err == nil {
		t.Error("Expected error for mismatched dimensions, but got nil")
	}

	_, err = DotProduct(a, b)
	if err == nil {
		t.Error("Expected error for mismatched dimensions, but got nil")
	}
}

// BenchmarkCosineSimilarity benchmarks cosine similarity for different dimensions
func BenchmarkCosineSimilarity(b *testing.B) {
	dimensions := []int{128, 384, 768, 1536}

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			v1 := make([]float32, dim)
			v2 := make([]float32, dim)
			for i := 0; i < dim; i++ {
				v1[i] = float32(i)
				v2[i] = float32(i + 1)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = CosineSimilarity(v1, v2)
			}
		})
	}
}

// BenchmarkEuclideanDistance benchmarks Euclidean distance
func BenchmarkEuclideanDistance(b *testing.B) {
	v1 := make([]float32, 768)
	v2 := make([]float32, 768)
	for i := 0; i < 768; i++ {
		v1[i] = float32(i)
		v2[i] = float32(i + 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EuclideanDistance(v1, v2)
	}
}
