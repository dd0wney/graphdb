package storage

import (
	"fmt"
	"sort"
	"sync"
)

// PropertyIndex maintains an index on a specific node property
type PropertyIndex struct {
	propertyKey string
	indexType   ValueType

	// Index maps property value -> list of node IDs
	// We use string keys for simplicity (convert all values to strings)
	index map[string][]uint64

	mu sync.RWMutex
}

// NewPropertyIndex creates a new property index
func NewPropertyIndex(propertyKey string, indexType ValueType) *PropertyIndex {
	return &PropertyIndex{
		propertyKey: propertyKey,
		indexType:   indexType,
		index:       make(map[string][]uint64),
	}
}

// Insert adds a node to the index
func (idx *PropertyIndex) Insert(nodeID uint64, value Value) error {
	if value.Type != idx.indexType {
		return fmt.Errorf("value type mismatch: expected %v, got %v", idx.indexType, value.Type)
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	key := idx.valueToKey(value)
	idx.index[key] = append(idx.index[key], nodeID)

	return nil
}

// Remove removes a node from the index
func (idx *PropertyIndex) Remove(nodeID uint64, value Value) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	key := idx.valueToKey(value)
	nodeIDs := idx.index[key]

	// Find and remove the node ID
	for i, id := range nodeIDs {
		if id == nodeID {
			// Remove by swapping with last element
			nodeIDs[i] = nodeIDs[len(nodeIDs)-1]
			idx.index[key] = nodeIDs[:len(nodeIDs)-1]

			// Clean up empty entries
			if len(idx.index[key]) == 0 {
				delete(idx.index, key)
			}

			return nil
		}
	}

	return fmt.Errorf("node %d not found in index", nodeID)
}

