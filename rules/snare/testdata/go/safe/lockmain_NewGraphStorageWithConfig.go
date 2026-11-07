// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/storage.go at b9bc9d2, lines 56-378, function NewGraphStorageWithConfig.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (SAFE): constructor; gs is not shared yet.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// NewGraphStorageWithConfig creates a new graph storage engine with custom config
func NewGraphStorageWithConfig(config StorageConfig) (*GraphStorage, error) {
	gs := &GraphStorage{
		nodesByLabel:    make(labelIndex),
		edgesByType:     make(labelIndex),
		outgoingEdges:   make(map[uint64][]uint64),
		incomingEdges:   make(map[uint64][]uint64),
		propertyIndexes: make(map[string]*PropertyIndex),
		vectorIndex:     NewVectorIndex(),
		// Tenant-scoped indexes for multi-tenancy.
		// Keyed by tenantid.TenantID since audit task A1 (2026-05-06).
		tenantNodesByLabel: make(map[tenantid.TenantID]labelIndex),
		tenantEdgesByType:  make(map[tenantid.TenantID]labelIndex),
		tenantStats:        make(map[tenantid.TenantID]*TenantStats),
		tenantNodeIDs:      make(map[tenantid.TenantID]map[uint64]struct{}),
		tenantEdgeIDs:      make(map[tenantid.TenantID]map[uint64]struct{}),
		compressedOutgoing: make(map[uint64]*CompressedEdgeList),
		compressedIncoming: make(map[uint64]*CompressedEdgeList),
		useEdgeCompression: config.EnableEdgeCompression,
		shardMask:          255, // 256 shards - 1 for bitwise AND
		dataDir:            config.DataDir,
		useBatching:        config.EnableBatching,
		useCompression:     config.EnableCompression,
		nextNodeID:         1,
		nextEdgeID:         1,
		metricsRegistry:    metrics.DefaultRegistry(),
		// Set before loadFromDisk below so an encrypted snapshot can
		// decrypt during construction (M-14).
		encryptionEngine: config.EncryptionEngine,
		keyManager:       config.KeyManager,
		// ADR 0001: the registry map always exists so RegisterUniquenessRule
		// never nil-checks it; the required list is deployment config,
		// copied once and never mutated after construction.
		uniquenessRules:         make(map[string]UniquenessRule),
		requiredUniquenessRules: config.RequiredUniquenessRules,
		ephemeral:               config.Ephemeral,
	}

	// Initialize shard locks for fine-grained concurrency
	for i := range gs.shardLocks {
		gs.shardLocks[i] = &sync.RWMutex{}
	}

	// Initialize the partitioned node and edge shards. Each shard is a
	// distinct map; *Shards[i & shardMask] holds the entity for ID i.
	for i := range gs.nodeShards {
		gs.nodeShards[i] = make(map[uint64]*Node)
	}
	for i := range gs.edgeShards {
		gs.edgeShards[i] = make(map[uint64]*Edge)
	}

	// Resolve the filesystem driver before the first file operation below.
	gs.fs = config.FS
	if gs.fs == nil {
		gs.fs = vfs.Default()
	}

	// Everything below can acquire a WAL handle, an EdgeStore and an mmap
	// mapping, and every error return after that point used to abandon them.
	// The caller receives no store on a failure, so it has nothing to Close and
	// the resources are unreclaimable for the life of the process. CERT
	// FIO42-C, and the shape that rule predicts: violated on the error paths,
	// because the happy path closes.
	//
	// A fault sweep counted the reach — 4 leaking points on an ordinary open,
	// 6 with disk-backed edges, and 17 on the encryption-toggle path, which
	// does the most work after the WAL opens. One defer, rather than a close
	// beside every return, is what keeps that count at zero as returns are
	// added.
	opened := false
	defer func() {
		if !opened {
			gs.releaseAfterFailedOpen()
		}
	}()

	// Create data directory if it doesn't exist
	if err := gs.fs.MkdirAll(config.DataDir, dirPermissions); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Lock the directory before anything reads or writes it. A second store
	// on one directory appends to the same WAL and overwrites the snapshot on
	// Close, so one of them silently discards the other's writes.
	if !config.skipDataDirLock {
		if err := gs.lockDataDir(config.DataDir); err != nil {
			return nil, err
		}
	}

	// Under the lock, so no other store can write into the directory between
	// this check and the first write below.
	if config.Ephemeral {
		if err := gs.guardEphemeralDirEmpty(); err != nil {
			return nil, err
		}
	}

	// Initialize WAL (compressed, batched, or regular) - skip in bulk import
	// mode and for an Ephemeral store, neither of which replays one.
	if !config.BulkImportMode && !config.Ephemeral {
		if config.EnableCompression {
			compressedWAL, err := wal.NewCompressedWALWithFS(filepath.Join(config.DataDir, "wal"), gs.fs)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize compressed WAL: %w", err)
			}
			gs.compressedWAL = compressedWAL
		} else if config.EnableBatching {
			batchedWAL, err := wal.NewBatchedWALWithFS(
				filepath.Join(config.DataDir, "wal"),
				config.BatchSize,
				config.FlushInterval,
				gs.fs,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize batched WAL: %w", err)
			}
			gs.batchedWAL = batchedWAL
		} else {
			walInstance, err := wal.NewWALWithFS(filepath.Join(config.DataDir, "wal"), gs.fs)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize WAL: %w", err)
			}
			gs.wal = walInstance
		}
	}

	// Load the uniqueness-rules registry (ADR 0001). Before the disk-backed
	// edge and snapshot loading below so a corrupt or oversized rules.json
	// refuses the open before any other on-disk state is touched.
	if err := gs.loadUniquenessRules(); err != nil {
		return nil, err
	}

	// Initialize disk-backed edge storage if enabled (Milestone 2)
	if config.UseDiskBackedEdges {
		cacheSize := config.EdgeCacheSize
		if cacheSize == 0 {
			cacheSize = 10000 // Default cache size
		}

		edgeStoreDir := filepath.Join(config.DataDir, "edgestore")
		edgeStore, err := NewEdgeStoreWithFS(gs.fs, edgeStoreDir, cacheSize)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize EdgeStore: %w", err)
		}
		gs.edgeStore = edgeStore
		gs.useDiskBackedEdges = true
	}

	// Try to load from disk. When the mmap reopen mode is eligible and a
	// snapshot.mmap exists, take the lazy mmap path; otherwise the JSON path.
	// (A store opened in mmap mode with only a legacy snapshot.json loads JSON
	// here and writes snapshot.mmap on its next Snapshot.)
	gs.useMmapSnapshot = mmapEligible(config)
	loadErr := error(nil)
	if gs.useMmapSnapshot && fileExistsWithFS(gs.fs, mmapSnapshotPath(config.DataDir)) {
		loadErr = gs.loadFromDiskMmap()
	} else {
		loadErr = gs.loadFromDisk()
	}
	if loadErr != nil {
		// A missing snapshot means a fresh database ONLY when no snapshot of
		// the other format is sitting beside it. Refuse rather than open
		// empty: the empty store is indistinguishable from a real one, and
		// the next Close() overwrites the snapshot that still holds the data.
		if os.IsNotExist(loadErr) && mmapSnapshotStranded(gs.fs, config) {
			return nil, fmt.Errorf(
				"data directory %q holds snapshot.mmap, but this store is not configured for the mmap "+
					"snapshot path, so it would load an empty database. Set GRAPHDB_STORAGE_MODE=mmap "+
					"(or StorageConfig.UseMmapSnapshot, with encryption and disk-backed edges off), or "+
					"restore a json-mode archive",
				config.DataDir)
		}
		// If no snapshot exists, that's OK (fresh database)
		if !os.IsNotExist(loadErr) {
			return nil, fmt.Errorf("failed to load from disk: %w", loadErr)
		}
	}

	// Refuse the open when the active WAL backend's recovered LSN is below
	// the boundary the loaded snapshot recorded. Must run before the raise
	// below: raiseWALLSNToSnapshotBoundary would otherwise overwrite the
	// recovered value this check needs. See guardWALNotBehindSnapshot.
	if err := gs.guardWALNotBehindSnapshot(); err != nil {
		return nil, err
	}

	// BulkImportMode opened no WAL, so the replay below reads nothing. Refuse
	// when a WAL file holds writes the snapshot does not cover, instead of
	// dropping them. See guardBulkImportWALReplayed.
	if config.BulkImportMode {
		if err := gs.guardBulkImportWALReplayed(); err != nil {
			return nil, err
		}
	}

	// Refuse the open when the OTHER WAL backend's file still holds bytes.
	// The guard above cannot see this: the backend this open selected
	// usually has no file at all, so its recovered LSN is 0 and that check
	// returns nil on the zero while the previous backend's writes sit
	// unread. See guardWALBackendNotSwitched.
	if err := gs.guardWALBackendNotSwitched(); err != nil {
		return nil, err
	}

	// Raise the active WAL backend's LSN counter to at least the boundary the
	// loaded snapshot recorded, before replay reads gs.snapshotBoundaryLSN to
	// decide what to skip. See raiseWALLSNToSnapshotBoundary.
	gs.raiseWALLSNToSnapshotBoundary()

	// Replay WAL entries since last snapshot
	if err := gs.replayWAL(); err != nil {
		return nil, fmt.Errorf("failed to replay WAL: %w", err)
	}

	// Rebuild the HNSW vector index from the FINAL node set (snapshot + WAL
	// replay). The index definitions were recreated in loadFromDisk; the graph
	// itself is not serialized, so without this every vector search silently
	// returns nothing after a restart. Must run last so post-snapshot writes
	// recovered above are indexed too.
	gs.rebuildVectorIndexesFromNodes()

	// H-3 toggle hygiene: encryption is on but the replay saw pre-toggle
	// plaintext entries. Checkpoint once (the snapshot is encrypted; WAL
	// entries ≤ boundary are dropped) so the plaintext leaves the disk
	// instead of lingering next to new ciphertext.
	//
	// MEASURED, not argued. A crash sweep by the github.com/dd0wney/fault
	// session (harness fault-graphdb-sweep, beside this repository and not
	// in it), pinned at graphdb b7f904f:
	//
	//	states 25, reopened 25, reopen failures 0
	//	states that held plaintext before the reopen 12
	//	violations 0
	//
	// Conditions, because a figure without them is the defect this comment
	// exists to prevent.
	//
	// MODEL. crash.Model{}, the zero value: exhaustive cover, whole-Write
	// loss (Sector 0, so no sector tearing), metadata pending until the
	// parent directory is synced. The same model the mmap publish sweep
	// used, NOT the Cover: Prefixes the LSA sweep needed.
	//
	// SCENARIO, two phases, because this branch needs a WAL written
	// WITHOUT encryption and then replayed WITH it.
	//
	//	phase 1  unencrypted store, 3 nodes, each with one property
	//	         holding a canary. ABANDONED, not closed.
	//	phase 2  record the crash over the real directory, reopen WITH
	//	         an engine, Close. Then run every state, reopen each
	//	         rebuilt state with the engine, and scan.
	//
	// Phase 1 is abandoned rather than closed because a clean Close
	// snapshots successfully and truncates the WAL (persistence.go, the
	// Close truncate path, gated on that success), which would destroy
	// the very plaintext this branch exists to purge.
	//
	// THE SCANNER, and this is the most important condition on the
	// figure. It reads the RAW BYTES of every file under the data
	// directory and matches three forms of the canary: the literal,
	// base64.StdEncoding, and base64.RawStdEncoding. graphdb stores
	// property values base64-encoded, so a scanner checking only for
	// literal ASCII would report "no plaintext" for data one base64 -d
	// from readable. Base64 is an encoding, not encryption.
	//
	// FOUR CONTROLS gate the figure above:
	//
	//	the canary must be findable BEFORE the toggle, or the sweep
	//	  refuses to run
	//	the UNCRASHED toggle must leave none, or the invariant is wrong
	//	  rather than the code
	//	the original directory must be byte-identical across the sweep,
	//	  or the run is VOID
	//	at least one state must have held plaintext when its reopen
	//	  began. 12 did — a reachability control, not a claim that 12 is
	//	  an interesting number.
	//
	// What the number does NOT say. It walks the toggle path, not every
	// route by which plaintext reaches a disk: nothing here scans the
	// LSM, the btree, backups, logs, swap, or core dumps. One fixture — 3
	// nodes, one tenant, one property, one key — is not a size or shape
	// argument. Sector 0 means whole writes are lost or kept; it says
	// nothing about a torn write inside one. This was a single process,
	// with no concurrent writer and no second store on the same
	// directory. It says nothing about a deployment where the toggle
	// never runs because encryption was on from the start; that is a
	// different path and it is unmeasured. 25 states is what this model
	// yields for THIS scenario — a longer scenario has more crash points
	// and this result does not extend to them. A reader who takes "the
	// encryption toggle is crash-safe" to mean "plaintext cannot survive
	// on this disk" would be wrong.
	if gs.encryptionEngine != nil && gs.walReplaySawPlaintext {
		if err := gs.CompactWAL(); err != nil {
			return nil, fmt.Errorf("failed to purge plaintext WAL entries after enabling encryption: %w", err)
		}
	}
	// A store with no snapshot, no WAL entry and no rules.json holds nothing
	// that a key could contradict, so the configured key defines it.
	if gs.encryptionEngine != nil && !gs.rulesFileExisted && gs.walReplayedEntries == 0 && os.IsNotExist(loadErr) {
		gs.encryptionKeyProven = true
	}
	if err := gs.sealPlaintextRulesAfterOpen(); err != nil {
		return nil, err
	}

	// Everything that construction can log is logged now (replay, the
	// plaintext purge above). A Close whose boundary still equals this LSN
	// wrote nothing to the WAL; mmapSnapshotCleanLocked reads it.
	gs.mu.Lock()
	gs.walLSNAtOpen = gs.walBoundaryLSNLocked()
	// JSON mode: the file we loaded is the live state when the replay put
	// nothing on top of it and a rewrite would produce the same envelope.
	// A plaintext purge above already recorded its own sync point.
	if gs.jsonSnapshotLoadedInSync && gs.walReplayedEntries == 0 {
		gs.markJSONSnapshotSyncedLocked(gs.walLSNAtOpen, gs.encryptionEngine, gs.jsonSyncEpoch)
	}
	gs.mu.Unlock()

	opened = true
	return gs, nil
}
