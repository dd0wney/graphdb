package storage

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// CompressedEdgeList stores a list of edge target node IDs in compressed format
// Uses delta encoding + varint compression for memory efficiency
type CompressedEdgeList struct {
	baseNodeID uint64 // First node ID in the list
	deltas     []byte // Varint-encoded deltas between consecutive sorted node IDs
	count      int    // Number of edges in the list
}

// NewCompressedEdgeList creates a compressed edge list from node IDs
func NewCompressedEdgeList(nodeIDs []uint64) *CompressedEdgeList {
	if len(nodeIDs) == 0 {
		return &CompressedEdgeList{
			deltas: []byte{},
			count:  0,
		}
	}

	// Sort node IDs for better compression with delta encoding
	sorted := make([]uint64, len(nodeIDs))
	copy(sorted, nodeIDs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// First node is stored as base
	base := sorted[0]

	// Encode deltas with varint
	buf := make([]byte, 0, len(nodeIDs)*2) // Initial estimate

	for i := 1; i < len(sorted); i++ {
		// Validate sorted order to prevent underflow
		if sorted[i] < sorted[i-1] {
			// This should never happen due to sort above, but guards against bugs
			panic(fmt.Sprintf("compression: unsorted data detected at index %d (%d < %d)",
				i, sorted[i], sorted[i-1]))
		}
		delta := sorted[i] - sorted[i-1]
		buf = binary.AppendUvarint(buf, delta)
	}

	return &CompressedEdgeList{
		baseNodeID: base,
		deltas:     buf,
		count:      len(nodeIDs),
	}
}

// Decompress returns the original list of node IDs
func (c *CompressedEdgeList) Decompress() []uint64 {
	if c.count == 0 {
		return []uint64{}
	}

	result := make([]uint64, 0, c.count)
	result = append(result, c.baseNodeID)

	if c.count == 1 {
		return result
	}

	current := c.baseNodeID
	buf := c.deltas

	for len(buf) > 0 {
		delta, n := binary.Uvarint(buf)
		if n <= 0 {
			// Corrupted data
			break
		}
		// Check for overflow before adding
		if current > ^uint64(0)-delta { // Would overflow
			// Corrupted compressed data
			break
		}
		current += delta
		result = append(result, current)
		buf = buf[n:]
	}

	return result
}

// Count returns the number of edges in the compressed list
func (c *CompressedEdgeList) Count() int {
	return c.count
}

// Size returns the memory size in bytes
func (c *CompressedEdgeList) Size() int {
	return 8 + len(c.deltas) + 4 // baseNodeID (8) + deltas + count (4)
}

// UncompressedSize returns the size if this list was uncompressed
func (c *CompressedEdgeList) UncompressedSize() int {
	return c.count * 8 // Each uint64 is 8 bytes
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
func (c *CompressedEdgeList) Add(nodeID uint64) *CompressedEdgeList {
	// Decompress, add, and recompress
	nodeIDs := c.Decompress()
	nodeIDs = append(nodeIDs, nodeID)
	return NewCompressedEdgeList(nodeIDs)
}

// Remove removes a node ID from the compressed list
// Note: This is less efficient than batch creation and should be used sparingly
func (c *CompressedEdgeList) Remove(nodeID uint64) *CompressedEdgeList {
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
	if c.count == 0 {
		return false
	}

	// Quick checks
	if nodeID < c.baseNodeID {
		return false
	}

	if c.count == 1 {
		return nodeID == c.baseNodeID
	}

	// Decompress and search
	// TODO: Optimize this to search without full decompression
	nodeIDs := c.Decompress()

	// Binary search (overflow-safe)
	left, right := 0, len(nodeIDs)-1
	for left <= right {
		mid := left + (right-left)/2 // Overflow-safe
		if nodeIDs[mid] == nodeID {
			return true
		} else if nodeIDs[mid] < nodeID {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	return false
}

// CompressionStats tracks compression statistics across all edge lists
type CompressionStats struct {
	TotalLists        int
	TotalEdges        int64
	UncompressedBytes int64
	CompressedBytes   int64
	AverageRatio      float64
}

// CalculateCompressionStats calculates statistics for multiple compressed lists
func CalculateCompressionStats(lists []*CompressedEdgeList) CompressionStats {
	stats := CompressionStats{
		TotalLists: len(lists),
	}

	totalRatio := 0.0

	for _, list := range lists {
		// Accumulate with overflow checking
		newEdges := stats.TotalEdges + int64(list.Count())
		newUncompressed := stats.UncompressedBytes + int64(list.UncompressedSize())
		newCompressed := stats.CompressedBytes + int64(list.Size())

		// Check for overflow (defensive programming)
		if newEdges < stats.TotalEdges || newUncompressed < stats.UncompressedBytes || newCompressed < stats.CompressedBytes {
			// Overflow detected, cap at max value
			stats.TotalEdges = 9223372036854775807 // math.MaxInt64
			stats.UncompressedBytes = 9223372036854775807
			stats.CompressedBytes = 9223372036854775807
			break
		}

		stats.TotalEdges = newEdges
		stats.UncompressedBytes = newUncompressed
		stats.CompressedBytes = newCompressed
		totalRatio += list.CompressionRatio()
	}

	if stats.TotalLists > 0 {
		stats.AverageRatio = totalRatio / float64(stats.TotalLists)
	}

	return stats
}
