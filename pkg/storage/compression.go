package storage

import (
	"encoding/binary"
	"fmt"
	"log"
	"sort"
)

// CompressedEdgeList stores a list of edge target node IDs in compressed format
// Uses delta encoding + varint compression for memory efficiency
type CompressedEdgeList struct {
	BaseNodeID uint64 // First node ID in the list (exported for gob encoding)
	Deltas     []byte // Varint-encoded deltas between consecutive sorted node IDs (exported for gob encoding)
	EdgeCount  int    // Number of edges in the list (exported for gob encoding)
}

// NewCompressedEdgeList creates a compressed edge list from node IDs
// Uses buffer pooling to reduce GC pressure.
// Returns error if data corruption is detected (should never happen in normal operation).
func NewCompressedEdgeList(nodeIDs []uint64) (*CompressedEdgeList, error) {
	if len(nodeIDs) == 0 {
		return &CompressedEdgeList{
			Deltas:    []byte{},
			EdgeCount: 0,
		}, nil
	}

	// Sort node IDs for better compression with delta encoding
	// Use pooled buffer for sorting
	sorted := getUint64Slice(len(nodeIDs))
	sorted = append(sorted, nodeIDs...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// First node is stored as base
	base := sorted[0]

	// Encode deltas with varint - use pooled byte buffer
	buf := getByteSlice(len(nodeIDs) * 2) // Initial estimate

	for i := 1; i < len(sorted); i++ {
		// Validate sorted order to prevent underflow
		if sorted[i] < sorted[i-1] {
			// Return buffers before returning error
			putUint64Slice(sorted)
			putByteSlice(buf)
			// This should never happen due to sort above, but guards against bugs
			return nil, fmt.Errorf("compression: unsorted data detected at index %d (%d < %d)",
				i, sorted[i], sorted[i-1])
		}
		delta := sorted[i] - sorted[i-1]
		buf = binary.AppendUvarint(buf, delta)
	}

	// Copy buf to final slice (so we can return pool buffer)
	deltas := make([]byte, len(buf))
	copy(deltas, buf)

	// Return buffers to pool
	putUint64Slice(sorted)
	putByteSlice(buf)

	return &CompressedEdgeList{
		BaseNodeID: base,
		Deltas:     deltas,
		EdgeCount:  len(nodeIDs),
	}, nil
}

// Decompress returns the original list of node IDs
// Uses buffer pooling to reduce GC pressure
func (c *CompressedEdgeList) Decompress() []uint64 {
	if c.EdgeCount == 0 {
		return []uint64{}
	}

	// Get buffer from pool instead of allocating
	result := getUint64Slice(c.EdgeCount)
	result = append(result, c.BaseNodeID)

	if c.EdgeCount == 1 {
		return result
	}

	current := c.BaseNodeID
	buf := c.Deltas

	for len(buf) > 0 {
		delta, n := binary.Uvarint(buf)
		if n <= 0 {
			// Defensive: log corrupted data detection
			log.Printf("Warning: corrupted varint detected in compressed edge list (baseID=%d, expected=%d, got=%d edges)",
				c.BaseNodeID, c.EdgeCount, len(result))
			break
		}
		// Check for overflow before adding
		if current > ^uint64(0)-delta { // Would overflow
			// Defensive: log overflow detection
			log.Printf("Warning: overflow detected in compressed edge list (baseID=%d, current=%d, delta=%d)",
				c.BaseNodeID, current, delta)
			break
		}
		current += delta
		result = append(result, current)
		buf = buf[n:]
	}

	// Defensive: verify we got expected number of edges
	if len(result) != c.EdgeCount {
		log.Printf("Warning: edge count mismatch in decompression (baseID=%d, expected=%d, got=%d)",
			c.BaseNodeID, c.EdgeCount, len(result))
	}

	return result
}

// Count returns the number of edges in the compressed list
func (c *CompressedEdgeList) Count() int {
	return c.EdgeCount
}

// Size returns the memory size in bytes
func (c *CompressedEdgeList) Size() int {
	return 8 + len(c.Deltas) + 4 // baseNodeID (8) + deltas + count (4)
}

// UncompressedSize returns the size if this list was uncompressed
func (c *CompressedEdgeList) UncompressedSize() int {
	return c.EdgeCount * 8 // Each uint64 is 8 bytes
}

// CompressionRatio returns the compression ratio (uncompressed / compressed)
func (c *CompressedEdgeList) CompressionRatio() float64 {
	if c.Size() == 0 {
		return 0
	}
	return float64(c.UncompressedSize()) / float64(c.Size())
}

// Add adds a new node ID to the compressed list
// Note: This is less efficient than batch creation and should be used sparingly
func (c *CompressedEdgeList) Add(nodeID uint64) (*CompressedEdgeList, error) {
	// Decompress, add, and recompress
	nodeIDs := c.Decompress()
	nodeIDs = append(nodeIDs, nodeID)
	return NewCompressedEdgeList(nodeIDs)
}

// Remove removes a node ID from the compressed list
// Note: This is less efficient than batch creation and should be used sparingly
func (c *CompressedEdgeList) Remove(nodeID uint64) (*CompressedEdgeList, error) {
	// Decompress, remove, and recompress
	nodeIDs := c.Decompress()

	// Find and remove the node ID
	for i, id := range nodeIDs {
		if id == nodeID {
			nodeIDs = append(nodeIDs[:i], nodeIDs[i+1:]...)
			break
		}
	}

	return NewCompressedEdgeList(nodeIDs)
}

// Contains checks if a node ID exists in the compressed list
// Uses binary search since the list is sorted
func (c *CompressedEdgeList) Contains(nodeID uint64) bool {
	if c.EdgeCount == 0 {
		return false
	}

	// Quick checks
	if nodeID < c.BaseNodeID {
		return false
	}

	if c.EdgeCount == 1 {
		return nodeID == c.BaseNodeID
	}

	// Decompress and search
	// TODO: Optimize this to search without full decompression
	nodeIDs := c.Decompress()

	// Binary search (overflow-safe)
	left, right := 0, len(nodeIDs)-1
	for left <= right {
		mid := left + (right-left)/2 // Overflow-safe
		switch {
		case nodeIDs[mid] == nodeID:
			return true
		case nodeIDs[mid] < nodeID:
			left = mid + 1
		default:
			right = mid - 1
		}
	}

	return false
}
