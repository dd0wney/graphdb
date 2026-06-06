package storage

import "sort"

// pageFromSortedIDs returns up to `limit` cloned entities whose ID is > afterID,
// in ascending-ID order, plus the next cursor (the last returned item's ID, or 0
// if this is the last page). It clones only the page, not the whole ID set —
// the index-level allocation win over "clone everything then slice".
//
// `ids` MUST be sorted ascending. `cloneAt` clones the live entity for an ID
// under its shard lock, returning ok=false when the entity was deleted between
// ID collection and clone (the same non-atomic-snapshot tradeoff accepted by
// GetAllNodesForTenant: a write between the ID collection and the clone loop is
// simply skipped rather than blocking the reader). When ok=false for an entity
// that would have been within the page, the page may be shorter than `limit`
// for that call — callers that need a strict page size should loop.
//
// limit < 1 returns an empty page; it is the caller's responsibility to pass a
// sensible limit (the API layer enforces [1, MaxPageLimit]).
func pageFromSortedIDs[T any](ids []uint64, afterID uint64, limit int,
	cloneAt func(uint64) (*T, bool)) ([]*T, uint64) {
	if limit < 1 {
		return nil, 0
	}
	// Binary-search to the first ID strictly greater than afterID.
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > afterID })

	page := make([]*T, 0, limit)
	var lastID uint64

	for i := start; i < len(ids); i++ {
		if len(page) == limit {
			// We already have a full page and there are more IDs remaining;
			// lastID is the ID of the last item on this page.
			return page, lastID
		}
		ent, ok := cloneAt(ids[i])
		if !ok {
			// Entity was deleted between ID collection and clone — skip it.
			continue
		}
		page = append(page, ent)
		lastID = ids[i]
	}
	// Exhausted all IDs: this is the last page, so next cursor = 0.
	return page, 0
}

// NodesPageForTenant returns up to `limit` nodes belonging to `tenantID` with
// ID > afterID, in ascending-ID order, plus the next cursor (the last returned
// node's ID, or 0 if this is the last page). afterID=0 starts from the
// beginning of the tenant's node set.
//
// Lock pattern mirrors GetAllNodesForTenant: collect the sorted ID slice under
// gs.mu.RLock, release, then clone each node under its per-shard RLock. This
// means the result is not an atomic snapshot — a node deleted between ID
// collection and clone is skipped — but the global lock is not held across the
// clone loop, so writers are not stalled.
func (gs *GraphStorage) NodesPageForTenant(tenantID string, afterID uint64, limit int) ([]*Node, uint64) {
	tid := effectiveTenantID(tenantID)

	gs.mu.RLock()
	idSet := gs.tenantNodeIDs[tid]
	ids := make([]uint64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	gs.mu.RUnlock()

	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	cloneAt := func(id uint64) (*Node, bool) {
		gs.rlockShard(id)
		n, ok := gs.lookupNodeShard(id)
		if ok {
			n = n.Clone()
		}
		gs.runlockShard(id)
		return n, ok
	}
	return pageFromSortedIDs(ids, afterID, limit, cloneAt)
}

// NodesByLabelPageForTenant returns up to `limit` nodes belonging to
// `tenantID` with the given label and ID > afterID, in ascending-ID order,
// plus the next cursor (the last returned node's ID, or 0 if this is the last
// page). afterID=0 starts from the beginning. Returns (nil, 0) when the tenant
// or label is not found, mirroring GetNodesByLabelForTenant.
//
// Lock pattern: collect sorted IDs from the label index under gs.mu.RLock,
// release, then clone each node under its per-shard RLock. Same non-atomic-
// snapshot tradeoff as NodesPageForTenant and GetAllNodesForTenant.
func (gs *GraphStorage) NodesByLabelPageForTenant(tenantID, label string, afterID uint64, limit int) ([]*Node, uint64) {
	tid := effectiveTenantID(tenantID)

	gs.mu.RLock()
	labelMap := gs.tenantNodesByLabel[tid]
	if labelMap == nil {
		gs.mu.RUnlock()
		return nil, 0
	}
	bucket := labelMap[label]
	if len(bucket) == 0 {
		gs.mu.RUnlock()
		return nil, 0
	}
	ids := sortedBucketIDs(bucket) // sortedBucketIDs allocates a new slice
	gs.mu.RUnlock()

	cloneAt := func(id uint64) (*Node, bool) {
		gs.rlockShard(id)
		n, ok := gs.lookupNodeShard(id)
		if ok {
			n = n.Clone()
		}
		gs.runlockShard(id)
		return n, ok
	}
	return pageFromSortedIDs(ids, afterID, limit, cloneAt)
}

// EdgesPageForTenant returns up to `limit` edges belonging to `tenantID` with
// ID > afterID, in ascending-ID order, plus the next cursor (the last returned
// edge's ID, or 0 if this is the last page). afterID=0 starts from the
// beginning of the tenant's edge set.
//
// Lock pattern mirrors GetAllEdgesForTenant and NodesPageForTenant: collect
// sorted IDs under gs.mu.RLock, release, then clone under per-shard RLocks.
func (gs *GraphStorage) EdgesPageForTenant(tenantID string, afterID uint64, limit int) ([]*Edge, uint64) {
	tid := effectiveTenantID(tenantID)

	gs.mu.RLock()
	idSet := gs.tenantEdgeIDs[tid]
	ids := make([]uint64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	gs.mu.RUnlock()

	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	cloneAt := func(id uint64) (*Edge, bool) {
		gs.rlockShard(id)
		e, ok := gs.lookupEdgeShard(id)
		if ok {
			e = e.Clone()
		}
		gs.runlockShard(id)
		return e, ok
	}
	return pageFromSortedIDs(ids, afterID, limit, cloneAt)
}

// EdgesByTypePageForTenant returns up to `limit` edges belonging to `tenantID`
// with the given type and ID > afterID, in ascending-ID order, plus the next
// cursor (the last returned edge's ID, or 0 if this is the last page).
// afterID=0 starts from the beginning. Returns (nil, 0) when the tenant or
// edge type is not found, mirroring GetEdgesByTypeForTenant.
//
// Lock pattern: collect sorted IDs from the type index under gs.mu.RLock,
// release, then clone each edge under its per-shard RLock. Same non-atomic-
// snapshot tradeoff as EdgesPageForTenant and GetAllEdgesForTenant.
func (gs *GraphStorage) EdgesByTypePageForTenant(tenantID, edgeType string, afterID uint64, limit int) ([]*Edge, uint64) {
	tid := effectiveTenantID(tenantID)

	gs.mu.RLock()
	typeMap := gs.tenantEdgesByType[tid]
	if typeMap == nil {
		gs.mu.RUnlock()
		return nil, 0
	}
	bucket := typeMap[edgeType]
	if len(bucket) == 0 {
		gs.mu.RUnlock()
		return nil, 0
	}
	ids := sortedBucketIDs(bucket)
	gs.mu.RUnlock()

	cloneAt := func(id uint64) (*Edge, bool) {
		gs.rlockShard(id)
		e, ok := gs.lookupEdgeShard(id)
		if ok {
			e = e.Clone()
		}
		gs.runlockShard(id)
		return e, ok
	}
	return pageFromSortedIDs(ids, afterID, limit, cloneAt)
}
