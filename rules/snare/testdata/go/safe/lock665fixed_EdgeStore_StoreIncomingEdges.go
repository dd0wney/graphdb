// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edgestore.go at b6fb6a4, lines 157-184, function EdgeStore.StoreIncomingEdges.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// StoreIncomingEdges stores the incoming edge list for a node
func (es *EdgeStore) StoreIncomingEdges(nodeID uint64, edges []uint64) error {
	key := makeEdgeStoreKey("in", nodeID)

	// Compress edge list
	compressed, err := NewCompressedEdgeList(edges)
	if err != nil {
		return fmt.Errorf("failed to compress edge list: %w", err)
	}

	// Serialize
	data, err := es.serializeEdgeList(compressed)
	if err != nil {
		return fmt.Errorf("failed to serialize edge list: %w", err)
	}

	// Store in LSM
	err = es.lsmPut("StoreIncomingEdges", key, data)

	if err != nil {
		return fmt.Errorf("failed to write to LSM: %w", err)
	}

	// Update cache
	es.cache.Put(key, compressed)

	return nil
}
