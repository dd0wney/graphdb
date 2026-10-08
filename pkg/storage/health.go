package storage

import "fmt"

// Health reports a condition that stops the store from serving writes
// durably and that lasts until the process restarts: the store is closed, or
// a failed fsync has poisoned the WAL (#624), so every later write reaches
// memory and not the disk. A readiness probe built on it takes the instance
// out of rotation instead of letting it keep acknowledging writes it cannot
// keep.
//
// A single failed append that did not poison the WAL is not reported: the
// next append can succeed, and the failed write already returned
// ErrWALWriteFailed to its caller. A store with no WAL (BulkImportMode) has
// nothing to poison.
func (gs *GraphStorage) Health() error {
	if gs.closed.Load() {
		return ErrStorageClosed
	}
	if err := gs.walPoisoned(); err != nil {
		return fmt.Errorf("storage cannot persist writes until restart: %w", err)
	}
	return nil
}

// walPoisoned returns the active WAL backend's poison error, or nil.
func (gs *GraphStorage) walPoisoned() error {
	switch {
	case gs.useBatching && gs.batchedWAL != nil:
		return gs.batchedWAL.Poisoned()
	case gs.useCompression && gs.compressedWAL != nil:
		return gs.compressedWAL.Poisoned()
	case gs.wal != nil:
		return gs.wal.Poisoned()
	}
	return nil
}
