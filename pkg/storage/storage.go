package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/wal"
)

var (
	ErrNodeNotFound = fmt.Errorf("node not found")
	ErrEdgeNotFound = fmt.Errorf("edge not found")
)

// GraphStorage is the core in-memory graph storage engine
type GraphStorage struct {
	// Core data structures
	nodes map[uint64]*Node
	edges map[uint64]*Edge

	// Indexes for fast lookups
	nodesByLabel    map[string][]uint64       // label -> node IDs
	edgesByType     map[string][]uint64       // edge type -> edge IDs
	outgoingEdges   map[uint64][]uint64       // node ID -> outgoing edge IDs (uncompressed)
	incomingEdges   map[uint64][]uint64       // node ID -> incoming edge IDs (uncompressed)
	propertyIndexes map[string]*PropertyIndex // property key -> index

	// Compressed edge storage (optional)
	compressedOutgoing map[uint64]*CompressedEdgeList // node ID -> compressed outgoing edges
	compressedIncoming map[uint64]*CompressedEdgeList // node ID -> compressed incoming edges
	useEdgeCompression bool

	// ID generators
	nextNodeID uint64
	nextEdgeID uint64

	// Concurrency control
	mu sync.RWMutex

	// Persistence
	dataDir        string
	wal            *wal.WAL
	batchedWAL     *wal.BatchedWAL
	compressedWAL  *wal.CompressedWAL
	useBatching    bool
	useCompression bool

	// Statistics (using atomic operations for thread-safety)
	stats Statistics
}

// StorageConfig holds configuration for GraphStorage
type StorageConfig struct {
	DataDir               string
	EnableBatching        bool
	EnableCompression     bool
	EnableEdgeCompression bool
	BatchSize             int
	FlushInterval         time.Duration
}

// Statistics tracks database statistics
type Statistics struct {
	NodeCount    uint64
	EdgeCount    uint64
	LastSnapshot time.Time
	TotalQueries uint64
	AvgQueryTime float64
}

// NewGraphStorage creates a new graph storage engine with default config
func NewGraphStorage(dataDir string) (*GraphStorage, error) {
	return NewGraphStorageWithConfig(StorageConfig{
		DataDir:               dataDir,
		EnableBatching:        false,
		EnableCompression:     false,
		EnableEdgeCompression: false,
		BatchSize:             100,
		FlushInterval:         10 * time.Millisecond,
	})
}

// NewGraphStorageWithConfig creates a new graph storage engine with custom config
func NewGraphStorageWithConfig(config StorageConfig) (*GraphStorage, error) {
	gs := &GraphStorage{
		nodes:              make(map[uint64]*Node),
		edges:              make(map[uint64]*Edge),
		nodesByLabel:       make(map[string][]uint64),
		edgesByType:        make(map[string][]uint64),
		outgoingEdges:      make(map[uint64][]uint64),
		incomingEdges:      make(map[uint64][]uint64),
		propertyIndexes:    make(map[string]*PropertyIndex),
		compressedOutgoing: make(map[uint64]*CompressedEdgeList),
		compressedIncoming: make(map[uint64]*CompressedEdgeList),
		useEdgeCompression: config.EnableEdgeCompression,
		dataDir:            config.DataDir,
		useBatching:        config.EnableBatching,
		useCompression:     config.EnableCompression,
		nextNodeID:         1,
		nextEdgeID:         1,
	}

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize WAL (compressed, batched, or regular)
	if config.EnableCompression {
		compressedWAL, err := wal.NewCompressedWAL(filepath.Join(config.DataDir, "wal"))
		if err != nil {
			return nil, fmt.Errorf("failed to initialize compressed WAL: %w", err)
		}
		gs.compressedWAL = compressedWAL
	} else if config.EnableBatching {
		batchedWAL, err := wal.NewBatchedWAL(
			filepath.Join(config.DataDir, "wal"),
			config.BatchSize,
			config.FlushInterval,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize batched WAL: %w", err)
		}
		gs.batchedWAL = batchedWAL
	} else {
		walInstance, err := wal.NewWAL(filepath.Join(config.DataDir, "wal"))
		if err != nil {
			return nil, fmt.Errorf("failed to initialize WAL: %w", err)
		}
		gs.wal = walInstance
	}

	// Try to load from disk
	if err := gs.loadFromDisk(); err != nil {
		// If no snapshot exists, that's OK (fresh database)
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load from disk: %w", err)
		}
	}

	// Replay WAL entries since last snapshot
	if err := gs.replayWAL(); err != nil {
		return nil, fmt.Errorf("failed to replay WAL: %w", err)
	}

	return gs, nil
}

