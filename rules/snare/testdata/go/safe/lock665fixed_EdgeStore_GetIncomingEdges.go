// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edgestore.go at b6fb6a4, lines 186-213, function EdgeStore.GetIncomingEdges.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// GetIncomingEdges retrieves the incoming edge list for a node
func (es *EdgeStore) GetIncomingEdges(nodeID uint64) ([]uint64, error) {
	key := makeEdgeStoreKey("in", nodeID)

	// Check cache first
	if cached := es.cache.Get(key); cached != nil {
		return cached.Decompress(), nil
	}

	// Cache miss - load from LSM
	data, found := es.lsmGet("GetIncomingEdges", key)

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
