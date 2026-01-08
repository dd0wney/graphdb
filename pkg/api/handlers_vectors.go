package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// Vector API Request/Response Types

// VectorIndexRequest represents a request to create a vector index
type VectorIndexRequest struct {
	PropertyName   string `json:"property_name"`
	Dimensions     int    `json:"dimensions"`
	M              int    `json:"m,omitempty"`               // HNSW parameter (default: 16)
	EfConstruction int    `json:"ef_construction,omitempty"` // HNSW parameter (default: 200)
	Metric         string `json:"metric,omitempty"`          // "cosine", "euclidean", "dot_product" (default: "cosine")
}

// VectorIndexResponse represents a vector index in API responses
type VectorIndexResponse struct {
	PropertyName string `json:"property_name"`
	Dimensions   int    `json:"dimensions,omitempty"`
	Metric       string `json:"metric,omitempty"`
}

// VectorIndexListResponse represents list of vector indexes
type VectorIndexListResponse struct {
	Indexes []VectorIndexResponse `json:"indexes"`
	Count   int                   `json:"count"`
}

// VectorSearchRequest represents a vector similarity search request
type VectorSearchRequest struct {
	PropertyName string    `json:"property_name"`
	QueryVector  []float32 `json:"query_vector"`
	K            int       `json:"k"`             // Number of results
	Ef           int       `json:"ef,omitempty"`  // Search parameter (default: k * 2)
	IncludeNodes bool      `json:"include_nodes"` // Include full node data in results
	FilterLabels []string  `json:"filter_labels"` // Optional: filter results by labels
}

// VectorSearchResult represents a single search result
type VectorSearchResult struct {
	NodeID   uint64        `json:"node_id"`
	Distance float32       `json:"distance"`
	Score    float32       `json:"score"` // Similarity score (1 - distance for cosine)
	Node     *NodeResponse `json:"node,omitempty"`
}

// VectorSearchResponse represents vector search results
type VectorSearchResponse struct {
	Results []VectorSearchResult `json:"results"`
	Count   int                  `json:"count"`
	Time    string               `json:"time"`
}

// Default HNSW parameters
const (
	defaultM              = 16
	defaultEfConstruction = 200
	defaultMetric         = "cosine"
	maxK                  = 1000
	maxDimensions         = 4096
)

// parseMetric converts string metric to vector.DistanceMetric
func parseMetric(s string) vector.DistanceMetric {
	switch strings.ToLower(s) {
	case "euclidean", "l2":
		return vector.MetricEuclidean
	case "dot_product", "dot", "inner_product":
		return vector.MetricDotProduct
	default:
		return vector.MetricCosine
	}
}

// metricToString converts vector.DistanceMetric to string
func metricToString(m vector.DistanceMetric) string {
	switch m {
	case vector.MetricEuclidean:
		return "euclidean"
	case vector.MetricDotProduct:
		return "dot_product"
	default:
		return "cosine"
	}
}

// handleVectorIndexes handles /vector-indexes endpoint
func (s *Server) handleVectorIndexes(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.listVectorIndexes(w, r) }).
		Post(func() { s.createVectorIndex(w, r) }).
		NotAllowed()
}

// handleVectorIndex handles /vector-indexes/{name} endpoint
func (s *Server) handleVectorIndex(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.getVectorIndex(w, r) }).
		Delete(func() { s.deleteVectorIndex(w, r) }).
		NotAllowed()
}

// handleVectorSearch handles /vector-search endpoint
func (s *Server) handleVectorSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	s.vectorSearch(w, r)
}

// listVectorIndexes returns all vector indexes
func (s *Server) listVectorIndexes(w http.ResponseWriter, r *http.Request) {
	indexNames := s.graph.ListVectorIndexes()

	indexes := make([]VectorIndexResponse, 0, len(indexNames))
	for _, name := range indexNames {
		indexes = append(indexes, VectorIndexResponse{
			PropertyName: name,
		})
	}

	s.respondJSON(w, http.StatusOK, VectorIndexListResponse{
		Indexes: indexes,
		Count:   len(indexes),
	})
}

// createVectorIndex creates a new vector index
func (s *Server) createVectorIndex(w http.ResponseWriter, r *http.Request) {
	var req VectorIndexRequest
	if s.NewRequestDecoder(w, r).DecodeJSON(&req).RespondError() {
		return
	}

	// Validate request
	if req.PropertyName == "" {
		s.respondError(w, http.StatusBadRequest, "property_name is required")
		return
	}
	if req.Dimensions <= 0 || req.Dimensions > maxDimensions {
		s.respondError(w, http.StatusBadRequest, "dimensions must be between 1 and 4096")
		return
	}

	// Apply defaults
	m := req.M
	if m <= 0 {
		m = defaultM
	}
	efConstruction := req.EfConstruction
	if efConstruction <= 0 {
		efConstruction = defaultEfConstruction
	}
	metric := parseMetric(req.Metric)

	// Check if index already exists
	if s.graph.HasVectorIndex(req.PropertyName) {
		s.respondError(w, http.StatusConflict, "Vector index already exists for property: "+req.PropertyName)
		return
	}

	// Create the index
	err := s.graph.CreateVectorIndex(req.PropertyName, req.Dimensions, m, efConstruction, metric)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "create vector index"))
		return
	}

	s.respondJSON(w, http.StatusCreated, VectorIndexResponse{
		PropertyName: req.PropertyName,
		Dimensions:   req.Dimensions,
		Metric:       metricToString(metric),
	})
}

