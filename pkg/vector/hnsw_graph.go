package vector

import "sort"

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

		// Update ep for next layer: descend from the nearest selected neighbor.
		// neighbors is ascending by distance (selectNeighbors returns nearest
		// first); candidates must not be read here — selectNeighbors drained it.
		if len(neighbors) > 0 {
			if node, exists := h.nodes[neighbors[0].ID]; exists {
				ep = node
			}
		}
	}
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

// pruneConnections trims node's layer connections back to maxConn using the
// connectivity-preserving heuristic (selectNeighborsHeuristic), not simple
// keep-nearest. Simple pruning drops the bridge links that keep distant cluster
// members reachable, collapsing recall once an index exceeds a cluster's worth
// of nodes; the heuristic keeps diverse neighbours that preserve reachability.
func (h *HNSWIndex) pruneConnections(node *hnswNode, layer int, maxConn int) {
	if layer >= len(node.friends) || len(node.friends[layer]) <= maxConn {
		return
	}

	// Build the candidate list ordered ascending by distance to node.
	ordered := make([]queueItem, 0, len(node.friends[layer]))
	for _, friendID := range node.friends[layer] {
		friend, ok := h.nodes[friendID]
		if !ok {
			continue
		}
		ordered = append(ordered, queueItem{id: friendID, distance: h.distanceBetweenNodes(node, friend)})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].distance < ordered[j].distance })

	kept := h.selectNeighborsHeuristic(ordered, maxConn)
	node.friends[layer] = make([]uint64, len(kept))
	for i, item := range kept {
		node.friends[layer][i] = item.id
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
