package storage

// A Close that changed nothing must not rewrite the snapshot.
//
// Close calls Snapshot unconditionally, and on the mmap path that merges
// overlay ∪ base − tombstones into a fresh snapshot.mmap. On the 2.0M-node
// ICIJ corpus a read-only consumer paid 8.3 s and 7.9 GB for that on every
// exit, and two read-only consumers exiting together rewrote the same
// customer-data file concurrently (COI_SCREEN_REAL_CORPUS_2026-09-13.md,
// finding 1). This file decides when the file on disk already IS the live
// state, so the rewrite can be skipped.
//
// The decision is structural, not a dirty flag. A flag has to be set on
// every write path and a missed one is silent data loss; the structure
// below is what every write path already produces. In mmap mode a write
// lands in exactly one of: the shard overlay (create, update), the
// tombstone sets (delete), the WAL (anything logged), or the metadata
// (index definitions, sticky keys, counters). A session is clean only when
// all four say nothing happened.

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"
)

// mmapSnapshotCleanLocked reports whether snapshot.mmap on disk already holds
// the live state. Caller holds gs.mu.RLock. boundary is the WAL LSN captured
// under that lock for this snapshot.
func (gs *GraphStorage) mmapSnapshotCleanLocked(boundary uint64) bool {
	if gs.mmapSnap == nil {
		return false // no base to be clean against (JSON mode, or DeleteAllNodes unmapped it)
	}
	if boundary != gs.walLSNAtOpen {
		return false // something was logged since open, whatever the maps say
	}
	if gs.nodeCount() != 0 || gs.edgeCount() != 0 {
		return false // overlay holds a create or an update
	}
	for i := range gs.deletedNodes {
		if len(gs.deletedNodes[i]) != 0 || len(gs.deletedEdges[i]) != 0 {
			return false // a base record was deleted
		}
	}
	if !mmapMetadataEquivalent(buildMmapMetadata(gs), gs.mmapSnap.metadata()) {
		return false // an index definition, a sticky key, a counter or a tenant stat moved
	}
	// The file could have been removed under us; a skip would then leave the
	// next open with nothing. Refusing to skip costs one rewrite.
	return fileExistsWithFS(gs.fs, mmapSnapshotPath(gs.dataDir))
}

// mmapMetadataEquivalent compares two metadata tails by their JSON encoding
// after normalisation, so that a field added to mmapMetadata later takes part
// in the comparison without anyone remembering to list it here. Normalised
// away: the three query-telemetry fields (TotalQueries, AvgQueryTime,
// LastSnapshot), which reads move and which are not worth a 1 GB rewrite to
// persist, and the order of the two sticky-key slices and the vector index
// definitions, which come out of map iteration.
func mmapMetadataEquivalent(live, base *mmapMetadata) bool {
	if live == nil || base == nil {
		return false
	}
	a, errA := json.Marshal(normaliseMmapMetadata(*live))
	b, errB := json.Marshal(normaliseMmapMetadata(*base))
	if errA != nil || errB != nil {
		return false // cannot prove equivalence, so do the rewrite
	}
	return bytes.Equal(a, b)
}

func normaliseMmapMetadata(m mmapMetadata) mmapMetadata {
	m.Stats.TotalQueries = 0
	m.Stats.AvgQueryTime = 0
	m.Stats.LastSnapshot = time.Time{}
	// WALBoundaryLSN is compared directly by mmapSnapshotCleanLocked
	// (boundary vs gs.walLSNAtOpen), not through this equivalence check.
	// buildMmapMetadata(gs) — the "live" side of every call here — never
	// sets it, so it always reads 0 on that side; comparing it against the
	// base's real recorded value would report every clean session as dirty.
	m.WALBoundaryLSN = 0
	m.StickyNodeLabels = sortedCopy(m.StickyNodeLabels)
	m.StickyEdgeTypes = sortedCopy(m.StickyEdgeTypes)
	defs := make([]VectorIndexDef, len(m.VectorIndexes))
	copy(defs, m.VectorIndexes)
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].TenantID != defs[j].TenantID {
			return defs[i].TenantID < defs[j].TenantID
		}
		return defs[i].PropertyName < defs[j].PropertyName
	})
	m.VectorIndexes = defs
	return m
}

func sortedCopy(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	sort.Strings(out)
	return out
}
