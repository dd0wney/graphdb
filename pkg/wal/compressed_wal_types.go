package wal

import (
	"bufio"
	"os"
	"sync"
)

// CompressedWAL is a Write-Ahead Log with snappy compression
type CompressedWAL struct {
	file       *os.File
	writer     *bufio.Writer
	currentLSN uint64
	dataDir    string
	mu         sync.Mutex

	// Statistics
	totalWrites       uint64
	bytesUncompressed uint64
	bytesCompressed   uint64
}

// CompressedWALStats holds compression statistics
type CompressedWALStats struct {
	TotalWrites       uint64
	BytesUncompressed uint64
	BytesCompressed   uint64
	CompressionRatio  float64 // e.g., 0.75 = 75% compression
	SpaceSavings      float64 // MB saved
}