// getVectorIndex gets info about a specific vector index
func (s *Server) getVectorIndex(w http.ResponseWriter, r *http.Request) {
	// Extract property name from path
	propertyName := strings.TrimPrefix(r.URL.Path, "/vector-indexes/")
	propertyName = strings.TrimSuffix(propertyName, "/")

	if propertyName == "" {
		s.respondError(w, http.StatusBadRequest, "Property name is required")
		return
	}

	if !s.graph.HasVectorIndex(propertyName) {
		s.respondError(w, http.StatusNotFound, "Vector index not found: "+propertyName)
		return
	}

	s.respondJSON(w, http.StatusOK, VectorIndexResponse{
		PropertyName: propertyName,
	})
}

// deleteVectorIndex deletes a vector index
func (s *Server) deleteVectorIndex(w http.ResponseWriter, r *http.Request) {
	// Extract property name from path
	propertyName := strings.TrimPrefix(r.URL.Path, "/vector-indexes/")
	propertyName = strings.TrimSuffix(propertyName, "/")

	if propertyName == "" {
		s.respondError(w, http.StatusBadRequest, "Property name is required")
		return
	}

	if !s.graph.HasVectorIndex(propertyName) {
		s.respondError(w, http.StatusNotFound, "Vector index not found: "+propertyName)
		return
	}

	err := s.graph.DropVectorIndex(propertyName)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "delete vector index"))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// vectorSearch performs k-NN vector similarity search
func (s *Server) vectorSearch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req VectorSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.PropertyName == "" {
		s.respondError(w, http.StatusBadRequest, "property_name is required")
		return
	}
	if len(req.QueryVector) == 0 {
		s.respondError(w, http.StatusBadRequest, "query_vector is required")
		return
	}
	if req.K <= 0 || req.K > maxK {
		s.respondError(w, http.StatusBadRequest, "k must be between 1 and 1000")
		return
	}

	// Validate query vector for NaN/Inf values (security: prevents HNSW corruption)
	for _, v := range req.QueryVector {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			s.respondError(w, http.StatusBadRequest, "query_vector contains invalid value (NaN or Inf)")
			return
		}
	}

	// Check index exists
	if !s.graph.HasVectorIndex(req.PropertyName) {
		s.respondError(w, http.StatusNotFound, "Vector index not found: "+req.PropertyName)
		return
	}

	// Get the metric for correct score calculation
	metric, err := s.graph.GetVectorIndexMetric(req.PropertyName)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "get index metric"))
		return
	}

	// Set default ef
	ef := req.Ef
	if ef <= 0 {
		ef = req.K * 2
		if ef < 50 {
			ef = 50
		}
	}

	// Perform search
	searchResults, err := s.graph.VectorSearch(req.PropertyName, req.QueryVector, req.K, ef)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "vector search"))
		return
	}

	// Build response with proper filtering
	results := make([]VectorSearchResult, 0, len(searchResults))
	for _, sr := range searchResults {
		// Apply label filter BEFORE adding to results (fixes count mismatch)
		if req.IncludeNodes && len(req.FilterLabels) > 0 {
			node, err := s.graph.GetNode(sr.ID)
			if err != nil || node == nil {
				continue // Skip nodes we can't retrieve
			}
			if !hasAnyLabel(node.Labels, req.FilterLabels) {
				continue // Skip nodes that don't match filter
			}
		}

		result := VectorSearchResult{
			NodeID:   sr.ID,
			Distance: sr.Distance,
			Score:    distanceToScore(sr.Distance, metric),
		}

		// Optionally include full node data
		if req.IncludeNodes {
			node, err := s.graph.GetNode(sr.ID)
			if err == nil && node != nil {
				result.Node = s.nodeToResponse(node)
			}
		}

		results = append(results, result)
	}

	elapsed := time.Since(start)
	s.respondJSON(w, http.StatusOK, VectorSearchResponse{
		Results: results,
		Count:   len(results),
		Time:    elapsed.String(),
	})
}

// distanceToScore converts a distance value to a similarity score based on metric
func distanceToScore(distance float32, metric vector.DistanceMetric) float32 {
	switch metric {
	case vector.MetricCosine:
		// Cosine distance is in [0, 2], convert to similarity in [-1, 1]
		return 1.0 - distance
	case vector.MetricEuclidean:
		// Euclidean distance is in [0, ∞), convert to score in (0, 1]
		return 1.0 / (1.0 + distance)
	case vector.MetricDotProduct:
		// Dot product in HNSW is stored as negative (for min-heap), negate to get similarity
		return -distance
	default:
		return 1.0 - distance
	}
}

// hasAnyLabel checks if node has any of the specified labels
func hasAnyLabel(nodeLabels, filterLabels []string) bool {
	for _, nl := range nodeLabels {
		for _, fl := range filterLabels {
			if strings.EqualFold(nl, fl) {
				return true
			}
		}
	}
	return false
}
