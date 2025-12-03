package storage

// buildNodeListFromIDs converts a slice of node IDs to a slice of cloned nodes
// Skips any node IDs that don't exist in storage
func (gs *GraphStorage) buildNodeListFromIDs(nodeIDs []uint64) []*Node {
	nodes := make([]*Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if node, exists := gs.nodes[nodeID]; exists {
			nodes = append(nodes, node.Clone())
		}
	}
	return nodes
}

// buildEdgeListFromIDs converts a slice of edge IDs to a slice of cloned edges
// Skips any edge IDs that don't exist in storage
func (gs *GraphStorage) buildEdgeListFromIDs(edgeIDs []uint64) []*Edge {
	edges := make([]*Edge, 0, len(edgeIDs))
	for _, edgeID := range edgeIDs {
		if edge, exists := gs.edges[edgeID]; exists {
			edges = append(edges, edge.Clone())
		}
	}
	return edges
}

// GetOutgoingEdges gets all outgoing edges from a node
func (gs *GraphStorage) GetOutgoingEdges(nodeID uint64) ([]*Edge, error) {
	defer gs.startQueryTiming()()

	// Use global read lock since we access global edge maps
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Get edge IDs using helper (checks disk/compressed/uncompressed storage)
	edgeIDs := gs.getEdgeIDsForNode(nodeID, true)
	if edgeIDs == nil {
		return []*Edge{}, nil
	}

	// Build edge list from IDs
	edges := gs.buildEdgeListFromIDs(edgeIDs)

	return edges, nil
}

// GetIncomingEdges gets all incoming edges to a node
func (gs *GraphStorage) GetIncomingEdges(nodeID uint64) ([]*Edge, error) {
	defer gs.startQueryTiming()()

	// Use global read lock since we access global edge maps
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Get edge IDs using helper (checks disk/compressed/uncompressed storage)
	edgeIDs := gs.getEdgeIDsForNode(nodeID, false)
	if edgeIDs == nil {
		return []*Edge{}, nil
	}

	// Build edge list from IDs
	edges := gs.buildEdgeListFromIDs(edgeIDs)

	return edges, nil
}

// GetAllLabels returns all unique node labels in the graph
func (gs *GraphStorage) GetAllLabels() []string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	labels := make([]string, 0, len(gs.nodesByLabel))
	for label := range gs.nodesByLabel {
		labels = append(labels, label)
	}

	return labels
}

// FindNodesByLabel finds all nodes with a specific label
func (gs *GraphStorage) FindNodesByLabel(label string) ([]*Node, error) {
	defer gs.startQueryTiming()()

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	nodeIDs, exists := gs.nodesByLabel[label]
	if !exists {
		return []*Node{}, nil
	}

	return gs.buildNodeListFromIDs(nodeIDs), nil
}

// FindNodesByProperty finds nodes with a specific property value
func (gs *GraphStorage) FindNodesByProperty(key string, value Value) ([]*Node, error) {
	defer gs.startQueryTiming()()

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	nodes := make([]*Node, 0)

	for _, node := range gs.nodes {
		if prop, exists := node.Properties[key]; exists {
			// Simple byte comparison for now (could be optimized)
			if string(prop.Data) == string(value.Data) && prop.Type == value.Type {
				nodes = append(nodes, node.Clone())
			}
		}
	}

	return nodes, nil
}

// FindEdgesByType finds all edges of a specific type
func (gs *GraphStorage) FindEdgesByType(edgeType string) ([]*Edge, error) {
	defer gs.startQueryTiming()()

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	edgeIDs, exists := gs.edgesByType[edgeType]
	if !exists {
		return []*Edge{}, nil
	}

	return gs.buildEdgeListFromIDs(edgeIDs), nil
}
