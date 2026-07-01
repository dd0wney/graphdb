package graphdb

// Node is a graph vertex. IDs are uint64 (graphdb's internal ID type).
type Node struct {
	ID         uint64         `json:"id"`
	Labels     []string       `json:"labels"`
	Properties map[string]any `json:"properties"`
}

// Edge is a directed, typed, weighted relationship.
type Edge struct {
	ID         uint64         `json:"id"`
	FromNodeID uint64         `json:"from_node_id"`
	ToNodeID   uint64         `json:"to_node_id"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Weight     float64        `json:"weight"`
}

// SearchHit is one full-text / hybrid search result.
type SearchHit struct {
	NodeID  uint64  `json:"node_id"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
	Node    *Node   `json:"node,omitempty"`
}

// HybridSearchResult wraps hybrid hits plus an optional degraded reason.
type HybridSearchResult struct {
	Results  []SearchHit `json:"results"`
	Degraded string      `json:"degraded,omitempty"`
}

// VectorHit is one nearest-neighbour result from vector search.
type VectorHit struct {
	NodeID   uint64  `json:"node_id"`
	Distance float64 `json:"distance"`
	Score    float64 `json:"score"`
	Node     *Node   `json:"node,omitempty"`
}

// VectorIndex describes a vector index.
type VectorIndex struct {
	PropertyName string `json:"property_name"`
	Dimensions   int    `json:"dimensions"`
	Metric       string `json:"metric,omitempty"`
}

// QueryResult is the raw result of a Cypher query.
type QueryResult struct {
	Rows []map[string]any `json:"rows"`
}

// EmbeddingsResult holds embedding vectors (OpenAI-shaped endpoint).
type EmbeddingsResult struct {
	Vectors [][]float64 `json:"vectors"`
}

// RetrievedDoc is one graph-augmented retrieval document.
type RetrievedDoc struct {
	NodeID  uint64  `json:"node_id"`
	Score   float64 `json:"score"`
	Content string  `json:"content,omitempty"`
}

// RetrieveResult is the result of graph-augmented retrieval.
type RetrieveResult struct {
	Documents []RetrievedDoc `json:"documents"`
}
