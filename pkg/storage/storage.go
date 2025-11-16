package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
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

	// Disk-backed edge storage (Milestone 2)
	edgeStore          *EdgeStore // LSM-backed edge storage with LRU cache
	useDiskBackedEdges bool       // If true, use EdgeStore instead of in-memory maps

	// ID generators
	nextNodeID uint64
	nextEdgeID uint64

	// Concurrency control
	mu sync.RWMutex // Global lock for operations spanning multiple shards
	shardLocks [256]*sync.RWMutex // Shard-specific locks for fine-grained concurrency
	shardMask uint64 // Mask for efficient shard calculation (255 for 256 shards)

	// Persistence
	dataDir        string
	wal            *wal.WAL
	batchedWAL     *wal.BatchedWAL
	compressedWAL  *wal.CompressedWAL
	useBatching    bool
	useCompression bool

	// Statistics (using atomic operations for thread-safety)
	stats Statistics
	// Internal field for atomic float64 operations on AvgQueryTime
	avgQueryTimeBits uint64 // Stores AvgQueryTime as bits for atomic access
}

// StorageConfig holds configuration for GraphStorage
type StorageConfig struct {
	DataDir               string
	EnableBatching        bool
	EnableCompression     bool
	EnableEdgeCompression bool
	BatchSize             int
	FlushInterval         time.Duration
	UseDiskBackedEdges    bool // Enable disk-backed adjacency lists (Milestone 2)
	EdgeCacheSize         int  // LRU cache size for hot edge lists (default: 10000)
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
		EnableEdgeCompression: true, // Enabled by default for 5.08x memory savings
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
		shardMask:          255, // 256 shards - 1 for bitwise AND
		dataDir:            config.DataDir,
		useBatching:        config.EnableBatching,
		useCompression:     config.EnableCompression,
		nextNodeID:         1,
		nextEdgeID:         1,
	}

	// Initialize shard locks for fine-grained concurrency
	for i := range gs.shardLocks {
		gs.shardLocks[i] = &sync.RWMutex{}
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

	// Initialize disk-backed edge storage if enabled (Milestone 2)
	if config.UseDiskBackedEdges {
		cacheSize := config.EdgeCacheSize
		if cacheSize == 0 {
			cacheSize = 10000 // Default cache size
		}

		edgeStoreDir := filepath.Join(config.DataDir, "edgestore")
		edgeStore, err := NewEdgeStore(edgeStoreDir, cacheSize)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize EdgeStore: %w", err)
		}
		gs.edgeStore = edgeStore
		gs.useDiskBackedEdges = true
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

// Helper functions for shard-based locking

// getShardIndex returns the shard index for a given ID
func (gs *GraphStorage) getShardIndex(id uint64) int {
	return int(id & gs.shardMask)
}

// lockShard acquires a write lock on the shard for the given ID
func (gs *GraphStorage) lockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].Lock()
}

// unlockShard releases a write lock on the shard for the given ID
func (gs *GraphStorage) unlockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].Unlock()
}

// rlockShard acquires a read lock on the shard for the given ID
func (gs *GraphStorage) rlockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].RLock()
}

