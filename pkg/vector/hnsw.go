package vector

import (
	"container/heap"
	"encoding/binary"
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

	store        nodeStore // Node storage (memory or persistent)
	entryPointID uint64    // ID of the entry point node
	maxLayer     int       // Current maximum layer

	// Metadata persistence
	kv      KVStore
	metaKey []byte
}

// NewHNSWIndex creates a new in-memory HNSW index
func NewHNSWIndex(dimensions, m, efConstruction int, metric DistanceMetric) (*HNSWIndex, error) {
	if dimensions <= 0 || m <= 0 || efConstruction <= 0 {
		return nil, fmt.Errorf("invalid HNSW parameters")
	}

	return &HNSWIndex{
		dimensions:     dimensions,
		m:              m,
		mMax:           m,
		mMax0:          m * 2,
		efConstruction: efConstruction,
		ml:             1.0 / math.Log(float64(m)),
		metric:         metric,
		store:          &memoryNodeStore{nodes: make(map[uint64]*hnswNode)},
		maxLayer:       -1,
	}, nil
}

// NewPersistentHNSWIndex creates a new HNSW index backed by a KVStore
func NewPersistentHNSWIndex(kv KVStore, prefix string, metaKey []byte, dimensions, m, efConstruction int, metric DistanceMetric) (*HNSWIndex, error) {
	h := &HNSWIndex{
		dimensions:     dimensions,
		m:              m,
		mMax:           m,
		mMax0:          m * 2,
		efConstruction: efConstruction,
		ml:             1.0 / math.Log(float64(m)),
		metric:         metric,
		store:          &kvNodeStore{kv: kv, prefix: prefix, cache: make(map[uint64]*hnswNode)},
		kv:             kv,
		metaKey:        metaKey,
		maxLayer:       -1,
	}

	// Load metadata if it exists
	if data, ok := kv.Get(metaKey); ok {
		if len(data) >= 21 { // 8 (ep) + 4 (layer) + 4 (dim) + 4 (m) + 1 (metric)
			h.entryPointID = binary.BigEndian.Uint64(data[0:8])
			h.maxLayer = int(binary.BigEndian.Uint32(data[8:12]))
			h.dimensions = int(binary.BigEndian.Uint32(data[12:16]))
			h.m = int(binary.BigEndian.Uint32(data[16:20]))
			h.metric = byteToMetric(data[20])
			// Re-calculate derived fields
			h.mMax = h.m
			h.mMax0 = h.m * 2
			h.ml = 1.0 / math.Log(float64(h.m))
		}
	}

	return h, nil
}

func (h *HNSWIndex) SaveMetadata() error {
	if h.kv == nil {
		return nil
	}
	data := make([]byte, 21)
	binary.BigEndian.PutUint64(data[0:8], h.entryPointID)
	binary.BigEndian.PutUint32(data[8:12], uint32(h.maxLayer))
	binary.BigEndian.PutUint32(data[12:16], uint32(h.dimensions))
	binary.BigEndian.PutUint32(data[16:20], uint32(h.m))
	data[20] = metricToByte(h.metric)
	return h.kv.Put(h.metaKey, data)
}

func metricToByte(m DistanceMetric) byte {
	switch m {
	case MetricEuclidean:
		return 1
	case MetricDotProduct:
		return 2
	default:
		return 0 // MetricCosine
	}
}

func byteToMetric(b byte) DistanceMetric {
	switch b {
	case 1:
		return MetricEuclidean
	case 2:
		return MetricDotProduct
	default:
		return MetricCosine
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

// Len returns the number of vectors in the index
func (h *HNSWIndex) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.store.Len()
}

// Insert adds a vector to the index
func (h *HNSWIndex) Insert(id uint64, vector []float32) error {
	if len(vector) != h.dimensions {
		return fmt.Errorf("vector dimensions mismatch: got %d, want %d", len(vector), h.dimensions)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if already exists
	if _, exists := h.store.Get(id); exists {
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
	if h.entryPointID == 0 {
		h.entryPointID = id
		h.maxLayer = level
		h.store.Put(node)
		h.SaveMetadata()
		return nil
	}

	// Find nearest neighbors and insert
	h.store.Put(node)
	h.insertNode(node)

	// Update entry point if necessary
	if level > h.maxLayer {
		h.maxLayer = level
		h.entryPointID = id
	}

	h.SaveMetadata()
	return nil
}

// Search finds k nearest neighbors
func (h *HNSWIndex) Search(query []float32, k int, ef int) ([]SearchResult, error) {
	if len(query) != h.dimensions {
		return nil, fmt.Errorf("query dimensions mismatch: got %d, want %d", len(query), h.dimensions)
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entryPointID == 0 {
		return []SearchResult{}, nil
	}

	// Search from entry point
	ep, ok := h.store.Get(h.entryPointID)
	if !ok {
		return []SearchResult{}, nil
	}

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

	node, exists := h.store.Get(id)
	if !exists {
		return fmt.Errorf("vector with ID %d not found", id)
	}

	// Remove connections to this node from all neighbors
	for layer := 0; layer <= node.level; layer++ {
		for _, friendID := range node.friends[layer] {
			if friend, ok := h.store.Get(friendID); ok {
				h.removeConnection(friend, id, layer)
			}
		}
	}

	// Delete the node
	h.store.Delete(id)

	// Update entry point if necessary
	if h.entryPointID == id {
		newEP := h.findNewEntryPoint()
		if newEP != nil {
			h.entryPointID = newEP.id
			h.maxLayer = newEP.level
		} else {
			h.entryPointID = 0
			h.maxLayer = -1
		}
		h.SaveMetadata()
	}

	return nil
}

// selectLevel randomly selects a level for a new node
func (h *HNSWIndex) selectLevel() int {
	// Use exponential decay probability
	return int(-math.Log(rand.Float64()) * h.ml)
}
