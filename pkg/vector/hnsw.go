package vector

import (
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

	// nodesByLevel indexes nodes by their top level (level → set of nodes at
	// that level). It lets findNewEntryPoint pick a replacement entry point —
	// always a node at the highest occupied level — without the O(N) scan of
	// every node the deletion path used to pay (audit M4). Maintained on
	// insert and delete; the cost is O(#distinct levels) ≈ O(log N), versus
	// O(N) before. Guarded by h.mu like nodes/entryPoint/maxLayer.
	nodesByLevel map[int]map[uint64]*hnswNode

	// visitedPool recycles the per-layer visited-set maps that searchLayer /
	// searchLayerKNN would otherwise make() fresh on every call. Under N
	// concurrent searches the old pattern's GC pressure scaled with
	// concurrency × index depth (audit M5). Reuse is safe: a search holds
	// h.mu.RLock for its whole duration and its per-layer searches run
	// sequentially (never nested), so a borrowed map is never touched by two
	// goroutines at once. Stores map[uint64]bool.
	visitedPool sync.Pool
}

// getVisited borrows a cleared visited-set map from the pool, allocating one
// only when the pool is empty (M5).
func (h *HNSWIndex) getVisited() map[uint64]bool {
	if v, ok := h.visitedPool.Get().(map[uint64]bool); ok {
		return v
	}
	return make(map[uint64]bool)
}

// putVisited returns a visited-set map to the pool after clearing it. clear()
// keeps the grown bucket array, so the next borrow skips the rehash growth a
// fresh make() would pay.
func (h *HNSWIndex) putVisited(v map[uint64]bool) {
	clear(v)
	h.visitedPool.Put(v) //nolint:staticcheck // map header is pointer-sized; SA6002 targets slice/array values, not maps
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
		nodesByLevel:   make(map[int]map[uint64]*hnswNode),
		maxLayer:       -1,
	}, nil
}

// addToLevelIndex records node in nodesByLevel under its top level (M4).
// Caller must hold h.mu.Lock.
func (h *HNSWIndex) addToLevelIndex(node *hnswNode) {
	bucket := h.nodesByLevel[node.level]
	if bucket == nil {
		bucket = make(map[uint64]*hnswNode)
		h.nodesByLevel[node.level] = bucket
	}
	bucket[node.id] = node
}

// removeFromLevelIndex drops node from nodesByLevel, deleting the level
// bucket once it empties so findNewEntryPoint never considers a stale level
// (M4). Caller must hold h.mu.Lock.
func (h *HNSWIndex) removeFromLevelIndex(node *hnswNode) {
	bucket := h.nodesByLevel[node.level]
	if bucket == nil {
		return
	}
	delete(bucket, node.id)
	if len(bucket) == 0 {
		delete(h.nodesByLevel, node.level)
	}
}

// Dimensions returns the vector dimensions
func (h *HNSWIndex) Dimensions() int {
	return h.dimensions
}

// Metric returns the distance metric used by this index
func (h *HNSWIndex) Metric() DistanceMetric {
	return h.metric
}

// M returns the construction parameter m (bi-directional links per node).
// Exposed so the index definition can be persisted and recreated identically
// on restart (the HNSW graph itself is not serialized; it is rebuilt from the
// node set on load).
func (h *HNSWIndex) M() int {
	return h.m
}

// EfConstruction returns the construction-time candidate-list size. See M.
func (h *HNSWIndex) EfConstruction() int {
	return h.efConstruction
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
		norm:    Magnitude(vector), // M6: cache ‖vector‖ once
	}
	copy(node.vector, vector)

	// Initialize friend lists
	for i := 0; i <= level; i++ {
		node.friends[i] = make([]uint64, 0)
	}

	// Record in the level index (M4). Both the first-node and normal paths
	// below keep the node, so index it once here.
	h.addToLevelIndex(node)

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

	// Take the k nearest from the result set, ascending by distance.
	nearest := extractNearest(&candidates, k)
	results := make([]SearchResult, len(nearest))
	for i, item := range nearest {
		results[i] = SearchResult{ID: item.id, Distance: item.distance}
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

	// Delete the node — from both the id map and the level index. The level
	// removal must precede findNewEntryPoint so the deleted node can't be
	// re-selected as the new entry point (M4).
	delete(h.nodes, id)
	h.removeFromLevelIndex(node)

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
