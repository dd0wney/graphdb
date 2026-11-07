// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 120-147, function GraphStorage.createEdgesUnderLock.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// createEdgesUnderLock creates every spec under one hold of gs.mu. On error it
// returns the edges created so far alongside the error. The deferred unlock
// matters: the Cypher executor and net/http recover panics, so a panic here
// that left gs.mu held would hang every later write, Close included.
func (gs *GraphStorage) createEdgesUnderLock(tenantID string, specs []EdgeSpec) (edgeBatch, error) {
	b := edgeBatch{ids: make([]uint64, 0, len(specs))}

	gs.mu.Lock()
	defer gs.mu.Unlock()
	panicPoint("CreateEdgesWithTenant")
	for _, s := range specs {
		if err := gs.verifyNodeExistsForTenant(s.FromID, "source", tenantID); err != nil {
			return b, err
		}
		if err := gs.verifyNodeExistsForTenant(s.ToID, "target", tenantID); err != nil {
			return b, err
		}
		edge, p, err := gs.createEdgeWithTenantNoVerify(tenantID, s.FromID, s.ToID, s.Type, s.Properties, s.Weight)
		if err != nil {
			return b, err
		}
		b.ids = append(b.ids, edge.ID)
		if p != nil {
			b.pendings = append(b.pendings, p)
		}
	}
	return b, nil
}
