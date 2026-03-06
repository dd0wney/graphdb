package query

import "fmt"

// VectorSimilarityFunc computes similarity between two vectors.
// Returns a float64 score (e.g., cosine similarity in [-1, 1]).
type VectorSimilarityFunc func(a, b []float32) (float64, error)

// VectorSearchFunc performs HNSW k-NN search on a named vector index.
type VectorSearchFunc func(propertyName string, query []float32, k, ef int) ([]VectorSearchResult, error)

// HasVectorIndexFunc checks whether a vector index exists for a property.
type HasVectorIndexFunc func(propertyName string) bool

// GetNodeFunc fetches a node by ID (avoids importing storage in step types).
type GetNodeFunc func(nodeID uint64) (any, error)

// VectorSearchResult mirrors vector.SearchResult without importing pkg/vector.
type VectorSearchResult struct {
	NodeID   uint64
	Distance float32
}

// SetVectorSearch wires up vector similarity and HNSW search for use in queries.
// Follows the same closure pattern as SetSearchIndex for full-text search.
func (e *Executor) SetVectorSearch(
	similarityFn VectorSimilarityFunc,
	searchFn VectorSearchFunc,
	hasIndexFn HasVectorIndexFunc,
	getNodeFn GetNodeFunc,
) {
	e.vectorSimilarity = similarityFn
	e.vectorSearch = searchFn
	e.hasVectorIndex = hasIndexFn
	e.getNode = getNodeFn

	// Propagate to optimizer for VectorSearchStep creation
	e.optimizer.vectorSearch = searchFn
	e.optimizer.similarityFn = similarityFn
	e.optimizer.hasVectorIndex = hasIndexFn
	e.optimizer.getNodeFn = getNodeFn

	RegisterFunction("vector.similarity", func(args []any) (any, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("vector.similarity requires 2 arguments (vector_a, vector_b)")
		}

		a, ok := toFloat32Slice(args[0])
		if !ok {
			return nil, fmt.Errorf("vector.similarity: first argument must be a vector, got %T", args[0])
		}

		b, ok := toFloat32Slice(args[1])
		if !ok {
			return nil, fmt.Errorf("vector.similarity: second argument must be a vector, got %T", args[1])
		}

		return similarityFn(a, b)
	})
}

// toFloat32Slice converts various vector representations to []float32.
// Handles []float32, []float64, and []any containing numeric values.
func toFloat32Slice(v any) ([]float32, bool) {
	switch vec := v.(type) {
	case []float32:
		return vec, true
	case []float64:
		result := make([]float32, len(vec))
		for i, f := range vec {
			result[i] = float32(f)
		}
		return result, true
	case []any:
		result := make([]float32, len(vec))
		for i, elem := range vec {
			switch val := elem.(type) {
			case float64:
				result[i] = float32(val)
			case float32:
				result[i] = val
			case int:
				result[i] = float32(val)
			case int64:
				result[i] = float32(val)
			default:
				return nil, false
			}
		}
		return result, true
	default:
		return nil, false
	}
}
