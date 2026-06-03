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
		Nodes:           gs.flattenNodesForSnapshot(),
		Edges:           gs.flattenEdgesForSnapshot(),
		NodesByLabel:    flattenLabelIndex(gs.nodesByLabel),
		EdgesByType:     flattenLabelIndex(gs.edgesByType),
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

	gs.rebucketSnapshotNodes(snapshot.Nodes)
	gs.rebucketSnapshotEdges(snapshot.Edges)
	// The global label/type indexes are DERIVED indexes: their MEMBERSHIP is
	// rebuilt below from the authoritative flat node/edge set (mirroring how the
	// per-tenant indexes and edge adjacency are rebuilt — see below + the
	// rebuildEdgeAdjacencyFromSnapshot comment). This is what lets the in-memory
	// index be a set without a snapshot format bump: the serialized form stays
	// map[string][]uint64 for on-disk compatibility.
	//
	// We still seed the KEYS from the serialized index so a "sticky" label/type
	// — one whose last node/edge was deleted, leaving an empty bucket — stays
	// registered across restart (GetAllLabels and the GraphQL schema built from
	// it keep exposing it). Pre-Path-C this happened implicitly because the
	// empty []uint64 was serialized and restored; here membership comes from the
	// flat set and the empty buckets are reconstructed from the keys.
	gs.nodesByLabel = make(labelIndex)
	gs.edgesByType = make(labelIndex)
	for label := range snapshot.NodesByLabel {
		if gs.nodesByLabel[label] == nil {
			gs.nodesByLabel[label] = make(map[uint64]struct{})
		}
	}
	for edgeType := range snapshot.EdgesByType {
		if gs.edgesByType[edgeType] == nil {
			gs.edgesByType[edgeType] = make(map[uint64]struct{})
		}
	}
	// Edge adjacency is a DERIVED index, rebuilt from the authoritative flat
	// edge set — not restored from the serialized maps. The snapshot persists
	// only the plain gs.outgoingEdges/incomingEdges; with edge compression
	// enabled (the NewGraphStorage default), live adjacency migrates into the
	// compressed representation (compressedOutgoing/Incoming) which is NOT
	// serialized, leaving the plain maps empty at save time. A naive
	// `= snapshot.OutgoingEdges` restore therefore loses ALL adjacency on
	// reopen: edges load fine (GetEdge works) but GetOutgoingEdges/Incoming
	// return nothing, silently breaking every traversal after a restart.
	// Surfaced independently by two consumers (coi-screen path-finding +
	// Stór reference-survival). Rebuilding here is config-independent and
	// format-free (no snapshot schema bump). Disk-backed adjacency persists in
	// edgeStore on its own, so skip the rebuild there to avoid double-counting.
	gs.rebuildEdgeAdjacencyFromSnapshot(snapshot.Edges)
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

	// H4.3-followup: rebuild tenantNodesByLabel + tenantStats from the
	// snapshot's flat node set. The snapshot struct (line 135 above) only
	// persists the global nodesByLabel — without this loop, post-restart
	// per-tenant GraphQL queries 400 with `Cannot query field "tasks" on
	// type "Query"` until the tenant's next write reseeds the index.
	// Sibling fix to the WAL-replay fix in persistence_replay.go's
	// replayCreateNode (H4.3).
	for _, node := range snapshot.Nodes {
		for _, label := range node.Labels {
			addToLabelIndex(gs.nodesByLabel, label, node.ID)
		}
		gs.addNodeToTenantIndex(node)
	}

	// Edge sibling of the node rebuild above: the snapshot persists only
	// the global edgesByType, so without this loop tenantEdgesByType stays
	// empty after a clean restart and GetEdgesByTypeForTenant returns nil
	// for every loaded edge until the tenant's next write reseeds it. Same
	// defect H4.3 fixed on the node side; addEdgeToTenantIndex also rebuilds
	// per-tenant EdgeCount stats.
	for _, edge := range snapshot.Edges {
		addToLabelIndex(gs.edgesByType, edge.Type, edge.ID)
		gs.addEdgeToTenantIndex(edge)
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
	// Check and mark as closed atomically. CompareAndSwap returns false
	// if closed was already true, giving us the "already closed" branch
	// without holding gs.mu.
	if !gs.closed.CompareAndSwap(false, true) {
		return fmt.Errorf("storage already closed")
	}

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
