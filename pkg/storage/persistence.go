package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
)

// PropertyIndexSnapshot is a serializable representation of a PropertyIndex
type PropertyIndexSnapshot struct {
	PropertyKey string
	IndexType   ValueType
	Index       map[string][]uint64
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
		Nodes           map[uint64]*Node
		Edges           map[uint64]*Edge
		NodesByLabel    map[string][]uint64
		EdgesByType     map[string][]uint64
		OutgoingEdges   map[uint64][]uint64
		IncomingEdges   map[uint64][]uint64
		PropertyIndexes map[string]PropertyIndexSnapshot
		NextNodeID      uint64
		NextEdgeID      uint64
		Stats           Statistics
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
		encrypted, err := gs.encryptionEngine.Encrypt(data)
		if err != nil {
			return fmt.Errorf("failed to encrypt snapshot: %w", err)
		}
		data = encrypted
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
		decrypted, err := gs.encryptionEngine.Decrypt(data)
		if err != nil {
			return fmt.Errorf("failed to decrypt snapshot: %w", err)
		}
		data = decrypted
	} else if isEncrypted && gs.encryptionEngine == nil {
		// Data is encrypted but no decryption engine available
		return fmt.Errorf("snapshot is encrypted but encryption is not enabled (set ENCRYPTION_ENABLED=true)")
	}

	var snapshot struct {
		Nodes           map[uint64]*Node
		Edges           map[uint64]*Edge
		NodesByLabel    map[string][]uint64
		EdgesByType     map[string][]uint64
		OutgoingEdges   map[uint64][]uint64
		IncomingEdges   map[uint64][]uint64
		PropertyIndexes map[string]PropertyIndexSnapshot
		NextNodeID      uint64
		NextEdgeID      uint64
		Stats           Statistics
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

// SetEncryption sets the encryption engine and key manager for the storage.
// Uses typed interfaces for compile-time safety.
func (gs *GraphStorage) SetEncryption(engine encryption.EncryptDecrypter, keyManager encryption.KeyProvider) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.encryptionEngine = engine
	gs.keyManager = keyManager
}

// Close performs cleanup
func (gs *GraphStorage) Close() error {
	// Check and mark as closed atomically
	gs.mu.Lock()
	if gs.closed {
		gs.mu.Unlock()
		return fmt.Errorf("storage already closed")
	}
	gs.closed = true
	gs.mu.Unlock()

	// Save snapshot on close (without holding the lock to avoid deadlock)
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
