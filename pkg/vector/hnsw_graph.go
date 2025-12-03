package vector

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

		// Update ep for next layer (defensive: check node exists)
		if len(candidates) > 0 {
			if node, exists := h.nodes[candidates[0].id]; exists {
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
