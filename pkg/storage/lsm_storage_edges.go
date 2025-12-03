package storage

import (
	"encoding/binary"
	"encoding/json"
	"time"
)

// CreateEdge creates a new edge in LSM storage
func (gs *LSMGraphStorage) CreateEdge(fromID, toID uint64, relType string, properties map[string]Value, weight float64) (*Edge, error) {
	gs.mu.Lock()
	edgeID := gs.nextEdgeID
	gs.nextEdgeID++
	gs.mu.Unlock()

	edge := &Edge{
		ID:         edgeID,
		FromNodeID: fromID,
		ToNodeID:   toID,
		Type:       relType,
		Properties: properties,
		Weight:     weight,
		CreatedAt:  time.Now().Unix(),
	}

	// Serialize edge
	data, err := json.Marshal(edge)
	if err != nil {
		return nil, err
	}

	// Write edge data
	if err := gs.lsm.Put(makeEdgeKey(edgeID), data); err != nil {
		return nil, err
	}

	// Write edge indexes
	edgeIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(edgeIDBytes, edgeID)

	if err := gs.lsm.Put(makeOutEdgeKey(fromID, toID), edgeIDBytes); err != nil {
		return nil, err
	}

	if err := gs.lsm.Put(makeInEdgeKey(toID, fromID), edgeIDBytes); err != nil {
		return nil, err
	}

	// Periodically save counters
	if edgeID%1000 == 0 {
		gs.saveCounters()
	}

	return edge, nil
}

// GetEdge retrieves an edge by ID from LSM storage
func (gs *LSMGraphStorage) GetEdge(edgeID uint64) (*Edge, error) {
	data, ok := gs.lsm.Get(makeEdgeKey(edgeID))
	if !ok {
		return nil, ErrEdgeNotFound
	}

	var edge Edge
	if err := json.Unmarshal(data, &edge); err != nil {
		return nil, err
	}

	return &edge, nil
}

// DeleteEdge deletes an edge from LSM storage
func (gs *LSMGraphStorage) DeleteEdge(edgeID uint64) error {
	edge, err := gs.GetEdge(edgeID)
	if err != nil {
		return err
	}

	// Delete edge indexes
	gs.lsm.Delete(makeOutEdgeKey(edge.FromNodeID, edge.ToNodeID))
	gs.lsm.Delete(makeInEdgeKey(edge.ToNodeID, edge.FromNodeID))

	// Delete edge data
	return gs.lsm.Delete(makeEdgeKey(edgeID))
}

// GetOutgoingEdges returns all outgoing edges from a node in LSM storage
func (gs *LSMGraphStorage) GetOutgoingEdges(nodeID uint64) ([]*Edge, error) {
	// Scan range: o:<nodeID>:*
	start := makeOutEdgeKey(nodeID, 0)
	end := makeOutEdgeKey(nodeID+1, 0)

	results, err := gs.lsm.Scan(start, end)
	if err != nil {
		return nil, err
	}

	edges := make([]*Edge, 0, len(results))
	for _, edgeIDBytes := range results {
		edgeID := binary.BigEndian.Uint64(edgeIDBytes)
		if edge, err := gs.GetEdge(edgeID); err == nil {
			edges = append(edges, edge)
		}
	}

	return edges, nil
}

// GetIncomingEdges returns all incoming edges to a node in LSM storage
func (gs *LSMGraphStorage) GetIncomingEdges(nodeID uint64) ([]*Edge, error) {
	// Scan range: i:<nodeID>:*
	start := makeInEdgeKey(nodeID, 0)
	end := makeInEdgeKey(nodeID+1, 0)

	results, err := gs.lsm.Scan(start, end)
	if err != nil {
		return nil, err
	}

	edges := make([]*Edge, 0, len(results))
	for _, edgeIDBytes := range results {
		edgeID := binary.BigEndian.Uint64(edgeIDBytes)
		if edge, err := gs.GetEdge(edgeID); err == nil {
			edges = append(edges, edge)
		}
	}

	return edges, nil
}
