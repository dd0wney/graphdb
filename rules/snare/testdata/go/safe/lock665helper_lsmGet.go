// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edgestore.go at b6fb6a4, lines 91-97, function EdgeStore.lsmGet.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// lsmGet reads one record under es.mu.RLock, with a deferred unlock. See lsmPut.
func (es *EdgeStore) lsmGet(site, key string) ([]byte, bool) {
	es.mu.RLock()
	defer es.mu.RUnlock()
	panicPoint(site)
	return es.lsm.Get([]byte(key))
}
