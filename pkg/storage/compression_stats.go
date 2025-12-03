package storage

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
