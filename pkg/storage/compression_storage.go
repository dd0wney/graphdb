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

	// An mmap-backed store never compresses its overlay — see the guard and
	// its comment in snapshotWithBoundary (persistence.go) for the
	// mechanism this avoids. This is the public manual entry point, so
	// refuse loudly rather than silently no-op: a caller polling this on a
	// timer must see the error, not a false "success" that did nothing.
	// gs.mmapSnap changes only under gs.mu, so check it under the lock
	// already held above.
	if gs.mmapSnap != nil {
		return fmt.Errorf("edge compression does not apply to an mmap-backed store")
	}

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