// CreateNode creates a new node
func (gs *GraphStorage) CreateNode(labels []string, properties map[string]Value) (*Node, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check for ID space exhaustion
	if gs.nextNodeID == ^uint64(0) { // MaxUint64
		return nil, fmt.Errorf("node ID space exhausted")
	}

	nodeID := gs.nextNodeID
	gs.nextNodeID++

	now := time.Now().Unix()
	node := &Node{
		ID:         nodeID,
		Labels:     labels,
		Properties: properties,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	gs.nodes[nodeID] = node

	// Update label indexes
	for _, label := range labels {
		gs.nodesByLabel[label] = append(gs.nodesByLabel[label], nodeID)
	}

	// Initialize edge lists
	gs.outgoingEdges[nodeID] = make([]uint64, 0)
	gs.incomingEdges[nodeID] = make([]uint64, 0)

	atomic.AddUint64(&gs.stats.NodeCount, 1)

	// Update property indexes
	for key, value := range properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			idx.Insert(nodeID, value)
		}
	}

	// Write to WAL for durability
	nodeData, err := json.Marshal(node)
	if err == nil {
		if gs.useBatching && gs.batchedWAL != nil {
			gs.batchedWAL.Append(wal.OpCreateNode, nodeData)
		} else if gs.wal != nil {
			gs.wal.Append(wal.OpCreateNode, nodeData)
		}
	}

	return node.Clone(), nil
}

// GetNode retrieves a node by ID
func (gs *GraphStorage) GetNode(nodeID uint64) (*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return nil, ErrNodeNotFound
	}

	return node.Clone(), nil
}

// UpdateNode updates a node's properties
func (gs *GraphStorage) UpdateNode(nodeID uint64, properties map[string]Value) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Update property indexes
	for k, newValue := range properties {
		if idx, exists := gs.propertyIndexes[k]; exists {
			// Remove old value from index if it exists
			if oldValue, exists := node.Properties[k]; exists {
				idx.Remove(nodeID, oldValue)
			}
			// Add new value to index
			idx.Insert(nodeID, newValue)
		}
	}

	// Update properties
	for k, v := range properties {
		node.Properties[k] = v
	}
	node.UpdatedAt = time.Now().Unix()

	return nil
}

// DeleteNode deletes a node and all its edges
func (gs *GraphStorage) DeleteNode(nodeID uint64) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Delete all outgoing edges (with underflow protection)
	for _, edgeID := range gs.outgoingEdges[nodeID] {
		if _, exists := gs.edges[edgeID]; exists {
			delete(gs.edges, edgeID)
			// Atomic decrement with underflow protection
			for {
				current := atomic.LoadUint64(&gs.stats.EdgeCount)
				if current == 0 {
					break
				}
				if atomic.CompareAndSwapUint64(&gs.stats.EdgeCount, current, current-1) {
					break
				}
			}
		}
	}

	// Delete all incoming edges (with underflow protection)
	for _, edgeID := range gs.incomingEdges[nodeID] {
		if _, exists := gs.edges[edgeID]; exists {
			delete(gs.edges, edgeID)
			// Atomic decrement with underflow protection
			for {
				current := atomic.LoadUint64(&gs.stats.EdgeCount)
				if current == 0 {
					break
				}
				if atomic.CompareAndSwapUint64(&gs.stats.EdgeCount, current, current-1) {
					break
				}
			}
		}
	}

	// Remove from label indexes
	for _, label := range node.Labels {
		gs.removeFromLabelIndex(label, nodeID)
	}

	// Remove from property indexes
	for key, value := range node.Properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			idx.Remove(nodeID, value)
		}
	}

	// Delete node
	delete(gs.nodes, nodeID)
	delete(gs.outgoingEdges, nodeID)
	delete(gs.incomingEdges, nodeID)

	// Atomic decrement with underflow protection
	for {
		current := atomic.LoadUint64(&gs.stats.NodeCount)
		if current == 0 {
			break
		}
		if atomic.CompareAndSwapUint64(&gs.stats.NodeCount, current, current-1) {
			break
		}
	}

	return nil
}

