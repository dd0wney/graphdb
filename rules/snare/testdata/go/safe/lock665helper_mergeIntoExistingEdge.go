// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 745-771, function GraphStorage.mergeIntoExistingEdge.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// mergeIntoExistingEdge promotes the edge into the shard overlay, merges
// properties into it and sets its weight, under the edge's shard write lock.
// Caller holds gs.mu. The deferred unlock keeps a panic from leaving the shard
// write-locked.
func (gs *GraphStorage) mergeIntoExistingEdge(edgeID uint64, properties map[string]Value, weight float64) (*Edge, error) {
	gs.lockShard(edgeID)
	defer gs.unlockShard(edgeID)
	panicPoint("UpsertEdge.shard")
	edge, err := gs.materializeEdgeLocked(edgeID) // mmap mode: promote base edge
	if err != nil {
		return nil, fmt.Errorf("upsert edge %d: %w", edgeID, err)
	}
	if err := checkPatchWidths(edge.Properties, properties, nil); err != nil {
		return nil, err
	}

	// Merge properties (new values override existing). An edge created
	// with nil properties has no map to merge into.
	if edge.Properties == nil && len(properties) > 0 {
		edge.Properties = make(map[string]Value, len(properties))
	}
	for k, v := range properties {
		edge.Properties[k] = v
	}
	edge.Weight = weight
	return edge, nil
}
