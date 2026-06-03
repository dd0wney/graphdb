package storage

import (
	"fmt"
	"sort"
)

// labelIndex is the shape of both the global (nodesByLabel / edgesByType) and
// the inner per-tenant (tenantNodesByLabel[tid] / tenantEdgesByType[tid])
// label/type indexes: a label-or-type string -> the set of IDs carrying it.
//
// A set rather than a []uint64 gives O(1) add/remove. The slice form paid an
// O(K) linear scan on every removal, and DeleteNode removes from BOTH the
// global and per-tenant index, so bulk delete / tenant offboarding was O(N^2)
// (Track P / M3). Removal is the hot path the set targets.
type labelIndex = map[string]map[uint64]struct{}

// addToLabelIndex records id under key, creating the bucket on first use.
// Idempotent — re-adding an existing id is a no-op, which makes the
// snapshot-load + WAL-replay rebuild safe to double-apply (Path C rebuilds
// these indexes from the flat node/edge set on load rather than deserializing
// them).
func addToLabelIndex(idx labelIndex, key string, id uint64) {
	bucket := idx[key]
	if bucket == nil {
		bucket = make(map[uint64]struct{})
		idx[key] = bucket
	}
	bucket[id] = struct{}{}
}

// removeFromLabelIndexSet drops id from key's bucket in O(1), GCing the bucket
// once it empties so an emptied label/type leaves no residue. Used by the
// per-tenant index, where dropping an offboarded tenant's empty buckets is the
// intended hygiene (see removeNodeFromTenantIndex).
func removeFromLabelIndexSet(idx labelIndex, key string, id uint64) {
	bucket := idx[key]
	if bucket == nil {
		return
	}
	delete(bucket, id)
	if len(bucket) == 0 {
		delete(idx, key)
	}
}

// removeFromLabelIndexKeepEmpty drops id from key's bucket in O(1) but leaves an
// empty bucket in place. The GLOBAL index uses this so a label/type stays
// "registered" — visible to GetAllLabels and the GraphQL schema generated from
// it — after its last node/edge is deleted. This matches the pre-Path-C global
// behavior, where swap-with-last removal left an empty []uint64 under the key.
// (Contrast removeFromLabelIndexSet, which GCs empties for the per-tenant index.)
func removeFromLabelIndexKeepEmpty(idx labelIndex, key string, id uint64) {
	if bucket := idx[key]; bucket != nil {
		delete(bucket, id)
	}
}

// sortedBucketIDs returns a bucket's IDs in ascending order. Set iteration is
// randomized, so any path that exposes IDs to a caller (Find* / Get*ForTenant)
// sorts on read to keep results deterministic for pagination and stable for
// test/consumer contracts.
func sortedBucketIDs(bucket map[uint64]struct{}) []uint64 {
	ids := make([]uint64, 0, len(bucket))
	for id := range bucket {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// flattenLabelIndex converts a set-based label index to the sorted-slice form
// persisted in the snapshot struct. The on-disk JSON shape (map[string][]uint64)
// is kept byte-for-byte identical to the pre-Path-C format — no version bump —
// even though the value is rebuilt from the flat node/edge set on load and the
// serialized copy is never read back (kept only for format stability and for
// any external reader of the snapshot). Sorted for deterministic disk bytes.
func flattenLabelIndex(idx labelIndex) map[string][]uint64 {
	out := make(map[string][]uint64, len(idx))
	for key, bucket := range idx {
		out[key] = sortedBucketIDs(bucket)
	}
	return out
}

// Property index helper methods

// updatePropertyIndexes updates property indexes when a node's properties change
func (gs *GraphStorage) updatePropertyIndexes(nodeID uint64, node *Node, properties map[string]Value) error {
	for k, newValue := range properties {
		if idx, exists := gs.propertyIndexes[k]; exists {
			// Remove old value from index if it exists
			if oldValue, exists := node.Properties[k]; exists {
				if err := idx.Remove(nodeID, oldValue); err != nil {
					return fmt.Errorf("failed to remove from property index %s: %w", k, err)
				}
			}
			// Add new value to index
			if err := idx.Insert(nodeID, newValue); err != nil {
				return fmt.Errorf("failed to insert into property index %s: %w", k, err)
			}
		}
	}
	return nil
}

// insertNodeIntoPropertyIndexes inserts a node into all matching property indexes
func (gs *GraphStorage) insertNodeIntoPropertyIndexes(nodeID uint64, properties map[string]Value) error {
	for key, value := range properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			if err := idx.Insert(nodeID, value); err != nil {
				return fmt.Errorf("failed to insert into property index %s: %w", key, err)
			}
		}
	}
	return nil
}

// removeNodeFromPropertyIndexes removes a node from all property indexes
func (gs *GraphStorage) removeNodeFromPropertyIndexes(nodeID uint64, properties map[string]Value) error {
	for key, value := range properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			if err := idx.Remove(nodeID, value); err != nil {
				return fmt.Errorf("failed to remove from property index %s: %w", key, err)
			}
		}
	}
	return nil
}

// removeFromLabelIndex removes a node from the global label index in O(1),
// keeping the (now possibly empty) label bucket so the label stays registered.
func (gs *GraphStorage) removeFromLabelIndex(label string, nodeID uint64) {
	removeFromLabelIndexKeepEmpty(gs.nodesByLabel, label, nodeID)
}
