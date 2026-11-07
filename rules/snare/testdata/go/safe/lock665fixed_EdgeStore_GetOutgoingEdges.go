// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edgestore.go at b6fb6a4, lines 128-155, function EdgeStore.GetOutgoingEdges.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// GetOutgoingEdges retrieves the outgoing edge list for a node
func (es *EdgeStore) GetOutgoingEdges(nodeID uint64) ([]uint64, error) {
	key := makeEdgeStoreKey("out", nodeID)

	// Check cache first
	if cached := es.cache.Get(key); cached != nil {
		return cached.Decompress(), nil
	}

	// Cache miss - load from LSM
	data, found := es.lsmGet("GetOutgoingEdges", key)

	if !found {
		// Key not found - return empty list
		return []uint64{}, nil
	}

	// Deserialize
	compressed, err := es.deserializeEdgeList(data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize edge list: %w", err)
	}

	// Add to cache
	es.cache.Put(key, compressed)

	return compressed.Decompress(), nil
}
