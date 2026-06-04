package storage

import (
	"fmt"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// CreatePropertyIndex creates an index on a node property
func (gs *GraphStorage) CreatePropertyIndex(propertyKey string, valueType ValueType) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check if index already exists
	if _, exists := gs.propertyIndexes[propertyKey]; exists {
		return fmt.Errorf("index on property %s already exists", propertyKey)
	}

	// Create new index
	idx := NewPropertyIndex(propertyKey, valueType)

	// Populate index with existing nodes
	var insertErr error
	gs.forEachNodeUnlocked(func(node *Node) bool {
		if prop, exists := node.Properties[propertyKey]; exists {
			if prop.Type == valueType {
				if err := idx.Insert(node.ID, prop); err != nil {
					insertErr = fmt.Errorf("failed to insert node %d into property index %s: %w", node.ID, propertyKey, err)
					return false
				}
			}
		}
		return true
	})
	if insertErr != nil {
		return insertErr
	}

	gs.propertyIndexes[propertyKey] = idx

	// Write to WAL for durability
	gs.writeToWAL(wal.OpCreatePropertyIndex, struct {
		PropertyKey string
		ValueType   ValueType
	}{
		PropertyKey: propertyKey,
		ValueType:   valueType,
	})

	return nil
}

// DropPropertyIndex removes an index on a node property
func (gs *GraphStorage) DropPropertyIndex(propertyKey string) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if _, exists := gs.propertyIndexes[propertyKey]; !exists {
		return fmt.Errorf("index on property %s does not exist", propertyKey)
	}

	delete(gs.propertyIndexes, propertyKey)

	// Write to WAL for durability
	gs.writeToWAL(wal.OpDropPropertyIndex, struct {
		PropertyKey string
	}{
		PropertyKey: propertyKey,
	})

	return nil
}

// FindNodesByPropertyIndexed uses an index to find nodes (O(1) lookup).
//
// Tenant-blind. The property index is global (not per-tenant), so
// this returns matches across every tenant. New callers in
// tenant-scoped code paths should prefer
// FindNodesByPropertyIndexedForTenant.
func (gs *GraphStorage) FindNodesByPropertyIndexed(key string, value Value) ([]*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Check if index exists
	idx, exists := gs.propertyIndexes[key]
	if !exists {
		return nil, fmt.Errorf("no index on property %s", key)
	}

	// Use index for O(1) lookup
	nodeIDs, err := idx.Lookup(value)
	if err != nil {
		return nil, err
	}

	// Fetch nodes
	return gs.buildNodeListFromIDs(nodeIDs), nil
}

// FindNodesByPropertyIndexedForTenant uses an index for the property
// lookup, then post-filters by tenant ownership.
// Audit A6c-storage (2026-05-08).
//
// The property index is global — keyed by property value, not
// (value, tenant). Per-tenant indexes would halve the scan work in
// the common case but are deferred: changes the on-disk index format
// (the audit's scope is correctness first, perf later). Until then,
// the post-index filter pays one extra pass per result set, which is
// acceptable because the index already collapses to O(matches), not
// O(nodes).
func (gs *GraphStorage) FindNodesByPropertyIndexedForTenant(key string, value Value, tenantID string) ([]*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	idx, exists := gs.propertyIndexes[key]
	if !exists {
		return nil, fmt.Errorf("no index on property %s", key)
	}

	nodeIDs, err := idx.Lookup(value)
	if err != nil {
		return nil, err
	}

	expected := effectiveTenantID(tenantID).String()
	out := make([]*Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, exists := gs.lookupNodeShard(nodeID)
		if !exists {
			continue
		}
		if node.TenantID != expected {
			continue
		}
		out = append(out, node.Clone())
	}
	return out, nil
}

// FindNodesByPropertyRangeAcrossTenants uses an index to find nodes in a range.
//
// Cross-tenant: returns matching nodes from ALL tenants (the property index is
// value-keyed, not tenant-partitioned). The explicit *AcrossTenants name (audit
// A3b) surfaces that — unlike the exact-match path there is no *ForTenant variant
// yet, and the only caller is cmd/benchmark-index. A request path must add a
// post-filtered *ForTenant variant (cf. FindNodesByPropertyIndexedForTenant)
// before exposing range queries.
func (gs *GraphStorage) FindNodesByPropertyRangeAcrossTenants(key string, start, end Value) ([]*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Check if index exists
	idx, exists := gs.propertyIndexes[key]
	if !exists {
		return nil, fmt.Errorf("no index on property %s", key)
	}

	// Use index for range lookup
	nodeIDs, err := idx.RangeLookup(start, end)
	if err != nil {
		return nil, err
	}

	// Fetch nodes
	return gs.buildNodeListFromIDs(nodeIDs), nil
}

// FindNodesByPropertyPrefixAcrossTenants uses an index to find nodes by string prefix.
//
// Cross-tenant (see FindNodesByPropertyRangeAcrossTenants): returns prefix matches
// from all tenants; only caller is cmd/benchmark-index. Add a post-filtered
// *ForTenant variant before exposing prefix queries on a request path.
func (gs *GraphStorage) FindNodesByPropertyPrefixAcrossTenants(key string, prefix string) ([]*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Check if index exists
	idx, exists := gs.propertyIndexes[key]
	if !exists {
		return nil, fmt.Errorf("no index on property %s", key)
	}

	// Use index for prefix lookup
	nodeIDs, err := idx.PrefixLookup(prefix)
	if err != nil {
		return nil, err
	}

	// Fetch nodes
	return gs.buildNodeListFromIDs(nodeIDs), nil
}

// HasPropertyIndex checks if an index exists for a given property key
func (gs *GraphStorage) HasPropertyIndex(key string) bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	_, exists := gs.propertyIndexes[key]
	return exists
}

// GetIndexStatistics returns statistics for all property indexes
func (gs *GraphStorage) GetIndexStatistics() map[string]IndexStatistics {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	stats := make(map[string]IndexStatistics)
	for key, idx := range gs.propertyIndexes {
		stats[key] = idx.GetStatistics()
	}

	return stats
}
