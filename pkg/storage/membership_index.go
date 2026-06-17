package storage

// mmap-native membership accessors (graphdb ask #1, Stage 2b). Each returns the
// sorted ID set for a membership key, merging the persisted base run (minus
// tombstones) with the post-open overlay map. When mmapSnap == nil (JSON mode)
// each returns the in-memory map's set directly — byte-identical to pre-2b.
//
// Caller holds gs.mu (R or W). New post-open IDs are disjoint from base IDs, so
// the union needs no dedup. The overlay never holds a tombstoned ID (deletes
// remove from the overlay map and tombstone atomically), so only the base run is
// tombstone-filtered.

import (
	"sort"

	"github.com/dd0wney/graphdb/pkg/tenantid"
)

// mergeBaseOverlay returns sorted (base − tombstoned) ∪ overlayIDs.
// overlay may be nil (ranging a nil map is a no-op).
func mergeBaseOverlay(base []uint64, overlay map[uint64]struct{}, deleted func(uint64) bool) []uint64 {
	out := make([]uint64, 0, len(base)+len(overlay))
	for _, id := range base {
		if !deleted(id) {
			out = append(out, id)
		}
	}
	for id := range overlay {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (gs *GraphStorage) membershipNodeIDsForTenantLocked(tid tenantid.TenantID) []uint64 {
	overlay := gs.tenantNodeIDs[tid]
	if gs.mmapSnap == nil {
		return sortedBucketIDs(overlay)
	}
	base := gs.mmapSnap.membershipRun(membKindNodeTenant, string(tid), "")
	return mergeBaseOverlay(base, overlay, gs.isNodeDeletedLocked)
}

func (gs *GraphStorage) membershipNodeIDsByLabelLocked(tid tenantid.TenantID, label string) []uint64 {
	var overlay map[uint64]struct{}
	if lm := gs.tenantNodesByLabel[tid]; lm != nil {
		overlay = lm[label]
	}
	if gs.mmapSnap == nil {
		return sortedBucketIDs(overlay)
	}
	base := gs.mmapSnap.membershipRun(membKindNodeLabel, string(tid), label)
	return mergeBaseOverlay(base, overlay, gs.isNodeDeletedLocked)
}

func (gs *GraphStorage) membershipEdgeIDsForTenantLocked(tid tenantid.TenantID) []uint64 {
	overlay := gs.tenantEdgeIDs[tid]
	if gs.mmapSnap == nil {
		return sortedBucketIDs(overlay)
	}
	base := gs.mmapSnap.membershipRun(membKindEdgeTenant, string(tid), "")
	return mergeBaseOverlay(base, overlay, gs.isEdgeDeletedLocked)
}

func (gs *GraphStorage) membershipEdgeIDsByTypeLocked(tid tenantid.TenantID, etype string) []uint64 {
	var overlay map[uint64]struct{}
	if tm := gs.tenantEdgesByType[tid]; tm != nil {
		overlay = tm[etype]
	}
	if gs.mmapSnap == nil {
		return sortedBucketIDs(overlay)
	}
	base := gs.mmapSnap.membershipRun(membKindEdgeType, string(tid), etype)
	return mergeBaseOverlay(base, overlay, gs.isEdgeDeletedLocked)
}

// membershipTenantsLocked returns every tenant ID seen in any of: the persisted
// snapshot stats, the post-open node overlay, or the post-open edge overlay.
func (gs *GraphStorage) membershipTenantsLocked() []tenantid.TenantID {
	seen := make(map[tenantid.TenantID]struct{})
	if gs.mmapSnap != nil {
		for _, t := range gs.mmapSnap.tenantList() {
			seen[tenantid.TenantID(t)] = struct{}{}
		}
	}
	for tid := range gs.tenantNodeIDs {
		seen[tid] = struct{}{}
	}
	for tid := range gs.tenantEdgeIDs {
		seen[tid] = struct{}{}
	}
	out := make([]tenantid.TenantID, 0, len(seen))
	for tid := range seen {
		out = append(out, tid)
	}
	return out
}

// membershipNodeIDsByLabelGlobalLocked unions kind-1 runs across all tenants.
func (gs *GraphStorage) membershipNodeIDsByLabelGlobalLocked(label string) []uint64 {
	if gs.mmapSnap == nil {
		return sortedBucketIDs(gs.nodesByLabel[label])
	}
	var all []uint64
	for _, tid := range gs.membershipTenantsLocked() {
		all = append(all, gs.membershipNodeIDsByLabelLocked(tid, label)...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	return all
}

// membershipEdgeIDsByTypeGlobalLocked unions kind-3 runs across all tenants.
func (gs *GraphStorage) membershipEdgeIDsByTypeGlobalLocked(etype string) []uint64 {
	if gs.mmapSnap == nil {
		return sortedBucketIDs(gs.edgesByType[etype])
	}
	var all []uint64
	for _, tid := range gs.membershipTenantsLocked() {
		all = append(all, gs.membershipEdgeIDsByTypeLocked(tid, etype)...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	return all
}

// membershipLabelsForTenantLocked returns label keys for a tenant (base dir ∪ overlay keys).
func (gs *GraphStorage) membershipLabelsForTenantLocked(tid tenantid.TenantID) []string {
	seen := make(map[string]struct{})
	if gs.mmapSnap != nil {
		for _, k := range gs.mmapSnap.membershipKeys(membKindNodeLabel, string(tid)) {
			seen[k] = struct{}{}
		}
	}
	if lm := gs.tenantNodesByLabel[tid]; lm != nil {
		for k := range lm {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// membershipEdgeTypesForTenantLocked mirrors the above for edge types.
func (gs *GraphStorage) membershipEdgeTypesForTenantLocked(tid tenantid.TenantID) []string {
	seen := make(map[string]struct{})
	if gs.mmapSnap != nil {
		for _, k := range gs.mmapSnap.membershipKeys(membKindEdgeType, string(tid)) {
			seen[k] = struct{}{}
		}
	}
	if tm := gs.tenantEdgesByType[tid]; tm != nil {
		for k := range tm {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