// CreateEdge creates a new edge between two nodes
func (gs *GraphStorage) CreateEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Verify nodes exist
	if _, exists := gs.nodes[fromID]; !exists {
		return nil, fmt.Errorf("source node %d not found", fromID)
	}
	if _, exists := gs.nodes[toID]; !exists {
		return nil, fmt.Errorf("target node %d not found", toID)
	}

	// Check for ID space exhaustion
	if gs.nextEdgeID == ^uint64(0) { // MaxUint64
		return nil, fmt.Errorf("edge ID space exhausted")
	}

	edgeID := gs.nextEdgeID
	gs.nextEdgeID++

	edge := &Edge{
		ID:         edgeID,
		FromNodeID: fromID,
		ToNodeID:   toID,
		Type:       edgeType,
		Properties: properties,
		Weight:     weight,
		CreatedAt:  time.Now().Unix(),
	}

	gs.edges[edgeID] = edge

	// Update indexes
	gs.edgesByType[edgeType] = append(gs.edgesByType[edgeType], edgeID)
	gs.outgoingEdges[fromID] = append(gs.outgoingEdges[fromID], edgeID)
	gs.incomingEdges[toID] = append(gs.incomingEdges[toID], edgeID)

	atomic.AddUint64(&gs.stats.EdgeCount, 1)

	// Write to WAL for durability
	edgeData, err := json.Marshal(edge)
	if err == nil {
		if gs.useBatching && gs.batchedWAL != nil {
			gs.batchedWAL.Append(wal.OpCreateEdge, edgeData)
		} else if gs.wal != nil {
			gs.wal.Append(wal.OpCreateEdge, edgeData)
		}
	}

	return edge.Clone(), nil
}

// GetEdge retrieves an edge by ID
func (gs *GraphStorage) GetEdge(edgeID uint64) (*Edge, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	edge, exists := gs.edges[edgeID]
	if !exists {
		return nil, ErrEdgeNotFound
	}

	return edge.Clone(), nil
}

// GetOutgoingEdges gets all outgoing edges from a node
func (gs *GraphStorage) GetOutgoingEdges(nodeID uint64) ([]*Edge, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	var edgeIDs []uint64

	// Check compressed storage first if compression is enabled
	if gs.useEdgeCompression {
		if compressed, exists := gs.compressedOutgoing[nodeID]; exists {
			edgeIDs = compressed.Decompress()
		}
	}

	// Fall back to uncompressed storage
	if edgeIDs == nil {
		if uncompressed, exists := gs.outgoingEdges[nodeID]; exists {
			edgeIDs = uncompressed
		} else {
			return []*Edge{}, nil
		}
	}

	edges := make([]*Edge, 0, len(edgeIDs))
	for _, edgeID := range edgeIDs {
		if edge, exists := gs.edges[edgeID]; exists {
			edges = append(edges, edge.Clone())
		}
	}

	return edges, nil
}

// GetIncomingEdges gets all incoming edges to a node
func (gs *GraphStorage) GetIncomingEdges(nodeID uint64) ([]*Edge, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	edgeIDs, exists := gs.incomingEdges[nodeID]
	if !exists {
		return []*Edge{}, nil
	}

	edges := make([]*Edge, 0, len(edgeIDs))
	for _, edgeID := range edgeIDs {
		if edge, exists := gs.edges[edgeID]; exists {
			edges = append(edges, edge.Clone())
		}
	}

	return edges, nil
}

// FindNodesByLabel finds all nodes with a specific label
func (gs *GraphStorage) FindNodesByLabel(label string) ([]*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	nodeIDs, exists := gs.nodesByLabel[label]
	if !exists {
		return []*Node{}, nil
	}

	nodes := make([]*Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if node, exists := gs.nodes[nodeID]; exists {
			nodes = append(nodes, node.Clone())
		}
	}

	return nodes, nil
}

// FindNodesByProperty finds nodes with a specific property value
func (gs *GraphStorage) FindNodesByProperty(key string, value Value) ([]*Node, error) {
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
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	edgeIDs, exists := gs.edgesByType[edgeType]
	if !exists {
		return []*Edge{}, nil
	}

	edges := make([]*Edge, 0, len(edgeIDs))
	for _, edgeID := range edgeIDs {
		if edge, exists := gs.edges[edgeID]; exists {
			edges = append(edges, edge.Clone())
		}
	}

	return edges, nil
}

