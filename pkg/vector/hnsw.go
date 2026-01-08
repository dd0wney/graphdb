package vector

import (
	"container/heap"
	"fmt"
	"math"
	"math/rand"
	"sync"
)

// HNSWIndex implements Hierarchical Navigable Small World graph for approximate nearest neighbor search
type HNSWIndex struct {
	mu             sync.RWMutex
	dimensions     int
	m              int            // Number of bi-directional links per node
	mMax           int            // Maximum number of connections for layer > 0
	mMax0          int            // Maximum number of connections for layer 0
	efConstruction int            // Size of dynamic candidate list during construction
	ml             float64        // Normalization factor for level generation
	metric         DistanceMetric // Distance metric to use

	nodes      map[uint64]*hnswNode // All nodes by ID
	entryPoint *hnswNode            // Entry point for search (highest layer node)
	maxLayer   int                  // Current maximum layer
}

// NewHNSWIndex creates a new HNSW index
func NewHNSWIndex(dimensions, m, efConstruction int, metric DistanceMetric) (*HNSWIndex, error) {
	if dimensions <= 0 {
		return nil, fmt.Errorf("dimensions must be > 0, got %d", dimensions)
	}
	if m <= 0 {
		return nil, fmt.Errorf("m must be > 0, got %d", m)
	}
	if efConstruction <= 0 {
		return nil, fmt.Errorf("efConstruction must be > 0, got %d", efConstruction)
	}

	return &HNSWIndex{
		dimensions:     dimensions,
		m:              m,
		mMax:           m,
		mMax0:          m * 2,
		efConstruction: efConstruction,
		ml:             1.0 / math.Log(float64(m)),
		metric:         metric,
		nodes:          make(map[uint64]*hnswNode),
		maxLayer:       -1,
	}, nil
}

// Dimensions returns the vector dimensions
func (h *HNSWIndex) Dimensions() int {
	return h.dimensions
}

// Metric returns the distance metric used by this index
func (h *HNSWIndex) Metric() DistanceMetric {
	return h.metric
}

// Len returns the number of vectors in the index
func (h *HNSWIndex) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.nodes)
}

// Insert adds a vector to the index
func (h *HNSWIndex) Insert(id uint64, vector []float32) error {
	if len(vector) != h.dimensions {
		return fmt.Errorf("vector dimensions mismatch: got %d, want %d", len(vector), h.dimensions)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if already exists
	if _, exists := h.nodes[id]; exists {
		return fmt.Errorf("vector with ID %d already exists", id)
	}

	// Select layer for new node
	level := h.selectLevel()

	// Create new node
	node := &hnswNode{
		id:      id,
		vector:  make([]float32, len(vector)),
		level:   level,
		friends: make([][]uint64, level+1),
	}
	copy(node.vector, vector)

	// Initialize friend lists
	for i := 0; i <= level; i++ {
		node.friends[i] = make([]uint64, 0)
	}

	// If this is the first node
	if h.entryPoint == nil {
		h.entryPoint = node
		h.maxLayer = level
		h.nodes[id] = node
		return nil
	}

	// Find nearest neighbors and insert
	h.nodes[id] = node
	h.insertNode(node)

	// Update entry point if necessary
	if level > h.maxLayer {
		h.maxLayer = level
		h.entryPoint = node
	}

	return nil
}

// Search finds k nearest neighbors
func (h *HNSWIndex) Search(query []float32, k int, ef int) ([]SearchResult, error) {
	if len(query) != h.dimensions {
		return nil, fmt.Errorf("query dimensions mismatch: got %d, want %d", len(query), h.dimensions)
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entryPoint == nil {
		return []SearchResult{}, nil
	}

	// Search from entry point
	ep := h.entryPoint

	// Search from top layer to layer 1
	for layer := h.maxLayer; layer > 0; layer-- {
		ep, _ = h.searchLayer(query, ep, 1, layer)
	}

	// Search at layer 0 with ef
	candidates := h.searchLayerKNN(query, ep, ef, 0)

	// Select k nearest from candidates
	results := make([]SearchResult, 0, k)
	for len(results) < k && len(candidates) > 0 {
		// Defensive: safe type assertion with ok check
		item, ok := heap.Pop(&candidates).(*queueItem)
		if !ok {
			continue
		}
		results = append(results, SearchResult{
			ID:       item.id,
			Distance: item.distance,
		})
	}

	// Reverse results (they're in max-heap order, we want nearest first)
	for i := 0; i < len(results)/2; i++ {
		results[i], results[len(results)-1-i] = results[len(results)-1-i], results[i]
	}

	return results, nil
}

// Delete removes a vector from the index
func (h *HNSWIndex) Delete(id uint64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	node, exists := h.nodes[id]
	if !exists {
		return fmt.Errorf("vector with ID %d not found", id)
	}

	// Remove connections to this node from all neighbors
	for layer := 0; layer <= node.level; layer++ {
		for _, friendID := range node.friends[layer] {
			if friend, ok := h.nodes[friendID]; ok {
				h.removeConnection(friend, id, layer)
			}
		}
	}

	// Delete the node
	delete(h.nodes, id)

	// Update entry point if necessary
	if h.entryPoint != nil && h.entryPoint.id == id {
		h.entryPoint = h.findNewEntryPoint()
	}

	return nil
}

// selectLevel randomly selects a level for a new node
func (h *HNSWIndex) selectLevel() int {
	// Use exponential decay probability
	return int(-math.Log(rand.Float64()) * h.ml)
}
