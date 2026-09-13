package storage

// A Close that changed nothing must not rewrite snapshot.json.
//
// This is the JSON-mode half of what mmap_snapshot_clean.go does for
// snapshot.mmap. On the 2.0M-node ICIJ corpus a read-only JSON consumer paid
// 25 s and 17.5 GB for the rewrite on every exit
// (COI_SCREEN_REAL_CORPUS_2026-09-13.md, finding 1).
//
// The mmap decision could read the state directly: every write lands in the
// overlay, the tombstones, the WAL or the metadata, and an empty set of all
// four means the file already is the live state. JSON mode has no overlay
// and no base; memory IS the state, and comparing it to the file would cost
// the marshal the rewrite is trying to avoid. So the proof here is a sync
// point instead: the WAL boundary LSN at the last moment the file on disk
// was known to equal memory. Every logged write moves the LSN past it. The
// sync point is recorded in exactly three places — a successful JSON
// snapshot publish, a construction that loaded a snapshot and replayed
// nothing on top of it, and nowhere else — and it is discarded by the one
// path that breaks the LSN's monotonicity, DeleteAllNodes, which truncates
// the WAL back to LSN 0.
//
// Two mutations never reach the WAL and are covered separately:
//
//   - SetEncryption swaps the engine. The sync point records the engine the
//     file was written with, so a swap is a mismatch and forces the rewrite;
//     a plaintext snapshot must not outlive the operator turning encryption
//     on, and a snapshot under the old key must not outlive a rotation.
//   - DeleteAllNodes clears memory, truncates the WAL (LSN back to 0) and
//     then writes its own empty snapshot. If that snapshot fails, memory is
//     empty, the WAL is empty and the old data is on disk, with the LSN
//     equal to what a read-only session would see. It invalidates the sync
//     point before it truncates; its own snapshot re-records one on success.
//
// A store with no WAL (BulkImportMode, in-memory only) has no LSN to compare
// and is never clean: it writes on every Close, as it always did.

import (
	"path/filepath"

	"github.com/dd0wney/graphdb/pkg/encryption"
)

func jsonSnapshotPath(dataDir string) string {
	return filepath.Join(dataDir, "snapshot.json")
}

// jsonSnapshotSync is the last moment snapshot.json on disk matched memory.
// Guarded by gs.mu. The zero value is "unknown", which reads as dirty.
type jsonSnapshotSync struct {
	valid bool
	// lsn is the WAL boundary the on-disk snapshot covers; a Close whose
	// boundary still equals it logged nothing since.
	lsn uint64
	// engine is the encryption engine the on-disk snapshot was written with
	// (nil for plaintext). Compared by identity: a rotation is a new engine.
	engine encryption.EncryptDecrypter
}

// jsonSnapshotCleanLocked reports whether snapshot.json on disk already holds
// the live state. Caller holds gs.mu (either side). boundary is the WAL LSN
// captured under that lock for this snapshot; engine is the current engine
// read under the same lock.
func (gs *GraphStorage) jsonSnapshotCleanLocked(boundary uint64, engine encryption.EncryptDecrypter) bool {
	if !gs.hasWAL() {
		return false // no LSN to compare: BulkImportMode or in-memory only
	}
	if gs.walWriteFailed.Load() {
		return false // a write reached memory and not the WAL; the LSN is behind
	}
	s := gs.jsonSnapshotSync
	if !s.valid || s.lsn != boundary || s.engine != engine {
		return false
	}
	// The file could have been removed under us; a skip would then leave the
	// next open with nothing. Refusing to skip costs one rewrite.
	return fileExistsWithFS(gs.fs, jsonSnapshotPath(gs.dataDir))
}

// markJSONSnapshotSyncedLocked records that the file on disk now equals the
// memory state at boundary, written with engine. Caller holds gs.mu.Lock.
//
// epoch is gs.jsonSyncEpoch as the publisher read it under the same lock as
// its boundary. A publish that captured its state before an invalidation and
// renamed its file after it has put stale bytes on disk, over whatever a
// newer publish put there. The sync point is discarded, not merely left as
// it was: the newer publish may have recorded one, and the file it
// described is gone. The next Close then rewrites, as it did before this
// file existed.
func (gs *GraphStorage) markJSONSnapshotSyncedLocked(boundary uint64, engine encryption.EncryptDecrypter, epoch uint64) {
	if epoch != gs.jsonSyncEpoch {
		gs.jsonSnapshotSync = jsonSnapshotSync{}
		return
	}
	gs.jsonSnapshotSync = jsonSnapshotSync{valid: true, lsn: boundary, engine: engine}
}

// noteWALWriteError records a WAL append failure and returns err unchanged.
// Every WAL write path calls it on its error, whether the caller gets the
// error or only a log line: wal.Append rolls the LSN back when the entry
// did not land, so from here on the LSN says nothing about the memory
// state, and Close must write.
func (gs *GraphStorage) noteWALWriteError(err error) error {
	if err != nil {
		gs.walWriteFailed.Store(true)
	}
	return err
}

// invalidateJSONSnapshotSyncLocked discards the sync point and moves the
// epoch, so a publish already in flight cannot record one. Caller holds
// gs.mu.Lock. Used before the WAL is truncated outside a snapshot publish,
// which resets the LSN and makes a later equality meaningless.
func (gs *GraphStorage) invalidateJSONSnapshotSyncLocked() {
	gs.jsonSnapshotSync = jsonSnapshotSync{}
	gs.jsonSyncEpoch++
}
