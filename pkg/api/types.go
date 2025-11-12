package api

import "time"

// API Request/Response Types

// QueryRequest represents a query execution request
type QueryRequest struct {
	Query      string                 `json:"query"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// QueryResponse represents a query execution response
type QueryResponse struct {
	Columns []string                 `json:"columns"`
	Rows    []map[string]interface{} `json:"rows"`
	Count   int                      `json:"count"`
	Time    string                   `json:"time"`
}

// NodeRequest represents a node creation/update request
type NodeRequest struct {
	Labels     []string               `json:"labels"`
	Properties map[string]interface{} `json:"properties"`
}

// NodeResponse represents a node in API responses
type NodeResponse struct {
	ID         uint64                 `json:"id"`
	Labels     []string               `json:"labels"`
	Properties map[string]interface{} `json:"properties"`
}

// EdgeRequest represents an edge creation request
type EdgeRequest struct {
	FromNodeID uint64                 `json:"from_node_id"`
	ToNodeID   uint64                 `json:"to_node_id"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Weight     float64                `json:"weight"`
}

// EdgeResponse represents an edge in API responses
type EdgeResponse struct {
	ID         uint64                 `json:"id"`
	FromNodeID uint64                 `json:"from_node_id"`
	ToNodeID   uint64                 `json:"to_node_id"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Weight     float64                `json:"weight"`
}

// TraversalRequest represents a graph traversal request
type TraversalRequest struct {
	StartNodeID uint64   `json:"start_node_id"`
	MaxDepth    int      `json:"max_depth"`
	EdgeTypes   []string `json:"edge_types,omitempty"`
	Direction   string   `json:"direction"` // "outgoing", "incoming", "both"
}

// TraversalResponse represents traversal results
type TraversalResponse struct {
	Nodes []*NodeResponse `json:"nodes"`
	Count int             `json:"count"`
	Time  string          `json:"time"`
}

// ShortestPathRequest represents a shortest path query
type ShortestPathRequest struct {
	StartNodeID uint64 `json:"start_node_id"`
	EndNodeID   uint64 `json:"end_node_id"`
	MaxDepth    int    `json:"max_depth"`
}

// ShortestPathResponse represents the shortest path result
type ShortestPathResponse struct {
	Path   []uint64 `json:"path"`
	Length int      `json:"length"`
	Found  bool     `json:"found"`
	Time   string   `json:"time"`
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
	Uptime    string    `json:"uptime"`
}

// MetricsResponse represents database metrics
type MetricsResponse struct {
	NodeCount    uint64  `json:"node_count"`
	EdgeCount    uint64  `json:"edge_count"`
	TotalQueries uint64  `json:"total_queries"`
	AvgQueryTime float64 `json:"avg_query_time_ms"`
	Uptime       string  `json:"uptime"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// BatchNodeRequest represents a batch node creation request
type BatchNodeRequest struct {
	Nodes []NodeRequest `json:"nodes"`
}

// BatchNodeResponse represents batch node creation response
type BatchNodeResponse struct {
	Nodes   []*NodeResponse `json:"nodes"`
	Created int             `json:"created"`
	Time    string          `json:"time"`
}

// BatchEdgeRequest represents a batch edge creation request
type BatchEdgeRequest struct {
	Edges []EdgeRequest `json:"edges"`
}

// BatchEdgeResponse represents batch edge creation response
type BatchEdgeResponse struct {
	Edges   []*EdgeResponse `json:"edges"`
	Created int             `json:"created"`
	Time    string          `json:"time"`
}

// AlgorithmRequest represents a graph algorithm execution request
type AlgorithmRequest struct {
	Algorithm  string                 `json:"algorithm"` // "pagerank", "betweenness", "louvain"
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// AlgorithmResponse represents algorithm execution results
type AlgorithmResponse struct {
	Algorithm string                 `json:"algorithm"`
	Results   map[string]interface{} `json:"results"`
	Time      string                 `json:"time"`
}
