package query

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// VectorSearchStep performs HNSW-accelerated pre-filtering.
// Inserted by the optimizer before MatchStep to pre-bind a variable
// to nodes whose embeddings are similar to a query vector.
type VectorSearchStep struct {
	variable             string   // e.g., "c"
	propertyName         string   // e.g., "embedding"
	threshold            float64  // minimum similarity (e.g., 0.8)
	labels               []string // label filter from MATCH pattern
	queryVectorParamName string   // parameter name (e.g., "query_embedding")
	queryVectorLiteral   []float32
	searchFn             VectorSearchFunc
	similarityFn         VectorSimilarityFunc
	getNodeFn            GetNodeFunc
	k                    int    // HNSW candidates (default 100)
	ef                   int    // HNSW search accuracy (default 50)
	distanceMetric       string // "cosine", "euclidean", "dot_product"
}

func (vs *VectorSearchStep) Execute(ctx *ExecutionContext) error {
	// Resolve query vector from parameter bindings
	queryVec, err := vs.resolveQueryVector(ctx)
	if err != nil {
		return fmt.Errorf("VectorSearchStep: %w", err)
	}

	// Call HNSW search
	results, err := vs.searchFn(vs.propertyName, queryVec, vs.k, vs.ef)
	if err != nil {
		return fmt.Errorf("VectorSearchStep search failed: %w", err)
	}

	newResults := make([]*BindingSet, 0, len(results))

	for _, sr := range results {
		similarity := vs.distanceToSimilarity(sr.Distance)

		if similarity < vs.threshold {
			continue
		}

		// Fetch the full node
		if vs.getNodeFn == nil {
			continue
		}
		nodeAny, err := vs.getNodeFn(sr.NodeID)
		if err != nil {
			continue // skip nodes that can't be fetched
		}
		node, ok := nodeAny.(*storage.Node)
		if !ok {
			continue
		}

		// Apply label filter
		if !vs.matchLabels(node) {
			continue
		}

		// Create new binding, carrying forward existing bindings from all input rows
		for _, existing := range ctx.results {
			newBinding := &BindingSet{
				bindings:     make(map[string]any, len(existing.bindings)+1),
				vectorScores: make(map[string]float64),
			}
			for k, v := range existing.bindings {
				newBinding.bindings[k] = v
			}
			newBinding.bindings[vs.variable] = node
			newBinding.vectorScores[vs.variable] = similarity
			newResults = append(newResults, newBinding)
		}
	}

	ctx.results = newResults
	return nil
}

// resolveQueryVector extracts the query vector from parameter bindings or literal
func (vs *VectorSearchStep) resolveQueryVector(ctx *ExecutionContext) ([]float32, error) {
	if vs.queryVectorLiteral != nil {
		return vs.queryVectorLiteral, nil
	}

	if vs.queryVectorParamName == "" {
		return nil, fmt.Errorf("no query vector specified")
	}

	// Look up parameter in bindings (parameters use "$" prefix)
	paramKey := "$" + vs.queryVectorParamName
	for _, binding := range ctx.results {
		if val, ok := binding.bindings[paramKey]; ok {
			vec, ok := toFloat32Slice(val)
			if !ok {
				return nil, fmt.Errorf("parameter $%s is not a vector: %T", vs.queryVectorParamName, val)
			}
			return vec, nil
		}
	}

	return nil, fmt.Errorf("parameter $%s not found in bindings", vs.queryVectorParamName)
}

// distanceToSimilarity converts HNSW distance to similarity score
func (vs *VectorSearchStep) distanceToSimilarity(distance float32) float64 {
	switch vs.distanceMetric {
	case "euclidean":
		return 1.0 / (1.0 + float64(distance))
	case "dot_product":
		return float64(-distance)
	default:
		// Cosine: similarity = 1 - distance (distance range [0,2])
		return 1.0 - float64(distance)
	}
}

// matchLabels checks if a node has all required labels
func (vs *VectorSearchStep) matchLabels(node *storage.Node) bool {
	if len(vs.labels) == 0 {
		return true
	}
	for _, required := range vs.labels {
		found := false
		for _, nodeLabel := range node.Labels {
			if nodeLabel == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (vs *VectorSearchStep) StepName() string { return "VectorSearchStep" }
func (vs *VectorSearchStep) StepDetail() string {
	return fmt.Sprintf("variable=%s property=%s threshold=%.2f k=%d", vs.variable, vs.propertyName, vs.threshold, vs.k)
}
