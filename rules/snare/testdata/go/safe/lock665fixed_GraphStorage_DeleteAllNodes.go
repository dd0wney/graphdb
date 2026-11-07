// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 1038-1057, function GraphStorage.DeleteAllNodes.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// DeleteAllNodes removes every node, edge, and index from the graph and truncates
// the WAL, then writes an empty snapshot so a restart doesn't replay the old data.
// Intended for full-reload scenarios where the caller will repopulate the graph
// immediately after.
func (gs *GraphStorage) DeleteAllNodes() error {
	if err := gs.clearAllUnderLock(); err != nil {
		return err
	}

	// Write an empty snapshot so a process restart doesn't reload from the old
	// snapshot.json + WAL.  Snapshot() acquires its own read lock so we release
	// the write lock first.
	if gs.dataDir != "" {
		if err := gs.Snapshot(); err != nil {
			return fmt.Errorf("write empty snapshot after clear: %w", err)
		}
	}

	return nil
}
