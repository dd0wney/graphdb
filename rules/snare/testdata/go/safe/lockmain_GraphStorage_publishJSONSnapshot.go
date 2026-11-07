// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/persistence.go at b9bc9d2, lines 285-319, function GraphStorage.publishJSONSnapshot.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (SAFE): callee compares one uint64 and assigns one struct.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// publishJSONSnapshot writes data as snapshot.json and records the sync point
// for it. One publish at a time: two callers shared the temp path, and the
// sync point must describe whichever file renamed last, which only holds if
// the rename and the record happen under the same lock.
func (gs *GraphStorage) publishJSONSnapshot(data []byte, boundary uint64, engine encryption.EncryptDecrypter, epoch uint64) error {
	gs.jsonPublishMu.Lock()
	defer gs.jsonPublishMu.Unlock()

	snapshotPath := jsonSnapshotPath(gs.dataDir)
	tmpPath := snapshotPath + ".tmp"

	// Write to temporary file first
	if err := writeFileWithFS(gs.fs, tmpPath, data, filePermissions); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	// Atomic rename
	if err := gs.fs.Rename(tmpPath, snapshotPath); err != nil {
		return fmt.Errorf("failed to rename snapshot: %w", err)
	}

	// The rename is atomic, but the directory entry it creates is not durable
	// until the parent directory is itself synced. Without this a power cut
	// after the rename loses the publish whole: the bytes are on the disk and
	// no name points at them. PR #530 did this for the mmap path; this path
	// kept the gap until a crash sweep generated the states it produces.
	if err := vfs.SyncParentDir(gs.fs, snapshotPath); err != nil {
		return fmt.Errorf("failed to sync the data directory after the snapshot rename: %w", err)
	}

	gs.mu.Lock()
	gs.markJSONSnapshotSyncedLocked(boundary, engine, epoch)
	gs.mu.Unlock()
	return nil
}
