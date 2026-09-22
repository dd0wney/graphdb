package storage

import (
	"fmt"
	"path/filepath"
)

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

// recoveredWALLSN returns the active WAL backend's LSN as recoverLSN set it
// at open — the same switch raiseWALLSNToSnapshotBoundary and
// walBoundaryLSNLocked use to pick the active backend. Call it before
// raiseWALLSNToSnapshotBoundary runs: after that call GetCurrentLSN no
// longer reports what was found on disk, only the raised value.
//
// The batched case reads GetCurrentLSN, not walBoundaryLSNLocked's
// CheckpointLSN (which drains in-flight batches first): the two agree here
// only because this runs during construction, before any caller can have
// enqueued a write, so the batch buffer is empty and there is nothing for
// CheckpointLSN to drain.
//
// Constructor-only; no locking.
func (gs *GraphStorage) recoveredWALLSN() uint64 {
	switch {
	case gs.useBatching && gs.batchedWAL != nil:
		return gs.batchedWAL.GetCurrentLSN()
	case gs.useCompression && gs.compressedWAL != nil:
		return gs.compressedWAL.GetCurrentLSN()
	case gs.wal != nil:
		return gs.wal.GetCurrentLSN()
	}
	return 0
}

// guardWALNotBehindSnapshot refuses the open when the active WAL backend's
// recovered LSN is below the WAL boundary LSN the loaded snapshot recorded.
// The constructor calls this before raiseWALLSNToSnapshotBoundary, which
// would otherwise raise the counter first and erase the evidence.
//
// recovered == 0 means an empty WAL (a fresh database, or a clean close with
// no further writes): nothing to compare against, so it opens.
// recovered == gs.snapshotBoundaryLSN is what a WAL truncate that failed at
// a prior Close leaves behind (T1, snapshot_boundary_test.go): the WAL still
// holds only entries the snapshot already covers, so it opens too.
// recovered strictly between 0 and the boundary means either a binary built
// before the WAL boundary LSN fix wrote to this WAL directory and reset the
// LSN counter to 0 on its own truncate (a downgrade), or the WAL's tail is
// damaged and ReadAll read short (pkg/wal/wal.go's ReadAll stops at a torn
// or zero-filled tail without itself returning an error — see that
// function's doc comment). Either way, opening would replay less than the
// WAL actually holds without saying so, so this refuses instead.
//
// It does NOT catch an ordinary WAL backend switch, though this comment
// claimed a third cause until 2026-09-15. The two backends write different
// files (wal.log vs wal_compressed.log — walFilePath in
// compact_wal_test.go), so a first switch opens a file that does not exist,
// recovered is 0, and the zero branch above returns nil while the previous
// backend's file still holds un-snapshotted writes. guardWALBackendNotSwitched
// below is the check that catches that.
func (gs *GraphStorage) guardWALNotBehindSnapshot() error {
	recovered := gs.recoveredWALLSN()
	if recovered == 0 || recovered >= gs.snapshotBoundaryLSN {
		return nil
	}
	return fmt.Errorf(
		"%w: recovered LSN %d, snapshot boundary %d, WAL directory %q — one of two things: an "+
			"older binary (built before the WAL boundary LSN fix) wrote to this WAL directory after "+
			"the snapshot and reset its LSN counter; or the WAL's tail is damaged and read short. "+
			"Do not open this data "+
			"directory with this binary until the WAL is inspected: the entries it currently holds "+
			"are not all reflected in the snapshot",
		ErrWALBehindSnapshot, recovered, gs.snapshotBoundaryLSN, filepath.Join(gs.dataDir, "wal"))
}

// raiseWALLSNToSnapshotBoundary raises the active WAL backend's LSN counter
// to at least gs.snapshotBoundaryLSN. The constructor calls this once, after
// the snapshot loads and sets that field, and before replayWAL runs.
//
// A freshly constructed WAL backend always starts its own counter at 0, no
// matter what boundary the snapshot it is paired with recorded. Without this
// call, a store that closed cleanly at boundary B would reopen with an empty
// WAL at LSN 0, hand the first post-open write LSN 1, and a crash before the
// next Close would then have the next replay skip that write as already
// covered by the boundary-B snapshot (replayEntry, persistence_replay.go).
// No locking: constructor-only, before any other goroutine can reach gs.
func (gs *GraphStorage) raiseWALLSNToSnapshotBoundary() {
	switch {
	case gs.useBatching && gs.batchedWAL != nil:
		gs.batchedWAL.RaiseLSNTo(gs.snapshotBoundaryLSN)
	case gs.useCompression && gs.compressedWAL != nil:
		gs.compressedWAL.RaiseLSNTo(gs.snapshotBoundaryLSN)
	case gs.wal != nil:
		gs.wal.RaiseLSNTo(gs.snapshotBoundaryLSN)
	}
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

	boundary, err := gs.snapshotWithBoundary(false)
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

// walBackendFileNames returns the WAL file this open will use and the file the
// OTHER backend would use, both inside <dataDir>/wal. The plain and batched
// backends share wal.log (BatchedWAL wraps a plain *wal.WAL); the compressed
// backend uses wal_compressed.log.
func (gs *GraphStorage) walBackendFileNames() (active, foreign string) {
	if gs.useCompression {
		return "wal_compressed.log", "wal.log"
	}
	return "wal.log", "wal_compressed.log"
}

// guardWALBackendNotSwitched refuses the open when the OTHER WAL backend's
// file still holds bytes.
//
// Flipping StorageConfig.EnableCompression changes which file the store
// reads. It does not migrate the old one, and nothing merges the two. After a
// clean Close the old file is empty, because Truncate replaces it, so an
// ordinary switch opens normally. Bytes in the other file mean a crash or
// writes no snapshot covers, and opening would serve a graph missing them
// while the bytes sit unread. See ErrWALBackendSwitched.
//
// guardWALNotBehindSnapshot cannot catch this: the newly selected backend's
// file usually does not exist, so its recovered LSN is 0 and that check
// returns nil on the zero.
//
// Constructor-only; no locking. A store with no WAL (BulkImportMode, or
// in-memory only) has nothing to compare and returns nil.
func (gs *GraphStorage) guardWALBackendNotSwitched() error {
	if !gs.hasWAL() {
		return nil
	}

	active, foreign := gs.walBackendFileNames()
	foreignPath := filepath.Join(gs.dataDir, "wal", foreign)

	info, err := gs.fs.Stat(foreignPath)
	if err != nil {
		return nil // absent, or unreadable: nothing this check can assert
	}
	if info.Size() == 0 {
		return nil // a clean Close leaves the replaced file empty
	}

	return fmt.Errorf(
		"%w: this open selected the %s WAL backend and reads %q, but %q still holds %d bytes. "+
			"StorageConfig.EnableCompression changed since the last write, and the two backends "+
			"use different files: nothing merges them, so opening would serve a graph missing "+
			"every write in that file. A clean shutdown leaves it empty, so bytes in it mean a "+
			"crash or writes no snapshot covers. Reopen with the previous EnableCompression "+
			"setting and close cleanly, which drains the file, before you switch",
		ErrWALBackendSwitched,
		map[bool]string{true: "compressed", false: "plain"}[gs.useCompression],
		filepath.Join(gs.dataDir, "wal", active), foreignPath, info.Size())
}
