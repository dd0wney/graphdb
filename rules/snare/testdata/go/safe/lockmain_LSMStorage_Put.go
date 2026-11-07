// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/lsm/lsm.go at b9bc9d2, lines 51-78, function LSMStorage.Put.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (SAFE): BlockCache.Delete takes its own mutex with defer; map delete and list.Remove on its own element.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// Put writes a key-value pair
func (lsm *LSMStorage) Put(key, value []byte) error {
	lsm.mu.Lock()

	// Invalidate cache entry for this key (handles updates)
	cacheKey := string(key)
	lsm.cache.Delete(cacheKey)

	// Write to MemTable
	if err := lsm.memTable.Put(key, value); err != nil {
		lsm.mu.Unlock()
		return err
	}

	// Update stats atomically (lock-free)
	lsm.stats.WriteCount.Add(1)
	lsm.stats.BytesWritten.Add(int64(len(key) + len(value)))

	// Check if MemTable is full
	needsFlush := lsm.memTable.IsFull()
	lsm.mu.Unlock()

	if needsFlush {
		lsm.triggerFlush()
	}

	return nil
}
