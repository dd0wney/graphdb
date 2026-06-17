package storage

// mmap-backed lazy reopen loader (graphdb ask #1, Stage 1, Phase 1).
//
// loadFromDiskMmap maps snapshot.mmap and builds ONLY the in-memory indexes
// (label/type, per-tenant, adjacency, stats, nextIDs, property/vector indexes)
// via a cheap field-scan — it does NOT materialize node/edge property bags into
// the shard maps. Full nodes/edges are materialized lazily on read from the
// mapping. The shard maps then serve as a copy-on-write overlay for post-open
// writes (see resolve/materialize helpers below).
//
// Every helper here degrades to the plain shard lookup when gs.mmapSnap == nil,
// so the default (JSON) path is behaviourally unchanged.

import (
	"math"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/dd0wney/graphdb/pkg/tenantid"
)

func mmapSnapshotPath(dataDir string) string {
	return filepath.Join(dataDir, "snapshot.mmap")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// mmapEligible reports whether the mmap reopen path may be used. Stage 1 supports
// the plaintext, in-memory-adjacency case only; encryption (mmap can't map
// ciphertext) and disk-backed edges fall back to the JSON path. The snapshot.mmap
// file existence is checked by the caller.
func mmapEligible(config StorageConfig) bool {
	return config.UseMmapSnapshot &&
		config.EncryptionEngine == nil &&
		!config.UseDiskBackedEdges
}

// loadFromDiskMmap maps the snapshot and rebuilds the in-memory indexes without
// materializing the full graph. Mirrors the index-building portion of
// loadFromDisk (persistence.go) but sources fields from a field-scan of the
// mapped records instead of a JSON unmarshal.
func (gs *GraphStorage) loadFromDiskMmap() error {
	snap, err := openMmapSnapshot(mmapSnapshotPath(gs.dataDir))
	if err != nil {
		return err
	}
	gs.mmapSnap = snap
	for i := range gs.deletedNodes {
		gs.deletedNodes[i] = make(map[uint64]struct{})
		gs.deletedEdges[i] = make(map[uint64]struct{})
	}

	meta := snap.metadata()

	// Sticky label/type keys: register (possibly-empty) buckets so GetAllLabels
	// and the GraphQL schema keep exposing a label whose last member was deleted.
	for _, label := range meta.StickyNodeLabels {
		if gs.nodesByLabel[label] == nil {
			gs.nodesByLabel[label] = make(map[uint64]struct{})
		}
	}
	for _, etype := range meta.StickyEdgeTypes {
		if gs.edgesByType[etype] == nil {
			gs.edgesByType[etype] = make(map[uint64]struct{})
		}
	}

	// Node indexes (global label + per-tenant), via field-scan (no property bags).
	snap.forEachNodeID(func(id uint64, off int64) {
		nid, tenant, labels := scanNodeFields(snap.data, off)
		stub := &Node{ID: nid, TenantID: tenant, Labels: labels}
		for _, label := range labels {
			addToLabelIndex(gs.nodesByLabel, label, nid)
		}
		gs.addNodeToTenantIndex(stub)
	})

	// Edge indexes (global type + per-tenant); adjacency now served from CSR
	// base in getEdgeIDsForNode (Stage 2a) — no eager rebuild needed.
	snap.forEachEdgeID(func(id uint64, off int64) {
		eid, _, _, tenant, etype := scanEdgeFields(snap.data, off)
		stub := &Edge{ID: eid, TenantID: tenant, Type: etype}
		addToLabelIndex(gs.edgesByType, etype, eid)
		gs.addEdgeToTenantIndex(stub)
		// adjacency now served from CSR base in getEdgeIDsForNode (Stage 2a)
	})

	// Property indexes (restored verbatim, like loadFromDisk).
	gs.propertyIndexes = make(map[string]*PropertyIndex, len(meta.PropertyIndexes))
	for key, idxSnapshot := range meta.PropertyIndexes {
		gs.propertyIndexes[key] = &PropertyIndex{
			propertyKey: idxSnapshot.PropertyKey,
			indexType:   idxSnapshot.IndexType,
			index:       idxSnapshot.Index,
		}
	}

	// Vector index DEFINITIONS (empty HNSW graphs); vectors are inserted by
	// rebuildVectorIndexesFromNodes after WAL replay, over the final node set.
	for _, def := range meta.VectorIndexes {
		if gs.vectorIndex.HasIndexForTenant(tenantid.TenantID(def.TenantID), def.PropertyName) {
			continue
		}
		if err := gs.vectorIndex.CreateIndexForTenant(
			tenantid.TenantID(def.TenantID), def.PropertyName,
			def.Dimensions, def.M, def.EfConstruction, def.Metric,
		); err != nil {
			return err
		}
	}

	gs.nextNodeID = meta.NextNodeID
	gs.nextEdgeID = meta.NextEdgeID
	gs.stats = meta.Stats
	atomic.StoreUint64(&gs.avgQueryTimeBits, math.Float64bits(meta.Stats.AvgQueryTime))
	return nil
}

// --- overlay resolution -----------------------------------------------------

// isNodeDeletedLocked reports whether id was tombstoned since open. Caller holds
// rlockShard(id) (read path) or gs.mu (R/W) — the delete path takes both.
func (gs *GraphStorage) isNodeDeletedLocked(id uint64) bool {
	if gs.mmapSnap == nil {
		return false
	}
	_, dead := gs.deletedNodes[gs.getShardIndex(id)][id]
	return dead
}

func (gs *GraphStorage) isEdgeDeletedLocked(id uint64) bool {
	if gs.mmapSnap == nil {
		return false
	}
	_, dead := gs.deletedEdges[gs.getShardIndex(id)][id]
	return dead
}

// resolveNodeRefLocked returns the current node for id: the shard overlay if
// present, else the lazily-materialized mmap base (a fresh copy), respecting
// tombstones. When mmapSnap == nil this is exactly lookupNodeShard. Caller holds
// the appropriate read lock (rlockShard for the hot path, or gs.mu).
func (gs *GraphStorage) resolveNodeRefLocked(id uint64) (*Node, bool) {
	if n, ok := gs.lookupNodeShard(id); ok {
		return n, true
	}
	if gs.mmapSnap == nil || gs.isNodeDeletedLocked(id) {
		return nil, false
	}
	return gs.mmapSnap.getNode(id)
}

func (gs *GraphStorage) resolveEdgeRefLocked(id uint64) (*Edge, bool) {
	if e, ok := gs.lookupEdgeShard(id); ok {
		return e, true
	}
	if gs.mmapSnap == nil || gs.isEdgeDeletedLocked(id) {
		return nil, false
	}
	return gs.mmapSnap.getEdge(id)
}

// materializeNodeLocked returns the node's shard-resident pointer, promoting it
// from the mmap base into the shard overlay first if needed (copy-on-write).
// Used by the write path before in-place mutation. Caller holds gs.mu.Lock AND
// lockShard(id). When mmapSnap == nil this is exactly lookupNodeShard.
func (gs *GraphStorage) materializeNodeLocked(id uint64) (*Node, bool) {
	if n, ok := gs.lookupNodeShard(id); ok {
		return n, true
	}
	if gs.mmapSnap == nil || gs.isNodeDeletedLocked(id) {
		return nil, false
	}
	n, ok := gs.mmapSnap.getNode(id)
	if !ok {
		return nil, false
	}
	gs.storeNodeInShard(n) // promote into the overlay
	return n, true
}

func (gs *GraphStorage) materializeEdgeLocked(id uint64) (*Edge, bool) {
	if e, ok := gs.lookupEdgeShard(id); ok {
		return e, true
	}
	if gs.mmapSnap == nil || gs.isEdgeDeletedLocked(id) {
		return nil, false
	}
	e, ok := gs.mmapSnap.getEdge(id)
	if !ok {
		return nil, false
	}
	gs.storeEdgeInShard(e)
	return e, true
}

// markNodeDeletedLocked tombstones an mmap-resident node so reads stop resolving
// it from the base. No-op when mmap mode is off. Caller holds gs.mu.Lock + lockShard(id).
func (gs *GraphStorage) markNodeDeletedLocked(id uint64) {
	if gs.mmapSnap != nil {
		gs.deletedNodes[gs.getShardIndex(id)][id] = struct{}{}
	}
}

func (gs *GraphStorage) markEdgeDeletedLocked(id uint64) {
	if gs.mmapSnap != nil {
		gs.deletedEdges[gs.getShardIndex(id)][id] = struct{}{}
	}
}
