// Package queryutil provides helpers for wiring search and vector capabilities
// into a query.Executor without creating import cycles.
package queryutil

import (
	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/search"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// WireCapabilities attaches full-text search and vector search to an executor.
// Returns the same executor pointer plus the constructed FullTextIndex so
// callers (notably the API server) can reference the same index instance
// as the executor — avoiding a second in-memory index and keeping query-DSL
// and REST/GraphQL surfaces consistent.
func WireCapabilities(executor *query.Executor, graph *storage.GraphStorage) (*query.Executor, *search.FullTextIndex) {
	// Full-text search
	idx := search.NewFullTextIndex(graph)
	executor.SetSearchIndex(idx)

	// Vector search — bridge vector.SearchResult → query.VectorSearchResult
	executor.SetVectorSearch(
		func(a, b []float32) (float64, error) {
			sim, err := vector.CosineSimilarity(a, b)
			return float64(sim), err
		},
		func(propertyName string, q []float32, k, ef int) ([]query.VectorSearchResult, error) {
			results, err := graph.VectorSearch(propertyName, q, k, ef)
			if err != nil {
				return nil, err
			}
			converted := make([]query.VectorSearchResult, len(results))
			for i, r := range results {
				converted[i] = query.VectorSearchResult{NodeID: r.ID, Distance: r.Distance}
			}
			return converted, nil
		},
		graph.HasVectorIndex,
		func(nodeID uint64) (any, error) {
			return graph.GetNode(nodeID)
		},
	)

	return executor, idx
}