// Lookup finds all nodes with a specific property value
func (idx *PropertyIndex) Lookup(value Value) ([]uint64, error) {
	if value.Type != idx.indexType {
		return nil, fmt.Errorf("value type mismatch: expected %v, got %v", idx.indexType, value.Type)
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	key := idx.valueToKey(value)
	nodeIDs := idx.index[key]

	// Return a copy to prevent external modification
	result := make([]uint64, len(nodeIDs))
	copy(result, nodeIDs)

	return result, nil
}

// RangeLookup finds all nodes with property values in a range [start, end]
// This is useful for numeric and timestamp ranges
func (idx *PropertyIndex) RangeLookup(start, end Value) ([]uint64, error) {
	if start.Type != idx.indexType || end.Type != idx.indexType {
		return nil, fmt.Errorf("value type mismatch")
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	startKey := idx.valueToKey(start)
	endKey := idx.valueToKey(end)

	// Get all keys in range
	var result []uint64
	for key, nodeIDs := range idx.index {
		if key >= startKey && key <= endKey {
			result = append(result, nodeIDs...)
		}
	}

	return result, nil
}

// PrefixLookup finds all nodes with string properties starting with a prefix
func (idx *PropertyIndex) PrefixLookup(prefix string) ([]uint64, error) {
	if idx.indexType != TypeString {
		return nil, fmt.Errorf("prefix lookup only supported for string indexes")
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var result []uint64
	for key, nodeIDs := range idx.index {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			result = append(result, nodeIDs...)
		}
	}

	return result, nil
}

// GetStatistics returns index statistics
func (idx *PropertyIndex) GetStatistics() IndexStatistics {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// Use int64 to prevent overflow with billions of nodes
	var totalNodes int64
	for _, nodeIDs := range idx.index {
		totalNodes += int64(len(nodeIDs))
	}

	return IndexStatistics{
		PropertyKey:    idx.propertyKey,
		UniqueValues:   len(idx.index),
		TotalNodes:     int(totalNodes), // Safe to cast back for statistics
		AvgNodesPerKey: float64(totalNodes) / float64(max(len(idx.index), 1)),
	}
}

// GetAllKeys returns all indexed keys (useful for debugging)
func (idx *PropertyIndex) GetAllKeys() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	keys := make([]string, 0, len(idx.index))
	for key := range idx.index {
		keys = append(keys, key)
	}

	sort.Strings(keys)
	return keys
}

// valueToKey converts a Value to a string key for indexing
func (idx *PropertyIndex) valueToKey(value Value) string {
	switch value.Type {
	case TypeString:
		str, _ := value.AsString()
		return str
	case TypeInt:
		// Use zero-padded format for numeric sorting
		// Add bias to handle negative numbers correctly
		// This ensures proper lexical ordering: negative < 0 < positive
		intVal, _ := value.AsInt()
		// Add bias of 2^63 to shift range [MinInt64, MaxInt64] to [0, MaxUint64]
		biased := uint64(intVal) + (1 << 63)
		return fmt.Sprintf("%020d", biased)
	case TypeFloat:
		floatVal, _ := value.AsFloat()
		return fmt.Sprintf("%020.6f", floatVal)
	case TypeBool:
		boolVal, _ := value.AsBool()
		if boolVal {
			return "1"
		}
		return "0"
	case TypeTimestamp:
		ts, _ := value.AsTimestamp()
		// Unix timestamp can be negative (before 1970)
		// Add bias to handle negative timestamps correctly
		intVal := ts.Unix()
		biased := uint64(intVal) + (1 << 63)
		return fmt.Sprintf("%020d", biased)
	case TypeBytes:
		return string(value.Data)
	default:
		return string(value.Data)
	}
}

// IndexStatistics holds statistics about an index
type IndexStatistics struct {
	PropertyKey    string
	UniqueValues   int
	TotalNodes     int
	AvgNodesPerKey float64
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// CompositeIndex maintains an index on multiple node properties.
// This enables efficient lookups when filtering by multiple properties simultaneously.
// For example, a composite index on (domain, status) allows O(1) lookups for
// "all concepts in domain 'math' with status 'active'".
type CompositeIndex struct {
	// propertyKeys are the properties that form the composite key, in order
	propertyKeys []string

	// indexTypes are the value types for each property key
	indexTypes []ValueType

	// index maps composite key string -> list of node IDs
	index map[string][]uint64

	// separator used between key parts (must not appear in values)
	separator string

	mu sync.RWMutex
}

// NewCompositeIndex creates a new composite index on multiple properties.
// The order of keys matters - lookups must provide values in the same order.
func NewCompositeIndex(propertyKeys []string, indexTypes []ValueType) *CompositeIndex {
	if len(propertyKeys) != len(indexTypes) {
		panic("propertyKeys and indexTypes must have the same length")
	}
	if len(propertyKeys) < 2 {
		panic("composite index requires at least 2 property keys")
	}

	return &CompositeIndex{
		propertyKeys: propertyKeys,
		indexTypes:   indexTypes,
		index:        make(map[string][]uint64),
		separator:    "\x00", // NULL byte as separator
	}
}

// PropertyKeys returns the property keys this index covers
func (idx *CompositeIndex) PropertyKeys() []string {
	return idx.propertyKeys
}

// Insert adds a node to the composite index
func (idx *CompositeIndex) Insert(nodeID uint64, values []Value) error {
	if len(values) != len(idx.propertyKeys) {
		return fmt.Errorf("expected %d values, got %d", len(idx.propertyKeys), len(values))
	}

	// Validate types
	for i, val := range values {
		if val.Type != idx.indexTypes[i] {
			return fmt.Errorf("type mismatch at position %d: expected %v, got %v",
				i, idx.indexTypes[i], val.Type)
		}
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	key := idx.valuesToKey(values)
	idx.index[key] = append(idx.index[key], nodeID)

	return nil
}

// Remove removes a node from the composite index
func (idx *CompositeIndex) Remove(nodeID uint64, values []Value) error {
	if len(values) != len(idx.propertyKeys) {
		return fmt.Errorf("expected %d values, got %d", len(idx.propertyKeys), len(values))
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	key := idx.valuesToKey(values)
	nodeIDs := idx.index[key]

	for i, id := range nodeIDs {
		if id == nodeID {
			nodeIDs[i] = nodeIDs[len(nodeIDs)-1]
			idx.index[key] = nodeIDs[:len(nodeIDs)-1]

			if len(idx.index[key]) == 0 {
				delete(idx.index, key)
			}
			return nil
		}
	}

	return fmt.Errorf("node %d not found in composite index", nodeID)
}

// Lookup finds all nodes matching all property values (exact match)
func (idx *CompositeIndex) Lookup(values []Value) ([]uint64, error) {
	if len(values) != len(idx.propertyKeys) {
		return nil, fmt.Errorf("expected %d values, got %d", len(idx.propertyKeys), len(values))
	}

	// Validate types
	for i, val := range values {
		if val.Type != idx.indexTypes[i] {
			return nil, fmt.Errorf("type mismatch at position %d: expected %v, got %v",
				i, idx.indexTypes[i], val.Type)
		}
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	key := idx.valuesToKey(values)
	nodeIDs := idx.index[key]

	result := make([]uint64, len(nodeIDs))
	copy(result, nodeIDs)

	return result, nil
}

// PrefixLookup finds all nodes matching a prefix of the composite key.
// This allows lookups like "all nodes with domain='math'" without specifying
// the second property. The values slice must be a prefix of the full key.
func (idx *CompositeIndex) PrefixLookup(values []Value) ([]uint64, error) {
	if len(values) > len(idx.propertyKeys) {
		return nil, fmt.Errorf("too many values: expected at most %d, got %d",
			len(idx.propertyKeys), len(values))
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one value required for prefix lookup")
	}

	// Validate types for provided values
	for i, val := range values {
		if val.Type != idx.indexTypes[i] {
			return nil, fmt.Errorf("type mismatch at position %d: expected %v, got %v",
				i, idx.indexTypes[i], val.Type)
		}
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// Build prefix key
	prefix := idx.valuesToKeyPrefix(values)

	var result []uint64
	for key, nodeIDs := range idx.index {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			result = append(result, nodeIDs...)
		}
	}

	return result, nil
}

// GetStatistics returns statistics about the composite index
func (idx *CompositeIndex) GetStatistics() CompositeIndexStatistics {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var totalNodes int64
	for _, nodeIDs := range idx.index {
		totalNodes += int64(len(nodeIDs))
	}

	return CompositeIndexStatistics{
		PropertyKeys:   idx.propertyKeys,
		UniqueKeys:     len(idx.index),
		TotalNodes:     int(totalNodes),
		AvgNodesPerKey: float64(totalNodes) / float64(max(len(idx.index), 1)),
	}
}

// valuesToKey converts multiple values to a composite key string
func (idx *CompositeIndex) valuesToKey(values []Value) string {
	result := ""
	for i, val := range values {
		if i > 0 {
			result += idx.separator
		}
		result += idx.singleValueToKey(val)
	}
	return result
}

// valuesToKeyPrefix converts a prefix of values to a partial key for prefix matching
func (idx *CompositeIndex) valuesToKeyPrefix(values []Value) string {
	result := ""
	for i, val := range values {
		if i > 0 {
			result += idx.separator
		}
		result += idx.singleValueToKey(val)
	}
	// Add separator at end to ensure we only match complete value prefixes
	result += idx.separator
	return result
}

// singleValueToKey converts a single Value to a key component
func (idx *CompositeIndex) singleValueToKey(value Value) string {
	switch value.Type {
	case TypeString:
		str, _ := value.AsString()
		return str
	case TypeInt:
		intVal, _ := value.AsInt()
		biased := uint64(intVal) + (1 << 63)
		return fmt.Sprintf("%020d", biased)
	case TypeFloat:
		floatVal, _ := value.AsFloat()
		return fmt.Sprintf("%020.6f", floatVal)
	case TypeBool:
		boolVal, _ := value.AsBool()
		if boolVal {
			return "1"
		}
		return "0"
	case TypeTimestamp:
		ts, _ := value.AsTimestamp()
		intVal := ts.Unix()
		biased := uint64(intVal) + (1 << 63)
		return fmt.Sprintf("%020d", biased)
	default:
		return string(value.Data)
	}
}

// CompositeIndexStatistics holds statistics about a composite index
type CompositeIndexStatistics struct {
	PropertyKeys   []string
	UniqueKeys     int
	TotalNodes     int
	AvgNodesPerKey float64
}
