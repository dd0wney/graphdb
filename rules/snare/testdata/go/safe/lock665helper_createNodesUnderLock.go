// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 97-122, function GraphStorage.createNodesUnderLock.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// createNodesUnderLock creates every spec under one hold of gs.mu. On error it
// returns the nodes created so far alongside the error. The deferred unlock
// matters: see createNodeUnderLock.
func (gs *GraphStorage) createNodesUnderLock(tenantID string, specs []NodeSpec) (nodeBatch, error) {
	b := nodeBatch{
		ids:     make([]uint64, 0, len(specs)),
		created: make([]*Node, 0, len(specs)),
	}

	gs.mu.Lock()
	defer gs.mu.Unlock()
	panicPoint("CreateNodesWithTenant")
	for _, s := range specs {
		node, wp, vps, err := gs.createNodeLocked(tenantID, s.Labels, s.Properties)
		if err != nil {
			return b, err
		}
		b.ids = append(b.ids, node.ID)
		b.created = append(b.created, node)
		if wp != nil {
			b.pendings = append(b.pendings, wp)
		}
		b.plans = append(b.plans, vps...)
	}
	return b, nil
}
