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

// EmbeddingsResult is the OpenAI-shaped /v1/embeddings response.
type EmbeddingsResult struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
}

// EmbeddingData is one embedding (with its position in the request's input array).
type EmbeddingData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

// Vectors returns just the embedding vectors, in the order returned by the server.
func (r *EmbeddingsResult) Vectors() [][]float64 {
	out := make([][]float64, len(r.Data))
	for i, d := range r.Data {
		out[i] = d.Embedding
	}
	return out
}

// RetrievedDoc is one graph-augmented retrieval document (LangChain-shaped).
type RetrievedDoc struct {
	PageContent string               `json:"page_content"`
	Metadata    RetrievedDocMetadata `json:"metadata"`
}

// RetrievedDocMetadata carries the graph signal for a retrieved chunk.
type RetrievedDocMetadata struct {
	NodeID uint64  `json:"node_id"`
	Score  float64 `json:"score"`
}

// RetrieveResult is the response of graph-augmented retrieval.
type RetrieveResult struct {
	Documents []RetrievedDoc `json:"documents"`
	Degraded  string         `json:"degraded,omitempty"`
}
