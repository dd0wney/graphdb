// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edgestore.go at b6fb6a4, lines 80-89, function EdgeStore.lsmPut.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// lsmPut writes one record under es.mu. The deferred unlock matters: the Cypher
// executor and net/http recover panics, so a panic in the LSM that left es.mu
// held would block every later edge-list read and write. site names the public
// caller for the panic hook.
func (es *EdgeStore) lsmPut(site, key string, data []byte) error {
	es.mu.Lock()
	defer es.mu.Unlock()
	panicPoint(site)
	return es.lsm.Put([]byte(key), data)
}
