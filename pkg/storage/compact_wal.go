package storage

import "fmt"

// Snapshot-isolation deep-copy helpers — see the ISOLATION comment in
// snapshotWithBoundary. Each runs under gs.mu.RLock; the copies are what
// json.Marshal touches after the lock is released.

// cloneNodesForSnapshotLocked walks the shards once, cloning directly —
// flatten-then-clone would build the intermediate pointer map only to
// throw it away, doubling allocations inside the RLock hold.
func (gs *GraphStorage) cloneNodesForSnapshotLocked() map[uint64]*Node {
	out := make(map[uint64]*Node, gs.nodeCount())
	for i := range gs.nodeShards {
		for id, node := range gs.nodeShards[i] {
			out[id] = node.Clone()
		}
	}
	return out
}

func (gs *GraphStorage) cloneEdgesForSnapshotLocked() map[uint64]*Edge {
	out := make(map[uint64]*Edge, gs.edgeCount())
	for i := range gs.edgeShards {
		for id, edge := range gs.edgeShards[i] {
			out[id] = edge.Clone()
		}
	}
	return out
}

func cloneAdjacency(in map[uint64][]uint64) map[uint64][]uint64 {
	out := make(map[uint64][]uint64, len(in))
	for id, ids := range in {
		copied := make([]uint64, len(ids))
		copy(copied, ids)
		out[id] = copied
	}
	return out
}

func cloneStringIDIndex(in map[string][]uint64) map[string][]uint64 {
	out := make(map[string][]uint64, len(in))
	for key, ids := range in {
		copied := make([]uint64, len(ids))
		copy(copied, ids)
		out[key] = copied
	}
	return out
}

// walBoundaryLSNLocked returns the LSN boundary consistent with the
// in-memory state observed under gs.mu: every write applied to memory
// before the caller's lock acquisition has a WAL LSN ≤ the returned value.
// The non-batched backends assign LSNs inline under gs.mu.Lock, so a bare
// read is exact; the batched backend assigns LSNs at flush time, so
// CheckpointLSN drains in-flight batches first. Callers must hold gs.mu
// (read or write side) — that's what excludes new writers — and must hold
// gs.txWALBarrier for the Transaction.Commit window (see Snapshot).
func (gs *GraphStorage) walBoundaryLSNLocked() uint64 {
	switch {
	case gs.useBatching && gs.batchedWAL != nil:
		return gs.batchedWAL.CheckpointLSN()
	case gs.useCompression && gs.compressedWAL != nil:
		return gs.compressedWAL.GetCurrentLSN()
	case gs.wal != nil:
		return gs.wal.GetCurrentLSN()
	}
	return 0
}

// CompactWAL checkpoints the WAL: it writes a snapshot of the current
// state, capturing the boundary LSN under the snapshot's lock, then drops
// every WAL entry the snapshot already covers (LSN ≤ boundary) while
// keeping concurrent writers' entries (LSN > boundary). The replay model
// is unchanged: snapshot (state ≤ boundary) + remaining WAL on top.
//
// M-1 (AUDIT_security_2026-06-10 / DESIGN_m1_wal_remanence_2026-06-10
// Option A): called after a tenant delete so the deleted tenant's
// OpCreate* records — its PII — leave the WAL immediately instead of
// lingering until the next Close. Safe under live traffic; a naive
// Snapshot+Truncate loses any write that lands between the two.
func (gs *GraphStorage) CompactWAL() error {
	if !gs.hasWAL() {
		return nil // in-memory only mode: nothing to compact
	}

	// One checkpoint at a time; concurrent calls queue rather than
	// snapshot+truncate over each other.
	gs.compactMu.Lock()
	defer gs.compactMu.Unlock()

	boundary, err := gs.snapshotWithBoundary()
	if err != nil {
		return fmt.Errorf("compact WAL: snapshot: %w", err)
	}
	if boundary == 0 {
		return nil // empty WAL — nothing to drop
	}

	switch {
	case gs.useBatching && gs.batchedWAL != nil:
		return gs.batchedWAL.TruncateUpTo(boundary)
	case gs.useCompression && gs.compressedWAL != nil:
		return gs.compressedWAL.TruncateUpTo(boundary)
	default:
		return gs.wal.TruncateUpTo(boundary)
	}
}
