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

	nodes          map[uint64]*hnswNode   // All nodes by ID
	entryPoint     *hnswNode              // Entry point for search (highest layer node)
	maxLayer       int                    // Current maximum layer
}

// hnswNode represents a node in the HNSW graph
type hnswNode struct {
	id      uint64
	vector  []float32
	level   int
	friends [][]uint64 // Connections at each layer [layer][neighbors]
}

// SearchResult represents a search result with ID and distance
type SearchResult struct {
	ID       uint64
	Distance float32
}

// priorityQueue implements a max-heap for nearest neighbor search
type priorityQueue []*queueItem

type queueItem struct {
	id       uint64
	distance float32
}

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// Max-heap: larger distances have higher priority
	return pq[i].distance > pq[j].distance
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *priorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*queueItem))
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

// NewHNSWIndex creates a new HNSW index
func NewHNSWIndex(dimensions, m, efConstruction int, metric DistanceMetric) (*HNSWIndex, error) {
	if dimensions <= 0 {
		return nil, fmt.Errorf("dimensions must be > 0, got %d", dimensions)
	}
	if m <= 0 {
		return nil, fmt.Errorf("M must be > 0, got %d", m)
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
		item := heap.Pop(&candidates).(*queueItem)
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

// insertNode inserts a node into the graph
func (h *HNSWIndex) insertNode(node *hnswNode) {
	// Start from entry point
	ep := h.entryPoint

	// Search from top layer to node's level + 1
	for layer := h.maxLayer; layer > node.level; layer-- {
		ep, _ = h.searchLayer(node.vector, ep, 1, layer)
	}

	// Insert into layers from node.level down to 0
	for layer := node.level; layer >= 0; layer-- {
		// Find M nearest neighbors
		m := h.m
		if layer == 0 {
			m = h.mMax0
		}

		candidates := h.searchLayerKNN(node.vector, ep, h.efConstruction, layer)

		// Select M neighbors
		neighbors := h.selectNeighbors(candidates, m)

		// Add bidirectional links
		for _, neighbor := range neighbors {
			h.addConnection(node, neighbor.ID, layer)
			h.addConnection(h.nodes[neighbor.ID], node.id, layer)

			// Prune neighbors if needed
			neighborNode := h.nodes[neighbor.ID]
			if layer < len(neighborNode.friends) {
				maxConn := h.mMax
				if layer == 0 {
					maxConn = h.mMax0
				}

				if len(neighborNode.friends[layer]) > maxConn {
					h.pruneConnections(neighborNode, layer, maxConn)
				}
			}
		}

		// Update ep for next layer
		if len(candidates) > 0 {
			ep = h.nodes[candidates[0].id]
		}
	}
}

// searchLayer performs greedy search at a specific layer
func (h *HNSWIndex) searchLayer(query []float32, ep *hnswNode, ef int, layer int) (*hnswNode, float32) {
	visited := make(map[uint64]bool)
	candidates := make(priorityQueue, 0)
	w := make(priorityQueue, 0)

	dist := h.distance(query, ep.vector)
	heap.Push(&candidates, &queueItem{id: ep.id, distance: dist})
	heap.Push(&w, &queueItem{id: ep.id, distance: dist})
	visited[ep.id] = true

	for candidates.Len() > 0 {
		c := heap.Pop(&candidates).(*queueItem)

		// Get furthest point in w
		furthest := w[0].distance

		if c.distance > furthest {
			break
		}

		// Check neighbors
		node := h.nodes[c.id]
		if layer < len(node.friends) {
			for _, friendID := range node.friends[layer] {
				if !visited[friendID] {
					visited[friendID] = true
					friend := h.nodes[friendID]
					friendDist := h.distance(query, friend.vector)

					if friendDist < furthest || w.Len() < ef {
						heap.Push(&candidates, &queueItem{id: friendID, distance: friendDist})
						heap.Push(&w, &queueItem{id: friendID, distance: friendDist})

						if w.Len() > ef {
							heap.Pop(&w)
						}
					}
				}
			}
		}
	}

	// Return nearest
	if w.Len() > 0 {
		nearest := w[len(w)-1]
		return h.nodes[nearest.id], nearest.distance
	}

	return ep, dist
}

// searchLayerKNN performs k-NN search at a specific layer
func (h *HNSWIndex) searchLayerKNN(query []float32, ep *hnswNode, ef int, layer int) priorityQueue {
	visited := make(map[uint64]bool)
	candidates := make(priorityQueue, 0)
	w := make(priorityQueue, 0)

	dist := h.distance(query, ep.vector)
	heap.Push(&candidates, &queueItem{id: ep.id, distance: dist})
	heap.Push(&w, &queueItem{id: ep.id, distance: dist})
	visited[ep.id] = true

	for candidates.Len() > 0 {
		c := heap.Pop(&candidates).(*queueItem)

		furthest := w[0].distance

		if c.distance > furthest {
			break
		}

		node := h.nodes[c.id]
		if layer < len(node.friends) {
			for _, friendID := range node.friends[layer] {
				if !visited[friendID] {
					visited[friendID] = true
					friend := h.nodes[friendID]
					friendDist := h.distance(query, friend.vector)

					if friendDist < furthest || w.Len() < ef {
						heap.Push(&candidates, &queueItem{id: friendID, distance: friendDist})
						heap.Push(&w, &queueItem{id: friendID, distance: friendDist})

						if w.Len() > ef {
							heap.Pop(&w)
						}
					}
				}
			}
		}
	}

	return w
}

// selectNeighbors selects M best neighbors from candidates
func (h *HNSWIndex) selectNeighbors(candidates priorityQueue, m int) []SearchResult {
	// Simple heuristic: select M nearest
	results := make([]SearchResult, 0, m)

	for len(results) < m && len(candidates) > 0 {
		item := heap.Pop(&candidates).(*queueItem)
		results = append(results, SearchResult{
			ID:       item.id,
			Distance: item.distance,
		})
	}

	return results
}

// addConnection adds a bidirectional connection
func (h *HNSWIndex) addConnection(from *hnswNode, toID uint64, layer int) {
	if layer < len(from.friends) {
		from.friends[layer] = append(from.friends[layer], toID)
	}
}

// removeConnection removes a connection
func (h *HNSWIndex) removeConnection(from *hnswNode, toID uint64, layer int) {
	if layer < len(from.friends) {
		friends := from.friends[layer]
		for i, id := range friends {
			if id == toID {
				from.friends[layer] = append(friends[:i], friends[i+1:]...)
				break
			}
		}
	}
}

// pruneConnections prunes connections to maintain max connections
func (h *HNSWIndex) pruneConnections(node *hnswNode, layer int, maxConn int) {
	if layer >= len(node.friends) || len(node.friends[layer]) <= maxConn {
		return
	}

	// Keep maxConn nearest neighbors
	distances := make([]struct {
		id   uint64
		dist float32
	}, len(node.friends[layer]))

	for i, friendID := range node.friends[layer] {
		friend := h.nodes[friendID]
		distances[i] = struct {
			id   uint64
			dist float32
		}{
			id:   friendID,
			dist: h.distance(node.vector, friend.vector),
		}
	}

	// Sort by distance
	for i := 0; i < len(distances)-1; i++ {
		for j := i + 1; j < len(distances); j++ {
			if distances[j].dist < distances[i].dist {
				distances[i], distances[j] = distances[j], distances[i]
			}
		}
	}

	// Keep only maxConn nearest
	node.friends[layer] = make([]uint64, maxConn)
	for i := 0; i < maxConn; i++ {
		node.friends[layer][i] = distances[i].id
	}
}

// findNewEntryPoint finds a new entry point after deletion
func (h *HNSWIndex) findNewEntryPoint() *hnswNode {
	var newEntry *hnswNode
	maxLevel := -1

	for _, node := range h.nodes {
		if node.level > maxLevel {
			maxLevel = node.level
			newEntry = node
		}
	}

	if newEntry != nil {
		h.maxLayer = maxLevel
	}

	return newEntry
}

// distance calculates distance between two vectors
func (h *HNSWIndex) distance(a, b []float32) float32 {
	return Distance(a, b, h.metric)
}
