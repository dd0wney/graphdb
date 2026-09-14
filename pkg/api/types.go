package api

import "time"

// API Request/Response Types

// QueryRequest represents a query execution request
type QueryRequest struct {
	Query          string         `json:"query"`
	Parameters     map[string]any `json:"parameters,omitempty"`
	TimeoutSeconds *int           `json:"timeout_seconds,omitempty"` // Optional per-query timeout (1-300 seconds)
}

// QueryResponse represents a query execution response
type QueryResponse struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Count   int              `json:"count"`
	Time    string           `json:"time"`
}

// NodeRequest represents a node creation/update request
type NodeRequest struct {
	Labels     []string       `json:"labels"`
	Properties map[string]any `json:"properties"`
}

// NodeResponse represents a node in API responses
type NodeResponse struct {
	ID         uint64         `json:"id"`
	Labels     []string       `json:"labels"`
	Properties map[string]any `json:"properties"`
}

// EdgeRequest represents an edge creation request
type EdgeRequest struct {
	FromNodeID uint64         `json:"from_node_id"`
	ToNodeID   uint64         `json:"to_node_id"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Weight     float64        `json:"weight"`
}

// EdgeUpdateRequest is the body for PUT /edges/{id}. Weight is a *pointer* so an
// omitted weight means "leave it unchanged" — a bare float64 would decode a
// missing weight as 0.0 and silently zero the edge's weight on a
// properties-only update. Maps to storage.UpdateEdgeForTenant(weight *float64).
type EdgeUpdateRequest struct {
	Properties map[string]any `json:"properties,omitempty"`
	Weight     *float64       `json:"weight,omitempty"`
}

// EdgeResponse represents an edge in API responses
type EdgeResponse struct {
	ID         uint64         `json:"id"`
	FromNodeID uint64         `json:"from_node_id"`
	ToNodeID   uint64         `json:"to_node_id"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Weight     float64        `json:"weight"`
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
	// Truncated is true when the traversal stopped at MaxTraversalNodes
	// before exhausting the reachable set (security audit H-8). The same
	// signal is mirrored in the X-Truncated response header.
	Truncated bool `json:"truncated,omitempty"`
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
	Status    string         `json:"status"`
	Timestamp time.Time      `json:"timestamp"`
	Version   string         `json:"version"`
	Edition   string         `json:"edition"`
	Features  []string       `json:"features"`
	Uptime    string         `json:"uptime"`
	Checks    map[string]any `json:"checks,omitempty"`
}

// MetricsResponse represents database metrics
type MetricsResponse struct {
	// Database stats
	NodeCount    uint64  `json:"node_count"`
	EdgeCount    uint64  `json:"edge_count"`
	TotalQueries uint64  `json:"total_queries"`
	AvgQueryTime float64 `json:"avg_query_time_ms"`

	// System stats
	MemoryUsedMB  uint64 `json:"memory_used_mb"`
	MemoryTotalMB uint64 `json:"memory_total_mb"`
	NumGoroutines int    `json:"num_goroutines"`
	NumCPU        int    `json:"num_cpu"`

	// Server stats
	Uptime        string `json:"uptime"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// notDurableFields are the five status fields shared by every response for
// a write whose in-memory change applied but whose WAL append failed
// (storage.ErrWALWriteFailed). Every reader already sees the change, and it
// becomes durable at the next snapshot or clean shutdown — a retry would
// apply the change a second time, so Retry is always false here.
//
// Message is a fixed sentence. The wrapped disk error never reaches this
// body (see respondWALWriteFailed in server_helpers.go) — same precedent as
// ErrRecordUnreadable (handlers_nodes.go's getNode).
//
// Embedded anonymously in NodeNotDurableResponse, EdgeNotDurableResponse
// and WriteNotDurableResponse below: encoding/json promotes an anonymous
// field's exported members into its parent's JSON object regardless of
// whether the field's own type name is exported, so each of the three
// response types below serializes as one flat object.
type notDurableFields struct {
	Applied bool   `json:"applied"`
	Durable bool   `json:"durable"`
	Retry   bool   `json:"retry"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

// NodeNotDurableResponse is the body createNode returns when its WAL append
// failed. It is a SUPERSET of the 201 NodeResponse body — same id, labels
// and properties (masking already applied), plus the five not-durable
// fields — so a client that treats any 2xx as success and reads only the
// node fields still builds a correct node; an informed client also reads
// Applied/Durable/Retry/Error/Message.
type NodeNotDurableResponse struct {
	NodeResponse
	notDurableFields
}

// EdgeNotDurableResponse mirrors NodeNotDurableResponse for createEdge.
type EdgeNotDurableResponse struct {
	EdgeResponse
	notDurableFields
}

// WriteNotDurableResponse is the body updateNode, deleteNode, updateEdge,
// deleteEdge, deleteAllNodes, handleDeleteTenant, and the vector-index
// create/drop handlers return when the WAL append failed. Unlike the two
// create paths above, these have no full entity to return — only an id
// (0, omitted, for deleteAllNodes/handleDeleteTenant's bulk deletes and for
// the vector-index handlers, which have no single per-write entity id).
type WriteNotDurableResponse struct {
	ID uint64 `json:"id,omitempty"`
	notDurableFields
}

// BatchNodeRequest represents a batch node creation request
type BatchNodeRequest struct {
	Nodes []NodeRequest `json:"nodes"`
}

// BatchItemError records one failed item from a batch create request.
// Index is the item's position in the REQUEST array (Nodes/Edges as
// submitted) — NOT its position in the response, since failed items
// are omitted from the response array entirely (#455).
type BatchItemError struct {
	Index int    `json:"index"`
	Error string `json:"error"`
}

// BatchNodeResponse represents batch node creation response
type BatchNodeResponse struct {
	Nodes   []*NodeResponse  `json:"nodes"`
	Created int              `json:"created"`
	Time    string           `json:"time"`
	Failed  int              `json:"failed"`
	Errors  []BatchItemError `json:"errors,omitempty"`
}

// BatchEdgeRequest represents a batch edge creation request
type BatchEdgeRequest struct {
	Edges []EdgeRequest `json:"edges"`
}

// BatchEdgeResponse represents batch edge creation response
type BatchEdgeResponse struct {
	Edges   []*EdgeResponse  `json:"edges"`
	Created int              `json:"created"`
	Time    string           `json:"time"`
	Failed  int              `json:"failed"`
	Errors  []BatchItemError `json:"errors,omitempty"`
}

// AlgorithmRequest represents a graph algorithm execution request
type AlgorithmRequest struct {
	Algorithm  string         `json:"algorithm"` // "pagerank", "betweenness", "louvain"
	Parameters map[string]any `json:"parameters,omitempty"`
}

// AlgorithmResponse represents algorithm execution results
type AlgorithmResponse struct {
	Algorithm string         `json:"algorithm"`
	Results   map[string]any `json:"results"`
	Time      string         `json:"time"`
}