// GetStatistics returns database statistics (thread-safe using atomic operations)
func (gs *GraphStorage) GetStatistics() Statistics {
	return Statistics{
		NodeCount:    atomic.LoadUint64(&gs.stats.NodeCount),
		EdgeCount:    atomic.LoadUint64(&gs.stats.EdgeCount),
		TotalQueries: atomic.LoadUint64(&gs.stats.TotalQueries),
		// Note: LastSnapshot and AvgQueryTime are read under lock if needed elsewhere
		// For now we don't atomically access these as they're not critical for correctness
		LastSnapshot: gs.stats.LastSnapshot,
		AvgQueryTime: gs.stats.AvgQueryTime,
	}
}

// allocateNodeID allocates a new node ID in a thread-safe manner
// Returns error if ID space is exhausted
func (gs *GraphStorage) allocateNodeID() (uint64, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check for ID space exhaustion
	if gs.nextNodeID == ^uint64(0) { // MaxUint64
		return 0, fmt.Errorf("node ID space exhausted")
	}

	nodeID := gs.nextNodeID
	gs.nextNodeID++
	return nodeID, nil
}

// allocateEdgeID allocates a new edge ID in a thread-safe manner
// Returns error if ID space is exhausted
func (gs *GraphStorage) allocateEdgeID() (uint64, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check for ID space exhaustion
	if gs.nextEdgeID == ^uint64(0) { // MaxUint64
		return 0, fmt.Errorf("edge ID space exhausted")
	}

	edgeID := gs.nextEdgeID
	gs.nextEdgeID++
	return edgeID, nil
}

// removeFromLabelIndex removes a node ID from a label index
func (gs *GraphStorage) removeFromLabelIndex(label string, nodeID uint64) {
	nodeIDs := gs.nodesByLabel[label]
	for i, id := range nodeIDs {
		if id == nodeID {
			// Remove by swapping with last element and truncating
			nodeIDs[i] = nodeIDs[len(nodeIDs)-1]
			gs.nodesByLabel[label] = nodeIDs[:len(nodeIDs)-1]
			break
		}
	}
}

// Snapshot saves the current state to disk
func (gs *GraphStorage) Snapshot() error {
	gs.mu.RLock()

	// Get statistics atomically before creating snapshot
	stats := gs.GetStatistics()

	snapshot := struct {
		Nodes         map[uint64]*Node
		Edges         map[uint64]*Edge
		NodesByLabel  map[string][]uint64
		EdgesByType   map[string][]uint64
		OutgoingEdges map[uint64][]uint64
		IncomingEdges map[uint64][]uint64
		NextNodeID    uint64
		NextEdgeID    uint64
		Stats         Statistics
	}{
		Nodes:         gs.nodes,
		Edges:         gs.edges,
		NodesByLabel:  gs.nodesByLabel,
		EdgesByType:   gs.edgesByType,
		OutgoingEdges: gs.outgoingEdges,
		IncomingEdges: gs.incomingEdges,
		NextNodeID:    gs.nextNodeID,
		NextEdgeID:    gs.nextEdgeID,
		Stats:         stats,
	}

	gs.mu.RUnlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	snapshotPath := filepath.Join(gs.dataDir, "snapshot.json")
	tmpPath := snapshotPath + ".tmp"

	// Write to temporary file first
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, snapshotPath); err != nil {
		return fmt.Errorf("failed to rename snapshot: %w", err)
	}

	// Update LastSnapshot timestamp (safe to modify after releasing lock)
	gs.stats.LastSnapshot = time.Now()

	return nil
}

// loadFromDisk loads the graph from disk
func (gs *GraphStorage) loadFromDisk() error {
	snapshotPath := filepath.Join(gs.dataDir, "snapshot.json")

	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return err
	}

	var snapshot struct {
		Nodes         map[uint64]*Node
		Edges         map[uint64]*Edge
		NodesByLabel  map[string][]uint64
		EdgesByType   map[string][]uint64
		OutgoingEdges map[uint64][]uint64
		IncomingEdges map[uint64][]uint64
		NextNodeID    uint64
		NextEdgeID    uint64
		Stats         Statistics
	}

	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	gs.nodes = snapshot.Nodes
	gs.edges = snapshot.Edges
	gs.nodesByLabel = snapshot.NodesByLabel
	gs.edgesByType = snapshot.EdgesByType
	gs.outgoingEdges = snapshot.OutgoingEdges
	gs.incomingEdges = snapshot.IncomingEdges
	gs.nextNodeID = snapshot.NextNodeID
	gs.nextEdgeID = snapshot.NextEdgeID
	gs.stats = snapshot.Stats

	return nil
}

