// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edgestore.go at b6fb6a4^, lines 173-202, function EdgeStore.GetIncomingEdges.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
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
	// ruleid: graphdb-lock-call-without-defer
	es.mu.RLock()
	data, found := es.lsm.Get([]byte(key))
	es.mu.RUnlock()

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