// runlockShard releases a read lock on the shard for the given ID
func (gs *GraphStorage) runlockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].RUnlock()
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
	start := time.Now()
	defer func() {
		gs.trackQueryTime(time.Since(start))
	}()

	// Use global read lock to properly synchronize with CreateNode's write lock
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

	// Write to WAL for durability
	updateData, err := json.Marshal(struct {
		NodeID     uint64
		Properties map[string]Value
	}{
		NodeID:     nodeID,
		Properties: properties,
	})
	if err == nil {
		if gs.useBatching && gs.batchedWAL != nil {
			gs.batchedWAL.Append(wal.OpUpdateNode, updateData)
		} else if gs.wal != nil {
			gs.wal.Append(wal.OpUpdateNode, updateData)
		}
	}

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

	// Get edges to delete (disk-backed or in-memory)
	var outgoingEdgeIDs, incomingEdgeIDs []uint64
	if gs.useDiskBackedEdges {
		outgoingEdgeIDs, _ = gs.edgeStore.GetOutgoingEdges(nodeID)
		incomingEdgeIDs, _ = gs.edgeStore.GetIncomingEdges(nodeID)
	} else {
		outgoingEdgeIDs = gs.outgoingEdges[nodeID]
		incomingEdgeIDs = gs.incomingEdges[nodeID]
	}

	// Cascade delete all outgoing edges
	for _, edgeID := range outgoingEdgeIDs {
		if edge, exists := gs.edges[edgeID]; exists {
			// Remove from other node's incoming edges
			if gs.useDiskBackedEdges {
				incoming, _ := gs.edgeStore.GetIncomingEdges(edge.ToNodeID)
				newIncoming := make([]uint64, 0, len(incoming))
				for _, id := range incoming {
					if id != edgeID {
						newIncoming = append(newIncoming, id)
					}
				}
				gs.edgeStore.StoreIncomingEdges(edge.ToNodeID, newIncoming)
			} else {
				if incoming, ok := gs.incomingEdges[edge.ToNodeID]; ok {
					newIncoming := make([]uint64, 0, len(incoming))
					for _, id := range incoming {
						if id != edgeID {
							newIncoming = append(newIncoming, id)
						}
					}
					gs.incomingEdges[edge.ToNodeID] = newIncoming
				}
			}

			// Remove from type index
			if edgeList, ok := gs.edgesByType[edge.Type]; ok {
				newList := make([]uint64, 0, len(edgeList))
				for _, id := range edgeList {
					if id != edgeID {
						newList = append(newList, id)
					}
				}
				gs.edgesByType[edge.Type] = newList
			}

			// Delete edge object
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

	// Cascade delete all incoming edges
	for _, edgeID := range incomingEdgeIDs {
		if edge, exists := gs.edges[edgeID]; exists {
			// Remove from other node's outgoing edges
			if gs.useDiskBackedEdges {
				outgoing, _ := gs.edgeStore.GetOutgoingEdges(edge.FromNodeID)
				newOutgoing := make([]uint64, 0, len(outgoing))
				for _, id := range outgoing {
					if id != edgeID {
						newOutgoing = append(newOutgoing, id)
					}
				}
				gs.edgeStore.StoreOutgoingEdges(edge.FromNodeID, newOutgoing)
			} else {
				if outgoing, ok := gs.outgoingEdges[edge.FromNodeID]; ok {
					newOutgoing := make([]uint64, 0, len(outgoing))
					for _, id := range outgoing {
						if id != edgeID {
							newOutgoing = append(newOutgoing, id)
						}
					}
					gs.outgoingEdges[edge.FromNodeID] = newOutgoing
				}
			}

			// Remove from type index
			if edgeList, ok := gs.edgesByType[edge.Type]; ok {
				newList := make([]uint64, 0, len(edgeList))
				for _, id := range edgeList {
					if id != edgeID {
						newList = append(newList, id)
					}
				}
				gs.edgesByType[edge.Type] = newList
			}

			// Delete edge object
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

	// Delete adjacency lists (disk-backed or in-memory)
	if gs.useDiskBackedEdges {
		gs.edgeStore.StoreOutgoingEdges(nodeID, []uint64{})
		gs.edgeStore.StoreIncomingEdges(nodeID, []uint64{})
	} else {
		delete(gs.outgoingEdges, nodeID)
		delete(gs.incomingEdges, nodeID)
	}

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

	// Write to WAL for durability
	nodeData, err := json.Marshal(node)
	if err == nil {
		if gs.useBatching && gs.batchedWAL != nil {
			gs.batchedWAL.Append(wal.OpDeleteNode, nodeData)
		} else if gs.wal != nil {
			gs.wal.Append(wal.OpDeleteNode, nodeData)
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

	// Store edge adjacency (disk-backed or in-memory)
	if gs.useDiskBackedEdges {
		// Disk-backed: Store in EdgeStore
		// Get current edge lists
		outgoing, _ := gs.edgeStore.GetOutgoingEdges(fromID)
		incoming, _ := gs.edgeStore.GetIncomingEdges(toID)

		// Append new edge
		outgoing = append(outgoing, edgeID)
		incoming = append(incoming, edgeID)

		// Store back
		gs.edgeStore.StoreOutgoingEdges(fromID, outgoing)
		gs.edgeStore.StoreIncomingEdges(toID, incoming)
	} else {
		// In-memory: Store in maps
		gs.outgoingEdges[fromID] = append(gs.outgoingEdges[fromID], edgeID)
		gs.incomingEdges[toID] = append(gs.incomingEdges[toID], edgeID)
	}

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

// DeleteEdge deletes an edge by ID
func (gs *GraphStorage) DeleteEdge(edgeID uint64) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Get edge to find fromID and toID
	edge, exists := gs.edges[edgeID]
	if !exists {
		return fmt.Errorf("edge %d not found", edgeID)
	}

	fromID := edge.FromNodeID
	toID := edge.ToNodeID

	// Delete from edges map
	delete(gs.edges, edgeID)

	// Remove from type index
	if edgeList, exists := gs.edgesByType[edge.Type]; exists {
		newList := make([]uint64, 0, len(edgeList))
		for _, id := range edgeList {
			if id != edgeID {
				newList = append(newList, id)
			}
		}
		gs.edgesByType[edge.Type] = newList
	}

	// Remove from adjacency (disk-backed or in-memory)
	if gs.useDiskBackedEdges {
		// Disk-backed: Remove from EdgeStore
		outgoing, _ := gs.edgeStore.GetOutgoingEdges(fromID)
		incoming, _ := gs.edgeStore.GetIncomingEdges(toID)

		// Filter out deleted edge
		newOutgoing := make([]uint64, 0, len(outgoing))
		for _, id := range outgoing {
			if id != edgeID {
				newOutgoing = append(newOutgoing, id)
			}
		}

		newIncoming := make([]uint64, 0, len(incoming))
		for _, id := range incoming {
			if id != edgeID {
				newIncoming = append(newIncoming, id)
			}
		}

		// Store back
		gs.edgeStore.StoreOutgoingEdges(fromID, newOutgoing)
		gs.edgeStore.StoreIncomingEdges(toID, newIncoming)
	} else {
		// In-memory: Remove from maps
		if outgoing, exists := gs.outgoingEdges[fromID]; exists {
			newOutgoing := make([]uint64, 0, len(outgoing))
			for _, id := range outgoing {
				if id != edgeID {
					newOutgoing = append(newOutgoing, id)
				}
			}
			gs.outgoingEdges[fromID] = newOutgoing
		}

		if incoming, exists := gs.incomingEdges[toID]; exists {
			newIncoming := make([]uint64, 0, len(incoming))
			for _, id := range incoming {
				if id != edgeID {
					newIncoming = append(newIncoming, id)
				}
			}
			gs.incomingEdges[toID] = newIncoming
		}
	}

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

	// Write to WAL for durability
	edgeData, err := json.Marshal(edge)
	if err == nil {
		if gs.useBatching && gs.batchedWAL != nil {
			gs.batchedWAL.Append(wal.OpDeleteEdge, edgeData)
		} else if gs.wal != nil {
			gs.wal.Append(wal.OpDeleteEdge, edgeData)
		}
	}

	return nil
}

// GetEdge retrieves an edge by ID
func (gs *GraphStorage) GetEdge(edgeID uint64) (*Edge, error) {
	start := time.Now()
	defer func() {
		gs.trackQueryTime(time.Since(start))
	}()

	// Use shard-level read lock for better concurrency
	gs.rlockShard(edgeID)
	defer gs.runlockShard(edgeID)

	edge, exists := gs.edges[edgeID]
	if !exists {
		return nil, ErrEdgeNotFound
	}

	return edge.Clone(), nil
}

// GetOutgoingEdges gets all outgoing edges from a node
func (gs *GraphStorage) GetOutgoingEdges(nodeID uint64) ([]*Edge, error) {
	start := time.Now()
	defer func() {
		gs.trackQueryTime(time.Since(start))
	}()

	// Use shard-level read lock for better concurrency
	gs.rlockShard(nodeID)
	defer gs.runlockShard(nodeID)

	var edgeIDs []uint64

	// Check disk-backed storage first if enabled (Milestone 2)
	if gs.useDiskBackedEdges {
		diskEdges, err := gs.edgeStore.GetOutgoingEdges(nodeID)
		if err == nil {
			edgeIDs = diskEdges
		}
	} else {
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
	}

	// Access gs.edges with global read lock (edges map is shared across all shards)
	gs.mu.RLock()
	edges := make([]*Edge, 0, len(edgeIDs))
	for _, edgeID := range edgeIDs {
		if edge, exists := gs.edges[edgeID]; exists {
			edges = append(edges, edge.Clone())
		}
	}
	gs.mu.RUnlock()

	return edges, nil
}

// GetIncomingEdges gets all incoming edges to a node
func (gs *GraphStorage) GetIncomingEdges(nodeID uint64) ([]*Edge, error) {
	start := time.Now()
	defer func() {
		gs.trackQueryTime(time.Since(start))
	}()

	// Use shard-level read lock for better concurrency
	gs.rlockShard(nodeID)
	defer gs.runlockShard(nodeID)

	var edgeIDs []uint64

	// Check disk-backed storage first if enabled (Milestone 2)
	if gs.useDiskBackedEdges {
		diskEdges, err := gs.edgeStore.GetIncomingEdges(nodeID)
		if err == nil {
			edgeIDs = diskEdges
		}
	} else {
		// Check compressed storage first if compression is enabled
		if gs.useEdgeCompression {
			if compressed, exists := gs.compressedIncoming[nodeID]; exists {
				edgeIDs = compressed.Decompress()
			}
		}

		// Fall back to uncompressed storage
		if edgeIDs == nil {
			if uncompressed, exists := gs.incomingEdges[nodeID]; exists {
				edgeIDs = uncompressed
			} else {
				return []*Edge{}, nil
			}
		}
	}

	// Access gs.edges with global read lock (edges map is shared across all shards)
	gs.mu.RLock()
	edges := make([]*Edge, 0, len(edgeIDs))
	for _, edgeID := range edgeIDs {
		if edge, exists := gs.edges[edgeID]; exists {
			edges = append(edges, edge.Clone())
		}
	}
	gs.mu.RUnlock()

	return edges, nil
}

// FindNodesByLabel finds all nodes with a specific label
func (gs *GraphStorage) FindNodesByLabel(label string) ([]*Node, error) {
	start := time.Now()
	defer func() {
		gs.trackQueryTime(time.Since(start))
	}()

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
	start := time.Now()
	defer func() {
		gs.trackQueryTime(time.Since(start))
	}()

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
	start := time.Now()
	defer func() {
		gs.trackQueryTime(time.Since(start))
	}()

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
		LastSnapshot: gs.stats.LastSnapshot,
		AvgQueryTime: math.Float64frombits(atomic.LoadUint64(&gs.avgQueryTimeBits)),
	}
}

// trackQueryTime records query execution time for statistics
// Uses exponential moving average with atomic operations for thread-safety
func (gs *GraphStorage) trackQueryTime(duration time.Duration) {
	atomic.AddUint64(&gs.stats.TotalQueries, 1)

	// Update average query time (milliseconds)
	// Using exponential moving average: new_avg = 0.9 * old_avg + 0.1 * new_value
	durationMs := float64(duration.Nanoseconds()) / 1000000.0

	// Thread-safe update using compare-and-swap loop
	for {
		oldBits := atomic.LoadUint64(&gs.avgQueryTimeBits)
		oldAvg := math.Float64frombits(oldBits)
		newAvg := 0.9*oldAvg + 0.1*durationMs
		newBits := math.Float64bits(newAvg)

		if atomic.CompareAndSwapUint64(&gs.avgQueryTimeBits, oldBits, newBits) {
			break
		}
		// CAS failed, retry with new value
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
// PropertyIndexSnapshot is a serializable representation of a PropertyIndex
type PropertyIndexSnapshot struct {
	PropertyKey string
	IndexType   ValueType
	Index       map[string][]uint64
}

func (gs *GraphStorage) Snapshot() error {
	// Compress edge lists before snapshot if compression is enabled
	if gs.useEdgeCompression {
		gs.mu.Lock()
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
		// Clear uncompressed maps to free memory
		gs.outgoingEdges = make(map[uint64][]uint64)
		gs.incomingEdges = make(map[uint64][]uint64)
		gs.mu.Unlock()
	}

	gs.mu.RLock()

	// Get statistics atomically before creating snapshot
	stats := gs.GetStatistics()

	// Serialize property indexes
	propertyIndexSnapshots := make(map[string]PropertyIndexSnapshot)
	for key, idx := range gs.propertyIndexes {
		idx.mu.RLock()
		propertyIndexSnapshots[key] = PropertyIndexSnapshot{
			PropertyKey: idx.propertyKey,
			IndexType:   idx.indexType,
			Index:       idx.index,
		}
		idx.mu.RUnlock()
	}

	snapshot := struct {
		Nodes          map[uint64]*Node
		Edges          map[uint64]*Edge
		NodesByLabel   map[string][]uint64
		EdgesByType    map[string][]uint64
		OutgoingEdges  map[uint64][]uint64
		IncomingEdges  map[uint64][]uint64
		PropertyIndexes map[string]PropertyIndexSnapshot
		NextNodeID     uint64
		NextEdgeID     uint64
		Stats          Statistics
	}{
		Nodes:           gs.nodes,
		Edges:           gs.edges,
		NodesByLabel:    gs.nodesByLabel,
		EdgesByType:     gs.edgesByType,
		OutgoingEdges:   gs.outgoingEdges,
		IncomingEdges:   gs.incomingEdges,
		PropertyIndexes: propertyIndexSnapshots,
		NextNodeID:      gs.nextNodeID,
		NextEdgeID:      gs.nextEdgeID,
		Stats:           stats,
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
		Nodes          map[uint64]*Node
		Edges          map[uint64]*Edge
		NodesByLabel   map[string][]uint64
		EdgesByType    map[string][]uint64
		OutgoingEdges  map[uint64][]uint64
		IncomingEdges  map[uint64][]uint64
		PropertyIndexes map[string]PropertyIndexSnapshot
		NextNodeID     uint64
		NextEdgeID     uint64
		Stats          Statistics
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
	// Restore avgQueryTimeBits from AvgQueryTime (needed for atomic operations)
	atomic.StoreUint64(&gs.avgQueryTimeBits, math.Float64bits(snapshot.Stats.AvgQueryTime))

	// Deserialize property indexes
	gs.propertyIndexes = make(map[string]*PropertyIndex)
	for key, idxSnapshot := range snapshot.PropertyIndexes {
		idx := &PropertyIndex{
			propertyKey: idxSnapshot.PropertyKey,
			indexType:   idxSnapshot.IndexType,
			index:       idxSnapshot.Index,
		}
		gs.propertyIndexes[key] = idx
	}

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

		// Insert into property indexes if they exist
		for key, value := range node.Properties {
			if idx, exists := gs.propertyIndexes[key]; exists {
				if value.Type == idx.indexType {
					idx.Insert(node.ID, value)
				}
			}
		}

		// Update stats atomically
		atomic.AddUint64(&gs.stats.NodeCount, 1)

		// Update next ID if necessary
		if node.ID >= gs.nextNodeID {
			gs.nextNodeID = node.ID + 1
		}

	case wal.OpUpdateNode:
		var updateInfo struct {
			NodeID     uint64
			Properties map[string]Value
		}
		if err := json.Unmarshal(entry.Data, &updateInfo); err != nil {
			return err
		}

		// Skip if node doesn't exist
		node, exists := gs.nodes[updateInfo.NodeID]
		if !exists {
			return nil
		}

		// Update property indexes - remove old values, add new values
		for key, newValue := range updateInfo.Properties {
			if idx, exists := gs.propertyIndexes[key]; exists {
				// Remove old value from index if it exists
				if oldValue, exists := node.Properties[key]; exists {
					idx.Remove(updateInfo.NodeID, oldValue)
				}
				// Add new value to index
				if newValue.Type == idx.indexType {
					idx.Insert(updateInfo.NodeID, newValue)
				}
			}
		}

		// Apply property updates
		for key, value := range updateInfo.Properties {
			node.Properties[key] = value
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

		// Rebuild adjacency lists (disk-backed or in-memory)
		if gs.useDiskBackedEdges {
			// Disk-backed: Rebuild EdgeStore adjacency lists from WAL
			outgoing, _ := gs.edgeStore.GetOutgoingEdges(edge.FromNodeID)
			incoming, _ := gs.edgeStore.GetIncomingEdges(edge.ToNodeID)

			outgoing = append(outgoing, edge.ID)
			incoming = append(incoming, edge.ID)

			gs.edgeStore.StoreOutgoingEdges(edge.FromNodeID, outgoing)
			gs.edgeStore.StoreIncomingEdges(edge.ToNodeID, incoming)
		} else {
			// In-memory: Update maps directly
			gs.outgoingEdges[edge.FromNodeID] = append(gs.outgoingEdges[edge.FromNodeID], edge.ID)
			gs.incomingEdges[edge.ToNodeID] = append(gs.incomingEdges[edge.ToNodeID], edge.ID)
		}

		// Update stats atomically
		atomic.AddUint64(&gs.stats.EdgeCount, 1)

		// Update next ID if necessary
		if edge.ID >= gs.nextEdgeID {
			gs.nextEdgeID = edge.ID + 1
		}

	case wal.OpDeleteEdge:
		var edge Edge
		if err := json.Unmarshal(entry.Data, &edge); err != nil {
			return err
		}

		// Skip if edge doesn't exist (already deleted or never existed)
		if _, exists := gs.edges[edge.ID]; !exists {
			return nil
		}

		// Replay edge deletion
		delete(gs.edges, edge.ID)

		// Remove from type index
		if edgeList, exists := gs.edgesByType[edge.Type]; exists {
			newList := make([]uint64, 0, len(edgeList))
			for _, id := range edgeList {
				if id != edge.ID {
					newList = append(newList, id)
				}
			}
			gs.edgesByType[edge.Type] = newList
		}

		// Remove from adjacency lists (disk-backed or in-memory)
		if gs.useDiskBackedEdges {
			// Disk-backed: Remove from EdgeStore
			outgoing, _ := gs.edgeStore.GetOutgoingEdges(edge.FromNodeID)
			incoming, _ := gs.edgeStore.GetIncomingEdges(edge.ToNodeID)

			// Filter out deleted edge
			newOutgoing := make([]uint64, 0, len(outgoing))
			for _, id := range outgoing {
				if id != edge.ID {
					newOutgoing = append(newOutgoing, id)
				}
			}

			newIncoming := make([]uint64, 0, len(incoming))
			for _, id := range incoming {
				if id != edge.ID {
					newIncoming = append(newIncoming, id)
				}
			}

			// Store back
			gs.edgeStore.StoreOutgoingEdges(edge.FromNodeID, newOutgoing)
			gs.edgeStore.StoreIncomingEdges(edge.ToNodeID, newIncoming)
		} else {
			// In-memory: Remove from maps
			if outgoing, exists := gs.outgoingEdges[edge.FromNodeID]; exists {
				newOutgoing := make([]uint64, 0, len(outgoing))
				for _, id := range outgoing {
					if id != edge.ID {
						newOutgoing = append(newOutgoing, id)
					}
				}
				gs.outgoingEdges[edge.FromNodeID] = newOutgoing
			}

			if incoming, exists := gs.incomingEdges[edge.ToNodeID]; exists {
				newIncoming := make([]uint64, 0, len(incoming))
				for _, id := range incoming {
					if id != edge.ID {
						newIncoming = append(newIncoming, id)
					}
				}
				gs.incomingEdges[edge.ToNodeID] = newIncoming
			}
		}

		// Decrement stats with underflow protection
		for {
			current := atomic.LoadUint64(&gs.stats.EdgeCount)
			if current == 0 {
				break
			}
			if atomic.CompareAndSwapUint64(&gs.stats.EdgeCount, current, current-1) {
				break
			}
		}

	case wal.OpDeleteNode:
		var node Node
		if err := json.Unmarshal(entry.Data, &node); err != nil {
			return err
		}

		// Skip if node doesn't exist (already deleted or never existed)
		if _, exists := gs.nodes[node.ID]; !exists {
			return nil
		}

		// Get edges to delete (disk-backed or in-memory)
		var outgoingEdgeIDs, incomingEdgeIDs []uint64
		if gs.useDiskBackedEdges {
			outgoingEdgeIDs, _ = gs.edgeStore.GetOutgoingEdges(node.ID)
			incomingEdgeIDs, _ = gs.edgeStore.GetIncomingEdges(node.ID)
		} else {
			outgoingEdgeIDs = gs.outgoingEdges[node.ID]
			incomingEdgeIDs = gs.incomingEdges[node.ID]
		}

		// Cascade delete all outgoing edges during replay
		for _, edgeID := range outgoingEdgeIDs {
			if edge, exists := gs.edges[edgeID]; exists {
				// Remove from other node's incoming edges
				if gs.useDiskBackedEdges {
					incoming, _ := gs.edgeStore.GetIncomingEdges(edge.ToNodeID)
					newIncoming := make([]uint64, 0, len(incoming))
					for _, id := range incoming {
						if id != edgeID {
							newIncoming = append(newIncoming, id)
						}
					}
					gs.edgeStore.StoreIncomingEdges(edge.ToNodeID, newIncoming)
				} else {
					if incoming, ok := gs.incomingEdges[edge.ToNodeID]; ok {
						newIncoming := make([]uint64, 0, len(incoming))
						for _, id := range incoming {
							if id != edgeID {
								newIncoming = append(newIncoming, id)
							}
						}
						gs.incomingEdges[edge.ToNodeID] = newIncoming
					}
				}

				// Remove from type index
				if edgeList, ok := gs.edgesByType[edge.Type]; ok {
					newList := make([]uint64, 0, len(edgeList))
					for _, id := range edgeList {
						if id != edgeID {
							newList = append(newList, id)
						}
					}
					gs.edgesByType[edge.Type] = newList
				}

				// Delete edge object
				delete(gs.edges, edgeID)

				// Decrement stats with underflow protection
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

		// Cascade delete all incoming edges during replay
		for _, edgeID := range incomingEdgeIDs {
			if edge, exists := gs.edges[edgeID]; exists {
				// Remove from other node's outgoing edges
				if gs.useDiskBackedEdges {
					outgoing, _ := gs.edgeStore.GetOutgoingEdges(edge.FromNodeID)
					newOutgoing := make([]uint64, 0, len(outgoing))
					for _, id := range outgoing {
						if id != edgeID {
							newOutgoing = append(newOutgoing, id)
						}
					}
					gs.edgeStore.StoreOutgoingEdges(edge.FromNodeID, newOutgoing)
				} else {
					if outgoing, ok := gs.outgoingEdges[edge.FromNodeID]; ok {
						newOutgoing := make([]uint64, 0, len(outgoing))
						for _, id := range outgoing {
							if id != edgeID {
								newOutgoing = append(newOutgoing, id)
							}
						}
						gs.outgoingEdges[edge.FromNodeID] = newOutgoing
					}
				}

				// Remove from type index
				if edgeList, ok := gs.edgesByType[edge.Type]; ok {
					newList := make([]uint64, 0, len(edgeList))
					for _, id := range edgeList {
						if id != edgeID {
							newList = append(newList, id)
						}
					}
					gs.edgesByType[edge.Type] = newList
				}

				// Delete edge object
				delete(gs.edges, edgeID)

				// Decrement stats with underflow protection
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
			gs.removeFromLabelIndex(label, node.ID)
		}

		// Remove from property indexes
		for key, value := range node.Properties {
			if idx, exists := gs.propertyIndexes[key]; exists {
				idx.Remove(node.ID, value)
			}
		}

		// Delete node
		delete(gs.nodes, node.ID)

		// Delete adjacency lists (disk-backed or in-memory)
		if gs.useDiskBackedEdges {
			gs.edgeStore.StoreOutgoingEdges(node.ID, []uint64{})
			gs.edgeStore.StoreIncomingEdges(node.ID, []uint64{})
		} else {
			delete(gs.outgoingEdges, node.ID)
			delete(gs.incomingEdges, node.ID)
		}

		// Decrement stats with underflow protection
		for {
			current := atomic.LoadUint64(&gs.stats.NodeCount)
			if current == 0 {
				break
			}
			if atomic.CompareAndSwapUint64(&gs.stats.NodeCount, current, current-1) {
				break
			}
		}

	case wal.OpCreatePropertyIndex:
		var indexInfo struct {
			PropertyKey string
			ValueType   ValueType
		}
		if err := json.Unmarshal(entry.Data, &indexInfo); err != nil {
			return err
		}

		// Skip if index already exists
		if _, exists := gs.propertyIndexes[indexInfo.PropertyKey]; exists {
			return nil
		}

		// Create index and populate with existing nodes
		idx := NewPropertyIndex(indexInfo.PropertyKey, indexInfo.ValueType)
		for nodeID, node := range gs.nodes {
			if prop, exists := node.Properties[indexInfo.PropertyKey]; exists {
				if prop.Type == indexInfo.ValueType {
					idx.Insert(nodeID, prop)
				}
			}
		}
		gs.propertyIndexes[indexInfo.PropertyKey] = idx

	case wal.OpDropPropertyIndex:
		var indexInfo struct {
			PropertyKey string
		}
		if err := json.Unmarshal(entry.Data, &indexInfo); err != nil {
			return err
		}

		// Remove index
		delete(gs.propertyIndexes, indexInfo.PropertyKey)
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

	// Write to WAL for durability
	indexData, err := json.Marshal(struct {
		PropertyKey string
		ValueType   ValueType
	}{
		PropertyKey: propertyKey,
		ValueType:   valueType,
	})
	if err == nil {
		if gs.useBatching && gs.batchedWAL != nil {
			gs.batchedWAL.Append(wal.OpCreatePropertyIndex, indexData)
		} else if gs.wal != nil {
			gs.wal.Append(wal.OpCreatePropertyIndex, indexData)
		}
	}

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

	// Write to WAL for durability
	indexData, err := json.Marshal(struct {
		PropertyKey string
	}{
		PropertyKey: propertyKey,
	})
	if err == nil {
		if gs.useBatching && gs.batchedWAL != nil {
			gs.batchedWAL.Append(wal.OpDropPropertyIndex, indexData)
		} else if gs.wal != nil {
			gs.wal.Append(wal.OpDropPropertyIndex, indexData)
		}
	}

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

	// Close EdgeStore if enabled
	if gs.useDiskBackedEdges && gs.edgeStore != nil {
		if err := gs.edgeStore.Close(); err != nil {
			return fmt.Errorf("failed to close EdgeStore: %w", err)
		}
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

// GetCurrentLSN returns the current LSN (Log Sequence Number) from the WAL
// This is used by replication to track the latest position in the write-ahead log
func (gs *GraphStorage) GetCurrentLSN() uint64 {
	if gs.useCompression && gs.compressedWAL != nil {
		return gs.compressedWAL.GetCurrentLSN()
	} else if gs.useBatching && gs.batchedWAL != nil {
		return gs.batchedWAL.GetCurrentLSN()
	} else if gs.wal != nil {
		return gs.wal.GetCurrentLSN()
	}
	return 0
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
//   import "github.com/dd0wney/cluso-graphdb/pkg/parallel"
//
//   traverser := parallel.NewParallelTraverser(graph, numWorkers)
//   defer traverser.Close()
//   results := traverser.TraverseBFS(startNodes, maxDepth)
//
// See pkg/parallel/traverse.go for full API documentation.
