package storage

// Lazy membership index build for the mmap reopen mode (graphdb ask #1, Stage 2a).
//
// In mmap mode the membership indexes (nodesByLabel, edgesByType, per-tenant
// label/type maps and ID sets) are NOT built at open — that field-scan was 74%
// of the residual reopen cost. They are built once, on the first enumeration
// query, from the mmap base, skipping any base entity that is already shadowed by
// the shard overlay or tombstoned (those were maintained at write time). Per-tenant
// COUNTS are restored from persisted metadata at open, so this build does not touch
// them (avoids double-counting).
//
// No-op when mmapSnap == nil or membership is already built.

// ensureMembershipBuilt builds the membership indexes from the mmap base exactly
// once. Safe under concurrent first-enumeration: takes gs.mu and double-checks.
// MUST NOT be called while already holding gs.mu (it acquires the lock itself).
func (gs *GraphStorage) ensureMembershipBuilt() {
	if gs.mmapSnap == nil {
		return
	}
	gs.mu.RLock()
	built := gs.membershipBuilt
	gs.mu.RUnlock()
	if built {
		return
	}

	gs.mu.Lock()
	defer gs.mu.Unlock()
	if gs.membershipBuilt {
		return
	}

	gs.mmapSnap.forEachNodeID(func(id uint64, off int64) {
		// Skip tombstoned nodes (deleted since open). Shadowed nodes (updated
		// since open via CoW promote) still carry the same labels as the base
		// (UpdateNode does not change labels), so we index from the base scan.
		// Post-open creates have IDs beyond the base range and are never visited
		// by forEachNodeID — their membership was set at write time.
		// A future label-mutation API would need to either skip shadowed nodes
		// here or call ensureMembershipBuilt before the mutation and update the
		// index in-place — otherwise a relabeled base node would be double-indexed.
		if gs.isNodeDeletedLocked(id) {
			return
		}
		nid, tenant, labels := scanNodeFields(gs.mmapSnap.data, off)
		for _, label := range labels {
			addToLabelIndex(gs.nodesByLabel, label, nid)
		}
		gs.addNodeToTenantIndexNoCount(nid, tenant, labels)
	})

	gs.mmapSnap.forEachEdgeID(func(id uint64, off int64) {
		// Skip tombstoned edges. Shadowed edges (updated since open) retain their
		// type, so we index from the base scan. Post-open creates are disjoint.
		if gs.isEdgeDeletedLocked(id) {
			return
		}
		eid, _, _, tenant, etype := scanEdgeFields(gs.mmapSnap.data, off)
		addToLabelIndex(gs.edgesByType, etype, eid)
		gs.addEdgeToTenantIndexNoCount(eid, tenant, etype)
	})

	gs.membershipBuilt = true
}

// addNodeToTenantIndexNoCount mirrors addNodeToTenantIndex's label/ID-set inserts
// but does NOT touch tenant counts (restored from metadata at open).
func (gs *GraphStorage) addNodeToTenantIndexNoCount(id uint64, tenantID string, labels []string) {
	tid := effectiveTenantID(tenantID)
	if gs.tenantNodesByLabel[tid] == nil {
		gs.tenantNodesByLabel[tid] = make(labelIndex)
	}
	for _, label := range labels {
		addToLabelIndex(gs.tenantNodesByLabel[tid], label, id)
	}
	if gs.tenantNodeIDs[tid] == nil {
		gs.tenantNodeIDs[tid] = make(map[uint64]struct{})
	}
	gs.tenantNodeIDs[tid][id] = struct{}{}
}

// addEdgeToTenantIndexNoCount mirrors addEdgeToTenantIndex without count side effects.
func (gs *GraphStorage) addEdgeToTenantIndexNoCount(id uint64, tenantID, etype string) {
	tid := effectiveTenantID(tenantID)
	if gs.tenantEdgesByType[tid] == nil {
		gs.tenantEdgesByType[tid] = make(labelIndex)
	}
	addToLabelIndex(gs.tenantEdgesByType[tid], etype, id)
	if gs.tenantEdgeIDs[tid] == nil {
		gs.tenantEdgeIDs[tid] = make(map[uint64]struct{})
	}
	gs.tenantEdgeIDs[tid][id] = struct{}{}
}
