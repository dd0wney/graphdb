package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/dd0wney/graphdb/pkg/encryption"
	"github.com/dd0wney/graphdb/pkg/tenantid"
)

// PropertyIndexSnapshot is a serializable representation of a PropertyIndex
type PropertyIndexSnapshot struct {
	PropertyKey string
	IndexType   ValueType
	Index       map[string][]uint64
}

// Snapshot saves the current state to disk
func (gs *GraphStorage) Snapshot() error {
	_, err := gs.snapshotWithBoundary()
	return err
}

// snapshotWithBoundary saves the current state to disk and returns the WAL
// boundary LSN captured under the same gs.mu.RLock as the serialized state:
// every write visible in the snapshot has LSN ≤ boundary, every write that
// lands after has LSN > boundary. CompactWAL pairs this with
// TruncateUpTo(boundary) to checkpoint the WAL without losing concurrent
// writers' entries (M-1).
func (gs *GraphStorage) snapshotWithBoundary() (uint64, error) {
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

	// Boundary capture. The barrier write-lock waits out any
	// Transaction.Commit that applied its changes in-memory (visible to
	// this snapshot) but hasn't appended its WAL batch yet — those
	// appends happen after gs.mu is released, so the RLock alone doesn't
	// exclude them. New commits can't reach that window while we hold
	// gs.mu.RLock. Holding the barrier across the LSN read costs nothing
	// here (no commit can contend for it) and keeps the critical section
	// non-empty.
	gs.txWALBarrier.Lock()
	boundary := gs.walBoundaryLSNLocked()
	gs.txWALBarrier.Unlock()

	// Capture the engine under the same RLock as the state it encrypts:
	// SetEncryption writes this field under gs.mu.Lock, and the encrypt
	// call + envelope flag below must agree on one value.
	engine := gs.encryptionEngine

	// Get statistics atomically before creating snapshot
	stats := gs.GetStatistics()

	// Serialize property indexes. Index is deep-copied (not referenced):
	// see the isolation comment on the snapshot struct below.
	propertyIndexSnapshots := make(map[string]PropertyIndexSnapshot)
	for key, idx := range gs.propertyIndexes {
		idx.mu.RLock()
		propertyIndexSnapshots[key] = PropertyIndexSnapshot{
			PropertyKey: idx.propertyKey,
			IndexType:   idx.indexType,
			Index:       cloneStringIDIndex(idx.index),
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
		VectorIndexes   []VectorIndexDef
		NextNodeID      uint64
		NextEdgeID      uint64
		Stats           Statistics
	}{
		// ISOLATION: every field below must be a deep copy, never a
		// reference to a live structure. json.Marshal runs after
		// gs.mu.RUnlock (deliberately — writers shouldn't stall for the
		// full reflection encode), so a writer mutating a referenced
		// map/slice/node during the marshal is a data race and a
		// mapEncoder index-out-of-range panic. Latent while Snapshot ran
		// only at Close() (no writers); CompactWAL (M-1) snapshots under
		// live traffic, where it bites. flattenLabelIndex already builds
		// fresh maps+slices; nodes/edges are Clone()d (the flat maps were
		// already fresh but shared the pointers); adjacency and the
		// property indexes above are copied explicitly.
		Nodes:           gs.cloneNodesForSnapshotLocked(),
		Edges:           gs.cloneEdgesForSnapshotLocked(),
		NodesByLabel:    flattenLabelIndex(gs.nodesByLabel),
		EdgesByType:     flattenLabelIndex(gs.edgesByType),
		OutgoingEdges:   cloneAdjacency(gs.outgoingEdges),
		IncomingEdges:   cloneAdjacency(gs.incomingEdges),
		PropertyIndexes: propertyIndexSnapshots,
		// Vector index DEFINITIONS only — the HNSW graph is not serialized; it
		// is rebuilt from the node set after WAL replay (rebuildVectorIndexes-
		// FromNodes). Additive field: snapshots written before this stay
		// readable (absent field -> no indexes recreated -> the prior
		// vectors-lost-on-restart behaviour, unchanged for old files).
		VectorIndexes: gs.vectorIndex.IndexDefinitions(),
		// Atomic loads: Transaction ops allocate IDs via atomic.AddUint64
		// WITHOUT gs.mu, so a plain read here races them (a high-water
		// counter that runs slightly ahead of visible state is fine —
		// recovery just starts IDs past it).
		NextNodeID: atomic.LoadUint64(&gs.nextNodeID),
		NextEdgeID: atomic.LoadUint64(&gs.nextEdgeID),
		Stats:      stats,
	}

	gs.mu.RUnlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Encrypt data if encryption is enabled
	if engine != nil {
		encrypted, err := engine.Encrypt(data)
		if err != nil {
			return 0, fmt.Errorf("failed to encrypt snapshot: %w", err)
		}
		data = encrypted
	}

	// M-14: versioned magic-header envelope; the encrypted flag replaces
	// the first-byte plaintext-vs-ciphertext heuristic on load.
	data = encodeSnapshotEnvelope(data, engine != nil)

	snapshotPath := filepath.Join(gs.dataDir, "snapshot.json")
	tmpPath := snapshotPath + ".tmp"

	// Write to temporary file first
	if err := os.WriteFile(tmpPath, data, filePermissions); err != nil {
		return 0, fmt.Errorf("failed to write snapshot: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, snapshotPath); err != nil {
		return 0, fmt.Errorf("failed to rename snapshot: %w", err)
	}

	// Update LastSnapshot timestamp (safe to modify after releasing lock)
	gs.stats.LastSnapshot = time.Now()

	return boundary, nil
}

// loadFromDisk loads the graph from disk
func (gs *GraphStorage) loadFromDisk() error {
	snapshotPath := filepath.Join(gs.dataDir, "snapshot.json")

	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return err
	}

	payload, isEncrypted, legacy, err := decodeSnapshotEnvelope(data)
	if err != nil {
		return err
	}
	data = payload

	if legacy {
		// Pre-M-14 headerless snapshot: no envelope flag to consult. The old
		// first-byte heuristic ("ciphertext isn't '{' or '['") mis-fired ~0.8%
		// of the time, because the stored payload begins with a random GCM
		// nonce that can collide with a JSON opener — a CI flake and a real
		// load-failure risk on genuine legacy encrypted snapshots. Instead,
		// let AES-GCM's authentication tag decide: a successful authenticated
		// decrypt means it was ciphertext; an auth failure means it was always
		// plaintext. This branch ages out as legacy files are rewritten with
		// the envelope.
		if gs.encryptionEngine != nil {
			if decrypted, derr := gs.encryptionEngine.Decrypt(data); derr == nil {
				data = decrypted
			}
			// derr != nil → not our ciphertext → genuine legacy plaintext; keep data.
		}
	} else if isEncrypted {
		// New-format envelope carries an explicit encrypted flag.
		if gs.encryptionEngine == nil {
			return fmt.Errorf("snapshot is encrypted but encryption is not enabled (set ENCRYPTION_ENABLED=true)")
		}
		decrypted, err := gs.encryptionEngine.Decrypt(data)
		if err != nil {
			return fmt.Errorf("failed to decrypt snapshot: %w", err)
		}
		data = decrypted
	}

	var snapshot struct {
		Nodes           map[uint64]*Node
		Edges           map[uint64]*Edge
		NodesByLabel    map[string][]uint64
		EdgesByType     map[string][]uint64
		OutgoingEdges   map[uint64][]uint64
		IncomingEdges   map[uint64][]uint64
		PropertyIndexes map[string]PropertyIndexSnapshot
		VectorIndexes   []VectorIndexDef
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

	// Recreate the vector index DEFINITIONS (empty HNSW graphs). The vectors
	// themselves are inserted after WAL replay, over the final node set, by
	// rebuildVectorIndexesFromNodes — so post-snapshot writes recovered from the
	// WAL are indexed too. Skip a definition whose index already exists (it can
	// when reopening into a process that pre-created indexes).
	for _, def := range snapshot.VectorIndexes {
		if gs.vectorIndex.HasIndexForTenant(tenantid.TenantID(def.TenantID), def.PropertyName) {
			continue
		}
		if err := gs.vectorIndex.CreateIndexForTenant(
			tenantid.TenantID(def.TenantID), def.PropertyName,
			def.Dimensions, def.M, def.EfConstruction, def.Metric,
		); err != nil {
			return fmt.Errorf("failed to recreate vector index %s/%s: %w", def.TenantID, def.PropertyName, err)
		}
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
