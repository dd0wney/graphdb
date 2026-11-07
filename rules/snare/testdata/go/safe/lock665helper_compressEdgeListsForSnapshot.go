// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/persistence.go at b6fb6a4, lines 48-63, function GraphStorage.compressEdgeListsForSnapshot.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// compressEdgeListsForSnapshot is the gs.mu.Lock section of
// snapshotWithBoundary. The comment there says why an mmap-backed store is
// skipped. The deferred unlock matters: the Cypher executor and net/http
// recover panics, so a panic here that left gs.mu held would hang every later
// call, Close included.
func (gs *GraphStorage) compressEdgeListsForSnapshot() {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	panicPoint("snapshotWithBoundary")
	if gs.mmapSnap == nil {
		gs.compressAllEdgeLists()
		// Clear uncompressed maps to free memory
		gs.outgoingEdges = make(map[uint64][]uint64)
		gs.incomingEdges = make(map[uint64][]uint64)
	}
}
