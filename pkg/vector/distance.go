package vector

import (
	"fmt"
	"math"
)

// ErrDimensionMismatch is returned when vector dimensions don't match
var ErrDimensionMismatch = fmt.Errorf("vector dimensions mismatch")

// DistanceMetric represents the type of distance calculation
type DistanceMetric string

const (
	MetricCosine     DistanceMetric = "cosine"
	MetricEuclidean  DistanceMetric = "euclidean"
	MetricDotProduct DistanceMetric = "dot_product"
)

// CosineSimilarity calculates the cosine similarity between two vectors
// Returns a value between -1 (opposite) and 1 (identical)
// Formula: (a · b) / (||a|| * ||b||)
// Returns error if vector dimensions don't match
func CosineSimilarity(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("%w: %d != %d", ErrDimensionMismatch, len(a), len(b))
	}

	dotProd := float32(0.0)
	normA := float32(0.0)
	normB := float32(0.0)

	for i := 0; i < len(a); i++ {
		dotProd += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	// Handle zero vectors
	if normA == 0 || normB == 0 {
		return 0, nil
	}

	return dotProd / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB)))), nil
}

// CosineDistance calculates the cosine distance between two vectors
// Returns a value between 0 (identical) and 2 (opposite)
// Formula: 1 - cosine_similarity(a, b)
// Returns error if vector dimensions don't match
func CosineDistance(a, b []float32) (float32, error) {
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		return 0, err
	}
	return 1.0 - sim, nil
}

// EuclideanDistance calculates the Euclidean (L2) distance between two vectors
// Formula: sqrt(sum((a[i] - b[i])^2))
// Returns error if vector dimensions don't match
func EuclideanDistance(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("%w: %d != %d", ErrDimensionMismatch, len(a), len(b))
	}

	sum := float32(0.0)
	for i := 0; i < len(a); i++ {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return float32(math.Sqrt(float64(sum))), nil
}

// DotProduct calculates the dot product of two vectors
// Formula: sum(a[i] * b[i])
// Returns error if vector dimensions don't match
func DotProduct(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("%w: %d != %d", ErrDimensionMismatch, len(a), len(b))
	}

	result := float32(0.0)
	for i := 0; i < len(a); i++ {
		result += a[i] * b[i]
	}

	return result, nil
}

// Normalize normalizes a vector to unit length
// Formula: v / ||v||
func Normalize(v []float32) []float32 {
	norm := float32(0.0)
	for _, val := range v {
		norm += val * val
	}

	norm = float32(math.Sqrt(float64(norm)))

	// Handle zero vector
	if norm == 0 {
		return v
	}

	result := make([]float32, len(v))
	for i, val := range v {
		result[i] = val / norm
	}

	return result
}

// Distance calculates the distance between two vectors using the specified metric
// Returns error if vector dimensions don't match
func Distance(a, b []float32, metric DistanceMetric) (float32, error) {
	switch metric {
	case MetricCosine:
		return CosineDistance(a, b)
	case MetricEuclidean:
		return EuclideanDistance(a, b)
	case MetricDotProduct:
		// For dot product, we negate to make "closer" values smaller
		// (since we typically want to minimize distance)
		dot, err := DotProduct(a, b)
		if err != nil {
			return 0, err
		}
		return -dot, nil
	default:
		// Default to cosine distance
		return CosineDistance(a, b)
	}
}

// Magnitude calculates the magnitude (L2 norm) of a vector
func Magnitude(v []float32) float32 {
	sum := float32(0.0)
	for _, val := range v {
		sum += val * val
	}
	return float32(math.Sqrt(float64(sum)))
}
