package gnn

import (
	"fmt"
	"math"
)

// AggregationType represents the type of neighbor feature aggregation
type AggregationType string

const (
	AggMean AggregationType = "mean"
	AggSum  AggregationType = "sum"
	AggMax  AggregationType = "max"
)

// AggregateVectors performs element-wise aggregation across multiple vectors.
func AggregateVectors(vectors [][]float32, agg AggregationType) ([]float32, error) {
	if len(vectors) == 0 {
		return nil, nil
	}

	dim := len(vectors[0])
	for i := 1; i < len(vectors); i++ {
		if len(vectors[i]) != dim {
			return nil, fmt.Errorf("vector dimension mismatch at index %d: expected %d, got %d", i, dim, len(vectors[i]))
		}
	}

	result := make([]float32, dim)

	switch agg {
	case AggSum, AggMean:
		for _, v := range vectors {
			for i := 0; i < dim; i++ {
				result[i] += v[i]
			}
		}
		if agg == AggMean {
			count := float32(len(vectors))
			for i := 0; i < dim; i++ {
				result[i] /= count
			}
		}

	case AggMax:
		for i := 0; i < dim; i++ {
			result[i] = float32(math.Inf(-1))
		}
		for _, v := range vectors {
			for i := 0; i < dim; i++ {
				if v[i] > result[i] {
					result[i] = v[i]
				}
			}
		}

	default:
		return nil, fmt.Errorf("unsupported aggregation type: %s", agg)
	}

	return result, nil
}