// replayWAL replays WAL entries to recover state
func (gs *GraphStorage) replayWAL() error {
	if gs.useBatching && gs.batchedWAL != nil {
		return gs.batchedWAL.Replay(func(entry *wal.Entry) error {
			return gs.replayEntry(entry)
		})
	} else if gs.wal != nil {
		return gs.wal.Replay(func(entry *wal.Entry) error {
			return gs.replayEntry(entry)
		})
	}
	return nil
}

// replayEntry replays a single WAL entry
func (gs *GraphStorage) replayEntry(entry *wal.Entry) error {
	switch entry.OpType {
	case wal.OpCreateNode:
		var node Node
		if err := json.Unmarshal(entry.Data, &node); err != nil {
			return err
		}

		// Skip if node already exists (already in snapshot)
		if _, exists := gs.nodes[node.ID]; exists {
			return nil
		}

		// Replay node creation
		gs.nodes[node.ID] = &node
		for _, label := range node.Labels {
			gs.nodesByLabel[label] = append(gs.nodesByLabel[label], node.ID)
		}
		if gs.outgoingEdges[node.ID] == nil {
			gs.outgoingEdges[node.ID] = make([]uint64, 0)
		}
		if gs.incomingEdges[node.ID] == nil {
			gs.incomingEdges[node.ID] = make([]uint64, 0)
		}

		// Update stats atomically
		atomic.AddUint64(&gs.stats.NodeCount, 1)

		// Update next ID if necessary
		if node.ID >= gs.nextNodeID {
			gs.nextNodeID = node.ID + 1
		}

	case wal.OpCreateEdge:
		var edge Edge
		if err := json.Unmarshal(entry.Data, &edge); err != nil {
			return err
		}

		// Skip if edge already exists (already in snapshot)
		if _, exists := gs.edges[edge.ID]; exists {
			return nil
		}

		// Replay edge creation
		gs.edges[edge.ID] = &edge
		gs.edgesByType[edge.Type] = append(gs.edgesByType[edge.Type], edge.ID)
		gs.outgoingEdges[edge.FromNodeID] = append(gs.outgoingEdges[edge.FromNodeID], edge.ID)
		gs.incomingEdges[edge.ToNodeID] = append(gs.incomingEdges[edge.ToNodeID], edge.ID)

		// Update stats atomically
		atomic.AddUint64(&gs.stats.EdgeCount, 1)

		// Update next ID if necessary
		if edge.ID >= gs.nextEdgeID {
			gs.nextEdgeID = edge.ID + 1
		}
	}

	return nil
}

// CreatePropertyIndex creates an index on a node property
func (gs *GraphStorage) CreatePropertyIndex(propertyKey string, valueType ValueType) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check if index already exists
	if _, exists := gs.propertyIndexes[propertyKey]; exists {
		return fmt.Errorf("index on property %s already exists", propertyKey)
	}

	// Create new index
	idx := NewPropertyIndex(propertyKey, valueType)

	// Populate index with existing nodes
	for nodeID, node := range gs.nodes {
		if prop, exists := node.Properties[propertyKey]; exists {
			if prop.Type == valueType {
				idx.Insert(nodeID, prop)
			}
		}
	}

	gs.propertyIndexes[propertyKey] = idx

	return nil
}

// DropPropertyIndex removes an index on a node property
func (gs *GraphStorage) DropPropertyIndex(propertyKey string) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if _, exists := gs.propertyIndexes[propertyKey]; !exists {
		return fmt.Errorf("index on property %s does not exist", propertyKey)
	}

	delete(gs.propertyIndexes, propertyKey)

	return nil
}

// FindNodesByPropertyIndexed uses an index to find nodes (O(1) lookup)
func (gs *GraphStorage) FindNodesByPropertyIndexed(key string, value Value) ([]*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Check if index exists
	idx, exists := gs.propertyIndexes[key]
	if !exists {
		return nil, fmt.Errorf("no index on property %s", key)
	}

	// Use index for O(1) lookup
	nodeIDs, err := idx.Lookup(value)
	if err != nil {
		return nil, err
	}

	// Fetch nodes
	nodes := make([]*Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if node, exists := gs.nodes[nodeID]; exists {
			nodes = append(nodes, node.Clone())
		}
	}

	return nodes, nil
}

