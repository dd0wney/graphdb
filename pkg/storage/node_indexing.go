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

// updatePropertyIndexes updates property indexes when a node's properties change.
//
// Both Remove and Insert are gated on value.Type == idx.indexType, mirroring
// insertNodeIntoPropertyIndexes (the create path). A PropertyIndex holds one
// declared type, so only type-matching values are ever indexed: a mismatched
// NEW value is skipped (not indexable — same as the build path), and a
// mismatched OLD value is skipped on removal because it was never inserted, so
// idx.Remove would error "not found". Without the gates this helper left a
// partial apply — UpdateNode runs it before mutating node.Properties and does
// not roll back, so a removed-old-then-failed-insert dropped a value the node
// still carried, and a Remove of a never-indexed value failed the update
// outright. Completeness: every value in the index has the matching type
// (insert gates on it), so gating Remove never skips a removal that should fire.
func (gs *GraphStorage) updatePropertyIndexes(nodeID uint64, node *Node, properties map[string]Value) error {
	for k, newValue := range properties {
		if idx, exists := gs.propertyIndexes[k]; exists {
			// Remove old value only if it was actually indexed (type matched).
			if oldValue, exists := node.Properties[k]; exists && oldValue.Type == idx.indexType {
				if err := idx.Remove(nodeID, oldValue); err != nil {
					return fmt.Errorf("failed to remove from property index %s: %w", k, err)
				}
			}
			// Add new value only if its type matches the index.
			if newValue.Type == idx.indexType {
				if err := idx.Insert(nodeID, newValue); err != nil {
					return fmt.Errorf("failed to insert into property index %s: %w", k, err)
				}
			}
		}
	}
	return nil
}

// insertNodeIntoPropertyIndexes inserts a node into all matching property indexes.
//
// Type-mismatched values are SKIPPED, not treated as errors. A PropertyIndex
// holds a single declared type, so a value of another type is not indexable
// (graphdb is schemaless — property types are per-node). Mirrors the build path
// (CreatePropertyIndex / replayCreatePropertyIndex, which filter on
// prop.Type == valueType). This is also the fix for a partial-apply: the sole
// caller persistNodeLocked has already published the node into the shard map,
// label/tenant indexes and NodeCount by the time this runs, and createNodeLocked
// does not roll back on error — so a fatal type-mismatch here left the node
// half-committed (visible + counted, but the create returned an error). Skipping
// removes that failure mode and makes the live write agree with what a snapshot
// rebuild / WAL replay produces for the same node.
func (gs *GraphStorage) insertNodeIntoPropertyIndexes(nodeID uint64, properties map[string]Value) error {
	for key, value := range properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			if value.Type != idx.indexType {
				continue
			}
			if err := idx.Insert(nodeID, value); err != nil {
				return fmt.Errorf("failed to insert into property index %s: %w", key, err)
			}
		}
	}
	return nil
}

// removeNodeFromPropertyIndexes removes a node from all property indexes.
//
// Gated on value.Type == idx.indexType: a mismatched value was never indexed
// (insert skips it), so idx.Remove would error "not found" and fail the delete.
// Mirrors the gate in updatePropertyIndexes / the create path.
func (gs *GraphStorage) removeNodeFromPropertyIndexes(nodeID uint64, properties map[string]Value) error {
	for key, value := range properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			if value.Type != idx.indexType {
				continue
			}
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
