// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/persistence.go at d08c106, lines 65-142, function GraphStorage.snapshotWithBoundary.
// Source: pkg/storage/persistence.go at d08c106, lines 173-247, function GraphStorage.captureJSONSnapshot.
// Source: pkg/storage/mmap_snapshot_persist.go at d08c106, lines 23-107, function GraphStorage.captureMmapSnapshot.
// Label: safe.
// Reason: The function after PR #672 (commit d08c106), with the helpers captureJSONSnapshot and captureMmapSnapshot. Each helper holds gs.mu.RLock with a deferred RUnlock.
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
// refusal (captureMmapSnapshot) stays a contract a caller can rely on.
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
		gs.compressEdgeListsForSnapshot()
	}

	// mmap reopen mode: write snapshot.mmap (merged overlay ∪ base − tombstones)
	// instead of the JSON snapshot. gs.useMmapSnapshot is set once, in the
	// constructor, so reading it here without gs.mu is safe.
	if gs.useMmapSnapshot {
		capture, err := gs.captureMmapSnapshot()
		if err != nil {
			return 0, err
		}
		return gs.writeMmapSnapshot(capture)
	}

	capture := gs.captureJSONSnapshot()
	snapshot, engine, epoch, boundary := capture.snapshot, capture.engine, capture.epoch, capture.boundary

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

// captureJSONSnapshot is the gs.mu.RLock section of the JSON path of
// snapshotWithBoundary. It is the only function that locks and unlocks gs.mu
// on this path, and the deferred unlock matters: the Cypher executor and
// net/http recover panics, so a panic here that left the read lock held would
// hang every later write and Close.
func (gs *GraphStorage) captureJSONSnapshot() jsonSnapshotCapture {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	panicPoint("captureJSONSnapshot")
	boundary := gs.walLSNBarrieredLocked()

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

	snapshot := jsonSnapshot{
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

	return jsonSnapshotCapture{snapshot: snapshot, engine: engine, epoch: epoch, boundary: boundary}
}

// captureMmapSnapshot is the gs.mu.RLock section of the mmap path of
// snapshotWithBoundary. It collects cloned live state under the lock and
// releases the lock before returning (so the file encode doesn't stall
// writers — matching the JSON path). It is the only function that locks and
// unlocks gs.mu on this path. The deferred unlock matters: decodeNodeRecordAt
// can panic on an out-of-range record, the Cypher executor and net/http
// recover panics, and a read lock left held hangs every later write and Close.
func (gs *GraphStorage) captureMmapSnapshot() (*mmapSnapshotCapture, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	panicPoint("captureMmapSnapshot")
	boundary := gs.walLSNBarrieredLocked()

	// Live nodes: shard overlay + non-shadowed, non-tombstoned base. The shard
	// nodes are cloned for isolation (the encode runs after RUnlock); a decoded
	// base record is already a fresh heap object, so it needs no second copy.
	//
	// This walks the overlay and the base inline instead of calling
	// gs.forEachNodeUnlocked. That helper cannot report a decode failure — its
	// callback returns bool, not error — so it SKIPS a damaged base record.
	// A skip costs a reader one absent node. It costs this function the record
	// itself: the write would complete without it, and the next open would
	// find neither the record nor its directory entry. The edge branch below
	// refuses for that reason, and this branch now matches it.
	nodes := make([]*Node, 0, gs.nodeCount())
	for i := range gs.nodeShards {
		for _, n := range gs.nodeShards[i] {
			nodes = append(nodes, n.Clone())
		}
	}
	var damagedNode uint64
	if gs.mmapSnap != nil {
		gs.mmapSnap.forEachNodeID(func(id uint64, off int64) {
			if _, shadowed := gs.lookupNodeShard(id); shadowed || gs.isNodeDeletedLocked(id) {
				return
			}
			n, err := decodeNodeRecordAt(gs.mmapSnap.data, off)
			if err != nil {
				// Refuse rather than drop, as the edge branch does.
				damagedNode = id
				return
			}
			nodes = append(nodes, n)
		})
	}
	if damagedNode != 0 {
		return nil, fmt.Errorf("refusing to write a snapshot: node record %d in the current snapshot is damaged", damagedNode)
	}

	// Live edges: shard overlay + non-shadowed, non-tombstoned base.
	edges := make([]*Edge, 0, gs.edgeCount())
	for i := range gs.edgeShards {
		for _, e := range gs.edgeShards[i] {
			edges = append(edges, e.Clone())
		}
	}
	var damagedEdge uint64
	if gs.mmapSnap != nil {
		gs.mmapSnap.forEachEdgeID(func(id uint64, off int64) {
			if _, shadowed := gs.lookupEdgeShard(id); shadowed || gs.isEdgeDeletedLocked(id) {
				return
			}
			e, err := decodeEdgeRecordAt(gs.mmapSnap.data, off)
			if err != nil {
				// Refuse rather than drop. Skipping the record here would turn
				// a damaged byte into permanent data loss at the next
				// snapshot, and the operator would never see it happen.
				damagedEdge = id
				return
			}
			edges = append(edges, e)
		})
	}
	if damagedEdge != 0 {
		return nil, fmt.Errorf("refusing to write a snapshot: edge record %d in the current snapshot is damaged", damagedEdge)
	}

	meta := buildMmapMetadata(gs)
	// buildMmapMetadata has no access to boundary (it is not a field on gs);
	// set it explicitly here so the file records the WAL boundary this
	// snapshot covers, for replay to skip on the next open.
	meta.WALBoundaryLSN = boundary

	return &mmapSnapshotCapture{nodes: nodes, edges: edges, meta: meta, boundary: boundary}, nil
}
