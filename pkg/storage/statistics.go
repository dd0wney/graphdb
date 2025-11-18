package storage

import (
	"math"
	"sync/atomic"
	"time"
)

// GetStatistics returns current database statistics
func (gs *GraphStorage) GetStatistics() Statistics {
	return Statistics{
		NodeCount:    atomic.LoadUint64(&gs.stats.NodeCount),
		EdgeCount:    atomic.LoadUint64(&gs.stats.EdgeCount),
		TotalQueries: atomic.LoadUint64(&gs.stats.TotalQueries),
		LastSnapshot: gs.stats.LastSnapshot,
		AvgQueryTime: math.Float64frombits(atomic.LoadUint64(&gs.avgQueryTimeBits)),
	}
}

// trackQueryTime records query execution time for statistics
// Uses exponential moving average with atomic operations for thread-safety
func (gs *GraphStorage) trackQueryTime(duration time.Duration) {
	atomic.AddUint64(&gs.stats.TotalQueries, 1)

	// Update average query time (milliseconds)
	// Using exponential moving average: new_avg = 0.9 * old_avg + 0.1 * new_value
	durationMs := float64(duration.Nanoseconds()) / 1000000.0

	// Thread-safe update using compare-and-swap loop
	for {
		oldBits := atomic.LoadUint64(&gs.avgQueryTimeBits)
		oldAvg := math.Float64frombits(oldBits)
		newAvg := 0.9*oldAvg + 0.1*durationMs
		newBits := math.Float64bits(newAvg)

		if atomic.CompareAndSwapUint64(&gs.avgQueryTimeBits, oldBits, newBits) {
			break
		}
		// CAS failed, retry with new value
	}
}

// startQueryTiming begins query time tracking and returns a cleanup function
// Usage: defer gs.startQueryTiming()()
func (gs *GraphStorage) startQueryTiming() func() {
	start := time.Now()
	return func() {
		gs.trackQueryTime(time.Since(start))
	}
}
