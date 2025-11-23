package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// PropertyIndexSnapshot is a serializable representation of a PropertyIndex
type PropertyIndexSnapshot struct {
	PropertyKey string
	IndexType   ValueType
	Index       map[string][]uint64
}

// writeToWAL writes an operation to the write-ahead log for durability
// Handles both batched and non-batched WAL writes
func (gs *GraphStorage) writeToWAL(operation wal.OpType, data interface{}) {
	encoded, err := json.Marshal(data)
	if err == nil {
		if gs.useBatching && gs.batchedWAL != nil {
			gs.batchedWAL.Append(operation, encoded)
		} else if gs.wal != nil {
			gs.wal.Append(operation, encoded)
		}
	}
}

// Snapshot saves the current state to disk
func (gs *GraphStorage) Snapshot() error {
	// Compress edge lists before snapshot if compression is enabled
	if gs.useEdgeCompression {
		gs.mu.Lock()
		gs.compressAllEdgeLists()
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

	// Encrypt data if encryption is enabled
	if gs.encryptionEngine != nil {
		// Type assert to access Encrypt method
		// Using interface{} with runtime type assertion to avoid import cycle
		type encrypter interface {
			Encrypt([]byte) ([]byte, error)
		}
		if engine, ok := gs.encryptionEngine.(encrypter); ok {
			encrypted, err := engine.Encrypt(data)
			if err != nil {
				return fmt.Errorf("failed to encrypt snapshot: %w", err)
			}
			data = encrypted
		}
	}

	snapshotPath := filepath.Join(gs.dataDir, "snapshot.json")
	tmpPath := snapshotPath + ".tmp"

	// Write to temporary file first
	if err := os.WriteFile(tmpPath, data, filePermissions); err != nil {
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

	// Try to detect if data is encrypted by checking if it's valid JSON
	// Valid JSON starts with '{' or '[', encrypted data is binary
	isEncrypted := len(data) > 0 && data[0] != '{' && data[0] != '['

	// Decrypt data if it appears to be encrypted and we have an encryption engine
	if isEncrypted && gs.encryptionEngine != nil {
		// Type assert to access Decrypt method
		type decrypter interface {
			Decrypt([]byte) ([]byte, error)
		}
		if engine, ok := gs.encryptionEngine.(decrypter); ok {
			decrypted, err := engine.Decrypt(data)
			if err != nil {
				return fmt.Errorf("failed to decrypt snapshot: %w", err)
			}
			data = decrypted
		}
	} else if isEncrypted && gs.encryptionEngine == nil {
		// Data is encrypted but no decryption engine available
		return fmt.Errorf("snapshot is encrypted but encryption is not enabled (set ENCRYPTION_ENABLED=true)")
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
		gs.insertNodeIntoPropertyIndexes(node.ID, node.Properties)

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

		// Update property indexes
		gs.updatePropertyIndexes(updateInfo.NodeID, node, updateInfo.Properties)

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
		gs.storeOutgoingEdge(edge.FromNodeID, edge.ID)
		gs.storeIncomingEdge(edge.ToNodeID, edge.ID)

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
		gs.removeEdgeFromTypeIndex(edge.Type, edge.ID)

		// Remove from adjacency lists (disk-backed or in-memory)
		gs.removeOutgoingEdge(edge.FromNodeID, edge.ID)
		gs.removeIncomingEdge(edge.ToNodeID, edge.ID)

		// Decrement stats with underflow protection
		atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)

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
			gs.cascadeDeleteOutgoingEdge(edgeID)
		}

		// Cascade delete all incoming edges during replay
		for _, edgeID := range incomingEdgeIDs {
			gs.cascadeDeleteIncomingEdge(edgeID)
		}

		// Remove from label indexes
		for _, label := range node.Labels {
			gs.removeFromLabelIndex(label, node.ID)
		}

		// Remove from property indexes
		gs.removeNodeFromPropertyIndexes(node.ID, node.Properties)

		// Delete node
		delete(gs.nodes, node.ID)

		// Delete adjacency lists (disk-backed or in-memory)
		gs.clearNodeAdjacency(node.ID)

		// Decrement stats with underflow protection
		atomicDecrementWithUnderflowProtection(&gs.stats.NodeCount)

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

// SetEncryption sets the encryption engine and key manager for the storage
func (gs *GraphStorage) SetEncryption(engine, keyManager interface{}) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.encryptionEngine = engine
	gs.keyManager = keyManager
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
