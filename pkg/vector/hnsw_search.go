package vector

import (
	"container/heap"
	"math"
)

// searchLayer performs greedy search at a specific layer
func (h *HNSWIndex) searchLayer(query []float32, ep *hnswNode, ef int, layer int) (*hnswNode, float32) {
	visited := make(map[uint64]bool)
	candidates := make(minPriorityQueue, 0) // min-heap: explore nearest candidate first
	w := make(priorityQueue, 0)             // max-heap: drop farthest when |W| > ef

	dist := h.distance(query, ep.vector)
	heap.Push(&candidates, &queueItem{id: ep.id, distance: dist})
	heap.Push(&w, &queueItem{id: ep.id, distance: dist})
	visited[ep.id] = true

	for candidates.Len() > 0 {
		// Defensive: safe type assertion with ok check
		c, ok := heap.Pop(&candidates).(*queueItem)
		if !ok {
			continue
		}

		// Get furthest point in w (defensive: check w is not empty)
		if w.Len() == 0 {
			break
		}
		furthest := w[0].distance

		if c.distance > furthest {
			break
		}

		// Check neighbors (defensive: verify node exists in map)
		node, exists := h.nodes[c.id]
		if !exists {
			continue
		}
		if layer < len(node.friends) {
			for _, friendID := range node.friends[layer] {
				if !visited[friendID] {
					visited[friendID] = true
					friend, friendExists := h.nodes[friendID]
					if !friendExists {
						continue
					}
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
	candidates := make(minPriorityQueue, 0) // min-heap: explore nearest candidate first
	w := make(priorityQueue, 0)             // max-heap: result set of ef best candidates

	dist := h.distance(query, ep.vector)
	heap.Push(&candidates, &queueItem{id: ep.id, distance: dist})
	heap.Push(&w, &queueItem{id: ep.id, distance: dist})
	visited[ep.id] = true

	for candidates.Len() > 0 {
		// Defensive: safe type assertion with ok check
		c, ok := heap.Pop(&candidates).(*queueItem)
		if !ok {
			continue
		}

		furthest := w[0].distance

		if c.distance > furthest {
			break
		}

		// Defensive: check node exists in map
		node, nodeExists := h.nodes[c.id]
		if !nodeExists {
			continue
		}
		if layer < len(node.friends) {
			for _, friendID := range node.friends[layer] {
				if !visited[friendID] {
					visited[friendID] = true
					friend, friendExists := h.nodes[friendID]
					if !friendExists {
						continue
					}
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

// selectNeighbors selects M nearest neighbors from a max-heap candidate set.
// The heap is drained in descending distance order; the M nearest are the
// last M elements, which are then reversed so index 0 is the nearest.
func (h *HNSWIndex) selectNeighbors(candidates priorityQueue, m int) []SearchResult {
	all := make([]SearchResult, 0, len(candidates))
	for len(candidates) > 0 {
		item, ok := heap.Pop(&candidates).(*queueItem)
		if !ok {
			continue
		}
		all = append(all, SearchResult{ID: item.id, Distance: item.distance})
	}
	// all[0] = farthest … all[len-1] = nearest; take m nearest
	start := len(all) - m
	if start < 0 {
		start = 0
	}
	results := all[start:]
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results
}

// distance calculates distance between two vectors
// Note: This assumes vectors have already been validated at insert time.
// If dimensions mismatch (should never happen), returns MaxFloat32 as a sentinel.
func (h *HNSWIndex) distance(a, b []float32) float32 {
	dist, err := Distance(a, b, h.metric)
	if err != nil {
		// This should never happen since we validate dimensions at insert time
		// Return max distance to ensure mismatched vectors are never selected
		return math.MaxFloat32
	}
	return dist
}
