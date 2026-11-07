// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edgestore.go at b6fb6a4^, lines 80-109, function EdgeStore.StoreOutgoingEdges.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// StoreOutgoingEdges stores the outgoing edge list for a node
func (es *EdgeStore) StoreOutgoingEdges(nodeID uint64, edges []uint64) error {
	key := makeEdgeStoreKey("out", nodeID)

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
	// ruleid: graphdb-lock-call-without-defer
	es.mu.Lock()
	err = es.lsm.Put([]byte(key), data)
	es.mu.Unlock()

	if err != nil {
		return fmt.Errorf("failed to write to LSM: %w", err)
	}

	// Update cache
	es.cache.Put(key, compressed)

	return nil
}
