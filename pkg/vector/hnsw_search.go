package vector

import (
	"math"
)

// searchLayer performs greedy search at a specific layer
func (h *HNSWIndex) searchLayer(query []float32, ep *hnswNode, ef int, layer int) (*hnswNode, float32) {
	visited := h.getVisited()
	defer h.putVisited(visited)
	candidates := make(candidateQueue, 0, ef) // min-heap: expand nearest first
	w := make(priorityQueue, 0, ef)           // max-heap: furthest result at root

	queryNorm := h.queryNorm(query) // M6: ‖query‖ once, reused for every neighbor
	dist := h.distanceToNode(query, queryNorm, ep)
	cqPush(&candidates, queueItem{id: ep.id, distance: dist})
	pqPush(&w, queueItem{id: ep.id, distance: dist})
	visited[ep.id] = true

	for candidates.Len() > 0 {
		c := cqPop(&candidates) // nearest unexplored candidate

		// Stop once the nearest candidate is farther than the worst result:
		// nothing closer can remain in the candidate set.
		if c.distance > w[0].distance {
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
					friendDist := h.distanceToNode(query, queryNorm, friend)

					// Admit if there's room or this beats the current furthest
					// result (re-read w[0] each time — w mutates as we add).
					if w.Len() < ef || friendDist < w[0].distance {
						cqPush(&candidates, queueItem{id: friendID, distance: friendDist})
						pqPush(&w, queueItem{id: friendID, distance: friendDist})

						if w.Len() > ef {
							pqPop(&w) // evict furthest
						}
					}
				}
			}
		}
	}

	// Return the single nearest result.
	nearest := extractNearest(&w, 1)
	if len(nearest) == 0 {
		return ep, dist
	}
	return h.nodes[nearest[0].id], nearest[0].distance
}

// searchLayerKNN performs k-NN search at a specific layer
func (h *HNSWIndex) searchLayerKNN(query []float32, ep *hnswNode, ef int, layer int) priorityQueue {
	visited := h.getVisited()
	defer h.putVisited(visited)
	candidates := make(candidateQueue, 0, ef) // min-heap: expand nearest first
	w := make(priorityQueue, 0, ef)           // max-heap: furthest result at root

	queryNorm := h.queryNorm(query) // M6: ‖query‖ once, reused for every neighbor
	dist := h.distanceToNode(query, queryNorm, ep)
	cqPush(&candidates, queueItem{id: ep.id, distance: dist})
	pqPush(&w, queueItem{id: ep.id, distance: dist})
	visited[ep.id] = true

	for candidates.Len() > 0 {
		c := cqPop(&candidates) // nearest unexplored candidate

		// Stop once the nearest candidate is farther than the worst result.
		if c.distance > w[0].distance {
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
					friendDist := h.distanceToNode(query, queryNorm, friend)

					// Admit if there's room or this beats the current furthest
					// result (re-read w[0] each time — w mutates as we add).
					if w.Len() < ef || friendDist < w[0].distance {
						cqPush(&candidates, queueItem{id: friendID, distance: friendDist})
						pqPush(&w, queueItem{id: friendID, distance: friendDist})

						if w.Len() > ef {
							pqPop(&w) // evict furthest
						}
					}
				}
			}
		}
	}

	return w
}

// selectNeighbors selects up to M neighbors from candidates using the
// connectivity-preserving heuristic (see selectNeighborsHeuristic). Note: this
// drains the candidates' backing array via extractNearest, so callers must not
// read candidates after calling it.
func (h *HNSWIndex) selectNeighbors(candidates priorityQueue, m int) []SearchResult {
	ordered := extractNearest(&candidates, candidates.Len()) // all, ascending by distance
	chosen := h.selectNeighborsHeuristic(ordered, m)
	results := make([]SearchResult, len(chosen))
	for i, item := range chosen {
		results[i] = SearchResult{ID: item.id, Distance: item.distance}
	}
	return results
}

// selectNeighborsHeuristic chooses up to m neighbors from ordered (ascending by
// distance to the base point) using Malkov & Yashunin's Algorithm 4: a
// candidate is kept only if it is closer to the base than to any
// already-selected neighbor. Keeping diverse "bridge" links this way preserves
// graph connectivity, where simple keep-m-nearest pruning disconnects clusters
// (true neighbours become unreachable from the entry point → recall collapse).
//
// Each item's distance field is its distance to the base, so no base vector is
// needed; cross-distances between candidates are computed from stored vectors.
func (h *HNSWIndex) selectNeighborsHeuristic(ordered []queueItem, m int) []queueItem {
	selected := make([]queueItem, 0, m)
	for _, cand := range ordered {
		if len(selected) >= m {
			break
		}
		candNode, ok := h.nodes[cand.id]
		if !ok {
			continue
		}
		keep := true
		for _, sel := range selected {
			selNode, ok := h.nodes[sel.id]
			if !ok {
				continue
			}
			// Discard cand if it sits closer to an already-selected neighbour
			// than to the base — that neighbour already covers this direction.
			if h.distanceBetweenNodes(candNode, selNode) < cand.distance {
				keep = false
				break
			}
		}
		if keep {
			selected = append(selected, cand)
		}
	}
	return selected
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

// usesCosine reports whether this index's metric routes to cosine distance.
// Mirrors Distance's switch exactly: cosine for MetricCosine AND the default
// (any unrecognised metric), so the norm-caching fast paths below stay
// bit-identical to the generic h.distance for every metric value.
func (h *HNSWIndex) usesCosine() bool {
	return h.metric != MetricEuclidean && h.metric != MetricDotProduct
}

// queryNorm precomputes ‖query‖ once per Search for the cosine path (M6);
// returns 0 (unused) for metrics that don't need it.
func (h *HNSWIndex) queryNorm(query []float32) float32 {
	if h.usesCosine() {
		return Magnitude(query)
	}
	return 0
}

// distanceToNode computes the query→stored distance, reusing the query's
// precomputed norm and the node's cached norm on the cosine path (M6).
func (h *HNSWIndex) distanceToNode(query []float32, queryNorm float32, target *hnswNode) float32 {
	if h.usesCosine() {
		return cosineDistanceWithNorms(query, target.vector, queryNorm, target.norm)
	}
	return h.distance(query, target.vector)
}

// distanceBetweenNodes computes a stored→stored distance, reusing both cached
// norms on the cosine path (M6).
func (h *HNSWIndex) distanceBetweenNodes(a, b *hnswNode) float32 {
	if h.usesCosine() {
		return cosineDistanceWithNorms(a.vector, b.vector, a.norm, b.norm)
	}
	return h.distance(a.vector, b.vector)
}
