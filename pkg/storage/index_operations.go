package storage

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
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
	for nodeID, node := range gs.nodes {
		if prop, exists := node.Properties[propertyKey]; exists {
			if prop.Type == valueType {
				if err := idx.Insert(nodeID, prop); err != nil {
					return fmt.Errorf("failed to insert node %d into property index %s: %w", nodeID, propertyKey, err)
				}
			}
		}
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

// FindNodesByPropertyIndexed uses an index to find nodes (O(1) lookup)
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

// FindNodesByPropertyRange uses an index to find nodes in a range
func (gs *GraphStorage) FindNodesByPropertyRange(key string, start, end Value) ([]*Node, error) {
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

// FindNodesByPropertyPrefix uses an index to find nodes by string prefix
func (gs *GraphStorage) FindNodesByPropertyPrefix(key string, prefix string) ([]*Node, error) {
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
