package storage

import (
	"encoding/binary"
	"encoding/json"
)

// CreateNode creates a new node in LSM storage
func (gs *LSMGraphStorage) CreateNode(labels []string, properties map[string]Value) (*Node, error) {
	gs.mu.Lock()
	nodeID := gs.nextNodeID
	gs.nextNodeID++
	gs.mu.Unlock()

	node := &Node{
		ID:         nodeID,
		Labels:     labels,
		Properties: properties,
	}

	// Serialize node
	data, err := json.Marshal(node)
	if err != nil {
		return nil, err
	}

	// Write node data
	if err := gs.lsm.Put(makeNodeKey(nodeID), data); err != nil {
		return nil, err
	}

	// Index labels
	for _, label := range labels {
		if err := gs.lsm.Put(makeLabelKey(label, nodeID), []byte{}); err != nil {
			return nil, err
		}
	}

	// Index properties
	for key, value := range properties {
		if err := gs.indexProperty(key, value, nodeID); err != nil {
			return nil, err
		}
	}

	// Periodically save counters
	if nodeID%1000 == 0 {
		gs.saveCounters()
	}

	return node, nil
}

// GetNode retrieves a node by ID from LSM storage
func (gs *LSMGraphStorage) GetNode(nodeID uint64) (*Node, error) {
	data, ok := gs.lsm.Get(makeNodeKey(nodeID))
	if !ok {
		return nil, ErrNodeNotFound
	}

	var node Node
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, err
	}

	return &node, nil
}

// UpdateNode updates a node's properties in LSM storage
func (gs *LSMGraphStorage) UpdateNode(nodeID uint64, properties map[string]Value) error {
	node, err := gs.GetNode(nodeID)
	if err != nil {
		return err
	}

	// Remove old property indexes
	for key, value := range node.Properties {
		if err := gs.lsm.Delete(makePropertyKey(key, value, nodeID)); err != nil {
			return err
		}
	}

	// Update properties
	node.Properties = properties

	// Serialize and write
	data, err := json.Marshal(node)
	if err != nil {
		return err
	}

	if err := gs.lsm.Put(makeNodeKey(nodeID), data); err != nil {
		return err
	}

	// Add new property indexes
	for key, value := range properties {
		if err := gs.indexProperty(key, value, nodeID); err != nil {
			return err
		}
	}

	return nil
}

// DeleteNode deletes a node and its edges from LSM storage
func (gs *LSMGraphStorage) DeleteNode(nodeID uint64) error {
	node, err := gs.GetNode(nodeID)
	if err != nil {
		return err
	}

	// Delete outgoing edges
	outEdges, err := gs.GetOutgoingEdges(nodeID)
	if err == nil {
		for _, edge := range outEdges {
			gs.DeleteEdge(edge.ID)
		}
	}

	// Delete incoming edges
	inEdges, err := gs.GetIncomingEdges(nodeID)
	if err == nil {
		for _, edge := range inEdges {
			gs.DeleteEdge(edge.ID)
		}
	}

	// Remove label indexes
	for _, label := range node.Labels {
		gs.lsm.Delete(makeLabelKey(label, nodeID))
	}

	// Remove property indexes
	for key, value := range node.Properties {
		gs.lsm.Delete(makePropertyKey(key, value, nodeID))
	}

	// Delete node
	return gs.lsm.Delete(makeNodeKey(nodeID))
}

// FindNodesByLabel returns all nodes with a given label from LSM storage
func (gs *LSMGraphStorage) FindNodesByLabel(label string) ([]*Node, error) {
	// Scan range: l:<label>:* to l:<label>:<max>
	// We use 0 for start and max uint64 for end to capture all nodeIDs
	start := makeLabelKey(label, 0)
	end := makeLabelKey(label, ^uint64(0)) // Max uint64

	results, err := gs.lsm.Scan(start, end)
	if err != nil {
		return nil, err
	}

	nodes := make([]*Node, 0, len(results))
	for keyStr := range results {
		key := []byte(keyStr)
		// Key format: 'l' + label + ':' + nodeID (8 bytes binary)
		// Calculate offset to nodeID: 1 (prefix) + len(label) + 1 (colon)
		offset := 1 + len(label) + 1
		if len(key) >= offset+8 {
			nodeID := binary.BigEndian.Uint64(key[offset:])
			if node, err := gs.GetNode(nodeID); err == nil {
				nodes = append(nodes, node)
			}
		}
	}

	return nodes, nil
}

// indexProperty creates a property index entry in LSM storage
func (gs *LSMGraphStorage) indexProperty(key string, value Value, nodeID uint64) error {
	return gs.lsm.Put(makePropertyKey(key, value, nodeID), []byte{})
}
