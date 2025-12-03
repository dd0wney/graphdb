package storage

import "fmt"

// CompressEdgeLists compresses all uncompressed edge lists
// This can be called periodically to reduce memory usage
func (gs *GraphStorage) CompressEdgeLists() error {
	if !gs.useEdgeCompression {
		return fmt.Errorf("edge compression is not enabled")
	}

	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Compress all edge lists using helper
	gs.compressAllEdgeLists()

	return nil
}

// GetCompressionStats returns compression statistics
func (gs *GraphStorage) GetCompressionStats() CompressionStats {
	if !gs.useEdgeCompression {
		return CompressionStats{}
	}

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	outgoingLists := make([]*CompressedEdgeList, 0, len(gs.compressedOutgoing))
	for _, list := range gs.compressedOutgoing {
		outgoingLists = append(outgoingLists, list)
	}

	incomingLists := make([]*CompressedEdgeList, 0, len(gs.compressedIncoming))
	for _, list := range gs.compressedIncoming {
		incomingLists = append(incomingLists, list)
	}

	allLists := append(outgoingLists, incomingLists...)
	return CalculateCompressionStats(allLists)
}