// FindNodesByPropertyRange uses an index to find nodes in a range
func (gs *GraphStorage) FindNodesByPropertyRange(key string, start, end Value) ([]*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Check if index exists
	idx, exists := gs.propertyIndexes[key]
	if !exists {
		return nil, fmt.Errorf("no index on property %s", key)
	}

	// Use index for range lookup
	nodeIDs, err := idx.RangeLookup(start, end)
	if err != nil {
		return nil, err
	}

	// Fetch nodes
	nodes := make([]*Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if node, exists := gs.nodes[nodeID]; exists {
			nodes = append(nodes, node.Clone())
		}
	}

	return nodes, nil
}

// FindNodesByPropertyPrefix uses an index to find nodes by string prefix
func (gs *GraphStorage) FindNodesByPropertyPrefix(key string, prefix string) ([]*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Check if index exists
	idx, exists := gs.propertyIndexes[key]
	if !exists {
		return nil, fmt.Errorf("no index on property %s", key)
	}

	// Use index for prefix lookup
	nodeIDs, err := idx.PrefixLookup(prefix)
	if err != nil {
		return nil, err
	}

	// Fetch nodes
	nodes := make([]*Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if node, exists := gs.nodes[nodeID]; exists {
			nodes = append(nodes, node.Clone())
		}
	}

	return nodes, nil
}

// GetIndexStatistics returns statistics for all property indexes
func (gs *GraphStorage) GetIndexStatistics() map[string]IndexStatistics {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	stats := make(map[string]IndexStatistics)
	for key, idx := range gs.propertyIndexes {
		stats[key] = idx.GetStatistics()
	}

	return stats
}

// Close performs cleanup
func (gs *GraphStorage) Close() error {
	// Save snapshot on close
	if err := gs.Snapshot(); err != nil {
		return err
	}

	// Close WAL
	if gs.useBatching && gs.batchedWAL != nil {
		// Truncate WAL after successful snapshot
		if err := gs.batchedWAL.Truncate(); err != nil {
			return err
		}
		return gs.batchedWAL.Close()
	} else if gs.wal != nil {
		// Truncate WAL after successful snapshot
		if err := gs.wal.Truncate(); err != nil {
			return err
		}
		return gs.wal.Close()
	}

	return nil
}

// CompressEdgeLists compresses all uncompressed edge lists
// This can be called periodically to reduce memory usage
func (gs *GraphStorage) CompressEdgeLists() error {
	if !gs.useEdgeCompression {
		return fmt.Errorf("edge compression is not enabled")
	}

	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Compress outgoing edges
	for nodeID, edgeIDs := range gs.outgoingEdges {
		if len(edgeIDs) > 0 {
			gs.compressedOutgoing[nodeID] = NewCompressedEdgeList(edgeIDs)
		}
	}

	// Compress incoming edges
	for nodeID, edgeIDs := range gs.incomingEdges {
		if len(edgeIDs) > 0 {
			gs.compressedIncoming[nodeID] = NewCompressedEdgeList(edgeIDs)
		}
	}

	return nil
}

// GetCompressionStats returns compression statistics
func (gs *GraphStorage) GetCompressionStats() CompressionStats {
	if !gs.useEdgeCompression {
		return CompressionStats{}
	}

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	outgoingLists := make([]*CompressedEdgeList, 0, len(gs.compressedOutgoing))
	for _, list := range gs.compressedOutgoing {
		outgoingLists = append(outgoingLists, list)
	}

	incomingLists := make([]*CompressedEdgeList, 0, len(gs.compressedIncoming))
	for _, list := range gs.compressedIncoming {
		incomingLists = append(incomingLists, list)
	}

	allLists := append(outgoingLists, incomingLists...)
	return CalculateCompressionStats(allLists)
}

// NOTE: Parallel traversal methods (BFS, DFS, shortest path) are available
// via the parallel package to avoid circular dependencies:
//
//   import "github.com/darraghdowney/cluso-graphdb/pkg/parallel"
//
//   traverser := parallel.NewParallelTraverser(graph, numWorkers)
//   defer traverser.Close()
//   results := traverser.TraverseBFS(startNodes, maxDepth)
//
// See pkg/parallel/traverse.go for full API documentation.
