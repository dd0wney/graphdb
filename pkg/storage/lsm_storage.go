package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/lsm"
)

// LSMGraphStorage is a disk-backed graph storage using LSM trees
type LSMGraphStorage struct {
	lsm *lsm.LSMStorage

	// Counters
	nextNodeID uint64
	nextEdgeID uint64

	mu sync.RWMutex
}

// Key prefixes
const (
	KeyPrefixNode     = 'n' // n:<nodeID> -> Node
	KeyPrefixEdge     = 'e' // e:<edgeID> -> Edge
	KeyPrefixOutEdge  = 'o' // o:<fromID>:<toID> -> edgeID
	KeyPrefixInEdge   = 'i' // i:<toID>:<fromID> -> edgeID
	KeyPrefixLabel    = 'l' // l:<label>:<nodeID> -> empty
	KeyPrefixProperty = 'p' // p:<key>:<value>:<nodeID> -> empty
	KeyPrefixMeta     = 'm' // m:<key> -> value (metadata like counters)
)

// NewLSMGraphStorage creates a new LSM-backed graph storage
func NewLSMGraphStorage(dataDir string) (*LSMGraphStorage, error) {
	opts := lsm.DefaultLSMOptions(dataDir)
	opts.MemTableSize = 16 * 1024 * 1024 // 16MB MemTable

	storage, err := lsm.NewLSMStorage(opts)
	if err != nil {
		return nil, err
	}

	gs := &LSMGraphStorage{
		lsm:        storage,
		nextNodeID: 1,
		nextEdgeID: 1,
	}

	// Load counters from disk
	if err := gs.loadCounters(); err != nil {
		return nil, err
	}

	return gs, nil
}

// loadCounters loads node/edge counters from LSM
func (gs *LSMGraphStorage) loadCounters() error {
	// Load next node ID
	if value, ok := gs.lsm.Get(makeMetaKey("next_node_id")); ok {
		gs.nextNodeID = binary.BigEndian.Uint64(value)
	}

	// Load next edge ID
	if value, ok := gs.lsm.Get(makeMetaKey("next_edge_id")); ok {
		gs.nextEdgeID = binary.BigEndian.Uint64(value)
	}

	return nil
}

// saveCounters persists node/edge counters to LSM
func (gs *LSMGraphStorage) saveCounters() error {
	// Save next node ID
	nodeValue := make([]byte, 8)
	binary.BigEndian.PutUint64(nodeValue, gs.nextNodeID)
	if err := gs.lsm.Put(makeMetaKey("next_node_id"), nodeValue); err != nil {
		return err
	}

	// Save next edge ID
	edgeValue := make([]byte, 8)
	binary.BigEndian.PutUint64(edgeValue, gs.nextEdgeID)
	if err := gs.lsm.Put(makeMetaKey("next_edge_id"), edgeValue); err != nil {
		return err
	}

	return nil
}

// CreateNode creates a new node
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

// GetNode retrieves a node by ID
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

// UpdateNode updates a node's properties
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

// DeleteNode deletes a node and its edges
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

// CreateEdge creates a new edge
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

// GetEdge retrieves an edge by ID
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

// DeleteEdge deletes an edge
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

// GetOutgoingEdges returns all outgoing edges from a node
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

// GetIncomingEdges returns all incoming edges to a node
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

// FindNodesByLabel returns all nodes with a given label
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

// indexProperty creates a property index entry
func (gs *LSMGraphStorage) indexProperty(key string, value Value, nodeID uint64) error {
	return gs.lsm.Put(makePropertyKey(key, value, nodeID), []byte{})
}

// GetStatistics returns graph statistics
func (gs *LSMGraphStorage) GetStatistics() Statistics {
	return Statistics{
		NodeCount: atomic.LoadUint64(&gs.nextNodeID) - 1,
		EdgeCount: atomic.LoadUint64(&gs.nextEdgeID) - 1,
	}
}

// Close closes the LSM storage
func (gs *LSMGraphStorage) Close() error {
	// Save counters before closing
	if err := gs.saveCounters(); err != nil {
		return err
	}
	return gs.lsm.Close()
}

// Key generation functions

func makeNodeKey(nodeID uint64) []byte {
	key := make([]byte, 9)
	key[0] = KeyPrefixNode
	binary.BigEndian.PutUint64(key[1:], nodeID)
	return key
}

func makeEdgeKey(edgeID uint64) []byte {
	key := make([]byte, 9)
	key[0] = KeyPrefixEdge
	binary.BigEndian.PutUint64(key[1:], edgeID)
	return key
}

func makeOutEdgeKey(fromID, toID uint64) []byte {
	key := make([]byte, 17)
	key[0] = KeyPrefixOutEdge
	binary.BigEndian.PutUint64(key[1:9], fromID)
	binary.BigEndian.PutUint64(key[9:17], toID)
	return key
}

func makeInEdgeKey(toID, fromID uint64) []byte {
	key := make([]byte, 17)
	key[0] = KeyPrefixInEdge
	binary.BigEndian.PutUint64(key[1:9], toID)
	binary.BigEndian.PutUint64(key[9:17], fromID)
	return key
}

func makeLabelKey(label string, nodeID uint64) []byte {
	key := make([]byte, 1+len(label)+1+8)
	key[0] = KeyPrefixLabel
	copy(key[1:], label)
	key[1+len(label)] = ':'
	binary.BigEndian.PutUint64(key[1+len(label)+1:], nodeID)
	return key
}

func makePropertyKey(propKey string, value Value, nodeID uint64) []byte {
	valueStr := fmt.Sprintf("%v", value.Data)
	key := make([]byte, 1+len(propKey)+1+len(valueStr)+1+8)
	offset := 0

	key[offset] = KeyPrefixProperty
	offset++

	copy(key[offset:], propKey)
	offset += len(propKey)

	key[offset] = ':'
	offset++

	copy(key[offset:], valueStr)
	offset += len(valueStr)

	key[offset] = ':'
	offset++

	binary.BigEndian.PutUint64(key[offset:], nodeID)

	return key
}

func makeMetaKey(key string) []byte {
	result := make([]byte, 1+len(key))
	result[0] = KeyPrefixMeta
	copy(result[1:], key)
	return result
}
