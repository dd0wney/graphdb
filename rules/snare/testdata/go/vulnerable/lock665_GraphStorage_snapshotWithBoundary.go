// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/persistence.go at b6fb6a4^, lines 48-207, function GraphStorage.snapshotWithBoundary.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Two sections are annotated: #665 fixed the gs.mu.Lock section, and #672 (d08c106) later fixed the gs.mu.RLock section.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// snapshotWithBoundary saves the current state to disk and returns the WAL
// boundary LSN captured under the same gs.mu.RLock as the serialized state:
// every write visible in the snapshot has LSN ≤ boundary, every write that
// lands after has LSN > boundary. CompactWAL pairs this with
// TruncateUpTo(boundary) to checkpoint the WAL without losing concurrent
// writers' entries (M-1).
//
// skipIfClean is Close's option: when the file on disk already holds the
// live state, return the boundary without writing. Snapshot and CompactWAL
// pass false — an explicit request to write is honoured, and its damage
// refusal (snapshotMmapLocked) stays a contract a caller can rely on.
func (gs *GraphStorage) snapshotWithBoundary(skipIfClean bool) (uint64, error) {
	// Decided before anything else runs: on a clean Close the edge
	// compression below was the whole remaining cost (1.1 s on the
	// 2.0M-node ICIJ store) for a file that was not going to be written.
	if skipIfClean {
		if boundary, clean := gs.snapshotAlreadyOnDisk(); clean {
			return boundary, nil
		}
	}

	// Compress edge lists before snapshot if compression is enabled — but
	// only on a JSON-backed store (gs.mmapSnap == nil). On an mmap-backed
	// store, gs.outgoingEdges/gs.incomingEdges hold the post-open OVERLAY
	// only; the base adjacency lives in the CSR run inside gs.mmapSnap and
	// is never copied into these maps. Compressing and clearing the overlay
	// there would leave a compressed entry that holds ONLY the overlay
	// edges for a node — and getEdgeIDsForNode checks the compressed maps
	// FIRST, returning that entry before it ever reaches the
	// overlay-plus-base union below, hiding the base edges entirely.
	// TestEdgeCompression_ComposesWithMmapOverlay names this defect. The
	// mmap base is already compact in its CSR form, so skipping compression
	// there costs only the (post-open-write-bounded) overlay.
	if gs.useEdgeCompression {
		// ruleid: graphdb-lock-call-without-defer
		gs.mu.Lock()
		if gs.mmapSnap == nil {
			gs.compressAllEdgeLists()
			// Clear uncompressed maps to free memory
			gs.outgoingEdges = make(map[uint64][]uint64)
			gs.incomingEdges = make(map[uint64][]uint64)
		}
		gs.mu.Unlock()
	}

	// ruleid: graphdb-lock-call-without-defer
	gs.mu.RLock()
	boundary := gs.walLSNBarrieredLocked()

	// mmap reopen mode: write snapshot.mmap (merged overlay ∪ base − tombstones)
	// instead of the JSON snapshot. snapshotMmapLocked releases gs.mu.RLock.
	if gs.useMmapSnapshot {
		return gs.snapshotMmapLocked(boundary)
	}

	// Capture the engine under the same RLock as the state it encrypts:
	// SetEncryption writes this field under gs.mu.Lock, and the encrypt
	// call + envelope flag below must agree on one value. The epoch is read
	// under the same lock so the publish can tell whether an invalidation
	// happened after this state was captured (json_snapshot_clean.go).
	engine := gs.encryptionEngine
	epoch := gs.jsonSyncEpoch

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
		// WALBoundaryLSN is additive: absent in a snapshot written before this
		// field existed, which decodes as 0 and means "the boundary is
		// unknown, skip nothing" — today's behaviour, unchanged for old files.
		WALBoundaryLSN uint64
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
		// The boundary this snapshot covers: every write visible above has a
		// WAL LSN ≤ boundary, so replay on the next open can skip WAL entries
		// at or below it instead of re-applying writes already reflected here.
		WALBoundaryLSN: boundary,
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

	if err := gs.publishJSONSnapshot(data, boundary, engine, epoch); err != nil {
		return 0, err
	}

	// Update LastSnapshot timestamp (safe to modify after releasing lock)
	gs.stats.LastSnapshot = time.Now()

	return boundary, nil
}
